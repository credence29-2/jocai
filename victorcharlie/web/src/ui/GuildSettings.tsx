import { useEffect, useState } from "react";
import { api, upload } from "../api";
import { bits, has, outranks, P, PERM_INFO, topRole } from "../perms";
import { navigate } from "../router";
import { channelById, displayName, get, set, toast, useStore } from "../store";
import type { Channel, Guild, Invite, Overwrite, Role, User } from "../types";
import { Avatar, closeModal, Field, GuildIcon, hexColor, Toggle } from "./common";
import { Icon } from "./Icon";

function SettingsShell({ title, tabs, tab, setTab, children, danger }: { title: string; tabs: { id: string; label: string }[]; tab: string; setTab: (t: string) => void; children: React.ReactNode; danger?: { label: string; onClick: () => void } }) {
  useEffect(() => {
    const k = (e: KeyboardEvent) => e.key === "Escape" && closeModal();
    window.addEventListener("keydown", k);
    return () => window.removeEventListener("keydown", k);
  }, []);
  return (
    <div className="settings">
      <nav className="settings-nav">
        <div className="settings-nav-title truncate">{title}</div>
        {tabs.map((t) => (
          <button key={t.id} className={tab === t.id ? "on" : ""} onClick={() => setTab(t.id)}>
            {t.label}
          </button>
        ))}
        {danger && (
          <>
            <div className="menu-divider" />
            <button className="danger" onClick={danger.onClick}>
              {danger.label}
            </button>
          </>
        )}
      </nav>
      <section className="settings-body">
        <button className="settings-close" onClick={closeModal} title="Close (Esc)">
          <Icon name="close" />
        </button>
        <select className="settings-tabs-mobile" value={tab} onChange={(e) => setTab(e.target.value)}>
          {tabs.map((t) => (
            <option key={t.id} value={t.id}>
              {t.label}
            </option>
          ))}
        </select>
        <div className="settings-content">{children}</div>
      </section>
    </div>
  );
}

export { SettingsShell };

export function GuildSettings({ guildId, tab: initial }: { guildId: string; tab?: string }) {
  const g = useStore((s) => s.guilds[guildId]);
  const me = useStore((s) => s.me)!;
  const [tab, setTab] = useState(initial ?? "overview");
  if (!g) return null;
  const p = g.permissions;
  const tabs = [
    ...(has(p, P.ManageServer) ? [{ id: "overview", label: "Overview" }] : []),
    ...(has(p, P.ManageRoles) ? [{ id: "roles", label: "Roles" }] : []),
    { id: "members", label: "Members" },
    ...(has(p, P.ManageServer) ? [{ id: "invites", label: "Invites" }] : []),
    ...(has(p, P.BanMembers) ? [{ id: "bans", label: "Bans" }] : []),
  ];
  const cur = tabs.some((t) => t.id === tab) ? tab : tabs[0].id;
  return (
    <SettingsShell
      title={g.name}
      tabs={tabs}
      tab={cur}
      setTab={setTab}
      danger={
        g.owner_id === me.id
          ? {
              label: "Delete server",
              onClick: () =>
                set({
                  modal: {
                    kind: "confirm",
                    title: `Delete ${g.name}`,
                    body: "This deletes the server, every channel and every message in it, for everyone. It can't be undone.",
                    action: "Delete server",
                    danger: true,
                    onConfirm: () => api.del(`/guilds/${g.id}`).catch((e) => toast(e.message)),
                  },
                }),
            }
          : undefined
      }
    >
      {cur === "overview" && <Overview g={g} />}
      {cur === "roles" && <Roles g={g} />}
      {cur === "members" && <MembersTab g={g} />}
      {cur === "invites" && <Invites g={g} />}
      {cur === "bans" && <Bans g={g} />}
    </SettingsShell>
  );
}

