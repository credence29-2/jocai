// Package diskscan measures where disk space goes: folder sizes, the
// largest files, and totals by file type.
package diskscan

import (
	"container/heap"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// File is one large file.
type File struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
	Type     string `json:"type"`
}

// Node is a folder in the scanned tree.
type Node struct {
	Name     string
	Path     string
	Size     int64
	Files    int
	Children []*Node
	Top      []File // largest files directly inside this folder
	Denied   bool
}

// TypeTotal is the space used by one kind of file.
type TypeTotal struct {
	Type  string `json:"type"`
	Size  int64  `json:"size"`
	Count int    `json:"count"`
}

// Tree is a finished scan.
type Tree struct {
	Root    *Node
	Largest []File
	Types   []TypeTotal
	Files   int
	Bytes   int64
	Denied  int
	Took    time.Duration
	byPath  map[string]*Node
	mu      sync.Mutex
}

const topPerFolder = 25
const largestKeep = 300

var typeByExt = map[string]string{}

func init() {
	add := func(t string, exts ...string) {
		for _, e := range exts {
			typeByExt[e] = t
		}
	}
	add("Videos", "mp4", "mkv", "avi", "mov", "wmv", "m4v", "webm", "flv", "mpg", "mpeg", "ts", "m2ts")
	add("Pictures", "jpg", "jpeg", "png", "gif", "heic", "heif", "bmp", "tif", "tiff", "webp", "raw", "cr2", "nef", "arw", "dng", "psd", "svg")
	add("Music", "mp3", "flac", "wav", "m4a", "aac", "ogg", "wma", "aiff", "opus")
	add("Documents", "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "rtf", "odt", "ods", "csv", "md", "one", "pub", "vsd", "vsdx")
	add("Archives", "zip", "rar", "7z", "tar", "gz", "bz2", "xz", "cab", "iso", "img", "wim", "esd")
	add("Installers", "exe", "msi", "msix", "msu", "appx", "appxbundle", "apk")
	add("Programs and libraries", "dll", "sys", "ocx", "drv", "com", "bin", "dat", "pak", "pdb")
	add("Virtual machines", "vhd", "vhdx", "vmdk", "vdi", "ova", "ovf")
	add("Backups and disk images", "bak", "bkf", "old", "tib", "ghost")
	add("Databases", "db", "sqlite", "mdb", "accdb", "mdf", "ldf", "ndf")
	add("Logs and temp", "log", "tmp", "temp", "etl", "dmp", "chk", "evtx")
	add("Code", "go", "js", "ts", "py", "cs", "java", "cpp", "c", "h", "rs", "json", "xml", "yml", "yaml", "html", "css")
}

// TypeOf names the kind of file from its extension.
func TypeOf(name string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
	if t, ok := typeByExt[ext]; ok {
		return t
	}
	return "Other"
}

type fileHeap []File

func (h fileHeap) Len() int           { return len(h) }
func (h fileHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
func (h fileHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(x any)        { *h = append(*h, x.(File)) }
func (h *fileHeap) Pop() any          { o := *h; n := len(o); x := o[n-1]; *h = o[:n-1]; return x }
func keepTop(h *fileHeap, f File, n int) {
	if h.Len() < n {
		heap.Push(h, f)
	} else if f.Size > (*h)[0].Size {
		(*h)[0] = f
		heap.Fix(h, 0)
	}
}

// Progress is called every so often with the running totals.
type Progress func(files int, bytes int64, path string)

// Scan measures root. Links and reparse points are never followed.
func Scan(ctx context.Context, root string, prog Progress) (*Tree, error) {
	start := time.Now()
	root = filepath.Clean(root)
	fi, err := os.Lstat(root)
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() {
		return nil, os.ErrInvalid
	}
	t := &Tree{byPath: map[string]*Node{}}
	t.Root = &Node{Name: root, Path: root}
	t.byPath[strings.ToLower(root)] = t.Root
	largest := &fileHeap{}
	types := map[string]*TypeTotal{}
	var scan func(n *Node) error
	last := time.Now()
	scan = func(n *Node) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entries, err := os.ReadDir(n.Path)
		if err != nil {
			n.Denied = true
			t.Denied++
			return nil
		}
		top := &fileHeap{}
		for _, e := range entries {
			p := filepath.Join(n.Path, e.Name())
			typ := e.Type()
			if typ&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				continue
			}
			if e.IsDir() {
				c := &Node{Name: e.Name(), Path: p}
				if err := scan(c); err != nil {
					return err
				}
				n.Size += c.Size
				n.Files += c.Files
				n.Children = append(n.Children, c)
				t.byPath[strings.ToLower(p)] = c
				continue
			}
			if !typ.IsRegular() {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			sz := info.Size()
			n.Size += sz
			n.Files++
			t.Files++
			t.Bytes += sz
			ft := TypeOf(e.Name())
			tt := types[ft]
			if tt == nil {
				tt = &TypeTotal{Type: ft}
				types[ft] = tt
			}
			tt.Size += sz
			tt.Count++
			f := File{Path: p, Size: sz, Modified: info.ModTime().Format("Jan 2, 2006"), Type: ft}
			keepTop(top, f, topPerFolder)
			keepTop(largest, f, largestKeep)
			if prog != nil && time.Since(last) > 150*time.Millisecond {
				last = time.Now()
				prog(t.Files, t.Bytes, p)
			}
		}
		n.Top = sortedDesc(*top)
		sort.Slice(n.Children, func(i, j int) bool { return n.Children[i].Size > n.Children[j].Size })
		return nil
	}
	if err := scan(t.Root); err != nil {
		return nil, err
	}
	t.Largest = sortedDesc(*largest)
	for _, tt := range types {
		t.Types = append(t.Types, *tt)
	}
	sort.Slice(t.Types, func(i, j int) bool { return t.Types[i].Size > t.Types[j].Size })
	t.Took = time.Since(start)
	return t, nil
}

func sortedDesc(h fileHeap) []File {
	out := append([]File(nil), h...)
	sort.Slice(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

// Node finds a scanned folder by path.
func (t *Tree) Node(path string) *Node {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.byPath[strings.ToLower(filepath.Clean(path))]
}

// Contains reports whether path is inside the scanned root.
func (t *Tree) Contains(path string) bool {
	p := strings.ToLower(filepath.Clean(path))
	r := strings.ToLower(t.Root.Path)
	return p == r || strings.HasPrefix(p, strings.TrimRight(r, `\/`)+string(filepath.Separator))
}

// Forget removes deleted files from the totals so the view updates without
// a rescan. Sizes along the path to the root are reduced.
func (t *Tree) Forget(path string, size int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	dir := filepath.Dir(path)
	for {
		n := t.byPath[strings.ToLower(dir)]
		if n == nil {
			break
		}
		n.Size -= size
		n.Files--
		if n == t.Root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if n := t.byPath[strings.ToLower(filepath.Dir(path))]; n != nil {
		n.Top = removeFile(n.Top, path)
	}
	t.Largest = removeFile(t.Largest, path)
	t.Files--
	t.Bytes -= size
}

func removeFile(list []File, path string) []File {
	out := list[:0]
	for _, f := range list {
		if !strings.EqualFold(f.Path, path) {
			out = append(out, f)
		}
	}
	return out
}
