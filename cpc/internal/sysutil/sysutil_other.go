//go:build !windows

package sysutil

import "errors"

var errNotWindows = errors.New("only supported on Windows")

func IsElevated() bool                      { return false }
func RunningProcesses() map[string]bool     { return map[string]bool{} }
func RecycleBinInfo() (int64, int64, error) { return 0, 0, errNotWindows }
func EmptyRecycleBin() error                { return errNotWindows }
func FlushDNS() error                       { return errNotWindows }
func ShowInExplorer(string) error           { return errNotWindows }
func ShellOpen(string) error                { return errNotWindows }
func RunCommandLine(string) error           { return errNotWindows }
func ShellRunAs(string) error               { return errNotWindows }
