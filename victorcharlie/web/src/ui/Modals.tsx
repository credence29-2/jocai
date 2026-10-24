import { useEffect, useState } from "react";
import { api } from "../api";
import { Markdown } from "../markdown";
import { has, outranks, P, topRole } from "../perms";
import { navigate, selectGuild } from "../router";
import { displayName, get, roleColor, set, statusOf, toast, useStore, type Modal } from "../store";
import type { DM, Invite, Message } from "../types";
import { voice } from "../voice";
import { Avatar, closeModal, Field, formatTime, hexColor, idTime, Modal as Shell } from "./common";
import { Icon } from "./Icon";
import { ChannelSettings, CreateChannel, GuildSettings } from "./GuildSettings";
import { UserSettings } from "./UserSettings";

export function Modals() {
  const modal = useStore((s) => s.modal);
  if (!modal) return null;
  return <ModalSwitch m={modal} />;
}

function ModalSwitch({ m }: { m: Modal }) {
  switch (m.kind) {
    case "createGuild":
      return <CreateGuild />;
    case "invite":
      return <InviteModal guildId={m.guildId} />;
    case "guildSettings":
      return <GuildSettings guildId={m.guildId} tab={m.tab} />;
    case "channelSettings":
      return <ChannelSettings channelId={m.channelId} />;
    case "createChannel":
      return <CreateChannel guildId={m.guildId} type={m.type} parentId={m.parentId} />;
    case "userSettings":
      return <UserSettings tab={m.tab} />;
    case "profile":
      return <Profile userId={m.userId} guildId={m.guildId ?? null} />;
    case "newDM":
      return <NewDM />;
    case "image":
      return (
        <div className="modal-backdrop image-viewer" onClick={closeModal}>
          <img src={m.url} alt={m.name} onClick={(e) => e.stopPropagation()} />
          <a className="open-original" href={m.url} target="_blank" rel="noreferrer" onClick={(e) => e.stopPropagation()}>
            Open original
          </a>
        </div>
      );
    case "confirm":
      return <Confirm {...m} />;
    case "screenPicker":
      return <ScreenPicker />;
    case "pins":
      return <Pins channelId={m.channelId} />;
    case "search":
      return <Search guildId={m.guildId} channelId={m.channelId} />;
  }
}

function Confirm({ title, body, action, danger, onConfirm }: { title: string; body: string; action: string; danger?: boolean; onConfirm: () => unknown }) {
  const [busy, setBusy] = useState(false);
  return (
    <Shell
      title={title}
      onClose={closeModal}
      footer={
        <>
          <button className="btn ghost" onClick={closeModal}>
            Cancel
          </button>
          <button
            className={"btn " + (danger ? "danger" : "primary")}
            disabled={busy}
            autoFocus
            onClick={async () => {
              setBusy(true);
              await onConfirm();
              closeModal();
            }}
          >
            {action}
          </button>
        </>
      }
    >
      <p>{body}</p>
    </Shell>
  );
}

function CreateGuild() {
  const me = useStore((s) => s.me)!;
  const [name, setName] = useState(`${me.display_name}'s server`);
  const [link, setLink] = useState("");
  const [err, setErr] = useState("");
  const create = async () => {
    try {
      const r = await api.post<{ id: string }>("/guilds", { name });
      closeModal();
      setTimeout(() => selectGuild(r.id), 200);
    } catch (e: any) {
      setErr(e.message);
    }
  };
  const join = async () => {
    const code = link.trim().split("/").pop() ?? "";
    try {
      const r = await api.post<{ guild_id: string }>(`/invites/${code}`);
      closeModal();
      setTimeout(() => selectGuild(r.guild_id), 200);
    } catch (e: any) {
      setErr(e.status === 404 ? "That invite link is expired or invalid." : e.message);
    }
  };
  return (
    <Shell title="Add a server" onClose={closeModal}>
      <h3>Create your own</h3>
      <Field label="Server name">
        <input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} onKeyDown={(e) => e.key === "Enter" && create()} />
      </Field>
      <button className="btn primary" onClick={create} disabled={!name.trim()}>
        Create
      </button>
      <div className="divider-or">or</div>
      <h3>Join with an invite</h3>
      <Field label="Invite link">
        <input value={link} onChange={(e) => setLink(e.target.value)} placeholder={`${location.origin}/invite/AbC123`} onKeyDown={(e) => e.key === "Enter" && join()} />
      </Field>
      <button className="btn" onClick={join} disabled={!link.trim()}>
        Join server
      </button>
      {err && <div className="error">{err}</div>}
    </Shell>
  );
}

