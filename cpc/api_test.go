package main

import (
	"encoding/json"
	"os"

	"github.com/credence29-2/jocai/cpc/internal/clean"

	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type recorder struct {
	mu     sync.Mutex
	events []string
	done   chan map[string]any
}

func (r *recorder) emit(name string, payload any) {
	r.mu.Lock()
	r.events = append(r.events, name)
	r.mu.Unlock()
	if name == "finished" {
		s := payload.(summaryDTO)
		r.done <- map[string]any{"mode": s.Mode, "files": s.Files, "bytes": s.Bytes, "log": s.LogPath, "msg": s.Message}
	}
}

func TestTargetsAndSelectionPersist(t *testing.T) {
	dir := t.TempDir()
	a := newAPI(dir, false, func(string, any) {})
	ts := a.Targets()
	if len(ts) == 0 {
		t.Fatal("no targets")
	}
	for _, x := range ts {
		if x.NeedsAdmin && (x.Selectable || x.Checked) {
			t.Fatalf("%s needs admin but is selectable when not elevated", x.ID)
		}
		if x.Group == "" || x.Label == "" {
			t.Fatalf("%s missing group or label", x.ID)
		}
	}
	if err := a.SetSelected([]string{"win.recycle", "win.temp.system"}); err != nil {
		t.Fatal(err)
	}
	b := newAPI(dir, false, func(string, any) {})
	checked := map[string]bool{}
	for _, x := range b.Targets() {
		checked[x.ID] = x.Checked
	}
	if !checked["win.recycle"] || checked["chrome.cache"] || checked["win.temp.system"] {
		t.Fatalf("selection not persisted correctly: %v", checked)
	}
}

func TestRunEmitsEventsAndRejectsDoubleStart(t *testing.T) {
	dir := t.TempDir()
	cache := filepath.Join(dir, "local", "App", "Cache")
	os.MkdirAll(cache, 0o755)
	os.WriteFile(filepath.Join(cache, "f1"), make([]byte, 100), 0o644)

	rec := &recorder{done: make(chan map[string]any, 1)}
	a := newAPI(filepath.Join(dir, "data"), false, rec.emit)
	// Swap in a target with native paths so this runs on any OS.
	tg := &clean.Target{ID: "chrome.cache", Group: "G", Section: "S", Name: "Test cache",
		Rules: []clean.Rule{{Root: cache, Recursive: true}}}
	a.targets = []*clean.Target{tg}
	a.byID = map[string]*clean.Target{tg.ID: tg}
	if err := a.Scan([]string{"chrome.cache"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Scan([]string{"chrome.cache"}); err == nil && a.busy() {
		t.Error("second scan while busy should be rejected")
	}
	var got map[string]any
	select {
	case got = <-rec.done:
	case <-time.After(5 * time.Second):
		t.Fatal("scan never finished")
	}
	if got["files"].(int) != 1 || got["bytes"].(int64) != 100 {
		t.Fatalf("scan summary %v", got)
	}
	if _, err := os.Stat(filepath.Join(cache, "f1")); err != nil {
		t.Fatal("scan deleted a file")
	}

	if err := a.Clean([]string{"chrome.cache"}); err != nil {
		t.Fatal(err)
	}
	got = <-rec.done
	if got["files"].(int) != 1 || got["log"].(string) == "" || !strings.HasPrefix(got["msg"].(string), "Freed 100 B") {
		t.Fatalf("clean summary %v", got)
	}
	if _, err := os.Stat(filepath.Join(cache, "f1")); !os.IsNotExist(err) {
		t.Fatal("clean did not delete the file")
	}
	logText, _ := os.ReadFile(got["log"].(string))
	if !strings.Contains(string(logText), "deleted ") {
		t.Fatalf("log missing deletions: %s", logText)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	joined := strings.Join(rec.events, ",")
	if !strings.Contains(joined, "progress") || !strings.Contains(joined, "result") {
		t.Fatalf("missing events: %s", joined)
	}
}

func TestRunNeedsSelection(t *testing.T) {
	a := newAPI(t.TempDir(), false, func(string, any) {})
	if err := a.Scan(nil); err == nil {
		t.Fatal("empty selection should error")
	}
	// Admin-only items are dropped when not elevated.
	if err := a.Clean([]string{"win.update"}); err == nil {
		t.Fatal("admin-only item should not run unelevated")
	}
}

func TestOpenPathOnlyOwnFiles(t *testing.T) {
	a := newAPI(t.TempDir(), false, func(string, any) {})
	if err := a.OpenPath("/etc/passwd"); err == nil || !strings.Contains(err.Error(), "not a CPC file") {
		t.Fatalf("expected refusal, got %v", err)
	}
}

func TestScheduleValidation(t *testing.T) {
	a := newAPI(t.TempDir(), true, func(string, any) {})
	if _, err := a.SetSchedule(true, "FUNDAY", "03:00"); err == nil {
		t.Fatal("bad day accepted")
	}
	if _, err := a.SetSchedule(true, "SUN", "25:00"); err == nil {
		t.Fatal("bad time accepted")
	}
	b := newAPI(t.TempDir(), false, func(string, any) {})
	if _, err := b.SetSchedule(true, "SUN", "03:00"); err == nil {
		t.Fatal("unelevated schedule accepted")
	}
}

func TestEventJS(t *testing.T) {
	js := eventJS("file", map[string]string{"path": `C:\a"b`})
	if js != `window.cpcEvent && window.cpcEvent("file",{"path":"C:\\a\"b"})` {
		t.Fatalf("got %s", js)
	}
}

func TestRegistryAPIGuards(t *testing.T) {
	a := newAPI(t.TempDir(), false, func(string, any) {})
	cats := a.RegCategories()
	if len(cats) == 0 {
		t.Fatal("no registry categories")
	}
	for _, c := range cats {
		if c.NeedsAdmin && c.Selectable {
			t.Errorf("%s needs admin but is selectable unelevated", c.ID)
		}
	}
	if err := a.RegScan(nil); err == nil {
		t.Error("empty category list accepted")
	}
	if err := a.RegFix([]int{1}); err == nil {
		t.Error("fix without a scan accepted")
	}
	if err := a.RegRestore("../../etc"); err == nil {
		t.Error("path traversal accepted")
	}
}

func TestToolGuards(t *testing.T) {
	a := newAPI(t.TempDir(), false, func(string, any) {})
	if err := a.DupeScan([]string{"/definitely/not/here"}, 0); err == nil {
		t.Error("missing folder accepted")
	}
	if err := a.DupeDelete([]int{1}); err == nil {
		t.Error("delete without a search accepted")
	}
	if err := a.UpdateApps(nil); err == nil {
		t.Error("empty update list accepted")
	}
	if err := a.OpenSettings("cmd.exe"); err == nil {
		t.Error("arbitrary settings target accepted")
	}
	if err := a.ShowFile("/etc/passwd"); err == nil {
		t.Error("ShowFile outside search results accepted")
	}
}

func TestDupeScanEvents(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "docs", "stuff")
	os.MkdirAll(root, 0o755)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("same content"), 0o644)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("same content"), 0o644)
	got := make(chan dupeResultDTO, 1)
	a := newAPI(t.TempDir(), false, func(name string, p any) {
		if name == "dupeFinished" {
			got <- p.(dupeResultDTO)
		}
	})
	if err := a.DupeScan([]string{root}, 0); err != nil {
		t.Fatal(err)
	}
	r := <-got
	if len(r.Groups) != 1 || r.Wasted != 12 {
		t.Fatalf("result %+v", r)
	}
	ids := []int{r.Groups[0].Files[0].ID, r.Groups[0].Files[1].ID}
	if err := a.DupeDelete(ids); err == nil {
		t.Fatal("deleting every copy must be refused")
	}
}

func TestExclusionsPersistAndApply(t *testing.T) {
	dir := t.TempDir()
	a := newAPI(dir, false, func(string, any) {})
	a.cfg.Exclude = []string{"/keep/me"}
	if _, err := a.RemoveExclusion("/KEEP/ME"); err != nil {
		t.Fatal(err)
	}
	if len(a.Exclusions()) != 0 || len(a.eng.Exclude) != 0 {
		t.Fatal("exclusion not removed (case-insensitive)")
	}
	b := newAPI(dir, false, func(string, any) {})
	if len(b.Exclusions()) != 0 {
		t.Fatal("removal not saved")
	}
}

// The page reads .length on these, so they must encode as [] not null.
func TestListsNeverEncodeAsNull(t *testing.T) {
	a := newAPI(t.TempDir(), false, func(string, any) {})
	for name, v := range map[string]any{
		"OpenApps": a.OpenApps(nil), "Exclusions": a.Exclusions(), "RegBackups": a.RegBackups(),
		"DupeDefaults": a.DupeDefaults(), "Targets": a.Targets(), "RegCategories": a.RegCategories(),
	} {
		b, _ := json.Marshal(v)
		if string(b) == "null" {
			t.Errorf("%s encodes as null", name)
		}
	}
}
