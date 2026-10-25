// Desktop shell for a Victor Charlie server: loads the server's web app in a window
// and adds what a browser tab can't do, like push-to-talk while another app
// has focus, a window picker for screen share, and a tray icon.

const { app, BrowserWindow, desktopCapturer, ipcMain, Menu, nativeImage, session, shell, Tray, dialog } = require("electron");
const fs = require("fs");
const path = require("path");
const keys = require("./keys");

const APP_NAME = "Project Victor Charlie";
const configPath = () => path.join(app.getPath("userData"), "config.json");

function readConfig() {
  try {
    return JSON.parse(fs.readFileSync(configPath(), "utf8"));
  } catch {
    return {};
  }
}

function writeConfig(c) {
  fs.mkdirSync(path.dirname(configPath()), { recursive: true });
  fs.writeFileSync(configPath(), JSON.stringify(c, null, 2));
}

let config = {};
let win = null;
let tray = null;
let quitting = false;

if (!app.requestSingleInstanceLock()) {
  app.quit();
} else {
  app.on("second-instance", () => showWindow());
}

// Browsers only allow the mic and screen capture on HTTPS. A server on the
// home network reached by plain http://192.168.x.x would otherwise have no
// voice in the app, so trust the configured server's origin as secure.
function trustServer() {
  config = readConfig();
  if (config.server && config.server.startsWith("http://")) {
    app.commandLine.appendSwitch("unsafely-treat-insecure-origin-as-secure", new URL(config.server).origin);
  }
}
app.setPath("userData", path.join(app.getPath("appData"), "VictorCharlie"));
trustServer();
app.setAppUserModelId("com.credence.victorcharlie");

function serverOrigin() {
  try {
    return new URL(config.server).origin;
  } catch {
    return null;
  }
}

// Whether a URL is the configured server: same scheme, host and port. A
// prefix check would let chat.example.org.evil.com pass as the server.
function isServer(url) {
  const origin = serverOrigin();
  if (!origin || !url) return false;
  try {
    return new URL(url).origin === origin;
  } catch {
    return false;
  }
}

function showWindow() {
  if (!win) return;
  if (win.isMinimized()) win.restore();
  win.show();
  win.focus();
}

function createWindow() {
  win = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 480,
    minHeight: 400,
    backgroundColor: "#1e1f22",
    title: APP_NAME,
    icon: path.join(__dirname, "build", "icon.png"),
    autoHideMenuBar: true,
    webPreferences: {
      preload: path.join(__dirname, "preload.js"),
      contextIsolation: true,
      sandbox: true,
      backgroundThrottling: false, // keep voice smooth while minimized
    },
  });
  win.removeMenu();

  win.on("close", (e) => {
    if (!quitting && config.closeToTray !== false) {
      e.preventDefault();
      win.hide();
    }
  });

  win.webContents.setWindowOpenHandler(({ url }) => {
    if (isServer(url) && new URL(url).pathname.startsWith("/uploads/")) {
      // Uploads need the login cookie, so open them in-app.
      return { action: "allow", overrideBrowserWindowOptions: { autoHideMenuBar: true, backgroundColor: "#000" } };
    }
    if (/^https?:/.test(url)) shell.openExternal(url);
    return { action: "deny" };
  });

  win.webContents.on("will-navigate", (e, url) => {
    if (url.startsWith("vc-setup://")) {
      e.preventDefault();
      // Only the bundled setup page may change the saved server.
      if (!win.webContents.getURL().startsWith("file:")) return;
      const u = new URL(url).searchParams.get("url");
      connectTo(u);
      return;
    }
    if (!isServer(url) && !url.startsWith("file:")) {
      e.preventDefault();
      shell.openExternal(url);
    }
  });

  win.webContents.on("did-fail-load", (_e, code, desc, url, isMain) => {
    if (!isMain || code === -3) return; // -3 is an aborted load, e.g. a redirect
    showSetup(`Couldn't reach ${url}: ${desc}`);
  });

  if (config.server) win.loadURL(config.server);
  else showSetup();
}

function showSetup(error) {
  const q = new URLSearchParams();
  if (config.server) q.set("current", config.server);
  if (error) q.set("error", error);
  win.loadFile(path.join(__dirname, "setup.html"), { search: q.toString() });
}

function connectTo(raw) {
  let u;
  try {
    u = new URL(raw);
  } catch {
    return showSetup("That doesn't look like an address.");
  }
  const wasHttp = config.server && config.server.startsWith("http://");
  const prev = config.server;
  config.server = u.origin;
  writeConfig(config);
  // The secure-origin switch only applies at startup.
  if (u.protocol === "http:" && (!wasHttp || new URL(prev).origin !== u.origin)) {
    app.relaunch();
    quitting = true;
    app.exit(0);
    return;
  }
  win.loadURL(u.pathname.startsWith("/invite/") ? u.href : u.origin);
}

// --- screen share ---

let chosen = null; // { id, audio } from the in-app picker

ipcMain.handle("sources", async () => {
  const list = await desktopCapturer.getSources({ types: ["screen", "window"], thumbnailSize: { width: 400, height: 225 }, fetchWindowIcons: true });
  return list
    .filter((s) => s.name !== APP_NAME || s.id.startsWith("screen:"))
    .map((s) => ({
      id: s.id,
      name: s.id.startsWith("screen:") && list.filter((x) => x.id.startsWith("screen:")).length === 1 ? "Entire screen" : s.name,
      thumbnail: s.thumbnail.toDataURL(),
      appIcon: s.appIcon && !s.appIcon.isEmpty() ? s.appIcon.toDataURL() : undefined,
      display: s.id.startsWith("screen:"),
    }));
});

