import { memo, useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api, upload } from "../api";
import { allEmoji, emojiCategories, quickReactions, recentEmoji, rememberEmoji, searchEmoji } from "../emoji";
import { Markdown } from "../markdown";
import { channelPerms, has, P } from "../perms";
import { addMessage, displayName, get, roleColor, set, toast, updateMessage, useStore, type ChannelMessages } from "../store";
import type { Attachment, Channel, Guild, ID, Message } from "../types";
import { Avatar, formatBytes, formatTime, idTime, openMenu, useNow } from "./common";
import { Icon } from "./Icon";
import { userMenu } from "./Sidebar";

const empty: ChannelMessages = { list: [], hasMore: true, loading: false, loaded: false };

async function loadMessages(cid: ID, before?: ID) {
  const cur = get().messages[cid] ?? empty;
  if (cur.loading) return;
  set((s) => ({ messages: { ...s.messages, [cid]: { ...cur, loading: true } } }));
  try {
    const q = before ? `?before=${before}&limit=50` : "?limit=50";
    const list = await api.get<Message[]>(`/channels/${cid}/messages${q}`);
    set((s) => {
      const now = s.messages[cid] ?? empty;
      const merged = before ? [...list, ...now.list] : [...list, ...now.list.filter((m) => m.pending && !list.some((x) => x.nonce === m.nonce))];
      return { messages: { ...s.messages, [cid]: { list: merged, hasMore: list.length === 50, loading: false, loaded: true } } };
    });
  } catch (e: any) {
    set((s) => ({ messages: { ...s.messages, [cid]: { ...(s.messages[cid] ?? empty), loading: false, loaded: true, hasMore: false } } }));
    if (e.status !== 404) toast(e.message);
  }
}

function ack(cid: ID, mid: ID) {
  const s = get();
  const rs = s.readStates[cid];
  if (rs && rs.last_read_id === mid && !rs.mention_count) return;
  set({ readStates: { ...s.readStates, [cid]: { channel_id: cid, last_read_id: mid, mention_count: 0 } } });
  api.post(`/channels/${cid}/ack`, { message_id: mid }).catch(() => {});
}

