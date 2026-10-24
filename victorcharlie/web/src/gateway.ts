import { addMessage, applyReady, channelById, displayName, get, removeGuild, set, updateMessage, upsertDM, upsertGuild } from "./store";
import type { DM, Guild, ID, Message, Presence, User, VoiceState } from "./types";
import { playSound } from "./sounds";
import { appName } from "./brand";
import { notify } from "./notify";
import { voice } from "./voice";
import { navigate } from "./router";

let ws: WebSocket | null = null;
let retry = 0;
let stopped = false;
let pingTimer: number | undefined;

export function connectGateway() {
  stopped = false;
  open();
}

export function disconnectGateway() {
  stopped = true;
  ws?.close();
  ws = null;
}

export function send(op: Record<string, unknown>) {
  if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify(op));
}

function open() {
  const proto = location.protocol === "https:" ? "wss:" : "ws:";
  const sock = new WebSocket(`${proto}//${location.host}/api/ws`);
  ws = sock;
  set({ connection: get().ready ? "reconnecting" : "connecting" });
  sock.onmessage = (e) => {
    const { t, d } = JSON.parse(e.data);
    try {
      handle(t, d);
    } catch (err) {
      console.error("gateway", t, err);
    }
  };
  sock.onopen = () => {
    retry = 0;
    clearInterval(pingTimer);
    pingTimer = window.setInterval(() => send({ op: "ping" }), 30000);
  };
  sock.onclose = (e) => {
    clearInterval(pingTimer);
    if (ws !== sock || stopped) return;
    set({ connection: "reconnecting" });
    if (e.code === 1006 || e.code === 1000 || e.code === 1001) {
      // Could be a logged-out session; check before retrying forever.
      fetch("/api/users/@me", { credentials: "same-origin" }).then((r) => {
        if (r.status === 401) location.reload();
      }, () => {});
    }
    const wait = Math.min(1000 * 2 ** retry, 15000) + Math.random() * 500;
    retry++;
    setTimeout(() => !stopped && open(), wait);
  };
}

// Reconnect promptly when the network comes back or the tab wakes up.
window.addEventListener("online", () => {
  if (!stopped && ws?.readyState !== WebSocket.OPEN) {
    retry = 0;
    ws?.close();
    open();
  }
});

