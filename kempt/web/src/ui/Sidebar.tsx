import { useState } from "react";
import { api } from "../api";
import { bits, channelPerms, has, outranks, P } from "../perms";
import { navigate } from "../router";
import { displayName, get, isUnread, set, statusOf, toast, useStore } from "../store";
import type { Channel, DM, Guild, ID, Status, VoiceState } from "../types";
import { useVoice, voice } from "../voice";
import { Avatar, openMenu } from "./common";
import { guildMenu } from "./GuildBar";
import { Icon } from "./Icon";

export function Sidebar() {
  const guildId = useStore((s) => s.guildId);
  const guild = useStore((s) => (s.guildId === "@me" ? null : s.guilds[s.guildId]));
  return (
    <aside className="sidebar">
      {guild ? (
        <>
          <button className="sidebar-head" onClick={(e) => guildMenu({ ...e, clientX: e.currentTarget.getBoundingClientRect().left + 8, clientY: e.currentTarget.getBoundingClientRect().bottom + 4 } as any, guild)}>
            <span className="truncate">{guild.name}</span>
            <Icon name="chevronDown" size={18} />
          </button>
          <ChannelList guild={guild} />
        </>
      ) : (
        <>
          <div className="sidebar-head static">Direct Messages</div>
          <DMList key={guildId} />
        </>
      )}
      <VoicePanel />
      <UserPanel />
    </aside>
  );
}

function sortChannels(list: Channel[]) {
  return [...list].sort((a, b) => {
    const av = a.type === "voice" ? 1 : 0;
    const bv = b.type === "voice" ? 1 : 0;
    return av - bv || a.position - b.position || (BigInt(a.id) < BigInt(b.id) ? -1 : 1);
  });
}

function ChannelList({ guild }: { guild: Guild }) {
  const current = useStore((s) => s.channelByGuild[guild.id]);
  const readStates = useStore((s) => s.readStates);
  const voiceStates = useStore((s) => s.voiceStates);
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>(() => JSON.parse(localStorage.getItem("kempt.collapsed") || "{}"));
  void readStates;
  const toggle = (id: ID) => {
    const n = { ...collapsed, [id]: !collapsed[id] };
    setCollapsed(n);
    localStorage.setItem("kempt.collapsed", JSON.stringify(n));
  };
  const cats = guild.channels.filter((c) => c.type === "category").sort((a, b) => a.position - b.position);
  const loose = sortChannels(guild.channels.filter((c) => c.type !== "category" && (!c.parent_id || !cats.some((k) => k.id === c.parent_id))));
  const canManage = has(guild.permissions, P.ManageChannels);
  const render = (c: Channel) => <ChannelRow key={c.id} c={c} guild={guild} active={current === c.id} voiceStates={voiceStates} hidden={false} />;
  return (
    <div className="channel-list" onContextMenu={(e) => e.target === e.currentTarget && canManage && openMenu(e, [
      { label: "Create channel", onClick: () => set({ modal: { kind: "createChannel", guildId: guild.id, type: "text" } }) },
      { label: "Create category", onClick: () => set({ modal: { kind: "createChannel", guildId: guild.id, type: "category" } }) },
    ])}>
      {loose.map(render)}
      {cats.map((cat) => {
        const kids = sortChannels(guild.channels.filter((c) => c.parent_id === cat.id));
        const isCollapsed = collapsed[cat.id];
        return (
          <div key={cat.id} className="category">
            <div
              className="category-head"
              onClick={() => toggle(cat.id)}
              onContextMenu={(e) =>
                openMenu(e, [
                  { label: isCollapsed ? "Expand category" : "Collapse category", onClick: () => toggle(cat.id) },
                  ...(canManage
                    ? [
                        { label: "Edit category", onClick: () => set({ modal: { kind: "channelSettings", channelId: cat.id } }) },
                        { label: "Delete category", danger: true, onClick: () => confirmDelete(cat) },
                      ]
                    : []),
                ])
              }
            >
              <Icon name={isCollapsed ? "chevronRight" : "chevronDown"} size={12} />
              <span className="truncate">{cat.name}</span>
              {canManage && (
                <button
                  className="icon-btn tiny"
                  title="Create channel"
                  onClick={(e) => {
                    e.stopPropagation();
                    set({ modal: { kind: "createChannel", guildId: guild.id, type: "text", parentId: cat.id } });
                  }}
                >
                  <Icon name="plus" size={16} />
                </button>
              )}
            </div>
            {/* Collapsed categories still show the channel you're in or talking in. */}
            {kids.filter((c) => !isCollapsed || c.id === current || Object.values(voiceStates).some((v) => v.channel_id === c.id)).map(render)}
          </div>
        );
      })}
    </div>
  );
}

