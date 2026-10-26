package bloat

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

// List returns the removable Store apps for the current user.
func List() ([]App, error) {
	var raw []struct {
		Name      string `json:"name"`
		Family    string `json:"family"`
		Publisher string `json:"publisher"`
		Version   string `json:"version"`
		Label     string `json:"label"`
		Size      int64  `json:"size"`
	}
	err := sysutil.PowerShellJSONTimeout(`Get-AppxPackage | Where-Object { -not $_.IsFramework -and -not $_.NonRemovable -and $_.SignatureKind -ne 'System' } | ForEach-Object {
	  $label = ''; try { $m = Get-AppxPackageManifest $_ -ErrorAction Stop; $label = [string]$m.Package.Properties.DisplayName } catch {}
	  if ($label -like 'ms-resource:*') { $label = '' }
	  $size = 0; try { $size = (Get-ChildItem -LiteralPath $_.InstallLocation -Recurse -File -ErrorAction SilentlyContinue | Measure-Object Length -Sum).Sum } catch {}
	  [pscustomobject]@{ name=[string]$_.Name; family=[string]$_.PackageFamilyName; publisher=[string]$_.Publisher; version=[string]$_.Version; label=$label; size=[int64]$size } } | ConvertTo-Json -Compress`, 4*time.Minute, &raw)
	if err != nil {
		return nil, err
	}
	var apps []App
	for _, r := range raw {
		pub := r.Publisher
		if i := strings.Index(pub, "O="); i >= 0 {
			pub = pub[i+2:]
			if j := strings.Index(pub, ","); j >= 0 {
				pub = pub[:j]
			}
		}
		apps = append(apps, App{Name: r.Name, Label: r.Label, Publisher: pub, Version: r.Version, Size: r.Size})
	}
	return Prepare(apps), nil
}

// Remove uninstalls a Store app for the current user, and when elevated and
// allUsers is set, for every user and new accounts too.
func Remove(name string, allUsers bool) error {
	if !SafeName(name) {
		return errors.New("unexpected package name")
	}
	script := `$ErrorActionPreference='Stop'; $p = Get-AppxPackage -Name '` + name + `'; if (-not $p) { throw 'not installed' }
$p | Remove-AppxPackage`
	if allUsers {
		script += `
try { Get-AppxPackage -AllUsers -Name '` + name + `' | Remove-AppxPackage -AllUsers } catch {}
try { Get-AppxProvisionedPackage -Online | Where-Object { $_.DisplayName -eq '` + name + `' } | Remove-AppxProvisionedPackage -Online | Out-Null } catch {}`
	}
	script += `
'{}'`
	var out []struct{}
	if err := sysutil.PowerShellJSONTimeout(script, 3*time.Minute, &out); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not installed") {
			return nil
		}
		if i := strings.Index(msg, "HRESULT"); i >= 0 {
			msg = msg[i:]
		}
		return fmt.Errorf("%s", strings.TrimSpace(msg))
	}
	return nil
}
