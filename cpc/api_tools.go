package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/credence29-2/jocai/cpc/internal/clean"
	"github.com/credence29-2/jocai/cpc/internal/dupes"
	"github.com/credence29-2/jocai/cpc/internal/systools"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// tools holds state for the Duplicate Finder and Software Updater.
type tools struct {
	mu         sync.Mutex
	asyncID    int
	dupeGroups []dupes.Group
	dupeCancel context.CancelFunc
	updBusy    bool
	drvBusy    bool
}

// async runs slow work off the UI thread. The page gets an "async" event
// with the returned token and the result.
func (a *api) async(work func() (any, error)) int {
	a.t.mu.Lock()
	a.t.asyncID++
	id := a.t.asyncID
	a.t.mu.Unlock()
	go func() {
		res, err := work()
		p := map[string]any{"token": id, "result": res}
		if err != nil {
			p["error"] = err.Error()
		}
		a.emit("async", p)
	}()
	return id
}

// ---- Startup Manager: scheduled tasks and services ------------------------

func (a *api) Tasks() int { return a.async(func() (any, error) { return systools.Tasks() }) }

func (a *api) SetTask(fullPath string, enabled bool) int {
	return a.async(func() (any, error) {
		if !a.elevated {
			return nil, errors.New("Restart CPC as administrator to change scheduled tasks.")
		}
		if err := systools.SetTask(fullPath, enabled); err != nil {
			return nil, err
		}
		return systools.Tasks()
	})
}

func (a *api) Services() int { return a.async(func() (any, error) { return systools.Services() }) }

func (a *api) SetService(name, mode string) int {
	return a.async(func() (any, error) {
		if !a.elevated {
			return nil, errors.New("Restart CPC as administrator to change services.")
		}
		if err := systools.SetService(name, mode); err != nil {
			return nil, err
		}
		return systools.Services()
	})
}

// ---- Driver Updater ----------------------------------------------------------

type driversDTO struct {
	Updates   []systools.DriverUpdate `json:"updates"`
	Ignored   []systools.DriverUpdate `json:"ignored"`
	Installed []systools.Driver       `json:"installed"`
	Devices   []systools.Device       `json:"devices"`
	Links     []systools.MakerLink    `json:"links"`
	WUError   string                  `json:"wuError,omitempty"`
}

// Drivers checks Windows Update for driver updates and lists installed
// drivers and problem devices. Searching Windows Update can take a minute.
func (a *api) Drivers() int {
	return a.async(func() (any, error) {
		var res driversDTO
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			up, ign, err := systools.DriverUpdates()
			res.Updates, res.Ignored = up, ign
			if err != nil {
				res.WUError = err.Error()
			}
		}()
		inst, _ := systools.InstalledDrivers()
		dev, _ := systools.ProblemDevices()
		maker := systools.SystemMaker()
		wg.Wait()
		systools.MatchUpdates(res.Updates, inst)
		systools.MatchUpdates(res.Ignored, inst)
		res.Installed, res.Devices = inst, dev
		res.Links = systools.MakerLinks(maker, inst)
		if res.Updates == nil {
			res.Updates = []systools.DriverUpdate{}
		}
		if res.Ignored == nil {
			res.Ignored = []systools.DriverUpdate{}
		}
		if res.Installed == nil {
			res.Installed = []systools.Driver{}
		}
		if res.Devices == nil {
			res.Devices = []systools.Device{}
		}
		if res.Links == nil {
			res.Links = []systools.MakerLink{}
		}
		return res, nil
	})
}