function confirmDelete(c: Channel) {
  set({
    modal: {
      kind: "confirm",
      title: `Delete ${c.type === "category" ? "category" : "channel"}`,
      body: c.type === "text" ? `Delete #${c.name}? Every message in it goes too, and this can't be undone.` : `Delete ${c.name}? This can't be undone.`,
      action: "Delete",
      danger: true,
      onConfirm: () => api.del(`/channels/${c.id}`).catch((e) => toast(e.message)),
    },
  });
}

function ChannelRow({ c, guild, active, voiceStates }: { c: Channel; guild: Guild; active: boolean; voiceStates: Record<ID, VoiceState>; hidden: boolean }) {
  const me = useStore((s) => s.me)!;
  const rs = useStore((s) => s.readStates[c.id]);
  const [dragOver, setDragOver] = useState(false);
  const unread = c.type === "text" && isUnread(c);
  const mentions = rs?.mention_count ?? 0;
  const everyone = c.overwrites.find((o) => o.id === guild.id);
  const isPrivate = !!everyone && has(everyone.deny, P.ViewChannel);
  const inVoice = Object.values(voiceStates).filter((v) => v.channel_id === c.id);
  const perms = channelPerms(guild, me.id, c);
  const canManage = has(perms, P.ManageChannels);
  const canMove = has(guild.permissions, P.MoveMembers);

  const open = () => {
    if (c.type === "voice") {
      if (useVoice.getState().channelId !== c.id) {
        if (!has(perms, P.Connect)) return toast("You don't have permission to join that channel.");
        voice.join(c.id);
      }
    }
    navigate(guild.id, c.id);
  };

  const menu = (e: React.MouseEvent) =>
    openMenu(e, [
      ...(c.type === "text" && unread && c.last_message_id ? [{ label: "Mark as read", onClick: () => api.post(`/channels/${c.id}/ack`, { message_id: c.last_message_id }) }] : []),
      ...(has(perms, P.CreateInvite) ? [{ label: "Invite people", onClick: () => set({ modal: { kind: "invite", guildId: guild.id } }) }] : []),
      ...(c.type === "voice" && useVoice.getState().channelId === c.id ? [{ label: "Disconnect", onClick: () => voice.leave() }] : []),
      ...(canManage
        ? [
            { label: "Edit channel", onClick: () => set({ modal: { kind: "channelSettings", channelId: c.id } }) },
            { label: "Move up", onClick: () => move(guild, c, -1) },
            { label: "Move down", onClick: () => move(guild, c, 1) },
            { label: "Delete channel", danger: true, onClick: () => confirmDelete(c) },
          ]
        : []),
      { divider: true, label: "" },
      { label: "Copy channel ID", onClick: () => navigator.clipboard?.writeText(c.id) },
    ]);

  return (
    <>
      <div
        className={"channel" + (active ? " active" : "") + (unread ? " unread" : "") + (dragOver ? " drag-over" : "")}
        onClick={open}
        onContextMenu={menu}
        onDragOver={(e) => {
          if (c.type === "voice" && canMove && e.dataTransfer.types.includes("kempt/user")) {
            e.preventDefault();
            setDragOver(true);
          }
        }}
        onDragLeave={() => setDragOver(false)}
        onDrop={(e) => {
          setDragOver(false);
          const uid = e.dataTransfer.getData("kempt/user");
          if (uid) api.patch(`/guilds/${guild.id}/members/${uid}`, { channel_id: c.id }).catch((err) => toast(err.message));
        }}
      >
        {unread && !active && <span className="unread-pill" />}
        <Icon name={c.type === "voice" ? "speaker" : "hash"} size={18} className="channel-icon" />
        <span className="truncate channel-name">{c.name}</span>
        {isPrivate && <Icon name="lock" size={12} className="muted" />}
        {c.type === "voice" && c.user_limit > 0 && (
          <span className="user-limit">
            {inVoice.length}/{c.user_limit}
          </span>
        )}
        {mentions > 0 && !active && <span className="badge inline">{mentions}</span>}
        {c.type === "voice" && (
          <button
            className="icon-btn tiny hover-only"
            title="Open chat"
            onClick={(e) => {
              e.stopPropagation();
              navigate(guild.id, c.id);
            }}
          >
            <Icon name="users" size={14} />
          </button>
        )}
      </div>
      {c.type === "voice" &&
        inVoice
          .sort((a, b) => displayName(a.user_id, guild.id).localeCompare(displayName(b.user_id, guild.id)))
          .map((v) => <VoiceUser key={v.user_id} v={v} guild={guild} canDrag={canMove} />)}
    </>
  );
}