function InviteModal({ guildId }: { guildId: string }) {
  const guild = useStore((s) => s.guilds[guildId]);
  const [inv, setInv] = useState<Invite | null>(null);
  const [maxUses, setMaxUses] = useState(0);
  const [hours, setHours] = useState(168);
  const [copied, setCopied] = useState(false);
  const make = () =>
    api
      .post<Invite>(`/guilds/${guildId}/invites`, { max_uses: maxUses, max_hours: hours })
      .then(setInv)
      .catch((e) => toast(e.message));
  useEffect(() => {
    make();
  }, [maxUses, hours]);
  const url = inv ? `${location.origin}/invite/${inv.code}` : "";
  return (
    <Shell title={`Invite people to ${guild?.name ?? ""}`} onClose={closeModal}>
      <p className="muted">Send this link to anyone you want in. If they don't have an account yet, they'll make one when they open it.</p>
      <div className="copy-row">
        <input readOnly value={url} onFocus={(e) => e.target.select()} />
        <button
          className="btn primary"
          onClick={() => {
            navigator.clipboard?.writeText(url).then(() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1500);
            });
          }}
        >
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <div className="row gap">
        <Field label="Expires after">
          <select value={hours} onChange={(e) => setHours(Number(e.target.value))}>
            <option value={1}>1 hour</option>
            <option value={24}>1 day</option>
            <option value={168}>7 days</option>
            <option value={720}>30 days</option>
            <option value={0}>Never</option>
          </select>
        </Field>
        <Field label="Max uses">
          <select value={maxUses} onChange={(e) => setMaxUses(Number(e.target.value))}>
            <option value={0}>No limit</option>
            <option value={1}>1 use</option>
            <option value={5}>5 uses</option>
            <option value={10}>10 uses</option>
            <option value={25}>25 uses</option>
          </select>
        </Field>
      </div>
    </Shell>
  );
}

function Profile({ userId, guildId }: { userId: string; guildId: string | null }) {
  const user = useStore((s) => s.users[userId]);
  const guild = useStore((s) => (guildId ? s.guilds[guildId] : undefined));
  const me = useStore((s) => s.me)!;
  const vs = useStore((s) => s.voiceStates[userId]);
  const member = guild?.members.find((m) => m.user_id === userId);
  if (!user) return null;
  const canRoles = guild && has(guild.permissions, P.ManageRoles) && (userId === me.id || outranks(guild, me.id, userId));
  const myTop = guild ? topRole(guild, me.id) : 0;
  const roles = guild ? guild.roles.filter((r) => member?.roles.includes(r.id)).sort((a, b) => b.position - a.position) : [];
  const assignable = guild ? guild.roles.filter((r) => r.id !== guild.id && r.position < myTop && !member?.roles.includes(r.id)) : [];
  const setRoles = (ids: string[]) => api.patch(`/guilds/${guildId}/members/${userId}`, { roles: ids }).catch((e) => toast(e.message));
  const message = async () => {
    try {
      const dm = await api.post<DM>("/users/@me/channels", { recipients: [userId] });
      closeModal();
      navigate("@me", dm.id);
    } catch (e: any) {
      toast(e.message);
    }
  };
  const vc = vs?.channel_id ? (vs.guild_id ? get().guilds[vs.guild_id]?.channels.find((c) => c.id === vs.channel_id) : null) : null;
  return (
    <Shell onClose={closeModal} className="profile-modal">
      <div className="profile-banner" style={{ background: hexColor(user.accent) ?? "var(--accent)" }} />
      <div className="profile-head">
        <Avatar user={user} size={88} status={statusOf(userId)} />
        <div className="profile-actions">
          {userId !== me.id && (
            <button className="btn primary" onClick={message}>
              Message
            </button>
          )}
          {userId === me.id && (
            <button className="btn" onClick={() => set({ modal: { kind: "userSettings", tab: "profile" } })}>
              Edit profile
            </button>
          )}
        </div>
      </div>
      <div className="profile-body">
        <h2 style={{ color: roleColor(userId, guildId) }}>{displayName(userId, guildId)}</h2>
        <div className="muted">
          {user.username}
          {member?.nick && ` · ${user.display_name}`}
        </div>
        {user.custom_status && <div className="profile-status">{user.custom_status}</div>}
        {user.about && (
          <div className="profile-section">
            <h4>About me</h4>
            <div className="content">
              <Markdown text={user.about} />
            </div>
          </div>
        )}
        {vc && (
          <div className="profile-section">
            <h4>In voice</h4>
            <div className="row gap">
              <Icon name="speaker" size={16} /> {vc.name}
              {vs?.streaming && <span className="live">LIVE</span>}
            </div>
          </div>
        )}
        {member && (
          <div className="profile-section">
            <h4>Member since</h4>
            <div>{new Date(member.joined_at).toLocaleDateString([], { year: "numeric", month: "long", day: "numeric" })}</div>
          </div>
        )}
        {guild && (roles.length > 0 || canRoles) && (
          <div className="profile-section">
            <h4>Roles</h4>
            <div className="role-chips">
              {roles.map((r) => (
                <span key={r.id} className="role-chip">
                  <span className="role-dot" style={{ background: hexColor(r.color) ?? "#99aab5" }} />
                  {r.name}
                  {canRoles && r.position < myTop && (
                    <button className="chip-x" title="Remove role" onClick={() => setRoles(member!.roles.filter((x) => x !== r.id))}>
                      <Icon name="close" size={12} />
                    </button>
                  )}
                </span>
              ))}
              {canRoles && assignable.length > 0 && (
                <select className="role-add" value="" onChange={(e) => e.target.value && setRoles([...(member?.roles ?? []), e.target.value])}>
                  <option value="">+ Add role</option>
                  {assignable.map((r) => (
                    <option key={r.id} value={r.id}>
                      {r.name}
                    </option>
                  ))}
                </select>
              )}
            </div>
          </div>
        )}
        {guild && member && has(guild.permissions, P.ManageNicknames) && userId !== me.id && outranks(guild, me.id, userId) && (
          <div className="profile-section">
            <h4>Nickname</h4>
            <NickEditor guildId={guild.id} userId={userId} nick={member.nick} />
          </div>
        )}
      </div>
    </Shell>
  );
}