export function Chat({ channel, guild }: { channel: Channel; guild: Guild | null }) {
  const me = useStore((s) => s.me)!;
  const cm = useStore((s) => s.messages[channel.id]) ?? empty;
  const perms = guild ? channelPerms(guild, me.id, channel) : P.SendMessages | P.AttachFiles | P.AddReactions | P.ReadHistory | P.ManageMessages;
  const scroller = useRef<HTMLDivElement>(null);
  const atBottom = useRef(true);
  const prevHeight = useRef(0);
  const firstId = useRef<ID | undefined>(undefined);
  // Where the "new messages" line goes, fixed when the channel opens.
  const [newSince] = useState(() => get().readStates[channel.id]?.last_read_id ?? null);

  useEffect(() => {
    if (!cm.loaded && !cm.loading) loadMessages(channel.id);
  }, [channel.id, cm.loaded]);

  // Keep the view pinned to the bottom, or steady when older messages load.
  useLayoutEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const first = cm.list[0]?.id;
    if (firstId.current && first !== firstId.current && prevHeight.current) {
      el.scrollTop += el.scrollHeight - prevHeight.current;
    } else if (atBottom.current) {
      el.scrollTop = el.scrollHeight;
    }
    firstId.current = first;
    prevHeight.current = el.scrollHeight;
  }, [cm.list]);

  // Images load after layout; stay at the bottom while they do.
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const ro = new ResizeObserver(() => {
      if (atBottom.current) el.scrollTop = el.scrollHeight;
      prevHeight.current = el.scrollHeight;
    });
    ro.observe(el.firstElementChild!);
    return () => ro.disconnect();
  }, []);

  const last = cm.list.filter((m) => !m.pending).at(-1);
  useEffect(() => {
    const tryAck = () => {
      if (last && atBottom.current && document.hasFocus()) ack(channel.id, last.id);
    };
    tryAck();
    window.addEventListener("focus", tryAck);
    return () => window.removeEventListener("focus", tryAck);
  }, [last?.id, channel.id]);

  const onScroll = () => {
    const el = scroller.current!;
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    if (atBottom.current && last && document.hasFocus()) ack(channel.id, last.id);
    if (el.scrollTop < 300 && cm.hasMore && !cm.loading && cm.loaded && cm.list.length) loadMessages(channel.id, cm.list[0].id);
  };

  const [showJump, setShowJump] = useState(false);
  useEffect(() => {
    const t = setInterval(() => setShowJump(!atBottom.current), 500);
    return () => clearInterval(t);
  }, []);

  const name = channel.type === "dm" ? displayName(get().dms[channel.id]?.users.find((u) => u.id !== me.id)?.id ?? me.id) : channel.name;

  return (
    <div className="chat">
      <div className="messages" ref={scroller} onScroll={onScroll}>
        <div className="messages-inner">
          {!cm.hasMore && cm.loaded && (
            <div className="chat-start">
              <div className="chat-start-icon">
                <Icon name={channel.type === "dm" ? "at" : "hash"} size={36} />
              </div>
              <h2>{channel.type === "dm" ? name : `Welcome to #${channel.name}`}</h2>
              <p className="muted">{channel.type === "dm" ? `This is the start of your conversation with ${name}.` : channel.topic || `This is the start of the #${channel.name} channel.`}</p>
            </div>
          )}
          {cm.loading && <div className="loading-more">Loading messages…</div>}
          <MessageList list={cm.list} guild={guild} perms={perms} newSince={newSince} meId={me.id} />
        </div>
      </div>
      {showJump && (
        <button
          className="jump-bottom"
          onClick={() => {
            const el = scroller.current!;
            el.scrollTop = el.scrollHeight;
            atBottom.current = true;
          }}
        >
          Jump to present
        </button>
      )}
      <Typing channelId={channel.id} guildId={guild?.id ?? null} />
      {has(perms, P.SendMessages) ? (
        <Composer channel={channel} guild={guild} perms={perms} placeholder={channel.type === "dm" ? `Message @${name}` : `Message #${channel.name}`} onSent={() => (atBottom.current = true)} />
      ) : (
        <div className="composer disabled">You don't have permission to send messages here.</div>
      )}
    </div>
  );
}

function sameGroup(a: Message | undefined, b: Message) {
  if (!a || a.type !== "default" || b.type !== "default" || b.reply_to) return false;
  return a.author.id === b.author.id && idTime(b.id) - idTime(a.id) < 7 * 60 * 1000;
}

const MessageList = memo(function MessageList({ list, guild, perms, newSince, meId }: { list: Message[]; guild: Guild | null; perms: number; newSince: ID | null; meId: ID }) {
  let lastDay = "";
  let newShown = false;
  return (
    <>
      {list.map((m, i) => {
        const day = new Date(idTime(m.pending ? String(BigInt(Date.now() - 1767225600000) << 22n) : m.id)).toDateString();
        const showDay = day !== lastDay;
        lastDay = day;
        const isNew = !newShown && newSince && !m.pending && m.author.id !== meId && BigInt(m.id) > BigInt(newSince);
        if (isNew) newShown = true;
        return (
          <div key={m.nonce ?? m.id}>
            {showDay && (
              <div className="day-divider">
                <span>{new Date(day).toLocaleDateString([], { weekday: "long", month: "long", day: "numeric", year: "numeric" })}</span>
              </div>
            )}
            {isNew && (
              <div className="new-divider">
                <span>NEW</span>
              </div>
            )}
            <MessageItem m={m} grouped={!showDay && !isNew && sameGroup(list[i - 1], m)} guild={guild} perms={perms} />
          </div>
        );
      })}
    </>
  );
});

