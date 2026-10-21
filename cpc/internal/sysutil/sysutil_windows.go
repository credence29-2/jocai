// Package sysutil wraps the handful of Win32 calls CPC needs.
package sysutil

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
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
