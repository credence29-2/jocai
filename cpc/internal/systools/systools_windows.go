package systools

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

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

// InstalledDrivers lists every driver that belongs to a named device.
func InstalledDrivers() ([]Driver, error) {
	var out []Driver
	err := sysutil.PowerShellJSON(`Get-CimInstance Win32_PnPSignedDriver | Where-Object { $_.DeviceName } | ForEach-Object {
	  [pscustomobject]@{ device=$_.DeviceName; class=[string]$_.DeviceClass; version=[string]$_.DriverVersion;
	    date=$(if ($_.DriverDate) { $_.DriverDate.ToString('yyyy-MM-dd') } else { '' }); manufacturer=[string]$_.Manufacturer;
	    provider=[string]$_.DriverProviderName; hardwareId=[string]$_.HardWareID } } | ConvertTo-Json -Compress`, &out)
	for i := range out {
		out[i].Group = DriverGroup(out[i].Class)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return strings.ToLower(out[i].Device) < strings.ToLower(out[j].Device)
	})
	return out, err
}

// SystemMaker returns the PC maker, e.g. "LENOVO" or "Dell Inc.".
func SystemMaker() string {
	var out []struct {
		Maker string `json:"maker"`
		Model string `json:"model"`
	}
	_ = sysutil.PowerShellJSON(`$c = Get-CimInstance Win32_ComputerSystem; [pscustomobject]@{ maker=[string]$c.Manufacturer; model=[string]$c.Model } | ConvertTo-Json -Compress`, &out)
	if len(out) == 0 {
		return ""
	}
	if strings.Contains(strings.ToLower(out[0].Model), "surface") {
		return "Surface"
	}
	return out[0].Maker
}

const wuSession = `$s = New-Object -ComObject Microsoft.Update.Session; $s.ClientApplicationID = 'Clear PC Cleaner'; `

const wuShape = `ForEach-Object { [pscustomobject]@{ id=[string]$_.Identity.UpdateID; title=[string]$_.Title; model=[string]$_.DriverModel;
  provider=[string]$_.DriverProvider; manufacturer=[string]$_.DriverManufacturer; class=[string]$_.DriverClass;
  date=$(if ($_.DriverVerDate) { ([datetime]$_.DriverVerDate).ToString('yyyy-MM-dd') } else { '' });
  size=[int64]$_.MaxDownloadSize; hardwareId=[string]$_.DriverHardwareID } }`

type wuResult struct {
	Updates []DriverUpdate `json:"updates"`
	Hidden  []DriverUpdate `json:"hidden"`
}

// DriverUpdates asks Windows Update which driver updates it has for this
// PC: offered ones and ones the user chose to ignore. This is the same
// catalog Settings shows under Optional updates.
func DriverUpdates() (offered, ignored []DriverUpdate, err error) {
	var out []wuResult
	err = sysutil.PowerShellJSONTimeout(wuSession+`$q = $s.CreateUpdateSearcher(); $q.Online = $true
$a = @($q.Search("IsInstalled=0 and Type='Driver' and IsHidden=0").Updates | `+wuShape+`)
$b = @($q.Search("IsInstalled=0 and Type='Driver' and IsHidden=1").Updates | `+wuShape+`)
[pscustomobject]@{ updates=$a; hidden=$b } | ConvertTo-Json -Compress -Depth 4`, 5*time.Minute, &out)
	if err != nil {
		return nil, nil, fmt.Errorf("Couldn't reach Windows Update: %v", err)
	}
	if len(out) == 0 {
		return nil, nil, nil
	}
	fix := func(list []DriverUpdate) []DriverUpdate {
		for i := range list {
			list[i].Group = DriverGroup(list[i].Class)
			if list[i].Model == "" {
				list[i].Model = list[i].Title
			}
		}
		sort.Slice(list, func(i, j int) bool {
			if list[i].Group != list[j].Group {
				return list[i].Group < list[j].Group
			}
			return list[i].Model < list[j].Model
		})
		return list
	}
	return fix(out[0].Updates), fix(out[0].Hidden), nil
}

