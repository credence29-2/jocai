// Package install puts Kempt into the user's programs folder with Start
// menu and desktop shortcuts and an Add/Remove Programs entry, so updates
// have one place to land and the exe stops living on the Desktop.
package install

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// App names used for the folder, shortcuts and registry entry.
const (
	AppName   = "Kempt"
	Publisher = "804 Industries"
	ExeName   = "Kempt.exe"
	RegKey    = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Kempt`
)

// Info describes the installed copy, if any.
type Info struct {
	Installed bool   `json:"installed"`
	Dir       string `json:"dir"`
	Exe       string `json:"exe"`
	Version   string `json:"version"`
	// ThisCopy is true when the running exe is the installed one.
	ThisCopy bool `json:"thisCopy"`
	// Portable is where the running exe lives when it is not installed.
	Portable string `json:"portable"`
}

// Dir is the per-user programs folder, which needs no administrator rights
// and is where Windows expects apps that update themselves.
func Dir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserConfigDir()
	}
	return filepath.Join(base, "Programs", AppName)
}

// Exe is the installed exe's path.
func Exe() string { return filepath.Join(Dir(), ExeName) }

// IsInstalledCopy reports whether exe is the installed one.
func IsInstalledCopy(exe string) bool {
	return SamePath(exe, Exe())
}

// SamePath compares two Windows paths loosely.
func SamePath(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// Removable reports whether a portable copy sits somewhere it is safe to
// delete after installing: the Desktop or Downloads.
func Removable(exe string) bool {
	home, _ := os.UserHomeDir()
	if home == "" {
		return false
	}
	dir := strings.ToLower(filepath.Dir(filepath.Clean(exe)))
	for _, d := range []string{"Desktop", "Downloads"} {
		if dir == strings.ToLower(filepath.Join(home, d)) {
			return true
		}
	}
	if od := os.Getenv("OneDrive"); od != "" && dir == strings.ToLower(filepath.Join(od, "Desktop")) {
		return true
	}
	return false
}

// copyFile copies src to dst, replacing a running exe by renaming it aside.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if _, err := os.Stat(dst); err == nil {
		old := dst + ".old"
		os.Remove(old)
		if err := os.Rename(dst, old); err != nil {
			return errors.New("could not replace the installed copy: " + err.Error())
		}
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ErrNotWindows is returned off Windows.
var ErrNotWindows = errors.New("only supported on Windows")

// RemoveOld deletes the exe left aside by an update or reinstall.
func RemoveOld() {
	old := Exe() + ".old"
	for i := 0; i < 5; i++ {
		if err := os.Remove(old); err == nil || os.IsNotExist(err) {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
}
