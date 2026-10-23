// Package sysutil wraps the handful of Win32 calls CPC needs.
package sysutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	shell32                = windows.NewLazySystemDLL("shell32.dll")
	procSHQueryRecycleBinW = shell32.NewProc("SHQueryRecycleBinW")
	procSHEmptyRecycleBinW = shell32.NewProc("SHEmptyRecycleBinW")
	procShellExecuteW      = shell32.NewProc("ShellExecuteW")

	dnsapi                    = windows.NewLazySystemDLL("dnsapi.dll")
	procDnsFlushResolverCache = dnsapi.NewProc("DnsFlushResolverCache")
)

// IsElevated reports whether CPC is running as administrator.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// RunningProcesses returns the lower-cased exe names of running processes.
func RunningProcesses() map[string]bool {
	out := map[string]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var pe windows.ProcessEntry32
	pe.Size = uint32(unsafe.Sizeof(pe))
	for err = windows.Process32First(snap, &pe); err == nil; err = windows.Process32Next(snap, &pe) {
		out[strings.ToLower(windows.UTF16ToString(pe.ExeFile[:]))] = true
	}
	return out
}

type shQueryRBInfo struct {
	cbSize   uint32
	size     int64
	numItems int64
}

// RecycleBinInfo returns the total size and item count across all drives.
func RecycleBinInfo() (size int64, items int64, err error) {
	info := shQueryRBInfo{}
	info.cbSize = uint32(unsafe.Sizeof(info))
	hr, _, _ := procSHQueryRecycleBinW.Call(0, uintptr(unsafe.Pointer(&info)))
	if hr != 0 {
		return 0, 0, syscall.Errno(hr)
	}
	return info.size, info.numItems, nil
}

// EmptyRecycleBin empties the Recycle Bin on every drive without prompting.
func EmptyRecycleBin() error {
	const (
		noConfirmation = 0x1
		noProgressUI   = 0x2
		noSound        = 0x4
	)
	hr, _, _ := procSHEmptyRecycleBinW.Call(0, 0, noConfirmation|noProgressUI|noSound)
	// S_OK, or E_UNEXPECTED when the bin is already empty.
	if hr != 0 && uint32(hr) != 0x8000FFFF {
		return syscall.Errno(hr)
	}
	return nil
}

// FlushDNS clears the DNS resolver cache, same as ipconfig /flushdns.
func FlushDNS() error {
	r, _, err := procDnsFlushResolverCache.Call()
	if r == 0 {
		if err != nil && err != windows.ERROR_SUCCESS {
			return err
		}
		return errors.New("DnsFlushResolverCache failed")
	}
	return nil
}

// ShowInExplorer opens Explorer with the file selected (or the folder open).
func ShowInExplorer(path string) error {
	cmd := exec.Command("explorer.exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `explorer.exe /select,"` + path + `"`}
	return cmd.Start()
}

// ShellOpen opens a file, folder or URL with its default handler.
func ShellOpen(target string) error {
	return shellExecute("open", target, "")
}

func shellExecute(verb, file, params string) error {
	v, _ := windows.UTF16PtrFromString(verb)
	f, _ := windows.UTF16PtrFromString(file)
	var p *uint16
	if params != "" {
		p, _ = windows.UTF16PtrFromString(params)
	}
	r, _, _ := procShellExecuteW.Call(0, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)),
		uintptr(unsafe.Pointer(p)), 0, windows.SW_SHOWNORMAL)
	if r <= 32 {
		return syscall.Errno(r)
	}
	return nil
}

// RunCommandLine starts a raw Windows command line (as stored in the registry
// for uninstallers). If the program needs elevation and we are not elevated,
// it is relaunched through a UAC prompt.
func RunCommandLine(cmdline string) error {
	si := &windows.StartupInfo{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	pi := &windows.ProcessInformation{}
	line, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return err
	}
	err = windows.CreateProcess(nil, line, nil, nil, false, 0, nil, nil, si, pi)
	if err == nil {
		windows.CloseHandle(pi.Thread)
		windows.CloseHandle(pi.Process)
		return nil
	}
	if errors.Is(err, windows.ERROR_ELEVATION_REQUIRED) {
		return shellExecute("runas", "cmd.exe", `/s /c "`+cmdline+`"`)
	}
	return err
}