function Overview({ g }: { g: Guild }) {
  const me = useStore((s) => s.me)!;
  const [name, setName] = useState(g.name);
  const [sys, setSys] = useState(g.system_channel_id ?? "0");
  const save = () => api.patch(`/guilds/${g.id}`, { name, system_channel_id: sys || "0" }).then(() => toast("Saved."), (e) => toast(e.message));
  const icon = (f: File) => {
    const form = new FormData();
    form.append("file", f);
    upload(`/guilds/${g.id}/icon`, form).catch((e) => toast(e.message));
  };
  const transfer = (uid: string) =>
    set({
      modal: {
        kind: "confirm",
        title: "Transfer ownership",
        body: `Make ${displayName(uid, g.id)} the owner of ${g.name}? You'll lose owner powers.`,
        action: "Transfer",
        danger: true,
        onConfirm: () => api.patch(`/guilds/${g.id}`, { owner_id: uid }).catch((e) => toast(e.message)),
      },
    });
  return (
    <>
      <h2>Server overview</h2>
      <div className="row gap-lg">
        <label className="icon-upload" title="Change icon">
          <GuildIcon name={g.name} icon={g.icon} size={96} />
          <span>Change</span>
          <input type="file" accept="image/*" hidden onChange={(e) => e.target.files?.[0] && icon(e.target.files[0])} />
        </label>
        <div className="grow">
          <Field label="Server name">
            <input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />
          </Field>
          <Field label="Welcome messages go to" hint="When someone joins, a welcome note is posted here.">
            <select value={sys} onChange={(e) => setSys(e.target.value)}>
              <option value="0">Nowhere</option>
              {g.channels
                .filter((c) => c.type === "text")
                .map((c) => (
                  <option key={c.id} value={c.id}>
                    #{c.name}
                  </option>
                ))}
            </select>
          </Field>
        </div>
      </div>
      <button className="btn primary" onClick={save}>
        Save changes
      </button>
      {g.owner_id === me.id && (
        <>
          <h3 className="mt">Ownership</h3>
          <Field label="Transfer this server to">
            <select value="" onChange={(e) => e.target.value && transfer(e.target.value)}>
              <option value="">Pick a member…</option>
              {g.members
                .filter((m) => m.user_id !== me.id)
                .map((m) => (
                  <option key={m.user_id} value={m.user_id}>
                    {displayName(m.user_id, g.id)}
                  </option>
                ))}
            </select>
          </Field>
        </>
      )}
    </>
  );
}

const roleColors = [0, 0x1abc9c, 0x2ecc71, 0x3498db, 0x9b59b6, 0xe91e63, 0xf1c40f, 0xe67e22, 0xe74c3c, 0x95a5a6, 0x607d8b, 0x3d7f6e];

function Roles({ g }: { g: Guild }) {
  const me = useStore((s) => s.me)!;
  const roles = [...g.roles].sort((a, b) => b.position - a.position);
  const [sel, setSel] = useState<string>(roles[0]?.id ?? g.id);
  const role = g.roles.find((r) => r.id === sel) ?? g.roles.find((r) => r.id === g.id)!;
  const myTop = topRole(g, me.id);
  const editable = role.id === g.id || role.position < myTop;
  const create = async () => {
    try {
      const r = await api.post<Role>(`/guilds/${g.id}/roles`, { name: "new role", permissions: 0 });
      setSel(r.id);
    } catch (e: any) {
      toast(e.message);
    }
  };
  const moveRole = (dir: number) => {
    const order = roles.filter((r) => r.id !== g.id).map((r) => r.id);
    const i = order.indexOf(role.id);
    const j = i + dir;
    if (i < 0 || j < 0 || j >= order.length) return;
    [order[i], order[j]] = [order[j], order[i]];
    api.patch(`/guilds/${g.id}/roles`, order).catch((e) => toast(e.message));
  };
  return (
    <>
      <h2>Roles</h2>
      <p className="muted">Roles group people and decide what they can do. Higher roles can manage lower ones. Use roles to keep channels private, too: set that per channel under Edit channel, Permissions.</p>
      <div className="roles-layout">
        <div className="role-list">
          <button className="btn small block" onClick={create}>
            <Icon name="plus" size={14} /> Create role
          </button>
          {roles.map((r) => (
            <button key={r.id} className={"role-row" + (r.id === sel ? " on" : "")} onClick={() => setSel(r.id)}>
              <span className="role-dot" style={{ background: hexColor(r.color) ?? "#99aab5" }} />
              <span className="truncate">{r.name}</span>
              <span className="muted small">{r.id === g.id ? "" : g.members.filter((m) => m.roles.includes(r.id)).length}</span>
            </button>
          ))}
        </div>
        <RoleEditor key={role.id} g={g} role={role} editable={editable} move={moveRole} />
      </div>
    </>
  );
}

