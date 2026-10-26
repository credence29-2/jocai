// Package repair runs the standard Windows fixes support techs reach for,
// with progress streamed back as it happens.
package repair

import (
	"bytes"
	"regexp"
	"strings"
	"unicode/utf16"
)

// Step is one command in a tool.
type Step struct {
	Name string
	Args []string
	// Optional: a step whose failure should not stop the tool.
	Optional bool
}

// Tool is one button on the Repair page.
type Tool struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	When        string `json:"when"`
	Minutes     string `json:"minutes"`
	NeedsAdmin  bool   `json:"needsAdmin"`
	Reboot      bool   `json:"reboot"`
	Steps       []Step `json:"-"`
}

// Tools in display order.
var Tools = []Tool{
	{ID: "sfc", Name: "Repair system files", Description: "Runs System File Checker, which checks every protected Windows file and replaces damaged ones from a known-good copy.",
		When: "Odd Windows errors, missing icons or dialogs, features that won't open.", Minutes: "5 to 20", NeedsAdmin: true,
		Steps: []Step{{"sfc.exe", []string{"/scannow"}, false}}},
	{ID: "dism", Name: "Repair the Windows image", Description: "Runs DISM RestoreHealth, which fixes the component store that System File Checker draws from. Run this if the first repair reports it couldn't fix everything.",
		When: "System File Checker failed, Windows Update errors, features that won't install.", Minutes: "10 to 30", NeedsAdmin: true,
		Steps: []Step{{"dism.exe", []string{"/Online", "/Cleanup-Image", "/RestoreHealth"}, false}}},
	{ID: "network", Name: "Reset the network stack", Description: "Resets Winsock and the TCP/IP settings, flushes DNS and renews the IP address. Same steps as the Windows network reset, without a restart.",
		When: "No internet while other devices are fine, POS terminal can't reach the back office, printers dropping off.", Minutes: "1", NeedsAdmin: true, Reboot: true,
		Steps: []Step{
			{"netsh.exe", []string{"winsock", "reset"}, false},
			{"netsh.exe", []string{"int", "ip", "reset"}, true},
			{"ipconfig.exe", []string{"/flushdns"}, true},
			{"ipconfig.exe", []string{"/release"}, true},
			{"ipconfig.exe", []string{"/renew"}, true},
		}},
	{ID: "wureset", Name: "Reset Windows Update", Description: "Stops the update services, clears the download cache, and starts them again. Windows re-downloads what it needs.",
		When: "Updates stuck downloading or failing with an error code.", Minutes: "1", NeedsAdmin: true,
		Steps: []Step{
			{"net.exe", []string{"stop", "wuauserv"}, true},
			{"net.exe", []string{"stop", "bits"}, true},
			{"net.exe", []string{"stop", "cryptsvc"}, true},
			{"cmd.exe", []string{"/c", `if exist "%SystemRoot%\SoftwareDistribution.old" rd /s /q "%SystemRoot%\SoftwareDistribution.old"`}, true},
			{"cmd.exe", []string{"/c", `ren "%SystemRoot%\SoftwareDistribution" SoftwareDistribution.old`}, true},
			{"cmd.exe", []string{"/c", `if exist "%SystemRoot%\System32\catroot2.old" rd /s /q "%SystemRoot%\System32\catroot2.old"`}, true},
			{"cmd.exe", []string{"/c", `ren "%SystemRoot%\System32\catroot2" catroot2.old`}, true},
			{"net.exe", []string{"start", "cryptsvc"}, true},
			{"net.exe", []string{"start", "bits"}, true},
			{"net.exe", []string{"start", "wuauserv"}, true},
		}},
	{ID: "icons", Name: "Rebuild the icon cache", Description: "Deletes the icon cache and restarts Explorer so Windows redraws every icon.",
		When: "Blank or wrong icons on the desktop or taskbar.", Minutes: "1",
		Steps: []Step{
			{"cmd.exe", []string{"/c", "taskkill /f /im explorer.exe"}, true},
			{"cmd.exe", []string{"/c", `del /a /q "%LOCALAPPDATA%\IconCache.db" "%LOCALAPPDATA%\Microsoft\Windows\Explorer\iconcache_*.db"`}, true},
			{"cmd.exe", []string{"/c", "start explorer.exe"}, false},
		}},
	{ID: "explorer", Name: "Restart Explorer", Description: "Restarts the Windows shell. The desktop and taskbar disappear for a second and come back.",
		When: "Taskbar frozen, Start menu not opening, desktop not refreshing.", Minutes: "under 1",
		Steps: []Step{
			{"cmd.exe", []string{"/c", "taskkill /f /im explorer.exe"}, true},
			{"cmd.exe", []string{"/c", "start explorer.exe"}, false},
		}},
	{ID: "chkdsk", Name: "Check the system drive", Description: "Runs a read-only disk check and reports whether a full repair is needed at the next restart.",
		When: "Files that won't open, random freezes, a drive that sounds busy for no reason.", Minutes: "2 to 10", NeedsAdmin: true,
		Steps: []Step{{"chkdsk.exe", []string{"/scan"}, false}}},
	{ID: "store", Name: "Reset the Microsoft Store", Description: "Clears the Store cache and re-registers it, the usual fix when the Store or Store apps won't open.",
		When: "Store opens blank, Store apps crash on launch.", Minutes: "1",
		Steps: []Step{
			{"wsreset.exe", []string{"-i"}, true},
			{"powershell.exe", []string{"-NoProfile", "-NonInteractive", "-Command", `Get-AppxPackage Microsoft.WindowsStore | ForEach-Object { Add-AppxPackage -DisableDevelopmentMode -Register "$($_.InstallLocation)\AppXManifest.xml" }`}, false},
		}},
	{ID: "printspool", Name: "Restart the print spooler", Description: "Restarts the print service and clears stuck jobs from the queue.",
		When: "Printer shows offline, jobs stuck in the queue, receipt printer not responding.", Minutes: "under 1", NeedsAdmin: true,
		Steps: []Step{
			{"net.exe", []string{"stop", "spooler"}, true},
			{"cmd.exe", []string{"/c", `del /q /f "%SystemRoot%\System32\spool\PRINTERS\*.*"`}, true},
			{"net.exe", []string{"start", "spooler"}, false},
		}},
}