// ShellRunAs launches a program through the UAC elevation prompt.
func ShellRunAs(exe string) error {
	return shellExecute("runas", exe, "")
}

// DiskSpace returns total and free bytes for the drive holding path.
func DiskSpace(path string) (total, free uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail uint64
	err = windows.GetDiskFreeSpaceEx(p, &avail, &total, &free)
	return total, avail, err
}

var (
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	user32                    = windows.NewLazySystemDLL("user32.dll")
	procMessageBoxW           = user32.NewProc("MessageBoxW")
)

// DarkTitleBar asks Windows 10 20H1+ and 11 to draw a dark title bar.
func DarkTitleBar(hwnd uintptr) {
	on := int32(1)
	for _, attr := range []uintptr{20, 19} { // DWMWA_USE_IMMERSIVE_DARK_MODE, and its pre-20H1 value
		if r, _, _ := procDwmSetWindowAttribute.Call(hwnd, attr, uintptr(unsafe.Pointer(&on)), 4); r == 0 {
			return
		}
	}
}

// MessageBox shows a plain Windows message box.
func MessageBox(title, text string, warning bool) {
	t, _ := windows.UTF16PtrFromString(title)
	m, _ := windows.UTF16PtrFromString(text)
	flags := uintptr(0x40) // MB_ICONINFORMATION
	if warning {
		flags = 0x30 // MB_ICONWARNING
	}
	procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags)
}

func schtasks(args ...string) (string, error) {
	cmd := exec.Command("schtasks.exe", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// ScheduleStatus reports whether the named scheduled task exists, with its
// next run time when Windows gives one.
func ScheduleStatus(name string) (exists bool, nextRun string) {
	out, err := schtasks("/query", "/tn", name, "/fo", "list")
	if err != nil {
		return false, ""
	}
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Next Run Time") {
			return true, strings.TrimSpace(v)
		}
	}
	return true, ""
}

// ScheduleWeekly creates or replaces a weekly task that runs exe with args,
// elevated, on the given day (MON..SUN) at hh:mm.
func ScheduleWeekly(name, exe, args, day, at string) error {
	out, err := schtasks("/create", "/f", "/tn", name, "/tr", `"`+exe+`" `+args,
		"/sc", "weekly", "/d", day, "/st", at, "/rl", "highest")
	if err != nil {
		return errors.New(out)
	}
	return nil
}

// ScheduleDelete removes the named task. Missing tasks are not an error.
func ScheduleDelete(name string) error {
	if ok, _ := ScheduleStatus(name); !ok {
		return nil
	}
	out, err := schtasks("/delete", "/f", "/tn", name)
	if err != nil {
		return errors.New(out)
	}
	return nil
}

var (
	procOpenClipboard  = user32.NewProc("OpenClipboard")
	procEmptyClipboard = user32.NewProc("EmptyClipboard")
	procCloseClipboard = user32.NewProc("CloseClipboard")
)

// ClearClipboard empties the Windows clipboard.
func ClearClipboard() error {
	if r, _, err := procOpenClipboard.Call(0); r == 0 {
		return err
	}
	defer procCloseClipboard.Call()
	if r, _, err := procEmptyClipboard.Call(); r == 0 {
		return err
	}
	return nil
}