function NickEditor({ guildId, userId, nick }: { guildId: string; userId: string; nick: string }) {
  const [v, setV] = useState(nick);
  return (
    <div className="copy-row">
      <input value={v} maxLength={32} placeholder="No nickname" onChange={(e) => setV(e.target.value)} />
      <button className="btn" disabled={v === nick} onClick={() => api.patch(`/guilds/${guildId}/members/${userId}`, { nick: v }).catch((e) => toast(e.message))}>
        Save
      </button>
    </div>
  );
}

function NewDM() {
  const guilds = useStore((s) => s.guilds);
  const me = useStore((s) => s.me)!;
  const [q, setQ] = useState("");
  const [picked, setPicked] = useState<string[]>([]);
  const people = new Map<string, string>();
  for (const g of Object.values(guilds)) for (const m of g.members) if (m.user_id !== me.id) people.set(m.user_id, displayName(m.user_id));
  const list = [...people.entries()].filter(([, n]) => n.toLowerCase().includes(q.toLowerCase())).sort((a, b) => a[1].localeCompare(b[1]));
  const go = async () => {
    try {
      const dm = await api.post<DM>("/users/@me/channels", { recipients: picked });
      closeModal();
      navigate("@me", dm.id);
    } catch (e: any) {
      toast(e.message);
    }
  };
  const users = get().users;
  return (
    <Shell
      title="New message"
      onClose={closeModal}
      footer={
        <button className="btn primary" disabled={!picked.length} onClick={go}>
          {picked.length > 1 ? "Create group" : "Open DM"}
        </button>
      }
    >
      <p className="muted small">Pick one person for a DM, or up to 9 for a group.</p>
      <input autoFocus placeholder="Find someone" value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="pick-list">
        {list.map(([id, name]) => (
          <label key={id} className="pick-row">
            <Avatar user={users[id]} size={32} status={statusOf(id)} />
            <span className="grow">{name}</span>
            <input type="checkbox" checked={picked.includes(id)} onChange={(e) => setPicked(e.target.checked ? [...picked, id].slice(0, 9) : picked.filter((x) => x !== id))} />
          </label>
        ))}
        {list.length === 0 && <div className="muted">Nobody matches. You can message people you share a server with.</div>}
      </div>
    </Shell>
  );
}

