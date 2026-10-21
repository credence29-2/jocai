package regclean

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

type root struct {
	key  registry.Key
	name string
}

var (
	hkcu = root{registry.CURRENT_USER, "HKEY_CURRENT_USER"}
	hklm = root{registry.LOCAL_MACHINE, "HKEY_LOCAL_MACHINE"}
)

const access = registry.QUERY_VALUE | registry.ENUMERATE_SUB_KEYS | registry.WOW64_64KEY

type scanner struct {
	f        fs
	elevated bool
	out      []Issue
}

func (s *scanner) add(cat, key, value string, whole, clear bool, problem string) {
	s.out = append(s.out, Issue{ID: len(s.out), Category: cat, Key: key, Value: value, WholeKey: whole, ClearValue: clear, Problem: problem})
}

func subkeys(r root, path string) []string {
	k, err := registry.OpenKey(r.key, path, access)
	if err != nil {
		return nil
	}
	defer k.Close()
	n, _ := k.ReadSubKeyNames(-1)
	return n
}

// eachString calls fn for every string value in a key.
func eachString(r root, path string, fn func(name, data string)) {
	k, err := registry.OpenKey(r.key, path, access)
	if err != nil {
		return
	}
	defer k.Close()
	names, _ := k.ReadValueNames(-1)
	for _, n := range names {
		v, _, err := k.GetStringValue(n)
		if err == nil {
			fn(n, v)
		}
	}
}

func valueNames(r root, path string) []string {
	k, err := registry.OpenKey(r.key, path, access)
	if err != nil {
		return nil
	}
	defer k.Close()
	n, _ := k.ReadValueNames(-1)
	return n
}

func getString(r root, path, name string) string {
	k, err := registry.OpenKey(r.key, path, access)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, _ := k.GetStringValue(name)
	return v
}