// InstallDrivers installs the chosen driver updates one at a time,
// optionally after making a restore point. Progress arrives as
// "drvProgress" and the end as "drvFinished".
func (a *api) InstallDrivers(ids []string, restorePoint bool) error {
	if !a.elevated {
		return errors.New("Restart CPC as administrator to install drivers.")
	}
	if len(ids) == 0 {
		return errors.New("Tick at least one driver first.")
	}
	for _, id := range ids {
		if !systools.ValidUpdateID(id) {
			return errors.New("unexpected update id")
		}
	}
	a.t.mu.Lock()
	if a.t.drvBusy {
		a.t.mu.Unlock()
		return errors.New("Drivers are already being installed.")
	}
	a.t.drvBusy = true
	a.t.mu.Unlock()
	go func() {
		defer func() { a.t.mu.Lock(); a.t.drvBusy = false; a.t.mu.Unlock() }()
		fin := map[string]any{"installed": 0, "failed": 0, "reboot": false}
		if restorePoint {
			a.emit("drvProgress", map[string]any{"stage": "restore"})
			note, err := systools.RestorePoint("Before Credence PC Cleaner driver update")
			if err != nil {
				fin["error"] = err.Error() + ". No drivers were installed. Untick the restore point option to install anyway."
				a.emit("drvFinished", fin)
				return
			}
			fin["restore"] = note
		}
		ok, failed, reboot := 0, 0, false
		for i, id := range ids {
			a.emit("drvProgress", map[string]any{"stage": "installing", "id": id, "index": i, "total": len(ids)})
			r, err := systools.InstallDriverUpdate(id)
			p := map[string]any{"stage": "done", "id": id, "ok": r.OK, "text": r.Text, "reboot": r.Reboot}
			if err != nil {
				p["ok"], p["text"] = false, err.Error()
			}
			if p["ok"] == true {
				ok++
			} else {
				failed++
			}
			reboot = reboot || r.Reboot
			a.emit("drvProgress", p)
		}
		fin["installed"], fin["failed"], fin["reboot"] = ok, failed, reboot
		a.emit("drvFinished", fin)
	}()
	return nil
}

// IgnoreDriver hides or unhides a driver update in Windows Update.
func (a *api) IgnoreDriver(id string, ignore bool) int {
	return a.async(func() (any, error) {
		if !a.elevated {
			return nil, errors.New("Restart CPC as administrator to change ignored drivers.")
		}
		return nil, systools.SetDriverUpdateHidden(id, ignore)
	})
}

// OpenMakerSite opens a known manufacturer driver page.
func (a *api) OpenMakerSite(key string) error {
	u, ok := systools.MakerURL(key)
	if !ok {
		return errors.New("unknown maker")
	}
	return sysutil.ShellOpen(u)
}

var settingsPages = map[string]string{
	"devicemanager":   "devmgmt.msc",
	"windowsupdate":   "ms-settings:windowsupdate",
	"optionalupdates": "ms-settings:windowsupdate-optionalupdates",
	"storage":         "ms-settings:storagesense",
	"apps":            "ms-settings:appsfeatures",
}

// OpenSettings opens one of a fixed set of Windows settings pages.
func (a *api) OpenSettings(which string) error {
	target, ok := settingsPages[which]
	if !ok {
		return errors.New("unknown settings page")
	}
	return sysutil.ShellOpen(target)
}

// ---- Software Updater --------------------------------------------------------

type updatesDTO struct {
	Available bool              `json:"available"`
	Updates   []systools.Update `json:"updates"`
}

func (a *api) Updates() int {
	return a.async(func() (any, error) {
		if !systools.WingetAvailable() {
			return updatesDTO{Available: false, Updates: []systools.Update{}}, nil
		}
		u, err := systools.Updates()
		if u == nil {
			u = []systools.Update{}
		}
		return updatesDTO{Available: true, Updates: u}, err
	})
}

// UpdateApps updates apps one at a time, reporting each with "updProgress"
// and finishing with "updFinished".
func (a *api) UpdateApps(ids []string) error {
	if len(ids) == 0 {
		return errors.New("Tick at least one app first.")
	}
	a.t.mu.Lock()
	if a.t.updBusy {
		a.t.mu.Unlock()
		return errors.New("Updates are already running.")
	}
	a.t.updBusy = true
	a.t.mu.Unlock()
	go func() {
		ok, failed := 0, 0
		for i, id := range ids {
			a.emit("updProgress", map[string]any{"id": id, "index": i, "total": len(ids), "status": "updating"})
			if err := systools.UpdateApp(id); err != nil {
				failed++
				a.emit("updProgress", map[string]any{"id": id, "index": i, "total": len(ids), "status": "failed", "error": err.Error()})
			} else {
				ok++
				a.emit("updProgress", map[string]any{"id": id, "index": i, "total": len(ids), "status": "updated"})
			}
		}
		a.t.mu.Lock()
		a.t.updBusy = false
		a.t.mu.Unlock()
		a.emit("updFinished", map[string]any{"updated": ok, "failed": failed})
	}()
	return nil
}

