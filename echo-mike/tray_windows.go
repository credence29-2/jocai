package main

import (
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The tray icon is how a desktop app without a Store package raises Windows
// notifications: a balloon on a notification-area icon shows as a normal
// Windows 10 and 11 notification, and clicking it brings the window back.

var (
	procShellNotifyIconW  = shell32.NewProc("Shell_NotifyIconW")
	procLoadImageW        = user32.NewProc("LoadImageW")
	procGetSystemMetrics  = user32.NewProc("GetSystemMetrics")
	procSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW   = user32.NewProc("CallWindowProcW")
	procShowWindow        = user32.NewProc("ShowWindow")
	procSetForegroundWnd  = user32.NewProc("SetForegroundWindow")
	procIsIconic          = user32.NewProc("IsIconic")
	kernel32              = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandleW  = kernel32.NewProc("GetModuleHandleW")
)

const (
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nimSetVersion = 4

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	nifShowTip = 0x80

	niifUser           = 0x04
	niifLargeIcon      = 0x20
	niifRespectQuiet   = 0x80
	notifyIconVersion4 = 4

	wmApp               = 0x8000
	trayMsg             = wmApp + 1
	wmLButtonUp         = 0x0202
	ninBalloonUserClick = 0x0405

	gwlpWndProc = ^uintptr(3) // -4
	imageIcon   = 1
	smCXSmIcon  = 49
	smCYSmIcon  = 50
	swRestore   = 9
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type tray struct {
	mu      sync.Mutex
	hwnd    uintptr
	small   uintptr
	large   uintptr
	oldProc uintptr
	added   bool
}

func loadIcon(cx, cy uintptr) uintptr {
	inst, _, _ := procGetModuleHandleW.Call(0)
	h, _, _ := procLoadImageW.Call(inst, 1, imageIcon, cx, cy, 0)
	return h
}

// newTray adds the notification-area icon. Call it on the window's thread.
func newTray(hwnd uintptr) *tray {
	cx, _, _ := procGetSystemMetrics.Call(smCXSmIcon)
	cy, _, _ := procGetSystemMetrics.Call(smCYSmIcon)
	t := &tray{hwnd: hwnd, small: loadIcon(cx, cy), large: loadIcon(64, 64)}

	// Route the icon's clicks through our window procedure, then on to the
	// original one.
	cb := windows.NewCallback(func(h, msg, wp, lp uintptr) uintptr {
		if msg == trayMsg {
			switch lp & 0xFFFF {
			case ninBalloonUserClick, wmLButtonUp:
				t.show()
			}
			return 0
		}
		r, _, _ := procCallWindowProcW.Call(t.oldProc, h, msg, wp, lp)
		return r
	})
	t.oldProc, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, cb)

	d := t.data()
	d.UFlags = nifMessage | nifIcon | nifTip | nifShowTip
	d.UCallbackMessage = trayMsg
	d.HIcon = t.small
	copy(d.SzTip[:], windows.StringToUTF16(appName))
	if r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(d))); r != 0 {
		t.added = true
		d.UVersion = notifyIconVersion4
		procShellNotifyIconW.Call(nimSetVersion, uintptr(unsafe.Pointer(d)))
	}
	return t
}

func (t *tray) data() *notifyIconData {
	d := &notifyIconData{HWnd: t.hwnd, UID: 1}
	d.CbSize = uint32(unsafe.Sizeof(*d))
	return d
}

// notify shows a Windows notification. Focus assist (Do not disturb) is
// respected. Safe to call from any goroutine.
func (t *tray) notify(title, body string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.added {
		return
	}
	d := t.data()
	d.UFlags = nifInfo
	d.DwInfoFlags = niifUser | niifLargeIcon | niifRespectQuiet
	d.HBalloonIcon = t.large
	copy(d.SzInfoTitle[:63], windows.StringToUTF16(truncate(title, 60)))
	copy(d.SzInfo[:255], windows.StringToUTF16(truncate(body, 250)))
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(d)))
}

func (t *tray) show() {
	if r, _, _ := procIsIconic.Call(t.hwnd); r != 0 {
		procShowWindow.Call(t.hwnd, swRestore)
	}
	procSetForegroundWnd.Call(t.hwnd)
}

// remove takes the icon out of the notification area on exit, so no ghost
// icon is left behind.
func (t *tray) remove() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.added {
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(t.data())))
		t.added = false
	}
}

func truncate(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\x00", ""))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
