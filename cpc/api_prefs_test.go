package main

import (
	"path/filepath"
	"testing"
)

func TestExportImportFiles(t *testing.T) {
	dir := t.TempDir()
	a := newAPI(dir, false, func(string, any) {})
	if _, err := a.SetPref("theme", "light"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetPref("techMode", "true"); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSelected([]string{"win.recycle"}); err != nil {
		t.Fatal(err)
	}
	a.cfg.Exclude = []string{`C:\Keep`}
	file := filepath.Join(dir, "x.json")
	if err := a.exportTo(file); err != nil {
		t.Fatal(err)
	}
	b := newAPI(t.TempDir(), false, func(string, any) {})
	r, err := b.importFrom(file)
	if err != nil {
		t.Fatal(err)
	}
	if r.Excluded != 1 || b.Prefs().Theme != "light" || !b.Prefs().TechMode || len(b.eng.Exclude) != 1 {
		t.Fatalf("import lost data: %+v %+v", r, b.Prefs())
	}
	checked := map[string]bool{}
	for _, x := range b.Targets() {
		checked[x.ID] = x.Checked
	}
	if !checked["win.recycle"] || checked["chrome.cache"] {
		t.Fatalf("selection not imported: %v", checked)
	}
	if _, err := b.importFrom(filepath.Join(dir, "missing.json")); err == nil {
		t.Fatal("missing file accepted")
	}
	if _, err := a.SetPref("theme", "neon"); err == nil {
		t.Fatal("bad theme accepted")
	}
}
