// Package clean holds the cleaning engine. The engine is platform neutral so it
// can be unit tested anywhere. The Windows specific list of targets lives in
// targets_windows.go.
package clean

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Rule describes one set of files to clean.
//
// Root is a directory. It may contain %ENV% variables and glob characters
// (for example a browser profile wildcard). Every matching directory is
// processed. Root directories themselves are never removed.
type Rule struct {
	Root string
	// Files limits deletion to file names matching one of these patterns.
	// Empty means every file.
	Files []string
	// Recursive descends into subfolders.
	Recursive bool
	// MinAge skips files modified more recently than this.
	MinAge time.Duration
	// Admin rules are skipped when the program is not elevated.
	Admin bool
	// Shallow allows a root one level below the drive (e.g. C:\FOUND.000)
	// for non-recursive rules that only match specific file patterns.
	Shallow bool
}

// Result is what a scan or clean produced for a single target.
type Result struct {
	Files  int
	Bytes  int64
	Failed int // files that could not be removed (usually locked)
	Err    error
	Note   string
}

// Custom lets a target provide its own scan and clean, for things that are
// not plain files (Recycle Bin, DNS cache).
type Custom interface {
	Scan(ctx context.Context) Result
	Clean(ctx context.Context) Result
}

// Target is one checkbox in the cleaner list.
type Target struct {
	ID      string
	Section string
	// Group is the collapsible heading the item sits under in the UI.
	Group       string
	Name        string
	Description string
	Default     bool
	NeedsAdmin  bool
	// Processes that should be closed for a full clean, e.g. "chrome.exe".
	Processes []string
	Rules     []Rule
	Custom    Custom
}

// Progress is called with the path currently being processed. It may be nil.
type Progress func(path string)

// Engine runs scans and cleans.
type Engine struct {
	// Expand resolves %ENV% variables. Defaults to an os.Getenv based expander.
	Expand func(string) string
	// Now is overridable for tests.
	Now func() time.Time
	// Elevated tells the engine whether Admin rules can run.
	Elevated bool
	// Exclude lists folders that are never cleaned, whatever the rules say.
	Exclude []string
	// remove is overridable for tests.
	remove func(string) error
}

// NewEngine returns an engine with real file system behaviour.
func NewEngine() *Engine {
	return &Engine{Expand: ExpandEnv, Now: time.Now, remove: removeFile}
}

// ExpandEnv expands Windows style %VAR% references. Unknown variables are left
// untouched so the safety check can reject the path.
func ExpandEnv(s string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '%')
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		j := strings.IndexByte(s[i+1:], '%')
		if j < 0 {
			b.WriteString(s)
			return b.String()
		}
		name := s[i+1 : i+1+j]
		b.WriteString(s[:i])
		if v, ok := os.LookupEnv(name); ok && v != "" {
			b.WriteString(v)
		} else {
			b.WriteString(s[i : i+j+2])
		}
		s = s[i+j+2:]
	}
}

// ErrUnsafeRoot is returned for roots that fail the safety check.
var ErrUnsafeRoot = errors.New("refusing to clean an unsafe location")

// SafeRoot reports whether a resolved root is acceptable to clean inside. It
// guards against unresolved variables or a root that collapses to a drive or
// a top level folder, which would be catastrophic.
func SafeRoot(p string) bool { return safeRootDepth(p, 2) }

// namedFilesOnly reports whether a rule only targets exact file names in a
// single folder (no wildcards, no recursion). Such rules may live one level
// below the drive, e.g. C:\Windows\MEMORY.DMP.
func namedFilesOnly(r Rule) bool {
	if r.Recursive || len(r.Files) == 0 {
		return false
	}
	for _, f := range r.Files {
		if strings.ContainsAny(f, "*?[") {
			return false
		}
	}
	return true
}

func matchesEverything(pats []string) bool {
	for _, p := range pats {
		if p == "*" || p == "*.*" {
			return true
		}
	}
	return false
}

func safeRootDepth(p string, minDepth int) bool {
	if p == "" || strings.Contains(p, "%") {
		return false
	}
	if !filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(p)
	vol := filepath.VolumeName(clean)
	rest := strings.Trim(clean[len(vol):], `\/`)
	if rest == "" {
		return false
	}
	parts := strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' })
	// Normally need two levels below the volume, e.g. C:\Windows\Temp.
	return len(parts) >= minDepth
}

