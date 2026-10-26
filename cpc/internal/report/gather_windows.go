package report

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/health"
	"github.com/credence29-2/jocai/cpc/internal/programs"
	"github.com/credence29-2/jocai/cpc/internal/startup"
	"github.com/credence29-2/jocai/cpc/internal/systools"
	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// Gather collects everything for the report. logsDir is Kempt's logs folder.
func Gather(app, version, company, logsDir string, elevated bool) *Report {
	r := &Report{App: app, Version: version, Company: company, Generated: time.Now(), Elevated: elevated}
	r.System = gatherSystem()
	r.Volumes = gatherVolumes()
	r.Disks, _ = health.Disks()
	r.Battery = health.BatteryInfo()
	if devs, err := systools.ProblemDevices(); err == nil {
		for _, d := range devs {
			r.Devices = append(r.Devices, Item{Name: d.Name, Detail: fmt.Sprintf("%s (code %d)", d.Problem, d.Code), Flag: "bad"})
		}
	}
	for _, e := range startup.List() {
		it := Item{Name: e.Name, Detail: e.Command + "  [" + e.Location + "]"}
		if !e.Enabled {
			it.Flag = "off"
			it.Detail = "disabled  " + it.Detail
		}
		r.Startup = append(r.Startup, it)
	}
	if svcs, err := systools.Services(); err == nil {
		for _, s := range svcs {
			r.Services = append(r.Services, Item{Name: s.DisplayName, Detail: s.StartMode + ", " + strings.ToLower(s.State)})
		}
	}
	for _, p := range programs.List() {
		d := p.Version
		if p.Publisher != "" {
			d += "  " + p.Publisher
		}
		if !p.InstallDate.IsZero() {
			d += "  (installed " + p.InstallDate.Format("Jan 2, 2006") + ")"
		}
		r.Programs = append(r.Programs, Item{Name: p.Name, Detail: strings.TrimSpace(d)})
	}
	r.Cleans = ReadCleans(logsDir, 5)
	if !elevated {
		r.Notes = append(r.Notes, "Run as administrator for drive condition, services and all-user startup entries.")
	}
	r.Assess()
	return r
}

func gatherSystem() System {
	var out []struct {
		Computer   string `json:"computer"`
		User       string `json:"user"`
		Maker      string `json:"maker"`
		Model      string `json:"model"`
		Serial     string `json:"serial"`
		OS         string `json:"os"`
		Version    string `json:"version"`
		Build      string `json:"build"`
		Installed  string `json:"installed"`
		LastBoot   string `json:"lastBoot"`
		LastUpdate string `json:"lastUpdate"`
		CPU        string `json:"cpu"`
		RAM        int64  `json:"ram"`
		Domain     string `json:"domain"`
		IP         string `json:"ip"`
		AV         string `json:"av"`
		Activated  string `json:"activated"`
	}
	_ = sysutil.PowerShellJSONTimeout(`$os = Get-CimInstance Win32_OperatingSystem; $cs = Get-CimInstance Win32_ComputerSystem; $cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$bios = Get-CimInstance Win32_BIOS; $cv = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
$ver = [string]$cv.DisplayVersion; if (-not $ver) { $ver = [string]$cv.ReleaseId }
$build = [string]$cv.CurrentBuild + '.' + [string]$cv.UBR
$hf = Get-CimInstance Win32_QuickFixEngineering | Where-Object { $_.InstalledOn } | Sort-Object InstalledOn -Descending | Select-Object -First 1
$lastUpd = if ($hf) { ([datetime]$hf.InstalledOn).ToString('yyyy-MM-dd') } else { '' }
$ip = (Get-NetIPConfiguration -ErrorAction SilentlyContinue | Where-Object { $_.IPv4DefaultGateway -and $_.NetAdapter.Status -eq 'Up' } | ForEach-Object { $_.InterfaceAlias + ' ' + $_.IPv4Address.IPAddress }) -join '; '
$av = ''; try { $av = (Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct -ErrorAction Stop | ForEach-Object { $_.displayName }) -join ', ' } catch {}
if (-not $av) { $av = 'None reported' }
$act = ''; try { $lic = Get-CimInstance SoftwareLicensingProduct -Filter "PartialProductKey IS NOT NULL AND ApplicationID='55c92734-d682-4d71-983e-d6ec3f16059f'" -ErrorAction Stop | Select-Object -First 1
  $act = if ($lic.LicenseStatus -eq 1) { 'Activated' } else { 'Not activated' } } catch { $act = 'Unknown' }
$dom = if ($cs.PartOfDomain) { 'Domain ' + $cs.Domain } else { 'Workgroup ' + $cs.Workgroup }
[pscustomobject]@{ computer=[string]$env:COMPUTERNAME; user=[string]$env:USERNAME; maker=[string]$cs.Manufacturer; model=[string]$cs.Model; serial=[string]$bios.SerialNumber;
  os=[string]$os.Caption; version=$ver; build=$build; installed=$os.InstallDate.ToString('yyyy-MM-dd'); lastBoot=$os.LastBootUpTime.ToString('yyyy-MM-dd HH:mm');
  lastUpdate=$lastUpd; cpu=[string]$cpu.Name; ram=[int64]$cs.TotalPhysicalMemory; domain=$dom; ip=$ip; av=$av; activated=$act } | ConvertTo-Json -Compress`, 2*time.Minute, &out)
	if len(out) == 0 {
		return System{Computer: os.Getenv("COMPUTERNAME"), User: os.Getenv("USERNAME")}
	}
	o := out[0]
	s := System{Computer: o.Computer, User: o.User, Maker: strings.TrimSpace(o.Maker), Model: strings.TrimSpace(o.Model), Serial: strings.TrimSpace(o.Serial),
		OS: strings.TrimPrefix(o.OS, "Microsoft "), Version: o.Version, Build: o.Build, Installed: o.Installed, LastBoot: o.LastBoot, LastUpdate: o.LastUpdate,
		CPU: strings.Join(strings.Fields(o.CPU), " "), RAMBytes: o.RAM, Domain: o.Domain, IP: o.IP, Antivirus: o.AV, Activated: o.Activated}
	if t, err := time.ParseInLocation("2006-01-02 15:04", o.LastBoot, time.Local); err == nil {
		d := time.Since(t)
		s.Uptime = fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	}
	return s
}

func gatherVolumes() []Volume {
	var out []Volume
	_ = sysutil.PowerShellJSONTimeout(`Get-CimInstance Win32_LogicalDisk -Filter "DriveType=3" | ForEach-Object {
	  [pscustomobject]@{ letter=[string]$_.DeviceID; label=[string]$_.VolumeName; fs=[string]$_.FileSystem; size=[int64]$_.Size; free=[int64]$_.FreeSpace } } | ConvertTo-Json -Compress`, 60*time.Second, &out)
	return out
}
