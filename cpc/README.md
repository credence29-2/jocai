# Kempt (Kempt)

A CCleaner replacement for Windows. One portable `Kempt.exe`, no installer, no ads, no upsells, nothing phoning home.

## What's in it

| Section | What it does |
| --- | --- |
| Health Check | One-click scan of the Recommended items, plus startup app count and free disk space. Fix it cleans only the Recommended items. |
| Custom Clean, Junk tab | Windows, System, Windows Update and logs, Applications, Windows Store, Multimedia, Windows Downloads, AI Tools, Utilities and Advanced groups. Includes temp files, Recycle Bin, clipboard, caches, crash dumps, Windows.old and DISM component cleanup. |
| Custom Clean, Browser tab | Chrome, Edge, Firefox, Brave and Vivaldi: internet cache, cookies, history, session, saved form data and saved passwords. Only caches are ticked by default. |
| Custom Clean, Registry tab | Application paths, obsolete software, run at startup, MUI cache, shared DLLs, installer folders, help files, fonts, firewall rules and sound events. Every fix is backed up and can be undone. |
| Uninstaller | Installed programs with size and install date. Runs each program's own uninstaller. |
| Duplicate Finder | Byte-for-byte duplicate files in your folders, including OneDrive. Extras go to the Recycle Bin. One copy is always kept. |
| Startup Manager | Startup apps, scheduled tasks from other programs, and third-party background services. Nothing is deleted, so every change can be reversed. |
| Software Updater | Lists and installs app updates silently through winget, which ships with Windows. |
| Driver Check | Hardware with Device Manager errors, explained in plain words, plus the age of your key drivers and a shortcut to Windows Update's driver updates. |
| Settings | Weekly automatic cleaning through Task Scheduler, folders to never clean, logs. |

Nothing is deleted until you confirm. Every clean writes a log of each deleted file to `%LOCALAPPDATA%\Kempt\logs`. Registry backups go to `%LOCALAPPDATA%\Kempt\backups`.

## Safety rules built in

- Temp files newer than 24 hours are left alone so running installers aren't broken.
- Files in use are skipped and counted, never forced.
- Links, junctions and other reparse points are never followed.
- Every location is checked before cleaning. A path that doesn't resolve cleanly, or resolves to a drive root or top-level folder, is refused.
- Cookies, history, sessions, saved passwords, Downloads and Windows.old are always off until you tick them. A unit test enforces this.
- The registry cleaner only flags entries pointing to files that are definitely gone on a local drive that is present. It never touches COM, ActiveX, type libraries or services, and it understands 64-bit Windows' System32 redirection.
- Windows' own scheduled tasks and services are hidden and can't be changed.
- Driver Check never downloads drivers from third-party sites.

## What's deliberately left out

- **Cloud Drive Cleaner.** It needs sign-in to cloud accounts. Duplicate Finder does search your local OneDrive folder.
- **Third-party driver downloads.** That's where driver updaters cause the most trouble. Kempt points you to Windows Update and the maker's site.
- **Wipe free space.** It's slow, wears SSDs, and isn't cleaning.

## Running it

Run `Kempt.exe`. It asks for administrator rights (UAC) if your account is an admin. Standard users can still run it, and admin-only items are locked.

Kempt needs the Microsoft Edge WebView2 Runtime. It's built into Windows 11 and updated Windows 10. If it's missing, Kempt opens Microsoft's download page.

Windows shows a SmartScreen warning the first time because the exe isn't code-signed. Click **More info** then **Run anyway**.

### Scheduled or remote cleaning

Turn on **Settings > Automatic cleaning** for a weekly clean. Or run `Kempt.exe /auto` yourself, from Task Scheduler, a script or an RMM tool. It cleans the items last ticked in Custom Clean, or the Recommended set if it's never been opened. It skips anything whose program is open, honors the never-clean folders, writes a log, and exits with 0 on success.

## Building

You need Go 1.24 or newer. Go builds the Windows exe from Windows, Mac or Linux.

```
./build.sh        # Mac or Linux
.\build.ps1       # Windows PowerShell
```

Output goes to `dist\Kempt.exe` and `dist\Kempt-arm64.exe`.

The interface is `web/index.html`, embedded into the exe. To work on it in a normal browser, load `web/dev/mock.js` first. It fakes the backend.

The icon, manifest and version info are pre-built into `rsrc_windows_*.syso`. To change them, edit `winres/winres.json` and run:

```
go install github.com/tc-hib/go-winres@latest
go-winres make --in winres/winres.json --arch amd64,arm64
```

## Testing

- `go test ./...` runs the unit tests anywhere.
- The **Kempt Windows test** GitHub workflow runs on a real Windows machine. It runs the unit tests natively, cleans planted junk with `/auto`, and then drives the actual window through UI Automation. That covers Health Check, scan, clean and confirm, registry scan, fix and undo, Duplicate Finder, Startup Manager tabs, Software Updater, Driver Check, the weekly schedule switch, and Help.
- The **Kempt build** workflow produces the exes as a downloadable artifact on every push.

## Code signing

Unsigned exes trigger SmartScreen, and some antivirus tools are wary of anything that deletes files in bulk. If Kempt goes out to client PCs, sign it. An OV code signing certificate runs about $200 to $400 a year. Azure Trusted Signing is cheaper if you qualify.

```
signtool sign /fd sha256 /tr http://timestamp.digicert.com /td sha256 /a dist\Kempt.exe
```

## Layout

| Path | What's in it |
| --- | --- |
| `web/index.html` | The whole interface |
| `api.go`, `api_tools.go` | Functions the interface calls, and the events it receives |
| `main_windows.go` | Creates the WebView2 window |
| `auto_windows.go` | `/auto` mode |
| `internal/clean` | Cleaning engine, safety checks, and the list of clean targets |
| `internal/regclean` | Registry scan, backup, fix and restore |
| `internal/dupes` | Duplicate Finder |
| `internal/systools` | Scheduled tasks, services, drivers, winget |
| `internal/startup` | Startup apps, compatible with Task Manager |
| `internal/programs` | Installed programs and uninstall |
| `internal/settings` | Saved choices and logs |
| `internal/sysutil` | Win32 calls |

To add a clean item, add it to `internal/clean/targets.go`. The tests catch common mistakes, like a missing description or a risky item that's on by default.
