package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/diskscan"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// ---- Disk Analyzer -----------------------------------------------------------

type diskState struct {
	mu     sync.Mutex
	tree   *diskscan.Tree
	cancel context.CancelFunc
}

var disk diskState

// DiskDrives lists local drives plus the usual folders to analyze.
func (a *api) DiskDrives() map[string]any {
	drives := sysutil.FixedDrives()
	if drives == nil {
		drives = []sysutil.Drive{}
	}
	return map[string]any{"drives": drives, "folders": a.DupeDefaults()}
}

// DiskScan measures a drive or folder. Progress arrives as "diskProgress"
// and the end as "diskDone".
func (a *api) DiskScan(root string) error {
	root = filepath.Clean(root)
	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		return errors.New("That folder does not exist.")
	}
	disk.mu.Lock()
	if disk.cancel != nil {
		disk.mu.Unlock()
		return errors.New("A scan is already running.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	disk.cancel = cancel
	disk.mu.Unlock()
	go func() {
		defer cancel()
		last := time.Now()
		t, err := diskscan.Scan(ctx, root, func(files int, bytes int64, path string) {
			if time.Since(last) < 250*time.Millisecond {
				return
			}
			last = time.Now()
			a.emit("diskProgress", map[string]any{"files": files, "bytes": bytes, "path": path})
		})
		disk.mu.Lock()
		disk.cancel = nil
		if t != nil {
			disk.tree = t
		}
		disk.mu.Unlock()
		p := map[string]any{"root": root}
		if err != nil && !errors.Is(err, context.Canceled) {
			p["error"] = err.Error()
		}
		if t != nil {
			p["files"] = t.Files
			p["bytes"] = t.Bytes
			p["denied"] = t.Denied
			p["took"] = t.Took.Round(time.Second).String()
			p["cancelled"] = errors.Is(err, context.Canceled)
		}
		a.emit("diskDone", p)
	}()
	return nil
}

// DiskCancel stops the running scan. What was measured so far is kept.
func (a *api) DiskCancel() {
	disk.mu.Lock()
	defer disk.mu.Unlock()
	if disk.cancel != nil {
		disk.cancel()
	}
}

type diskChildDTO struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Files  int    `json:"files"`
	Denied bool   `json:"denied"`
	Dir    bool   `json:"dir"`
}

