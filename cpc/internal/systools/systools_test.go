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
	if got[0] != (Update{"Google Chrome", "Google.Chrome", "130.0.6723.70", "131.0.6778.86"}) {
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