func full(r root, path string) string { return r.name + `\` + path }

// Scan looks for issues in the chosen categories. HKLM categories are
// skipped when not elevated, since they could not be fixed anyway.
func Scan(cats []string, elevated bool) []Issue {
	s := &scanner{f: realFS{}, elevated: elevated}
	want := map[string]bool{}
	for _, c := range cats {
		want[c] = true
	}
	machine := func() []root {
		if elevated {
			return []root{hkcu, hklm}
		}
		return []root{hkcu}
	}

	if want["apppaths"] {
		for _, r := range machine() {
			for _, base := range []string{`SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\App Paths`} {
				for _, sk := range subkeys(r, base) {
					p := base + `\` + sk
					if path, missing := MissingPath(getString(r, p, ""), s.f); missing && !UnderWindows(path) {
						s.add("apppaths", full(r, p), "", true, false, "Points to missing "+path)
					}
				}
			}
		}
	}
	if want["obsolete"] && elevated {
		for _, r := range []root{hklm, hkcu} {
			for _, base := range []string{`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`} {
				for _, sk := range subkeys(r, base) {
					p := base + `\` + sk
					name := getString(r, p, "DisplayName")
					un := getString(r, p, "UninstallString")
					if name == "" || un == "" {
						continue
					}
					exe, missing := MissingCommand(un, s.f)
					if !missing {
						continue
					}
					// Only when the install folder is gone too.
					if loc := getString(r, p, "InstallLocation"); loc != "" {
						if _, locMissing := MissingPath(loc, s.f); !locMissing {
							continue
						}
					}
					s.add("obsolete", full(r, p), "", true, false, fmt.Sprintf("%s: uninstaller %s is gone", name, exe))
				}
			}
		}
	}
	if want["startup"] {
		runKeys := []struct {
			r    root
			path string
		}{{hkcu, `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`}}
		if elevated {
			runKeys = append(runKeys,
				struct {
					r    root
					path string
				}{hklm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`},
				struct {
					r    root
					path string
				}{hklm, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`})
		}
		for _, rk := range runKeys {
			eachString(rk.r, rk.path, func(name, data string) {
				if exe, missing := MissingCommand(data, s.f); missing {
					s.add("startup", full(rk.r, rk.path), name, false, false, name+": "+exe+" is gone")
				}
			})
		}
	}
	if want["muicache"] {
		p := `Software\Classes\Local Settings\Software\Microsoft\Windows\Shell\MuiCache`
		for _, n := range valueNames(hkcu, p) {
			if file := MuiCacheFile(n); file != "" {
				if path, missing := MissingPath(file, s.f); missing {
					s.add("muicache", full(hkcu, p), n, false, false, path+" is gone")
				}
			}
		}
	}
	if want["shareddlls"] && elevated {
		for _, p := range []string{`SOFTWARE\Microsoft\Windows\CurrentVersion\SharedDLLs`, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\SharedDLLs`} {
			for _, n := range valueNames(hklm, p) {
				if path, missing := MissingPath(n, s.f); missing && !UnderWindows(path) {
					s.add("shareddlls", full(hklm, p), n, false, false, path+" is gone")
				}
			}
		}
	}
	if want["installers"] && elevated {
		p := `SOFTWARE\Microsoft\Windows\CurrentVersion\Installer\Folders`
		for _, n := range valueNames(hklm, p) {
			if path, missing := MissingPath(n, s.f); missing {
				s.add("installers", full(hklm, p), n, false, false, path+" no longer exists")
			}
		}
	}
	if want["help"] {
		for _, r := range machine() {
			for _, p := range []string{`SOFTWARE\Microsoft\Windows\HTML Help`, `SOFTWARE\Microsoft\Windows\Help`} {
				eachString(r, p, func(name, data string) {
					if name == "" || data == "" {
						return
					}
					if path, missing := MissingPath(filepath.Join(strings.TrimSpace(data), name), s.f); missing && !UnderWindows(path) {
						s.add("help", full(r, p), name, false, false, path+" is gone")
					}
				})
			}
		}
	}
	if want["fonts"] {
		fontDir := filepath.Join(os.Getenv("SystemRoot"), "Fonts")
		for _, r := range machine() {
			p := `SOFTWARE\Microsoft\Windows NT\CurrentVersion\Fonts`
			eachString(r, p, func(name, data string) {
				file := data
				if !strings.Contains(file, `\`) {
					if os.Getenv("SystemRoot") == "" {
						return
					}
					file = filepath.Join(fontDir, file)
				}
				if path, missing := MissingPath(file, s.f); missing {
					s.add("fonts", full(r, p), name, false, false, path+" is gone")
				}
			})
		}
	}
	if want["firewall"] && elevated {
		p := `SYSTEM\CurrentControlSet\Services\SharedAccess\Parameters\FirewallPolicy\FirewallRules`
		eachString(hklm, p, func(name, data string) {
			if app := FirewallApp(data); app != "" && !strings.EqualFold(app, "system") {
				if path, missing := MissingPath(app, s.f); missing && !UnderWindows(path) {
					s.add("firewall", full(hklm, p), name, false, false, "Rule for "+path+", which is gone")
				}
			}
		})
	}
	if want["sounds"] {
		base := `AppEvents\Schemes\Apps`
		for _, a := range subkeys(hkcu, base) {
			for _, e := range subkeys(hkcu, base+`\`+a) {
				p := base + `\` + a + `\` + e + `\.Current`
				if path, missing := MissingPath(getString(hkcu, p, ""), s.f); missing {
					s.add("sounds", full(hkcu, p), "", false, true, e+": "+path+" is gone")
				}
			}
		}
	}
	return s.out
}

func runReg(args ...string) error {
	cmd := exec.Command("reg.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errors.New(strings.TrimSpace(string(out)))
	}
	return nil
}

// Fix backs up every affected key into a new folder under backupRoot, then
// removes the issues. It returns the backup folder.
func Fix(issues []Issue, backupRoot string) (fixed, failed int, backup string, err error) {
	backup = filepath.Join(backupRoot, "registry-"+time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return 0, 0, "", err
	}
	// Export each distinct key once, before changing anything.
	keys := map[string]bool{}
	for _, is := range issues {
		keys[is.Key] = true
	}
	var sorted []string
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	var manifest []string
	for i, k := range sorted {
		file := filepath.Join(backup, fmt.Sprintf("%03d.reg", i+1))
		if err := runReg("export", k, file, "/y"); err != nil {
			return 0, 0, backup, fmt.Errorf("backup of %s failed, nothing was changed: %v", k, err)
		}
		manifest = append(manifest, fmt.Sprintf("%03d.reg\t%s", i+1, k))
	}
	_ = os.WriteFile(filepath.Join(backup, "keys.txt"), []byte(strings.Join(manifest, "\r\n")+"\r\n"), 0o644)

	var log []string
	for _, is := range issues {
		var e error
		switch {
		case is.WholeKey:
			e = runReg("delete", is.Key, "/f")
		case is.ClearValue:
			if is.Value == "" {
				e = runReg("add", is.Key, "/ve", "/d", "", "/f")
			} else {
				e = runReg("add", is.Key, "/v", is.Value, "/d", "", "/f")
			}
		case is.Value == "":
			e = runReg("delete", is.Key, "/ve", "/f")
		default:
			e = runReg("delete", is.Key, "/v", is.Value, "/f")
		}
		if e != nil {
			failed++
			log = append(log, "FAILED "+is.Key+" "+is.Value+": "+e.Error())
		} else {
			fixed++
			log = append(log, "fixed "+is.Key+" "+is.Value+" ("+is.Problem+")")
		}
	}
	_ = os.WriteFile(filepath.Join(backup, "changes.log"), []byte(strings.Join(log, "\r\n")+"\r\n"), 0o644)
	return fixed, failed, backup, nil
}

// Backup describes one saved registry backup.
type Backup struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	When  string `json:"when"`
	Count int    `json:"count"`
}

// Backups lists saved backups, newest first.
func Backups(backupRoot string) []Backup {
	entries, _ := os.ReadDir(backupRoot)
	var out []Backup
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "registry-") {
			continue
		}
		t, err := time.ParseInLocation("20060102-150405", strings.TrimPrefix(e.Name(), "registry-"), time.Local)
		if err != nil {
			continue
		}
		files, _ := filepath.Glob(filepath.Join(backupRoot, e.Name(), "*.reg"))
		out = append(out, Backup{Name: e.Name(), Path: filepath.Join(backupRoot, e.Name()), When: t.Format("Jan 2, 2006 3:04 PM"), Count: len(files)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out
}

// Restore imports every .reg file in a backup folder.
func Restore(backupRoot, name string) error {
	if strings.ContainsAny(name, `\/`) || !strings.HasPrefix(name, "registry-") {
		return errors.New("not a CPC backup")
	}
	files, _ := filepath.Glob(filepath.Join(backupRoot, name, "*.reg"))
	if len(files) == 0 {
		return errors.New("that backup is empty")
	}
	sort.Strings(files)
	for _, f := range files {
		if err := runReg("import", f); err != nil {
			return fmt.Errorf("restoring %s: %v", filepath.Base(f), err)
		}
	}
	return nil
}