// ---- Duplicate Finder ------------------------------------------------------

// DupeDefaults suggests the usual personal folders that exist on this PC.
func (a *api) DupeDefaults() []string {
	home, _ := os.UserHomeDir()
	var out []string
	for _, d := range []string{"Desktop", "Documents", "Downloads", "Pictures", "Videos", "Music"} {
		p := filepath.Join(home, d)
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			out = append(out, p)
		}
	}
	if od := os.Getenv("OneDrive"); od != "" {
		if fi, err := os.Stat(od); err == nil && fi.IsDir() {
			out = append(out, od)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// PickFolder shows the folder picker. Returns "" when cancelled.
func (a *api) PickFolder() string {
	return sysutil.PickFolder(a.hwnd, "Choose a folder to search for duplicates")
}

type dupeResultDTO struct {
	Groups []dupes.Group `json:"groups"`
	Wasted int64         `json:"wasted"`
	Error  string        `json:"error,omitempty"`
}

// DupeScan searches folders for duplicates. Progress arrives as
// "dupeProgress" and the result as "dupeFinished".
func (a *api) DupeScan(folders []string, minKB int) error {
	var roots []string
	for _, f := range folders {
		if fi, err := os.Stat(f); err == nil && fi.IsDir() && clean.SafeRoot(f) {
			roots = append(roots, f)
		}
	}
	if len(roots) == 0 {
		return errors.New("Add at least one folder to search.")
	}
	a.t.mu.Lock()
	if a.t.dupeCancel != nil {
		a.t.mu.Unlock()
		return errors.New("A search is already running.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.t.dupeCancel = cancel
	a.t.mu.Unlock()
	go func() {
		defer cancel()
		groups, err := dupes.Find(ctx, roots, dupes.Options{MinSize: int64(minKB) * 1024, SkipDirs: dupes.DefaultSkip, SkipHidden: true},
			func(n int, p string) { a.emit("dupeProgress", map[string]any{"files": n, "path": p}) })
		res := dupeResultDTO{Groups: groups, Wasted: dupes.Wasted(groups)}
		if res.Groups == nil {
			res.Groups = []dupes.Group{}
		}
		if err != nil {
			res.Error = "Search stopped."
		}
		a.t.mu.Lock()
		a.t.dupeGroups = groups
		a.t.dupeCancel = nil
		a.t.mu.Unlock()
		a.emit("dupeFinished", res)
	}()
	return nil
}

func (a *api) DupeCancel() {
	a.t.mu.Lock()
	defer a.t.mu.Unlock()
	if a.t.dupeCancel != nil {
		a.t.dupeCancel()
	}
}

// DupeDelete moves the chosen copies to the Recycle Bin. At least one copy
// of every file is always kept. The result arrives as "dupeDeleted".
func (a *api) DupeDelete(ids []int) error {
	a.t.mu.Lock()
	groups := a.t.dupeGroups
	a.t.mu.Unlock()
	paths, err := dupes.CheckSelection(groups, ids)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("Tick at least one file first.")
	}
	var bytes int64
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, g := range groups {
		for _, f := range g.Files {
			if want[f.ID] {
				bytes += f.Size
			}
		}
	}
	go func() {
		err := sysutil.RecycleFiles(paths)
		p := map[string]any{"deleted": len(paths), "bytes": bytes}
		if err != nil {
			p["error"] = err.Error()
			p["deleted"] = 0
			p["bytes"] = 0
		}
		a.emit("dupeDeleted", p)
	}()
	return nil
}

// ShowFile opens Explorer at a file from the last duplicate search.
func (a *api) ShowFile(path string) error {
	a.t.mu.Lock()
	defer a.t.mu.Unlock()
	for _, g := range a.t.dupeGroups {
		for _, f := range g.Files {
			if strings.EqualFold(f.Path, path) {
				return sysutil.ShowInExplorer(path)
			}
		}
	}
	return errors.New("not a file from the last search")
}
