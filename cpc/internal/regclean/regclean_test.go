package regclean

import (
	"strings"
	"testing"
)

type fakeFS struct {
	files  map[string]bool
	drives map[string]bool
}

func (f fakeFS) Exists(p string) bool      { return f.files[strings.ToLower(p)] }
func (f fakeFS) DriveExists(r string) bool { return f.drives[strings.ToLower(r[:1])] }

var ffs = fakeFS{
	files:  map[string]bool{`c:\program files\app\app.exe`: true, `c:\windows\fonts\arial.ttf`: true},
	drives: map[string]bool{"c": true},
}

func TestMissingPath(t *testing.T) {
	t.Setenv("CPC_PF", `C:\Program Files`)
	cases := []struct {
		in      string
		missing bool
	}{
		{`C:\Program Files\App\app.exe`, false},
		{`"C:\Program Files\App\app.exe"`, false},
		{`C:\Program Files\Gone\gone.exe`, true},
		{`%CPC_PF%\Gone\gone.exe`, true},
		{`%CPC_PF%\App\app.exe`, false},
		{`%CPC_UNKNOWN_VAR%\x.exe`, false}, // unknown variable: leave alone
		{`D:\Games\game.exe`, false},       // drive not present: leave alone
		{`\\server\share\tool.exe`, false}, // network path: leave alone
		{`notepad.exe`, false},             // relative: leave alone
		{`@%SystemRoot%\system32\shell32.dll,-1`, false},
		{``, false},
	}
	for _, c := range cases {
		if _, got := MissingPath(c.in, ffs); got != c.missing {
			t.Errorf("MissingPath(%q) = %v, want %v", c.in, got, c.missing)
		}
	}
}

func TestMissingCommand(t *testing.T) {
	cases := map[string]bool{
		`"C:\Program Files\Gone\gone.exe" --tray`:            true,
		`C:\Program Files\Gone\gone.exe --tray`:              true,
		`"C:\Program Files\App\app.exe" /min`:                false,
		`rundll32.exe C:\Gone\thing.dll,Start`:               false,
		`C:\Windows\System32\rundll32.exe C:\Gone\x.dll,Run`: false,
		`MsiExec.exe /X{1234}`:                               false,
		`cmd.exe /c start something`:                         false,
	}
	for in, want := range cases {
		if _, got := MissingCommand(in, ffs); got != want {
			t.Errorf("MissingCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFirewallAndMui(t *testing.T) {
	r := `v2.30|Action=Allow|Active=TRUE|Dir=In|Protocol=6|App=C:\Gone\server.exe|Name=Gone Server|`
	if FirewallApp(r) != `C:\Gone\server.exe` {
		t.Errorf("FirewallApp = %q", FirewallApp(r))
	}
	if FirewallApp(`v2.30|Action=Allow|Svc=dnscache|`) != "" {
		t.Error("rule without App should give empty")
	}
	if MuiCacheFile(`C:\Apps\tool.exe.FriendlyAppName`) != `C:\Apps\tool.exe` || MuiCacheFile(`LangID`) != "" {
		t.Error("MuiCacheFile wrong")
	}
}

func TestCategoriesUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Categories {
		if seen[c.ID] || c.Name == "" || c.Description == "" {
			t.Errorf("bad category %+v", c)
		}
		seen[c.ID] = true
	}
}

func TestWow64Variants(t *testing.T) {
	v := wow64Variants(`C:\Windows\system32\VsGraphicsHelper.dll`)
	if len(v) != 3 || v[1] != `C:\Windows\SysWOW64\VsGraphicsHelper.dll` || v[2] != `C:\Windows\Sysnative\VsGraphicsHelper.dll` {
		t.Fatalf("variants = %v", v)
	}
	if len(wow64Variants(`C:\Program Files\App\a.dll`)) != 1 {
		t.Fatal("non-system paths should have no variants")
	}
}

func TestUnderWindows(t *testing.T) {
	t.Setenv("SystemRoot", `C:\Windows`)
	if !UnderWindows(`C:\WINDOWS\system32\MuxSvcHost.exe`) || UnderWindows(`C:\Windows.old\x.exe`) || UnderWindows(`C:\Program Files\x.exe`) {
		t.Fatal("UnderWindows wrong")
	}
}