function RoleEditor({ g, role, editable, move }: { g: Guild; role: Role; editable: boolean; move: (d: number) => void }) {
  const [name, setName] = useState(role.name);
  const isEveryone = role.id === g.id;
  const patch = (body: Partial<Role>) => api.patch(`/guilds/${g.id}/roles/${role.id}`, body).catch((e) => toast(e.message));
  const togglePerm = (bit: number, on: boolean) => patch({ permissions: on ? bits.or(role.permissions, bit) : bits.andNot(role.permissions, bit) });
  const groups = [...new Set(PERM_INFO.map((p) => p.group))];
  const members = g.members.filter((m) => m.roles.includes(role.id));
  return (
    <div className="role-editor">
      {!editable && <div className="notice">This role is at or above your highest role, so you can't change it.</div>}
      <fieldset disabled={!editable}>
        <Field label="Role name">
          <input value={name} disabled={isEveryone} onChange={(e) => setName(e.target.value)} onBlur={() => name !== role.name && patch({ name })} maxLength={100} />
        </Field>
        {!isEveryone && (
          <>
            <Field label="Color">
              <div className="swatches">
                {roleColors.map((c) => (
                  <button key={c} className={"swatch" + (role.color === c ? " on" : "")} style={{ background: hexColor(c) ?? "#99aab5" }} onClick={() => patch({ color: c })} title={c ? hexColor(c) : "Default"} />
                ))}
                <input type="color" value={hexColor(role.color) ?? "#99aab5"} onChange={(e) => patch({ color: parseInt(e.target.value.slice(1), 16) })} />
              </div>
            </Field>
            <div className="toggle-row">
              <div>
                <div className="strong">Show separately in the member list</div>
              </div>
              <Toggle on={role.hoist} onChange={(v) => patch({ hoist: v })} />
            </div>
            <div className="toggle-row">
              <div>
                <div className="strong">Anyone can @mention this role</div>
              </div>
              <Toggle on={role.mentionable} onChange={(v) => patch({ mentionable: v })} />
            </div>
            <div className="row gap">
              <button className="btn small" onClick={() => move(-1)}>
                Move up
              </button>
              <button className="btn small" onClick={() => move(1)}>
                Move down
              </button>
              <button
                className="btn small danger push"
                onClick={() =>
                  set({
                    modal: {
                      kind: "confirm",
                      title: `Delete ${role.name}`,
                      body: "Everyone with this role loses it.",
                      action: "Delete role",
                      danger: true,
                      onConfirm: () => api.del(`/guilds/${g.id}/roles/${role.id}`).catch((e) => toast(e.message)).then(() => set({ modal: { kind: "guildSettings", guildId: g.id, tab: "roles" } })),
                    },
                  })
                }
              >
                Delete role
              </button>
            </div>
          </>
        )}
        {isEveryone && <p className="muted">@everyone is every member of the server. Its permissions are the starting point for everybody.</p>}
        {groups.map((grp) => (
          <div key={grp} className="perm-group">
            <h4>{grp}</h4>
            {PERM_INFO.filter((p) => p.group === grp).map((p) => (
              <div key={p.key} className="toggle-row">
                <div>
                  <div className="strong">{p.label}</div>
                  <div className="muted small">{p.desc}</div>
                </div>
                <Toggle on={has(role.permissions, P[p.key])} onChange={(v) => togglePerm(P[p.key], v)} disabled={!has(g.permissions, P[p.key])} />
              </div>
            ))}
          </div>
        ))}
        {!isEveryone && (
          <div className="perm-group">
            <h4>Members with this role ({members.length})</h4>
            <AssignRole g={g} role={role} />
          </div>
        )}
      </fieldset>
    </div>
  );
}

function AssignRole({ g, role }: { g: Guild; role: Role }) {
  const me = useStore((s) => s.me)!;
  const has_ = g.members.filter((m) => m.roles.includes(role.id));
  const without = g.members.filter((m) => !m.roles.includes(role.id) && (m.user_id === me.id || outranks(g, me.id, m.user_id)));
  const setRoles = (uid: string, roles: string[]) => api.patch(`/guilds/${g.id}/members/${uid}`, { roles }).catch((e) => toast(e.message));
  return (
    <>
      <select value="" onChange={(e) => {
        const m = g.members.find((x) => x.user_id === e.target.value);
        if (m) setRoles(m.user_id, [...m.roles, role.id]);
      }}>
        <option value="">+ Add someone to this role</option>
        {without.map((m) => (
          <option key={m.user_id} value={m.user_id}>
            {displayName(m.user_id, g.id)}
          </option>
        ))}
      </select>
      {has_.map((m) => (
        <div key={m.user_id} className="person-row">
          <Avatar user={m.user} size={28} />
          <span className="grow">{displayName(m.user_id, g.id)}</span>
          <button className="icon-btn tiny" title="Remove from role" onClick={() => setRoles(m.user_id, m.roles.filter((r) => r !== role.id))}>
            <Icon name="close" size={16} />
          </button>
        </div>
      ))}
    </>
  );
}

function MembersTab({ g }: { g: Guild }) {
  const me = useStore((s) => s.me)!;
  const [q, setQ] = useState("");
  const list = g.members.filter((m) => displayName(m.user_id, g.id).toLowerCase().includes(q.toLowerCase()) || m.user.username.toLowerCase().includes(q.toLowerCase()));
  return (
    <>
      <h2>Members ({g.members.length})</h2>
      <input placeholder="Search members" value={q} onChange={(e) => setQ(e.target.value)} />
      <div className="member-table">
        {list.map((m) => (
          <div key={m.user_id} className="person-row">
            <Avatar user={m.user} size={36} />
            <div className="grow">
              <div className="strong">
                {displayName(m.user_id, g.id)} {g.owner_id === m.user_id && <Icon name="crown" size={14} className="crown" />}
              </div>
              <div className="muted small">
                {m.user.username} · joined {new Date(m.joined_at).toLocaleDateString()}
              </div>
              <div className="role-chips small">
                {g.roles
                  .filter((r) => m.roles.includes(r.id))
                  .sort((a, b) => b.position - a.position)
                  .map((r) => (
                    <span key={r.id} className="role-chip">
                      <span className="role-dot" style={{ background: hexColor(r.color) ?? "#99aab5" }} />
                      {r.name}
                    </span>
                  ))}
              </div>
            </div>
            <button className="btn small" onClick={() => set({ modal: { kind: "profile", userId: m.user_id, guildId: g.id } })}>
              Manage
            </button>
            {m.user_id !== me.id && outranks(g, me.id, m.user_id) && has(g.permissions, P.KickMembers) && (
              <button className="btn small danger" onClick={() => api.del(`/guilds/${g.id}/members/${m.user_id}`).catch((e) => toast(e.message))}>
                Kick
              </button>
            )}
            {m.user_id !== me.id && outranks(g, me.id, m.user_id) && has(g.permissions, P.BanMembers) && (
              <button className="btn small danger" onClick={() => api.put(`/guilds/${g.id}/bans/${m.user_id}`, {}).catch((e) => toast(e.message))}>
                Ban
              </button>
            )}
          </div>
        ))}
      </div>
    </>
  );
}

function Invites({ g }: { g: Guild }) {
  const [list, setList] = useState<Invite[] | null>(null);
  const load = () => api.get<Invite[]>(`/guilds/${g.id}/invites`).then(setList, (e) => toast(e.message));
  useEffect(() => {
    load();
  }, []);
  return (
    <>
      <h2>Invites</h2>
      <p className="muted">Active invite links. Revoke one and it stops working right away.</p>
      <button className="btn primary" onClick={() => set({ modal: { kind: "invite", guildId: g.id } })}>
        Create invite
      </button>
      <div className="member-table">
        {list?.length === 0 && <div className="muted">No active invites.</div>}
        {list?.map((inv) => (
          <div key={inv.code} className="person-row">
            <code className="grow">{inv.code}</code>
            <span className="muted small">by {displayName(inv.creator_id)}</span>
            <span className="muted small">
              {inv.uses}
              {inv.max_uses ? `/${inv.max_uses}` : ""} uses
            </span>
            <span className="muted small">{inv.expires_at ? "until " + new Date(inv.expires_at).toLocaleString() : "never expires"}</span>
            <button className="btn small" onClick={() => navigator.clipboard?.writeText(`${location.origin}/invite/${inv.code}`)}>
              Copy
            </button>
            <button className="btn small danger" onClick={() => api.del(`/guilds/${g.id}/invites/${inv.code}`).then(load, (e) => toast(e.message))}>
              Revoke
            </button>
          </div>
        ))}
      </div>
    </>
  );
}

function Bans({ g }: { g: Guild }) {
  const [list, setList] = useState<{ user: User; reason: string }[] | null>(null);
  const load = () => api.get<{ user: User; reason: string }[]>(`/guilds/${g.id}/bans`).then(setList, (e) => toast(e.message));
  useEffect(() => {
    load();
  }, []);
  return (
    <>
      <h2>Bans</h2>
      <div className="member-table">
        {list?.length === 0 && <div className="muted">Nobody is banned.</div>}
        {list?.map((b) => (
          <div key={b.user.id} className="person-row">
            <Avatar user={b.user} size={32} />
            <span className="grow">{b.user.display_name}</span>
            <button className="btn small" onClick={() => api.del(`/guilds/${g.id}/bans/${b.user.id}`).then(load, (e) => toast(e.message))}>
              Unban
            </button>
          </div>
        ))}
      </div>
    </>
  );
}

// --- channels ---

export function CreateChannel({ guildId, type: initial, parentId }: { guildId: string; type: "text" | "voice" | "category"; parentId?: string | null }) {
  const g = useStore((s) => s.guilds[guildId]);
  const [type, setType] = useState(initial);
  const [name, setName] = useState("");
  const [priv, setPriv] = useState(false);
  // Default to the category that already holds this kind of channel.
  const [parent, setParent] = useState(() => parentId ?? g?.channels.find((c) => c.type === initial && c.parent_id)?.parent_id ?? "");
  if (!g) return null;
  const create = async () => {
    const overwrites: Overwrite[] | undefined = priv ? [{ id: g.id, allow: 0, deny: P.ViewChannel }] : undefined;
    try {
      const c = await api.post<Channel>(`/guilds/${guildId}/channels`, { type, name, parent_id: type === "category" ? null : parent || null, overwrites });
      closeModal();
      if (priv) set({ modal: { kind: "channelSettings", channelId: c.id } });
      else if (c.type === "text") {
        setTimeout(() => navigate(guildId, c.id), 150);
      }
    } catch (e: any) {
      toast(e.message);
    }
  };
  return (
    <div className="modal-backdrop" onMouseDown={(e) => e.target === e.currentTarget && closeModal()}>
      <div className="modal">
        <div className="modal-head">
          <h2>{type === "category" ? "Create category" : "Create channel"}</h2>
          <button className="icon-btn" onClick={closeModal}>
            <Icon name="close" />
          </button>
        </div>
        <div className="modal-body">
          {type !== "category" && (
            <div className="type-pick">
              <label className={type === "text" ? "on" : ""}>
                <input type="radio" checked={type === "text"} onChange={() => setType("text")} />
                <Icon name="hash" />
                <div>
                  <div className="strong">Text</div>
                  <div className="muted small">Messages, images, files</div>
                </div>
              </label>
              <label className={type === "voice" ? "on" : ""}>
                <input type="radio" checked={type === "voice"} onChange={() => setType("voice")} />
                <Icon name="speaker" />
                <div>
                  <div className="strong">Voice</div>
                  <div className="muted small">Talk, video and screen share</div>
                </div>
              </label>
            </div>
          )}
          <Field label="Name">
            <input autoFocus value={name} onChange={(e) => setName(e.target.value)} maxLength={100} placeholder={type === "text" ? "new-channel" : type === "voice" ? "Hangout" : "Category"} onKeyDown={(e) => e.key === "Enter" && name.trim() && create()} />
          </Field>
          {type !== "category" && (
            <Field label="Category">
              <select value={parent} onChange={(e) => setParent(e.target.value)}>
                <option value="">None</option>
                {g.channels
                  .filter((c) => c.type === "category")
                  .map((c) => (
                    <option key={c.id} value={c.id}>
                      {c.name}
                    </option>
                  ))}
              </select>
            </Field>
          )}
          <div className="toggle-row">
            <div>
              <div className="strong">Private</div>
              <div className="muted small">Only roles you pick can see it. You'll choose them next.</div>
            </div>
            <Toggle on={priv} onChange={setPriv} />
          </div>
        </div>
        <div className="modal-foot">
          <button className="btn ghost" onClick={closeModal}>
            Cancel
          </button>
          <button className="btn primary" onClick={create} disabled={!name.trim()}>
            Create
          </button>
        </div>
      </div>
    </div>
  );
}

export function ChannelSettings({ channelId }: { channelId: string }) {
  useStore((s) => s.guilds);
  const c = channelById(channelId);
  const [tab, setTab] = useState("overview");
  if (!c || !c.guild_id) return null;
  const g = get().guilds[c.guild_id];
  if (!g) return null;
  return (
    <SettingsShell
      title={c.type === "category" ? c.name : (c.type === "text" ? "#" : "") + c.name}
      tabs={[
        { id: "overview", label: "Overview" },
        ...(has(g.permissions, P.ManageRoles) ? [{ id: "perms", label: "Permissions" }] : []),
      ]}
      tab={tab}
      setTab={setTab}
      danger={{
        label: c.type === "category" ? "Delete category" : "Delete channel",
        onClick: () =>
          set({
            modal: { kind: "confirm", title: "Delete " + c.name, body: "This can't be undone.", action: "Delete", danger: true, onConfirm: () => api.del(`/channels/${c.id}`).catch((e) => toast(e.message)) },
          }),
      }}
    >
      {tab === "overview" ? <ChannelOverview c={c} /> : <ChannelPerms c={c} g={g} />}
    </SettingsShell>
  );
}

function ChannelOverview({ c }: { c: Channel }) {
  const [name, setName] = useState(c.name);
  const [topic, setTopic] = useState(c.topic);
  const [bitrate, setBitrate] = useState(c.bitrate || 96);
  const [limit, setLimit] = useState(c.user_limit);
  const [slow, setSlow] = useState(c.slowmode);
  const save = () =>
    api
      .patch(`/channels/${c.id}`, { name, topic, ...(c.type === "voice" ? { bitrate, user_limit: limit } : {}), ...(c.type === "text" ? { slowmode: slow } : {}) })
      .then(() => toast("Saved."), (e) => toast(e.message));
  return (
    <>
      <h2>Overview</h2>
      <Field label="Name">
        <input value={name} onChange={(e) => setName(e.target.value)} maxLength={100} />
      </Field>
      {c.type === "text" && (
        <>
          <Field label="Topic">
            <textarea value={topic} onChange={(e) => setTopic(e.target.value)} maxLength={1024} rows={3} />
          </Field>
          <Field label={`Slow mode: ${slow ? slow + "s between messages" : "off"}`}>
            <select value={slow} onChange={(e) => setSlow(Number(e.target.value))}>
              {[0, 5, 10, 30, 60, 300, 900, 3600].map((s) => (
                <option key={s} value={s}>
                  {s === 0 ? "Off" : s < 60 ? `${s} seconds` : `${s / 60} minutes`}
                </option>
              ))}
            </select>
          </Field>
        </>
      )}
      {c.type === "voice" && (
        <>
          <Field label={`Voice quality: ${bitrate} kbps`} hint="Higher is clearer but uses more upload on everyone's connection and on the server. 96 is great for voice; 128 to 256 helps music.">
            <input type="range" min={32} max={384} step={8} value={bitrate} onChange={(e) => setBitrate(Number(e.target.value))} />
          </Field>
          <Field label={`User limit: ${limit || "none"}`}>
            <input type="range" min={0} max={50} value={limit} onChange={(e) => setLimit(Number(e.target.value))} />
          </Field>
        </>
      )}
      <button className="btn primary" onClick={save}>
        Save changes
      </button>
    </>
  );
}

function ChannelPerms({ c, g }: { c: Channel; g: Guild }) {
  const roles = [...g.roles].sort((a, b) => b.position - a.position);
  const [sel, setSel] = useState(g.id);
  const ow = c.overwrites.find((o) => o.id === sel) ?? { id: sel, allow: 0, deny: 0 };
  const everyone = c.overwrites.find((o) => o.id === g.id);
  const isPrivate = !!everyone && has(everyone.deny, P.ViewChannel);
  const saveOverwrites = (list: Overwrite[]) => api.patch(`/channels/${c.id}`, { overwrites: list.filter((o) => o.allow || o.deny) }).catch((e) => toast(e.message));
  const setOne = (next: Overwrite) => saveOverwrites([...c.overwrites.filter((o) => o.id !== next.id), next]);
  const setState = (bit: number, state: "allow" | "neutral" | "deny") => {
    let { allow, deny } = ow;
    allow = bits.andNot(allow, bit);
    deny = bits.andNot(deny, bit);
    if (state === "allow") allow = bits.or(allow, bit);
    if (state === "deny") deny = bits.or(deny, bit);
    setOne({ id: sel, allow, deny });
  };
  const setPrivate = (on: boolean) => {
    const e = everyone ?? { id: g.id, allow: 0, deny: 0 };
    setOne({ id: g.id, allow: bits.andNot(e.allow, P.ViewChannel), deny: on ? bits.or(e.deny, P.ViewChannel) : bits.andNot(e.deny, P.ViewChannel) });
  };
  const relevant = PERM_INFO.filter((p) => p.channel && (p.channel === "both" || p.channel === (c.type === "category" ? p.channel : c.type)));
  return (
    <>
      <h2>Permissions</h2>
      <div className="toggle-row">
        <div>
          <div className="strong">Private {c.type === "category" ? "category" : "channel"}</div>
          <div className="muted small">Hide it from everyone except the roles you allow below (and admins).</div>
        </div>
        <Toggle on={isPrivate} onChange={setPrivate} />
      </div>
      <div className="roles-layout">
        <div className="role-list">
          {roles.map((r) => {
            const o = c.overwrites.find((x) => x.id === r.id);
            return (
              <button key={r.id} className={"role-row" + (r.id === sel ? " on" : "")} onClick={() => setSel(r.id)}>
                <span className="role-dot" style={{ background: hexColor(r.color) ?? "#99aab5" }} />
                <span className="truncate">{r.name}</span>
                {o && (o.allow || o.deny) ? <span className="muted small">•</span> : null}
              </button>
            );
          })}
        </div>
        <div className="role-editor">
          <p className="muted small">Green allows, red denies, the middle leaves it to the role's server-wide setting.</p>
          {relevant.map((p) => {
            const bit = P[p.key];
            const state = has(ow.allow, bit) ? "allow" : has(ow.deny, bit) ? "deny" : "neutral";
            return (
              <div key={p.key} className="toggle-row">
                <div>
                  <div className="strong">{p.label}</div>
                  <div className="muted small">{p.desc}</div>
                </div>
                <div className="tristate">
                  <button className={"deny" + (state === "deny" ? " on" : "")} onClick={() => setState(bit, "deny")} title="Deny">
                    <Icon name="close" size={14} />
                  </button>
                  <button className={"neutral" + (state === "neutral" ? " on" : "")} onClick={() => setState(bit, "neutral")} title="Default">
                    /
                  </button>
                  <button className={"allow" + (state === "allow" ? " on" : "")} onClick={() => setState(bit, "allow")} title="Allow">
                    <Icon name="check" size={14} />
                  </button>
                </div>
              </div>
            );
          })}
        </div>
      </div>
    </>
  );
}
