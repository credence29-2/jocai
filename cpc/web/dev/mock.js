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
    cpcInfo: async () => ({ app: "Credence PC Cleaner", version: "1.1.0", elevated: !!window.__mockAdmin, logsDir: "C:\\logs", diskTotal: 511e9, diskFree: 96e9, drive: "C:" }),
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
    cpcExclusions: async () => (window.__excl = window.__excl || ["C:\\Users\\Jacob\\AppData\\Local\\Temp\\POS-Backups"]),
    cpcAddExclusion: async () => (window.__excl.push("D:\\Keep"), window.__excl),
    cpcRemoveExclusion: async f => (window.__excl = window.__excl.filter(x => x !== f)),
    cpcTasks: async () => later([
      { name: "GoogleUpdateTaskMachineUA", path: "\\", state: "Ready", author: "Google LLC", action: "C:\\Program Files (x86)\\Google\\Update\\GoogleUpdate.exe /ua", enabled: true },
      { name: "Adobe Acrobat Update Task", path: "\\", state: "Disabled", author: "Adobe Systems", action: "C:\\Program Files (x86)\\Common Files\\Adobe\\ARM\\1.0\\AdobeARM.exe", enabled: false }]),
    cpcSetTask: async () => later(null, "Restart CPC as administrator to change scheduled tasks."),
    cpcServices: async () => later([
      { name: "AdobeARMservice", displayName: "Adobe Acrobat Update Service", startMode: "Automatic", state: "Running", path: "C:\\Program Files (x86)\\Common Files\\Adobe\\ARM\\1.0\\armsvc.exe", description: "Keeps Adobe Acrobat up to date." },
      { name: "TeamViewer", displayName: "TeamViewer", startMode: "Manual", state: "Stopped", path: "C:\\Program Files\\TeamViewer\\TeamViewer_Service.exe", description: "" }]),
    cpcSetService: async () => later([]),
    cpcDrivers: async () => later({ devices: [{ name: "Unknown USB Device (Device Descriptor Request Failed)", class: "USB", code: 43, problem: "Windows stopped this device because it reported problems." }],
      drivers: [{ device: "Realtek PCIe GbE Family Controller", class: "NET", version: "10.38.1128.2019", date: "2019-11-28", manufacturer: "Realtek" },
                { device: "Intel(R) UHD Graphics 620", class: "DISPLAY", version: "31.0.101.2125", date: "2023-05-10", manufacturer: "Intel Corporation" }] }),
    cpcOpenSettings: async () => null,
    cpcUpdates: async () => later({ available: true, updates: [
      { name: "Google Chrome", id: "Google.Chrome", version: "130.0.6723.70", available: "131.0.6778.86" },
      { name: "Zoom Workplace", id: "Zoom.Zoom", version: "6.2.5.4567", available: "6.3.0.5527" },
      { name: "7-Zip 23.01 (x64)", id: "7zip.7zip", version: "23.01", available: "24.08" }] }),
    cpcUpdateApps: async ids => { ids.forEach((id, i) => setTimeout(() => { emit("updProgress", { id, index: i, total: ids.length, status: i === 1 ? "failed" : "updated", error: "Installer failed" }); if (i === ids.length - 1) emit("updFinished", { updated: ids.length - 1, failed: 1 }); }, 50 * (i + 1))); return null; },
    cpcDupeDefaults: async () => ["C:\\Users\\Jacob\\Documents", "C:\\Users\\Jacob\\Downloads", "C:\\Users\\Jacob\\Pictures", "C:\\Users\\Jacob\\OneDrive"],
    cpcPickFolder: async () => "D:\\Photos",
    cpcDupeScan: async () => { setTimeout(() => emit("dupeFinished", { wasted: 1.3e9, groups: [
      { id: 0, size: 1.2e9, files: [{ id: 0, path: "C:\\Users\\Jacob\\Downloads\\Win11_23H2.iso", size: 1.2e9, modified: "Jan 4, 2025" }, { id: 1, path: "C:\\Users\\Jacob\\Documents\\ISOs\\Win11_23H2.iso", size: 1.2e9, modified: "Mar 9, 2025" }] },
      { id: 1, size: 4.2e6, files: [{ id: 2, path: "C:\\Users\\Jacob\\Pictures\\IMG_2041.jpg", size: 4.2e6, modified: "Jul 1, 2024" }, { id: 3, path: "C:\\Users\\Jacob\\OneDrive\\Pictures\\IMG_2041.jpg", size: 4.2e6, modified: "Jul 1, 2024" }, { id: 4, path: "C:\\Users\\Jacob\\Downloads\\IMG_2041 (1).jpg", size: 4.2e6, modified: "Aug 2, 2024" }] }] }), 80); return null; },
    cpcDupeCancel: async () => null, cpcDupeDelete: async () => null, cpcShowFile: async () => null, cpcOpenPath: async () => null, cpcRestartAdmin: async () => null,
  });
})();
