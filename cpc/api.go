package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/clean"
	"github.com/credence29-2/jocai/cpc/internal/programs"
	"github.com/credence29-2/jocai/cpc/internal/regclean"
	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/startup"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

const appName = "Clear PC Cleaner"

// version is set at build time with -ldflags "-X main.version=...".
var version = "1.3.0"

// scheduleName is the Task Scheduler entry CPC manages. oldScheduleName is
// what earlier versions created; it is replaced when the schedule changes.
const (
	scheduleName    = "Clear PC Cleaner weekly clean"
	oldScheduleName = "Credence PC Cleaner weekly clean"
)

// api is everything the window's JavaScript can call. Each exported method
// is bound as a global function named "cpc" + method name, e.g. cpcTargets().
// Methods run on the UI thread, so anything slow starts a goroutine and
// reports back through events.
type api struct {
	dataDir  string
	elevated bool
	exe      string

	// emit sends an event to the page. It must be safe to call from any
	// goroutine.
	emit func(name string, payload any)
	// restartElevated relaunches CPC through UAC. Set by main.
	restartElevated func() error

	mu       sync.Mutex
	cfg      settings.Settings
	targets  []*clean.Target
	byID     map[string]*clean.Target
	eng      *clean.Engine
	cancelFn context.CancelFunc

	startupRows []startup.Entry
	programRows []programs.Program

	regMu     sync.Mutex
	regIssues []regclean.Issue
	regBusy   bool

	t    tools
	hwnd uintptr
}

func newAPI(dataDir string, elevated bool, emit func(string, any)) *api {
	a := &api{dataDir: dataDir, elevated: elevated, emit: emit, byID: map[string]*clean.Target{}}
	a.exe, _ = os.Executable()
	a.cfg = settings.Load(dataDir)
	a.targets = clean.AllTargets()
	for _, t := range a.targets {
		a.byID[t.ID] = t
	}
	a.eng = clean.NewEngine()
	a.eng.Elevated = elevated
	a.eng.Exclude = append([]string(nil), a.cfg.Exclude...)
	return a
}

// Exclusions returns the folders the cleaner never touches.
func (a *api) Exclusions() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cfg.Exclude == nil {
		return []string{}
	}
	return append([]string(nil), a.cfg.Exclude...)
}

// AddExclusion asks for a folder and adds it to the never-clean list.
func (a *api) AddExclusion() ([]string, error) {
	if a.busy() {
		return a.Exclusions(), errors.New("Wait for the current clean to finish.")
	}
	f := sysutil.PickFolder(a.hwnd, "Choose a folder CPC should never clean")
	if f == "" {
		return a.Exclusions(), nil
	}
	a.mu.Lock()
	for _, x := range a.cfg.Exclude {
		if strings.EqualFold(x, f) {
			a.mu.Unlock()
			return a.Exclusions(), nil
		}
	}
	a.cfg.Exclude = append(a.cfg.Exclude, f)
	a.eng.Exclude = append([]string(nil), a.cfg.Exclude...)
	err := settings.Save(a.dataDir, a.cfg)
	a.mu.Unlock()
	return a.Exclusions(), err
}

// RemoveExclusion takes a folder off the never-clean list.
func (a *api) RemoveExclusion(folder string) ([]string, error) {
	if a.busy() {
		return a.Exclusions(), errors.New("Wait for the current clean to finish.")
	}
	a.mu.Lock()
	var keep []string
	for _, x := range a.cfg.Exclude {
		if !strings.EqualFold(x, folder) {
			keep = append(keep, x)
		}
	}
	a.cfg.Exclude = keep
	a.eng.Exclude = append([]string(nil), keep...)
	err := settings.Save(a.dataDir, a.cfg)
	a.mu.Unlock()
	return a.Exclusions(), err
}

// ---- general -------------------------------------------------------------

type infoDTO struct {
	App       string   `json:"app"`
	Version   string   `json:"version"`
	Elevated  bool     `json:"elevated"`
	LogsDir   string   `json:"logsDir"`
	DiskTotal uint64   `json:"diskTotal"`
	DiskFree  uint64   `json:"diskFree"`
	Drive     string   `json:"drive"`
	Groups    []string `json:"groups"`
}

