package programs

import (
	"testing"
	"time"
)

func TestUninstallCommand(t *testing.T) {
	cases := map[string]string{
		`MsiExec.exe /I{1234-ABCD}`:                    `MsiExec.exe /X{1234-ABCD}`,
		`msiexec /i {1234}`:                            `msiexec /X {1234}`,
		`"C:\Windows\System32\MsiExec.exe" /I{1}`:      `"C:\Windows\System32\MsiExec.exe" /X{1}`,
		`MsiExec.exe /X{1234}`:                         `MsiExec.exe /X{1234}`,
		`"C:\Program Files\App\unins000.exe"`:          `"C:\Program Files\App\unins000.exe"`,
		`"C:\Program Files\Installer\setup.exe" /I{x}`: `"C:\Program Files\Installer\setup.exe" /I{x}`,
	}
	for in, want := range cases {
		if got := UninstallCommand(in); got != want {
			t.Errorf("UninstallCommand(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseInstallDate(t *testing.T) {
	if got := ParseInstallDate("20240115"); !got.Equal(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("got %v", got)
	}
	for _, bad := range []string{"", "2024", "2024-01-15", "abcdefgh"} {
		if !ParseInstallDate(bad).IsZero() {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func TestDedupe(t *testing.T) {
	in := []Program{{Name: "App", Version: "1"}, {Name: "app", Version: "1"}, {Name: "App", Version: "2"}}
	if got := Dedupe(in); len(got) != 2 {
		t.Errorf("got %d", len(got))
	}
}
