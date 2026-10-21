package clean

import (
	"context"
	"strings"
)

// Hooks report progress from Run. Every field is optional.
type Hooks struct {
	OnStart func(i int, t *Target)
	OnDone  func(i int, t *Target, r Result)
	OnFile  func(path string)
	// Deleted is called for each file removed (clean only), for logging.
	Deleted func(path string)
}

// Summary totals a run.
type Summary struct {
	Files    int
	Bytes    int64
	Failed   int
	Errors   int
	Canceled bool
}

// Run scans (clean=false) or cleans (clean=true) each target in order.
func Run(ctx context.Context, e *Engine, targets []*Target, clean bool, h Hooks) Summary {
	var s Summary
	for i, t := range targets {
		if ctx.Err() != nil {
			s.Canceled = true
			break
		}
		if h.OnStart != nil {
			h.OnStart(i, t)
		}
		var r Result
		if clean {
			r = e.Clean(ctx, t, h.OnFile, h.Deleted)
		} else {
			r = e.Scan(ctx, t, h.OnFile)
		}
		s.Files += r.Files
		s.Bytes += r.Bytes
		s.Failed += r.Failed
		if r.Err != nil {
			if ctx.Err() != nil {
				s.Canceled = true
			} else {
				s.Errors++
			}
		}
		if h.OnDone != nil {
			h.OnDone(i, t, r)
		}
	}
	return s
}

// OpenProcesses returns the targets' processes that are currently running,
// given the running set from sysutil.RunningProcesses.
func OpenProcesses(targets []*Target, running map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range targets {
		for _, p := range t.Processes {
			if running[strings.ToLower(p)] && !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// Selectable reports whether a target can run with the current privileges.
func Selectable(t *Target, elevated bool) bool {
	return elevated || !t.NeedsAdmin
}
