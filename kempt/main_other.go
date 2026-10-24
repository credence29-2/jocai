//go:build !windows

package main

import (
	"os"
	"syscall"
)

func pauseOnWindows() {}

// restartSelf replaces this process with a fresh copy of itself.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(exe, os.Args, append(os.Environ(), "KEMPT_RESTARTED=1"))
}