const MessageItem = memo(function MessageItem({ m, grouped, guild, perms }: { m: Message; grouped: boolean; guild: Guild | null; perms: number }) {
  const me = useStore((s) => s.me)!;
  const editing = useStore((s) => s.editing === m.id);
  const gid = guild?.id ?? null;
  const mine = m.author.id === me.id;
  const canManage = has(perms, P.ManageMessages);
  const mentioned = m.mentions.includes(me.id) || m.mention_everyone || (guild && m.mention_roles.some((r) => guild.members.find((x) => x.user_id === me.id)?.roles.includes(r)));
  const color = roleColor(m.author.id, gid);

  if (m.type === "join") {
    return (
      <div className="message system">
        <span className="system-icon">→</span>
        <span>
          <b className="clickable" onClick={() => set({ modal: { kind: "profile", userId: m.author.id, guildId: gid } })}>
            {displayName(m.author.id, gid)}
          </b>{" "}
          joined the server.
        </span>
        <span className="time">{formatTime(m.id)}</span>
      </div>
    );
  }

  const react = (emoji: string) => {
    rememberEmoji(emoji);
    const r = m.reactions.find((r) => r.emoji === emoji);
    const on = r?.users.includes(me.id);
    const path = `/channels/${m.channel_id}/messages/${m.id}/reactions/${encodeURIComponent(emoji)}`;
    (on ? api.del(path) : api.put(path)).catch((e) => toast(e.message));
  };

  const del = () =>
    set({
      modal: {
        kind: "confirm",
        title: "Delete message",
        body: "Delete this message? This can't be undone.",
        action: "Delete",
        danger: true,
        onConfirm: () => api.del(`/channels/${m.channel_id}/messages/${m.id}`).catch((e) => toast(e.message)),
      },
    });

  const reply = () => set((s) => ({ replyTo: { ...s.replyTo, [m.channel_id]: m } }));
  const canPin = !guild || canManage;

  const menu = (e: React.MouseEvent) =>
    openMenu(e, [
      ...(has(perms, P.AddReactions) ? quickReactions.slice(0, 4).map((em) => ({ label: `React ${em}`, onClick: () => react(em) })) : []),
      { label: "Reply", onClick: reply },
      ...(mine ? [{ label: "Edit message", onClick: () => set({ editing: m.id }) }] : []),
      ...(canPin ? [{ label: m.pinned ? "Unpin message" : "Pin message", onClick: () => (m.pinned ? api.del(`/channels/${m.channel_id}/pins/${m.id}`) : api.put(`/channels/${m.channel_id}/pins/${m.id}`)).catch((err) => toast(err.message)) }] : []),
      { label: "Copy text", onClick: () => navigator.clipboard?.writeText(m.content) },
      { label: "Mark unread", onClick: () => markUnread(m) },
      { label: "Copy message ID", onClick: () => navigator.clipboard?.writeText(m.id) },
      ...(mine || canManage ? [{ divider: true, label: "" }, { label: "Delete message", danger: true, onClick: del }] : []),
    ]);

  return (
    <div className={"message" + (grouped ? " grouped" : "") + (mentioned ? " mentioned" : "") + (m.pending ? " pending" : "") + (m.failed ? " failed" : "")} onContextMenu={m.pending ? undefined : menu} data-id={m.id}>
      {m.referenced !== undefined && m.reply_to && (
        <div className="reply-preview" onClick={() => document.querySelector(`[data-id="${m.reply_to}"]`)?.scrollIntoView({ behavior: "smooth", block: "center" })}>
          <span className="reply-line" />
          {m.referenced ? (
            <>
              <Avatar user={m.referenced.author} size={16} />
              <span className="strong" style={{ color: roleColor(m.referenced.author.id, gid) }}>
                {displayName(m.referenced.author.id, gid)}
              </span>
              <span className="truncate muted">{m.referenced.content || "Click to see attachment"}</span>
            </>
          ) : (
            <span className="muted">Original message was deleted</span>
          )}
        </div>
      )}
      {grouped ? (
        <span className="time side">{new Date(idTime(m.id)).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })}</span>
      ) : (
        <div className="message-avatar" onClick={() => set({ modal: { kind: "profile", userId: m.author.id, guildId: gid } })} onContextMenu={(e) => userMenu(e, m.author.id, gid)}>
          <Avatar user={m.author} size={40} />
        </div>
      )}
      <div className="message-main">
        {!grouped && (
          <div className="message-head">
            <span className="author" style={{ color }} onClick={() => set({ modal: { kind: "profile", userId: m.author.id, guildId: gid } })} onContextMenu={(e) => userMenu(e, m.author.id, gid)}>
              {displayName(m.author.id, gid)}
            </span>
            <span className="time">{m.pending ? "Sending…" : formatTime(m.id)}</span>
          </div>
        )}
        {editing ? (
          <EditBox m={m} />
        ) : (
          m.content && (
            <div className="content">
              <Markdown text={m.content} guildId={gid} />
              {m.edited_at > 0 && <span className="edited" title={formatTime(m.edited_at)}> (edited)</span>}
            </div>
          )
        )}
        {m.failed && <div className="error small">Couldn't send this message.</div>}
        {m.attachments.length > 0 && (
          <div className="attachments">
            {m.attachments.map((a) => (
              <AttachmentView key={a.id} a={a} />
            ))}
          </div>
        )}
        {m.reactions.length > 0 && (
          <div className="reactions">
            {m.reactions.map((r) => (
              <button key={r.emoji} className={"reaction" + (r.users.includes(me.id) ? " mine" : "")} title={r.users.map((u) => displayName(u, gid)).join(", ")} onClick={() => has(perms, P.AddReactions) && react(r.emoji)}>
                <span>{r.emoji}</span>
                <span>{r.users.length}</span>
              </button>
            ))}
          </div>
        )}
      </div>
      {!m.pending && !editing && (
        <div className="message-tools">
          {has(perms, P.AddReactions) && <EmojiButton onPick={react} title="Add reaction" />}
          <button className="icon-btn tiny" title="Reply" onClick={reply}>
            <Icon name="reply" size={18} />
          </button>
          {mine && (
            <button className="icon-btn tiny" title="Edit" onClick={() => set({ editing: m.id })}>
              <Icon name="edit" size={18} />
            </button>
          )}
          {(mine || canManage) && (
            <button className="icon-btn tiny danger-hover" title="Delete" onClick={(e) => (e.shiftKey ? api.del(`/channels/${m.channel_id}/messages/${m.id}`) : del())}>
              <Icon name="trash" size={18} />
            </button>
          )}
          <button className="icon-btn tiny" title="More" onClick={menu}>
            <Icon name="more" size={18} />
          </button>
        </div>
      )}
    </div>
  );
});

