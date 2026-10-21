// Package dupes finds duplicate files.
//
// Files are grouped by size first, then by a hash of the first 64 KB, then
// by a full SHA-256 of the content, so only real byte-for-byte duplicates
// are reported. Links and other reparse points are never followed.
package dupes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// File is one file in a duplicate group.
type File struct {
	ID       int       `json:"id"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"-"`
	ModText  string    `json:"modified"`
}

// Group is a set of identical files.
type Group struct {
	ID    int    `json:"id"`
	Size  int64  `json:"size"`
	Files []File `json:"files"`
}

// Options control a search.
type Options struct {
	MinSize int64
	// SkipDirs are folder names (case-insensitive) never entered, e.g. AppData.
	SkipDirs []string
	// SkipHidden skips files and folders whose name starts with a dot.
	SkipHidden bool
}

// DefaultSkip are folders that hold program data, never user documents.
var DefaultSkip = []string{"AppData", "$Recycle.Bin", "System Volume Information", "Windows", "Program Files", "Program Files (x86)", "ProgramData", "node_modules", ".git"}

type cand struct {
	path string
	size int64
	mod  time.Time
}

// Find searches roots for duplicates. progress is called now and then with
// the number of files looked at and the current path.
func Find(ctx context.Context, roots []string, opt Options, progress func(n int, path string)) ([]Group, error) {
	skip := map[string]bool{}
	for _, s := range opt.SkipDirs {
		skip[strings.ToLower(s)] = true
	}
	bySize := map[int64][]cand{}
	seen := map[string]bool{}
	n := 0
	last := time.Now()
	for _, root := range roots {
		root = filepath.Clean(root)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				if d != nil && d.IsDir() && p != root {
					return fs.SkipDir
				}
				return nil
			}
			if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if p != root && (skip[strings.ToLower(name)] || (opt.SkipHidden && strings.HasPrefix(name, "."))) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() || (opt.SkipHidden && strings.HasPrefix(name, ".")) {
				return nil
			}
			key := strings.ToLower(p)
			if seen[key] {
				return nil
			}
			seen[key] = true
			info, err := d.Info()
			if err != nil || info.Size() < opt.MinSize || info.Size() == 0 {
				return nil
			}
			n++
			if progress != nil && time.Since(last) > 150*time.Millisecond {
				last = time.Now()
				progress(n, p)
			}
			bySize[info.Size()] = append(bySize[info.Size()], cand{p, info.Size(), info.ModTime()})
			return nil
		})
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	var groups []Group
	sizes := make([]int64, 0, len(bySize))
	for s, list := range bySize {
		if len(list) > 1 {
			sizes = append(sizes, s)
		}
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] > sizes[j] })
	for _, size := range sizes {
		// Narrow by the first 64 KB, then by full content.
		for _, quick := range bucket(bySize[size], func(c cand) string { return hashFile(c.path, 64*1024) }) {
			if len(quick) < 2 {
				continue
			}
			full := quick
			if size > 64*1024 {
				full = nil
				for _, b := range bucket(quick, func(c cand) string { return hashFile(c.path, -1) }) {
					if len(b) > 1 {
						groups = append(groups, makeGroup(size, b))
					}
				}
				continue
			}
			groups = append(groups, makeGroup(size, full))
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	id := 0
	for gi := range groups {
		groups[gi].ID = gi
		for fi := range groups[gi].Files {
			groups[gi].Files[fi].ID = id
			id++
		}
	}
	return groups, nil
}

func bucket(list []cand, key func(cand) string) [][]cand {
	m := map[string][]cand{}
	var order []string
	for _, c := range list {
		k := key(c)
		if k == "" {
			continue // unreadable, leave it out
		}
		if _, ok := m[k]; !ok {
			order = append(order, k)
		}
		m[k] = append(m[k], c)
	}
	out := make([][]cand, 0, len(order))
	for _, k := range order {
		out = append(out, m[k])
	}
	return out
}

func makeGroup(size int64, list []cand) Group {
	g := Group{Size: size}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.Before(list[j].mod) })
	for _, c := range list {
		g.Files = append(g.Files, File{Path: c.path, Size: c.size, Modified: c.mod, ModText: c.mod.Format("Jan 2, 2006")})
	}
	return g
}

// hashFile hashes the first limit bytes, or the whole file when limit < 0.
func hashFile(p string, limit int64) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	var r io.Reader = f
	if limit >= 0 {
		r = io.LimitReader(f, limit)
	}
	if _, err := io.Copy(h, r); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Wasted is the space that removing all but one copy in each group frees.
func Wasted(groups []Group) int64 {
	var w int64
	for _, g := range groups {
		w += g.Size * int64(len(g.Files)-1)
	}
	return w
}

// CheckSelection makes sure at least one copy in every group survives.
// It returns the paths to delete.
func CheckSelection(groups []Group, ids []int) ([]string, error) {
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var paths []string
	for _, g := range groups {
		kept := 0
		for _, f := range g.Files {
			if want[f.ID] {
				paths = append(paths, f.Path)
			} else {
				kept++
			}
		}
		if kept == 0 {
			return nil, &AllSelectedError{Path: g.Files[0].Path}
		}
	}
	return paths, nil
}

// AllSelectedError means every copy in a group was selected.
type AllSelectedError struct{ Path string }

func (e *AllSelectedError) Error() string {
	return "Every copy of " + filepath.Base(e.Path) + " is selected. Keep at least one."
}
