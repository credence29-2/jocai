// Package systools covers the Windows tools behind CPC's Startup Manager
// tabs, Driver Check and Software Updater.
package systools

import (
	"regexp"
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

// Driver is an installed driver.
type Driver struct {
	Device       string `json:"device"`
	Class        string `json:"class"`
	Group        string `json:"group"`
	Version      string `json:"version"`
	Date         string `json:"date"`
	Manufacturer string `json:"manufacturer"`
	Provider     string `json:"provider"`
	HardwareID   string `json:"hardwareId"`
	HasUpdate    bool   `json:"hasUpdate"`
}

// DriverUpdate is a driver update Windows Update offers for this PC.
type DriverUpdate struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	Manufacturer string `json:"manufacturer"`
	Class        string `json:"class"`
	Group        string `json:"group"`
	Date         string `json:"date"`
	Size         int64  `json:"size"`
	HardwareID   string `json:"hardwareId"`
	// Current version and date of the installed driver it replaces, when known.
	CurrentVersion string `json:"currentVersion"`
	CurrentDate    string `json:"currentDate"`
}

// MakerLink is a manufacturer's own driver download page.
type MakerLink struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	URL  string `json:"-"`
}

// DriverGroup turns a Windows device class into the headings CCleaner
// style lists use.
func DriverGroup(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "media", "audioendpoint", "audioprocessingobject":
		return "Audio"
	case "bluetooth":
		return "Bluetooth"
	case "net", "netservice", "nettrans", "netclient":
		return "Connectivity"
	case "display", "monitor":
		return "Graphics"
	case "camera", "image":
		return "Camera"
	case "hidclass", "keyboard", "mouse", "biometric":
		return "Input devices"
	case "scsiadapter", "hdc", "diskdrive", "volume", "storagecontroller", "nvme":
		return "Storage"
	case "usb", "usbdevice":
		return "USB"
	case "printer", "printqueue":
		return "Printers"
	case "firmware":
		return "Firmware"
	case "system", "softwarecomponent", "extension", "processor", "securitydevices", "computer":
		return "System and chipset"
	}
	return "Other"
}

// splitIDs splits a list of hardware IDs as Windows reports them.
func splitIDs(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == '\n' || r == ' ' }) {
		if p = strings.ToUpper(strings.TrimSpace(p)); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// MatchUpdates fills in the installed version for each update and marks
// installed drivers that have one. Hardware IDs are matched exactly, or by
// the update's ID being the prefix of a more specific installed ID.
func MatchUpdates(updates []DriverUpdate, installed []Driver) {
	for ui := range updates {
		want := strings.ToUpper(strings.TrimSpace(updates[ui].HardwareID))
		if want == "" {
			continue
		}
		for di := range installed {
			for _, id := range splitIDs(installed[di].HardwareID) {
				if id == want || strings.HasPrefix(id, want+"&") {
					updates[ui].CurrentVersion = installed[di].Version
					updates[ui].CurrentDate = installed[di].Date
					installed[di].HasUpdate = true
					break
				}
			}
			if updates[ui].CurrentVersion != "" {
				break
			}
		}
	}
}

var makerPages = []struct {
	key, name, url string
	match          []string
}{
	{"intel", "Intel Driver & Support Assistant", "https://www.intel.com/content/www/us/en/support/detect.html", []string{"intel"}},
	{"amd", "AMD drivers and support", "https://www.amd.com/en/support/download/drivers.html", []string{"amd", "advanced micro devices", "ati technologies"}},
	{"nvidia", "NVIDIA drivers", "https://www.nvidia.com/en-us/drivers/", []string{"nvidia"}},
	{"realtek", "Realtek downloads", "https://www.realtek.com/Download/", []string{"realtek"}},
	{"dell", "Dell drivers and downloads", "https://www.dell.com/support/home/", []string{"dell"}},
	{"hp", "HP drivers and downloads", "https://support.hp.com/drivers", []string{"hp", "hewlett-packard", "hewlett packard"}},
	{"lenovo", "Lenovo drivers and downloads", "https://pcsupport.lenovo.com/", []string{"lenovo"}},
	{"surface", "Surface drivers and firmware", "https://support.microsoft.com/surface-drivers", []string{"surface"}},
	{"asus", "ASUS download center", "https://www.asus.com/support/download-center/", []string{"asus", "asustek"}},
	{"acer", "Acer drivers and manuals", "https://www.acer.com/support/drivers-and-manuals", []string{"acer"}},
	{"msi", "MSI drivers", "https://www.msi.com/support/download", []string{"micro-star", "msi"}},
}

// MakerLinks picks the maker download pages relevant to this PC from the
// system maker and the makers of installed drivers.
func MakerLinks(systemMaker string, installed []Driver) []MakerLink {
	names := []string{strings.ToLower(systemMaker)}
	for _, d := range installed {
		names = append(names, strings.ToLower(d.Manufacturer), strings.ToLower(d.Provider))
	}
	var out []MakerLink
	for _, m := range makerPages {
		hit := false
		for _, n := range names {
			for _, w := range m.match {
				if n == w || strings.HasPrefix(n, w+" ") || strings.HasPrefix(n, w+",") || strings.HasPrefix(n, w+"(") || strings.Contains(n, " "+w+" ") {
					hit = true
				}
			}
		}
		if hit {
			out = append(out, MakerLink{Key: m.key, Name: m.name, URL: m.url})
		}
	}
	return out
}

// MakerURL returns the download page for a key from MakerLinks.
func MakerURL(key string) (string, bool) {
	for _, m := range makerPages {
		if m.key == key {
			return m.url, true
		}
	}
	return "", false
}

var guid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidUpdateID reports whether s looks like a Windows Update ID.
func ValidUpdateID(s string) bool { return guid.MatchString(s) }

// InstallResultText explains a Windows Update result code.
func InstallResultText(code int) (ok bool, text string) {
	switch code {
	case 2:
		return true, "Installed"
	case 3:
		return true, "Installed with warnings"
	case 4:
		return false, "Windows Update couldn't install it"
	case 5:
		return false, "Installation was cancelled"
	}
	return false, "Didn't finish installing"
}

// Update is an app winget can update.
type Update struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Available string `json:"available"`
	// SelfUpdate marks apps that update through their own built-in updater,
	// where winget's installer is unreliable. The UI sends these to the
	// app's own update screen instead.
	SelfUpdate bool `json:"selfUpdate"`
}