func (a *api) Info() infoDTO {
	drive := os.Getenv("SystemDrive")
	if drive == "" {
		drive = "C:"
	}
	total, free, _ := sysutil.DiskSpace(drive + `\`)
	return infoDTO{App: appName, Version: version, Elevated: a.elevated,
		LogsDir: filepath.Join(a.dataDir, "logs"), DiskTotal: total, DiskFree: free, Drive: drive, Groups: clean.GroupOrder}
}

func (a *api) RestartAdmin() error {
	if a.restartElevated == nil {
		return errors.New("not available")
	}
	return a.restartElevated()
}

func (a *api) OpenLogs() error { return sysutil.ShellOpen(filepath.Join(a.dataDir, "logs")) }

func (a *api) OpenPath(p string) error {
	// Only open files CPC wrote itself.
	if !strings.HasPrefix(strings.ToLower(filepath.Clean(p)), strings.ToLower(filepath.Clean(a.dataDir))) {
		return errors.New("not a CPC file")
	}
	return sysutil.ShellOpen(p)
}

// ---- cleaner -------------------------------------------------------------

type targetDTO struct {
	ID          string `json:"id"`
	Section     string `json:"section"`
	Group       string `json:"group"`
	Label       string `json:"label"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
	NeedsAdmin  bool   `json:"needsAdmin"`
	Selectable  bool   `json:"selectable"`
	Checked     bool   `json:"checked"`
}

func (a *api) Targets() []targetDTO {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]targetDTO, 0, len(a.targets))
	for _, t := range a.targets {
		sel := clean.Selectable(t, a.elevated)
		out = append(out, targetDTO{
			ID: t.ID, Section: t.Section, Group: t.Group, Label: t.Label(), Name: t.Name,
			Description: t.Description, Default: t.Default, NeedsAdmin: t.NeedsAdmin, Selectable: sel,
			Checked: sel && a.cfg.IsSelected(t.ID, t.Default),
		})
	}
	return out
}

// SetSelected stores which items are ticked so the next run and /auto use them.
func (a *api) SetSelected(ids []string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	on := map[string]bool{}
	for _, id := range ids {
		on[id] = true
	}
	for _, t := range a.targets {
		if clean.Selectable(t, a.elevated) {
			a.cfg.Selected[t.ID] = on[t.ID]
		}
	}
	return settings.Save(a.dataDir, a.cfg)
}

func (a *api) pick(ids []string) []*clean.Target {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []*clean.Target
	for _, t := range a.targets { // keep display order
		if want[t.ID] && clean.Selectable(t, a.elevated) {
			out = append(out, t)
		}
	}
	return out
}

// OpenApps lists programs that are open and would stop a full clean.
func (a *api) OpenApps(ids []string) []string {
	names := []string{} // never nil: the page expects a list
	for _, p := range clean.OpenProcesses(a.pick(ids), sysutil.RunningProcesses()) {
		names = append(names, friendlyProcess(p))
	}
	return names
}

var processNames = map[string]string{
	"chrome.exe": "Google Chrome", "msedge.exe": "Microsoft Edge", "brave.exe": "Brave",
	"vivaldi.exe": "Vivaldi", "firefox.exe": "Firefox", "teams.exe": "Microsoft Teams (classic)",
	"ms-teams.exe": "Microsoft Teams", "discord.exe": "Discord", "spotify.exe": "Spotify", "zoom.exe": "Zoom",
}

func friendlyProcess(exe string) string {
	if n, ok := processNames[exe]; ok {
		return n
	}
	return exe
}

