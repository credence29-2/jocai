# Credence PC Cleaner (CPC)

A small, no-nonsense replacement for CCleaner. One portable `CPC.exe`, no installer, no ads, no upsells, no background service, nothing phoning home.

## What it does

**Cleaner tab.** Tick what you want, click **Analyze** to see how much space you'd get back, then **Run Cleaner**. Nothing is deleted until you click Run Cleaner and confirm.

| Section | Items |
| --- | --- |
| Windows | Temp files (yours and Windows'), Recycle Bin, thumbnail cache, error reports, DNS cache, memory dumps, Windows Update download cache, Delivery Optimization cache, old Windows logs, graphics shader cache, Prefetch, Recent documents list |
| Browsers | Chrome, Edge, Brave, Vivaldi: cache, cookies, history. Firefox: cache, cookies |
| Applications | Microsoft Teams (classic and new), Discord, Spotify, Zoom logs |

"Recommended" ticks only the items that are safe for anyone: temp files, Recycle Bin, thumbnails, error reports, DNS, and browser caches. Cookies, history and anything that makes the PC slower afterwards are off unless you tick them.

**Startup tab.** Lists everything that launches at sign-in (registry Run keys and Startup folders) and lets you enable or disable it. CPC uses the same switch Task Manager uses, so nothing is deleted and Task Manager shows the same state. Double-click a row to toggle it.

**Programs tab.** Searchable list of installed programs with publisher, version, size and install date. **Uninstall...** launches the program's own uninstaller.

## Safety rules built in

- Temp files newer than 24 hours are left alone so running installers aren't broken.
- Files that are in use are skipped and counted, never forced.
- Links, junctions and other reparse points are never followed.
- Every location is checked before cleaning. If a path doesn't resolve properly, or resolves to a drive root or a top level folder, it's refused.
- Bookmarks, saved passwords, and Firefox history (stored in the same file as bookmarks) are never touched.
- If a browser or app is open, CPC tells you which ones before cleaning.
- Every clean writes a log of each deleted file to `%LOCALAPPDATA%\CredencePCCleaner\logs` (the last 30 are kept). The **Open log** button appears after a clean.

## What it deliberately doesn't do

No registry cleaner. Registry cleaning doesn't make Windows measurably faster and is the most common way cleaners break PCs. Microsoft doesn't support it either. No "driver updater" and no "performance booster" either.

## Running it

Just run `CPC.exe`. It asks for administrator rights (UAC) if your account is an admin. Standard users can still run it, and admin-only items are greyed out.

Windows will show a SmartScreen "Windows protected your PC" warning the first time because the exe isn't code-signed. Click **More info** then **Run anyway**. See "Code signing" below to get rid of that.

### Scheduled or remote cleaning

`CPC.exe /auto` runs a clean with no window, using the items last ticked in the app (or the Recommended set if it's never been opened). Anything whose program is open at the time is skipped. It writes the same log and exits with 0 on success, 1 if some items errored.

Weekly clean at 3 AM Sunday, from an admin command prompt:

```
schtasks /create /tn "CPC weekly clean" /tr "\"C:\Tools\CPC.exe\" /auto" /sc weekly /d SUN /st 03:00 /rl highest
```

This also works from an RMM tool or remote shell.

Settings live in `%LOCALAPPDATA%\CredencePCCleaner\settings.json`.

## Building

You need Go 1.24 or newer. Go builds the Windows exe from Windows, Mac or Linux. Nothing else is needed.

Windows (PowerShell):

```
.\build.ps1
```

Mac or Linux:

```
./build.sh
```

Output goes to `dist\CPC.exe` (64-bit Intel/AMD) and `dist\CPC-arm64.exe` (Windows on ARM).

The icon, manifest and version info are pre-built into `rsrc_windows_*.syso`. To change them, edit `winres/winres.json` or the icon PNGs and run:

```
go install github.com/tc-hib/go-winres@latest
go-winres make --in winres/winres.json --arch amd64,arm64
```

Tests run anywhere with `go test ./...`.

GitHub Actions builds both exes on every push that touches `cpc/`. Download them from the run's **Artifacts** section.

## Code signing

Unsigned exes trigger SmartScreen and some antivirus tools are suspicious of anything that deletes files in bulk. If CPC goes out to client or customer PCs, sign it. An OV code signing certificate runs roughly $200 to $400 a year. Azure Trusted Signing is cheaper if you qualify. Then:

```
signtool sign /fd sha256 /tr http://timestamp.digicert.com /td sha256 /a dist\CPC.exe
```

## Layout

| Path | What's in it |
| --- | --- |
| `internal/clean` | Cleaning engine, safety checks, and the list of clean targets |
| `internal/startup` | Startup items and the Task Manager compatible enable/disable |
| `internal/programs` | Installed programs and uninstall launching |
| `internal/settings` | Saved selection and clean logs |
| `internal/sysutil` | Win32 calls: elevation, Recycle Bin, DNS flush, process list |
| `*_windows.go` | The window and its three tabs, plus `/auto` mode |

To add a new clean target, add an entry to `internal/clean/targets.go`. The tests check every target for obvious mistakes like a missing description or an unfiltered rule on a whole user folder.
