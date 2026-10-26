package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/credence29-2/jocai/cpc/internal/settings"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// runReport writes the tech report with no window. With no destination it
// goes to the reports folder and opens in the browser. With a destination
// folder or .html/.txt file path it is written there, for RMM scripts.
// Returns the exit code.
func runReport(dest string) int {
	a := newAPI(settings.Dir(), sysutil.IsElevated(), func(string, any) {})
	htmlPath, txtPath, r, err := a.buildReport()
	if err != nil {
		return 2
	}
	if dest == "" {
		_ = sysutil.ShellOpen(htmlPath)
		return 0
	}
	if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
		dest = filepath.Join(dest, filepath.Base(htmlPath))
	}
	var data []byte
	if strings.EqualFold(filepath.Ext(dest), ".txt") {
		data = []byte(r.Text())
	} else {
		data = []byte(r.HTML())
	}
	_ = txtPath
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return 2
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return 2
	}
	return 0
}
