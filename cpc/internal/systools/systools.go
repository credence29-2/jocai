// Package systools covers the Windows tools behind CPC's Startup Manager
// tabs, Driver Check and Software Updater.
package systools

import (
	"strings"
	"unicode/utf8"
)

// Task is a scheduled task that isn't part of Windows itself.
type Task struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	Author  string `json:"author"`
	Action  string `json:"action"`
	Enabled bool   `json:"enabled"`
}

// Service is a background service installed by a program.
type Service struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	StartMode   string `json:"startMode"`
	State       string `json:"state"`
	Path        string `json:"path"`
	Description string `json:"description"`
}

// Device is hardware Windows reports a problem with.
type Device struct {
	Name    string `json:"name"`
	Class   string `json:"class"`
	Code    int    `json:"code"`
	Problem string `json:"problem"`
}

// Driver is an installed driver for an important kind of hardware.
type Driver struct {
	Device       string `json:"device"`
	Class        string `json:"class"`
	Version      string `json:"version"`
	Date         string `json:"date"`
	Manufacturer string `json:"manufacturer"`
}

// Update is an app winget can update.
type Update struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Available string `json:"available"`
}

// ProblemText explains a Device Manager error code in plain words.
func ProblemText(code int) string {
	switch code {
	case 1, 18:
		return "Needs its driver reinstalled."
	case 3:
		return "The driver may be damaged or the PC is low on memory."
	case 10:
		return "The device can't start. Usually a driver problem."
	case 12:
		return "Not enough free resources for this device."
	case 14:
		return "Restart the PC to finish setting up this device."
	case 19, 39:
		return "Windows can't load the driver. Reinstall it."
	case 21, 47:
		return "Windows is removing or has stopped this device."
	case 22:
		return "This device is turned off in Device Manager."
	case 24, 45:
		return "The device isn't connected right now."
	case 28:
		return "No driver is installed."
	case 31:
		return "The driver is installed but Windows can't use it."
	case 32:
		return "The driver's service is disabled."
	case 37:
		return "The driver failed to start."
	case 43:
		return "Windows stopped this device because it reported problems."
	case 48:
		return "The driver is blocked because it has known problems."
	case 52:
		return "Windows can't verify the driver's signature."
	}
	return "Windows reports a problem with this device."
}

var validModes = map[string]string{"automatic": "auto", "manual": "demand", "disabled": "disabled"}

// ScMode converts a start mode name to the value sc.exe expects.
func ScMode(mode string) (string, bool) {
	v, ok := validModes[strings.ToLower(mode)]
	return v, ok
}

// ParseWinget reads the table printed by "winget upgrade". It works on any
// display language because it finds columns from the dashed rule under the
// header rather than the header words.
func ParseWinget(out string) []Update {
	var lines []string
	for _, raw := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		// Progress spinners use carriage returns; keep only the final text.
		if i := strings.LastIndex(raw, "\r"); i >= 0 {
			raw = raw[i+1:]
		}
		lines = append(lines, strings.TrimRight(raw, " "))
	}
	ruleAt := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if len(t) > 10 && strings.Trim(t, "-") == "" && i > 0 {
			ruleAt = i
			break
		}
	}
	if ruleAt < 0 {
		return nil
	}
	header := []rune(lines[ruleAt-1])
	// Column starts: a non-space after a space (or at 0).
	var starts []int
	for i, r := range header {
		if r != ' ' && (i == 0 || header[i-1] == ' ') {
			starts = append(starts, i)
		}
	}
	if len(starts) < 4 {
		return nil
	}
	col := func(row []rune, i int) string {
		if starts[i] >= len(row) {
			return ""
		}
		end := len(row)
		if i+1 < len(starts) && starts[i+1] < end {
			end = starts[i+1]
		}
		return strings.TrimSpace(string(row[starts[i]:end]))
	}
	var out2 []Update
	for _, l := range lines[ruleAt+1:] {
		if strings.TrimSpace(l) == "" {
			break
		}
		row := []rune(l)
		if utf8.RuneCountInString(l) < starts[3] {
			continue // summary lines like "3 upgrades available."
		}
		u := Update{Name: col(row, 0), ID: col(row, 1), Version: col(row, 2), Available: col(row, 3)}
		if u.ID == "" || strings.Contains(u.ID, " ") || u.Available == "" {
			continue
		}
		out2 = append(out2, u)
	}
	return out2
}
