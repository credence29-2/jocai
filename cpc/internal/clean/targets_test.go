package clean

import (
	"strings"
	"testing"
)

func TestTargetsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, tg := range AllTargets() {
		if tg.ID == "" || tg.Name == "" || tg.Section == "" || tg.Group == "" || tg.Description == "" {
			t.Errorf("incomplete target %+v", tg)
		}
		if ids[tg.ID] {
			t.Errorf("duplicate id %s", tg.ID)
		}
		ids[tg.ID] = true
		if (tg.Custom == nil) == (len(tg.Rules) == 0) {
			t.Errorf("%s must have rules or a custom handler, not both", tg.ID)
		}
		for _, r := range tg.Rules {
			if !strings.HasPrefix(r.Root, "%") {
				t.Errorf("%s root %q must start from an environment folder", tg.ID, r.Root)
			}
			if tg.NeedsAdmin && !r.Admin {
				t.Errorf("%s needs admin but rule %q is not marked admin", tg.ID, r.Root)
			}
		}
		for _, p := range tg.Processes {
			if p != strings.ToLower(p) {
				t.Errorf("%s process %q must be lower case", tg.ID, p)
			}
		}
	}
}

// Wide-open rules (recursive, every file) must never point at a folder that
// holds user data. This is a guard against typos in the target list.
func TestNoBroadRulesOnUserFolders(t *testing.T) {
	banned := []string{`%USERPROFILE%`, `%APPDATA%`, `%LOCALAPPDATA%`, `%ProgramData%`, `%SystemRoot%`}
	for _, tg := range AllTargets() {
		for _, r := range tg.Rules {
			for _, b := range banned {
				if strings.EqualFold(strings.TrimRight(r.Root, `\`), b) && len(r.Files) == 0 {
					t.Errorf("%s: rule on bare %s with no file filter", tg.ID, b)
				}
			}
		}
	}
}

func TestLabels(t *testing.T) {
	for _, tg := range AllTargets() {
		if tg.ID == "chrome.cache" && tg.Label() != "Internet cache" {
			t.Errorf("chrome cache label = %q", tg.Label())
		}
		if tg.ID == "chrome.cookies" && tg.Label() != "Cookies" {
			t.Errorf("chrome cookies label = %q", tg.Label())
		}
		if tg.ID == "win.recycle" && tg.Label() != "Recycle Bin" {
			t.Errorf("recycle label = %q", tg.Label())
		}
	}
}

func TestGroupsKnown(t *testing.T) {
	known := map[string]bool{}
	for _, g := range GroupOrder {
		known[g] = true
	}
	for _, tg := range AllTargets() {
		if !known[tg.Group] {
			t.Errorf("%s has group %q missing from GroupOrder", tg.ID, tg.Group)
		}
	}
}

// Risky items must never be ticked by default.
func TestRiskyItemsOffByDefault(t *testing.T) {
	for _, tg := range AllTargets() {
		for _, risky := range []string{"passwords", "cookies", "history", "session", "dl.", "win.old", "componentstore", "dumps"} {
			if strings.Contains(tg.ID, risky) && tg.Default {
				t.Errorf("%s is on by default", tg.ID)
			}
		}
	}
}
