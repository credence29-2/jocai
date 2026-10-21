# Real-Windows smoke test for CPC. Runs on a GitHub Windows runner.
# 1. /auto clean against planted junk, checking what was deleted and kept.
# 2. Opens the window and drives it through UI Automation like a person would.
$ErrorActionPreference = "Stop"
$exe = (Resolve-Path "dist\CPC.exe").Path
$shots = New-Item -ItemType Directory -Force -Path "smoke"
$failed = $false
function Fail($msg) { Write-Host "FAIL: $msg" -ForegroundColor Red; $script:failed = $true }
function Pass($msg) { Write-Host "PASS: $msg" -ForegroundColor Green }

function Seed {
    $temp = [IO.Path]::GetTempPath()
    $old = (Get-Date).AddDays(-3)
    New-Item -ItemType Directory -Force "$temp\cpc-old-installer" | Out-Null
    foreach ($f in "cpc-old-a.tmp", "cpc-old-installer\setup.msi") {
        [IO.File]::WriteAllBytes("$temp\$f", (New-Object byte[] 2000000)); (Get-Item "$temp\$f").LastWriteTime = $old
    }
    (Get-Item "$temp\cpc-old-installer").LastWriteTime = $old
    [IO.File]::WriteAllBytes("$temp\cpc-fresh.tmp", (New-Object byte[] 1000))
    $ud = "$env:LOCALAPPDATA\Google\Chrome\User Data\Default"
    New-Item -ItemType Directory -Force "$ud\Cache\Cache_Data", "$ud\Network" | Out-Null
    1..3 | ForEach-Object { [IO.File]::WriteAllBytes("$ud\Cache\Cache_Data\f_$_", (New-Object byte[] 500000)) }
    Set-Content "$ud\Network\Cookies" "cookie"; Set-Content "$ud\Bookmarks" "bookmarks"
    return @{ Temp = $temp; UD = $ud }
}

function Check-Cleaned($p, $label) {
    if (Test-Path "$($p.Temp)\cpc-old-a.tmp") { Fail "$label old temp file still there" } else { Pass "$label removed old temp file" }
    if (Test-Path "$($p.Temp)\cpc-old-installer") { Fail "$label old emptied temp folder still there" } else { Pass "$label removed old temp folder" }
    if (Test-Path "$($p.Temp)\cpc-fresh.tmp") { Pass "$label kept fresh temp file" } else { Fail "$label deleted a fresh temp file" }
    if (Test-Path "$($p.UD)\Cache\Cache_Data\f_1") { Fail "$label Chrome cache still there" } else { Pass "$label cleared Chrome cache" }
    if ((Test-Path "$($p.UD)\Network\Cookies") -and (Test-Path "$($p.UD)\Bookmarks")) { Pass "$label kept cookies and bookmarks" } else { Fail "$label touched cookies or bookmarks" }
}

Write-Host "TEMP is $([IO.Path]::GetTempPath())"

# ---------------------------------------------------------------- /auto
$p = Seed
$proc = Start-Process $exe -ArgumentList "/auto" -PassThru -Wait
if ($proc.ExitCode -eq 0) { Pass "/auto exit code 0" } else { Fail "/auto exit code $($proc.ExitCode)" }
Check-Cleaned $p "/auto"
$log = Get-ChildItem "$env:LOCALAPPDATA\CredencePCCleaner\logs\*.log" | Sort-Object LastWriteTime | Select-Object -Last 1
Write-Host "----- /auto log -----"; Get-Content $log.FullName | Select-Object -First 60; Write-Host "---------------------"

# ---------------------------------------------------------------- GUI
Add-Type -AssemblyName UIAutomationClient, UIAutomationTypes, System.Windows.Forms, System.Drawing
$AE = [System.Windows.Automation.AutomationElement]
$TS = [System.Windows.Automation.TreeScope]

