package install

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPaths(t *testing.T) {
	t.Setenv("LOCALAPPDATA", `C:\Users\j\AppData\Local`)
	if Exe() != filepath.Join(`C:\Users\j\AppData\Local`, "Programs", "Kempt", "Kempt.exe") {
		t.Fatalf("Exe = %s", Exe())
	}
	if !IsInstalledCopy(filepath.Join(`c:\users\J\appdata\local`, "programs", "kempt", "KEMPT.EXE")) {
		t.Fatal("case-insensitive match failed")
	}
	home, _ := os.UserHomeDir()
	if !Removable(filepath.Join(home, "Desktop", "Kempt.exe")) || !Removable(filepath.Join(home, "Downloads", "Kempt.exe")) {
		t.Fatal("Desktop and Downloads copies should be removable")
	}
	if Removable(filepath.Join(home, "Documents", "Tools", "Kempt.exe")) || Removable(Exe()) {
		t.Fatal("other locations must not be removable")
	}
}

func TestCopyReplacesRunning(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "new.exe"), filepath.Join(dir, "Kempt.exe")
	os.WriteFile(src, []byte("new"), 0o755)
	os.WriteFile(dst, []byte("old"), 0o755)
	if err := copyFile(src, dst); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "new" {
		t.Fatal("not replaced")
	}
	if b, _ := os.ReadFile(dst + ".old"); string(b) != "old" {
		t.Fatal("old copy not kept aside")
	}
}
