//go:build !windows

package sysutil

import (
	"errors"
	"time"
)

var errNotWindows = errors.New("only supported on Windows")

func IsElevated() bool                      { return false }
func RunningProcesses() map[string]bool     { return map[string]bool{} }
func RecycleBinInfo() (int64, int64, error) { return 0, 0, errNotWindows }
func EmptyRecycleBin() error                { return errNotWindows }
func FlushDNS() error                       { return errNotWindows }
func ShowInExplorer(string) error           { return errNotWindows }
func ShellOpen(string) error                { return errNotWindows }
func ShellOpenWith(string, string) error    { return errNotWindows }
func RunCommandLine(string) error           { return errNotWindows }
func ShellRunAs(string) error               { return errNotWindows }

func DiskSpace(string) (uint64, uint64, error)  { return 0, 0, errNotWindows }
func DarkTitleBar(uintptr)                      {}
func MessageBox(string, string, bool)           {}
func ScheduleStatus(string) (bool, string)      { return false, "" }
func ScheduleWeekly(_, _, _, _, _ string) error { return errNotWindows }
func ScheduleDelete(string) error               { return errNotWindows }

func ClearClipboard() error                       { return errNotWindows }
func RunHidden(string, ...string) (string, error) { return "", errNotWindows }
func RemoveProtectedTree(string) error            { return errNotWindows }

func PickFolder(uintptr, string) string                      { return "" }
func RecycleFiles([]string) error                            { return errNotWindows }
func PowerShellJSON(string, any) error                       { return errNotWindows }
func PowerShellJSONTimeout(string, time.Duration, any) error { return errNotWindows }

// Drive is one fixed disk volume.
type Drive struct {
	Path  string `json:"path"`
	Label string `json:"label"`
	Total uint64 `json:"total"`
	Free  uint64 `json:"free"`
}

func FixedDrives() []Drive { return nil }
