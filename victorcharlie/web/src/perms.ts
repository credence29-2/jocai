import type { Channel, Guild, ID } from "./types";

export const P = {
  ViewChannel: 1 << 0,
  SendMessages: 1 << 1,
  AttachFiles: 1 << 2,
  AddReactions: 1 << 3,
  MentionEveryone: 1 << 4,
  ManageMessages: 1 << 5,
  ReadHistory: 1 << 6,
  Connect: 1 << 7,
  Speak: 1 << 8,
  Video: 1 << 9,
  UseVAD: 1 << 10,
  MuteMembers: 1 << 11,
  DeafenMembers: 1 << 12,
  MoveMembers: 1 << 13,
  KickMembers: 1 << 14,
  BanMembers: 1 << 15,
  CreateInvite: 1 << 16,
  ManageChannels: 1 << 17,
  ManageRoles: 1 << 18,
  ManageServer: 1 << 19,
  ChangeNickname: 1 << 20,
  ManageNicknames: 1 << 21,
  PrioritySpeaker: 1 << 22,
  Administrator: 1 << 30,
};

export const ALL = 2 ** 31 - 1;

// Bitwise ops in JS are 32-bit signed; these keep results positive.
const and = (a: number, b: number) => (a & b) >>> 0;
const or = (a: number, b: number) => (a | b) >>> 0;
const andNot = (a: number, b: number) => (a & ~b) >>> 0;

export const has = (perms: number, p: number) => and(perms, p) === p;

export const PERM_INFO: { key: keyof typeof P; label: string; desc: string; group: string; channel?: "text" | "voice" | "both" }[] = [
  { key: "Administrator", label: "Administrator", desc: "Every permission, and bypasses channel permissions. Give this carefully.", group: "General" },
  { key: "ViewChannel", label: "View Channels", desc: "See channels and read their messages.", group: "General", channel: "both" },
  { key: "ManageChannels", label: "Manage Channels", desc: "Create, edit and delete channels.", group: "General", channel: "both" },
  { key: "ManageRoles", label: "Manage Roles", desc: "Create and edit roles below their own, and set channel permissions.", group: "General", channel: "both" },
  { key: "ManageServer", label: "Manage Server", desc: "Change the server name and icon, and manage invites.", group: "General" },
  { key: "CreateInvite", label: "Create Invite", desc: "Make invite links.", group: "Membership" },
  { key: "ChangeNickname", label: "Change Nickname", desc: "Change their own nickname.", group: "Membership" },
  { key: "ManageNicknames", label: "Manage Nicknames", desc: "Change other people's nicknames.", group: "Membership" },
  { key: "KickMembers", label: "Kick Members", desc: "Remove people from the server. They can rejoin with an invite.", group: "Membership" },
  { key: "BanMembers", label: "Ban Members", desc: "Remove people and stop them from coming back.", group: "Membership" },
  { key: "SendMessages", label: "Send Messages", desc: "Post in text channels.", group: "Text", channel: "text" },
  { key: "AttachFiles", label: "Attach Files", desc: "Upload images and files.", group: "Text", channel: "text" },
  { key: "AddReactions", label: "Add Reactions", desc: "React to messages with emoji.", group: "Text", channel: "text" },
  { key: "MentionEveryone", label: "Mention @everyone and all roles", desc: "Ping everyone at once, or any role.", group: "Text", channel: "text" },
  { key: "ManageMessages", label: "Manage Messages", desc: "Delete other people's messages and pin messages.", group: "Text", channel: "text" },
  { key: "ReadHistory", label: "Read Message History", desc: "Scroll back through earlier messages.", group: "Text", channel: "text" },
  { key: "Connect", label: "Connect", desc: "Join voice channels.", group: "Voice", channel: "voice" },
  { key: "Speak", label: "Speak", desc: "Talk in voice channels.", group: "Voice", channel: "voice" },
  { key: "Video", label: "Video and Screen Share", desc: "Turn on a webcam or share a screen.", group: "Voice", channel: "voice" },
  { key: "UseVAD", label: "Use Voice Activity", desc: "Talk with an open mic. Without this, push-to-talk is required.", group: "Voice", channel: "voice" },
  { key: "PrioritySpeaker", label: "Priority Speaker", desc: "Others get quieter while this person talks.", group: "Voice", channel: "voice" },
  { key: "MuteMembers", label: "Mute Members", desc: "Server-mute people in voice.", group: "Voice", channel: "voice" },
  { key: "DeafenMembers", label: "Deafen Members", desc: "Server-deafen people in voice.", group: "Voice", channel: "voice" },
  { key: "MoveMembers", label: "Move Members", desc: "Drag people between voice channels or disconnect them.", group: "Voice", channel: "voice" },
];

export function basePerms(g: Guild, uid: ID): number {
  if (uid === g.owner_id) return ALL;
  const m = g.members.find((x) => x.user_id === uid);
  if (!m) return 0;
  let p = g.roles.find((r) => r.id === g.id)?.permissions ?? 0;
  for (const rid of m.roles) {
    const r = g.roles.find((x) => x.id === rid);
    if (r) p = or(p, r.permissions);
  }
  if (has(p, P.Administrator)) return ALL;
  return p;
}

export function channelPerms(g: Guild, uid: ID, c: Channel): number {
  let p = basePerms(g, uid);
  if (p === ALL) return p;
  const m = g.members.find((x) => x.user_id === uid);
  if (!m) return 0;
  let allow = 0,
    deny = 0;
  for (const o of c.overwrites) {
    if (o.id === g.id) p = or(andNot(p, o.deny), o.allow);
    else if (m.roles.includes(o.id)) {
      allow = or(allow, o.allow);
      deny = or(deny, o.deny);
    }
  }
  p = or(andNot(p, deny), allow);
  for (const o of c.overwrites) if (o.id === uid) p = or(andNot(p, o.deny), o.allow);
  if (!has(p, P.ViewChannel)) return 0;
  return p;
}

export function topRole(g: Guild, uid: ID): number {
  if (uid === g.owner_id) return 1 << 30;
  const m = g.members.find((x) => x.user_id === uid);
  let top = 0;
  for (const rid of m?.roles ?? []) {
    const r = g.roles.find((x) => x.id === rid);
    if (r && r.position > top) top = r.position;
  }
  return top;
}

export function outranks(g: Guild, actor: ID, target: ID): boolean {
  if (target === g.owner_id) return false;
  return actor === g.owner_id || topRole(g, actor) > topRole(g, target);
}

export const bits = { and, or, andNot };
