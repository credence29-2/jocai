package health

import (
	"time"

	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// Disks reads every physical drive with its SMART-derived counters.
func Disks() ([]Disk, error) {
	var out []Disk
	err := sysutil.PowerShellJSONTimeout(`Get-PhysicalDisk | ForEach-Object {
	  $r = $null; try { $r = $_ | Get-StorageReliabilityCounter -ErrorAction Stop } catch {}
	  [pscustomobject]@{ name=[string]$_.FriendlyName; model=[string]$_.Model; mediaType=[string]$_.MediaType; bus=[string]$_.BusType; size=[int64]$_.Size;
	    status=[string]$_.HealthStatus;
	    wear=$(if ($r -and $r.Wear -ne $null) { [int]$r.Wear } else { -1 });
	    temperature=$(if ($r -and $r.Temperature) { [int]$r.Temperature } else { 0 });
	    powerOnHours=$(if ($r -and $r.PowerOnHours) { [int64]$r.PowerOnHours } else { 0 });
	    readErrors=$(if ($r -and $r.ReadErrorsUncorrected) { [int64]$r.ReadErrorsUncorrected } else { 0 });
	    writeErrors=$(if ($r -and $r.WriteErrorsUncorrected) { [int64]$r.WriteErrorsUncorrected } else { 0 }) } } | ConvertTo-Json -Compress`, 90*time.Second, &out)
	for i := range out {
		if out[i].Name == "" {
			out[i].Name = out[i].Model
		}
		out[i].Rate()
	}
	return out, err
}

// BatteryInfo reads the battery, if the PC has one.
func BatteryInfo() Battery {
	var out []struct {
		Present bool  `json:"present"`
		Charge  int   `json:"charge"`
		Design  int64 `json:"design"`
		Full    int64 `json:"full"`
		Cycles  int   `json:"cycles"`
		Status  int   `json:"status"`
	}
	_ = sysutil.PowerShellJSONTimeout(`$b = Get-CimInstance Win32_Battery -ErrorAction SilentlyContinue | Select-Object -First 1
if (-not $b) { [pscustomobject]@{ present=$false } | ConvertTo-Json -Compress; exit }
$design = 0; $full = 0; $cycles = 0
try { $design = [int64](Get-CimInstance -Namespace root\wmi -ClassName BatteryStaticData -ErrorAction Stop | Select-Object -First 1).DesignedCapacity } catch {}
try { $full = [int64](Get-CimInstance -Namespace root\wmi -ClassName BatteryFullChargedCapacity -ErrorAction Stop | Select-Object -First 1).FullChargedCapacity } catch {}
try { $cycles = [int](Get-CimInstance -Namespace root\wmi -ClassName BatteryCycleCount -ErrorAction Stop | Select-Object -First 1).CycleCount } catch {}
[pscustomobject]@{ present=$true; charge=[int]$b.EstimatedChargeRemaining; design=$design; full=$full; cycles=$cycles; status=[int]$b.BatteryStatus } | ConvertTo-Json -Compress`, 60*time.Second, &out)
	b := Battery{}
	if len(out) > 0 && out[0].Present {
		b = Battery{Present: true, Charge: out[0].Charge, DesignCapacity: out[0].Design, FullCapacity: out[0].Full, CycleCount: out[0].Cycles}
		switch out[0].Status {
		case 2, 6, 7, 8, 9:
			b.Status = "Charging"
		case 1, 3, 4, 5:
			b.Status = "On battery"
		default:
			b.Status = "Unknown"
		}
	}
	b.Rate()
	return b
}
