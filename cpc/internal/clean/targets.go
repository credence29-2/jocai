package clean

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/credence29-2/jocai/cpc/internal/sysutil"
)

const day = 24 * time.Hour

// Section names, in display order.
const (
	SecWindows  = "Windows"
	SecBrowsers = "Browsers"
	SecApps     = "Applications"
)

// chromium builds the cache, cookie and history targets for a Chromium based
// browser. userData is the "User Data" folder holding the profiles.
func chromium(id, name, userData, exe string) []*Target {
	p := userData + `\*`
	procs := []string{exe}
	return []*Target{
		{
			ID: id + ".cache", Group: name, Section: SecBrowsers, Name: name + " - Cache",
			Description: "Cached web pages, images and scripts. Safe to remove, sites just load a little slower the first time.",
			Default:     true, Processes: procs,
			Rules: []Rule{
				{Root: p + `\Cache`, Recursive: true},
				{Root: p + `\Code Cache`, Recursive: true},
				{Root: p + `\GPUCache`, Recursive: true},
				{Root: p + `\DawnGraphiteCache`, Recursive: true},
				{Root: p + `\DawnWebGPUCache`, Recursive: true},
				{Root: p + `\Service Worker\CacheStorage`, Recursive: true},
				{Root: p + `\Service Worker\ScriptCache`, Recursive: true},
				{Root: userData + `\ShaderCache`, Recursive: true},
				{Root: userData + `\GrShaderCache`, Recursive: true},
				{Root: userData + `\GraphiteDawnCache`, Recursive: true},
			},
		},
		{
			ID: id + ".cookies", Group: name, Section: SecBrowsers, Name: name + " - Cookies",
			Description: "Signs you out of websites. Off by default.",
			Processes:   procs,
			Rules: []Rule{
				{Root: p, Files: []string{"Cookies", "Cookies-journal"}},
				{Root: p + `\Network`, Files: []string{"Cookies", "Cookies-journal"}},
			},
		},
		{
			ID: id + ".history", Group: name, Section: SecBrowsers, Name: name + " - History",
			Description: "Browsing and download history. Bookmarks and saved passwords are not touched. Off by default.",
			Processes:   procs,
			Rules: []Rule{
				{Root: p, Files: []string{"History", "History-journal", "Visited Links", "Top Sites", "Top Sites-journal", "Shortcuts", "Shortcuts-journal"}},
			},
		},
	}
}

