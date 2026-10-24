import { api } from "../api";
import { channelPerms, has, P } from "../perms";
import { navigate } from "../router";
import { displayName, get, set, statusOf, toast, useStore } from "../store";
import type { Channel, DM, Guild } from "../types";
import { useVoice, voice } from "../voice";
import { Chat } from "./Chat";
import { Avatar } from "./common";
import { Icon } from "./Icon";
import { Members } from "./Members";
import { VoiceView } from "./VoiceView";

export function Main() {
  const guildId = useStore((s) => s.guildId);
  const cid = useStore((s) => s.channelByGuild[s.guildId]);
  const guild = useStore((s) => (s.guildId === "@me" ? null : s.guilds[s.guildId]));
  const dm = useStore((s) => (cid ? s.dms[cid] : undefined));
  const showMembers = useStore((s) => s.showMembers);
  const voiceChannel = useVoice((s) => s.channelId);
  const channel = guild?.channels.find((c) => c.id === cid) ?? dm;

  if (!channel) {
    return (
      <main className="main">
        <Header title={guildId === "@me" ? "Home" : guild?.name ?? ""} icon="home" />
        {guildId === "@me" ? <Home /> : <div className="empty-state">Pick a channel on the left.</div>}
      </main>
    );
  }

  const isVoice = channel.type === "voice";
  const inCall = channel.type === "dm" && voiceChannel === channel.id;
  const membersVisible = showMembers && !isVoice && (channel.type !== "dm" || (dm?.users.length ?? 0) > 2);

  return (
    <main className="main">
      <ChannelHeader channel={channel} guild={guild} dm={dm} />
      <div className="main-body">
        <div className="main-content">
          {isVoice ? (
            voiceChannel === channel.id ? (
              <VoiceView />
            ) : (
              <VoiceLobby channel={channel} guild={guild!} />
            )
          ) : (
            <>
              {inCall && (
                <div className="call-area">
                  <VoiceView compact />
                </div>
              )}
              <Chat channel={channel} guild={guild} key={channel.id} />
            </>
          )}
        </div>
        {membersVisible && (guild ? <Members guild={guild} channel={channel} /> : dm && <Members dm={dm} />)}
      </div>
    </main>
  );
}

function Header({ title, icon, children, topic }: { title: string; icon: string; children?: React.ReactNode; topic?: string }) {
  return (
    <header className="main-head">
      <button className="icon-btn mobile-only" onClick={() => set({ mobilePane: "sidebar" })} aria-label="Back">
        <Icon name="menu" />
      </button>
      <Icon name={icon} className="muted" />
      <h1 className="truncate">{title}</h1>
      {topic && <div className="topic truncate" title={topic}>{topic}</div>}
      <div className="head-actions">{children}</div>
    </header>
  );
}

function ChannelHeader({ channel, guild, dm }: { channel: Channel; guild: Guild | null; dm?: DM }) {
  const me = useStore((s) => s.me)!;
  const showMembers = useStore((s) => s.showMembers);
  const voiceChannel = useVoice((s) => s.channelId);
  const toggleMembers = () => {
    localStorage.setItem("kempt.showMembers", showMembers ? "0" : "1");
    set({ showMembers: !showMembers, mobilePane: window.innerWidth < 800 ? "members" : get().mobilePane });
  };
  if (dm) {
    const others = dm.users.filter((u) => u.id !== me.id);
    const title = dm.name || others.map((u) => u.display_name).join(", ");
    return (
      <Header title={title} icon={others.length > 1 ? "users" : "at"}>
        {voiceChannel !== dm.id && (
          <button className="icon-btn" title="Start a voice call" onClick={() => voice.join(dm.id)}>
            <Icon name="phone" />
          </button>
        )}
        <button className="icon-btn" title="Pinned messages" onClick={() => set({ modal: { kind: "pins", channelId: dm.id } })}>
          <Icon name="pin" />
        </button>
        <button className="icon-btn" title="Search" onClick={() => set({ modal: { kind: "search", guildId: null, channelId: dm.id } })}>
          <Icon name="search" />
        </button>
        {others.length > 1 && (
          <button className={"icon-btn" + (showMembers ? " on" : "")} title="Member list" onClick={toggleMembers}>
            <Icon name="users" />
          </button>
        )}
      </Header>
    );
  }
  const g = guild!;
  const canInvite = has(channelPerms(g, me.id, channel), P.CreateInvite);
  return (
    <Header title={channel.name} icon={channel.type === "voice" ? "speaker" : "hash"} topic={channel.topic}>
      {canInvite && (
        <button className="icon-btn" title="Invite people" onClick={() => set({ modal: { kind: "invite", guildId: g.id } })}>
          <Icon name="userPlus" />
        </button>
      )}
      {channel.type === "text" && (
        <>
          <button className="icon-btn" title="Pinned messages" onClick={() => set({ modal: { kind: "pins", channelId: channel.id } })}>
            <Icon name="pin" />
          </button>
          <button className="icon-btn" title="Search" onClick={() => set({ modal: { kind: "search", guildId: g.id } })}>
            <Icon name="search" />
          </button>
          <button className={"icon-btn" + (showMembers ? " on" : "")} title="Member list" onClick={toggleMembers}>
            <Icon name="users" />
          </button>
        </>
      )}
    </Header>
  );
}

