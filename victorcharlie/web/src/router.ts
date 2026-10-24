import { channelById, get, set } from "./store";
import type { ID } from "./types";
import { setOpener } from "./notify";

// URLs look like /channels/<guild id or @me>/<channel id>, so a refresh or a
// shared link lands in the same place.

export function navigate(guildId: ID | "@me", channelId?: ID, replace = false) {
  const s = get();
  if (!channelId) channelId = s.channelByGuild[guildId] ?? defaultChannel(guildId);
  set({
    guildId,
    channelByGuild: channelId ? { ...s.channelByGuild, [guildId]: channelId } : s.channelByGuild,
    mobilePane: channelId ? "main" : "sidebar",
    editing: null,
  });
  if (channelId) localStorage.setItem("vc.last." + guildId, channelId);
  localStorage.setItem("vc.lastGuild", guildId);
  const path = `/channels/${guildId}${channelId ? "/" + channelId : ""}`;
  if (location.pathname !== path) history[replace ? "replaceState" : "pushState"](null, "", path);
}

export function selectGuild(guildId: ID | "@me") {
  const s = get();
  const remembered = s.channelByGuild[guildId] ?? localStorage.getItem("vc.last." + guildId) ?? undefined;
  const ok = remembered && channelById(remembered);
  navigate(guildId, ok ? remembered : defaultChannel(guildId));
  set({ mobilePane: "sidebar" });
}

function defaultChannel(guildId: ID | "@me"): ID | undefined {
  if (guildId === "@me") return undefined;
  const g = get().guilds[guildId];
  if (!g) return undefined;
  if (g.system_channel_id && g.channels.some((c) => c.id === g.system_channel_id)) return g.system_channel_id;
  return g.channels.filter((c) => c.type === "text").sort((a, b) => a.position - b.position)[0]?.id;
}

// applyLocation reads the URL after READY.
export function applyLocation() {
  const m = /^\/channels\/(@me|\d+)(?:\/(\d+))?/.exec(location.pathname);
  const s = get();
  if (m) {
    const gid = m[1];
    if (gid === "@me" || s.guilds[gid]) {
      const cid = m[2] && channelById(m[2]) ? m[2] : undefined;
      navigate(gid, cid, true);
      if (cid) set({ mobilePane: "main" });
      return;
    }
  }
  const last = localStorage.getItem("vc.lastGuild");
  if (last && (last === "@me" || s.guilds[last])) selectGuild(last);
  else {
    const first = Object.keys(s.guilds)[0];
    selectGuild(first ?? "@me");
  }
  history.replaceState(null, "", `/channels/${get().guildId}${get().channelByGuild[get().guildId] ? "/" + get().channelByGuild[get().guildId] : ""}`);
}

window.addEventListener("popstate", () => {
  if (get().ready) applyLocation();
});

setOpener((cid) => {
  const c = channelById(cid);
  if (!c) return;
  navigate(c.type === "dm" ? "@me" : c.guild_id!, cid);
});

export function inviteCodeFromURL(): string | null {
  const m = /^\/invite\/([\w-]+)/.exec(location.pathname);
  return m ? m[1] : null;
}