function Shot($name) {
    $b = [System.Windows.Forms.Screen]::PrimaryScreen.Bounds
    $bmp = New-Object System.Drawing.Bitmap $b.Width, $b.Height
    $g = [System.Drawing.Graphics]::FromImage($bmp); $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
    $bmp.Save("$shots\$name.png"); $g.Dispose(); $bmp.Dispose()
}
function Wait-For([scriptblock]$cond, [int]$seconds = 30) {
    $end = (Get-Date).AddSeconds($seconds)
    while ((Get-Date) -lt $end) { $r = & $cond; if ($r) { return $r }; Start-Sleep -Milliseconds 400 }
    return $null
}
function Find-Name($root, $name) {
    $c = New-Object System.Windows.Automation.PropertyCondition($AE::NameProperty, $name)
    return $root.FindFirst($TS::Descendants, $c)
}
function Texts($root) {
    $c = New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, [System.Windows.Automation.ControlType]::Text)
    return ($root.FindAll($TS::Descendants, $c) | ForEach-Object { $_.Current.Name }) -join " | "
}
function Click($root, $name) {
    $el = Wait-For { Find-Name $root $name } 20
    if (-not $el) { Fail "could not find '$name'"; return $false }
    try { $el.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke() }
    catch { try { $el.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern).Toggle() } catch { Fail "could not click '$name': $_"; return $false } }
    Start-Sleep -Milliseconds 600
    return $true
}
function Expect($root, $regex, $label, $seconds = 60) {
    $t = Wait-For { $x = Texts $root; if ($x -match $regex) { $x } } $seconds
    if ($t) { Pass "$label"; return $true }
    Fail "$label. Screen text was: $(Texts $root)"; return $false
}

$p = Seed
$gui = Start-Process $exe -PassThru
$win = Wait-For { $c = New-Object System.Windows.Automation.PropertyCondition($AE::NameProperty, "Credence PC Cleaner"); $AE::RootElement.FindFirst($TS::Children, $c) } 40
if (-not $win) { Shot "no-window"; Fail "window never appeared"; if ($gui.HasExited) { Fail "process exited with $($gui.ExitCode)" }; exit 1 }
Pass "window opened"
Start-Sleep 3
if ($gui.HasExited) { Fail "CPC closed by itself"; exit 1 }

if (Expect $win "Run a health check" "Health Check page rendered") { Shot "1-health" }
Write-Host "Sidebar admin text: " (Texts $win | Select-String -Pattern "administrator" -AllMatches | ForEach-Object { $_.Matches.Value } | Select-Object -First 1)

Click $win "Run Health Check" | Out-Null
if (Expect $win "(of junk can be removed|Your PC looks clean)" "Health Check scan finished" 120) { Shot "2-health-done" }

Click $win "Custom Clean" | Out-Null
Expect $win "Clean up system clutter" "Custom Clean page rendered" | Out-Null
Click $win "Scan now" | Out-Null
if (Expect $win " found" "Custom Clean scan finished" 120) { Shot "3-scanned" }
Write-Host "Scan result text: $(Texts $win)"

Click $win "Clean and fix" | Out-Null
if (Expect $win "Clean selected items\?" "confirmation dialog shown" 20) { Shot "4-confirm" }
Click $win "Clean" | Out-Null
if (Expect $win "Freed " "clean finished" 180) { Shot "5-cleaned" }
Check-Cleaned $p "GUI clean"

Click $win "Browser" | Out-Null
Expect $win "Clear your browsing data|Google Chrome" "Browser tab rendered" | Out-Null

Click $win "Startup Manager" | Out-Null
if (Expect $win "(start with Windows|No startup apps found)" "Startup Manager rendered") { Shot "6-startup" }

Click $win "Uninstaller" | Out-Null
if (Expect $win "programs" "Uninstaller rendered") { Shot "7-uninstaller" }

Click $win "Settings" | Out-Null
if (Expect $win "Automatic cleaning" "Settings rendered") { Shot "8-settings" }
Click $win "Turn automatic cleaning on or off" | Out-Null
$task = Wait-For { schtasks /query /tn "Credence PC Cleaner weekly clean" 2>$null } 15
if ($task) { Pass "weekly scheduled task created" } else { Fail "scheduled task not created. Screen: $(Texts $win)" }
Click $win "Turn automatic cleaning on or off" | Out-Null
Start-Sleep 2
schtasks /query /tn "Credence PC Cleaner weekly clean" 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) { Pass "weekly scheduled task removed" } else { Fail "scheduled task still there after turning off" }

if ($gui.HasExited) { Fail "CPC crashed during the test" } else { Pass "CPC still running at the end"; Stop-Process -Id $gui.Id }

if ($failed) { Write-Host "SMOKE TEST FAILED"; exit 1 }
Write-Host "SMOKE TEST PASSED"
