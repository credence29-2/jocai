// Package regclean finds registry entries that point at files which no
// longer exist, backs them up, and removes them.
//
// It is deliberately conservative. An entry is only reported when it names
// an absolute path on a local drive that is present, and that path is
// definitely missing. Network paths, unplugged drives, relative paths and
// anything with unexpanded variables are left alone. COM, ActiveX, type
// library and service entries are never touched, because removing those is
// how registry cleaners break Windows.
package regclean

import (
	"os"
	"regexp"
	"strings"
)

// Category is one checkbox on the Registry tab.
type Category struct {
	ID          string
	Name        string
	Description string
	NeedsAdmin  bool
}

// Categories in display order.
var Categories = []Category{
	{"apppaths", "Application paths", "Shortcuts Windows keeps to launch programs by name, for programs that were removed.", false},
	{"obsolete", "Obsolete software", "Apps & Features entries for programs whose uninstaller and folder are both gone.", true},
	{"startup", "Run at startup", "Startup entries that point to programs that no longer exist.", false},
	{"muicache", "MUI cache", "Names Windows remembered for programs that were removed.", false},
	{"shareddlls", "Missing shared DLLs", "Counters for shared library files that are no longer on disk.", true},
	{"installers", "Installer folders", "Records of install folders that no longer exist.", true},
	{"help", "Help files", "Links to help files that were removed.", false},
	{"fonts", "Fonts", "Font entries whose font file is gone.", false},
	{"firewall", "Firewall rules", "Windows Firewall rules for programs that were removed.", true},
	{"sounds", "Sound events", "Windows sounds set to audio files that no longer exist. The sound is set back to none.", false},
}

// Issue is one problem found by a scan.
type Issue struct {
	ID       int    `json:"id"`
	Category string `json:"category"`
	Key      string `json:"key"`   // full key path, e.g. HKEY_CURRENT_USER\Software\...
	Value    string `json:"value"` // value name, empty with WholeKey for the default value
	WholeKey bool   `json:"wholeKey"`
	// ClearValue sets the value to an empty string instead of deleting it.
	ClearValue bool   `json:"clearValue"`
	Problem    string `json:"problem"`
}

var envVar = regexp.MustCompile(`%([^%]+)%`)

// expand replaces %VAR% using the environment. ok is false if any variable
// is unknown.
func expand(s string) (string, bool) {
	ok := true
	out := envVar.ReplaceAllStringFunc(s, func(m string) string {
		v, found := os.LookupEnv(m[1 : len(m)-1])
		if !found || v == "" {
			ok = false
			return m
		}
		return v
	})
	return out, ok
}

// fs abstracts the file system so the rules can be tested anywhere.
type fs interface {
	Exists(path string) bool
	DriveExists(root string) bool
}

type realFS struct{}

func (realFS) Exists(p string) bool { _, err := os.Stat(p); return err == nil || !os.IsNotExist(err) }
func (realFS) DriveExists(root string) bool {
	_, err := os.Stat(root)
	return err == nil
}

// MissingPath reports whether raw names a local file that is definitely
// gone. It returns the cleaned path for display.
func MissingPath(raw string, f fs) (string, bool) {
	p := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), `"`))
	if p == "" || strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "@") {
		return p, false
	}
	p, ok := expand(p)
	if !ok {
		return p, false
	}
	// Needs a drive letter: X:\...
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return p, false
	}
	letter := p[0] | 0x20
	if letter < 'a' || letter > 'z' {
		return p, false
	}
	if !f.DriveExists(p[:3]) {
		return p, false
	}
	if strings.ContainsAny(p, `*?"<>|`) {
		return p, false
	}
	return p, !f.Exists(p)
}

// CommandExe pulls the program path out of a command line.
func CommandExe(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if end := strings.Index(cmd[1:], `"`); end >= 0 {
			return cmd[1 : end+1]
		}
		return strings.Trim(cmd, `"`)
	}
	lower := strings.ToLower(cmd)
	for _, ext := range []string{".exe", ".com", ".bat", ".cmd", ".lnk"} {
		if i := strings.Index(lower, ext); i >= 0 {
			return cmd[:i+len(ext)]
		}
	}
	if i := strings.IndexByte(cmd, ' '); i >= 0 {
		return cmd[:i]
	}
	return cmd
}

// isSystemLauncher is true for command lines run through Windows' own
// helpers, where the first word is not the real target.
func isSystemLauncher(exe string) bool {
	b := strings.ToLower(exe)
	if i := strings.LastIndexAny(b, `\/`); i >= 0 {
		b = b[i+1:]
	}
	switch b {
	case "rundll32.exe", "rundll32", "msiexec.exe", "msiexec", "cmd.exe", "cmd", "wscript.exe", "cscript.exe", "powershell.exe", "pwsh.exe", "explorer.exe", "regsvr32.exe":
		return true
	}
	return false
}

// MissingCommand reports whether a command line launches a program that is
// gone. Bare names like "notepad.exe" and Windows helpers are never flagged.
func MissingCommand(cmd string, f fs) (string, bool) {
	exe := CommandExe(cmd)
	if exe == "" || isSystemLauncher(exe) {
		return exe, false
	}
	return MissingPath(exe, f)
}

// FirewallApp returns the App= path from a firewall rule value.
func FirewallApp(rule string) string {
	for _, part := range strings.Split(rule, "|") {
		if strings.HasPrefix(strings.ToLower(part), "app=") {
			return part[4:]
		}
	}
	return ""
}

// MuiCacheFile returns the program path from a MuiCache value name such as
// "C:\Apps\tool.exe.FriendlyAppName".
func MuiCacheFile(name string) string {
	for _, suffix := range []string{".FriendlyAppName", ".ApplicationCompany"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return ""
}
