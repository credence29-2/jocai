import { get } from "./store";
import { appName } from "./brand";
import type { ID } from "./types";

// The Android app has no Notification API in its WebView; it exposes a
// bridge instead.
declare global {
  interface Window {
    VictorCharlieAndroid?: { notify(title: string, body: string, tag: string): void; setBadge?(n: number): void; voice?(on: boolean, label: string): void; changeServer?(): void };
    vcOpenChannel?: (id: string) => void;
    victorCharlieDesktop?: DesktopBridge;
  }
}

export interface DesktopBridge {
  platform: string;
  getSources(): Promise<{ id: string; name: string; thumbnail: string; appIcon?: string; display: boolean }[]>;
  chooseSource(id: string | null, audio: boolean): void;
  setPTTKey(key: string | null): void;
  onPTT(cb: (down: boolean) => void): void;
  onToggle(cb: (what: "mute" | "deafen") => void): void;
  setToggleKeys(keys: { mute: string | null; deafen: string | null }): void;
  setBadge(n: number): void;
  flash(): void;
}

export function notificationsEnabled() {
  return localStorage.getItem("vc.notifications") !== "0";
}

export async function askNotificationPermission() {
  if (window.VictorCharlieAndroid || !("Notification" in window)) return;
  if (Notification.permission === "default") await Notification.requestPermission();
}

export function notify(title: string, body: string, channelId: ID) {
  if (!notificationsEnabled()) return;
  if (document.hasFocus()) return;
  body = body.replace(/<@&?(\d+)>/g, "@mention").slice(0, 200);
  if (window.VictorCharlieAndroid) {
    window.VictorCharlieAndroid.notify(title, body, channelId);
    return;
  }
  window.victorCharlieDesktop?.flash();
  if (!("Notification" in window) || Notification.permission !== "granted") return;
  const n = new Notification(title, { body, tag: channelId, icon: "/favicon.svg" });
  n.onclick = () => {
    window.focus();
    openChannelById(channelId);
    n.close();
  };
}

// Opening a channel from a notification; the router registers itself here
// to avoid an import cycle.
let opener: (id: ID) => void = () => {};
export function setOpener(fn: (id: ID) => void) {
  opener = fn;
}
export function openChannelById(id: ID) {
  opener(id);
}
window.vcOpenChannel = openChannelById;

// Badge count for the desktop and Android apps.
export function updateBadge() {
  const s = get();
  let n = 0;
  for (const rs of Object.values(s.readStates)) n += rs.mention_count;
  window.victorCharlieDesktop?.setBadge(n);
  window.VictorCharlieAndroid?.setBadge?.(n);
  document.title = n ? `(${n}) ${appName()}` : appName();
}

