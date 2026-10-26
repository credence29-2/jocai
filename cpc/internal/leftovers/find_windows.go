//go:build windows

package leftovers

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Bases are the folders programs usually install or save settings into.
func Bases() []string {
	var out []string
	for _, v := range []string{"ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "LOCALAPPDATA", "APPDATA", "ProgramData"} {
		if x := os.Getenv(v); x != "" {
			out = append(out, x)
		}
	}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		out = append(out, filepath.Join(la, "Programs"))
	}
	return out
}

// Find checks the candidate folders and keys for one uninstalled program.
// installed holds the install locations of programs still present.
func Find(p Program, installed []string) Result {
	r := Result{Program: p.Name}
	for _, c := range FolderCandidates(p, Bases()) {
		fi, err := os.Lstat(c)
		if err != nil || !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !SafeFolder(c, installed) {
			continue
		}
		size, files := measure(c)
		r.Folders = append(r.Folders, Item{Path: c, Size: size, Files: files, Why: "Folder named after the program"})
		r.Bytes += size
	}
	for _, k := range KeyCandidates(p) {
		if keyExists(k) {
			why := "Program settings"
			if strings.Contains(k, `\Uninstall\`) {
				why = "Leftover uninstall entry"
			}
			r.Keys = append(r.Keys, Item{Path: k, Why: why})
		}
	}
	r.Folders = Sorted(r.Folders)
	return r
}

func measure(root string) (size int64, files int) {
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 || d.Type()&os.ModeIrregular != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			if fi, e := d.Info(); e == nil {
				size += fi.Size()
				files++
			}
		}
		if files > 500000 {
			return filepath.SkipAll
		}
		return nil
	})
	return
}

func keyExists(full string) bool {
	hive, path, ok := strings.Cut(full, `\`)
	if !ok {
		return false
	}
	var root registry.Key
	switch hive {
	case "HKEY_CURRENT_USER":
		root = registry.CURRENT_USER
	case "HKEY_LOCAL_MACHINE":
		root = registry.LOCAL_MACHINE
	default:
		return false
	}
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return false
	}
	k.Close()
	return true
}