// DriverInstallResult is what installing one driver update did.
type DriverInstallResult struct {
	OK      bool   `json:"ok"`
	Text    string `json:"text"`
	Reboot  bool   `json:"reboot"`
	HResult int64  `json:"hresult"`
}

// InstallDriverUpdate downloads and installs one driver update from
// Windows Update. Needs administrator rights.
func InstallDriverUpdate(id string) (DriverInstallResult, error) {
	if !ValidUpdateID(id) {
		return DriverInstallResult{}, errors.New("unexpected update id")
	}
	var out []struct {
		Code    int   `json:"code"`
		HResult int64 `json:"hresult"`
		Reboot  bool  `json:"reboot"`
	}
	err := sysutil.PowerShellJSONTimeout(`$ErrorActionPreference = 'Stop'; `+wuSession+`
$r = $s.CreateUpdateSearcher().Search("UpdateID='`+id+`'")
if ($r.Updates.Count -eq 0) { throw 'Windows Update no longer offers this driver.' }
$c = New-Object -ComObject Microsoft.Update.UpdateColl
$u = $r.Updates.Item(0); if (-not $u.EulaAccepted) { $u.AcceptEula() }; [void]$c.Add($u)
$d = $s.CreateUpdateDownloader(); $d.Updates = $c; $dr = $d.Download()
if ($dr.ResultCode -ne 2 -and $dr.ResultCode -ne 3) { throw "The download failed (code $($dr.ResultCode))." }
$i = $s.CreateUpdateInstaller(); $i.Updates = $c; $ir = $i.Install(); $g = $ir.GetUpdateResult(0)
[pscustomobject]@{ code=[int]$g.ResultCode; hresult=[int64]$g.HResult; reboot=[bool]$ir.RebootRequired } | ConvertTo-Json -Compress`, 20*time.Minute, &out)
	if err != nil {
		return DriverInstallResult{}, err
	}
	if len(out) == 0 {
		return DriverInstallResult{}, errors.New("Windows Update gave no result")
	}
	ok, text := InstallResultText(out[0].Code)
	return DriverInstallResult{OK: ok, Text: text, Reboot: out[0].Reboot, HResult: out[0].HResult}, nil
}

// SetDriverUpdateHidden ignores or un-ignores a driver update. Windows
// Update itself will then stop or start offering it.
func SetDriverUpdateHidden(id string, hidden bool) error {
	if !ValidUpdateID(id) {
		return errors.New("unexpected update id")
	}
	flag := "$false"
	if hidden {
		flag = "$true"
	}
	var out []struct{}
	return sysutil.PowerShellJSONTimeout(`$ErrorActionPreference = 'Stop'; `+wuSession+`
$r = $s.CreateUpdateSearcher().Search("UpdateID='`+id+`'")
if ($r.Updates.Count -eq 0) { throw 'Windows Update no longer offers this driver.' }
$r.Updates.Item(0).IsHidden = `+flag+`
'{}'`, 3*time.Minute, &out)
}

// RestorePoint creates a System Restore point. If Windows already made one
// in the last 24 hours it keeps that one, which is fine.
func RestorePoint(desc string) (string, error) {
	var out []struct {
		Note string `json:"note"`
	}
	err := sysutil.PowerShellJSONTimeout(`$ErrorActionPreference = 'Stop'
Checkpoint-Computer -Description '`+strings.ReplaceAll(desc, "'", "")+`' -RestorePointType MODIFY_SETTINGS -WarningVariable w -WarningAction SilentlyContinue
[pscustomobject]@{ note=[string]($w -join ' ') } | ConvertTo-Json -Compress`, 3*time.Minute, &out)
	if err != nil {
		msg := err.Error()
		if strings.Contains(strings.ToLower(msg), "disabled") || strings.Contains(msg, "0x80070422") {
			return "", errors.New("System Restore is turned off on this PC")
		}
		return "", fmt.Errorf("Couldn't create a restore point: %v", err)
	}
	if len(out) > 0 && strings.Contains(out[0].Note, "1440") {
		return "A restore point from the last 24 hours already exists.", nil
	}
	return "Restore point created.", nil
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