// RunHidden runs a console program without showing a window and returns
// its combined output.
func RunHidden(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// RemoveProtectedTree deletes a folder Windows protects, such as
// Windows.old, by taking ownership first. Same steps Disk Cleanup takes.
func RemoveProtectedTree(path string) error {
	if _, err := RunHidden("takeown.exe", "/F", path, "/R", "/A", "/D", "Y"); err != nil {
		return err
	}
	if _, err := RunHidden("icacls.exe", path, "/grant", "*S-1-5-32-544:F", "/T", "/C", "/Q"); err != nil {
		return err
	}
	out, err := RunHidden("cmd.exe", "/c", "rd", "/s", "/q", path)
	if err != nil {
		return errors.New(strings.TrimSpace(out))
	}
	return nil
}

var (
	procSHBrowseForFolderW   = shell32.NewProc("SHBrowseForFolderW")
	procSHGetPathFromIDListW = shell32.NewProc("SHGetPathFromIDListW")
	procSHFileOperationW     = shell32.NewProc("SHFileOperationW")
	ole32                    = windows.NewLazySystemDLL("ole32.dll")
	procCoTaskMemFree        = ole32.NewProc("CoTaskMemFree")
)

type browseInfo struct {
	Owner       uintptr
	Root        uintptr
	DisplayName *uint16
	Title       *uint16
	Flags       uint32
	Callback    uintptr
	LParam      uintptr
	Image       int32
}

// PickFolder shows the Windows folder picker. Returns "" if cancelled.
func PickFolder(owner uintptr, title string) string {
	name := make([]uint16, windows.MAX_PATH)
	t, _ := windows.UTF16PtrFromString(title)
	bi := browseInfo{Owner: owner, DisplayName: &name[0], Title: t, Flags: 0x0001 | 0x0040} // RETURNONLYFSDIRS | NEWDIALOGSTYLE
	pidl, _, _ := procSHBrowseForFolderW.Call(uintptr(unsafe.Pointer(&bi)))
	if pidl == 0 {
		return ""
	}
	defer procCoTaskMemFree.Call(pidl)
	buf := make([]uint16, 32768)
	if r, _, _ := procSHGetPathFromIDListW.Call(pidl, uintptr(unsafe.Pointer(&buf[0]))); r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf)
}

type shFileOp struct {
	Hwnd          uintptr
	Func          uint32
	From          *uint16
	To            *uint16
	Flags         uint16
	AnyAborted    int32
	NameMappings  uintptr
	ProgressTitle *uint16
}

// RecycleFiles moves files to the Recycle Bin so they can be restored.
func RecycleFiles(paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	var from []uint16
	for _, p := range paths {
		u, err := windows.UTF16FromString(p) // includes the trailing NUL
		if err != nil {
			return err
		}
		from = append(from, u...)
	}
	from = append(from, 0) // list ends with a double NUL
	const (
		foDelete        = 3
		fofSilent       = 0x0004
		fofNoConfirm    = 0x0010
		fofAllowUndo    = 0x0040
		fofNoErrorUI    = 0x0400
		fofNoConfirmMkd = 0x0200
	)
	op := shFileOp{Func: foDelete, From: &from[0], Flags: fofSilent | fofNoConfirm | fofAllowUndo | fofNoErrorUI | fofNoConfirmMkd}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return fmt.Errorf("Windows could not move the files to the Recycle Bin (code %d)", r)
	}
	if op.AnyAborted != 0 {
		return errors.New("moving files to the Recycle Bin was cancelled")
	}
	return nil
}

// PowerShellJSON runs a PowerShell script that writes JSON and decodes it.
func PowerShellJSON(script string, out any) error {
	return PowerShellJSONTimeout(script, 2*time.Minute, out)
}

// PowerShellJSONTimeout is PowerShellJSON with a time limit.
func PowerShellJSONTimeout(script string, limit time.Duration, out any) error {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command",
		"[Console]::OutputEncoding=[Text.Encoding]::UTF8; "+script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	b, err := cmd.Output()
	if ctx.Err() != nil {
		return errors.New("timed out")
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(ee.Stderr)))
		}
		return err
	}
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 {
		b = []byte("[]")
	}
	// A single object comes back without brackets.
	if b[0] == '{' {
		b = append(append([]byte{'['}, b...), ']')
	}
	return json.Unmarshal(b, out)
}
