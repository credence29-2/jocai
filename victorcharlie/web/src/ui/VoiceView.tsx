import { useEffect, useRef, useState } from "react";
import { Participant, Track, type VideoTrack } from "livekit-client";
import { displayName, set, useStore } from "../store";
import { roomParticipants, saveSettings, useVoice, voice } from "../voice";
import { Avatar, openMenu } from "./common";
import { Icon } from "./Icon";
import { userMenu } from "./Sidebar";

interface Tile {
  key: string;
  uid: string;
  kind: "user" | "screen";
  local: boolean;
  video?: VideoTrack;
  watching?: boolean;
}

function tilesFor(ps: Participant[], watching: Record<string, boolean>): Tile[] {
  const out: Tile[] = [];
  for (const p of ps) {
    const local = p === voice.room?.localParticipant;
    const cam = p.getTrackPublication(Track.Source.Camera);
    const camTrack = cam && !cam.isMuted ? (cam.videoTrack as VideoTrack | undefined) : undefined;
    out.push({ key: p.identity + ":camera", uid: p.identity, kind: "user", local, video: camTrack });
    const scr = p.getTrackPublication(Track.Source.ScreenShare);
    if (scr) {
      out.push({ key: p.identity + ":screen", uid: p.identity, kind: "screen", local, video: scr.videoTrack as VideoTrack | undefined, watching: local || !!watching[p.identity] });
    }
  }
  return out;
}

export function VoiceView({ compact }: { compact?: boolean }) {
  const v = useVoice();
  const ps = roomParticipants();
  void v.version;
  const tiles = tilesFor(ps, v.watching);
  const focused = v.focus ? tiles.find((t) => t.key === v.focus) : undefined;
  const [hideControls, setHide] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const poke = () => {
    setHide(false);
    clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setHide(true), 3000);
  };
  useEffect(() => () => clearTimeout(timer.current), []);

  if (v.status !== "connected" && v.status !== "reconnecting") {
    return <div className={"voice-view" + (compact ? " compact" : "")}><div className="voice-wait">Connecting to voice…</div></div>;
  }

  const others = focused ? tiles.filter((t) => t !== focused) : tiles;
  return (
    <div className={"voice-view" + (compact ? " compact" : "") + (focused ? " has-focus" : "") + (hideControls && focused ? " hide-controls" : "")} onMouseMove={poke}>
      {focused ? (
        <>
          <div className="focus-area">
            <TileView t={focused} focused />
          </div>
          <div className="tile-strip">
            {others.map((t) => (
              <TileView key={t.key} t={t} small />
            ))}
          </div>
        </>
      ) : (
        <div className={"tile-grid n" + Math.min(tiles.length, 9)}>
          {tiles.map((t) => (
            <TileView key={t.key} t={t} />
          ))}
        </div>
      )}
      <Controls />
    </div>
  );
}

function TileView({ t, focused, small }: { t: Tile; focused?: boolean; small?: boolean }) {
  const user = useStore((s) => s.users[t.uid]);
  const vs = useStore((s) => s.voiceStates[t.uid]);
  const guildId = useVoice((s) => s.guildId);
  const speaking = useVoice((s) => s.speaking[t.uid]);
  const localMute = useVoice((s) => s.localMutes[t.uid]);
  const ref = useRef<HTMLDivElement>(null);
  const name = displayName(t.uid, guildId);
  const muted = vs?.self_mute || vs?.mute || localMute;
  const deaf = vs?.self_deaf || vs?.deaf;

  const toggleFocus = () => voice.setFocus(focused ? null : t.key);
  const fullscreen = () => {
    const el = ref.current;
    if (!el) return;
    if (document.fullscreenElement) document.exitFullscreen();
    else el.requestFullscreen?.().catch(() => {});
  };

  if (t.kind === "screen" && !t.watching) {
    return (
      <div className={"tile screen-offer" + (small ? " small" : "")} ref={ref}>
        <Avatar user={user} size={small ? 32 : 64} />
        <div className="tile-offer-text">
          <span className="live">LIVE</span> {name} is sharing their screen
        </div>
        <button className="btn primary" onClick={() => voice.watch(t.uid, true)}>
          Watch stream
        </button>
      </div>
    );
  }

  return (
    <div
      ref={ref}
      className={"tile " + t.kind + (speaking && !muted && t.kind === "user" ? " speaking" : "") + (small ? " small" : "") + (focused ? " focused" : "")}
      onClick={toggleFocus}
      onDoubleClick={fullscreen}
      onContextMenu={(e) =>
        t.kind === "screen" && !t.local
          ? openMenu(e, [
              { label: "Stream volume", slider: { value: Math.round((useVoice.getState().volumes["s:" + t.uid] ?? 1) * 100), min: 0, max: 200, format: (n) => n + "%", onChange: (n) => voice.setUserVolume(t.uid, n / 100, true) } },
              { label: "Full screen", onClick: fullscreen },
              { label: "Stop watching", onClick: () => voice.watch(t.uid, false) },
            ])
          : userMenu(e, t.uid, guildId)
      }
    >
      {t.video ? <VideoEl track={t.video} mirror={t.local && t.kind === "user"} fit={t.kind === "screen" ? "contain" : "cover"} /> : (
        <div className="tile-avatar">
          <Avatar user={user} size={small ? 40 : 80} speaking={speaking && !muted} />
        </div>
      )}
      <div className="tile-label">
        {t.kind === "screen" && <span className="live">LIVE</span>}
        <span className="truncate">{t.kind === "screen" ? `${name}'s screen` : name}</span>
        {t.kind === "user" && muted && <Icon name="micOff" size={14} className={vs?.mute ? "danger" : ""} />}
        {t.kind === "user" && deaf && <Icon name="headphonesOff" size={14} className={vs?.deaf ? "danger" : ""} />}
      </div>
      {!small && (
        <div className="tile-actions">
          {t.kind === "screen" && !t.local && (
            <button className="icon-btn tiny" title="Stop watching" onClick={(e) => (e.stopPropagation(), voice.watch(t.uid, false))}>
              <Icon name="eyeOff" size={16} />
            </button>
          )}
          <button className="icon-btn tiny" title="Full screen" onClick={(e) => (e.stopPropagation(), fullscreen())}>
            <Icon name="maximize" size={16} />
          </button>
        </div>
      )}
    </div>
  );
}