function move(guild: Guild, c: Channel, dir: number) {
  const siblings = sortChannels(guild.channels.filter((x) => x.parent_id === c.parent_id && (x.type === "category") === (c.type === "category") && (c.type === "category" || (x.type === "voice") === (c.type === "voice"))));
  const i = siblings.findIndex((x) => x.id === c.id);
  const j = i + dir;
  if (j < 0 || j >= siblings.length) return;
  [siblings[i], siblings[j]] = [siblings[j], siblings[i]];
  api.patch(`/guilds/${guild.id}/channels`, siblings.map((x, k) => ({ id: x.id, position: k }))).catch((e) => toast(e.message));
}

function VoiceUser({ v, guild, canDrag }: { v: VoiceState; guild: Guild | null; canDrag: boolean }) {
  const user = useStore((s) => s.users[v.user_id]);
  const speaking = useVoice((s) => s.speaking[v.user_id]);
  const localMute = useVoice((s) => s.localMutes[v.user_id]);
  return (
    <div
      className="voice-user"
      draggable={canDrag}
      onDragStart={(e) => e.dataTransfer.setData("kempt/user", v.user_id)}
      onClick={() => set({ modal: { kind: "profile", userId: v.user_id, guildId: guild?.id } })}
      onContextMenu={(e) => userMenu(e, v.user_id, guild?.id ?? null)}
    >
      <Avatar user={user} size={24} speaking={speaking && !v.self_mute && !v.mute} />
      <span className="truncate">{displayName(v.user_id, guild?.id)}</span>
      <span className="voice-icons">
        {v.streaming && <span className="live">LIVE</span>}
        {v.video && <Icon name="video" size={14} />}
        {(v.mute || v.self_mute || localMute) && <Icon name="micOff" size={14} className={v.mute ? "danger" : ""} />}
        {(v.deaf || v.self_deaf) && <Icon name="headphonesOff" size={14} className={v.deaf ? "danger" : ""} />}
      </span>
    </div>
  );
}

