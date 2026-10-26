package main

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The notification area icon. The webview window is subclassed so tray
// clicks and the close button can be handled: with the tray on, closing
// the window hides it and Kempt keeps running.

var (
	user32x                 = windows.NewLazySystemDLL("user32.dll")
	shell32x                = windows.NewLazySystemDLL("shell32.dll")
	procSetWindowLongPtrW   = user32x.NewProc("SetWindowLongPtrW")
	procCallWindowProcW     = user32x.NewProc("CallWindowProcW")
	procShellNotifyIconW    = shell32x.NewProc("Shell_NotifyIconW")
	procLoadIconW           = user32x.NewProc("LoadIconW")
	procCreatePopupMenu     = user32x.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32x.NewProc("AppendMenuW")
	procDestroyMenu         = user32x.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32x.NewProc("TrackPopupMenu")
	procGetCursorPos        = user32x.NewProc("GetCursorPos")
	procSetForegroundWindow = user32x.NewProc("SetForegroundWindow")
	procShowWindow          = user32x.NewProc("ShowWindow")
	procPostMessageW        = user32x.NewProc("PostMessageW")
	procIsIconic            = user32x.NewProc("IsIconic")
)

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            windows.GUID
	BalloonIcon     uintptr
}

const (
	wmApp         = 0x8000
	wmTray        = wmApp + 1
	wmClose       = 0x0010
	wmCommand     = 0x0111
	wmLButtonUp   = 0x0202
	wmLButtonDbl  = 0x0203
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B
	nimAdd        = 0
	nimDelete     = 2
	nifMessage    = 1
	nifIcon       = 2
	nifTip        = 4
	swHide        = 0
	swShow        = 5
	swRestore     = 9
	gwlpWndProc   = ^uintptr(3) // -4
	mfString      = 0
	mfSeparator   = 0x800
	tpmReturnCmd  = 0x100
	tpmRightBtn   = 0x2
	tpmBottomAlgn = 0x20

	menuOpen  = 1
	menuClean = 2
	menuExit  = 3
)

type tray struct {
	mu       sync.Mutex
	hwnd     uintptr
	on       bool
	shown    bool
	oldProc  uintptr
	onClean  func()
	onExit   func()
	menuBusy bool
}

var trayState tray

// installTray subclasses the window. The icon itself is shown by setTray.
func installTray(hwnd uintptr, onClean, onExit func()) {
	trayState.hwnd, trayState.onClean, trayState.onExit = hwnd, onClean, onExit
	cb := windows.NewCallback(trayProc)
	trayState.oldProc, _, _ = procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, cb)
}

// setTray adds or removes the notification icon.
func setTray(on bool) {
	trayState.mu.Lock()
	defer trayState.mu.Unlock()
	trayState.on = on
	if on && !trayState.shown {
		d := trayData()
		d.Flags = nifMessage | nifIcon | nifTip
		d.CallbackMessage = wmTray
		d.Icon, _, _ = procLoadIconW.Call(moduleHandle(), 1)
		copy(d.Tip[:], windows.StringToUTF16(appName))
		r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&d)))
		trayState.shown = r != 0
	} else if !on && trayState.shown {
		d := trayData()
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&d)))
		trayState.shown = false
	}
}

func trayData() notifyIconData {
	d := notifyIconData{Wnd: trayState.hwnd, ID: 1}
	d.Size = uint32(unsafe.Sizeof(d))
	return d
}

func moduleHandle() uintptr {
	var h windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &h)
	return uintptr(h)
}

func showMainWindow() {
	if r, _, _ := procIsIconic.Call(trayState.hwnd); r != 0 {
		procShowWindow.Call(trayState.hwnd, swRestore)
	} else {
		procShowWindow.Call(trayState.hwnd, swShow)
	}
	procSetForegroundWindow.Call(trayState.hwnd)
}

func trayProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmClose:
		trayState.mu.Lock()
		on := trayState.on
		trayState.mu.Unlock()
		if on {
			procShowWindow.Call(hwnd, swHide)
			return 0
		}
		setTray(false)
	case wmTray:
		switch lp & 0xFFFF {
		case wmLButtonUp, wmLButtonDbl:
			showMainWindow()
		case wmRButtonUp, wmContextMenu:
			trayMenu(hwnd)
		}
		return 0
	}
	r, _, _ := procCallWindowProcW.Call(trayState.oldProc, hwnd, msg, wp, lp)
	return r
}

type point struct{ X, Y int32 }

func trayMenu(hwnd uintptr) {
	if trayState.menuBusy {
		return
	}
	trayState.menuBusy = true
	defer func() { trayState.menuBusy = false }()
	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)
	add := func(id uintptr, text string) {
		t, _ := windows.UTF16PtrFromString(text)
		procAppendMenuW.Call(m, mfString, id, uintptr(unsafe.Pointer(t)))
	}
	add(menuOpen, "Open "+appName)
	add(menuClean, "Clean now (Recommended items)")
	procAppendMenuW.Call(m, mfSeparator, 0, 0)
	add(menuExit, "Exit")
	var p point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	procSetForegroundWindow.Call(hwnd)
	cmd, _, _ := procTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightBtn|tpmBottomAlgn, uintptr(p.X), uintptr(p.Y), 0, hwnd, 0)
	procPostMessageW.Call(hwnd, 0, 0, 0) // WM_NULL, as the docs ask
	switch cmd {
	case menuOpen:
		showMainWindow()
	case menuClean:
		showMainWindow()
		if trayState.onClean != nil {
			trayState.onClean()
		}
	case menuExit:
		setTray(false)
		if trayState.onExit != nil {
			trayState.onExit()
		}
	}
}
