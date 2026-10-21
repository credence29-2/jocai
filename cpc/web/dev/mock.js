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
    cpcOpenLogs: async () => null, cpcOpenPath: async () => null, cpcRestartAdmin: async () => null,
  });
})();
