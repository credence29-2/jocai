package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/credence29-2/jocai/cpc/internal/leftovers"
	"github.com/credence29-2/jocai/cpc/internal/programs"
	"github.com/credence29-2/jocai/cpc/internal/regclean"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// ---- Uninstall leftovers ----------------------------------------------------

// rememberUninstall snapshots a program when its uninstaller starts so the
// leftovers can be found once it is gone.
func (a *api) rememberUninstall(p programs.Program) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.pendingUninstalls == nil {
		a.pendingUninstalls = map[string]leftovers.Program{}
	}
	a.pendingUninstalls[strings.ToLower(p.Name+"\x00"+p.Key)] = leftovers.Program{Name: p.Name, Publisher: p.Publisher, Location: p.Location, Key: p.Key}
}

// Leftovers looks for what the uninstallers started from Kempt left behind,
// for every program that has since disappeared from the installed list.
func (a *api) Leftovers() int {
	return a.async(func() (any, error) {
		a.mu.Lock()
		pending := make(map[string]leftovers.Program, len(a.pendingUninstalls))
		for k, v := range a.pendingUninstalls {
			pending[k] = v
		}
		a.mu.Unlock()
		out := []leftovers.Result{}
		if len(pending) == 0 {
			return out, nil
		}
		list := programs.List()
		present := map[string]bool{}
		var installed []string
		for _, p := range list {
			present[strings.ToLower(p.Name+"\x00"+p.Key)] = true
			if p.Location != "" {
				installed = append(installed, p.Location)
			}
		}
		found := map[string]leftovers.Result{}
		for k, p := range pending {
			if present[k] {
				continue
			}
			r := leftovers.Find(p, installed)
			found[k] = r
			if len(r.Folders)+len(r.Keys) > 0 {
				out = append(out, r)
			}
		}
		a.mu.Lock()
		for k, r := range found {
			delete(a.pendingUninstalls, k)
			if len(r.Folders)+len(r.Keys) > 0 {
				if a.leftoverResults == nil {
					a.leftoverResults = map[string]leftovers.Result{}
				}
				a.leftoverResults[strings.ToLower(r.Program)] = r
			}
		}
		a.mu.Unlock()
		return out, nil
	})
}

// RemoveLeftovers moves the chosen folders to the Recycle Bin and deletes
// the chosen registry keys after backing them up. Only items from the last
// Leftovers result are accepted. The result arrives as "leftoversDone".
func (a *api) RemoveLeftovers(program string, folders []string, keys []string) error {
	a.mu.Lock()
	r, ok := a.leftoverResults[strings.ToLower(program)]
	a.mu.Unlock()
	if !ok {
		return errors.New("Those leftovers are no longer listed. Refresh and try again.")
	}
	allowed := func(items []leftovers.Item, p string) bool {
		for _, it := range items {
			if strings.EqualFold(it.Path, p) {
				return true
			}
		}
		return false
	}
	var dirs []string
	for _, f := range folders {
		if !allowed(r.Folders, f) {
			return errors.New("Only the listed folders can be removed.")
		}
		if fi, err := os.Lstat(f); err == nil && fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
			dirs = append(dirs, f)
		}
	}
	var issues []regclean.Issue
	for i, k := range keys {
		if !allowed(r.Keys, k) {
			return errors.New("Only the listed registry keys can be removed.")
		}
		issues = append(issues, regclean.Issue{ID: i, Category: "leftovers", Key: k, WholeKey: true, Problem: "left behind by " + program})
	}
	if len(dirs)+len(issues) == 0 {
		return errors.New("Tick at least one item first.")
	}
	go func() {
		p := map[string]any{"program": program, "folders": 0, "keys": 0, "bytes": int64(0)}
		var errs []string
		if len(dirs) > 0 {
			if err := sysutil.RecycleFiles(dirs); err != nil {
				errs = append(errs, err.Error())
			}
			n, bytes := 0, int64(0)
			for _, d := range dirs {
				if _, err := os.Lstat(d); err != nil {
					n++
					for _, it := range r.Folders {
						if strings.EqualFold(it.Path, d) {
							bytes += it.Size
						}
					}
				}
			}
			p["folders"], p["bytes"] = n, bytes
		}
		if len(issues) > 0 {
			fixed, failed, backup, err := regclean.Fix(issues, a.backupRoot())
			if err != nil {
				errs = append(errs, err.Error())
			} else if failed > 0 {
				errs = append(errs, "Some registry keys could not be removed. The rest are backed up in "+filepath.Base(backup)+".")
			}
			p["keys"], p["backup"] = fixed, filepath.Base(backup)
		}
		if len(errs) > 0 {
			p["error"] = strings.Join(errs, " ")
		}
		a.mu.Lock()
		delete(a.leftoverResults, strings.ToLower(program))
		a.mu.Unlock()
		a.emit("leftoversDone", p)
	}()
	return nil
}

// DismissLeftovers forgets a leftovers result without removing anything.
func (a *api) DismissLeftovers(program string) {
	a.mu.Lock()
	delete(a.leftoverResults, strings.ToLower(program))
	a.mu.Unlock()
}
