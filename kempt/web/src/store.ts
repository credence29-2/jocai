import { create } from "zustand";
import type { Channel, DM, Guild, ID, Message, Presence, ReadState, Ready, Status, User, VoiceState } from "./types";

export interface ChannelMessages {
  list: Message[];
  hasMore: boolean;
  loading: boolean;
  loaded: boolean;
}

export type Modal =
  | { kind: "createGuild" }
  | { kind: "invite"; guildId: ID }
  | { kind: "guildSettings"; guildId: ID; tab?: string }
  | { kind: "channelSettings"; channelId: ID }
  | { kind: "createChannel"; guildId: ID; type: "text" | "voice" | "category"; parentId?: ID | null }
  | { kind: "userSettings"; tab?: string }
  | { kind: "profile"; userId: ID; guildId?: ID | null }
  | { kind: "newDM" }
  | { kind: "image"; url: string; name: string }
  | { kind: "confirm"; title: string; body: string; action: string; danger?: boolean; onConfirm: () => unknown }
  | { kind: "screenPicker" }
  | { kind: "pins"; channelId: ID }
  | { kind: "search"; guildId: ID | null; channelId?: ID };

export interface ContextMenuItem {
  label: string;
  danger?: boolean;
  checked?: boolean;
  disabled?: boolean;
  onClick?: () => void;
  slider?: { value: number; min: number; max: number; onChange: (v: number) => void; format?: (v: number) => string };
  divider?: boolean;
}

export interface State {
  me: User | null;
  status: Status;
  ready: boolean;
  connection: "connecting" | "ready" | "reconnecting" | "offline";
  maxUploadMB: number;
  guilds: Record<ID, Guild>;
  dms: Record<ID, DM>;
  users: Record<ID, User>;
  presences: Record<ID, Presence>;
  readStates: Record<ID, ReadState>;
  voiceStates: Record<ID, VoiceState>;
  messages: Record<ID, ChannelMessages>;
  typing: Record<ID, Record<ID, number>>;

  // UI
  guildId: ID | "@me";
  channelByGuild: Record<string, ID>;
  modal: Modal | null;
  menu: { x: number; y: number; items: ContextMenuItem[] } | null;
  replyTo: Record<ID, Message | undefined>;
  editing: ID | null;
  showMembers: boolean;
  mobilePane: "sidebar" | "main" | "members";
  incomingCall: { channel_id: ID; user_id: ID } | null;
  toast: { text: string; id: number } | null;
}

export const useStore = create<State>(() => ({
  me: null,
  status: "online",
  ready: false,
  connection: "connecting",
  maxUploadMB: 100,
  guilds: {},
  dms: {},
  users: {},
  presences: {},
  readStates: {},
  voiceStates: {},
  messages: {},
  typing: {},
  guildId: "@me",
  channelByGuild: {},
  modal: null,
  menu: null,
  replyTo: {},
  editing: null,
  showMembers: localStorage.getItem("kempt.showMembers") !== "0",
  mobilePane: "sidebar",
  incomingCall: null,
  toast: null,
}));

export const get = useStore.getState;
export const set = useStore.setState;

export function toast(text: string) {
  const id = Date.now();
  set({ toast: { text, id } });
  setTimeout(() => {
    if (get().toast?.id === id) set({ toast: null });
  }, 4000);
}

function collectUsers(guilds: Guild[], dms: DM[], into: Record<ID, User>) {
  for (const g of guilds) for (const m of g.members) into[m.user_id] = m.user;
  for (const d of dms) for (const u of d.users) into[u.id] = u;
  return into;
}

export function applyReady(r: Ready) {
  const guilds: Record<ID, Guild> = {};
  for (const g of r.guilds) guilds[g.id] = g;
  const dms: Record<ID, DM> = {};
  for (const d of r.dms) dms[d.id] = d;
  const readStates: Record<ID, ReadState> = {};
  for (const rs of r.read_states) readStates[rs.channel_id] = rs;
  const voiceStates: Record<ID, VoiceState> = {};
  for (const v of r.voice_states) voiceStates[v.user_id] = v;
  const presences: Record<ID, Presence> = {};
  for (const p of r.presences ?? []) presences[p.user_id] = p;
  const users = collectUsers(r.guilds, r.dms, { [r.user.id]: r.user });
  const s = get();
  // A reconnect may have missed messages; drop cached history so open
  // channels refetch.
  set({
    me: r.user,
    status: r.status,
    ready: true,
    connection: "ready",
    maxUploadMB: r.max_upload_mb,
    guilds,
    dms,
    users,
    presences,
    readStates,
    voiceStates,
    messages: s.ready ? {} : s.messages,
  });
}

