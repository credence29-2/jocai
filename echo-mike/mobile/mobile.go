// Package mobile is the Android app's way into Project Echo Mike. It's built
// with gomobile bind into an .aar, so it only uses types gomobile can bridge.
//
// The Android app runs the same local server and interface as Windows, in a
// WebView, plus a background job that calls CheckMail.
package mobile

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/credence29-2/jocai/echo-mike/internal/app"
	"github.com/credence29-2/jocai/echo-mike/internal/store"
)

// Host is what the Android side provides.
type Host interface {
	// OpenURL opens a link or a sign-in page in the browser.
	OpenURL(url string)
	// ShowNotification shows a new-mail notification. (Not "Notify", which
	// would clash with Java's Object.notify.)
	ShowNotification(title, body string)
	// Refresh tells the open window that new mail arrived.
	Refresh()
}

var (
	mu   sync.Mutex
	cur  *app.App
	addr string
)

// Start starts the app once per process and returns the address to load in
// the WebView. Later calls return the same address.
func Start(dataDir string, demo bool, host Host) (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if cur != nil && addr != "" {
		return addr, nil
	}
	a, err := open(dataDir, demo, host)
	if err != nil {
		return "", err
	}
	u, err := a.Listen()
	if err != nil {
		return "", err
	}
	a.OnNewMail = host.Refresh
	cur, addr = a, u
	go a.WatchMail(context.Background(), time.Minute)
	return u, nil
}

// CheckMail looks for new mail once and notifies about it. The background
// job calls it every 15 minutes or so, whether or not the app is open.
func CheckMail(dataDir string, host Host) error {
	mu.Lock()
	a := cur
	if a == nil {
		var err error
		if a, err = open(dataDir, false, host); err != nil {
			mu.Unlock()
			return err
		}
		cur = a
	}
	mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	a.CheckMail(ctx)
	return nil
}

func open(dataDir string, demo bool, host Host) (*app.App, error) {
	dir := dataDir
	if demo {
		// Sample accounts get their own folder so they never mix with real
		// ones. Android has no shared temp folder to use instead.
		dir = filepath.Join(dataDir, "demo")
		os.RemoveAll(dir)
	}
	st, err := store.Open(dir)
	if err != nil {
		return nil, err
	}
	a := app.New(st, demo, func(u string) error { host.OpenURL(u); return nil })
	a.Notify = host.ShowNotification
	return a, nil
}