function handle(t: string, d: any) {
  const s = get();
  switch (t) {
    case "READY":
      applyReady(d);
      voice.resync();
      break;
    case "PONG":
      break;
    case "GUILD_SYNC":
      upsertGuild(d as Guild);
      voice.onGuildChanged(d.id);
      break;
    case "GUILD_DELETE":
      removeGuild(d.id);
      if (voice.guildId === d.id) voice.leave();
      break;
    case "DM_CREATE":
      upsertDM(d as DM);
      break;
    case "USER_UPDATE": {
      const u = d as User;
      const users = { ...s.users, [u.id]: u };
      const guilds = { ...s.guilds };
      for (const g of Object.values(guilds)) {
        if (g.members.some((m) => m.user_id === u.id)) {
          guilds[g.id] = { ...g, members: g.members.map((m) => (m.user_id === u.id ? { ...m, user: u } : m)) };
        }
      }
      const dms = { ...s.dms };
      for (const dm of Object.values(dms)) {
        if (dm.users.some((x) => x.id === u.id)) dms[dm.id] = { ...dm, users: dm.users.map((x) => (x.id === u.id ? u : x)) };
      }
      set({ users, guilds, dms, me: s.me?.id === u.id ? { ...s.me, ...u } : s.me });
      break;
    }
    case "PRESENCE": {
      const p = d as Presence;
      set({ presences: { ...s.presences, [p.user_id]: p } });
      break;
    }
    case "MESSAGE_CREATE": {
      const m = d as Message;
      addMessage(m);
      onNewMessage(m);
      break;
    }
    case "MESSAGE_UPDATE":
      updateMessage(d.channel_id, d.id, () => d);
      break;
    case "MESSAGE_DELETE":
      updateMessage(d.channel_id, d.id, () => null);
      break;
    case "REACTION_ADD":
    case "REACTION_REMOVE": {
      const add = t === "REACTION_ADD";
      updateMessage(d.channel_id, d.message_id, (m) => {
        let reactions = m.reactions.map((r) => ({ ...r, users: [...r.users] }));
        let r = reactions.find((r) => r.emoji === d.emoji);
        if (add) {
          if (!r) reactions.push((r = { emoji: d.emoji, users: [] }));
          if (!r.users.includes(d.user_id)) r.users.push(d.user_id);
        } else if (r) {
          r.users = r.users.filter((u) => u !== d.user_id);
          reactions = reactions.filter((x) => x.users.length);
        }
        return { ...m, reactions };
      });
      break;
    }
    case "TYPING": {
      if (d.user_id === s.me?.id) break;
      const cur = { ...(s.typing[d.channel_id] ?? {}), [d.user_id]: Date.now() + 9000 };
      set({ typing: { ...s.typing, [d.channel_id]: cur } });
      break;
    }
    case "READ_STATE":
      set({ readStates: { ...s.readStates, [d.channel_id]: { channel_id: d.channel_id, last_read_id: d.last_read_id, mention_count: 0 } } });
      break;
    case "VOICE_STATE": {
      const v = d as VoiceState & { left_channel_id?: ID };
      const prev = s.voiceStates[v.user_id];
      const voiceStates = { ...s.voiceStates };
      if (v.channel_id) voiceStates[v.user_id] = v;
      else delete voiceStates[v.user_id];
      set({ voiceStates });
      // Join and leave chimes for the channel you're in.
      const mine = voice.channelId;
      if (mine && v.user_id !== s.me?.id) {
        const was = prev?.channel_id === mine;
        const is = v.channel_id === mine;
        if (!was && is) playSound("join");
        if (was && !is) playSound("leave");
      }
      if (v.user_id === s.me?.id) voice.onOwnState(v);
      if (s.incomingCall && v.channel_id === s.incomingCall.channel_id && v.user_id === s.me?.id) set({ incomingCall: null });
      if (s.incomingCall && !v.channel_id && v.left_channel_id === s.incomingCall.channel_id) {
        // The caller hung up before we answered.
        const stillThere = Object.values(voiceStates).some((x) => x.channel_id === s.incomingCall!.channel_id);
        if (!stillThere) set({ incomingCall: null });
      }
      break;
    }
    case "VOICE_MOVE": {
      // Follow the move on screen if you were looking at the old channel.
      const viewing = currentChannelId();
      const was = voice.channelId;
      voice.join(d.channel_id);
      if (viewing && viewing === was) navigate(d.guild_id, d.channel_id);
      break;
    }
    case "CALL_RING":
      if (voice.channelId !== d.channel_id) {
        set({ incomingCall: { channel_id: d.channel_id, user_id: d.user_id } });
        playSound("ring");
        notify(`${displayName(d.user_id)} is calling`, `Open ${appName()} to answer`, d.channel_id);
      }
      break;
  }
}

function onNewMessage(m: Message) {
  const s = get();
  const me = s.me;
  if (!me || m.author.id === me.id) return;
  const viewing = currentChannelId() === m.channel_id && document.hasFocus();
  if (viewing) return;
  const c = channelById(m.channel_id);
  if (!c) return;
  let mentioned = c.type === "dm" || m.mentions.includes(me.id) || m.mention_everyone;
  if (!mentioned && m.guild_id) {
    const mine = s.guilds[m.guild_id]?.members.find((x) => x.user_id === me.id)?.roles ?? [];
    mentioned = m.mention_roles.some((r) => mine.includes(r));
  }
  const rs = s.readStates[m.channel_id];
  if (mentioned) {
    set({ readStates: { ...s.readStates, [m.channel_id]: { channel_id: m.channel_id, last_read_id: rs?.last_read_id ?? null, mention_count: (rs?.mention_count ?? 0) + 1 } } });
    if (s.status !== "dnd") {
      playSound("message");
      const where = c.type === "dm" ? "" : ` (#${c.name})`;
      notify(displayName(m.author.id, m.guild_id) + where, m.content || (m.attachments.length ? "Sent a file" : ""), m.channel_id);
    }
  }
}

export function currentChannelId(): ID | undefined {
  const s = get();
  return s.channelByGuild[s.guildId];
}