type resultDTO struct {
	ID     string `json:"id"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
	Failed int    `json:"failed"`
	Error  string `json:"error,omitempty"`
	Note   string `json:"note,omitempty"`
	Text   string `json:"text"`
}

type summaryDTO struct {
	Mode     string `json:"mode"` // "scan" or "clean"
	Files    int    `json:"files"`
	Bytes    int64  `json:"bytes"`
	Failed   int    `json:"failed"`
	Errors   int    `json:"errors"`
	Canceled bool   `json:"canceled"`
	LogPath  string `json:"logPath,omitempty"`
	Message  string `json:"message"`
}

func (a *api) Scan(ids []string) error  { return a.run(ids, false) }
func (a *api) Clean(ids []string) error { return a.run(ids, true) }

func (a *api) Cancel() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cancelFn != nil {
		a.cancelFn()
	}
}

func (a *api) busy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cancelFn != nil
}

func (a *api) run(ids []string, doClean bool) error {
	targets := a.pick(ids)
	if len(targets) == 0 {
		return errors.New("Tick at least one item first.")
	}
	a.mu.Lock()
	if a.cancelFn != nil {
		a.mu.Unlock()
		return errors.New("Already running.")
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelFn = cancel
	a.mu.Unlock()

	var log *settings.Log
	if doClean {
		log, _ = settings.NewLog(a.dataDir, time.Now())
		log.Printf("%s %s clean started %s", appName, version, time.Now().Format(time.RFC1123))
	}
	mode := "scan"
	if doClean {
		mode = "clean"
	}

	go func() {
		defer cancel()
		var lastEmit time.Time
		var current string
		h := clean.Hooks{
			OnStart: func(i int, t *clean.Target) {
				current = t.Name
				a.emit("progress", map[string]any{"mode": mode, "index": i, "total": len(targets), "id": t.ID, "name": t.Name})
				log.Printf("")
				log.Printf("[%s]", t.Name)
			},
			OnFile: func(p string) {
				if time.Since(lastEmit) < 150*time.Millisecond {
					return
				}
				lastEmit = time.Now()
				a.emit("file", map[string]any{"name": current, "path": p})
			},
			Deleted: func(p string) { log.Printf("deleted %s", p) },
			OnDone: func(_ int, t *clean.Target, r clean.Result) {
				d := resultDTO{ID: t.ID, Files: r.Files, Bytes: r.Bytes, Failed: r.Failed, Note: r.Note, Text: describe(r, doClean)}
				if r.Err != nil && ctx.Err() == nil {
					d.Error = r.Err.Error()
				}
				log.Printf("%s", d.Text)
				a.emit("result", d)
			},
		}
		sum := clean.Run(ctx, a.eng, targets, doClean, h)
		s := summaryDTO{Mode: mode, Files: sum.Files, Bytes: sum.Bytes, Failed: sum.Failed, Errors: sum.Errors, Canceled: sum.Canceled}
		s.Message = summaryText(s)
		log.Printf("")
		log.Printf("%s", s.Message)
		if log != nil {
			s.LogPath = log.Path
		}
		log.Close()

		a.mu.Lock()
		a.cancelFn = nil
		a.mu.Unlock()
		a.emit("finished", s)
	}()
	return nil
}

func summaryText(s summaryDTO) string {
	var msg string
	switch {
	case s.Canceled && s.Mode == "clean":
		msg = fmt.Sprintf("Stopped. Removed %s (%s) before stopping.", clean.FormatBytes(s.Bytes), clean.Files(s.Files))
	case s.Canceled:
		msg = "Scan stopped."
	case s.Mode == "clean":
		msg = fmt.Sprintf("Freed %s (%s).", clean.FormatBytes(s.Bytes), clean.Files(s.Files))
		if s.Failed > 0 {
			msg += fmt.Sprintf(" %s in use and skipped.", clean.Files(s.Failed))
		}
	default:
		msg = fmt.Sprintf("Found %s in %s.", clean.FormatBytes(s.Bytes), clean.Files(s.Files))
	}
	if s.Errors > 0 {
		msg += fmt.Sprintf(" %d item(s) had errors.", s.Errors)
	}
	return msg
}

// describe is the one-line result for an item, used in logs and the UI.
func describe(r clean.Result, cleaned bool) string {
	if r.Err != nil {
		return "Error: " + r.Err.Error()
	}
	if r.Note != "" && r.Files == 0 {
		return r.Note
	}
	if !cleaned {
		if r.Files == 0 {
			return "Nothing to clean"
		}
		return fmt.Sprintf("%s (%s)", clean.FormatBytes(r.Bytes), clean.Files(r.Files))
	}
	s := fmt.Sprintf("Removed %s (%s)", clean.FormatBytes(r.Bytes), clean.Files(r.Files))
	if r.Files == 0 {
		s = "Nothing removed"
	}
	if r.Failed > 0 {
		s += fmt.Sprintf(", %d in use", r.Failed)
	}
	return s
}

// ---- startup -------------------------------------------------------------

type startupDTO struct {
	Index      int    `json:"index"`
	Name       string `json:"name"`
	Command    string `json:"command"`
	Location   string `json:"location"`
	Enabled    bool   `json:"enabled"`
	NeedsAdmin bool   `json:"needsAdmin"`
	Locked     bool   `json:"locked"`
}

func (a *api) Startup() []startupDTO {
	a.startupRows = startup.List()
	out := make([]startupDTO, 0, len(a.startupRows))
	for i, e := range a.startupRows {
		out = append(out, startupDTO{Index: i, Name: e.Name, Command: e.Command, Location: e.Location,
			Enabled: e.Enabled, NeedsAdmin: e.NeedsAdmin, Locked: e.NeedsAdmin && !a.elevated})
	}
	return out
}

func (a *api) SetStartup(index int, enabled bool) ([]startupDTO, error) {
	if index < 0 || index >= len(a.startupRows) {
		return a.Startup(), errors.New("That item changed. The list was refreshed.")
	}
	e := a.startupRows[index]
	if e.NeedsAdmin && !a.elevated {
		return a.Startup(), fmt.Errorf("%q applies to all users. Restart CPC as administrator to change it.", e.Name)
	}
	if err := startup.SetEnabled(e, enabled); err != nil {
		return a.Startup(), fmt.Errorf("Couldn't change %q: %v", e.Name, err)
	}
	return a.Startup(), nil
}

func (a *api) StartupLocation(index int) error {
	if index < 0 || index >= len(a.startupRows) {
		return errors.New("That item changed. Refresh the list.")
	}
	return sysutil.ShowInExplorer(startup.Target(a.startupRows[index]))
}

// ---- programs ------------------------------------------------------------

type programDTO struct {
	Index     int    `json:"index"`
	Name      string `json:"name"`
	Publisher string `json:"publisher"`
	Version   string `json:"version"`
	SizeKB    int64  `json:"sizeKB"`
	Size      string `json:"size"`
	Installed string `json:"installed"`
	HasFolder bool   `json:"hasFolder"`
}

func (a *api) Programs() []programDTO {
	a.programRows = programs.List()
	out := make([]programDTO, 0, len(a.programRows))
	for i, p := range a.programRows {
		d := programDTO{Index: i, Name: p.Name, Publisher: p.Publisher, Version: p.Version, SizeKB: p.SizeKB, HasFolder: p.Location != ""}
		if p.SizeKB > 0 {
			d.Size = clean.FormatBytes(p.SizeKB * 1024)
		}
		if !p.InstallDate.IsZero() {
			d.Installed = p.InstallDate.Format("Jan 2, 2006")
		}
		out = append(out, d)
	}
	return out
}

func (a *api) Uninstall(index int) error {
	if index < 0 || index >= len(a.programRows) {
		return errors.New("That program changed. Refresh the list.")
	}
	return programs.RunUninstaller(a.programRows[index])
}

func (a *api) ProgramFolder(index int) error {
	if index < 0 || index >= len(a.programRows) || a.programRows[index].Location == "" {
		return errors.New("This program didn't record an install folder.")
	}
	return sysutil.ShellOpen(a.programRows[index].Location)
}

// ---- schedule ------------------------------------------------------------

type scheduleDTO struct {
	Enabled bool   `json:"enabled"`
	NextRun string `json:"nextRun"`
}

func (a *api) Schedule() scheduleDTO {
	ok, next := sysutil.ScheduleStatus(scheduleName)
	if !ok {
		ok, next = sysutil.ScheduleStatus(oldScheduleName)
	}
	return scheduleDTO{Enabled: ok, NextRun: next}
}

var validDays = map[string]bool{"MON": true, "TUE": true, "WED": true, "THU": true, "FRI": true, "SAT": true, "SUN": true}

// SetSchedule turns the weekly automatic clean on or off. day is MON..SUN,
// at is HH:MM in 24 hour time.
func (a *api) SetSchedule(enabled bool, day, at string) (scheduleDTO, error) {
	if !enabled {
		_ = sysutil.ScheduleDelete(oldScheduleName)
		err := sysutil.ScheduleDelete(scheduleName)
		return a.Schedule(), err
	}
	if !a.elevated {
		return a.Schedule(), errors.New("Restart CPC as administrator to set up automatic cleaning.")
	}
	day = strings.ToUpper(day)
	if !validDays[day] {
		return a.Schedule(), errors.New("Pick a day of the week.")
	}
	if _, err := time.Parse("15:04", at); err != nil {
		return a.Schedule(), errors.New("Pick a time.")
	}
	_ = sysutil.ScheduleDelete(oldScheduleName)
	err := sysutil.ScheduleWeekly(scheduleName, a.exe, "/auto", day, at)
	return a.Schedule(), err
}

// ---- registry ------------------------------------------------------------

type regCatDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	NeedsAdmin  bool   `json:"needsAdmin"`
	Selectable  bool   `json:"selectable"`
}

func (a *api) backupRoot() string { return filepath.Join(a.dataDir, "backups") }

func (a *api) RegCategories() []regCatDTO {
	out := make([]regCatDTO, 0, len(regclean.Categories))
	for _, c := range regclean.Categories {
		out = append(out, regCatDTO{ID: c.ID, Name: c.Name, Description: c.Description, NeedsAdmin: c.NeedsAdmin, Selectable: a.elevated || !c.NeedsAdmin})
	}
	return out
}

func (a *api) startReg() error {
	a.regMu.Lock()
	defer a.regMu.Unlock()
	if a.regBusy {
		return errors.New("Already working on the registry.")
	}
	a.regBusy = true
	return nil
}

func (a *api) endReg() {
	a.regMu.Lock()
	a.regBusy = false
	a.regMu.Unlock()
}

// RegScan looks for issues in the chosen categories. The result arrives as
// a "regScanned" event.
func (a *api) RegScan(cats []string) error {
	if len(cats) == 0 {
		return errors.New("Tick at least one category first.")
	}
	if err := a.startReg(); err != nil {
		return err
	}
	go func() {
		defer a.endReg()
		issues := regclean.Scan(cats, a.elevated)
		a.regMu.Lock()
		a.regIssues = issues
		a.regMu.Unlock()
		if issues == nil {
			issues = []regclean.Issue{}
		}
		a.emit("regScanned", issues)
	}()
	return nil
}

type regFixDTO struct {
	Fixed  int    `json:"fixed"`
	Failed int    `json:"failed"`
	Backup string `json:"backup"`
	Name   string `json:"name"`
	Error  string `json:"error,omitempty"`
}

// RegFix backs up and fixes the issues with the given IDs from the last
// scan. The result arrives as a "regFixed" event.
func (a *api) RegFix(ids []int) error {
	a.regMu.Lock()
	var chosen []regclean.Issue
	want := map[int]bool{}
	for _, id := range ids {
		want[id] = true
	}
	for _, is := range a.regIssues {
		if want[is.ID] {
			chosen = append(chosen, is)
		}
	}
	a.regMu.Unlock()
	if len(chosen) == 0 {
		return errors.New("Tick at least one issue first.")
	}
	if err := a.startReg(); err != nil {
		return err
	}
	go func() {
		defer a.endReg()
		fixed, failed, backup, err := regclean.Fix(chosen, a.backupRoot())
		d := regFixDTO{Fixed: fixed, Failed: failed, Backup: backup, Name: filepath.Base(backup)}
		if err != nil {
			d.Error = err.Error()
		}
		a.regMu.Lock()
		a.regIssues = nil
		a.regMu.Unlock()
		a.emit("regFixed", d)
	}()
	return nil
}

func (a *api) RegBackups() []regclean.Backup {
	b := regclean.Backups(a.backupRoot())
	if b == nil {
		b = []regclean.Backup{}
	}
	return b
}

func (a *api) RegRestore(name string) error { return regclean.Restore(a.backupRoot(), name) }

func (a *api) OpenBackups() error {
	_ = os.MkdirAll(a.backupRoot(), 0o755)
	return sysutil.ShellOpen(a.backupRoot())
}

// ---- events --------------------------------------------------------------

// eventJS builds the JavaScript that delivers an event to the page.
func eventJS(name string, payload any) string {
	n, _ := json.Marshal(name)
	p, err := json.Marshal(payload)
	if err != nil {
		p = []byte("null")
	}
	return "window.cpcEvent && window.cpcEvent(" + string(n) + "," + string(p) + ")"
}