function markUnread(m: Message) {
  const s = get();
  const list = s.messages[m.channel_id]?.list ?? [];
  const i = list.findIndex((x) => x.id === m.id);
  const before = i > 0 ? list[i - 1].id : null;
  // The server only moves read markers forward, so this is local until the
  // next message arrives.
  set({ readStates: { ...s.readStates, [m.channel_id]: { channel_id: m.channel_id, last_read_id: before, mention_count: 0 } } });
}

function AttachmentView({ a }: { a: Attachment }) {
  if (a.content_type.startsWith("image/") && a.width) {
    const max = 400;
    const scale = Math.min(1, max / a.width, 300 / (a.height || 1));
    return (
      <img
        className="attachment-image"
        src={a.url}
        alt={a.filename}
        loading="lazy"
        width={Math.round(a.width * scale)}
        height={Math.round((a.height ?? 0) * scale)}
        onClick={() => set({ modal: { kind: "image", url: a.url, name: a.filename } })}
      />
    );
  }
  if (a.content_type.startsWith("video/")) return <video className="attachment-video" src={a.url} controls preload="metadata" />;
  if (a.content_type.startsWith("audio/"))
    return (
      <div className="attachment-file">
        <div className="strong truncate">{a.filename}</div>
        <audio src={a.url} controls preload="metadata" />
      </div>
    );
  return (
    <a className="attachment-file row" href={a.url} download={a.filename}>
      <Icon name="file" size={32} />
      <div className="grow">
        <div className="link truncate">{a.filename}</div>
        <div className="muted small">{formatBytes(a.size)}</div>
      </div>
      <Icon name="download" />
    </a>
  );
}

