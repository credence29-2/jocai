package settings

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRoundTripAndDefaults(t *testing.T) {
	d := t.TempDir()
	s := Load(d)
	if !s.IsSelected("x", true) || s.IsSelected("x", false) {
		t.Fatal("missing id should use default")
	}
	s.Selected["x"] = false
	if err := Save(d, s); err != nil {
		t.Fatal(err)
	}
	if Load(d).IsSelected("x", true) {
		t.Fatal("saved choice not loaded")
	}
	os.WriteFile(filepath.Join(d, "settings.json"), []byte("{not json"), 0o644)
	if Load(d).Selected == nil {
		t.Fatal("broken file must still give a usable map")
	}
}

func TestLogPrune(t *testing.T) {
	d := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < keepLogs+5; i++ {
		l, err := NewLog(d, base.Add(time.Duration(i)*time.Second))
		if err != nil {
			t.Fatal(err)
		}
		l.Printf("line %d", i)
		l.Close()
	}
	entries, _ := os.ReadDir(filepath.Join(d, "logs"))
	if len(entries) > keepLogs+1 {
		t.Fatalf("expected pruning, got %d logs", len(entries))
	}
	var nilLog *Log
	nilLog.Printf("no panic")
	nilLog.Close()
}

func TestDirMigratesOldFolder(t *testing.T) {
	base := t.TempDir()
	t.Setenv("LOCALAPPDATA", base)
	old := filepath.Join(base, "CredencePCCleaner")
	os.MkdirAll(old, 0o755)
	os.WriteFile(filepath.Join(old, "settings.json"), []byte(`{"selected":{"x":true}}`), 0o644)
	d := Dir()
	if filepath.Base(d) != "Kempt" {
		t.Fatalf("dir = %s", d)
	}
	if !Load(d).IsSelected("x", false) {
		t.Fatal("old settings were not carried over")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old folder should have been moved")
	}
	// Second call must not touch anything.
	if Dir() != d {
		t.Fatal("dir changed on second call")
	}
}

func TestExportRoundTrip(t *testing.T) {
	s := Settings{Selected: map[string]bool{"win.temp.user": true, "chrome.cookies": false}, Exclude: []string{`C:\Keep`}, Theme: "light", TechMode: true, Tray: true, LastUpdateCheck: "2026-10-26T00:00:00Z"}
	b, err := Encode("Kempt", "1.5.0", s)
	if err != nil {
		t.Fatal(err)
	}
	e, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if e.App != "Kempt" || e.Version != "1.5.0" || e.Settings.Theme != "light" || !e.Settings.TechMode || !e.Settings.Tray || len(e.Settings.Exclude) != 1 || e.Settings.Selected["win.temp.user"] != true {
		t.Fatalf("round trip lost data: %+v", e)
	}
	if e.Settings.LastUpdateCheck != "" {
		t.Fatal("export should not carry the update check time")
	}
	m := Merge(Settings{LastUpdateCheck: "keep"}, e)
	if m.LastUpdateCheck != "keep" || m.Theme != "light" {
		t.Fatalf("merge wrong: %+v", m)
	}
	if _, err := Decode([]byte(`{"hello": 1}`)); err == nil {
		t.Fatal("random JSON accepted")
	}
	if _, err := Decode([]byte(`not json`)); err == nil {
		t.Fatal("garbage accepted")
	}
	if e, err := Decode([]byte(`{"selected":{"a":true}}`)); err != nil || !e.Settings.Selected["a"] {
		t.Fatalf("bare settings.json rejected: %v", err)
	}
	if e, _ := Decode([]byte(`{"app":"Kempt","settings":{"selected":{},"theme":"neon"}}`)); e.Settings.Theme != "" {
		t.Fatal("unknown theme kept")
	}
}
