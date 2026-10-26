# Real-Windows smoke test for Kempt. Runs on a GitHub Windows runner.
# 1. /auto clean against planted junk, checking what was deleted and kept.
# 2. Opens the window and drives it through UI Automation like a person would.
$ErrorActionPreference = "Stop"
$exe = (Resolve-Path "dist\Kempt.exe").Path
$shots = New-Item -ItemType Directory -Force -Path "smoke"
$failed = $false
$failures = New-Object System.Collections.Generic.List[string]
function Fail($msg) { Write-Host "FAIL: $msg" -ForegroundColor Red; $script:failed = $true; $script:failures.Add($msg.Substring(0, [Math]::Min(300, $msg.Length))) }
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
$log = Get-ChildItem "$env:LOCALAPPDATA\Kempt\logs\*.log" | Sort-Object LastWriteTime | Select-Object -Last 1
Write-Host "----- /auto log (summary lines) -----"; Get-Content $log.FullName | Where-Object { $_ -notmatch '^deleted ' } | Select-Object -First 80; Write-Host "---------------------"

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
    # Prefer real controls (buttons, tabs, checkboxes) over plain text with the same words.
    $n = New-Object System.Windows.Automation.PropertyCondition($AE::NameProperty, $name)
    foreach ($type in "Button", "CheckBox", "TabItem", "Hyperlink") {
        $t = New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, [System.Windows.Automation.ControlType]::$type)
        $el = $root.FindFirst($TS::Descendants, (New-Object System.Windows.Automation.AndCondition($n, $t)))
        if ($el) { return $el }
    }
    return $root.FindFirst($TS::Descendants, $n)
}
function Texts($root) {
    $c = New-Object System.Windows.Automation.PropertyCondition($AE::ControlTypeProperty, [System.Windows.Automation.ControlType]::Text)
    return ($root.FindAll($TS::Descendants, $c) | ForEach-Object { $_.Current.Name }) -join " | "
}
function NameLike($root, $regex) {
    $all = $root.FindAll($TS::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
    foreach ($el in $all) { if ($el.Current.Name -match $regex) { return $el.Current.Name } }
    return $null
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
$win = Wait-For { $c = New-Object System.Windows.Automation.PropertyCondition($AE::NameProperty, "Kempt"); $AE::RootElement.FindFirst($TS::Children, $c) } 40
if (-not $win) { Shot "no-window"; Fail "window never appeared"; if ($gui.HasExited) { Fail "process exited with $($gui.ExitCode)" }; exit 1 }
Pass "window opened"
Start-Sleep 3
if ($gui.HasExited) { Fail "Kempt closed by itself"; exit 1 }

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

Click $win "Registry" | Out-Null
# Plant a startup entry that points to a program that doesn't exist.
$runKey = "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run"
if (-not (Test-Path $runKey)) { New-Item -Path $runKey | Out-Null }
New-ItemProperty -Path $runKey -Name "CPCTestBroken" -Value '"C:\Program Files\CPCMissingApp\gone.exe" /tray' -PropertyType String -Force | Out-Null
if (Expect $win "Fix issues in the Windows Registry" "Registry tab rendered") {
    Click $win "Select all" | Out-Null
    Click $win "Scan for issues" | Out-Null
    if (Expect $win "(\d+ issues? found|No issues found)" "registry scan finished" 120) {
        Shot "5b-registry"
        $t = Texts $win
        if ($t -match "CPCTestBroken") { Pass "planted broken startup entry was found" } else { Fail "planted broken startup entry not found. Screen: $t" }
        if ($t -match "MuxSvcHost|VsGraphicsHelper") { Fail "Windows-owned entries were flagged" } else { Pass "no Windows-owned false positives" }
        if ($t -match "\d+ issues? found") {
            Click $win "Fix selected issues" | Out-Null
            Click $win "Fix issues" | Out-Null
            if (Expect $win "Fixed \d+ issue" "registry fix finished with a backup" 120) {
                if ((Get-ItemProperty $runKey -Name "CPCTestBroken" -ErrorAction SilentlyContinue)) { Fail "broken entry still present after fix" } else { Pass "broken entry removed" }
                $bk = Get-ChildItem "$env:LOCALAPPDATA\Kempt\backups" -Directory | Sort-Object Name | Select-Object -Last 1
                $regs = @(Get-ChildItem "$($bk.FullName)\*.reg")
                if ($regs.Count -gt 0) { Pass "registry backup written ($($regs.Count) files)" } else { Fail "no registry backup files" }
                Click $win "Undo these changes" | Out-Null
                Click $win "Restore" | Out-Null
                if (Expect $win "Backup restored" "registry backup restored" 60) {
                    if ((Get-ItemProperty $runKey -Name "CPCTestBroken" -ErrorAction SilentlyContinue)) { Pass "undo put the entry back" } else { Fail "undo did not restore the entry" }
                }
            }
        }
    }
}
Remove-ItemProperty -Path $runKey -Name "CPCTestBroken" -ErrorAction SilentlyContinue
Click $win "Junk" | Out-Null

Click $win "Startup Manager" | Out-Null
if (Expect $win "(start with Windows|No startup apps found)" "Startup Manager rendered") { Shot "6-startup" }
Click $win "Scheduled tasks" | Out-Null
if (Expect $win "(tasks are on|No scheduled tasks)" "Scheduled tasks tab rendered" 60) { Write-Host "Tasks: $(Texts $win)" }
Click $win "Background services" | Out-Null
if (Expect $win "(services from other programs|No services from other programs)" "Background services tab rendered" 60) { Shot "6b-services" }
Click $win "Startup apps" | Out-Null

# Duplicate Finder: plant two identical files in Documents and remove one.
$docs = [Environment]::GetFolderPath("MyDocuments")
$blob = New-Object byte[] 2100000; (New-Object Random 7).NextBytes($blob)
New-Item -ItemType Directory -Force "$docs\cpc-dupes\a", "$docs\cpc-dupes\b" | Out-Null
[IO.File]::WriteAllBytes("$docs\cpc-dupes\a\report.bin", $blob); (Get-Item "$docs\cpc-dupes\a\report.bin").LastWriteTime = (Get-Date).AddDays(-10)
[IO.File]::WriteAllBytes("$docs\cpc-dupes\b\report copy.bin", $blob)
Click $win "Duplicate Finder" | Out-Null
Click $win "Search for duplicates" | Out-Null
if (Expect $win "(sets of duplicates|No duplicates found)" "duplicate search finished" 180) {
    Shot "6c-dupes"
    if ((Texts $win) -match "sets of duplicates") {
        Click $win "Keep the oldest copy" | Out-Null
        Click $win "Move to Recycle Bin" | Out-Null
        Click $win "Move files" | Out-Null
        Start-Sleep 3
        $a = Test-Path "$docs\cpc-dupes\a\report.bin"; $b = Test-Path "$docs\cpc-dupes\b\report copy.bin"
        if ($a -and -not $b) { Pass "duplicate moved to Recycle Bin, oldest copy kept" } else { Fail "duplicate removal wrong: oldest=$a newer=$b" }
    } else { Fail "planted duplicates were not found" }
}

Click $win "Software Updater" | Out-Null
if (Expect $win "(updates? available|Everything is up to date|winget isn't installed|Couldn't check)" "Software Updater finished checking" 240) { Shot "6d-updater"; Write-Host "Updater: $(Texts $win)" }

Click $win "Driver Updater" | Out-Null
if (Expect $win "(drivers? to update|has no driver updates|Couldn't check Windows Update)" "Driver Updater checked Windows Update" 360) {
    Shot "6e-drivers"
    $t = Texts $win
    Write-Host "Drivers: $($t.Substring(0, [Math]::Min(1500, $t.Length)))"
    $tab = Wait-For { NameLike $win "^Installed drivers \(\d+\)$" } 10
    if ($tab -and $tab -notmatch "\(0\)") { Pass "installed driver list loaded: $tab" } else { Fail "installed driver list empty or missing: $tab" }
    Click $win "Fix a problem" | Out-Null
    Expect $win "(has a problem|have a problem|No hardware problems found)" "Fix a problem shows device status" 20 | Out-Null
    Click $win "Close" | Out-Null
}

Click $win "Repair Tools" | Out-Null
if (Expect $win "Repair system files" "Repair Tools rendered" 20) {
    Shot "6f-repair"
    # Restart the print spooler is quick and harmless on a runner.
    $btn = Wait-For { $c = New-Object System.Windows.Automation.PropertyCondition($AE::NameProperty, "Run"); $all = $win.FindAll($TS::Descendants, $c); if ($all.Count -ge 9) { $all } } 10
    if ($btn) {
        # Buttons are in tool order; print spooler is the last tool.
        try { $btn[$btn.Count - 1].GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke(); Start-Sleep 1 } catch { Fail "could not start a repair tool: $_" }
        if (Expect $win "(Print spooler restarted|Finished|Done\.)" "repair tool ran and reported" 120) { Shot "6g-repair-done" }
    } else { Fail "repair Run buttons not found" }
}

Click $win "Bloatware" | Out-Null
if (Expect $win "(selected|Nothing to remove)" "Bloatware list loaded" 240) { Shot "6h-bloat"; Write-Host "Bloat: $((Texts $win).Substring(0, [Math]::Min(600, (Texts $win).Length)))" }

Click $win "Tech Report" | Out-Null
Click $win "Build report" | Out-Null
if (Expect $win "Report ready for" "tech report built" 240) {
    Shot "6i-report"
    $rpt = Get-ChildItem "$env:LOCALAPPDATA\Kempt\reports\*.html" | Sort-Object LastWriteTime | Select-Object -Last 1
    if ($rpt -and (Get-Content $rpt.FullName -Raw) -match "Kempt tech report") { Pass "report file written: $($rpt.Name)" } else { Fail "report html missing" }
    $txt = Get-ChildItem "$env:LOCALAPPDATA\Kempt\reports\*.txt" | Sort-Object LastWriteTime | Select-Object -Last 1
    Write-Host "----- report text (first 40 lines) -----"; Get-Content $txt.FullName | Select-Object -First 40; Write-Host "-----"
}
# /report from the command line
$cli = Start-Process $exe -ArgumentList "/report", "`"$env:RUNNER_TEMP\kempt-cli-report.txt`"" -PassThru -Wait
if ($cli.ExitCode -eq 0 -and (Test-Path "$env:RUNNER_TEMP\kempt-cli-report.txt")) { Pass "/report wrote a text report" } else { Fail "/report failed with $($cli.ExitCode)" }

Click $win "Health Check" | Out-Null
if (Expect $win "(Drive condition)" "health tiles rendered" 60) { Shot "6j-health" }

Click $win "Help" | Out-Null
Expect $win "What Kempt never touches" "Help rendered" | Out-Null

Click $win "Uninstaller" | Out-Null
if (Expect $win "programs" "Uninstaller rendered") { Shot "7-uninstaller" }

Click $win "Settings" | Out-Null
if (Expect $win "Automatic cleaning" "Settings rendered") { Shot "8-settings" }
Click $win "Turn automatic cleaning on or off" | Out-Null
$task = Wait-For { schtasks /query /tn "Kempt weekly clean" 2>$null } 15
if ($task) { Pass "weekly scheduled task created" } else { Fail "scheduled task not created. Screen: $(Texts $win)" }
Click $win "Turn automatic cleaning on or off" | Out-Null
Start-Sleep 2
schtasks /query /tn "Kempt weekly clean" 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) { Pass "weekly scheduled task removed" } else { Fail "scheduled task still there after turning off" }

if ($gui.HasExited) { Fail "Kempt crashed during the test" } else { Pass "Kempt still running at the end"; Stop-Process -Id $gui.Id }

if ($failed) { Write-Host "SMOKE TEST FAILED:"; $failures | ForEach-Object { Write-Host " - $_" }; exit 1 }
Write-Host "SMOKE TEST PASSED"
exit 0
