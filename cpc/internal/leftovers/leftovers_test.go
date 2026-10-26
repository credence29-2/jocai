package leftovers

import (
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"7-Zip 24.08 (x64)":       "7-Zip",
		"Google Chrome":           "Google Chrome",
		"Zoom Workplace (64-bit)": "Zoom Workplace",
		"Microsoft Visual C++ 2015-2022 Redistributable (x64) - 14.40.33810": "Microsoft Visual C++ Redistributable",
		"Python 3.12.4 (64-bit)":   "Python",
		"Git":                      "Git",
		"Notepad++ (64-bit x64)":   "Notepad++",
		"Toast POS Terminal 4.2.1": "Toast POS Terminal",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPublisherName(t *testing.T) {
	cases := map[string]string{"Toast, Inc.": "Toast", "Google LLC": "Google", "Igor Pavlov": "Igor Pavlov", "Zoom Communications, Inc.": "Zoom Communications", "VideoLAN": "VideoLAN", "Microsoft Corporation": "Microsoft", "The Document Foundation": "The Document Foundation"}
	for in, want := range cases {
		if got := PublisherName(in); got != want {
			t.Errorf("PublisherName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNames(t *testing.T) {
	got := Names("Google Chrome", "Google LLC")
	if strings.Join(got, "|") != "Google Chrome|Chrome" {
		t.Errorf("Names = %v", got)
	}
	got = Names("Python 3.12.4 (64-bit)", "Python Software Foundation")
	if len(got) != 1 || got[0] != "Python 3.12.4 (64-bit)" {
		// "Python" alone is generic, so only the exact display name is usable.
		t.Errorf("Names python = %v", got)
	}
	if got := Names("Microsoft Edge", "Microsoft Corporation"); strings.Join(got, "|") != "Microsoft Edge|Edge" {
		t.Errorf("Names edge = %v", got)
	}
	if got := Names("Up", "X"); len(got) != 0 {
		t.Errorf("short name should give nothing, got %v", got)
	}
}

func TestFolderCandidates(t *testing.T) {
	p := Program{Name: "VLC media player", Publisher: "VideoLAN", Location: `C:\Program Files\VideoLAN\VLC`}
	got := FolderCandidates(p, []string{`C:\Program Files`, `C:\Users\j\AppData\Roaming`})
	want := []string{`C:\Program Files\VideoLAN\VLC`, `C:\Program Files\VLC media player`, `C:\Program Files\VideoLAN\VLC media player`, `C:\Users\j\AppData\Roaming\VLC media player`, `C:\Users\j\AppData\Roaming\VideoLAN\VLC media player`, `C:\Users\j\AppData\Roaming\VideoLAN\VLC`}
	for _, w := range want {
		found := false
		for _, g := range got {
			if strings.EqualFold(g, w) {
				found = true
			}
		}
		if !found {
			t.Errorf("missing candidate %s in %v", w, got)
		}
	}
	for _, g := range got {
		if strings.HasSuffix(strings.ToLower(g), `\videolan`) {
			t.Errorf("publisher folder alone must not be a candidate: %s", g)
		}
	}
}

func TestKeyCandidates(t *testing.T) {
	p := Program{Name: "Toast POS Terminal", Publisher: "Toast, Inc.", Key: "{ABC-123}"}
	got := strings.Join(KeyCandidates(p), "\n")
	for _, w := range []string{`HKEY_CURRENT_USER\Software\Toast\Toast POS Terminal`, `HKEY_LOCAL_MACHINE\SOFTWARE\Toast POS Terminal`, `HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Toast\Toast POS Terminal`, `HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{ABC-123}`} {
		if !strings.Contains(got, w) {
			t.Errorf("missing key %s in:\n%s", w, got)
		}
	}
	if strings.Contains(got, `HKEY_CURRENT_USER\Software\Toast`+"\n") {
		t.Error("publisher key alone must not be offered")
	}
}

func TestSafeFolder(t *testing.T) {
	installed := []string{`C:\Users\j\AppData\Local\Programs\Python\Python312`, `C:\Program Files\Git`}
	if SafeFolder(`C:\Users\j\AppData\Local\Programs\Python`, installed) {
		t.Error("folder holding another installed program offered")
	}
	if SafeFolder(`C:\Program Files\Git`, installed) {
		t.Error("still-installed location offered")
	}
	if !SafeFolder(`C:\Program Files\7-Zip`, installed) {
		t.Error("plain leftover refused")
	}
	if SafeFolder(`C:\Program Files`, installed) || SafeFolder(`C:\`, installed) {
		t.Error("top level folder offered")
	}
}
