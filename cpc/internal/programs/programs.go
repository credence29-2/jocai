// Package programs lists installed software and launches uninstallers.
package programs

import (
	"regexp"
	"strings"
	"time"
)

// Program is one entry from Apps & Features.
type Program struct {
	Name        string
	Publisher   string
	Version     string
	SizeKB      int64
	InstallDate time.Time
	Uninstall   string
	Location    string
	Key         string
}

var msiModify = regexp.MustCompile(`(?i)^(\s*"?[^"]*msiexec(\.exe)?"?\s+)/I(\s*\{)`)

// UninstallCommand turns the registry uninstall string into the command to
// run. Many MSI packages register "MsiExec.exe /I{GUID}", which opens the
// repair/modify screen. We switch that to /X so it actually uninstalls.
func UninstallCommand(s string) string {
	return msiModify.ReplaceAllString(s, "${1}/X${3}")
}

// ParseInstallDate reads the YYYYMMDD format installers write.
func ParseInstallDate(s string) time.Time {
	s = strings.TrimSpace(s)
	if len(s) != 8 {
		return time.Time{}
	}
	t, err := time.Parse("20060102", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// Dedupe drops entries that show up in more than one registry view with the
// same name and version.
func Dedupe(in []Program) []Program {
	seen := map[string]bool{}
	var out []Program
	for _, p := range in {
		k := strings.ToLower(p.Name + "\x00" + p.Version)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}