// userMenu is the right-click menu for a person anywhere in the app.
export function userMenu(e: React.MouseEvent, uid: ID, guildId: ID | null) {
  const s = get();
  const me = s.me!;
  const g = guildId ? s.guilds[guildId] : null;
  const v = s.voiceStates[uid];
  const vs = useVoice.getState();
  const inMyVoice = vs.channelId && v?.channel_id === vs.channelId && uid !== me.id;
  const items: Parameters<typeof openMenu>[1] = [
    { label: "Profile", onClick: () => set({ modal: { kind: "profile", userId: uid, guildId } }) },
  ];
  if (uid !== me.id) {
    items.push({
      label: "Message",
      onClick: async () => {
        try {
          const dm = await api.post<DM>("/users/@me/channels", { recipients: [uid] });
          navigate("@me", dm.id);
        } catch (err: any) {
          toast(err.message);
        }
      },
    });
  }
  if (inMyVoice) {
    items.push(
      { divider: true, label: "" },
      { label: "User volume", slider: { value: Math.round((vs.volumes[uid] ?? 1) * 100), min: 0, max: 200, format: (n) => n + "%", onChange: (n) => voice.setUserVolume(uid, n / 100) } },
      ...(v?.streaming ? [{ label: "Stream volume", slider: { value: Math.round((vs.volumes["s:" + uid] ?? 1) * 100), min: 0, max: 200, format: (n: number) => n + "%", onChange: (n: number) => voice.setUserVolume(uid, n / 100, true) } }] : []),
      { label: "Mute for me", checked: !!vs.localMutes[uid], onClick: () => voice.setLocalMute(uid, !vs.localMutes[uid]) },
    );
  }
  if (g && v?.channel_id && v.guild_id === g.id) {
    const can = (p: number) => has(g.permissions, p) && (uid === me.id || outranks(g, me.id, uid));
    const mod: typeof items = [];
    if (can(P.MuteMembers)) mod.push({ label: "Server mute", checked: v.mute, onClick: () => api.patch(`/guilds/${g.id}/members/${uid}`, { mute: !v.mute }).catch((err) => toast(err.message)) });
    if (can(P.DeafenMembers)) mod.push({ label: "Server deafen", checked: v.deaf, onClick: () => api.patch(`/guilds/${g.id}/members/${uid}`, { deaf: !v.deaf }).catch((err) => toast(err.message)) });
    if (has(g.permissions, P.MoveMembers)) {
      for (const c of g.channels.filter((c) => c.type === "voice" && c.id !== v.channel_id)) {
        mod.push({ label: `Move to ${c.name}`, onClick: () => api.patch(`/guilds/${g.id}/members/${uid}`, { channel_id: c.id }).catch((err) => toast(err.message)) });
      }
      mod.push({ label: "Disconnect", danger: true, onClick: () => api.patch(`/guilds/${g.id}/members/${uid}`, { channel_id: null }).catch((err) => toast(err.message)) });
    }
    if (mod.length) items.push({ divider: true, label: "" }, ...mod);
  }
  if (g && uid !== me.id && g.members.some((m) => m.user_id === uid)) {
    const mod: typeof items = [];
    if (has(g.permissions, P.ManageRoles)) mod.push({ label: "Roles…", onClick: () => set({ modal: { kind: "profile", userId: uid, guildId: g.id } }) });
    if (has(g.permissions, P.KickMembers) && outranks(g, me.id, uid))
      mod.push({
        label: "Kick",
        danger: true,
        onClick: () =>
          set({ modal: { kind: "confirm", title: `Kick ${displayName(uid, g.id)}`, body: "They can rejoin with an invite.", action: "Kick", danger: true, onConfirm: () => api.del(`/guilds/${g.id}/members/${uid}`).catch((err) => toast(err.message)) } }),
      });
    if (has(g.permissions, P.BanMembers) && outranks(g, me.id, uid))
      mod.push({
        label: "Ban",
        danger: true,
        onClick: () =>
          set({ modal: { kind: "confirm", title: `Ban ${displayName(uid, g.id)}`, body: "They'll be removed and can't rejoin until unbanned in Server Settings.", action: "Ban", danger: true, onConfirm: () => api.put(`/guilds/${g.id}/bans/${uid}`, {}).catch((err) => toast(err.message)) } }),
      });
    if (mod.length) items.push({ divider: true, label: "" }, ...mod);
  }
  items.push({ divider: true, label: "" }, { label: "Copy user ID", onClick: () => navigator.clipboard?.writeText(uid) });
  openMenu(e, items);
}

