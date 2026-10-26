// Command echo-mike is Project Echo Mike, one window for all your Gmail and Outlook
// accounts.
//
// The window is a WebView2 page served by a local server on 127.0.0.1 (see
// server.go). The page, the API and the sign-in redirect all stay on this PC.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"github.com/credence29-2/jocai/echo-mike/internal/app"
	"github.com/credence29-2/jocai/echo-mike/internal/install"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
	"github.com/credence29-2/jocai/echo-mike/internal/update"
)

const webview2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	shell32                   = windows.NewLazySystemDLL("shell32.dll")
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	procMessageBoxW           = user32.NewProc("MessageBoxW")
	procShellExecuteW         = shell32.NewProc("ShellExecuteW")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

func main() {
	demoMode := flag.Bool("demo", false, "use sample accounts instead of real ones")
	mcpMode := flag.Bool("mcp", false, "serve the MCP tools over stdin/stdout for Claude Desktop, no window")
	portable := flag.Bool("portable", false, "run from here without installing")
	uninstall := flag.Bool("uninstall", false, "remove Project Echo Mike from this PC")
	flag.Parse()

	dataDir := store.Dir()
	if *uninstall {
		runUninstall(dataDir)
		return
	}
	exe, _ := os.Executable()
	if !*demoMode && !*mcpMode && !*portable && exe != "" && !install.Installed(exe) {
		if installed := offerInstall(exe, dataDir); installed != "" {
			exec.Command(installed).Start()
			return
		}
	} else if exe != "" && install.Installed(exe) && !*mcpMode {
		install.Register(exe, app.Version) // keeps Apps & features' version current after an update
	}
	st, err := app.OpenStore(dataDir, *demoMode)
	if err != nil {
		messageBox(app.Name, "Project Echo Mike couldn't load its settings:\n\n"+err.Error())
		os.Exit(1)
	}
	a := app.New(st, *demoMode, shellOpen)
	if *mcpMode {
		// Claude Desktop starts the exe with pipes on stdin and stdout and
		// talks MCP over them. No window, no tray.
		if err := a.ServeMCP(context.Background()); err != nil {
			os.Exit(1)
		}
		return
	}
	if exe, err := os.Executable(); err == nil && !*demoMode {
		update.Cleanup(exe)
		a.Updater = update.New(app.Version, exe)
		a.Restart = func() {
			if beforeExit != nil {
				beforeExit()
			}
			exec.Command(exe).Start()
			os.Exit(0)
		}
	}
	u, err := a.Listen()
	if err != nil {
		messageBox(app.Name, "Could not start: "+err.Error())
		os.Exit(1)
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  dataDir + `\WebView2`,
		WindowOptions: webview2.WindowOptions{
			Title:  app.Name,
			Width:  1320,
			Height: 840,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		messageBox(app.Name, "Project Echo Mike needs the Microsoft Edge WebView2 Runtime, which comes with Windows 11 and updated Windows 10.\n\n"+
			"It wasn't found on this PC. Your browser will open the download page. Install it, then start Project Echo Mike again.")
		shellOpen(webview2Download)
		os.Exit(1)
	}
	defer w.Destroy()
	w.SetSize(960, 600, webview2.HintMin)
	darkTitleBar(uintptr(w.Window()))
	w.Navigate(u)

	t := newTray(uintptr(w.Window()))
	defer t.remove()
	beforeExit = t.remove
	a.Notify = t.notify
	a.OnNewMail = func() {
		w.Dispatch(func() { w.Eval("window.onNewMail && window.onNewMail()") })
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go a.WatchMail(ctx, time.Minute)
	go a.WatchUpdates(ctx, func(info update.Info) {
		js := "window.onUpdate && window.onUpdate(" + jsonString(info.Latest) + ")"
		w.Dispatch(func() { w.Eval(js) })
	})

	w.Run()
}

// beforeExit runs before a restart, so the tray icon doesn't linger.
var beforeExit func()

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// shellOpen opens a link in the default browser or mail program.
func shellOpen(target string) error {
	v, _ := windows.UTF16PtrFromString("open")
	f, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)), 0, 0, windows.SW_SHOWNORMAL)
	if r <= 32 {
		return windows.Errno(r)
	}
	return nil
}

// offerInstall asks once whether to install. "No" is remembered in the
// data folder so the question doesn't come back; -portable skips it too.
func offerInstall(exe, dataDir string) string {
	marker := dataDir + `\portable`
	if _, err := os.Stat(marker); err == nil {
		return ""
	}
	t, _ := windows.UTF16PtrFromString(app.Name)
	m, _ := windows.UTF16PtrFromString("Install Project Echo Mike on this PC?\n\nIt copies itself to " + install.Dir() + ", adds a Start menu and desktop shortcut, and shows up in Apps & features. Updates then always replace that one copy.\n\nChoose No to keep running it from where it is now.")
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x40|0x4) // MB_ICONINFORMATION | MB_YESNO
	if r != 6 { // IDYES
		os.MkdirAll(dataDir, 0o755)
		os.WriteFile(marker, []byte("Delete this file to be offered the install again.\n"), 0o644)
		return ""
	}
	dst, err := install.Install(exe, app.Version)
	if err != nil {
		messageBox(app.Name, "Couldn't install: "+err.Error()+"\n\nProject Echo Mike will run from here instead.")
		if dst == "" {
			return ""
		}
	}
	return dst
}

func runUninstall(dataDir string) {
	t, _ := windows.UTF16PtrFromString(app.Name)
	m, _ := windows.UTF16PtrFromString("Remove Project Echo Mike from this PC?\n\nYes removes the program and its shortcuts and keeps your settings and sign-ins in " + dataDir + ".\nTo remove those too, delete that folder afterwards.")
	r, _, _ := procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x20|0x4) // MB_ICONQUESTION | MB_YESNO
	if r != 6 {
		return
	}
	if err := install.Uninstall(true, dataDir); err != nil {
		messageBox(app.Name, "Couldn't remove everything: "+err.Error())
	}
}

func messageBox(title, text string) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), 0x30) // MB_ICONWARNING
}

// darkTitleBar asks Windows 10 20H1+ and 11 to draw a dark title bar.
func darkTitleBar(hwnd uintptr) {
	on := int32(1)
	for _, attr := range []uintptr{20, 19} { // DWMWA_USE_IMMERSIVE_DARK_MODE, and its pre-20H1 value
		if r, _, _ := procDwmSetWindowAttribute.Call(hwnd, attr, uintptr(unsafe.Pointer(&on)), 4); r == 0 {
			return
		}
	}
}
