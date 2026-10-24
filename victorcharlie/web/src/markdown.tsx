import { Fragment, useState, type ReactNode } from "react";
import { channelById, displayName, get, roleColor, set } from "./store";
import type { ID } from "./types";
import { openChannelById } from "./notify";

// A small Discord-flavored markdown renderer. It builds React elements, never
// HTML strings, so message text can't inject markup.

type Rule = { re: RegExp; render: (m: RegExpExecArray, ctx: Ctx, key: number) => ReactNode };
type Ctx = { guildId?: ID | null; inline?: boolean };

const urlRe = /^(https?:\/\/[^\s<]+[^\s<.,:;"')\]!?])/;

const rules: Rule[] = [
  { re: /^```(?:([\w+-]{1,20})\n)?\n?([\s\S]*?)```/, render: (m, ctx, k) => (ctx.inline ? <code key={k}>{m[2]}</code> : <pre key={k} className="codeblock"><code>{m[2].replace(/\n$/, "")}</code></pre>) },
  { re: /^`([^`]+)`/, render: (m, _c, k) => <code key={k}>{m[1]}</code> },
  { re: /^\|\|([\s\S]+?)\|\|/, render: (m, c, k) => <Spoiler key={k}>{parse(m[1], c)}</Spoiler> },
  { re: /^\*\*([\s\S]+?)\*\*(?!\*)/, render: (m, c, k) => <strong key={k}>{parse(m[1], c)}</strong> },
  { re: /^__([\s\S]+?)__(?!_)/, render: (m, c, k) => <u key={k}>{parse(m[1], c)}</u> },
  { re: /^\*([^\s*][\s\S]*?)\*(?!\*)/, render: (m, c, k) => <em key={k}>{parse(m[1], c)}</em> },
  { re: /^_([^\s_][\s\S]*?)_(?![\w_])/, render: (m, c, k) => <em key={k}>{parse(m[1], c)}</em> },
  { re: /^~~([\s\S]+?)~~/, render: (m, c, k) => <s key={k}>{parse(m[1], c)}</s> },
  { re: /^<@(\d+)>/, render: (m, c, k) => <UserMention key={k} id={m[1]} guildId={c.guildId} /> },
  { re: /^<@&(\d+)>/, render: (m, c, k) => <RoleMention key={k} id={m[1]} guildId={c.guildId} /> },
  { re: /^<#(\d+)>/, render: (m, _c, k) => <ChannelMention key={k} id={m[1]} /> },
  { re: /^@(everyone|here)\b/, render: (m, _c, k) => <span key={k} className="mention">@{m[1]}</span> },
  { re: /^<(https?:\/\/[^\s>]+)>/, render: (m, _c, k) => <Link key={k} href={m[1]} /> },
  { re: /^\[([^\]\n]+)\]\((https?:\/\/[^\s)]+)\)/, render: (m, _c, k) => <Link key={k} href={m[2]} text={m[1]} /> },
  { re: urlRe, render: (m, _c, k) => <Link key={k} href={m[1]} /> },
  { re: /^\\([*_`~|\\<>#@[\]()])/, render: (m) => m[1] },
];

// Text up to the next character that could start a rule.
const plainRe = /^[\s\S]+?(?=[*_`~|\\<@[h]|$)/;

export function parse(src: string, ctx: Ctx = {}): ReactNode[] {
  const out: ReactNode[] = [];
  let text = "";
  let k = 0;
  const flush = () => {
    if (text) out.push(text);
    text = "";
  };
  while (src.length) {
    let matched = false;
    for (const r of rules) {
      const m = r.re.exec(src);
      if (m) {
        flush();
        out.push(r.render(m, ctx, k++));
        src = src.slice(m[0].length);
        matched = true;
        break;
      }
    }
    if (matched) continue;
    const p = plainRe.exec(src);
    const take = p && p[0].length ? p[0].length : 1;
    text += src.slice(0, take);
    src = src.slice(take);
  }
  flush();
  return out;
}

// Block-level: code fences, quotes and headings, then inline rules.
export function Markdown({ text, guildId }: { text: string; guildId?: ID | null }) {
  const blocks: ReactNode[] = [];
  const parts = text.split(/(```[\s\S]*?```)/);
  parts.forEach((part, i) => {
    if (i % 2 === 1) {
      blocks.push(<Fragment key={blocks.length}>{parse(part, { guildId })}</Fragment>);
      return;
    }
    // Drop the newline that sits right next to a code fence.
    if (i > 0) part = part.replace(/^\n/, "");
    if (i < parts.length - 1) part = part.replace(/\n$/, "");
    if (!part) return;
    let quote: string[] = [];
    let para: string[] = [];
    const flushQuote = () => {
      if (quote.length) blocks.push(<blockquote key={blocks.length}>{parse(quote.join("\n"), { guildId })}</blockquote>);
      quote = [];
    };
    const flushPara = () => {
      if (para.length) blocks.push(<Fragment key={blocks.length}>{parse(para.join("\n"), { guildId })}</Fragment>);
      para = [];
    };
    for (const line of part.split("\n")) {
      const q = /^> ?(.*)$/.exec(line);
      if (q) {
        flushPara();
        quote.push(q[1]);
        continue;
      }
      flushQuote();
      const h = /^(#{1,3}) (.+)$/.exec(line);
      if (h) {
        flushPara();
        const Tag = (["h1", "h2", "h3"] as const)[h[1].length - 1];
        blocks.push(<Tag key={blocks.length}>{parse(h[2], { guildId })}</Tag>);
        continue;
      }
      para.push(line);
    }
    flushQuote();
    flushPara();
  });
  return <>{blocks}</>;
}

function Spoiler({ children }: { children: ReactNode }) {
  const [shown, setShown] = useState(false);
  return (
    <span className={"spoiler" + (shown ? " shown" : "")} onClick={() => setShown(true)}>
      {children}
    </span>
  );
}

function Link({ href, text }: { href: string; text?: string }) {
  return (
    <a href={href} target="_blank" rel="noreferrer noopener">
      {text ?? href}
    </a>
  );
}

function UserMention({ id, guildId }: { id: ID; guildId?: ID | null }) {
  return (
    <span className="mention" onClick={() => set({ modal: { kind: "profile", userId: id, guildId } })}>
      @{displayName(id, guildId)}
    </span>
  );
}

function RoleMention({ id, guildId }: { id: ID; guildId?: ID | null }) {
  const r = guildId ? get().guilds[guildId]?.roles.find((r) => r.id === id) : undefined;
  const color = r?.color ? "#" + r.color.toString(16).padStart(6, "0") : undefined;
  return (
    <span className="mention" style={color ? { color, background: color + "26" } : undefined}>
      @{r?.name ?? "deleted-role"}
    </span>
  );
}

function ChannelMention({ id }: { id: ID }) {
  const c = channelById(id);
  return (
    <span className="mention" onClick={() => c && openChannelById(id)}>
      #{c?.name ?? "unknown"}
    </span>
  );
}

export { roleColor };