function VoiceLobby({ channel, guild }: { channel: Channel; guild: Guild }) {
  const voiceStates = useStore((s) => s.voiceStates);
  const users = useStore((s) => s.users);
  const here = Object.values(voiceStates).filter((v) => v.channel_id === channel.id);
  const me = useStore((s) => s.me)!;
  const canJoin = has(channelPerms(guild, me.id, channel), P.Connect);
  return (
    <div className="voice-lobby">
      <Icon name="speaker" size={48} className="muted" />
      <h2>{channel.name}</h2>
      <div className="lobby-people">
        {here.length === 0 ? (
          <span className="muted">No one's here yet.</span>
        ) : (
          here.map((v) => (
            <div key={v.user_id} className="lobby-person">
              <Avatar user={users[v.user_id]} size={40} />
              <span className="small">{displayName(v.user_id, guild.id)}</span>
              {v.streaming && <span className="live">LIVE</span>}
            </div>
          ))
        )}
      </div>
      <button className="btn primary" disabled={!canJoin} onClick={() => voice.join(channel.id)}>
        {canJoin ? "Join voice" : "You can't join this channel"}
      </button>
    </div>
  );
}

function Home() {
  const guilds = useStore((s) => s.guilds);
  const me = useStore((s) => s.me)!;
  const presences = useStore((s) => s.presences);
  void presences;
  const people = new Map<string, { id: string; guilds: string[] }>();
  for (const g of Object.values(guilds)) {
    for (const m of g.members) {
      if (m.user_id === me.id) continue;
      const p = people.get(m.user_id) ?? { id: m.user_id, guilds: [] };
      p.guilds.push(g.name);
      people.set(m.user_id, p);
    }
  }
  const list = [...people.values()].sort((a, b) => {
    const on = (id: string) => (statusOf(id) === "offline" ? 1 : 0);
    return on(a.id) - on(b.id) || displayName(a.id).localeCompare(displayName(b.id));
  });
  const users = get().users;
  return (
    <div className="home">
      <h2>People</h2>
      {list.length === 0 && (
        <p className="muted">
          Nobody else is here yet. Open a server, right-click it, and pick <b>Invite people</b> to send someone a link.
        </p>
      )}
      <div className="people-list">
        {list.map((p) => (
          <div key={p.id} className="person-row" onClick={() => set({ modal: { kind: "profile", userId: p.id } })}>
            <Avatar user={users[p.id]} size={36} status={statusOf(p.id)} />
            <div className="grow">
              <div className="strong">{displayName(p.id)}</div>
              <div className="muted small truncate">{users[p.id]?.custom_status || p.guilds.join(", ")}</div>
            </div>
            <button
              className="icon-btn"
              title="Message"
              onClick={async (e) => {
                e.stopPropagation();
                try {
                  const dm = await api.post<DM>("/users/@me/channels", { recipients: [p.id] });
                  navigate("@me", dm.id);
                } catch (err: any) {
                  toast(err.message);
                }
              }}
            >
              <Icon name="message" />
            </button>
          </div>
        ))}
      </div>
    </div>
  );
}
