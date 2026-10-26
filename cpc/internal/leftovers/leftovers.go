// Package leftovers finds what an uninstaller left behind: folders named
// after the program and its registry keys. Detection is deliberately narrow.
// Only exact name matches are offered, never anything another installed
// program still lives in.
package leftovers

import (
	"regexp"
	"sort"
	"strings"
)

// Paths are Windows paths whatever the build host, so joining and cleaning
// are done by hand rather than with path/filepath.
func join(parts ...string) string {
	var out []string
	for _, p := range parts {
		p = strings.Trim(p, `\/`)
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, `\`)
}

func norm(p string) string {
	return strings.ToLower(strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(p), "/", `\`), `\`))
}

func isAbs(p string) bool {
	return len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/')
}

// Program is a snapshot taken before the uninstaller ran.
type Program struct {
	Name      string `json:"name"`
	Publisher string `json:"publisher"`
	Location  string `json:"location"`
	Key       string `json:"key"`
}

// Item is one folder or registry key that is still there.
type Item struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Files int    `json:"files"`
	Why   string `json:"why"`
}

// Result is what was left behind by one program.
type Result struct {
	Program string `json:"program"`
	Folders []Item `json:"folders"`
	Keys    []Item `json:"keys"`
	Bytes   int64  `json:"bytes"`
}

var (
	paren    = regexp.MustCompile(`\s*\([^)]*\)`)
	version  = regexp.MustCompile(`(?i)\b(v|ver\.?|version)?\s*\d+(\.\d+)+[a-z0-9.-]*\b`)
	yearSpan = regexp.MustCompile(`\b(19|20)\d\d(\s*-\s*(19|20)\d\d)?\b`)
	arch     = regexp.MustCompile(`(?i)\b(x64|x86|64-bit|32-bit|amd64|arm64|win64|win32)\b`)
	spaces   = regexp.MustCompile(`\s+`)
	trailing = regexp.MustCompile(`[\s\-_,.:;]+$`)
)

// CleanName strips version numbers, architecture tags and bracketed notes:
// "7-Zip 24.08 (x64)" becomes "7-Zip".
func CleanName(name string) string {
	s := paren.ReplaceAllString(name, "")
	s = version.ReplaceAllString(s, " ")
	s = yearSpan.ReplaceAllString(s, " ")
	s = arch.ReplaceAllString(s, " ")
	s = spaces.ReplaceAllString(s, " ")
	s = trailing.ReplaceAllString(strings.TrimSpace(s), "")
	return strings.TrimSpace(s)
}

var publisherSuffix = regexp.MustCompile(`(?i)[\s,]*\b(inc|incorporated|llc|ltd|limited|corp|corporation|co|gmbh|ag|s\.?a\.?|plc|pty|b\.?v\.?|srl|the)\b\.?[\s,]*$`)

// PublisherName trims legal suffixes: "Toast, Inc." becomes "Toast".
func PublisherName(pub string) string {
	s := strings.TrimSpace(pub)
	for i := 0; i < 3; i++ {
		s = publisherSuffix.ReplaceAllString(s, "")
	}
	s = trailing.ReplaceAllString(strings.TrimSpace(s), "")
	return s
}

// generic names are never matched on their own; too many unrelated things
// share them.
var generic = map[string]bool{
	"microsoft": true, "windows": true, "google": true, "apple": true, "intel": true, "nvidia": true, "amd": true, "adobe": true,
	"common files": true, "common": true, "programs": true, "program": true, "packages": true, "package": true, "temp": true, "tmp": true, "cache": true,
	"update": true, "updates": true, "updater": true, "setup": true, "install": true, "installer": true, "app": true, "apps": true, "application": true,
	"applications": true, "software": true, "tools": true, "tool": true, "system": true, "runtime": true, "redistributable": true, "driver": true,
	"drivers": true, "client": true, "server": true, "service": true, "services": true, "framework": true, "library": true, "sdk": true, "data": true,
	"local": true, "roaming": true, "default": true, "user": true, "users": true, "public": true, "shared": true, "classes": true, "wow6432node": true,
	"policies": true, "clients": true, "office": true, "visual studio": true, "visual c++": true, "net": true, ".net": true, "java": true, "python": true,
	"node": true, "nodejs": true, "git": false, "edge": false,
}

// Generic reports whether a name is too common to identify one program.
func Generic(s string) bool {
	return generic[strings.ToLower(strings.TrimSpace(s))]
}

func usable(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 3 || Generic(s) || strings.ContainsAny(s, `\/:*?"<>|`) {
		return false
	}
	return true
}

