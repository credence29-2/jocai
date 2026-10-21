package systools

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// Tasks lists scheduled tasks outside Microsoft's own folder.
func Tasks() ([]Task, error) {
	var out []Task
	err := sysutil.PowerShellJSON(`Get-ScheduledTask | Where-Object { $_.TaskPath -notlike '\Microsoft\*' } | ForEach-Object {
	  [pscustomobject]@{ name=$_.TaskName; path=$_.TaskPath; state=[string]$_.State; author=[string]$_.Author;
	    action=(($_.Actions | ForEach-Object { ($_.Execute + ' ' + $_.Arguments).Trim() }) -join '; ') } } | ConvertTo-Json -Compress`, &out)
	for i := range out {
		out[i].Enabled = !strings.EqualFold(out[i].State, "Disabled")
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, err
}

func hidden(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	b, err := cmd.CombinedOutput()
	if err != nil {
		return string(b), errors.New(strings.TrimSpace(string(b)))
	}
	return string(b), nil
}

// SetTask enables or disables a scheduled task by its full path.
func SetTask(fullPath string, enabled bool) error {
	if strings.HasPrefix(strings.ToLower(fullPath), `\microsoft\`) {
		return errors.New("Windows' own tasks can't be changed here.")
	}
	flag := "/disable"
	if enabled {
		flag = "/enable"
	}
	_, err := hidden("schtasks.exe", "/change", "/tn", fullPath, flag)
	return err
}

var microsoftPath = regexp.MustCompile(`(?i)\\Windows\\|\\Microsoft\\|\\Microsoft[ .]|\\Windows Defender\\`)

// Services lists services installed by programs, leaving out Windows' own.
func Services() ([]Service, error) {
	var all []Service
	err := sysutil.PowerShellJSON(`Get-CimInstance Win32_Service | ForEach-Object {
	  [pscustomobject]@{ name=$_.Name; displayName=$_.DisplayName; startMode=[string]$_.StartMode; state=[string]$_.State;
	    path=[string]$_.PathName; description=[string]$_.Description } } | ConvertTo-Json -Compress`, &all)
	var out []Service
	for _, s := range all {
		if s.Path == "" || microsoftPath.MatchString(s.Path) {
			continue
		}
		if s.StartMode == "Auto" {
			s.StartMode = "Automatic"
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].DisplayName) < strings.ToLower(out[j].DisplayName) })
	return out, err
}

// SetService changes a third-party service's start mode.
func SetService(name, mode string) error {
	v, ok := ScMode(mode)
	if !ok {
		return errors.New("Pick Automatic, Manual or Disabled.")
	}
	list, err := Services()
	if err != nil {
		return err
	}
	found := false
	for _, s := range list {
		if strings.EqualFold(s.Name, name) {
			found = true
		}
	}
	if !found {
		return errors.New("Only services installed by other programs can be changed here.")
	}
	_, err = hidden("sc.exe", "config", name, "start=", v)
	return err
}

// ProblemDevices lists hardware Device Manager flags with an error.
func ProblemDevices() ([]Device, error) {
	var out []Device
	err := sysutil.PowerShellJSON(`Get-CimInstance Win32_PnPEntity | Where-Object { $_.ConfigManagerErrorCode -ne 0 -and $_.Name } | ForEach-Object {
	  [pscustomobject]@{ name=$_.Name; class=[string]$_.PNPClass; code=[int]$_.ConfigManagerErrorCode } } | ConvertTo-Json -Compress`, &out)
	for i := range out {
		out[i].Problem = ProblemText(out[i].Code)
	}
	return out, err
}

// KeyDrivers lists drivers for graphics, network, audio, Bluetooth and
// cameras, oldest first.
func KeyDrivers() ([]Driver, error) {
	var out []Driver
	err := sysutil.PowerShellJSON(`Get-CimInstance Win32_PnPSignedDriver | Where-Object { $_.DeviceName -and $_.DeviceClass -in 'DISPLAY','NET','MEDIA','BLUETOOTH','CAMERA','IMAGE','PRINTER' } | ForEach-Object {
	  [pscustomobject]@{ device=$_.DeviceName; class=[string]$_.DeviceClass; version=[string]$_.DriverVersion;
	    date=$(if ($_.DriverDate) { $_.DriverDate.ToString('yyyy-MM-dd') } else { '' }); manufacturer=[string]$_.Manufacturer } } | ConvertTo-Json -Compress`, &out)
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, err
}

// WingetAvailable reports whether winget is installed.
func WingetAvailable() bool {
	_, err := hidden("winget.exe", "--version")
	return err == nil
}

// Updates lists apps winget can update.
func Updates() ([]Update, error) {
	out, err := hidden("winget.exe", "upgrade", "--source", "winget", "--accept-source-agreements", "--disable-interactivity")
	list := ParseWinget(out)
	if err != nil && len(list) == 0 && !strings.Contains(strings.ToLower(out), "no installed package") {
		// winget exits non-zero when nothing needs updating on some versions.
		if strings.Contains(out, "---") {
			return list, nil
		}
		return nil, fmt.Errorf("winget: %v", err)
	}
	return list, nil
}

var safeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)

// UpdateApp updates one app silently.
func UpdateApp(id string) error {
	if !safeID.MatchString(id) {
		return errors.New("unexpected app id")
	}
	out, err := hidden("winget.exe", "upgrade", "--id", id, "--exact", "--source", "winget", "--silent",
		"--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity")
	if err != nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		return errors.New(strings.TrimSpace(lines[len(lines)-1]))
	}
	return nil
}
