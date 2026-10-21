package clean

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

type fakeCustom struct{ cleaned bool }

func (f *fakeCustom) Scan(context.Context) Result { return Result{Files: 2, Bytes: 20} }
func (f *fakeCustom) Clean(context.Context) Result {
	f.cleaned = true
	return Result{Files: 2, Bytes: 20}
}

func TestRunTotalsAndHooks(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a", "b", "f1"), 5, 0)
	fc := &fakeCustom{}
	targets := []*Target{
		{ID: "files", Rules: []Rule{{Root: filepath.Join(dir, "a", "b")}}},
		{ID: "custom", Custom: fc},
	}
	var started, done []string
	var deleted int
	h := Hooks{
		OnStart: func(i int, t *Target) { started = append(started, t.ID) },
		OnDone:  func(i int, t *Target, r Result) { done = append(done, t.ID) },
		Deleted: func(string) { deleted++ },
	}
	s := Run(context.Background(), NewEngine(), targets, false, h)
	if s.Files != 3 || s.Bytes != 25 || fc.cleaned {
		t.Fatalf("scan summary %+v cleaned=%v", s, fc.cleaned)
	}
	s = Run(context.Background(), NewEngine(), targets, true, h)
	if s.Files != 3 || deleted != 1 || !fc.cleaned {
		t.Fatalf("clean summary %+v deleted=%d", s, deleted)
	}
	if !reflect.DeepEqual(started, []string{"files", "custom", "files", "custom"}) || len(done) != 4 {
		t.Fatalf("hooks started=%v done=%v", started, done)
	}
}

func TestOpenProcessesAndSelectable(t *testing.T) {
	targets := []*Target{{Processes: []string{"chrome.exe"}}, {Processes: []string{"chrome.exe", "msedge.exe"}}}
	got := OpenProcesses(targets, map[string]bool{"chrome.exe": true})
	if !reflect.DeepEqual(got, []string{"chrome.exe"}) {
		t.Fatalf("got %v", got)
	}
	if Selectable(&Target{NeedsAdmin: true}, false) || !Selectable(&Target{NeedsAdmin: true}, true) {
		t.Fatal("selectable wrong")
	}
}