function ScreenPicker() {
  const [sources, setSources] = useState<Awaited<ReturnType<NonNullable<typeof window.victorCharlieDesktop>["getSources"]>>>([]);
  const [tab, setTab] = useState<"window" | "screen">("screen");
  const [audio, setAudio] = useState(true);
  useEffect(() => {
    window.victorCharlieDesktop?.getSources().then(setSources);
  }, []);
  const pick = (id: string) => {
    window.victorCharlieDesktop?.chooseSource(id, audio);
    closeModal();
    voice.startShare();
  };
  const shown = sources.filter((s) => (tab === "screen") === s.display);
  return (
    <Shell title="Share your screen" onClose={() => (window.victorCharlieDesktop?.chooseSource(null, false), closeModal())} wide>
      <div className="tabs">
        <button className={tab === "screen" ? "on" : ""} onClick={() => setTab("screen")}>
          Screens
        </button>
        <button className={tab === "window" ? "on" : ""} onClick={() => setTab("window")}>
          Applications
        </button>
        <label className="row gap small push">
          <input type="checkbox" checked={audio} onChange={(e) => setAudio(e.target.checked)} /> Share sound
        </label>
      </div>
      <div className="source-grid">
        {shown.map((s) => (
          <button key={s.id} className="source" onClick={() => pick(s.id)}>
            <img src={s.thumbnail} alt="" />
            <span className="truncate">
              {s.appIcon && <img src={s.appIcon} alt="" className="app-icon" />}
              {s.name}
            </span>
          </button>
        ))}
        {shown.length === 0 && <div className="muted">Loading…</div>}
      </div>
    </Shell>
  );
}

function MessageResult({ m, onJump }: { m: Message; onJump: () => void }) {
  return (
    <div className="result" onClick={onJump}>
      <Avatar user={m.author} size={32} />
      <div className="grow">
        <div className="row gap">
          <span className="strong">{displayName(m.author.id, m.guild_id)}</span>
          <span className="muted small">{formatTime(m.id)}</span>
        </div>
        <div className="content">
          <Markdown text={m.content} guildId={m.guild_id} />
        </div>
        {m.attachments.length > 0 && <div className="muted small">{m.attachments.length} attachment(s)</div>}
      </div>
    </div>
  );
}

function jumpTo(m: Message) {
  closeModal();
  navigate(m.guild_id ?? "@me", m.channel_id);
  setTimeout(() => {
    const el = document.querySelector(`[data-id="${m.id}"]`);
    el?.scrollIntoView({ block: "center" });
    el?.classList.add("flash");
  }, 400);
}

function Pins({ channelId }: { channelId: string }) {
  const [list, setList] = useState<Message[] | null>(null);
  useEffect(() => {
    api.get<Message[]>(`/channels/${channelId}/pins`).then(setList, (e) => toast(e.message));
  }, [channelId]);
  return (
    <Shell title="Pinned messages" onClose={closeModal}>
      {!list ? <div className="muted">Loading…</div> : list.length === 0 ? <div className="muted">Nothing's pinned here yet. Right-click a message to pin it.</div> : list.map((m) => <MessageResult key={m.id} m={m} onJump={() => jumpTo(m)} />)}
    </Shell>
  );
}

function Search({ guildId, channelId }: { guildId: string | null; channelId?: string }) {
  const [q, setQ] = useState("");
  const [list, setList] = useState<Message[] | null>(null);
  const run = async () => {
    if (!q.trim()) return;
    const params = new URLSearchParams({ q });
    if (channelId) params.set("channel_id", channelId);
    else if (guildId) params.set("guild_id", guildId);
    try {
      setList(await api.get<Message[]>(`/search?${params}`));
    } catch (e: any) {
      toast(e.message);
    }
  };
  return (
    <Shell title="Search messages" onClose={closeModal}>
      <div className="copy-row">
        <input autoFocus placeholder="Words to find" value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === "Enter" && run()} />
        <button className="btn primary" onClick={run}>
          Search
        </button>
      </div>
      <div className="results">
        {list && list.length === 0 && <div className="muted">No results.</div>}
        {list?.map((m) => (
          <MessageResult key={m.id} m={m} onJump={() => jumpTo(m)} />
        ))}
      </div>
    </Shell>
  );
}

export { idTime };