function DMList() {
  const dms = useStore((s) => s.dms);
  const current = useStore((s) => s.channelByGuild["@me"]);
  const me = useStore((s) => s.me)!;
  const readStates = useStore((s) => s.readStates);
  const voiceStates = useStore((s) => s.voiceStates);
  void readStates;
  const list = Object.values(dms).sort((a, b) => (BigInt(b.last_message_id ?? b.id) > BigInt(a.last_message_id ?? a.id) ? 1 : -1));
  return (
    <div className="channel-list">
      <div className="category-head static">
        <span>Direct messages</span>
        <button className="icon-btn tiny" title="New message" onClick={() => set({ modal: { kind: "newDM" } })}>
          <Icon name="plus" size={16} />
        </button>
      </div>
      {list.length === 0 && <div className="empty-hint">No conversations yet. Right-click someone in a server, or use the + above.</div>}
      {list.map((d) => {
        const others = d.users.filter((u) => u.id !== me.id);
        const name = d.name || others.map((u) => u.display_name).join(", ") || "Just you";
        const unread = isUnread(d);
        const mentions = readStates[d.id]?.mention_count ?? 0;
        const call = Object.values(voiceStates).some((v) => v.channel_id === d.id);
        return (
          <div
            key={d.id}
            className={"channel dm" + (current === d.id ? " active" : "") + (unread ? " unread" : "")}
            onClick={() => navigate("@me", d.id)}
            onContextMenu={(e) =>
              openMenu(e, [
                ...(others.length === 1 ? [{ label: "Profile", onClick: () => set({ modal: { kind: "profile", userId: others[0].id } }) }] : []),
                ...(unread && d.last_message_id ? [{ label: "Mark as read", onClick: () => api.post(`/channels/${d.id}/ack`, { message_id: d.last_message_id }) }] : []),
                { label: "Copy channel ID", onClick: () => navigator.clipboard?.writeText(d.id) },
              ])
            }
          >
            {others.length === 1 ? <Avatar user={others[0]} size={32} status={statusOf(others[0].id)} /> : <div className="group-dm-icon"><Icon name="users" size={18} /></div>}
            <div className="dm-text">
              <span className="truncate channel-name">{name}</span>
              {others.length > 1 && <span className="muted small">{d.users.length} members</span>}
              {others.length === 1 && others[0].custom_status && <span className="muted small truncate">{others[0].custom_status}</span>}
            </div>
            {call && <Icon name="speaker" size={14} className="green" />}
            {mentions > 0 && current !== d.id && <span className="badge inline">{mentions}</span>}
          </div>
        );
      })}
    </div>
  );
}

