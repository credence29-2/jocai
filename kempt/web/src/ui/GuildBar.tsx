import { api } from "../api";
import { has, P } from "../perms";
import { selectGuild } from "../router";
import { get, isUnread, set, toast, useStore } from "../store";
import type { Guild } from "../types";
import { GuildIcon, openMenu } from "./common";
import { Icon } from "./Icon";

export function GuildBar() {
  const guilds = useStore((s) => s.guilds);
  const current = useStore((s) => s.guildId);
  const readStates = useStore((s) => s.readStates);
  const dms = useStore((s) => s.dms);
  const voiceStates = useStore((s) => s.voiceStates);
  const me = useStore((s) => s.me);

  const order = Object.values(guilds).sort((a, b) => (BigInt(a.id) < BigInt(b.id) ? -1 : 1));
  const dmMentions = Object.values(dms).reduce((n, d) => n + (readStates[d.id]?.mention_count ?? 0), 0);

  return (
    <nav className="guildbar">
      <GuildButton active={current === "@me"} label="Direct Messages" badge={dmMentions} onClick={() => selectGuild("@me")}>
        <div className="guild-icon-text home">
          <Icon name="message" size={24} />
        </div>
      </GuildButton>
      <div className="guild-sep" />
      {order.map((g) => {
        const mentions = g.channels.reduce((n, c) => n + (readStates[c.id]?.mention_count ?? 0), 0);
        const unread = g.channels.some((c) => c.type === "text" && isUnread(c));
        const inVoice = !!me && voiceStates[me.id]?.guild_id === g.id;
        return (
          <GuildButton key={g.id} active={current === g.id} label={g.name} badge={mentions} unread={unread} voice={inVoice} onClick={() => selectGuild(g.id)} onContext={(e) => guildMenu(e, g)}>
            <GuildIcon name={g.name} icon={g.icon} />
          </GuildButton>
        );
      })}
      <GuildButton label="Add a server" onClick={() => set({ modal: { kind: "createGuild" } })} className="add">
        <div className="guild-icon-text add">
          <Icon name="plus" size={24} />
        </div>
      </GuildButton>
    </nav>
  );
}

function GuildButton({ children, active, label, badge, unread, voice, onClick, onContext, className }: { children: React.ReactNode; active?: boolean; label: string; badge?: number; unread?: boolean; voice?: boolean; onClick: () => void; onContext?: (e: React.MouseEvent) => void; className?: string }) {
  return (
    <div className={"guild-btn" + (active ? " active" : "") + (unread ? " unread" : "") + (className ? " " + className : "")} title={label}>
      <span className="guild-pill" />
      <button onClick={onClick} onContextMenu={onContext} aria-label={label}>
        {children}
      </button>
      {!!badge && <span className="badge">{badge > 99 ? "99+" : badge}</span>}
      {voice && !badge && (
        <span className="voice-dot">
          <Icon name="speaker" size={10} />
        </span>
      )}
    </div>
  );
}

export function guildMenu(e: React.MouseEvent, g: Guild) {
  const me = get().me!;
  const perms = g.permissions;
  openMenu(e, [
    {
      label: "Mark as read",
      onClick: () => {
        for (const c of g.channels) if (c.type === "text" && c.last_message_id && isUnread(c)) api.post(`/channels/${c.id}/ack`, { message_id: c.last_message_id }).catch(() => {});
      },
    },
    ...(has(perms, P.CreateInvite) ? [{ label: "Invite people", onClick: () => set({ modal: { kind: "invite", guildId: g.id } }) }] : []),
    ...(has(perms, P.ManageServer) || has(perms, P.ManageRoles) || has(perms, P.KickMembers) || has(perms, P.BanMembers) ? [{ label: "Server settings", onClick: () => set({ modal: { kind: "guildSettings", guildId: g.id } }) }] : []),
    ...(has(perms, P.ManageChannels)
      ? [
          { label: "Create channel", onClick: () => set({ modal: { kind: "createChannel", guildId: g.id, type: "text" } }) },
          { label: "Create category", onClick: () => set({ modal: { kind: "createChannel", guildId: g.id, type: "category" } }) },
        ]
      : []),
    { divider: true, label: "" },
    { label: "Copy server ID", onClick: () => navigator.clipboard?.writeText(g.id) },
    ...(g.owner_id !== me.id
      ? [
          {
            label: "Leave server",
            danger: true,
            onClick: () =>
              set({
                modal: {
                  kind: "confirm",
                  title: `Leave ${g.name}`,
                  body: "You won't be able to rejoin unless someone invites you again.",
                  action: "Leave",
                  danger: true,
                  onConfirm: () => api.del(`/guilds/${g.id}/members/@me`).catch((e) => toast(e.message)),
                },
              }),
          },
        ]
      : []),
  ]);
}
