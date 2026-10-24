package main

import (
	"fmt"
	"os"
	"os/exec"
)

// pauseOnWindows keeps the console open after a startup error when Kempt
// was started by double-clicking, so the message can be read.
func pauseOnWindows() {
	if os.Getenv("KEMPT_RESTARTED") != "" {
		return
	}
	fmt.Fprintln(os.Stderr, "\nPress Enter to close.")
	fmt.Scanln()
}

// restartSelf starts a fresh copy of Kempt in this console and exits.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "KEMPT_RESTARTED=1")
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
