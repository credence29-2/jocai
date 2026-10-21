package main

import (
	"context"
	"fmt"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/clean"
	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// runAuto cleans the saved selection with no window, for Task Scheduler.
// Items whose program is open are skipped instead of cleaned half way.
// Returns the process exit code.
func runAuto() int {
	dir := settings.Dir()
	cfg := settings.Load(dir)
	elevated := sysutil.IsElevated()
	running := sysutil.RunningProcesses()

	log, err := settings.NewLog(dir, time.Now())
	if err != nil {
		return 2
	}
	defer log.Close()
	log.Printf("%s %s automatic clean %s (administrator: %v)", appName, version, time.Now().Format(time.RFC1123), elevated)

	var targets []*clean.Target
	for _, t := range clean.AllTargets() {
		if !cfg.IsSelected(t.ID, t.Default) || !clean.Selectable(t, elevated) {
			continue
		}
		if open := clean.OpenProcesses([]*clean.Target{t}, running); len(open) > 0 {
			log.Printf("skipped %s because %v is running", t.Name, open)
			continue
		}
		targets = append(targets, t)
	}

	eng := clean.NewEngine()
	eng.Elevated = elevated
	sum := clean.Run(context.Background(), eng, targets, true, clean.Hooks{
		OnStart: func(_ int, t *clean.Target) { log.Printf(""); log.Printf("[%s]", t.Name) },
		Deleted: func(p string) { log.Printf("deleted %s", p) },
		OnDone:  func(_ int, _ *clean.Target, r clean.Result) { log.Printf("%s", describe(r, true)) },
	})
	log.Printf("")
	log.Printf("%s", fmt.Sprintf("Freed %s (%s), %d in use, %d errors.", clean.FormatBytes(sum.Bytes), clean.Files(sum.Files), sum.Failed, sum.Errors))
	if sum.Errors > 0 {
		return 1
	}
	return 0
}
