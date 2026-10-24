// Package settings stores Kempt's preferences and cleaning logs under
// %LOCALAPPDATA%\Kempt.
package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Settings is what Kempt remembers between runs.
type Settings struct {
	// Selected maps target IDs to whether they are ticked. Targets not in the
	// map fall back to their default.
	Selected map[string]bool `json:"selected"`
	// Exclude lists folders the cleaner must never touch.
	Exclude []string `json:"exclude,omitempty"`
}

// Dir returns Kempt's data folder, creating it if needed.
func Dir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	d := filepath.Join(base, "Kempt")
	// Earlier versions used older product names. Carry the folder over so
	// saved choices, logs and registry backups are kept.
	if _, err := os.Stat(d); os.IsNotExist(err) {
		for _, name := range []string{"ClearPCCleaner", "CredencePCCleaner"} {
			old := filepath.Join(base, name)
			if fi, err := os.Stat(old); err == nil && fi.IsDir() {
				_ = os.Rename(old, d)
				break
			}
		}
	}
	_ = os.MkdirAll(d, 0o755)
	return d
}

func path(dir string) string { return filepath.Join(dir, "settings.json") }

// Load reads settings from dir. A missing or broken file gives defaults.
func Load(dir string) Settings {
	s := Settings{Selected: map[string]bool{}}
	b, err := os.ReadFile(path(dir))
	if err == nil {
		_ = json.Unmarshal(b, &s)
	}
	if s.Selected == nil {
		s.Selected = map[string]bool{}
	}
	return s
}

// Save writes settings to dir atomically.
func Save(dir string, s Settings) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path(dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path(dir))
}

// IsSelected applies the stored choice or the target default.
func (s Settings) IsSelected(id string, def bool) bool {
	if v, ok := s.Selected[id]; ok {
		return v
	}
	return def
}

// Log is a cleaning log file.
type Log struct {
	f    *os.File
	Path string
}

// keepLogs is how many old logs are kept.
const keepLogs = 30

// NewLog creates a new timestamped log in dir\logs and prunes old ones.
func NewLog(dir string, now time.Time) (*Log, error) {
	ld := filepath.Join(dir, "logs")
	if err := os.MkdirAll(ld, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(ld, "clean-"+now.Format("20060102-150405")+".log")
	f, err := os.Create(p)
	if err != nil {
		return nil, err
	}
	prune(ld)
	return &Log{f: f, Path: p}, nil
}

func prune(ld string) {
	entries, err := os.ReadDir(ld)
	if err != nil {
		return
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "clean-") && strings.HasSuffix(e.Name(), ".log") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for len(names) > keepLogs {
		_ = os.Remove(filepath.Join(ld, names[0]))
		names = names[1:]
	}
}

// Printf writes a line to the log. Safe to call on a nil log.
func (l *Log) Printf(format string, args ...any) {
	if l == nil {
		return
	}
	fmt.Fprintf(l.f, format+"\r\n", args...)
}

// Close closes the log. Safe on nil.
func (l *Log) Close() {
	if l != nil {
		l.f.Close()
	}
}