export function upsertGuild(g: Guild) {
  set((s) => {
    const users = { ...s.users };
    for (const m of g.members) users[m.user_id] = m.user;
    return { guilds: { ...s.guilds, [g.id]: g }, users };
  });
}

export function removeGuild(id: ID) {
  set((s) => {
    const guilds = { ...s.guilds };
    delete guilds[id];
    return { guilds, guildId: s.guildId === id ? "@me" : s.guildId };
  });
}

export function upsertDM(d: DM) {
  set((s) => {
    const users = { ...s.users };
    for (const u of d.users) users[u.id] = u;
    return { dms: { ...s.dms, [d.id]: d }, users };
  });
}

export function channelById(id: ID | undefined | null): Channel | DM | undefined {
  if (!id) return undefined;
  const s = get();
  if (s.dms[id]) return s.dms[id];
  for (const g of Object.values(s.guilds)) {
    const c = g.channels.find((c) => c.id === id);
    if (c) return c;
  }
  return undefined;
}

export function addMessage(m: Message) {
  set((s) => {
    const cm = s.messages[m.channel_id];
    const out: Partial<State> = {};
    if (cm?.loaded) {
      let list = cm.list;
      const i = m.nonce ? list.findIndex((x) => x.nonce === m.nonce) : -1;
      if (i >= 0) {
        list = list.slice();
        list[i] = m;
      } else if (!list.some((x) => x.id === m.id)) {
        list = [...list, m];
        if (list.length > 400) list = list.slice(-300);
      }
      out.messages = { ...s.messages, [m.channel_id]: { ...cm, list, hasMore: cm.hasMore || cm.list.length !== list.length - 1 } };
    }
    if (!m.pending) {
      out.users = { ...s.users, [m.author.id]: m.author };
      out.guilds = bumpLast(s.guilds, m);
      if (s.dms[m.channel_id]) out.dms = { ...s.dms, [m.channel_id]: { ...s.dms[m.channel_id], last_message_id: m.id } };
      const t = s.typing[m.channel_id];
      if (t?.[m.author.id]) {
        const nt = { ...t };
        delete nt[m.author.id];
        out.typing = { ...s.typing, [m.channel_id]: nt };
      }
    }
    return out;
  });
}

function bumpLast(guilds: Record<ID, Guild>, m: Message) {
  if (!m.guild_id || !guilds[m.guild_id]) return guilds;
  const g = guilds[m.guild_id];
  return { ...guilds, [g.id]: { ...g, channels: g.channels.map((c) => (c.id === m.channel_id ? { ...c, last_message_id: m.id } : c)) } };
}

export function updateMessage(cid: ID, id: ID, fn: (m: Message) => Message | null) {
  set((s) => {
    const cm = s.messages[cid];
    if (!cm) return {};
    const list: Message[] = [];
    for (const m of cm.list) {
      if (m.id === id || m.nonce === id) {
        const n = fn(m);
        if (n) list.push(n);
      } else {
        // Replies show a preview of the message they point at.
        if (m.referenced?.id === id) {
          const n = fn(m.referenced);
          list.push({ ...m, referenced: n ?? undefined });
        } else list.push(m);
      }
    }
    return { messages: { ...s.messages, [cid]: { ...cm, list } } };
  });
}

export function isUnread(c: Channel): boolean {
  const s = get();
  if (!c.last_message_id) return false;
  const rs = s.readStates[c.id];
  return !rs?.last_read_id || BigInt(c.last_message_id) > BigInt(rs.last_read_id);
}

export function displayName(uid: ID, guildId?: ID | null): string {
  const s = get();
  if (guildId) {
    const m = s.guilds[guildId]?.members.find((m) => m.user_id === uid);
    if (m?.nick) return m.nick;
  }
  const u = s.users[uid];
  return u?.display_name || u?.username || "Unknown";
}

export function roleColor(uid: ID, guildId?: ID | null): string | undefined {
  if (!guildId) return undefined;
  const g = get().guilds[guildId];
  const m = g?.members.find((m) => m.user_id === uid);
  if (!g || !m) return undefined;
  let best: { pos: number; color: number } | null = null;
  for (const rid of m.roles) {
    const r = g.roles.find((r) => r.id === rid);
    if (r && r.color && (!best || r.position > best.pos)) best = { pos: r.position, color: r.color };
  }
  return best ? "#" + best.color.toString(16).padStart(6, "0") : undefined;
}

export function statusOf(uid: ID): Status {
  const s = get();
  if (uid === s.me?.id) return s.connection === "ready" ? (s.status === "invisible" ? "offline" : s.status) : "offline";
  return s.presences[uid]?.status ?? "offline";
}