function EditBox({ m }: { m: Message }) {
  const [text, setText] = useState(m.content);
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = ref.current!;
    el.focus();
    el.setSelectionRange(el.value.length, el.value.length);
  }, []);
  const save = async () => {
    set({ editing: null });
    if (text.trim() === m.content) return;
    if (!text.trim() && !m.attachments.length) return;
    try {
      await api.patch(`/channels/${m.channel_id}/messages/${m.id}`, { content: text });
    } catch (e: any) {
      toast(e.message);
    }
  };
  return (
    <div className="edit-box">
      <AutoTextarea
        ref={ref}
        value={text}
        onChange={setText}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            save();
          }
          if (e.key === "Escape") set({ editing: null });
        }}
      />
      <div className="small muted">
        escape to <a onClick={() => set({ editing: null })}>cancel</a> · enter to <a onClick={save}>save</a>
      </div>
    </div>
  );
}

const AutoTextarea = ({ value, onChange, onKeyDown, placeholder, ref, onPaste }: { value: string; onChange: (v: string) => void; onKeyDown?: (e: React.KeyboardEvent<HTMLTextAreaElement>) => void; placeholder?: string; ref?: React.Ref<HTMLTextAreaElement>; onPaste?: (e: React.ClipboardEvent) => void }) => {
  const inner = useRef<HTMLTextAreaElement | null>(null);
  useLayoutEffect(() => {
    const el = inner.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, 300) + "px";
  }, [value]);
  return (
    <textarea
      ref={(el) => {
        inner.current = el;
        if (typeof ref === "function") ref(el);
        else if (ref) (ref as React.RefObject<HTMLTextAreaElement | null>).current = el;
      }}
      rows={1}
      value={value}
      placeholder={placeholder}
      onChange={(e) => onChange(e.target.value)}
      onKeyDown={onKeyDown}
      onPaste={onPaste}
    />
  );
};

function Typing({ channelId, guildId }: { channelId: ID; guildId: ID | null }) {
  const typing = useStore((s) => s.typing[channelId]);
  const now = useNow(1000);
  const who = Object.entries(typing ?? {})
    .filter(([, until]) => until > now)
    .map(([uid]) => displayName(uid, guildId));
  return (
    <div className="typing">
      {who.length > 0 && (
        <>
          <span className="typing-dots">
            <i />
            <i />
            <i />
          </span>
          <b>{who.length > 3 ? "Several people" : who.join(", ")}</b> {who.length === 1 ? "is" : "are"} typing…
        </>
      )}
    </div>
  );
}

interface Pending {
  file: File;
  id: number;
  spoiler: boolean;
}

