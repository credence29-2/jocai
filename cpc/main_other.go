//go:build !windows

// Command cpc is Credence PC Cleaner. It only runs on Windows; this stub lets
// the packages build and test on other systems.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "Credence PC Cleaner only runs on Windows. Build with GOOS=windows.")
	os.Exit(1)
}
