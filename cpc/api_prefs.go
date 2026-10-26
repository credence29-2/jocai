package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
	"github.com/credence29-2/jocai/cpc/internal/update"
)

// ---- Preferences -----------------------------------------------------------

type prefsDTO struct {
	Theme        string `json:"theme"`
	TechMode     bool   `json:"techMode"`
	Tray         bool   `json:"tray"`
	CheckUpdates bool   `json:"checkUpdates"`
}

func (a *api) prefsLocked() prefsDTO {
	theme := a.cfg.Theme
	if theme == "" {
		theme = "dark"
	}
	return prefsDTO{Theme: theme, TechMode: a.cfg.TechMode, Tray: a.cfg.Tray, CheckUpdates: !a.cfg.NoUpdateCheck}
}

// Prefs returns the look and behaviour preferences.
func (a *api) Prefs() prefsDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prefsLocked()
}

// SetPref changes one preference: theme (dark or light), techMode, tray or
// checkUpdates (true or false).
func (a *api) SetPref(name, value string) (prefsDTO, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	on := value == "true" || value == "1" || value == "on"
	switch name {
	case "theme":
		if value != "dark" && value != "light" {
			return a.prefsLocked(), errors.New("unknown theme")
		}
		a.cfg.Theme = value
	case "techMode":
		a.cfg.TechMode = on
	case "tray":
		a.cfg.Tray = on
		if a.setTray != nil {
			a.setTray(on)
		}
	case "checkUpdates":
		a.cfg.NoUpdateCheck = !on
	default:
		return a.prefsLocked(), errors.New("unknown preference")
	}
	return a.prefsLocked(), settings.Save(a.dataDir, a.cfg)
}

// ---- Export and import -------------------------------------------------------

// ExportSettings asks where to save and writes the portable settings file.
// Returns the path, or "" when cancelled.
func (a *api) ExportSettings() (string, error) {
	home, _ := os.UserHomeDir()
	def := filepath.Join(home, "Desktop", "Kempt-settings.json")
	if _, err := os.Stat(filepath.Dir(def)); err != nil {
		def = "Kempt-settings.json"
	}
	dest := sysutil.SaveFileDialog(a.hwnd, "Export Kempt settings", def, "Kempt settings (*.json)", "*.json", "json")
	if dest == "" {
		return "", nil
	}
	return dest, a.exportTo(dest)
}

func (a *api) exportTo(dest string) error {
	a.mu.Lock()
	cfg := a.cfg
	a.mu.Unlock()
	b, err := settings.Encode(appName, version, cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, b, 0o644)
}

type importDTO struct {
	Path     string `json:"path"`
	Items    int    `json:"items"`
	Excluded int    `json:"excluded"`
	From     string `json:"from"`
}

// ImportSettings asks for a settings file and applies it. The page reloads
// its lists afterwards. Returns an empty path when cancelled.
func (a *api) ImportSettings() (importDTO, error) {
	if a.busy() {
		return importDTO{}, errors.New("Wait for the current clean to finish.")
	}
	src := sysutil.OpenFileDialog(a.hwnd, "Import Kempt settings", "Kempt settings (*.json)", "*.json")
	if src == "" {
		return importDTO{}, nil
	}
	return a.importFrom(src)
}

func (a *api) importFrom(src string) (importDTO, error) {
	b, err := os.ReadFile(src)
	if err != nil {
		return importDTO{}, errors.New("could not read " + filepath.Base(src))
	}
	e, err := settings.Decode(b)
	if err != nil {
		return importDTO{}, err
	}
	a.mu.Lock()
	a.cfg = settings.Merge(a.cfg, e)
	a.eng.Exclude = append([]string(nil), a.cfg.Exclude...)
	if a.setTray != nil {
		a.setTray(a.cfg.Tray)
	}
	err = settings.Save(a.dataDir, a.cfg)
	a.mu.Unlock()
	from := e.Version
	if !e.Exported.IsZero() {
		from = strings.TrimSpace(from + " on " + e.Exported.Local().Format("Jan 2, 2006"))
	}
	return importDTO{Path: src, Items: len(e.Settings.Selected), Excluded: len(e.Settings.Exclude), From: from}, err
}

// ---- Self-update ---------------------------------------------------------------

type updState struct {
	mu      sync.Mutex
	latest  *update.Release
	busy    bool
	checked time.Time
}

var upd updState

type updateDTO struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	Notes     string `json:"notes"`
	Page      string `json:"page"`
	Size      int64  `json:"size"`
	Error     string `json:"error,omitempty"`
	Checked   string `json:"checked"`
}

// UpdateCheck asks GitHub Releases whether a newer Kempt exists.
func (a *api) UpdateCheck() int {
	return a.async(func() (any, error) {
		rel, newer, err := update.Check(context.Background(), version)
		d := updateDTO{Current: version, Checked: time.Now().Format("Jan 2, 3:04 PM")}
		if err != nil {
			d.Error = err.Error()
			return d, nil
		}
		d.Latest, d.Available, d.Notes, d.Page, d.Size = rel.Version, newer, rel.Notes, rel.Page, rel.Size
		upd.mu.Lock()
		upd.latest = &rel
		upd.checked = time.Now()
		upd.mu.Unlock()
		a.mu.Lock()
		a.cfg.LastUpdateCheck = time.Now().Format(time.RFC3339)
		_ = settings.Save(a.dataDir, a.cfg)
		a.mu.Unlock()
		return d, nil
	})
}

// UpdateDue reports whether the daily background check should run now.
func (a *api) UpdateDue() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.NoUpdateCheck {
		return false
	}
	t, err := time.Parse(time.RFC3339, a.cfg.LastUpdateCheck)
	return err != nil || time.Since(t) > 20*time.Hour
}

// UpdateInstall downloads the release found by UpdateCheck, verifies it,
// swaps the exe and restarts Kempt. Progress arrives as "updateProgress"
// and the end as "updateDone".
func (a *api) UpdateInstall() error {
	upd.mu.Lock()
	rel := upd.latest
	if upd.busy {
		upd.mu.Unlock()
		return errors.New("An update is already downloading.")
	}
	if rel == nil || !update.Newer(rel.Version, version) {
		upd.mu.Unlock()
		return errors.New("Check for updates first.")
	}
	upd.busy = true
	upd.mu.Unlock()
	go func() {
		defer func() { upd.mu.Lock(); upd.busy = false; upd.mu.Unlock() }()
		dir := filepath.Join(a.dataDir, "update")
		last := time.Now()
		file, err := update.Download(context.Background(), *rel, dir, func(done, total int64) {
			if time.Since(last) < 150*time.Millisecond && done != total {
				return
			}
			last = time.Now()
			a.emit("updateProgress", map[string]any{"done": done, "total": total})
		})
		if err != nil {
			a.emit("updateDone", map[string]any{"ok": false, "text": err.Error()})
			return
		}
		if err := update.Apply(a.exe, file); err != nil {
			a.emit("updateDone", map[string]any{"ok": false, "text": err.Error(), "page": rel.Page})
			return
		}
		a.emit("updateDone", map[string]any{"ok": true, "text": "Kempt " + rel.Version + " is installed. Restarting..."})
		time.Sleep(1200 * time.Millisecond)
		if err := update.Relaunch(a.exe); err == nil && a.quit != nil {
			a.quit()
		}
	}()
	return nil
}