function Composer({ channel, guild, perms, placeholder, onSent }: { channel: Channel; guild: Guild | null; perms: number; placeholder: string; onSent: () => void }) {
  const me = useStore((s) => s.me)!;
  const maxMB = useStore((s) => s.maxUploadMB);
  const replyTo = useStore((s) => s.replyTo[channel.id]);
  const draftKey = "vc.draft." + channel.id;
  const [text, setText] = useState(() => localStorage.getItem(draftKey) ?? "");
  const [files, setFiles] = useState<Pending[]>([]);
  const [progress, setProgress] = useState<number | null>(null);
  const [dragging, setDragging] = useState(false);
  const [sel, setSel] = useState(0);
  const ref = useRef<HTMLTextAreaElement>(null);
  const fileInput = useRef<HTMLInputElement>(null);
  const lastTyping = useRef(0);

  useEffect(() => {
    ref.current?.focus();
  }, [channel.id, replyTo]);
  useEffect(() => {
    if (text) localStorage.setItem(draftKey, text);
    else localStorage.removeItem(draftKey);
  }, [text]);

  // @mention, #channel and :emoji completion for the word at the cursor.
  const [cursor, setCursor] = useState(0);
  const suggestions = useMemo(() => {
    const before = text.slice(0, cursor);
    const m = /(^|\s)([@#:])([^\s@#:]*)$/.exec(before);
    if (!m) return null;
    const [, , kind, q] = m;
    const ql = q.toLowerCase();
    const start = before.length - q.length - 1;
    if (kind === "@") {
      const items: Suggestion[] = [];
      const users = guild ? guild.members.map((x) => x.user) : get().dms[channel.id]?.users ?? [];
      for (const u of users) {
        const name = displayName(u.id, guild?.id);
        if (name.toLowerCase().includes(ql) || u.username.toLowerCase().includes(ql)) items.push({ label: name, sub: u.username, insert: `<@${u.id}> `, key: u.id });
      }
      if (guild) {
        for (const r of guild.roles) if (r.id !== guild.id && (r.mentionable || has(perms, P.MentionEveryone)) && r.name.toLowerCase().includes(ql)) items.push({ label: "@" + r.name, sub: "role", insert: `<@&${r.id}> `, key: r.id });
        if (has(perms, P.MentionEveryone)) for (const e of ["everyone", "here"]) if (e.startsWith(ql)) items.push({ label: "@" + e, sub: "notify everyone", insert: `@${e} `, key: e });
      }
      return { start, items: items.slice(0, 8) };
    }
    if (kind === "#" && guild) {
      const items: Suggestion[] = guild.channels.filter((c) => c.type === "text" && c.name.includes(ql)).slice(0, 8).map((c) => ({ label: "#" + c.name, insert: `<#${c.id}> `, key: c.id }));
      return { start, items };
    }
    if (kind === ":" && q.length >= 2) {
      return { start, items: searchEmoji(ql).map((e): Suggestion => ({ label: `${e.e}  :${e.n}:`, insert: e.e + " ", key: e.n })) };
    }
    return null;
  }, [text, cursor, guild, channel.id]);
  useEffect(() => setSel(0), [suggestions?.items.length]);

  const complete = (insert: string) => {
    if (!suggestions) return;
    const n = text.slice(0, suggestions.start) + insert + text.slice(cursor);
    setText(n);
    const pos = suggestions.start + insert.length;
    requestAnimationFrame(() => {
      ref.current?.setSelectionRange(pos, pos);
      setCursor(pos);
    });
  };

  const addFiles = (list: FileList | File[]) => {
    if (!has(perms, P.AttachFiles)) return toast("You can't upload files here.");
    const arr = [...list];
    for (const f of arr) if (f.size > maxMB * 1024 * 1024) return toast(`${f.name} is over the ${maxMB} MB limit.`);
    setFiles((cur) => [...cur, ...arr.map((file) => ({ file, id: Math.random(), spoiler: false }))].slice(0, 10));
  };

  const send = async () => {
    const content = text.trim();
    if (!content && !files.length) return;
    if (content.length > 4000) return toast("Messages are up to 4000 characters.");
    const nonce = String(Date.now()) + Math.random().toString(36).slice(2, 6);
    const reply = replyTo?.id;
    const sending = files;
    setText("");
    setFiles([]);
    set((s) => ({ replyTo: { ...s.replyTo, [channel.id]: undefined } }));
    onSent();
    if (!sending.length) {
      // Show it right away; the server's copy replaces it.
      const temp: Message = { id: nonce, nonce, channel_id: channel.id, guild_id: guild?.id ?? null, author: me, type: "default", content, edited_at: 0, reply_to: reply ?? null, attachments: [], mentions: [], mention_roles: [], mention_everyone: false, pinned: false, reactions: [], pending: true };
      addMessage(temp);
      try {
        await api.post(`/channels/${channel.id}/messages`, { content, reply_to: reply, nonce });
      } catch (e: any) {
        updateMessage(channel.id, nonce, (m) => ({ ...m, failed: true, pending: false }));
        toast(e.message);
      }
      return;
    }
    const form = new FormData();
    form.append("payload_json", JSON.stringify({ content, reply_to: reply, nonce }));
    for (const p of sending) form.append("files", p.file, (p.spoiler ? "SPOILER_" : "") + p.file.name);
    setProgress(0);
    try {
      await upload(`/channels/${channel.id}/messages`, form, setProgress);
    } catch (e: any) {
      toast(e.message);
      setFiles(sending);
    } finally {
      setProgress(null);
    }
  };

  const onKey = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (suggestions?.items.length) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        const n = suggestions.items.length;
        setSel((s) => (s + (e.key === "ArrowDown" ? 1 : n - 1)) % n);
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        complete(suggestions.items[sel].insert);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !(e.nativeEvent as any).isComposing && !isTouch()) {
      e.preventDefault();
      send();
      return;
    }
    if (e.key === "Escape" && replyTo) set((s) => ({ replyTo: { ...s.replyTo, [channel.id]: undefined } }));
    if (e.key === "ArrowUp" && !text) {
      const mine = (get().messages[channel.id]?.list ?? []).filter((m) => m.author.id === me.id && !m.pending && m.type === "default").at(-1);
      if (mine) {
        e.preventDefault();
        set({ editing: mine.id });
      }
    }
  };

  const onChange = (v: string) => {
    setText(v);
    setCursor(ref.current?.selectionStart ?? v.length);
    if (v && Date.now() - lastTyping.current > 8000) {
      lastTyping.current = Date.now();
      api.post(`/channels/${channel.id}/typing`).catch(() => {});
    }
  };

  return (
    <div
      className={"composer" + (dragging ? " dragging" : "")}
      onDragOver={(e) => {
        if (e.dataTransfer.types.includes("Files")) {
          e.preventDefault();
          setDragging(true);
        }
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        if (e.dataTransfer.files.length) addFiles(e.dataTransfer.files);
      }}
    >
      {suggestions && suggestions.items.length > 0 && (
        <div className="suggestions">
          {suggestions.items.map((it, i) => (
            <div key={it.key} className={"suggestion" + (i === sel ? " sel" : "")} onMouseDown={(e) => (e.preventDefault(), complete(it.insert))} onMouseEnter={() => setSel(i)}>
              <span>{it.label}</span>
              {it.sub && <span className="muted small">{it.sub}</span>}
            </div>
          ))}
        </div>
      )}
      {replyTo && (
        <div className="reply-bar">
          <span>
            Replying to <b>{displayName(replyTo.author.id, guild?.id)}</b>
          </span>
          <button className="icon-btn tiny" onClick={() => set((s) => ({ replyTo: { ...s.replyTo, [channel.id]: undefined } }))}>
            <Icon name="close" size={16} />
          </button>
        </div>
      )}
      {files.length > 0 && (
        <div className="pending-files">
          {files.map((p) => (
            <PendingFile key={p.id} p={p} onRemove={() => setFiles((f) => f.filter((x) => x.id !== p.id))} onSpoiler={() => setFiles((f) => f.map((x) => (x.id === p.id ? { ...x, spoiler: !x.spoiler } : x)))} />
          ))}
        </div>
      )}
      {progress !== null && (
        <div className="upload-progress">
          <div style={{ width: `${Math.round(progress * 100)}%` }} />
          <span>Uploading… {Math.round(progress * 100)}%</span>
        </div>
      )}
      <div className="composer-row">
        {has(perms, P.AttachFiles) && (
          <button className="icon-btn" title="Upload a file" onClick={() => fileInput.current?.click()}>
            <Icon name="plus" />
          </button>
        )}
        <input ref={fileInput} type="file" multiple hidden onChange={(e) => (e.target.files && addFiles(e.target.files), (e.target.value = ""))} />
        <AutoTextarea
          ref={ref}
          value={text}
          onChange={onChange}
          onKeyDown={onKey}
          placeholder={placeholder}
          onPaste={(e) => {
            const fs = [...e.clipboardData.files];
            if (fs.length) {
              e.preventDefault();
              addFiles(fs);
            }
          }}
        />
        <EmojiButton onPick={(em) => {
          const pos = ref.current?.selectionStart ?? text.length;
          setText(text.slice(0, pos) + em + text.slice(pos));
          rememberEmoji(em);
        }} title="Emoji" />
        {(isTouch() || text.trim() || files.length > 0) && (
          <button className="icon-btn send" title="Send" onClick={send}>
            <Icon name="chevronRight" />
          </button>
        )}
      </div>
    </div>
  );
}

