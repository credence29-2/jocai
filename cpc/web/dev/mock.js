// Fake backend for previewing web/index.html in a normal browser.
// Load it before the page's own script, e.g. with Playwright's addInitScript.
(() => {
  const groups = {
    "System": [["win.temp.user", "Temporary files (your account)", 1], ["win.temp.system", "Temporary files (Windows)", 1, 1], ["win.recycle", "Recycle Bin", 1], ["win.thumbs", "Thumbnail cache", 1], ["win.wer", "Windows error reports", 1], ["win.dns", "DNS cache", 1]],
    "Windows Update and logs": [["win.update", "Windows Update download cache", 0, 1], ["win.delivery", "Delivery Optimization cache", 0, 1], ["win.logs", "Old Windows log files", 0, 1]],
    "Advanced": [["win.dumps", "Memory dumps", 0], ["win.shader", "Graphics shader cache", 0], ["win.prefetch", "Prefetch data", 0, 1], ["win.recent", "Recent documents list", 0]],
    "Applications": [["app.teams", "Microsoft Teams cache", 0], ["app.discord", "Discord cache", 0], ["app.spotify", "Spotify cache", 0], ["app.zoom", "Zoom logs", 0]],
  };
  const browsers = ["Google Chrome", "Microsoft Edge", "Brave", "Vivaldi"];
  const targets = [];
  for (const [g, items] of Object.entries(groups))
    for (const [id, name, def, admin] of items)
      targets.push({ id, section: g === "Applications" ? "Applications" : "Windows", group: g, label: name, name, description: "Description of " + name.toLowerCase() + ".", default: !!def, needsAdmin: !!admin, selectable: !admin || window.__mockAdmin, checked: !!def && (!admin || window.__mockAdmin) });
  for (const b of browsers) for (const [k, def] of [["Cache", 1], ["Cookies", 0], ["History", 0]])
    targets.push({ id: b + k, section: "Browsers", group: b, label: k, name: b + " - " + k, description: k + " for " + b, default: !!def, needsAdmin: false, selectable: true, checked: !!def });
  targets.push({ id: "ff.cache", section: "Browsers", group: "Firefox", label: "Cache", name: "Firefox - Cache", description: "", default: true, needsAdmin: false, selectable: true, checked: true });
  const emit = (n, p) => window.cpcEvent && window.cpcEvent(n, p);
  let tok = 0;
  const later = (result, error) => { const t = ++tok; setTimeout(() => emit("async", error ? { token: t, error } : { token: t, result }), 30); return t; };
  const sizes = { "win.temp.user": 812e6, "win.recycle": 2.1e9, "win.thumbs": 64e6, "win.wer": 3.2e6, "Google ChromeCache": 433e6, "Microsoft EdgeCache": 210e6 };
  function run(ids, clean) {
    let i = 0, tot = { files: 0, bytes: 0 };
    const step = () => {
      if (i >= ids.length) { emit("finished", { mode: clean ? "clean" : "scan", files: tot.files, bytes: tot.bytes, failed: 0, errors: 0, canceled: false, logPath: clean ? "C:\\log.log" : "", message: (clean ? "Freed " : "Found ") + (tot.bytes / 1e6).toFixed(1) + " MB in " + tot.files + " files." }); return; }
      const id = ids[i];
      emit("progress", { mode: "scan", index: i, total: ids.length, id, name: targets.find(t => t.id === id).name });
      emit("file", { path: "C:\\Users\\Jacob\\AppData\\Local\\Temp\\some\\deep\\folder\\file" + i + ".tmp" });
      const b = Math.round(sizes[id] || 0), f = b ? Math.round(b / 90000) : 0;
      tot.files += f; tot.bytes += b;
      emit("result", { id, files: f, bytes: b, failed: id === "win.temp.user" && clean ? 3 : 0, note: id === "win.dns" ? (clean ? "flushed" : "Ready to flush") : "", text: "" });
      i++; setTimeout(step, window.__mockFast ? 0 : 120);
    };
    setTimeout(step, 10);
  }
  Object.assign(window, {
    cpcInfo: async () => ({ app: "Kempt", company: "804 Industries", version: "1.4.2", elevated: !!window.__mockAdmin, logsDir: "C:\\logs", diskTotal: 511e9, diskFree: 96e9, drive: "C:" }),
    cpcTargets: async () => targets,
    cpcSetSelected: async () => null,
    cpcScan: async ids => run(ids, false),
    cpcClean: async ids => run(ids, true),
    cpcCancel: async () => null,
    cpcOpenApps: async () => ["Google Chrome"],
    cpcStartup: async () => [
      { index: 0, name: "OneDrive", command: '"C:\\Program Files\\Microsoft OneDrive\\OneDrive.exe" /background', location: "Registry (this user)", enabled: true },
      { index: 1, name: "Toast POS Helper", command: "C:\\Toast\\helper.exe --tray", location: "Registry (all users)", enabled: true, needsAdmin: true, locked: !window.__mockAdmin },
      { index: 2, name: "Spotify", command: "C:\\Users\\Jacob\\AppData\\Roaming\\Spotify\\Spotify.exe /minimized", location: "Registry (this user)", enabled: false },
      { index: 3, name: "Zoom.lnk", command: "C:\\Users\\Jacob\\AppData\\...\\Startup\\Zoom.lnk", location: "Startup folder (this user)", enabled: true },
    ],
    cpcSetStartup: async (i, on) => { const r = await window.cpcStartup(); r[i].enabled = on; return r; },
    cpcStartupLocation: async () => null,
    cpcPrograms: async () => [
      { index: 0, name: "Google Chrome", publisher: "Google LLC", version: "131.0.6778.86", sizeKB: 812000, size: "793.0 MB", installed: "Mar 3, 2025", hasFolder: true },
      { index: 1, name: "Toast POS Terminal", publisher: "Toast, Inc.", version: "4.2.1", sizeKB: 52000, size: "50.8 MB", installed: "Jan 12, 2026", hasFolder: true },
      { index: 2, name: "Zoom Workplace", publisher: "Zoom Communications", version: "6.2.5", sizeKB: 310000, size: "302.7 MB", installed: "Aug 30, 2025", hasFolder: false },
      { index: 3, name: "7-Zip 24.08 (x64)", publisher: "Igor Pavlov", version: "24.08", sizeKB: 5800, size: "5.7 MB", installed: "", hasFolder: true },
    ],
    cpcUninstall: async () => null, cpcProgramFolder: async () => null,
    cpcSchedule: async () => ({ enabled: false, nextRun: "" }),
    cpcSetSchedule: async on => ({ enabled: on, nextRun: on ? "10/26/2026 12:00:00 PM" : "" }),
    cpcRegCategories: async () => [["apppaths","Application paths",0],["obsolete","Obsolete software",1],["startup","Run at startup",0],["muicache","MUI cache",0],["shareddlls","Missing shared DLLs",1],["installers","Installer folders",1],["help","Help files",0],["fonts","Fonts",0],["firewall","Firewall rules",1],["sounds","Sound events",0]]
      .map(([id, name, admin]) => ({ id, name, description: "About " + name.toLowerCase() + ".", needsAdmin: !!admin, selectable: !admin || !!window.__mockAdmin })),
    cpcRegScan: async cats => setTimeout(() => emit("regScanned", [
      { id: 0, category: "muicache", key: "HKEY_CURRENT_USER\\Software\\Classes\\Local Settings\\Software\\Microsoft\\Windows\\Shell\\MuiCache", value: "C:\\Old\\tool.exe.FriendlyAppName", problem: "C:\\Old\\tool.exe is gone" },
      { id: 1, category: "startup", key: "HKEY_CURRENT_USER\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\Run", value: "OldUpdater", problem: "OldUpdater: C:\\Program Files\\Old\\upd.exe is gone" },
      { id: 2, category: "apppaths", key: "HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Windows\\CurrentVersion\\App Paths\\old.exe", value: "", wholeKey: true, problem: "Points to missing C:\\Old\\old.exe" },
    ].filter(i => cats.includes(i.category))), 100),
    cpcRegFix: async ids => setTimeout(() => emit("regFixed", { fixed: ids.length, failed: 0, backup: "C:\\backups\\registry-x", name: "registry-x" }), 100),
    cpcRegBackups: async () => [{ name: "registry-x", when: "Oct 21, 2026 6:02 PM", count: 2 }],
    cpcRegRestore: async () => null, cpcOpenBackups: async () => null,
    cpcOpenLogs: async () => null,
    cpcRepairTools: async () => [
      ["sfc","Repair system files","Runs System File Checker, which checks every protected Windows file and replaces damaged ones from a known-good copy.","Odd Windows errors, missing icons or dialogs, features that won't open.","5 to 20",1,0],
      ["dism","Repair the Windows image","Runs DISM RestoreHealth, which fixes the component store that System File Checker draws from.","System File Checker failed, Windows Update errors.","10 to 30",1,0],
      ["network","Reset the network stack","Resets Winsock and TCP/IP, flushes DNS and renews the IP address.","No internet while other devices are fine, POS terminal can't reach the back office.","1",1,1],
      ["wureset","Reset Windows Update","Stops the update services, clears the download cache, and starts them again.","Updates stuck downloading or failing.","1",1,0],
      ["icons","Rebuild the icon cache","Deletes the icon cache and restarts Explorer.","Blank or wrong icons.","1",0,0],
      ["explorer","Restart Explorer","Restarts the Windows shell.","Taskbar frozen, Start menu not opening.","under 1",0,0],
      ["chkdsk","Check the system drive","Runs a read-only disk check.","Files that won't open, random freezes.","2 to 10",1,0],
      ["store","Reset the Microsoft Store","Clears the Store cache and re-registers it.","Store opens blank.","1",0,0],
      ["printspool","Restart the print spooler","Restarts the print service and clears stuck jobs.","Printer offline, receipt printer not responding.","under 1",1,0]]
      .map(([id,name,description,when,minutes,needsAdmin,reboot]) => ({ id, name, description, when, minutes, needsAdmin: !!needsAdmin, reboot: !!reboot, selectable: !needsAdmin || !!window.__mockAdmin })),
    cpcRunRepair: async id => { let i = 0; const lines = []; const tick = () => { i++; lines.push("Verification " + (i * 20) + "% complete."); if (i < 5) { emit("repairProgress", { id, step: 0, total: 1, lines: lines.slice(), pct: i * 20 }); setTimeout(tick, 80); } else emit("repairDone", { id, ok: true, text: "No damaged system files found.", output: lines.join("\n") + "\nWindows Resource Protection did not find any integrity violations.", reboot: false }); }; setTimeout(tick, 50); return null; },
    cpcStopRepair: async () => null,
    cpcDiskDrives: async () => ({ drives: [{ path: "C:\\", label: "OS", total: 511e9, free: 96e9 }, { path: "D:\\", label: "Data", total: 2e12, free: 1.4e12 }], folders: ["C:\\Users\\Jacob\\Downloads", "C:\\Users\\Jacob\\Documents", "C:\\Users\\Jacob\\Videos"] }),
    cpcDiskScan: async root => { window.__diskRoot = root; let f = 0; const tick = () => { f += 40000; if (f < 200000) { emit("diskProgress", { files: f, bytes: f * 9000, path: root + "Users\\Jacob\\AppData\\Local\\some\\deep\\folder\\file" + f + ".dat" }); setTimeout(tick, window.__mockFast ? 0 : 150); } else emit("diskDone", { root, files: 412880, bytes: 415e9, denied: 3, took: "48s", cancelled: false }); }; setTimeout(tick, 10); },
    cpcDiskCancel: async () => null,
    cpcDiskNode: async path => {
      const root = window.__diskRoot || "C:\\";
      if (!path || path === root) return { path: root, size: 415e9, files: 412880, root, crumbs: [{ name: root, path: root }], items: [
        { name: "Users", path: root + "Users", size: 212e9, files: 301000, dir: true }, { name: "Windows", path: root + "Windows", size: 31e9, files: 98000, dir: true }, { name: "Program Files", path: root + "Program Files", size: 24e9, files: 12000, dir: true },
        { name: "ProgramData", path: root + "ProgramData", size: 9e9, files: 1700, dir: true }, { name: "System Volume Information", path: root + "System Volume Information", size: 0, files: 0, dir: true, denied: true },
        { name: "pagefile.sys", path: root + "pagefile.sys", size: 12.8e9, files: 1 }, { name: "hiberfil.sys", path: root + "hiberfil.sys", size: 6.4e9, files: 1 }] };
      return { path, size: 212e9, files: 301000, root, crumbs: [{ name: root, path: root }, { name: "Users", path }], items: [
        { name: "Jacob", path: path + "\\Jacob", size: 180e9, files: 280000, dir: true }, { name: "Public", path: path + "\\Public", size: 2e9, files: 400, dir: true },
        { name: "big-backup.zip", path: path + "\\big-backup.zip", size: 28e9, files: 1 }] };
    },
    cpcDiskLargest: async () => [
      { path: "C:\\Users\\Jacob\\Videos\\Toast training raw.mp4", size: 14.2e9, type: "Videos", modified: "Mar 3, 2025" }, { path: "C:\\pagefile.sys", size: 12.8e9, type: "System", modified: "Oct 26, 2026" },
      { path: "C:\\Users\\Jacob\\Downloads\\Win11_24H2_English_x64.iso", size: 6.9e9, type: "Disc images and virtual disks", modified: "Jun 20, 2025" }, { path: "C:\\Users\\Jacob\\Downloads\\old-laptop-backup.zip", size: 4.3e9, type: "Archives", modified: "Jan 9, 2025" },
      { path: "C:\\Users\\Jacob\\AppData\\Local\\Docker\\wsl\\data\\ext4.vhdx", size: 3.1e9, type: "Disc images and virtual disks", modified: "Oct 20, 2026" }],
    cpcDiskTypes: async () => [{ type: "Videos", size: 96e9, count: 1200 }, { type: "Programs and installers", size: 88e9, count: 41000 }, { type: "System", size: 61e9, count: 90000 }, { type: "Documents", size: 44e9, count: 120000 }, { type: "Pictures", size: 39e9, count: 80000 }, { type: "Archives", size: 31e9, count: 900 }, { type: "Disc images and virtual disks", size: 22e9, count: 6 }, { type: "Other", size: 34e9, count: 79000 }],
    cpcDiskDelete: async paths => setTimeout(() => emit("diskDeleted", { deleted: paths.length, bytes: paths.length * 4.3e9 }), 100),
    cpcShowDiskPath: async () => null,
    cpcBloatList: async () => later({ elevated: !!window.__mockAdmin, groups: ["Promotions and games", "Microsoft extras", "Xbox", "PC maker apps", "Other Store apps"], apps: [
      { name: "king.com.CandyCrushSaga", label: "King games (Candy Crush and others)", publisher: "king.com", version: "1.2", size: 180e6, group: "Promotions and games", known: true, checked: true },
      { name: "Microsoft.BingNews", label: "Microsoft News", publisher: "Microsoft Corporation", version: "4.5", size: 40e6, group: "Promotions and games", known: true, checked: true },
      { name: "Microsoft.MicrosoftSolitaireCollection", label: "Solitaire Collection", publisher: "Microsoft Corporation", version: "4.19", size: 120e6, group: "Promotions and games", known: true, checked: true },
      { name: "Microsoft.GetHelp", label: "Get Help", publisher: "Microsoft Corporation", version: "10.2", size: 8e6, group: "Microsoft extras", known: true, note: "Microsoft's support app. Harmless, rarely used.", checked: false },
      { name: "Microsoft.XboxGamingOverlay", label: "Xbox Game Bar", publisher: "Microsoft Corporation", version: "7.1", size: 60e6, group: "Xbox", known: true, checked: true },
      { name: "DellInc.DellSupportAssistforPCs", label: "Dell preinstalled apps", publisher: "Dell Inc.", version: "3.1", size: 90e6, group: "PC maker apps", known: true, note: "Dell support tools. Review before removing.", checked: false },
      { name: "SomeVendor.Widget", label: "Widget Thing", publisher: "Some Vendor", version: "1.0", size: 5e6, group: "Other Store apps", known: false, checked: false }] }),
    cpcBloatRemove: async names => { names.forEach((n, i) => setTimeout(() => { emit("bloatProgress", { name: n, index: i, total: names.length, status: i === 1 ? "failed" : "removed", error: "HRESULT: 0x80073CFA, Removal failed. This app is part of Windows." }); if (i === names.length - 1) emit("bloatDone", { removed: names.length - 1, failed: 1 }); }, 60 * (i + 1))); return null; },
    cpcReport: async () => later({ html: "C:\\r.html", txt: "C:\\r.txt", computer: "POS-01", warnings: ["Drive C: is 96% full (9.3 GB free).", "Battery: Holds 48% of its original charge. It's worn out and worth replacing.", "2 device(s) report a problem in Device Manager."], text: "Kempt tech report  Oct 26, 2026 9:00 AM\n============================================================\nComputer     POS-01  (user cashier)\nMake/model   Dell Inc. OptiPlex 3080  S/N 7XK2Q93\nWindows      Windows 11 Pro  version 24H2  build 26100.4351\nInstalled    2024-03-12   Last update 2026-10-14   Activation: Activated\nLast boot    2026-10-25 06:12   Uptime 1d 3h\nCPU / RAM    Intel(R) Core(TM) i5-10500T CPU @ 2.30GHz / 15.8 GB\nNetwork      Ethernet 192.168.1.44   Domain/workgroup: Workgroup WORKGROUP\nAntivirus    Windows Defender\n\nNEEDS ATTENTION\n  ! Drive C: is 96% full (9.3 GB free).\n  ! Battery: Holds 48% of its original charge.\n\nDRIVES\n  C: OS             237.0 GB total    9.3 GB free  (96% used)\n  KINGSTON SA400S37240G (SSD, 223.6 GB)  Healthy.  wear 12%  18231h on\n\nSTARTUP PROGRAMS (4)\n  OneDrive  \"C:\\Program Files\\Microsoft OneDrive\\OneDrive.exe\" /background\n..." }),
    cpcOpenReports: async () => null,
    cpcHealth: async () => later({ disks: [{ name: "KINGSTON SA400S37240G", mediaType: "SSD", size: 240e9, status: "Healthy", wear: 12, temperature: 38, powerOnHours: 18231, level: "good", text: "Healthy." }, { name: "WDC WD10EZEX", mediaType: "HDD", size: 1e12, status: "Warning", wear: -1, level: "warn", text: "Windows reports a warning for this drive. Back up important files." }],
      battery: { present: true, charge: 83, healthPercent: 48, status: "Charging", level: "bad", text: "Holds 48% of its original charge. It's worn out and worth replacing." } }),
    cpcExclusions: async () => (window.__excl = window.__excl || ["C:\\Users\\Jacob\\AppData\\Local\\Temp\\POS-Backups"]),
    cpcAddExclusion: async () => (window.__excl.push("D:\\Keep"), window.__excl),
    cpcRemoveExclusion: async f => (window.__excl = window.__excl.filter(x => x !== f)),
    cpcTasks: async () => later([
      { name: "GoogleUpdateTaskMachineUA", path: "\\", state: "Ready", author: "Google LLC", action: "C:\\Program Files (x86)\\Google\\Update\\GoogleUpdate.exe /ua", enabled: true },
      { name: "Adobe Acrobat Update Task", path: "\\", state: "Disabled", author: "Adobe Systems", action: "C:\\Program Files (x86)\\Common Files\\Adobe\\ARM\\1.0\\AdobeARM.exe", enabled: false }]),
    cpcSetTask: async () => later(null, "Restart Kempt as administrator to change scheduled tasks."),
    cpcServices: async () => later([
      { name: "AdobeARMservice", displayName: "Adobe Acrobat Update Service", startMode: "Automatic", state: "Running", path: "C:\\Program Files (x86)\\Common Files\\Adobe\\ARM\\1.0\\armsvc.exe", description: "Keeps Adobe Acrobat up to date." },
      { name: "TeamViewer", displayName: "TeamViewer", startMode: "Manual", state: "Stopped", path: "C:\\Program Files\\TeamViewer\\TeamViewer_Service.exe", description: "" }]),
    cpcSetService: async () => later([]),
    cpcDrivers: async () => later({
      updates: [
        { id: "11111111-1111-1111-1111-111111111111", title: "Advanced Micro Devices, Inc. - MEDIA - 10.0.1.42", model: "AMD High Definition Audio Device", provider: "Advanced Micro Devices, Inc.", class: "MEDIA", group: "Audio", date: "2025-11-03", size: 1.2e6, currentVersion: "10.0.1.24", currentDate: "2021-06-14" },
        { id: "22222222-2222-2222-2222-222222222222", title: "Intel Corporation - Bluetooth - 23.40.0.2", model: "Intel(R) Wireless Bluetooth(R)", provider: "Intel Corporation", class: "Bluetooth", group: "Bluetooth", date: "2024-02-21", size: 9.8e6, currentVersion: "22.150.0.6", currentDate: "2022-09-01" },
        { id: "33333333-3333-3333-3333-333333333333", title: "Realtek - Net - 1168.30.508.2026", model: "Realtek PCIe GbE Family Controller", provider: "Realtek", class: "Net", group: "Connectivity", date: "2026-05-07", size: 2.1e6, currentVersion: "10.38.1128.2019", currentDate: "2019-11-28" },
        { id: "44444444-4444-4444-4444-444444444444", title: "Intel - Net - 23.40.1.1", model: "Intel(R) Dual Band Wireless-AC 3168", provider: "Intel", class: "Net", group: "Connectivity", date: "2024-03-19", size: 22e6, currentVersion: "20.70.0.5", currentDate: "2019-04-11" }],
      ignored: [],
      installed: [
        { device: "AMD High Definition Audio Device", group: "Audio", version: "10.0.1.24", date: "2021-06-14", provider: "Advanced Micro Devices", hasUpdate: true },
        { device: "Realtek PCIe GbE Family Controller", group: "Connectivity", version: "10.38.1128.2019", date: "2019-11-28", provider: "Realtek", hasUpdate: true },
        { device: "Intel(R) UHD Graphics 620", group: "Graphics", version: "31.0.101.2125", date: "2023-05-10", provider: "Intel Corporation", hasUpdate: false }],
      devices: [{ name: "Unknown USB Device (Device Descriptor Request Failed)", class: "USB", code: 43, problem: "Windows stopped this device because it reported problems." }],
      links: [{ key: "intel", name: "Intel Driver & Support Assistant" }, { key: "amd", name: "AMD drivers and support" }, { key: "realtek", name: "Realtek downloads" }, { key: "lenovo", name: "Lenovo drivers and downloads" }] }),
    cpcInstallDrivers: async (ids, rp) => { if (rp) emit("drvProgress", { stage: "restore" }); ids.forEach((id, i) => setTimeout(() => { emit("drvProgress", { stage: "installing", id, index: i, total: ids.length }); emit("drvProgress", { stage: "done", id, ok: i !== 1, text: i !== 1 ? "Installed" : "Windows Update couldn't install it", reboot: i === 0 }); if (i === ids.length - 1) emit("drvFinished", { installed: ids.length - 1, failed: 1, reboot: true, restore: "Restore point created." }); }, 60 * (i + 1))); return null; },
    cpcIgnoreDriver: async () => later(null),
    cpcOpenMakerSite: async () => null,
    cpcOpenSettings: async () => null,
    cpcUpdates: async () => later({ available: true, updates: [
      { name: "Google Chrome", id: "Google.Chrome", version: "130.0.6723.70", available: "131.0.6778.86" },
      { name: "Zoom Workplace", id: "Zoom.Zoom", version: "6.2.5.4567", available: "6.3.0.5527" },
      { name: "7-Zip 23.01 (x64)", id: "7zip.7zip", version: "23.01", available: "24.08" },
      { name: "Microsoft Edge", id: "Microsoft.Edge", version: "154.0.4258.62", available: "155.0.4283.62", selfUpdate: true }] }),
    cpcOpenSelfUpdater: async () => null,
    cpcUpdateApps: async ids => { ids.forEach((id, i) => setTimeout(() => { emit("updProgress", { id, index: i, total: ids.length, status: i === 1 ? "failed" : "updated", error: "The app's installer failed (1603). Close the app and try again, or update it from inside the app." }); if (i === ids.length - 1) emit("updFinished", { updated: ids.length - 1, failed: 1 }); }, 50 * (i + 1))); return null; },
    cpcDupeDefaults: async () => ["C:\\Users\\Jacob\\Documents", "C:\\Users\\Jacob\\Downloads", "C:\\Users\\Jacob\\Pictures", "C:\\Users\\Jacob\\OneDrive"],
    cpcPickFolder: async () => "D:\\Photos",
    cpcDupeScan: async () => { setTimeout(() => emit("dupeFinished", { wasted: 1.3e9, groups: [
      { id: 0, size: 1.2e9, files: [{ id: 0, path: "C:\\Users\\Jacob\\Downloads\\Win11_23H2.iso", size: 1.2e9, modified: "Jan 4, 2025" }, { id: 1, path: "C:\\Users\\Jacob\\Documents\\ISOs\\Win11_23H2.iso", size: 1.2e9, modified: "Mar 9, 2025" }] },
      { id: 1, size: 4.2e6, files: [{ id: 2, path: "C:\\Users\\Jacob\\Pictures\\IMG_2041.jpg", size: 4.2e6, modified: "Jul 1, 2024" }, { id: 3, path: "C:\\Users\\Jacob\\OneDrive\\Pictures\\IMG_2041.jpg", size: 4.2e6, modified: "Jul 1, 2024" }, { id: 4, path: "C:\\Users\\Jacob\\Downloads\\IMG_2041 (1).jpg", size: 4.2e6, modified: "Aug 2, 2024" }] }] }), 80); return null; },
    cpcDupeCancel: async () => null, cpcDupeDelete: async () => null, cpcShowFile: async () => null, cpcOpenPath: async () => null, cpcRestartAdmin: async () => null,
  });
})();
