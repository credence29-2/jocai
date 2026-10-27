package main

import (
	"errors"
	"os"

	"github.com/credence29-2/jocai/cpc/internal/install"
	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
	"github.com/credence29-2/jocai/cpc/internal/update"
)

// ---- Installation ------------------------------------------------------------

// InstallInfo says where Kempt is installed and whether this copy is it.
func (a *api) InstallInfo() install.Info {
	return install.Current(a.exe)
}

// InstallNow installs this copy (or updates the installed one), opens the
// installed copy and closes this one.
func (a *api) InstallNow() error {
	if a.busy() {
		return errors.New("Wait for the current clean to finish.")
	}
	if err := install.Install(a.exe, version, true, scheduleName); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg.NoInstallPrompt = false
	_ = settings.Save(a.dataDir, a.cfg)
	a.mu.Unlock()
	if install.IsInstalledCopy(a.exe) {
		return nil
	}
	args := []string{}
	if install.Removable(a.exe) {
		args = append(args, "/cleanup", a.exe)
	}
	if err := update.Relaunch(install.Exe(), args...); err != nil {
		return errors.New("Installed, but could not open the installed copy: " + err.Error())
	}
	if a.quit != nil {
		a.quit()
	}
	return nil
}

// AddDesktopShortcut puts a shortcut to the installed copy on the Desktop.
func (a *api) AddDesktopShortcut() error { return install.AddDesktopShortcut() }

// OpenInstallFolder shows the programs folder in Explorer.
func (a *api) OpenInstallFolder() error {
	if _, err := os.Stat(install.Dir()); err != nil {
		return errors.New("Kempt is not installed yet.")
	}
	return sysutil.ShellOpen(install.Dir())
}

// UninstallNow removes Kempt from this PC and closes it. With purge the
// settings, logs and backups go too.
func (a *api) UninstallNow(purge bool) error {
	if a.busy() {
		return errors.New("Wait for the current clean to finish.")
	}
	if err := install.Uninstall(scheduleName, a.dataDir, purge); err != nil {
		return err
	}
	if a.quit != nil {
		a.quit()
	}
	return nil
}