function VoicePanel() {
  const v = useVoice();
  const guilds = useStore((s) => s.guilds);
  const dms = useStore((s) => s.dms);
  if (v.status === "idle" || !v.channelId) return null;
  const g = v.guildId ? guilds[v.guildId] : null;
  const c = g ? g.channels.find((c) => c.id === v.channelId) : dms[v.channelId];
  const label = g ? `${c?.name ?? "Voice"} / ${g.name}` : "Call";
  const quality = v.ping < 1 ? "" : v.ping < 100 ? "good" : v.ping < 250 ? "ok" : "bad";
  return (
    <div className="voice-panel">
      <div className="voice-panel-row">
        <div className="voice-status" onClick={() => c && navigate(g ? g.id : "@me", c.id)}>
          <div className={"voice-conn " + (v.status === "connected" ? "on " + quality : "wait")}>
            <Icon name="signal" size={16} />
            {v.status === "connected" ? "Voice Connected" : v.status === "reconnecting" ? "Reconnecting…" : "Connecting…"}
          </div>
          <div className="muted small truncate" title={v.ping ? `${v.ping} ms` : undefined}>
            {label}
            {v.ping > 0 && ` · ${v.ping} ms`}
          </div>
        </div>
        <button className="icon-btn" title="Disconnect" onClick={() => voice.leave()}>
          <Icon name="hangup" />
        </button>
      </div>
      {v.status === "connected" && (
        <div className="voice-panel-actions">
          <button className={"panel-btn" + (v.camera ? " on" : "")} disabled={!v.canVideo} onClick={() => voice.toggleCamera()} title={v.canVideo ? (v.camera ? "Turn off camera" : "Turn on camera") : "You can't use video here"}>
            <Icon name={v.camera ? "video" : "videoOff"} size={18} />
            <span>Camera</span>
          </button>
          <button className={"panel-btn" + (v.sharing ? " on" : "")} disabled={!v.canShare} onClick={() => voice.toggleShare()} title={v.canShare ? (v.sharing ? "Stop sharing" : "Share your screen") : v.canVideo ? "This device can't share its screen" : "You can't share here"}>
            <Icon name="screen" size={18} />
            <span>{v.sharing ? "Stop" : "Screen"}</span>
          </button>
        </div>
      )}
    </div>
  );
}

const statusLabels: Record<Status, string> = { online: "Online", idle: "Idle", dnd: "Do Not Disturb", invisible: "Invisible", offline: "Offline" };

function UserPanel() {
  const me = useStore((s) => s.me)!;
  const status = useStore((s) => s.status);
  const v = useVoice();
  const own = useStore((s) => s.voiceStates[me.id]);
  const serverMuted = !!own?.mute && v.channelId === own.channel_id;
  const ptt = v.settings.mode === "ptt" || (v.mustPTT && v.status !== "idle");
  const setStatus = (st: Status) => {
    set({ status: st });
    api.patch("/users/@me", { status: st }).catch((e) => toast(e.message));
  };
  return (
    <div className="user-panel">
      <div
        className="user-panel-me"
        onClick={(e) =>
          openMenu(e, [
            ...(["online", "idle", "dnd", "invisible"] as Status[]).map((st) => ({ label: statusLabels[st], checked: status === st, onClick: () => setStatus(st) })),
            { divider: true, label: "" },
            {
              label: "Set a custom status",
              onClick: () => {
                const text = prompt("Custom status", me.custom_status);
                if (text !== null) api.patch("/users/@me", { custom_status: text.trim() }).catch((err) => toast(err.message));
              },
            },
            { label: "Copy user ID", onClick: () => navigator.clipboard?.writeText(me.id) },
          ])
        }
      >
        <Avatar user={me} size={32} status={statusOf(me.id)} speaking={v.speaking[me.id] && !v.selfMute} />
        <div className="user-panel-names">
          <span className="truncate strong">{me.display_name}</span>
          <span className="truncate muted small">{ptt && v.status !== "idle" ? (v.pttDown ? "Talking" : "Push to talk") : me.custom_status || statusLabels[status]}</span>
        </div>
      </div>
      <button className={"icon-btn" + (v.selfMute || serverMuted ? " danger" : "")} title={serverMuted ? "Server muted" : v.selfMute ? "Unmute" : "Mute"} onClick={() => voice.toggleMute()}>
        <Icon name={v.selfMute || serverMuted ? "micOff" : "mic"} />
      </button>
      <button className={"icon-btn" + (v.selfDeaf ? " danger" : "")} title={v.selfDeaf ? "Undeafen" : "Deafen"} onClick={() => voice.toggleDeaf()}>
        <Icon name={v.selfDeaf ? "headphonesOff" : "headphones"} />
      </button>
      <button className="icon-btn" title="Settings" onClick={() => set({ modal: { kind: "userSettings" } })}>
        <Icon name="settings" />
      </button>
    </div>
  );
}

export { bits };