// ByID finds a tool.
func ByID(id string) (Tool, bool) {
	for _, t := range Tools {
		if t.ID == id {
			return t, true
		}
	}
	return Tool{}, false
}

// Decode turns console output into text. sfc and some other tools write
// UTF-16, most write the OEM code page. The heuristic: lots of NUL bytes
// means UTF-16.
func Decode(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	}
	nul := bytes.Count(b, []byte{0})
	if len(b) > 1 && nul > len(b)/4 {
		if len(b)%2 == 1 {
			b = b[:len(b)-1]
		}
		u := make([]uint16, len(b)/2)
		for i := range u {
			u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
		}
		return string(utf16.Decode(u))
	}
	return string(b)
}

var percent = regexp.MustCompile(`(\d{1,3})(?:\.\d+)?\s*%`)

// Lines splits raw progress output into clean lines. Progress written with
// carriage returns is reduced to the last state of each line. Returns the
// lines and the last percentage seen, or -1.
func Lines(s string) (lines []string, pct int) {
	pct = -1
	for _, raw := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		if i := strings.LastIndex(raw, "\r"); i >= 0 {
			raw = raw[i+1:]
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if m := percent.FindStringSubmatch(raw); m != nil {
			var n int
			for _, c := range m[1] {
				n = n*10 + int(c-'0')
			}
			if n <= 100 {
				pct = n
			}
		}
		lines = append(lines, raw)
	}
	return lines, pct
}

// Summary gives the plain-language outcome for a tool from its output.
func Summary(id string, out string, exitOK bool) (ok bool, text string) {
	l := strings.ToLower(out)
	switch id {
	case "sfc":
		switch {
		case strings.Contains(l, "did not find any integrity violations"):
			return true, "No damaged system files found."
		case strings.Contains(l, "successfully repaired"):
			return true, "Damaged system files were found and repaired. Restart to finish."
		case strings.Contains(l, "unable to fix"):
			return false, "Some files couldn't be repaired. Run \"Repair the Windows image\", then run this again."
		case strings.Contains(l, "could not perform"):
			return false, "The scan couldn't run. Restart the PC and try again."
		}
	case "dism":
		switch {
		case strings.Contains(l, "the restore operation completed successfully"):
			return true, "The Windows image is healthy or was repaired."
		case strings.Contains(l, "0x800f081f"), strings.Contains(l, "source files could not be found"):
			return false, "DISM couldn't download repair files. Check the internet connection and Windows Update, then try again."
		}
	case "chkdsk":
		switch {
		case strings.Contains(l, "found no problems"), strings.Contains(l, "no further action is required"):
			return true, "No problems found on the drive."
		case strings.Contains(l, "errors found"), strings.Contains(l, "/f"), strings.Contains(l, "spotfix"):
			return false, "Problems were found. Windows will offer to repair the drive at the next restart."
		}
	case "network":
		return exitOK, "Network stack reset. Restart the PC to finish."
	case "wureset":
		return exitOK, "Windows Update cache cleared. Check for updates again."
	case "icons", "explorer":
		return exitOK, "Done."
	case "store":
		return exitOK, "Store reset. Open it again to check."
	case "printspool":
		return exitOK, "Print spooler restarted and the queue cleared."
	}
	if exitOK {
		return true, "Finished."
	}
	return false, "Finished with errors. See the output above."
}
