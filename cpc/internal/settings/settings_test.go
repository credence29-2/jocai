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
	if filepath.Base(d) != "ClearPCCleaner" {
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
