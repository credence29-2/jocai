//go:build windows

package install

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// Current reads the installed copy from the registry and disk.
func Current(runningExe string) Info {
	info := Info{Dir: Dir(), Exe: Exe()}
	if _, err := os.Stat(info.Exe); err == nil {
		info.Installed = true
	}
	if k, err := registry.OpenKey(registry.CURRENT_USER, RegKey, registry.QUERY_VALUE); err == nil {
		info.Version, _, _ = k.GetStringValue("DisplayVersion")
		k.Close()
	}
	info.ThisCopy = IsInstalledCopy(runningExe)
	if !info.ThisCopy {
		info.Portable = runningExe
	}
	return info
}

func startMenuLink() string {
	return filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", AppName+".lnk")
}

func desktopLink() string {
	home, _ := os.UserHomeDir()
	d := filepath.Join(home, "Desktop")
	if od := os.Getenv("OneDrive"); od != "" {
		if fi, err := os.Stat(filepath.Join(od, "Desktop")); err == nil && fi.IsDir() {
			if _, err := os.Stat(d); err != nil {
				d = filepath.Join(od, "Desktop")
			}
		}
	}
	return filepath.Join(d, AppName+".lnk")
}

// Register writes the Add/Remove Programs entry for the installed exe.
func Register(version string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, RegKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	exe := Exe()
	size := uint32(0)
	if fi, err := os.Stat(exe); err == nil {
		size = uint32(fi.Size() / 1024)
	}
	for name, val := range map[string]string{
		"DisplayName":     AppName,
		"DisplayVersion":  version,
		"Publisher":       Publisher,
		"InstallLocation": Dir(),
		"DisplayIcon":     exe,
		"UninstallString": `"` + exe + `" /uninstall`,
		"InstallDate":     time.Now().Format("20060102"),
	} {
		if err := k.SetStringValue(name, val); err != nil {
			return err
		}
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", size)
	return nil
}

// Install copies srcExe into the programs folder, registers it, creates the
// shortcuts and points the weekly task at the new path. desktop controls
// the desktop shortcut.
func Install(srcExe, version string, desktop bool, scheduleName string) error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	dst := Exe()
	if !SamePath(srcExe, dst) {
		if err := copyFile(srcExe, dst); err != nil {
			return err
		}
	}
	if err := Register(version); err != nil {
		return err
	}
	if err := sysutil.CreateShortcut(startMenuLink(), dst, "", Dir(), AppName+" PC cleaner"); err != nil {
		return errors.New("installed, but the Start menu shortcut failed: " + err.Error())
	}
	if desktop {
		_ = sysutil.CreateShortcut(desktopLink(), dst, "", Dir(), AppName+" PC cleaner")
	}
	if scheduleName != "" {
		_ = sysutil.ScheduleRetarget(scheduleName, dst, "/auto")
	}
	return nil
}

// AddDesktopShortcut creates the desktop shortcut for the installed copy.
func AddDesktopShortcut() error {
	if _, err := os.Stat(Exe()); err != nil {
		return errors.New("Kempt is not installed yet.")
	}
	return sysutil.CreateShortcut(desktopLink(), Exe(), "", Dir(), AppName+" PC cleaner")
}

// Uninstall removes the shortcuts, the registry entry, the weekly task and
// then the program folder. The folder is removed by a detached command a
// few seconds later so the running exe can exit first. With purge, the
// settings and logs folder goes too.
func Uninstall(scheduleName, dataDir string, purge bool) error {
	os.Remove(startMenuLink())
	os.Remove(desktopLink())
	_ = registry.DeleteKey(registry.CURRENT_USER, RegKey)
	if scheduleName != "" {
		_ = sysutil.ScheduleDelete(scheduleName)
	}
	// The folder may hold the running exe, so retry for a while.
	script := "@echo off\r\nset n=0\r\n:again\r\n" +
		"ping -n 2 127.0.0.1 >nul\r\n" +
		"rmdir /s /q \"" + Dir() + "\" 2>nul\r\n" +
		"if exist \"" + Exe() + "\" (set /a n+=1 & if %n% lss 20 goto again)\r\n"
	if purge && dataDir != "" {
		script += "rmdir /s /q \"" + dataDir + "\" 2>nul\r\n"
	}
	return sysutil.RunScriptDetached("kempt-uninstall", script)
}

// RemoveLater deletes a portable exe once it has exited, retrying for a
// while. Used by the installed copy to tidy the Desktop.
func RemoveLater(exe string) {
	if !Removable(exe) || SamePath(exe, Exe()) {
		return
	}
	go func() {
		for i := 0; i < 40; i++ {
			time.Sleep(500 * time.Millisecond)
			if err := os.Remove(exe); err == nil || os.IsNotExist(err) {
				return
			}
		}
	}()
}
