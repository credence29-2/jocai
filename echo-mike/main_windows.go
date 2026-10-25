// Command echo-mike is Project Echo Mike, one window for all your Gmail and Outlook
// accounts.
//
// The window is a WebView2 page served by a local server on 127.0.0.1 (see
// server.go). The page, the API and the sign-in redirect all stay on this PC.
package main

import (
	"context"
	"flag"
	"os"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"github.com/credence29-2/jocai/echo-mike/internal/store"
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
	flag.Parse()

	dataDir := store.Dir()
	st, err := openStore(dataDir, *demoMode)
	if err != nil {
		messageBox(appName, "Project Echo Mike couldn't load its settings:\n\n"+err.Error())
		os.Exit(1)
	}
	a := newApp(st, *demoMode, shellOpen)
	u, err := a.listen()
	if err != nil {
		messageBox(appName, "Could not start: "+err.Error())
		os.Exit(1)
	}

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  dataDir + `\WebView2`,
		WindowOptions: webview2.WindowOptions{
			Title:  appName,
			Width:  1320,
			Height: 840,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		messageBox(appName, "Project Echo Mike needs the Microsoft Edge WebView2 Runtime, which comes with Windows 11 and updated Windows 10.\n\n"+
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
	a.notify = t.notify
	a.onNewMail = func() {
		w.Dispatch(func() { w.Eval("window.onNewMail && window.onNewMail()") })
	}
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go a.watchMail(ctx, time.Minute)

	w.Run()
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
