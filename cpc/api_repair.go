package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/bloat"
	"github.com/credence29-2/jocai/cpc/internal/health"
	"github.com/credence29-2/jocai/cpc/internal/repair"
	"github.com/credence29-2/jocai/cpc/internal/report"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// ---- Repair tools ----------------------------------------------------------

type repairState struct {
	mu      sync.Mutex
	running string
	cancel  context.CancelFunc
}

var repairs repairState

// RepairTools lists the tools with whether each can run right now.
func (a *api) RepairTools() []map[string]any {
	out := []map[string]any{}
	for _, t := range repair.Tools {
		out = append(out, map[string]any{"id": t.ID, "name": t.Name, "description": t.Description, "when": t.When,
			"minutes": t.Minutes, "needsAdmin": t.NeedsAdmin, "reboot": t.Reboot, "selectable": a.elevated || !t.NeedsAdmin})
	}
	return out
}

// RunRepair starts a tool. Output arrives as "repairProgress" events and the
// outcome as "repairDone".
func (a *api) RunRepair(id string) error {
	t, ok := repair.ByID(id)
	if !ok {
		return errors.New("unknown tool")
	}
	if t.NeedsAdmin && !a.elevated {
		return errors.New("Restart Kempt as administrator to run this.")
	}
	repairs.mu.Lock()
	if repairs.running != "" {
		repairs.mu.Unlock()
		return errors.New("Another repair is still running.")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	repairs.running, repairs.cancel = id, cancel
	repairs.mu.Unlock()
	go func() {
		defer cancel()
		var last time.Time
		res := repair.Run(ctx, t, func(step, total int, lines []string, pct int) {
			if time.Since(last) < 200*time.Millisecond {
				return
			}
			last = time.Now()
			if len(lines) > 60 {
				lines = lines[len(lines)-60:]
			}
			a.emit("repairProgress", map[string]any{"id": id, "step": step, "total": total, "lines": lines, "pct": pct})
		})
		repairs.mu.Lock()
		repairs.running, repairs.cancel = "", nil
		repairs.mu.Unlock()
		a.emit("repairDone", map[string]any{"id": id, "ok": res.OK, "text": res.Text, "output": res.Output, "reboot": res.Reboot})
	}()
	return nil
}

// StopRepair cancels the running tool.
func (a *api) StopRepair() {
	repairs.mu.Lock()
	defer repairs.mu.Unlock()
	if repairs.cancel != nil {
		repairs.cancel()
	}
}

// ---- Bloatware remover -------------------------------------------------------

func (a *api) BloatList() int {
	return a.async(func() (any, error) {
		list, err := bloat.List()
		if list == nil {
			list = []bloat.App{}
		}
		return map[string]any{"apps": list, "groups": bloat.GroupOrder, "elevated": a.elevated}, err
	})
}

// BloatRemove removes the chosen apps one at a time. Progress arrives as
// "bloatProgress" and the end as "bloatDone".
func (a *api) BloatRemove(names []string, allUsers bool) error {
	if len(names) == 0 {
		return errors.New("Tick at least one app first.")
	}
	for _, n := range names {
		if !bloat.SafeName(n) {
			return errors.New("unexpected package name")
		}
	}
	allUsers = allUsers && a.elevated
	go func() {
		ok, failed := 0, 0
		for i, n := range names {
			a.emit("bloatProgress", map[string]any{"name": n, "index": i, "total": len(names), "status": "removing"})
			if err := bloat.Remove(n, allUsers); err != nil {
				failed++
				a.emit("bloatProgress", map[string]any{"name": n, "index": i, "total": len(names), "status": "failed", "error": err.Error()})
			} else {
				ok++
				a.emit("bloatProgress", map[string]any{"name": n, "index": i, "total": len(names), "status": "removed"})
			}
		}
		a.emit("bloatDone", map[string]any{"removed": ok, "failed": failed})
	}()
	return nil
}

// ---- Tech report -------------------------------------------------------------

func (a *api) reportsDir() string { return filepath.Join(a.dataDir, "reports") }

// buildReport gathers and writes the report files, returning the HTML path.
func (a *api) buildReport() (htmlPath, txtPath string, r *report.Report, err error) {
	r = report.Gather(appName, version, companyName, filepath.Join(a.dataDir, "logs"), a.elevated)
	dir := a.reportsDir()
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return "", "", nil, err
	}
	base := filepath.Join(dir, "report-"+r.System.Computer+"-"+r.Generated.Format("20060102-150405"))
	htmlPath, txtPath = base+".html", base+".txt"
	if err = os.WriteFile(htmlPath, []byte(r.HTML()), 0o644); err != nil {
		return "", "", nil, err
	}
	if err = os.WriteFile(txtPath, []byte(r.Text()), 0o644); err != nil {
		return "", "", nil, err
	}
	pruneReports(dir, 20)
	return htmlPath, txtPath, r, nil
}

func pruneReports(dir string, keep int) {
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	// names sort by time because of the timestamp; keep the newest 2*keep files (html+txt)
	for len(names) > keep*2 {
		_ = os.Remove(filepath.Join(dir, names[0]))
		names = names[1:]
	}
}

// Report builds the tech report. The result has the file paths, the plain
// text for copying and the warnings.
func (a *api) Report() int {
	return a.async(func() (any, error) {
		h, t, r, err := a.buildReport()
		if err != nil {
			return nil, err
		}
		return map[string]any{"html": h, "txt": t, "text": r.Text(), "warnings": r.Warnings, "computer": r.System.Computer}, nil
	})
}

func (a *api) OpenReports() error {
	_ = os.MkdirAll(a.reportsDir(), 0o755)
	return sysutil.ShellOpen(a.reportsDir())
}

// ---- Drive and battery health ------------------------------------------------

func (a *api) Health() int {
	return a.async(func() (any, error) {
		disks, _ := health.Disks()
		if disks == nil {
			disks = []health.Disk{}
		}
		return map[string]any{"disks": disks, "battery": health.BatteryInfo()}, nil
	})
}
