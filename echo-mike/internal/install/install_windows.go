// Package install puts Project Echo Mike where Windows expects a program:
// one exe under %LOCALAPPDATA%\Programs, a Start menu shortcut, and an
// entry in Apps & features. The updater replaces that one exe.
package install

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

const (
	name    = "Project Echo Mike"
	exeName = "ProjectEchoMike.exe"
	regKey  = `Software\Microsoft\Windows\CurrentVersion\Uninstall\ProjectEchoMike`
)

// Dir is where the installed program lives.
func Dir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base, _ = os.UserCacheDir()
	}
	return filepath.Join(base, "Programs", name)
}

// Exe is the installed program's path.
func Exe() string { return filepath.Join(Dir(), exeName) }

// Installed reports whether exe is the installed copy.
func Installed(exe string) bool {
	return strings.EqualFold(filepath.Clean(exe), filepath.Clean(Exe()))
}

// Install copies exe into Dir, makes the shortcuts and registers the
// program. It returns the installed exe's path.
func Install(exe, version string) (string, error) {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return "", err
	}
	dst := Exe()
	if !Installed(exe) {
		// An older installed copy may be running; move it aside first.
		os.Remove(dst + ".old")
		os.Rename(dst, dst+".old")
		if err := copyFile(exe, dst); err != nil {
			os.Rename(dst+".old", dst)
			return "", fmt.Errorf("couldn't copy the program to %s: %w", Dir(), err)
		}
	}
	if err := shortcut(startMenuLink(), dst); err != nil {
		return dst, fmt.Errorf("installed, but couldn't make the Start menu shortcut: %w", err)
	}
	if _, err := os.Stat(desktopLink()); err == nil || !os.IsNotExist(err) {
		shortcut(desktopLink(), dst) // refresh an existing desktop shortcut only
	} else {
		shortcut(desktopLink(), dst)
	}
	return dst, Register(dst, version)
}

// Register writes the Apps & features entry. The updater calls it after
// each update so the version shown there stays right.
func Register(exe, version string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, regKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	var size int64
	if st, err := os.Stat(exe); err == nil {
		size = st.Size()
	}
	for key, val := range map[string]string{
		"DisplayName":     name,
		"DisplayVersion":  strings.TrimPrefix(version, "v"),
		"DisplayIcon":     exe,
		"Publisher":       "Credence",
		"InstallLocation": filepath.Dir(exe),
		"UninstallString": `"` + exe + `" -uninstall`,
		"URLInfoAbout":    "https://github.com/credence29-2/jocai",
	} {
		if err := k.SetStringValue(key, val); err != nil {
			return err
		}
	}
	k.SetDWordValue("NoModify", 1)
	k.SetDWordValue("NoRepair", 1)
	k.SetDWordValue("EstimatedSize", uint32(size/1024))
	return nil
}

// Uninstall removes the shortcuts and the Apps & features entry, then
// deletes the program folder once this process has exited. keepData
// leaves the settings and sign-ins alone.
func Uninstall(keepData bool, dataDir string) error {
	os.Remove(startMenuLink())
	os.Remove(desktopLink())
	registry.DeleteKey(registry.CURRENT_USER, regKey)
	if !keepData && dataDir != "" {
		os.RemoveAll(dataDir)
	}
	// cmd waits a moment for the exe to exit, then removes the folder.
	script := fmt.Sprintf(`ping 127.0.0.1 -n 3 > nul & rmdir /s /q "%s"`, Dir())
	cmd := exec.Command("cmd", "/c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008} // DETACHED_PROCESS
	return cmd.Start()
}

func startMenuLink() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", name+".lnk")
}

func desktopLink() string {
	home, _ := os.UserHomeDir()
	if d := os.Getenv("USERPROFILE"); d != "" {
		home = d
	}
	return filepath.Join(home, "Desktop", name+".lnk")
}

// shortcut writes a .lnk through the Windows Script Host, which saves
// talking COM from Go.
func shortcut(link, target string) error {
	os.MkdirAll(filepath.Dir(link), 0o755)
	ps := fmt.Sprintf(`$s=(New-Object -ComObject WScript.Shell).CreateShortcut('%s'); $s.TargetPath='%s'; $s.WorkingDirectory='%s'; $s.IconLocation='%s,0'; $s.Description='%s'; $s.Save()`,
		psq(link), psq(target), psq(filepath.Dir(target)), psq(target), name)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", ps)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func psq(s string) string { return strings.ReplaceAll(s, "'", "''") }

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
