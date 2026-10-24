package systools

import (
	"fmt"
	"strings"
	"testing"
)

func row(a, b, c, d, e string) string {
	return fmt.Sprintf("%-35s%-28s%-16s%-16s%s\r\n", a, b, c, d, e)
}

var sample = "\r   - \r   \\ \r\n" +
	row("Name", "Id", "Version", "Available", "Source") +
	strings.Repeat("-", 100) + "\r\n" +
	row("Google Chrome", "Google.Chrome", "130.0.6723.70", "131.0.6778.86", "winget") +
	row("Zoom Workplace", "Zoom.Zoom", "6.2.5.4567", "6.3.0.5527", "winget") +
	row("Café Notes ünïcode", "Example.Cafe", "1.0", "1.1", "winget") +
	"3 upgrades available.\r\n"

func TestParseWinget(t *testing.T) {
	got := ParseWinget(sample)
	if len(got) != 3 {
		t.Fatalf("got %d: %+v", len(got), got)
	}
	if got[0] != (Update{Name: "Google Chrome", ID: "Google.Chrome", Version: "130.0.6723.70", Available: "131.0.6778.86"}) {
		t.Errorf("row 0 = %+v", got[0])
	}
	if got[2].ID != "Example.Cafe" || got[2].Name != "Café Notes ünïcode" || got[2].Available != "1.1" {
		t.Errorf("unicode row = %+v", got[2])
	}
}

func TestParseWingetLocalizedAndEmpty(t *testing.T) {
	de := "Name           ID              Version  Verfügbar  Quelle\n" +
		"---------------------------------------------------------\n" +
		"7-Zip          7zip.7zip       23.01    24.08      winget\n"
	if got := ParseWinget(de); len(got) != 1 || got[0].ID != "7zip.7zip" || got[0].Available != "24.08" {
		t.Fatalf("german table: %+v", got)
	}
	if got := ParseWinget("No installed package found matching input criteria."); got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestScModeAndProblems(t *testing.T) {
	if v, ok := ScMode("Manual"); !ok || v != "demand" {
		t.Error("manual")
	}
	if _, ok := ScMode("boot"); ok {
		t.Error("boot must not be allowed")
	}
	if ProblemText(28) == ProblemText(9999) {
		t.Error("code 28 should have its own text")
	}
}

func TestDriverGroups(t *testing.T) {
	cases := map[string]string{"MEDIA": "Audio", "Bluetooth": "Bluetooth", "Net": "Connectivity", "Display": "Graphics", "SoftwareComponent": "System and chipset", "Weird": "Other"}
	for in, want := range cases {
		if got := DriverGroup(in); got != want {
			t.Errorf("DriverGroup(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchUpdates(t *testing.T) {
	ups := []DriverUpdate{{ID: "a", HardwareID: `PCI\VEN_10EC&DEV_8168`}, {ID: "b", HardwareID: `USB\VID_8087&PID_0AA7`}}
	inst := []Driver{
		{Device: "Realtek PCIe GbE", Version: "10.38", Date: "2019-11-28", HardwareID: `PCI\VEN_10EC&DEV_8168&SUBSYS_86771043&REV_15`},
		{Device: "Intel Wireless Bluetooth", Version: "22.1", Date: "2022-01-01", HardwareID: `usb\vid_8087&pid_0aa7`},
		{Device: "Something else", HardwareID: `PCI\VEN_10EC&DEV_81680`},
	}
	MatchUpdates(ups, inst)
	if ups[0].CurrentVersion != "10.38" || ups[1].CurrentVersion != "22.1" {
		t.Fatalf("current versions = %q, %q", ups[0].CurrentVersion, ups[1].CurrentVersion)
	}
	if !inst[0].HasUpdate || !inst[1].HasUpdate || inst[2].HasUpdate {
		t.Fatalf("HasUpdate flags wrong: %+v", inst)
	}
}

func TestMakerLinks(t *testing.T) {
	links := MakerLinks("LENOVO", []Driver{{Manufacturer: "Intel Corporation"}, {Provider: "Realtek Semiconductor Corp."}, {Manufacturer: "Microsoft"}, {Manufacturer: "Shpaint"}})
	got := map[string]bool{}
	for _, l := range links {
		got[l.Key] = true
	}
	if !got["lenovo"] || !got["intel"] || !got["realtek"] || got["hp"] || got["amd"] {
		t.Fatalf("links = %+v", links)
	}
	if u, ok := MakerURL("intel"); !ok || u == "" {
		t.Fatal("intel url missing")
	}
	if _, ok := MakerURL("evil"); ok {
		t.Fatal("unknown key accepted")
	}
}

func TestValidUpdateID(t *testing.T) {
	if !ValidUpdateID("3f1c3a2e-1d2b-4c5d-9e8f-0a1b2c3d4e5f") || ValidUpdateID("x' or 1=1") || ValidUpdateID("") {
		t.Fatal("ValidUpdateID wrong")
	}
}

func TestWingetErrorText(t *testing.T) {
	cases := map[string]string{
		"Found Microsoft Edge [Microsoft.Edge]\nInstaller failed with exit code: 1603":                      "The app's installer failed (1603). Close the app and try again, or update it from inside the app.",
		"Installer hash does not match; this cannot be overridden when running as admin":                    "The downloaded installer didn't match what winget expected. Try again later; the package may be mid-update.",
		"No available upgrade found.\nNo newer package versions are available from the configured sources.": "Already up to date according to winget.",
		"   - \r   \\ \r\nSomething odd happened\nInstaller failed with exit code: 2":                       "Installer failed with exit code: 2",
		"":      "winget reported a failure without details.",
		"-\\|/": "winget reported a failure without details.",
	}
	for in, want := range cases {
		if got := WingetErrorText(in); got != want {
			t.Errorf("WingetErrorText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSelfUpdating(t *testing.T) {
	list := []Update{{ID: "Microsoft.Edge"}, {ID: "Google.Chrome"}, {ID: "Microsoft.EdgeWebView2Runtime"}}
	MarkSelfUpdating(list)
	if !list[0].SelfUpdate || list[1].SelfUpdate || !list[2].SelfUpdate {
		t.Fatalf("flags wrong: %+v", list)
	}
	if SelfUpdater("microsoft.edge") != "edge" || SelfUpdater("7zip.7zip") != "" {
		t.Fatal("SelfUpdater wrong")
	}
}