func (e *Engine) roots(r Rule) ([]string, error) {
	minDepth := 2
	if namedFilesOnly(r) || (r.Shallow && !r.Recursive && len(r.Files) > 0 && !matchesEverything(r.Files)) {
		minDepth = 1
	}
	p := e.Expand(r.Root)
	if !safeRootDepth(p, minDepth) {
		return nil, fmt.Errorf("%w: %s", ErrUnsafeRoot, p)
	}
	p = filepath.Clean(p)
	if !strings.ContainsAny(p, "*?[") {
		return []string{p}, nil
	}
	matches, err := filepath.Glob(p)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range matches {
		if !safeRootDepth(m, minDepth) {
			continue
		}
		fi, err := os.Lstat(m)
		if err != nil || !fi.IsDir() || fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func matchAny(name string, pats []string) bool {
	if len(pats) == 0 {
		return true
	}
	lname := strings.ToLower(name)
	for _, p := range pats {
		if ok, _ := filepath.Match(strings.ToLower(p), lname); ok {
			return true
		}
	}
	return false
}

// visit walks every deletable file for a rule and calls fn for it.
func (e *Engine) visit(ctx context.Context, r Rule, done *[]os.FileInfo, prog Progress, fn func(path string, size int64)) (dirs []string, err error) {
	if r.Admin && !e.Elevated {
		return nil, nil
	}
	roots, err := e.roots(r)
	if err != nil {
		return nil, err
	}
	now := e.Now()
	for _, root := range roots {
		fi, err := os.Lstat(root)
		if err != nil || !fi.IsDir() || fi.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			continue
		}
		// The same folder can be reached two ways (for example %TEMP% as an
		// 8.3 short path). Process each physical folder once per target.
		if sameAsAny(fi, *done) {
			continue
		}
		*done = append(*done, fi)
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				// Unreadable folder: skip it, keep going.
				if d != nil && d.IsDir() && path != root {
					return fs.SkipDir
				}
				return nil
			}
			// Never follow or touch links, junctions or other reparse points.
			if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if e.excluded(path) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if path == root {
					return nil
				}
				if !r.Recursive {
					return fs.SkipDir
				}
				// Only offer to remove folders that are old enough themselves,
				// so a folder an installer just created is left alone.
				if r.MinAge == 0 {
					dirs = append(dirs, path)
				} else if info, err := d.Info(); err == nil && now.Sub(info.ModTime()) >= r.MinAge {
					dirs = append(dirs, path)
				}
				return nil
			}
			if !d.Type().IsRegular() || !matchAny(d.Name(), r.Files) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			if r.MinAge > 0 && now.Sub(info.ModTime()) < r.MinAge {
				return nil
			}
			if prog != nil {
				prog(path)
			}
			fn(path, info.Size())
			return nil
		})
		if walkErr != nil && ctx.Err() != nil {
			return dirs, ctx.Err()
		}
	}
	return dirs, nil
}

// Scan measures what Clean would remove without deleting anything.
func (e *Engine) Scan(ctx context.Context, t *Target, prog Progress) Result {
	if t.Custom != nil {
		return t.Custom.Scan(ctx)
	}
	var res Result
	seen := map[string]bool{}
	var done []os.FileInfo
	for _, r := range t.Rules {
		_, err := e.visit(ctx, r, &done, prog, func(p string, size int64) {
			k := strings.ToLower(p)
			if seen[k] {
				return
			}
			seen[k] = true
			res.Files++
			res.Bytes += size
		})
		if err != nil && !errors.Is(err, ErrUnsafeRoot) {
			res.Err = err
			return res
		}
	}
	return res
}

// Clean deletes the files for a target. Files that are in use are skipped and
// counted in Result.Failed.
func (e *Engine) Clean(ctx context.Context, t *Target, prog Progress, logf func(string)) Result {
	if t.Custom != nil {
		return t.Custom.Clean(ctx)
	}
	var res Result
	seen := map[string]bool{}
	var done []os.FileInfo
	for _, r := range t.Rules {
		dirs, err := e.visit(ctx, r, &done, prog, func(p string, size int64) {
			k := strings.ToLower(p)
			if seen[k] {
				return
			}
			seen[k] = true
			if err := e.remove(p); err != nil {
				res.Failed++
				return
			}
			res.Files++
			res.Bytes += size
			if logf != nil {
				logf(p)
			}
		})
		// Remove now-empty subfolders, deepest first. Roots are kept.
		sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
		for _, d := range dirs {
			_ = os.Remove(d) // only succeeds when empty
		}
		if err != nil && !errors.Is(err, ErrUnsafeRoot) {
			res.Err = err
			return res
		}
	}
	return res
}

// excluded reports whether p is one of the excluded folders or inside one.
func (e *Engine) excluded(p string) bool {
	lp := strings.ToLower(filepath.Clean(p))
	for _, x := range e.Exclude {
		lx := strings.ToLower(filepath.Clean(x))
		if lx == "" || lx == "." {
			continue
		}
		if lp == lx || strings.HasPrefix(lp, strings.TrimRight(lx, `\/`)+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func sameAsAny(fi os.FileInfo, list []os.FileInfo) bool {
	for _, o := range list {
		if os.SameFile(fi, o) {
			return true
		}
	}
	return false
}

func removeFile(p string) error {
	err := os.Remove(p)
	if err == nil || errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	// Read-only files need their attribute cleared first.
	if chErr := os.Chmod(p, 0o666); chErr == nil {
		if err2 := os.Remove(p); err2 == nil {
			return nil
		}
	}
	return err
}

// FormatBytes renders a byte count for people.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Files renders a file count with the right plural.
func Files(n int) string {
	if n == 1 {
		return "1 file"
	}
	return fmt.Sprintf("%d files", n)
}
