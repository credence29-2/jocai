package clean

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

const day = 24 * time.Hour

// Section names. Browsers get their own tab in the UI.
const (
	SecWindows  = "Windows"
	SecBrowsers = "Browsers"
	SecApps     = "Applications"
)

// Group names, in the order the UI shows them.
const (
	GWindows   = "Windows"
	GSystem    = "System"
	GUpdates   = "Windows Update and logs"
	GApps      = "Applications"
	GStore     = "Windows Store"
	GMedia     = "Multimedia"
	GDownloads = "Windows Downloads"
	GAI        = "AI Tools"
	GUtilities = "Utilities"
	GAdvanced  = "Advanced"
)

// GroupOrder is the display order for groups.
var GroupOrder = []string{GWindows, GSystem, GUpdates, GApps, GStore, GMedia, GDownloads, GAI, GUtilities, GAdvanced,
	"Google Chrome", "Microsoft Edge", "Firefox", "Brave", "Vivaldi", "Opera"}

// electron returns the cache rules for an Electron or Chromium based app
// whose profile lives at base.
func electron(base string) []Rule {
	var r []Rule
	for _, d := range []string{"Cache", "Code Cache", "GPUCache", "DawnCache", "DawnGraphiteCache", "DawnWebGPUCache", `Service Worker\CacheStorage`, `Service Worker\ScriptCache`} {
		r = append(r, Rule{Root: base + `\` + d, Recursive: true})
	}
	return r
}

func app(id, group, name, desc string, procs []string, rules ...Rule) *Target {
	return &Target{ID: id, Group: group, Section: SecApps, Name: name, Description: desc, Processes: procs, Rules: rules}
}

// chromium builds the targets for a Chromium based browser. userData is the
// "User Data" folder holding the profiles.
func chromium(id, name, userData, exe string) []*Target {
	p := userData + `\*`
	procs := []string{exe}
	mk := func(suffix, label, desc string, def bool, rules ...Rule) *Target {
		return &Target{ID: id + "." + suffix, Group: name, Section: SecBrowsers, Name: name + " - " + label,
			Description: desc, Default: def, Processes: procs, Rules: rules}
	}
	cache := append(electron(p), Rule{Root: userData + `\ShaderCache`, Recursive: true},
		Rule{Root: userData + `\GrShaderCache`, Recursive: true}, Rule{Root: userData + `\GraphiteDawnCache`, Recursive: true})
	return []*Target{
		mk("cache", "Internet cache", "Cached pages, images and scripts. Sites load a little slower the first time afterwards.", true, cache...),
		mk("cookies", "Cookies", "Signs you out of websites.", false,
			Rule{Root: p, Files: []string{"Cookies", "Cookies-journal"}},
			Rule{Root: p + `\Network`, Files: []string{"Cookies", "Cookies-journal"}}),
		mk("history", "History", "Browsing and download history. Bookmarks are not touched.", false,
			Rule{Root: p, Files: []string{"History", "History-journal", "Visited Links", "Top Sites", "Top Sites-journal", "Shortcuts", "Shortcuts-journal"}}),
		mk("session", "Session", "The tabs the browser reopens on start.", false,
			Rule{Root: p + `\Sessions`, Recursive: true},
			Rule{Root: p, Files: []string{"Current Session", "Current Tabs", "Last Session", "Last Tabs"}}),
		mk("passwords", "Saved passwords", "Deletes every password saved in this browser on this PC. Passwords synced to your account come back. Use with care.", false,
			Rule{Root: p, Files: []string{"Login Data", "Login Data-journal", "Login Data For Account", "Login Data For Account-journal"}}),
	}
}

// AllTargets returns every cleanable item in display order.
func AllTargets() []*Target {
	t := []*Target{
		// ---- Windows (Explorer)
		{ID: "win.thumbs", Group: GWindows, Section: SecWindows, Name: "Thumbnail cache", Default: true,
			Description: "Explorer's picture previews. Windows rebuilds them as you browse folders.",
			Rules:       []Rule{{Root: `%LOCALAPPDATA%\Microsoft\Windows\Explorer`, Files: []string{"thumbcache_*.db"}}}},
		{ID: "win.recent", Group: GWindows, Section: SecWindows, Name: "Recent documents list",
			Description: "The shortcuts behind File Explorer's Recent list. The documents themselves are not touched.",
			Rules:       []Rule{{Root: `%APPDATA%\Microsoft\Windows\Recent`, Files: []string{"*.lnk"}}}},
		{ID: "win.jumplists", Group: GWindows, Section: SecWindows, Name: "Taskbar jump lists",
			Description: "Recent files shown when you right-click apps on the taskbar. Pinned items in jump lists are cleared too.",
			Rules: []Rule{
				{Root: `%APPDATA%\Microsoft\Windows\Recent\AutomaticDestinations`, Files: []string{"*.automaticDestinations-ms"}},
				{Root: `%APPDATA%\Microsoft\Windows\Recent\CustomDestinations`, Files: []string{"*.customDestinations-ms"}},
			}},
		{ID: "win.iconcache", Group: GWindows, Section: SecWindows, Name: "Icon cache",
			Description: "Fixes wrong or blank icons. Files Explorer has open are skipped and rebuilt on next sign-in.",
			Rules:       []Rule{{Root: `%LOCALAPPDATA%\Microsoft\Windows\Explorer`, Files: []string{"iconcache_*.db"}}}},

		// ---- System
		{ID: "win.temp.user", Group: GSystem, Section: SecWindows, Name: "Temporary files (your account)", Default: true,
			Description: "Leftovers in your Temp folder older than 24 hours. Newer files are kept so running installers are not disturbed.",
			Rules: []Rule{
				{Root: `%TEMP%`, Recursive: true, MinAge: day},
				{Root: `%LOCALAPPDATA%\Temp`, Recursive: true, MinAge: day},
			}},
		{ID: "win.temp.system", Group: GSystem, Section: SecWindows, Name: "Temporary files (Windows)", Default: true, NeedsAdmin: true,
			Description: "Windows' own Temp folder, older than 24 hours.",
			Rules:       []Rule{{Root: `%SystemRoot%\Temp`, Recursive: true, MinAge: day, Admin: true}}},
		{ID: "win.recycle", Group: GSystem, Section: SecWindows, Name: "Recycle Bin", Default: true,
			Description: "Permanently deletes everything in the Recycle Bin on all drives.", Custom: recycleBin{}},
		{ID: "win.clipboard", Group: GSystem, Section: SecWindows, Name: "Clipboard",
			Description: "Whatever you last copied.", Custom: clipboard{}},
		{ID: "win.wer", Group: GSystem, Section: SecWindows, Name: "Windows error reports", Default: true,
			Description: "Crash reports that were already sent or queued for Microsoft.",
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\Microsoft\Windows\WER`, Recursive: true},
				{Root: `%ProgramData%\Microsoft\Windows\WER\ReportArchive`, Recursive: true, Admin: true},
				{Root: `%ProgramData%\Microsoft\Windows\WER\ReportQueue`, Recursive: true, Admin: true},
				{Root: `%ProgramData%\Microsoft\Windows\WER\Temp`, Recursive: true, Admin: true},
			}},
		{ID: "win.dns", Group: GSystem, Section: SecWindows, Name: "DNS cache", Default: true,
			Description: "Same as running ipconfig /flushdns.", Custom: dnsCache{}},
		{ID: "win.dumps", Group: GSystem, Section: SecWindows, Name: "Memory dumps",
			Description: "Crash dump files. These can be several GB. Leave this off if you are troubleshooting blue screens or app crashes.",
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\CrashDumps`, Recursive: true},
				{Root: `%SystemRoot%\Minidump`, Recursive: true, Admin: true},
				{Root: `%SystemRoot%`, Files: []string{"MEMORY.DMP"}, Admin: true},
				{Root: `%SystemRoot%\LiveKernelReports`, Files: []string{"*.dmp"}, Recursive: true, Admin: true},
			}},
		{ID: "win.chkdsk", Group: GSystem, Section: SecWindows, Name: "Chkdsk file fragments", NeedsAdmin: true,
			Description: "Pieces of damaged files that Check Disk saved in FOUND folders. Almost never useful.",
			Rules:       []Rule{{Root: `%SystemDrive%\FOUND.*`, Files: []string{"*.chk"}, Shallow: true, Admin: true}}},

		// ---- Windows Update and logs
		{ID: "win.update", Group: GUpdates, Section: SecWindows, Name: "Windows Update download cache", NeedsAdmin: true,
			Description: "Update installers Windows already downloaded. Files Windows is using are skipped. Can free a lot of space.",
			Rules:       []Rule{{Root: `%SystemRoot%\SoftwareDistribution\Download`, Recursive: true, MinAge: day, Admin: true}}},
		{ID: "win.delivery", Group: GUpdates, Section: SecWindows, Name: "Delivery Optimization cache", NeedsAdmin: true,
			Description: "Update pieces Windows keeps to share with other PCs.",
			Rules:       []Rule{{Root: `%SystemRoot%\ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization\Cache`, Recursive: true, Admin: true}}},
		{ID: "win.logs", Group: GUpdates, Section: SecWindows, Name: "Old Windows log files", NeedsAdmin: true,
			Description: "Log files under the Windows folder older than 7 days.",
			Rules:       []Rule{{Root: `%SystemRoot%\Logs`, Files: []string{"*.log", "*.etl", "*.cab"}, Recursive: true, MinAge: 7 * day, Admin: true}}},
		{ID: "win.setuplogs", Group: GUpdates, Section: SecWindows, Name: "Windows setup and upgrade logs", NeedsAdmin: true,
			Description: "Logs left by Windows upgrades, older than 7 days.",
			Rules: []Rule{
				{Root: `%SystemRoot%\Panther`, Files: []string{"*.log", "*.etl", "*.xml"}, Recursive: true, MinAge: 7 * day, Admin: true},
				{Root: `%SystemRoot%\inf`, Files: []string{"setupapi.*.log"}, MinAge: 7 * day, Admin: true},
			}},
	}

	// ---- Applications
	t = append(t,
		app("app.teams", GApps, "Microsoft Teams", "Classic and new Teams cache. A common fix for Teams being slow or not loading. You stay signed in.",
			[]string{"teams.exe", "ms-teams.exe"},
			append(electron(`%APPDATA%\Microsoft\Teams`), electron(`%LOCALAPPDATA%\Packages\MSTeams_8wekyb3d8bbwe\LocalCache\Microsoft\MSTeams\EBWebView\*`)...)...),
		app("app.slack", GApps, "Slack", "Cached images and files from Slack. You stay signed in.", []string{"slack.exe"}, electron(`%APPDATA%\Slack`)...),
		app("app.discord", GApps, "Discord", "Cached images and media from Discord.", []string{"discord.exe"}, electron(`%APPDATA%\discord`)...),
		app("app.zoom", GApps, "Zoom logs", "Zoom's log files. Recordings are not touched.", []string{"zoom.exe"},
			Rule{Root: `%APPDATA%\Zoom\logs`, Recursive: true}),
		app("app.onedrive", GApps, "OneDrive logs", "OneDrive's own log files. Your synced files are not touched.", []string{"onedrive.exe"},
			Rule{Root: `%LOCALAPPDATA%\Microsoft\OneDrive\logs`, Recursive: true, MinAge: day}),
		app("app.office", GApps, "Microsoft Office temporary files", "Office's web cache and temp files. Documents and the upload queue are not touched.",
			[]string{"winword.exe", "excel.exe", "powerpnt.exe", "outlook.exe", "onenote.exe"},
			Rule{Root: `%LOCALAPPDATA%\Microsoft\Office\16.0\Wef`, Recursive: true},
			Rule{Root: `%LOCALAPPDATA%\Microsoft\Windows\INetCache\Content.MSO`, Recursive: true},
			Rule{Root: `%LOCALAPPDATA%\Microsoft\Windows\INetCache\Content.Word`, Recursive: true}),
		app("app.steam", GApps, "Steam web cache", "The cache for Steam's built-in browser. Games are not touched.", []string{"steam.exe", "steamwebhelper.exe"},
			Rule{Root: `%LOCALAPPDATA%\Steam\htmlcache`, Recursive: true}),
	)

	// ---- Windows Store apps
	t = append(t,
		&Target{ID: "store.temp", Group: GStore, Section: SecApps, Name: "Store apps temporary files", Default: true,
			Description: "Temp folders and web caches of apps installed from the Microsoft Store.",
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\Packages\*\TempState`, Recursive: true, MinAge: day},
				{Root: `%LOCALAPPDATA%\Packages\*\AC\Temp`, Recursive: true, MinAge: day},
				{Root: `%LOCALAPPDATA%\Packages\*\AC\INetCache`, Recursive: true},
			}},
		&Target{ID: "store.cache", Group: GStore, Section: SecApps, Name: "Microsoft Store cache",
			Description: "The Store app's own cache. Same as running wsreset, a common fix when the Store won't load.",
			Processes:   []string{"winstore.app.exe"},
			Rules:       []Rule{{Root: `%LOCALAPPDATA%\Packages\Microsoft.WindowsStore_8wekyb3d8bbwe\LocalCache`, Recursive: true}}},
	)

	// ---- Multimedia
	t = append(t,
		app("app.spotify", GMedia, "Spotify cache", "Streamed songs Spotify keeps locally. Downloaded playlists are stored elsewhere and are not touched.",
			[]string{"spotify.exe"}, Rule{Root: `%LOCALAPPDATA%\Spotify\Data`, Recursive: true}),
		app("media.vlc", GMedia, "VLC album art cache", "Cover art VLC downloaded.", []string{"vlc.exe"},
			Rule{Root: `%APPDATA%\vlc\art`, Recursive: true}),
		app("media.adobe", GMedia, "Adobe media cache", "Preview files Premiere, After Effects and Media Encoder rebuild when needed. Can be very large.",
			[]string{"adobe premiere pro.exe", "afterfx.exe", "adobe media encoder.exe"},
			Rule{Root: `%APPDATA%\Adobe\Common\Media Cache Files`, Recursive: true},
			Rule{Root: `%APPDATA%\Adobe\Common\Media Cache`, Recursive: true}),
	)

	// ---- Windows Downloads
	t = append(t,
		&Target{ID: "dl.installers", Group: GDownloads, Section: SecWindows, Name: "Old installers in Downloads",
			Description: "Setup files (.exe, .msi) in your Downloads folder older than 30 days. Installed programs don't need them.",
			Rules:       []Rule{{Root: `%USERPROFILE%\Downloads`, Files: []string{"*.exe", "*.msi", "*.msix", "*.appx"}, MinAge: 30 * day}}},
		&Target{ID: "dl.old", Group: GDownloads, Section: SecWindows, Name: "Everything in Downloads older than 90 days",
			Description: "Every file in your Downloads folder you haven't touched in 90 days. Check the folder before using this.",
			Rules:       []Rule{{Root: `%USERPROFILE%\Downloads`, Recursive: true, MinAge: 90 * day}}},
	)

	// ---- AI tools
	t = append(t,
		app("ai.claude", GAI, "Claude desktop cache", "Cached files from the Claude app. Chats are not touched.", []string{"claude.exe"}, electron(`%APPDATA%\Claude`)...),
		app("ai.chatgpt", GAI, "ChatGPT desktop cache", "Cached files from the ChatGPT app. Chats are stored online and are not touched.", []string{"chatgpt.exe"},
			electron(`%LOCALAPPDATA%\Packages\OpenAI.ChatGPT-Desktop_*\LocalCache\Roaming\ChatGPT`)...),
		app("ai.copilot", GAI, "Microsoft Copilot cache", "Web cache of the Copilot app.", []string{"copilot.exe"},
			electron(`%LOCALAPPDATA%\Packages\Microsoft.Copilot_*\LocalState\EBWebView\*`)...),
		app("ai.cursor", GAI, "Cursor cache", "Cache of the Cursor code editor. Projects and settings are not touched.", []string{"cursor.exe"},
			append(electron(`%APPDATA%\Cursor`), Rule{Root: `%APPDATA%\Cursor\CachedData`, Recursive: true})...),
	)

	// ---- Utilities
	t = append(t,
		app("util.vscode", GUtilities, "Visual Studio Code cache", "Editor caches and old logs. Extensions, settings and projects are not touched.", []string{"code.exe"},
			append(electron(`%APPDATA%\Code`), Rule{Root: `%APPDATA%\Code\CachedData`, Recursive: true},
				Rule{Root: `%APPDATA%\Code\CachedExtensionVSIXs`, Recursive: true}, Rule{Root: `%APPDATA%\Code\logs`, Recursive: true, MinAge: day})...),
		app("util.java", GUtilities, "Java cache", "Downloaded Java applets and web start files.", nil,
			Rule{Root: `%USERPROFILE%\AppData\LocalLow\Sun\Java\Deployment\cache`, Recursive: true}),
		app("util.npm", GUtilities, "npm and pip caches", "Package caches for developers. They download again when needed.", nil,
			Rule{Root: `%LOCALAPPDATA%\npm-cache\_cacache`, Recursive: true},
			Rule{Root: `%LOCALAPPDATA%\pip\cache`, Recursive: true}),
		app("util.nvidia", GUtilities, "NVIDIA installer leftovers", "Driver installer files NVIDIA leaves behind after updating.", nil,
			Rule{Root: `%SystemDrive%\NVIDIA\DisplayDriver`, Recursive: true, Admin: true},
			Rule{Root: `%ProgramData%\NVIDIA Corporation\Downloader`, Recursive: true, Admin: true}),
	)

	// ---- Advanced
	t = append(t,
		&Target{ID: "win.shader", Group: GAdvanced, Section: SecWindows, Name: "Graphics shader cache",
			Description: "DirectX, NVIDIA and AMD shader caches. Games may stutter briefly while they rebuild it.",
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\D3DSCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\NVIDIA\DXCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\NVIDIA\GLCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\AMD\DxCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\AMD\DxcCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\AMD\VkCache`, Recursive: true},
			}},
		&Target{ID: "win.prefetch", Group: GAdvanced, Section: SecWindows, Name: "Old prefetch data", NeedsAdmin: true,
			Description: "Prefetch entries for programs not run in 30 days. Recent ones are kept so apps still start fast.",
			Rules:       []Rule{{Root: `%SystemRoot%\Prefetch`, Files: []string{"*.pf"}, MinAge: 30 * day, Admin: true}}},
		&Target{ID: "win.componentstore", Group: GAdvanced, Section: SecWindows, Name: "Windows component store cleanup", NeedsAdmin: true,
			Description: "Removes superseded Windows update files with Microsoft's own DISM tool. Takes several minutes. Old updates can't be uninstalled afterwards.",
			Custom:      componentStore{}},
		&Target{ID: "win.old", Group: GAdvanced, Section: SecWindows, Name: "Old Windows installation (Windows.old)", NeedsAdmin: true,
			Description: "The previous Windows version kept after an upgrade, often 10 GB or more. Removing it means you can't roll back the upgrade.",
			Custom:      windowsOld{}},
	)

	t = append(t, chromium("chrome", "Google Chrome", `%LOCALAPPDATA%\Google\Chrome\User Data`, "chrome.exe")...)
	t = append(t, chromium("edge", "Microsoft Edge", `%LOCALAPPDATA%\Microsoft\Edge\User Data`, "msedge.exe")...)
	t = append(t, chromium("brave", "Brave", `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\User Data`, "brave.exe")...)
	t = append(t, chromium("vivaldi", "Vivaldi", `%LOCALAPPDATA%\Vivaldi\User Data`, "vivaldi.exe")...)
	ffProfiles := `%APPDATA%\Mozilla\Firefox\Profiles\*`
	ff := func(id, label, desc string, def bool, rules ...Rule) *Target {
		return &Target{ID: "firefox." + id, Group: "Firefox", Section: SecBrowsers, Name: "Firefox - " + label,
			Description: desc, Default: def, Processes: []string{"firefox.exe"}, Rules: rules}
	}
	t = append(t,
		ff("cache", "Internet cache", "Cached pages, images and scripts.", true,
			Rule{Root: `%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\cache2`, Recursive: true},
			Rule{Root: `%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\startupCache`, Recursive: true},
			Rule{Root: `%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\thumbnails`, Recursive: true}),
		ff("cookies", "Cookies", "Signs you out of websites.", false,
			Rule{Root: ffProfiles, Files: []string{"cookies.sqlite", "cookies.sqlite-wal", "cookies.sqlite-shm"}}),
		ff("forms", "Saved form data", "Text Firefox suggests in form fields.", false,
			Rule{Root: ffProfiles, Files: []string{"formhistory.sqlite", "formhistory.sqlite-wal", "formhistory.sqlite-shm"}}),
		ff("session", "Session", "The tabs Firefox reopens on start.", false,
			Rule{Root: ffProfiles + `\sessionstore-backups`, Recursive: true},
			Rule{Root: ffProfiles, Files: []string{"sessionstore.jsonlz4"}}),
		ff("passwords", "Saved passwords", "Deletes every password saved in Firefox on this PC. Use with care.", false,
			Rule{Root: ffProfiles, Files: []string{"logins.json", "logins-backup.json"}}),
	)
	return t
}

// Label is the item's name inside its group, e.g. "Cookies" under "Google Chrome".
func (t *Target) Label() string {
	return strings.TrimPrefix(t.Name, t.Group+" - ")
}

// ---- custom targets -------------------------------------------------------

type recycleBin struct{}

func (recycleBin) Scan(context.Context) Result {
	size, items, err := sysutil.RecycleBinInfo()
	if err != nil {
		return Result{Err: err}
	}
	return Result{Files: int(items), Bytes: size}
}

func (r recycleBin) Clean(ctx context.Context) Result {
	before := r.Scan(ctx)
	if err := sysutil.EmptyRecycleBin(); err != nil {
		return Result{Err: err}
	}
	return Result{Files: before.Files, Bytes: before.Bytes}
}

type dnsCache struct{}

func (dnsCache) Scan(context.Context) Result { return Result{Note: "Ready to flush"} }

func (dnsCache) Clean(context.Context) Result {
	if err := sysutil.FlushDNS(); err != nil {
		return Result{Err: fmt.Errorf("flush DNS: %w", err)}
	}
	return Result{Note: "Flushed"}
}

type clipboard struct{}

func (clipboard) Scan(context.Context) Result { return Result{Note: "Ready to clear"} }

func (clipboard) Clean(context.Context) Result {
	if err := sysutil.ClearClipboard(); err != nil {
		return Result{Err: fmt.Errorf("clear clipboard: %w", err)}
	}
	return Result{Note: "Cleared"}
}

func systemDrive() string {
	if d := os.Getenv("SystemDrive"); d != "" {
		return d + `\`
	}
	return `C:\`
}

type componentStore struct{}

func (componentStore) Scan(context.Context) Result {
	return Result{Note: "Ready to run. Size is only known afterwards."}
}

func (componentStore) Clean(ctx context.Context) Result {
	_, before, _ := sysutil.DiskSpace(systemDrive())
	out, err := sysutil.RunHidden("dism.exe", "/Online", "/Cleanup-Image", "/StartComponentCleanup", "/Quiet")
	if err != nil {
		return Result{Err: fmt.Errorf("DISM: %v %s", err, strings.TrimSpace(out))}
	}
	_, after, _ := sysutil.DiskSpace(systemDrive())
	freed := int64(after) - int64(before)
	if freed < 0 {
		freed = 0
	}
	return Result{Bytes: freed, Note: "Component store cleaned"}
}

type windowsOld struct{}

func (windowsOld) path() string { return filepath.Join(systemDrive(), "Windows.old") }

func (w windowsOld) Scan(ctx context.Context) Result {
	p := w.path()
	if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
		return Result{}
	}
	var res Result
	_ = filepath.WalkDir(p, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			return nil
		}
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				res.Files++
				res.Bytes += info.Size()
			}
		}
		return nil
	})
	return res
}

func (w windowsOld) Clean(ctx context.Context) Result {
	before := w.Scan(ctx)
	if before.Files == 0 {
		return Result{}
	}
	if err := sysutil.RemoveProtectedTree(w.path()); err != nil {
		return Result{Err: fmt.Errorf("remove Windows.old: %w", err)}
	}
	return before
}
