export type ID = string;

export interface User {
  id: ID;
  username: string;
  display_name: string;
  avatar: string;
  about: string;
  accent: number;
  custom_status: string;
  is_admin?: boolean;
}

export interface Role {
  id: ID;
  guild_id: ID;
  name: string;
  color: number;
  position: number;
  permissions: number;
  hoist: boolean;
  mentionable: boolean;
}

export interface Overwrite {
  id: ID;
  allow: number;
  deny: number;
}

export type ChannelType = "text" | "voice" | "category" | "dm";

export interface Channel {
  id: ID;
  guild_id: ID | null;
  type: ChannelType;
  name: string;
  topic: string;
  position: number;
  parent_id: ID | null;
  overwrites: Overwrite[];
  bitrate: number;
  user_limit: number;
  slowmode: number;
  owner_id: ID | null;
  recipients?: ID[];
  last_message_id: ID | null;
}

export interface DM extends Channel {
  users: User[];
}

export interface Member {
  guild_id: ID;
  user_id: ID;
  nick: string;
  roles: ID[];
  joined_at: number;
  mute: boolean;
  deaf: boolean;
  user: User;
}

export interface Guild {
  id: ID;
  name: string;
  icon: string;
  owner_id: ID;
  afk_channel_id: ID | null;
  system_channel_id: ID | null;
  roles: Role[];
  channels: Channel[];
  members: Member[];
  permissions: number;
}

export interface Attachment {
  id: ID;
  filename: string;
  size: number;
  content_type: string;
  url: string;
  width?: number;
  height?: number;
}

export interface Reaction {
  emoji: string;
  users: ID[];
}

export interface Message {
  id: ID;
  channel_id: ID;
  guild_id: ID | null;
  author: User;
  type: "default" | "join";
  content: string;
  edited_at: number;
  reply_to: ID | null;
  referenced?: Message;
  attachments: Attachment[];
  mentions: ID[];
  mention_roles: ID[];
  mention_everyone: boolean;
  pinned: boolean;
  reactions: Reaction[];
  nonce?: string;
  // Local only: a message still being sent, or one that failed.
  pending?: boolean;
  failed?: boolean;
}

export interface ReadState {
  channel_id: ID;
  last_read_id: ID | null;
  mention_count: number;
}

export interface VoiceState {
  user_id: ID;
  guild_id: ID | null;
  channel_id: ID | null;
  self_mute: boolean;
  self_deaf: boolean;
  mute: boolean;
  deaf: boolean;
  streaming: boolean;
  video: boolean;
}

export type Status = "online" | "idle" | "dnd" | "invisible" | "offline";

export interface Presence {
  user_id: ID;
  status: Status;
  custom_status: string;
}

export interface Ready {
  user: User;
  guilds: Guild[];
  dms: DM[];
  read_states: ReadState[];
  voice_states: VoiceState[];
  presences: Presence[];
  status: Status;
  max_upload_mb: number;
}

export interface Invite {
  code: string;
  guild_id: ID;
  creator_id: ID;
  uses: number;
  max_uses: number;
  expires_at: number;
}
