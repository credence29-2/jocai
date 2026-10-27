// Command cpc is Kempt, a CCleaner replacement for Windows.
//
// The window is a WebView2 page (web/index.html, embedded in the exe). The
// page calls Go through bound functions named cpc<Method>, see api.go.
package main

import (
	_ "embed"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	webview2 "github.com/jchv/go-webview2"

	"github.com/credence29-2/jocai/cpc/internal/install"
	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
	"github.com/credence29-2/jocai/cpc/internal/update"
)

//go:embed web/index.html
var indexHTML string

const webview2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

func main() {
	for i, a := range os.Args[1:] {
		switch strings.ToLower(a) {
		case "/auto", "-auto", "--auto":
			os.Exit(runAuto())
		case "/report", "-report", "--report":
			dest := ""
			if i+2 < len(os.Args) {
				dest = os.Args[i+2]
			}
			os.Exit(runReport(dest))
		case "/install", "-install", "--install":
			os.Exit(runInstall())
		case "/uninstall", "-uninstall", "--uninstall":
			os.Exit(runUninstall(hasFlag("/purge")))
		case "/cleanup", "-cleanup", "--cleanup":
			if i+2 < len(os.Args) {
				install.RemoveLater(os.Args[i+2])
			}
		case "/export", "-export", "--export", "/import", "-import", "--import":
			file := ""
			if i+2 < len(os.Args) {
				file = os.Args[i+2]
			}
			os.Exit(runSettingsFile(strings.Contains(a, "export"), file))
		}
	}
	if exe, err := os.Executable(); err == nil {
		update.CleanupOld(exe)
	}

	dataDir := settings.Dir()
	if offerInstall(dataDir) {
		return
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  dataDir + `\WebView2`,
		WindowOptions: webview2.WindowOptions{
			Title:  appName,
			Width:  1180,
			Height: 760,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		sysutil.MessageBox(appName, "Kempt needs the Microsoft Edge WebView2 Runtime, which comes with Windows 11 and updated Windows 10.\n\n"+
			"It wasn't found on this PC. Your browser will open the download page. Install it, then start Kempt again.", true)
		_ = sysutil.ShellOpen(webview2Download)
		os.Exit(1)
	}
	defer w.Destroy()
	w.SetSize(900, 600, webview2.HintMin)
	sysutil.DarkTitleBar(uintptr(w.Window()))

	a := newAPI(dataDir, sysutil.IsElevated(), func(name string, payload any) {
		js := eventJS(name, payload)
		w.Dispatch(func() { w.Eval(js) })
	})
	a.hwnd = uintptr(w.Window())
	a.quit = func() { w.Dispatch(func() { w.Terminate() }) }
	installTray(a.hwnd, func() { a.emit("trayClean", nil) }, func() { w.Terminate() })
	a.setTray = setTray
	if a.cfg.Tray {
		setTray(true)
	}
	a.restartElevated = func() error {
		if err := sysutil.ShellRunAs(a.exe); err != nil {
			return err
		}
		w.Terminate()
		return nil
	}
	if err := bindAll(w, a); err != nil {
		sysutil.MessageBox(appName, "Could not start: "+err.Error(), true)
		os.Exit(1)
	}
	w.SetHtml(indexHTML)
	w.Run()
	a.Cancel()
}

// bindAll exposes every exported api method to the page as cpc<Method>.
func bindAll(w webview2.WebView, a *api) error {
	v := reflect.ValueOf(a)
	t := v.Type()
	for i := 0; i < t.NumMethod(); i++ {
		m := t.Method(i)
		if err := w.Bind("cpc"+m.Name, v.Method(i).Interface()); err != nil {
			return err
		}
	}
	return nil
}

// runSettingsFile handles Kempt.exe /export <file> and /import <file> for
// scripts that set up many PCs the same way.
func runSettingsFile(export bool, file string) int {
	if file == "" {
		if export {
			file = "Kempt-settings.json"
		} else {
			return 2
		}
	}
	a := newAPI(settings.Dir(), sysutil.IsElevated(), func(string, any) {})
	if export {
		if err := a.exportTo(file); err != nil {
			return 1
		}
		return 0
	}
	if _, err := a.importFrom(file); err != nil {
		return 1
	}
	return 0
}

func hasFlag(flag string) bool {
	for _, a := range os.Args[1:] {
		if strings.EqualFold(a, flag) {
			return true
		}
	}
	return false
}

// offerInstall runs when Kempt starts from somewhere other than its
// programs folder. It offers to install (or update the installed copy) and
// returns true when the installed copy was started instead of this one.
func offerInstall(dataDir string) bool {
	exe, err := os.Executable()
	if err != nil || hasFlag("/portable") {
		return false
	}
	cur := install.Current(exe)
	if cur.ThisCopy {
		if cur.Version != version {
			_ = install.Register(version)
		}
		install.RemoveOld()
		return false
	}
	cfg := settings.Load(dataDir)
	var title, text string
	switch {
	case !cur.Installed:
		if cfg.NoInstallPrompt {
			return false
		}
		title = "Install " + appName + "?"
		text = appName + " can install itself into your apps folder, with a Start menu entry and a desktop shortcut, so updates always land in the same place."
		if install.Removable(exe) {
			text += " The copy in " + filepath.Base(filepath.Dir(exe)) + " is replaced by the shortcut."
		}
		text += "\n\nYes installs. No runs it from here and won't ask again. Cancel closes."
	case update.Newer(version, cur.Version):
		title = "Update " + appName + "?"
		text = "This is " + appName + " " + version + " and the installed copy is " + cur.Version + ".\n\nYes replaces the installed copy with this version and opens it. No runs this copy from here. Cancel closes."
	default:
		title = appName + " is already installed"
		text = appName + " " + cur.Version + " is installed in " + cur.Dir + ".\n\nYes opens the installed copy. No runs this copy from here. Cancel closes."
	}
	switch sysutil.MessageBoxYesNoCancel(title, text) {
	case 0:
		os.Exit(0)
	case 2:
		if !cur.Installed {
			cfg.NoInstallPrompt = true
			_ = settings.Save(dataDir, cfg)
		}
		return false
	}
	if !cur.Installed || update.Newer(version, cur.Version) {
		if err := install.Install(exe, version, true, scheduleName); err != nil {
			sysutil.MessageBox(appName, "Could not install: "+err.Error()+"\n\n"+appName+" will run from here instead.", true)
			return false
		}
	}
	args := []string{}
	if install.Removable(exe) {
		args = append(args, "/cleanup", exe)
	}
	if err := update.Relaunch(install.Exe(), args...); err != nil {
		sysutil.MessageBox(appName, "Installed, but could not open the installed copy: "+err.Error(), true)
		return false
	}
	return true
}

// runInstall handles Kempt.exe /install: a silent install for scripts.
func runInstall() int {
	exe, err := os.Executable()
	if err != nil {
		return 1
	}
	if err := install.Install(exe, version, !hasFlag("/nodesktop"), scheduleName); err != nil {
		return 1
	}
	return 0
}

// runUninstall handles Kempt.exe /uninstall [/purge].
func runUninstall(purge bool) int {
	if err := install.Uninstall(scheduleName, settings.Dir(), purge); err != nil {
		return 1
	}
	return 0
}