// Names returns the folder and key names a program is likely to have used:
// the cleaned display name, and that name without a leading publisher word
// ("Google Chrome" by Google also matches "Chrome").
func Names(name, publisher string) []string {
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if !usable(s) {
			return
		}
		for _, o := range out {
			if strings.EqualFold(o, s) {
				return
			}
		}
		out = append(out, s)
	}
	cn := CleanName(name)
	add(cn)
	add(name)
	pub := PublisherName(publisher)
	if pub != "" && len(cn) > len(pub)+1 && strings.HasPrefix(strings.ToLower(cn), strings.ToLower(pub)+" ") {
		add(cn[len(pub)+1:])
	}
	if f := strings.Fields(cn); len(f) > 1 && pub != "" && strings.EqualFold(f[0], pub) {
		add(strings.Join(f[1:], " "))
	}
	return out
}

// FolderCandidates lists folders to check under the given base folders:
// base\Name and base\Publisher\Name for every usable name, plus the
// recorded install location. First word of the name is tried only under
// the publisher folder ("VideoLAN\VLC" for "VLC media player").
func FolderCandidates(p Program, bases []string) []string {
	names := Names(p.Name, p.Publisher)
	pub := PublisherName(p.Publisher)
	var firsts []string
	if cn := CleanName(p.Name); cn != "" {
		if f := strings.Fields(cn); len(f) > 1 && usable(f[0]) && !strings.EqualFold(f[0], pub) {
			firsts = append(firsts, f[0])
		}
	}
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		k := norm(p)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(p), "/", `\`), `\`))
	}
	if isAbs(p.Location) {
		add(p.Location)
	}
	for _, b := range bases {
		if b == "" {
			continue
		}
		for _, n := range names {
			add(join(b, n))
			if usable(pub) {
				add(join(b, pub, n))
			}
		}
		if usable(pub) {
			for _, f := range firsts {
				add(join(b, pub, f))
			}
		}
	}
	return out
}

// KeyCandidates lists registry keys a program usually creates. Keys are in
// reg.exe form. The uninstall entry itself is included when its key name is
// known, since some uninstallers leave it behind.
func KeyCandidates(p Program) []string {
	names := Names(p.Name, p.Publisher)
	pub := PublisherName(p.Publisher)
	var out []string
	seen := map[string]bool{}
	add := func(k string) {
		if seen[strings.ToLower(k)] {
			return
		}
		seen[strings.ToLower(k)] = true
		out = append(out, k)
	}
	for _, hive := range []string{`HKEY_CURRENT_USER\Software`, `HKEY_LOCAL_MACHINE\SOFTWARE`, `HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node`} {
		for _, n := range names {
			if usable(pub) {
				add(hive + `\` + pub + `\` + n)
			}
			add(hive + `\` + n)
		}
	}
	if p.Key != "" && !strings.ContainsAny(p.Key, `\/`) {
		for _, hive := range []string{`HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, `HKEY_LOCAL_MACHINE\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Uninstall`} {
			add(hive + `\` + p.Key)
		}
	}
	return out
}

// SafeFolder reports whether a leftover folder may be offered: it must sit
// at least two levels below the drive and not contain or equal a folder
// another installed program still uses.
func SafeFolder(path string, installed []string) bool {
	c := norm(path)
	if !isAbs(c) {
		return false
	}
	rest := strings.Trim(c[2:], `\`)
	if rest == "" || len(strings.Split(rest, `\`)) < 2 {
		return false
	}
	for _, loc := range installed {
		l := norm(loc)
		if l == "" {
			continue
		}
		if l == c || strings.HasPrefix(l, c+`\`) {
			return false
		}
	}
	return true
}

// Sorted returns items biggest first.
func Sorted(items []Item) []Item {
	out := append([]Item(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}