// DiskNode returns one folder of the last scan: its subfolders and the
// largest files directly inside it, biggest first.
func (a *api) DiskNode(path string) (map[string]any, error) {
	disk.mu.Lock()
	t := disk.tree
	disk.mu.Unlock()
	if t == nil {
		return nil, errors.New("Scan a drive first.")
	}
	if path == "" {
		path = t.Root.Path
	}
	n := t.Node(path)
	if n == nil {
		return nil, errors.New("That folder was not part of the scan.")
	}
	items := make([]diskChildDTO, 0, len(n.Children)+len(n.Top))
	for _, c := range n.Children {
		items = append(items, diskChildDTO{Name: c.Name, Path: c.Path, Size: c.Size, Files: c.Files, Denied: c.Denied, Dir: true})
	}
	for _, f := range n.Top {
		items = append(items, diskChildDTO{Name: filepath.Base(f.Path), Path: f.Path, Size: f.Size, Files: 1})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Size > items[j].Size })
	var crumbs []map[string]string
	p := n.Path
	for {
		crumbs = append([]map[string]string{{"name": crumbName(p, t.Root.Path), "path": p}}, crumbs...)
		if strings.EqualFold(p, t.Root.Path) {
			break
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return map[string]any{
		"path": n.Path, "size": n.Size, "files": n.Files, "denied": n.Denied,
		"items": items, "crumbs": crumbs, "root": t.Root.Path,
		"totalFiles": t.Files, "totalBytes": t.Bytes, "totalDenied": t.Denied,
	}, nil
}

func crumbName(p, root string) string {
	if strings.EqualFold(p, root) {
		return root
	}
	return filepath.Base(p)
}

// DiskLargest returns the biggest files found anywhere in the scan.
func (a *api) DiskLargest(n int) ([]diskscan.File, error) {
	disk.mu.Lock()
	t := disk.tree
	disk.mu.Unlock()
	if t == nil {
		return nil, errors.New("Scan a drive first.")
	}
	if n <= 0 || n > len(t.Largest) {
		n = len(t.Largest)
	}
	out := append([]diskscan.File{}, t.Largest[:n]...)
	return out, nil
}

// DiskTypes returns space used by kind of file.
func (a *api) DiskTypes() ([]diskscan.TypeTotal, error) {
	disk.mu.Lock()
	t := disk.tree
	disk.mu.Unlock()
	if t == nil {
		return nil, errors.New("Scan a drive first.")
	}
	out := append([]diskscan.TypeTotal{}, t.Types...)
	return out, nil
}

// DiskDelete moves files from the last scan to the Recycle Bin. Folders and
// anything under Windows or Program Files are refused. The result arrives
// as "diskDeleted".
func (a *api) DiskDelete(paths []string) error {
	disk.mu.Lock()
	t := disk.tree
	disk.mu.Unlock()
	if t == nil {
		return errors.New("Scan a drive first.")
	}
	if len(paths) == 0 {
		return errors.New("Tick at least one file first.")
	}
	type victim struct {
		path string
		size int64
	}
	var ok []victim
	for _, p := range paths {
		if !t.Contains(p) {
			return errors.New("Only files from the scan can be removed.")
		}
		if why := diskProtected(p); why != "" {
			return errors.New(filepath.Base(p) + " " + why)
		}
		fi, err := os.Lstat(p)
		if err != nil {
			continue
		}
		if fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return errors.New("Only files can be removed here, not folders.")
		}
		ok = append(ok, victim{p, fi.Size()})
	}
	if len(ok) == 0 {
		return errors.New("Those files are already gone.")
	}
	go func() {
		var list []string
		var bytes int64
		for _, v := range ok {
			list = append(list, v.path)
		}
		err := sysutil.RecycleFiles(list)
		p := map[string]any{"deleted": 0, "bytes": int64(0)}
		if err != nil {
			p["error"] = err.Error()
		} else {
			for _, v := range ok {
				if _, statErr := os.Lstat(v.path); statErr != nil {
					t.Forget(v.path, v.size)
					bytes += v.size
					p["deleted"] = p["deleted"].(int) + 1
				}
			}
			p["bytes"] = bytes
		}
		a.emit("diskDeleted", p)
	}()
	return nil
}

// ShowDiskPath opens Explorer at a file or folder from the last scan.
func (a *api) ShowDiskPath(path string) error {
	disk.mu.Lock()
	t := disk.tree
	disk.mu.Unlock()
	if t == nil || !t.Contains(path) {
		return errors.New("not part of the last scan")
	}
	if fi, err := os.Stat(path); err == nil && fi.IsDir() {
		return sysutil.ShellOpen(path)
	}
	return sysutil.ShowInExplorer(path)
}

// systemFolders are places Disk Analyzer will never delete from.
func systemFolders() []string {
	var out []string
	for _, v := range []string{"SystemRoot", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432"} {
		if x := os.Getenv(v); x != "" {
			out = append(out, x)
		}
	}
	if pd := os.Getenv("ProgramData"); pd != "" {
		out = append(out, filepath.Join(pd, "Microsoft"))
	}
	if len(out) == 0 {
		out = []string{`C:\Windows`, `C:\Program Files`, `C:\Program Files (x86)`}
	}
	return out
}

var protectedFiles = map[string]bool{
	"pagefile.sys": true, "hiberfil.sys": true, "swapfile.sys": true, "dumpstack.log.tmp": true,
	"bootmgr": true, "ntldr": true, "desktop.ini": true,
}

// diskProtected says why a file must not be removed, or "" when it may be.
func diskProtected(p string) string {
	return diskProtectedIn(p, systemFolders())
}

func diskProtectedIn(p string, folders []string) string {
	norm := func(s string) string { return strings.ToLower(strings.ReplaceAll(filepath.Clean(s), "\\", "/")) }
	lp := norm(p)
	if protectedFiles[lp[strings.LastIndex(lp, "/")+1:]] {
		return "is a Windows system file. Kempt will not remove it."
	}
	for _, f := range folders {
		nf := strings.TrimRight(norm(f), "/")
		if lp == nf || strings.HasPrefix(lp, nf+"/") {
			return "is inside a Windows or program folder. Uninstall the program instead."
		}
	}
	return ""
}
