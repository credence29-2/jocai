// Command cpc is Credence PC Cleaner, a CCleaner replacement for Windows.
//
// The window is a WebView2 page (web/index.html, embedded in the exe). The
// page calls Go through bound functions named cpc<Method>, see api.go.
package main

import (
	_ "embed"
	"os"
	"reflect"
	"strings"

	webview2 "github.com/jchv/go-webview2"

	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

//go:embed web/index.html
var indexHTML string

const webview2Download = "https://go.microsoft.com/fwlink/p/?LinkId=2124703"

func main() {
	for _, a := range os.Args[1:] {
		switch strings.ToLower(a) {
		case "/auto", "-auto", "--auto":
			os.Exit(runAuto())
		}
	}

	dataDir := settings.Dir()
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
		sysutil.MessageBox(appName, "CPC needs the Microsoft Edge WebView2 Runtime, which comes with Windows 11 and updated Windows 10.\n\n"+
			"It wasn't found on this PC. Your browser will open the download page. Install it, then start CPC again.", true)
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
