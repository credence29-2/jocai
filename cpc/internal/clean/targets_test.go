package clean

import (
	"strings"
	"testing"
)

func TestTargetsWellFormed(t *testing.T) {
	ids := map[string]bool{}
	for _, tg := range AllTargets() {
		if tg.ID == "" || tg.Name == "" || tg.Section == "" || tg.Description == "" {
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