// selfUpdating lists winget IDs whose winget installer usually fails
// because the app keeps itself current through its own update service.
var selfUpdating = map[string]string{
	"microsoft.edge":                      "edge",
	"microsoft.edgewebview2runtime":       "edge",
	"microsoft.edge.beta":                 "edge",
	"microsoft.edge.dev":                  "edge",
	"microsoft.teams":                     "teams",
	"microsoft.onedrive":                  "onedrive",
	"microsoft.visualstudio.enterprise":   "vs",
	"microsoft.visualstudio.professional": "vs",
	"microsoft.visualstudio.community":    "vs",
}

// SelfUpdater returns which built-in updater handles an app, or "".
func SelfUpdater(id string) string { return selfUpdating[strings.ToLower(id)] }

// MarkSelfUpdating flags the updates that should not go through winget.
func MarkSelfUpdating(list []Update) {
	for i := range list {
		list[i].SelfUpdate = SelfUpdater(list[i].ID) != ""
	}
}

// WingetErrorText turns winget's output for a failed upgrade into one plain
// sentence. It looks for the known error codes first, then falls back to
// the last line that mentions an error.
func WingetErrorText(out string) string {
	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "0x8a15002b"), strings.Contains(lower, "no available upgrade"):
		return "Already up to date according to winget."
	case strings.Contains(lower, "0x8a150011"), strings.Contains(lower, "hash does not match"), strings.Contains(lower, "hash mismatch"):
		return "The downloaded installer didn't match what winget expected. Try again later; the package may be mid-update."
	case strings.Contains(lower, "0x8a150010"), strings.Contains(lower, "no applicable installer"):
		return "winget has no installer for this PC's Windows version or architecture."
	case strings.Contains(lower, "1618"), strings.Contains(lower, "another installation is already in progress"):
		return "Another installer is running. Wait for it to finish, then try again."
	case strings.Contains(lower, "1603"), strings.Contains(lower, "0x80070643"):
		return "The app's installer failed (1603). Close the app and try again, or update it from inside the app."
	case strings.Contains(lower, "0x8a150014"), strings.Contains(lower, "installed version not available"), strings.Contains(lower, "install technology"):
		return "This app was installed a different way than winget expects. Update it from inside the app."
	case strings.Contains(lower, "0x80070005"), strings.Contains(lower, "access is denied"):
		return "Access was denied. Restart CPC as administrator and try again."
	case strings.Contains(lower, "0x8a150019"), strings.Contains(lower, "in use"):
		return "The app is open. Close it and try again."
	case strings.Contains(lower, "cancelled"), strings.Contains(lower, "canceled"):
		return "The installer was cancelled."
	}
	// Fall back to the most useful line.
	var last string
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.Trim(l, "-|\\/ ") == "" {
			continue
		}
		ll := strings.ToLower(l)
		if strings.Contains(ll, "error") || strings.Contains(ll, "fail") || strings.Contains(ll, "0x8") || strings.Contains(ll, "exit code") {
			return l
		}
		last = l
	}
	if last == "" {
		return "winget reported a failure without details."
	}
	return last
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
