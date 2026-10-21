package startup

import "testing"

func TestExePath(t *testing.T) {
	cases := map[string]string{
		`"C:\Program Files\App\app.exe" --min`:   `C:\Program Files\App\app.exe`,
		`C:\Program Files\App\app.exe --min`:     `C:\Program Files\App\app.exe`,
		`C:\Tools\thing.EXE`:                     `C:\Tools\thing.EXE`,
		`  rundll32.exe shell32.dll,Control_Run`: `rundll32.exe`,
		`"unterminated`:                          `unterminated`,
		`script.bat arg`:                         `script.bat`,
	}
	for in, want := range cases {
		if got := ExePath(in); got != want {
			t.Errorf("ExePath(%q) = %q, want %q", in, got, want)
		}
	}
}
