package startup

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const approvedBase = `Software\Microsoft\Windows\CurrentVersion\Explorer\StartupApproved\`

type source struct {
	root        registry.Key
	runKey      string // empty for startup folders
	folder      string
	approvedKey string
}

func sources() []struct {
	label string
	admin bool
	src   source
} {
	return []struct {
		label string
		admin bool
		src   source
	}{
		{"Registry (this user)", false, source{root: registry.CURRENT_USER, runKey: `Software\Microsoft\Windows\CurrentVersion\Run`, approvedKey: approvedBase + "Run"}},
		{"Registry (all users)", true, source{root: registry.LOCAL_MACHINE, runKey: `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, approvedKey: approvedBase + "Run"}},
		{"Registry (all users, 32-bit)", true, source{root: registry.LOCAL_MACHINE, runKey: `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Run`, approvedKey: approvedBase + "Run32"}},
		{"Startup folder (this user)", false, source{root: registry.CURRENT_USER, folder: os.ExpandEnv(`${APPDATA}\Microsoft\Windows\Start Menu\Programs\Startup`), approvedKey: approvedBase + "StartupFolder"}},
		{"Startup folder (all users)", true, source{root: registry.LOCAL_MACHINE, folder: os.ExpandEnv(`${ProgramData}\Microsoft\Windows\Start Menu\Programs\Startup`), approvedKey: approvedBase + "StartupFolder"}},
	}
}

func approvedData(s source, name string) []byte {
	k, err := registry.OpenKey(s.root, s.approvedKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return nil
	}
	defer k.Close()
	b, _, err := k.GetBinaryValue(name)
	if err != nil {
		return nil
	}
	return b
}

// List returns every startup entry CPC knows about.
func List() []Entry {
	var out []Entry
	for _, s := range sources() {
		if s.src.runKey != "" {
			k, err := registry.OpenKey(s.src.root, s.src.runKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
			if err != nil {
				continue
			}
			names, _ := k.ReadValueNames(-1)
			for _, n := range names {
				if n == "" {
					continue
				}
				cmd, _, err := k.GetStringValue(n)
				if err != nil {
					continue
				}
				out = append(out, Entry{Name: n, Command: cmd, Location: s.label, NeedsAdmin: s.admin,
					Enabled: !IsDisabled(approvedData(s.src, n)), src: s.src})
			}
			k.Close()
			continue
		}
		files, err := os.ReadDir(s.src.folder)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || strings.EqualFold(f.Name(), "desktop.ini") {
				continue
			}
			out = append(out, Entry{Name: f.Name(), Command: filepath.Join(s.src.folder, f.Name()), Location: s.label,
				NeedsAdmin: s.admin, Enabled: !IsDisabled(approvedData(s.src, f.Name())), src: s.src})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

// SetEnabled turns a startup entry on or off.
func SetEnabled(e Entry, enabled bool) error {
	k, _, err := registry.CreateKey(e.src.root, e.src.approvedKey, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer k.Close()
	return k.SetBinaryValue(e.Name, ApprovedValue(enabled, time.Now()))
}

// Target returns the file the entry launches, best effort, for "open location".
func Target(e Entry) string {
	if e.src.folder != "" {
		return e.Command
	}
	return ExePath(e.Command)
}