function VideoEl({ track, mirror, fit }: { track: VideoTrack; mirror?: boolean; fit: "cover" | "contain" }) {
  const ref = useRef<HTMLVideoElement>(null);
  useEffect(() => {
    const el = ref.current!;
    track.attach(el);
    return () => {
      track.detach(el);
    };
  }, [track]);
  return <video ref={ref} autoPlay playsInline muted style={{ objectFit: fit, transform: mirror ? "scaleX(-1)" : undefined }} />;
}

function Controls() {
  const v = useVoice();
  const me = useStore((s) => s.me)!;
  const own = useStore((s) => s.voiceStates[me.id]);
  const serverMuted = !!own?.mute;
  const shareMenu = (e: React.MouseEvent) => {
    const st = v.settings;
    openMenu(e, [
      ...(["720", "1080", "1440", "source"] as const).map((r) => ({ label: r === "source" ? "Source resolution" : r + "p", checked: st.shareRes === r, onClick: () => saveSettings({ shareRes: r }) })),
      { divider: true, label: "" },
      ...([15, 30, 60] as const).map((f) => ({ label: f + " fps", checked: st.shareFps === f, onClick: () => saveSettings({ shareFps: f }) })),
      { divider: true, label: "" },
      { label: "Share audio", checked: st.shareAudio, onClick: () => saveSettings({ shareAudio: !st.shareAudio }) },
      { label: "Changes apply next time you share", disabled: true },
    ]);
  };
  return (
    <div className="voice-controls">
      <button className={"round-btn" + (v.selfMute || serverMuted ? " off" : "")} title={v.selfMute ? "Unmute" : "Mute"} onClick={() => voice.toggleMute()}>
        <Icon name={v.selfMute || serverMuted ? "micOff" : "mic"} />
      </button>
      <button className={"round-btn" + (v.selfDeaf ? " off" : "")} title={v.selfDeaf ? "Undeafen" : "Deafen"} onClick={() => voice.toggleDeaf()}>
        <Icon name={v.selfDeaf ? "headphonesOff" : "headphones"} />
      </button>
      <button className={"round-btn" + (v.camera ? " on" : "")} disabled={!v.canVideo} title="Camera" onClick={() => voice.toggleCamera()}>
        <Icon name={v.camera ? "video" : "videoOff"} />
      </button>
      <div className="split-btn">
        <button className={"round-btn" + (v.sharing ? " on" : "")} disabled={!v.canShare} title={!v.canShare ? "Screen sharing isn't available here" : v.sharing ? "Stop sharing" : "Share your screen"} onClick={() => voice.toggleShare()}>
          <Icon name={v.sharing ? "screenOff" : "screen"} />
        </button>
        <button className="round-btn mini" title="Stream quality" onClick={shareMenu}>
          <Icon name="chevronDown" size={14} />
        </button>
      </div>
      <button className="round-btn hangup" title="Disconnect" onClick={() => voice.leave()}>
        <Icon name="hangup" />
      </button>
      <button className="round-btn" title="Voice settings" onClick={() => set({ modal: { kind: "userSettings", tab: "voice" } })}>
        <Icon name="settings" />
      </button>
    </div>
  );
}
