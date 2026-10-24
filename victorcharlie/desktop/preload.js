// The bridge the web app looks for as window.victorCharlieDesktop.
const { contextBridge, ipcRenderer } = require("electron");

contextBridge.exposeInMainWorld("victorCharlieDesktop", {
  platform: process.platform,
  getSources: () => ipcRenderer.invoke("sources"),
  chooseSource: (id, audio) => ipcRenderer.send("choose-source", id, audio),
  setPTTKey: (key) => ipcRenderer.send("ptt-key", key),
  setToggleKeys: (keys) => ipcRenderer.send("toggle-keys", keys),
  onPTT: (cb) => ipcRenderer.on("ptt", (_e, down) => cb(down)),
  onToggle: (cb) => ipcRenderer.on("toggle", (_e, what) => cb(what)),
  setBadge: (n) => ipcRenderer.send("badge", n),
  flash: () => ipcRenderer.send("flash"),
});
