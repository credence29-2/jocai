package main

import (
	"path/filepath"
	"testing"
)

func TestDiskProtected(t *testing.T) {
	folders := []string{`C:\Windows`, `C:\Program Files`, `C:\ProgramData\Microsoft`}
	bad := []string{`C:\Windows\explorer.exe`, `C:\WINDOWS\System32\x.dll`, `C:\Program Files\App\a.exe`, `C:\pagefile.sys`, `D:\Hiberfil.SYS`, `C:\Users\j\Desktop.ini`}
	for _, p := range bad {
		if diskProtectedIn(filepath.FromSlash(p), folders) == "" {
			t.Errorf("%s should be protected", p)
		}
	}
	good := []string{`C:\Users\j\Downloads\big.iso`, `C:\Windows.old\x.exe`, `C:\Program Files Extra\x.bin`, `D:\Movies\a.mkv`}
	for _, p := range good {
		if why := diskProtectedIn(filepath.FromSlash(p), folders); why != "" {
			t.Errorf("%s should be allowed: %s", p, why)
		}
	}
}