type Suggestion = { label: string; sub?: string; insert: string; key: string };

const isTouch = () => matchMedia("(pointer: coarse)").matches;

function PendingFile({ p, onRemove, onSpoiler }: { p: Pending; onRemove: () => void; onSpoiler: () => void }) {
  const url = useMemo(() => (p.file.type.startsWith("image/") ? URL.createObjectURL(p.file) : null), [p.file]);
  useEffect(() => () => void (url && URL.revokeObjectURL(url)), [url]);
  return (
    <div className="pending-file">
      {url ? <img src={url} alt="" className={p.spoiler ? "blur" : ""} /> : <Icon name="file" size={40} />}
      <div className="truncate small">{p.file.name}</div>
      <div className="pending-actions">
        {url && (
          <button className="icon-btn tiny" title={p.spoiler ? "Remove spoiler" : "Mark as spoiler"} onClick={onSpoiler}>
            <Icon name={p.spoiler ? "eye" : "eyeOff"} size={16} />
          </button>
        )}
        <button className="icon-btn tiny danger-hover" title="Remove" onClick={onRemove}>
          <Icon name="trash" size={16} />
        </button>
      </div>
    </div>
  );
}

export function EmojiButton({ onPick, title }: { onPick: (e: string) => void; title: string }) {
  const [open, setOpen] = useState(false);
  return (
    <div className="emoji-wrap">
      <button className="icon-btn tiny" title={title} onClick={() => setOpen(!open)}>
        <Icon name="smile" size={20} />
      </button>
      {open && (
        <EmojiPicker
          onPick={(e) => {
            onPick(e);
            setOpen(false);
          }}
          onClose={() => setOpen(false)}
        />
      )}
    </div>
  );
}