ipcMain.on("choose-source", (_e, id, audio) => {
  chosen = id ? { id, audio } : null;
});

function setupCapture() {
  session.defaultSession.setDisplayMediaRequestHandler(async (_req, callback) => {
    const sources = await desktopCapturer.getSources({ types: ["screen", "window"] });
    const pick = (chosen && sources.find((s) => s.id === chosen.id)) || sources.find((s) => s.id.startsWith("screen:"));
    const audio = chosen ? chosen.audio : false;
    chosen = null;
    if (!pick) return callback({});
    // Windows can capture what's playing on the PC alongside the screen.
    callback({ video: pick, audio: audio && process.platform === "win32" ? "loopback" : undefined });
  });

  session.defaultSession.setPermissionRequestHandler((wc, permission, callback, details) => {
    const ok = isServer(details.requestingUrl || wc.getURL());
    callback(!!ok && ["media", "display-capture", "notifications", "fullscreen", "clipboard-sanitized-write", "clipboard-read"].includes(permission));
  });
  session.defaultSession.setPermissionCheckHandler((_wc, permission, requestingOrigin) => {
    const origin = serverOrigin();
    return !!origin && requestingOrigin === origin && ["media", "notifications", "fullscreen", "clipboard-sanitized-write"].includes(permission);
  });
}

// --- global keys ---

let ptt = null;
let toggles = { mute: null, deafen: null };
let pttHeld = false;

ipcMain.on("ptt-key", (_e, code) => {
  ptt = keys.parse(code);
  pttHeld = false;
});
ipcMain.on("toggle-keys", (_e, k) => {
  toggles = { mute: keys.parseCombo(k.mute), deafen: keys.parseCombo(k.deafen) };
});

function send(channel, ...args) {
  if (win && !win.isDestroyed()) win.webContents.send(channel, ...args);
}

function startHooks() {
  let uIOhook;
  try {
    ({ uIOhook } = require("uiohook-napi"));
  } catch (e) {
    console.error("global keys unavailable:", e);
    return;
  }
  const matches = (c, e) => c && c.key === e.keycode && c.ctrl === e.ctrlKey && c.alt === e.altKey && c.shift === e.shiftKey && c.meta === e.metaKey;
  uIOhook.on("keydown", (e) => {
    if (ptt && ptt.key === e.keycode && !pttHeld) {
      pttHeld = true;
      send("ptt", true);
    }
    if (matches(toggles.mute, e)) send("toggle", "mute");
    if (matches(toggles.deafen, e)) send("toggle", "deafen");
  });
  uIOhook.on("keyup", (e) => {
    if (ptt && ptt.key === e.keycode && pttHeld) {
      pttHeld = false;
      send("ptt", false);
    }
  });
  uIOhook.on("mousedown", (e) => {
    if (ptt && ptt.mouse === e.button && !pttHeld) {
      pttHeld = true;
      send("ptt", true);
    }
  });
  uIOhook.on("mouseup", (e) => {
    if (ptt && ptt.mouse === e.button && pttHeld) {
      pttHeld = false;
      send("ptt", false);
    }
  });
  uIOhook.start();
  app.on("will-quit", () => uIOhook.stop());
}

// --- tray, badge, attention ---

const badgeIcon = () => nativeImage.createFromPath(path.join(__dirname, "build", "badge.png"));

ipcMain.on("badge", (_e, n) => {
  if (!win) return;
  if (process.platform === "win32") win.setOverlayIcon(n > 0 ? badgeIcon() : null, n > 0 ? `${n} unread mentions` : "");
  else app.setBadgeCount(n);
  tray?.setToolTip(n > 0 ? `${APP_NAME} (${n})` : APP_NAME);
});

ipcMain.on("flash", () => {
  if (win && !win.isFocused()) win.flashFrame(true);
});

function setupTray() {
  tray = new Tray(nativeImage.createFromPath(path.join(__dirname, "build", "tray.png")));
  tray.setToolTip(APP_NAME);
  const menu = () =>
    Menu.buildFromTemplate([
      { label: `Open ${APP_NAME}`, click: showWindow },
      { type: "separator" },
      { label: "Change server…", click: () => (showWindow(), showSetup()) },
      {
        label: "Close button hides to tray",
        type: "checkbox",
        checked: config.closeToTray !== false,
        click: (item) => {
          config.closeToTray = item.checked;
          writeConfig(config);
        },
      },
      {
        label: "Start when Windows starts",
        type: "checkbox",
        checked: app.getLoginItemSettings().openAtLogin,
        click: (item) => app.setLoginItemSettings({ openAtLogin: item.checked, args: ["--hidden"] }),
      },
      { label: "Reload", click: () => win?.webContents.reload() },
      { label: "Developer tools", click: () => win?.webContents.openDevTools({ mode: "detach" }) },
      { type: "separator" },
      {
        label: "Quit",
        click: () => {
          quitting = true;
          app.quit();
        },
      },
    ]);
  tray.setContextMenu(menu());
  tray.on("right-click", () => tray.setContextMenu(menu()));
  tray.on("click", showWindow);
}

app.whenReady().then(() => {
  setupCapture();
  createWindow();
  if (process.argv.includes("--hidden")) win.hide();
  setupTray();
  startHooks();
});

app.on("before-quit", () => {
  quitting = true;
});

process.on("uncaughtException", (e) => {
  dialog.showErrorBox(APP_NAME, String(e?.stack || e));
});
