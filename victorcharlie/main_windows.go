package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// pauseOnWindows keeps the console open after a startup error when Victor Charlie
// was started by double-clicking, so the message can be read.
func pauseOnWindows() {
	if os.Getenv("VC_RESTARTED") != "" {
		return
	}
	// When started in the background (Task Scheduler) nobody can press
	// Enter, so give up after a while; the task then restarts Victor Charlie.
	fmt.Fprintln(os.Stderr, "\nPress Enter to close.")
	done := make(chan struct{})
	go func() {
		fmt.Scanln()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

// restartSelf starts a fresh copy of Victor Charlie in this console and exits.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = append(os.Environ(), "VC_RESTARTED=1")
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil
}