function EmojiPicker({ onPick, onClose }: { onPick: (e: string) => void; onClose: () => void }) {
  const [q, setQ] = useState("");
  const ref = useRef<HTMLDivElement>(null);
  const close = useCallback(
    (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) onClose();
    },
    [onClose],
  );
  useEffect(() => {
    setTimeout(() => window.addEventListener("mousedown", close));
    return () => window.removeEventListener("mousedown", close);
  }, [close]);
  // Open above or below depending on room.
  const [up, setUp] = useState(true);
  useLayoutEffect(() => {
    const r = ref.current?.parentElement?.getBoundingClientRect();
    if (r) setUp(r.top > 380);
  }, []);
  const recent = recentEmoji();
  const results = q ? allEmoji.filter((x) => x.n.includes(q.toLowerCase())) : null;
  return (
    <div className={"emoji-picker" + (up ? " up" : " down")} ref={ref}>
      <input autoFocus placeholder="Find an emoji" value={q} onChange={(e) => setQ(e.target.value)} onKeyDown={(e) => e.key === "Escape" && onClose()} />
      <div className="emoji-scroll">
        {results ? (
          <div className="emoji-grid">
            {results.map((x) => (
              <button key={x.n} title={`:${x.n}:`} onClick={() => onPick(x.e)}>
                {x.e}
              </button>
            ))}
          </div>
        ) : (
          <>
            {recent.length > 0 && (
              <>
                <div className="emoji-cat">Recent</div>
                <div className="emoji-grid">
                  {recent.map((e) => (
                    <button key={e} onClick={() => onPick(e)}>
                      {e}
                    </button>
                  ))}
                </div>
              </>
            )}
            {emojiCategories.map((c) => (
              <div key={c.name}>
                <div className="emoji-cat">{c.name}</div>
                <div className="emoji-grid">
                  {c.list.map((x) => (
                    <button key={x.n} title={`:${x.n}:`} onClick={() => onPick(x.e)}>
                      {x.e}
                    </button>
                  ))}
                </div>
              </div>
            ))}
          </>
        )}
      </div>
    </div>
  );
}
