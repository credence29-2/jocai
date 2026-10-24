import { channelPerms, has, P } from "../perms";
import { displayName, roleColor, set, statusOf, useStore } from "../store";
import type { Channel, DM, Guild, Member, User } from "../types";
import { Avatar } from "./common";
import { Icon } from "./Icon";
import { userMenu } from "./Sidebar";

export function Members({ guild, channel, dm }: { guild?: Guild; channel?: Channel; dm?: DM }) {
  const presences = useStore((s) => s.presences);
  const voiceStates = useStore((s) => s.voiceStates);
  void presences;
  if (dm) {
    return (
      <aside className="members">
        <div className="member-group">Members — {dm.users.length}</div>
        {dm.users.map((u) => (
          <MemberRow key={u.id} user={u} guildId={null} owner={dm.owner_id === u.id} />
        ))}
      </aside>
    );
  }
  const g = guild!;
  // People who can see this channel, grouped by their highest hoisted role,
  // then online and offline.
  const visible = g.members.filter((m) => !channel || has(channelPerms(g, m.user_id, channel), P.ViewChannel));
  const hoisted = g.roles.filter((r) => r.hoist && r.id !== g.id).sort((a, b) => b.position - a.position);
  const groups = new Map<string, { name: string; list: Member[] }>();
  for (const r of hoisted) groups.set(r.id, { name: r.name, list: [] });
  const online: Member[] = [];
  const offline: Member[] = [];
  for (const m of visible) {
    if (statusOf(m.user_id) === "offline") {
      offline.push(m);
      continue;
    }
    const top = hoisted.find((r) => m.roles.includes(r.id));
    if (top) groups.get(top.id)!.list.push(m);
    else online.push(m);
  }
  const byName = (a: Member, b: Member) => displayName(a.user_id, g.id).localeCompare(displayName(b.user_id, g.id));
  const sections = [...groups.values(), { name: "Online", list: online }, { name: "Offline", list: offline }].filter((s) => s.list.length);
  return (
    <aside className="members">
      <button className="icon-btn mobile-only members-close" onClick={() => set({ mobilePane: "main" })}>
        <Icon name="back" />
      </button>
      {sections.map((s) => (
        <div key={s.name}>
          <div className="member-group">
            {s.name} — {s.list.length}
          </div>
          {s.list.sort(byName).map((m) => (
            <MemberRow key={m.user_id} user={m.user} guildId={g.id} owner={g.owner_id === m.user_id} inVoice={!!voiceStates[m.user_id]?.channel_id && voiceStates[m.user_id]?.guild_id === g.id} />
          ))}
        </div>
      ))}
    </aside>
  );
}

function MemberRow({ user, guildId, owner, inVoice }: { user: User; guildId: string | null; owner?: boolean; inVoice?: boolean }) {
  const status = statusOf(user.id);
  return (
    <div className={"member" + (status === "offline" ? " offline" : "")} onClick={() => set({ modal: { kind: "profile", userId: user.id, guildId } })} onContextMenu={(e) => userMenu(e, user.id, guildId)}>
      <Avatar user={user} size={32} status={status} />
      <div className="member-text">
        <span className="truncate" style={{ color: roleColor(user.id, guildId) }}>
          {displayName(user.id, guildId)}
          {owner && <Icon name="crown" size={14} className="crown" />}
        </span>
        {user.custom_status && status !== "offline" && <span className="truncate muted small">{user.custom_status}</span>}
      </div>
      {inVoice && <Icon name="speaker" size={14} className="muted" />}
    </div>
  );
}