// AllTargets returns every cleanable item in display order.
func AllTargets() []*Target {
	t := []*Target{
		{
			ID: "win.temp.user", Group: "System", Section: SecWindows, Name: "Temporary files (your account)",
			Description: "Leftovers in your Temp folder older than 24 hours. Newer files are kept so running installers are not disturbed.",
			Default:     true,
			Rules: []Rule{
				{Root: `%TEMP%`, Recursive: true, MinAge: day},
				{Root: `%LOCALAPPDATA%\Temp`, Recursive: true, MinAge: day},
			},
		},
		{
			ID: "win.temp.system", Group: "System", Section: SecWindows, Name: "Temporary files (Windows)",
			Description: "Windows' own Temp folder, older than 24 hours.",
			Default:     true, NeedsAdmin: true,
			Rules: []Rule{{Root: `%SystemRoot%\Temp`, Recursive: true, MinAge: day, Admin: true}},
		},
		{
			ID: "win.recycle", Group: "System", Section: SecWindows, Name: "Recycle Bin",
			Description: "Permanently deletes everything in the Recycle Bin on all drives.",
			Default:     true, Custom: recycleBin{},
		},
		{
			ID: "win.thumbs", Group: "System", Section: SecWindows, Name: "Thumbnail cache",
			Description: "Explorer's picture previews. Windows rebuilds them as you browse folders.",
			Default:     true,
			Rules:       []Rule{{Root: `%LOCALAPPDATA%\Microsoft\Windows\Explorer`, Files: []string{"thumbcache_*.db"}}},
		},
		{
			ID: "win.wer", Group: "System", Section: SecWindows, Name: "Windows error reports",
			Description: "Crash reports that were already sent or queued for Microsoft.",
			Default:     true,
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\Microsoft\Windows\WER`, Recursive: true},
				{Root: `%ProgramData%\Microsoft\Windows\WER\ReportArchive`, Recursive: true, Admin: true},
				{Root: `%ProgramData%\Microsoft\Windows\WER\ReportQueue`, Recursive: true, Admin: true},
				{Root: `%ProgramData%\Microsoft\Windows\WER\Temp`, Recursive: true, Admin: true},
			},
		},
		{
			ID: "win.dns", Group: "System", Section: SecWindows, Name: "DNS cache",
			Description: "Same as running ipconfig /flushdns.",
			Default:     true, Custom: dnsCache{},
		},
		{
			ID: "win.dumps", Group: "Advanced", Section: SecWindows, Name: "Memory dumps",
			Description: "Crash dump files. These can be several GB. Leave this off if you are troubleshooting blue screens or app crashes.",
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\CrashDumps`, Recursive: true},
				{Root: `%SystemRoot%\Minidump`, Recursive: true, Admin: true},
				{Root: `%SystemRoot%`, Files: []string{"MEMORY.DMP"}, Admin: true},
				{Root: `%SystemRoot%\LiveKernelReports`, Files: []string{"*.dmp"}, Recursive: true, Admin: true},
			},
		},
		{
			ID: "win.update", Group: "Windows Update and logs", Section: SecWindows, Name: "Windows Update download cache",
			Description: "Update installers Windows already downloaded. Files Windows is using are skipped. Can free a lot of space.",
			NeedsAdmin:  true,
			Rules:       []Rule{{Root: `%SystemRoot%\SoftwareDistribution\Download`, Recursive: true, MinAge: day, Admin: true}},
		},
		{
			ID: "win.delivery", Group: "Windows Update and logs", Section: SecWindows, Name: "Delivery Optimization cache",
			Description: "Update pieces Windows keeps to share with other PCs.",
			NeedsAdmin:  true,
			Rules:       []Rule{{Root: `%SystemRoot%\ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization\Cache`, Recursive: true, Admin: true}},
		},
		{
			ID: "win.logs", Group: "Windows Update and logs", Section: SecWindows, Name: "Old Windows log files",
			Description: "Log files under the Windows folder older than 7 days.",
			NeedsAdmin:  true,
			Rules:       []Rule{{Root: `%SystemRoot%\Logs`, Files: []string{"*.log", "*.etl", "*.cab"}, Recursive: true, MinAge: 7 * day, Admin: true}},
		},
		{
			ID: "win.shader", Group: "Advanced", Section: SecWindows, Name: "Graphics shader cache",
			Description: "DirectX, NVIDIA and AMD shader caches. Games may stutter briefly while they rebuild it.",
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\D3DSCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\NVIDIA\DXCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\NVIDIA\GLCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\AMD\DxCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\AMD\DxcCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\AMD\VkCache`, Recursive: true},
			},
		},
		{
			ID: "win.prefetch", Group: "Advanced", Section: SecWindows, Name: "Prefetch data",
			Description: "Windows uses this to start apps faster. Clearing it usually makes the next boot slower, so it is off by default.",
			NeedsAdmin:  true,
			Rules:       []Rule{{Root: `%SystemRoot%\Prefetch`, Files: []string{"*.pf"}, Admin: true}},
		},
		{
			ID: "win.recent", Group: "Advanced", Section: SecWindows, Name: "Recent documents list",
			Description: "The shortcuts behind File Explorer's Recent list. The documents themselves are not touched.",
			Rules:       []Rule{{Root: `%APPDATA%\Microsoft\Windows\Recent`, Files: []string{"*.lnk"}}},
		},
	}

	t = append(t, chromium("chrome", "Google Chrome", `%LOCALAPPDATA%\Google\Chrome\User Data`, "chrome.exe")...)
	t = append(t, chromium("edge", "Microsoft Edge", `%LOCALAPPDATA%\Microsoft\Edge\User Data`, "msedge.exe")...)
	t = append(t, chromium("brave", "Brave", `%LOCALAPPDATA%\BraveSoftware\Brave-Browser\User Data`, "brave.exe")...)
	t = append(t, chromium("vivaldi", "Vivaldi", `%LOCALAPPDATA%\Vivaldi\User Data`, "vivaldi.exe")...)
	t = append(t,
		&Target{
			ID: "firefox.cache", Group: "Firefox", Section: SecBrowsers, Name: "Firefox - Cache",
			Description: "Cached web pages, images and scripts.",
			Default:     true, Processes: []string{"firefox.exe"},
			Rules: []Rule{
				{Root: `%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\cache2`, Recursive: true},
				{Root: `%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\startupCache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\Mozilla\Firefox\Profiles\*\thumbnails`, Recursive: true},
			},
		},
		&Target{
			ID: "firefox.cookies", Group: "Firefox", Section: SecBrowsers, Name: "Firefox - Cookies",
			Description: "Signs you out of websites. Off by default. History is not offered because Firefox stores it in the same file as bookmarks.",
			Processes:   []string{"firefox.exe"},
			Rules:       []Rule{{Root: `%APPDATA%\Mozilla\Firefox\Profiles\*`, Files: []string{"cookies.sqlite", "cookies.sqlite-wal", "cookies.sqlite-shm"}}},
		},
		&Target{
			ID: "app.teams", Group: "Applications", Section: SecApps, Name: "Microsoft Teams cache",
			Description: "Classic and new Teams cache. A common fix for Teams being slow or not loading. You stay signed in.",
			Processes:   []string{"teams.exe", "ms-teams.exe"},
			Rules: []Rule{
				{Root: `%APPDATA%\Microsoft\Teams\Cache`, Recursive: true},
				{Root: `%APPDATA%\Microsoft\Teams\Code Cache`, Recursive: true},
				{Root: `%APPDATA%\Microsoft\Teams\GPUCache`, Recursive: true},
				{Root: `%APPDATA%\Microsoft\Teams\Service Worker\CacheStorage`, Recursive: true},
				{Root: `%LOCALAPPDATA%\Packages\MSTeams_8wekyb3d8bbwe\LocalCache\Microsoft\MSTeams\EBWebView\*\Cache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\Packages\MSTeams_8wekyb3d8bbwe\LocalCache\Microsoft\MSTeams\EBWebView\*\Code Cache`, Recursive: true},
				{Root: `%LOCALAPPDATA%\Packages\MSTeams_8wekyb3d8bbwe\LocalCache\Microsoft\MSTeams\EBWebView\*\GPUCache`, Recursive: true},
			},
		},
		&Target{
			ID: "app.discord", Group: "Applications", Section: SecApps, Name: "Discord cache",
			Description: "Cached images and media from Discord.",
			Processes:   []string{"discord.exe"},
			Rules: []Rule{
				{Root: `%APPDATA%\discord\Cache`, Recursive: true},
				{Root: `%APPDATA%\discord\Code Cache`, Recursive: true},
				{Root: `%APPDATA%\discord\GPUCache`, Recursive: true},
			},
		},
		&Target{
			ID: "app.spotify", Group: "Applications", Section: SecApps, Name: "Spotify cache",
			Description: "Streamed songs Spotify keeps locally. Downloaded playlists are stored elsewhere and are not touched.",
			Processes:   []string{"spotify.exe"},
			Rules:       []Rule{{Root: `%LOCALAPPDATA%\Spotify\Data`, Recursive: true}},
		},
		&Target{
			ID: "app.zoom", Group: "Applications", Section: SecApps, Name: "Zoom logs",
			Description: "Zoom's log files. Recordings are not touched.",
			Processes:   []string{"zoom.exe"},
			Rules:       []Rule{{Root: `%APPDATA%\Zoom\logs`, Recursive: true}},
		},
	)
	return t
}

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
	return Result{Note: "flushed"}
}

// Label is the item's name inside its group, e.g. "Cache" under "Google Chrome".
func (t *Target) Label() string {
	return strings.TrimPrefix(t.Name, t.Group+" - ")
}
