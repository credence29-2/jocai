import {
  ConnectionQuality,
  DisconnectReason,
  LocalAudioTrack,
  LocalParticipant,
  Participant,
  RemoteParticipant,
  RemoteTrack,
  RemoteTrackPublication,
  Room,
  RoomEvent,
  Track,
  VideoPresets,
  type AudioCaptureOptions,
  type ScreenShareCaptureOptions,
  type TrackPublishOptions,
  type VideoCodec,
} from "livekit-client";
import { create } from "zustand";
import { api } from "./api";
import { appName } from "./brand";
import { get as getApp, set as setApp, toast } from "./store";
import { channelPerms, has, P } from "./perms";
import { playSound } from "./sounds";
import { send } from "./gateway";
import type { ID, VoiceState } from "./types";

// --- settings, kept per device ---

export interface VoiceSettings {
  inputDevice: string;
  outputDevice: string;
  videoDevice: string;
  mode: "vad" | "ptt";
  pttKey: string; // KeyboardEvent.code, or "Mouse4" style
  pttRelease: number; // ms to keep the mic open after letting go
  noiseSuppression: boolean;
  echoCancellation: boolean;
  autoGain: boolean;
  stereo: boolean;
  outputVolume: number; // 0..2
  // Screen share
  shareRes: "720" | "1080" | "1440" | "source";
  shareFps: 15 | 30 | 60;
  shareAudio: boolean;
  shareCodec: VideoCodec;
  muteKey: string;
  deafenKey: string;
}

const defaults: VoiceSettings = {
  inputDevice: "default",
  outputDevice: "default",
  videoDevice: "",
  mode: "vad",
  pttKey: "Backquote",
  pttRelease: 200,
  noiseSuppression: true,
  echoCancellation: true,
  autoGain: true,
  stereo: false,
  outputVolume: 1,
  shareRes: "1080",
  shareFps: 30,
  shareAudio: true,
  shareCodec: "vp8",
  muteKey: "",
  deafenKey: "",
};

function loadSettings(): VoiceSettings {
  try {
    return { ...defaults, ...JSON.parse(localStorage.getItem("vc.voice") || "{}") };
  } catch {
    return defaults;
  }
}

function loadVolumes(): Record<string, number> {
  try {
    return JSON.parse(localStorage.getItem("vc.volumes") || "{}");
  } catch {
    return {};
  }
}

// --- UI state ---

export interface VoiceUI {
  channelId: ID | null;
  guildId: ID | null;
  status: "idle" | "connecting" | "connected" | "reconnecting";
  selfMute: boolean;
  selfDeaf: boolean;
  pttDown: boolean;
  settings: VoiceSettings;
  volumes: Record<string, number>; // uid -> 0..2, and "s:uid" for stream audio
  localMutes: Record<string, boolean>;
  watching: Record<string, boolean>; // uid -> watching their stream
  focus: string | null; // "uid:camera" or "uid:screen"
  speaking: Record<string, boolean>;
  quality: Record<string, ConnectionQuality>;
  ping: number;
  canSpeak: boolean;
  canVideo: boolean;
  canShare: boolean; // video allowed and this device can capture its screen
  mustPTT: boolean;
  sharing: boolean;
  camera: boolean;
  version: number; // bumps whenever tracks change, for re-rendering
}

export const useVoice = create<VoiceUI>(() => ({
  channelId: null,
  guildId: null,
  status: "idle",
  selfMute: localStorage.getItem("vc.selfMute") === "1",
  selfDeaf: localStorage.getItem("vc.selfDeaf") === "1",
  pttDown: false,
  settings: loadSettings(),
  volumes: loadVolumes(),
  localMutes: {},
  watching: {},
  focus: null,
  speaking: {},
  quality: {},
  ping: 0,
  canSpeak: true,
  canVideo: true,
  canShare: true,
  mustPTT: false,
  sharing: false,
  camera: false,
  version: 0,
}));

const vget = useVoice.getState;
const vset = useVoice.setState;
const bump = () => vset((s) => ({ version: s.version + 1 }));

export function saveSettings(patch: Partial<VoiceSettings>) {
  const settings = { ...vget().settings, ...patch };
  localStorage.setItem("vc.voice", JSON.stringify(settings));
  vset({ settings });
  voice.applySettings(patch);
}

const shareSizes = { "720": { width: 1280, height: 720 }, "1080": { width: 1920, height: 1080 }, "1440": { width: 2560, height: 1440 }, source: { width: 3840, height: 2160 } };

function shareBitrate(res: VoiceSettings["shareRes"], fps: number) {
  const base = { "720": 1_700_000, "1080": 3_000_000, "1440": 5_000_000, source: 8_000_000 }[res];
  return Math.round(base * (fps >= 60 ? 1.6 : fps <= 15 ? 0.6 : 1));
}

class Voice {
  room: Room | null = null;
  private joinSeq = 0;
  private pttTimer: number | undefined;
  private leaving = false;
  private audioEls = new Map<string, HTMLMediaElement>();
  private audioBox: HTMLDivElement;
  private priority = new Set<string>();

  constructor() {
    this.audioBox = document.createElement("div");
    this.audioBox.style.display = "none";
    document.addEventListener("DOMContentLoaded", () => document.body.appendChild(this.audioBox));
    if (document.body) document.body.appendChild(this.audioBox);
    this.bindKeys();
  }

  get channelId() {
    return vget().channelId;
  }
  get guildId() {
    return vget().guildId;
  }

  async join(cid: ID) {
    const cur = vget();
    if (cur.channelId === cid && cur.status !== "idle") return;
    if (this.room) await this.leave(true);
    const seq = ++this.joinSeq;
    const app = getApp();
    const c = Object.values(app.guilds).flatMap((g) => g.channels).find((c) => c.id === cid) ?? app.dms[cid];
    if (!c) return;
    vset({ channelId: cid, guildId: c.guild_id || null, status: "connecting", watching: {}, focus: null, speaking: {}, quality: {}, sharing: false, camera: false });
    let res: { token: string; permissions: number; bitrate: number; use_vad: boolean };
    try {
      res = await api.post(`/voice/${cid}/join`, { self_mute: cur.selfMute, self_deaf: cur.selfDeaf });
    } catch (e: any) {
      if (seq === this.joinSeq) vset({ channelId: null, guildId: null, status: "idle" });
      toast(e.message);
      return;
    }
    if (seq !== this.joinSeq) return;
    const st = vget().settings;
    const room = new Room({
      adaptiveStream: true,
      dynacast: true,
      webAudioMix: true,
      disconnectOnPageLeave: true,
      audioCaptureDefaults: this.captureOptions(st),
      audioOutput: { deviceId: st.outputDevice },
      publishDefaults: {
        audioPreset: { maxBitrate: res.bitrate * 1000 },
        dtx: true,
        red: true,
        forceStereo: st.stereo,
        simulcast: true,
        videoCodec: "vp8",
      },
      videoCaptureDefaults: { resolution: VideoPresets.h720.resolution, deviceId: st.videoDevice || undefined },
    });
    this.room = room;
    this.wire(room);
    const url = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}`;
    try {
      await room.connect(url, res.token, { autoSubscribe: false });
    } catch (e: any) {
      if (this.room === room) {
        this.room = null;
        vset({ channelId: null, guildId: null, status: "idle" });
        toast(
          location.protocol !== "https:" && location.hostname !== "localhost"
            ? "Couldn't connect to voice. This page isn't on HTTPS, which voice needs."
            : "Couldn't connect to voice. If you're outside the server's network, its voice ports may not be forwarded. " + (e?.message ?? ""),
        );
      }
      return;
    }
    if (this.room !== room) return;
    room.startAudio().catch(() => {});
    const canSpeak = has(res.permissions, P.Speak);
    vset({
      status: "connected",
      canSpeak,
      canVideo: has(res.permissions, P.Video),
      canShare: has(res.permissions, P.Video) && (!!window.victorCharlieDesktop || !!navigator.mediaDevices?.getDisplayMedia),
      mustPTT: !res.use_vad,
    });
    playSound("join");
    window.VictorCharlieAndroid?.voice?.(true, c.type === "dm" ? "In a call" : `In ${c.name}`);
    for (const p of room.remoteParticipants.values()) this.subscribeDefaults(p);
    this.refreshPriority();
    await this.publishMic();
    this.sendState();
    if (c.type === "dm" && room.remoteParticipants.size === 0) {
      api.post(`/voice/${cid}/ring`).catch(() => {});
    }
  }

  async leave(switching = false) {
    this.joinSeq++;
    const room = this.room;
    this.room = null;
    if (room) {
      this.leaving = true;
      await room.disconnect().catch(() => {});
      this.leaving = false;
    }
    for (const el of this.audioEls.values()) el.remove();
    this.audioEls.clear();
    if (!switching) window.VictorCharlieAndroid?.voice?.(false, "");
    if (!switching) {
      if (vget().status !== "idle") playSound("disconnect");
      vset({ channelId: null, guildId: null, status: "idle", sharing: false, camera: false, watching: {}, focus: null, speaking: {} });
    }
    bump();
  }

  private captureOptions(st: VoiceSettings): AudioCaptureOptions {
    return {
      deviceId: st.inputDevice === "default" ? undefined : st.inputDevice,
      noiseSuppression: st.noiseSuppression,
      echoCancellation: st.echoCancellation,
      autoGainControl: st.autoGain,
      channelCount: st.stereo ? 2 : 1,
      voiceIsolation: st.noiseSuppression,
    } as AudioCaptureOptions;
  }

  private micAllowed() {
    const v = vget();
    const own = getApp().voiceStates[getApp().me?.id ?? ""];
    return v.canSpeak && !own?.mute;
  }

  private async publishMic() {
    const room = this.room;
    if (!room || !this.micAllowed()) return;
    if (room.localParticipant.getTrackPublication(Track.Source.Microphone)?.track) {
      this.applyMicGate();
      return;
    }
    try {
      await room.localParticipant.setMicrophoneEnabled(true, this.captureOptions(vget().settings));
    } catch (e: any) {
      toast(e?.name === "NotAllowedError" ? appName() + " doesn't have permission to use your microphone. Allow it in your browser and rejoin." : "Couldn't start your microphone: " + (e?.message ?? e));
      vset({ selfMute: true });
      this.sendState();
      return;
    }
    this.applyMicGate();
  }

  // The mic sends audio only when not muted, not deafened, and (in
  // push-to-talk) while the key is held.
  private applyMicGate() {
    const room = this.room;
    if (!room) return;
    const pub = room.localParticipant.getTrackPublication(Track.Source.Microphone);
    const track = pub?.track as LocalAudioTrack | undefined;
    if (!track) return;
    const v = vget();
    const ptt = v.settings.mode === "ptt" || v.mustPTT;
    const open = !v.selfMute && !v.selfDeaf && (!ptt || v.pttDown);
    if (open && track.isMuted) track.unmute();
    if (!open && !track.isMuted) track.mute();
  }

  toggleMute() {
    const v = vget();
    if (v.selfDeaf) {
      // Unmuting while deafened undeafens too, like Discord.
      this.setDeaf(false);
      this.setMute(false);
      return;
    }
    this.setMute(!v.selfMute);
  }

  setMute(m: boolean) {
    vset({ selfMute: m });
    localStorage.setItem("vc.selfMute", m ? "1" : "0");
    playSound(m ? "mute" : "unmute");
    this.applyMicGate();
    this.sendState();
  }

  toggleDeaf() {
    this.setDeaf(!vget().selfDeaf);
  }

  setDeaf(d: boolean) {
    vset({ selfDeaf: d });
    localStorage.setItem("vc.selfDeaf", d ? "1" : "0");
    playSound(d ? "deafen" : "undeafen");
    this.applyMicGate();
    this.applyDeaf();
    this.sendState();
  }

  private applyDeaf() {
    const room = this.room;
    if (!room) return;
    for (const p of room.remoteParticipants.values()) this.subscribeDefaults(p);
  }

  private sendState() {
    const v = vget();
    if (v.status === "idle") return;
    send({ op: "voice_state", self_mute: v.selfMute, self_deaf: v.selfDeaf });
  }

  // Mic audio and webcams are always received; screen shares only while
  // you're watching them, so nobody downloads streams they aren't looking at.
  private subscribeDefaults(p: RemoteParticipant) {
    const v = vget();
    const own = getApp().voiceStates[getApp().me?.id ?? ""];
    const deaf = v.selfDeaf || !!own?.deaf;
    for (const pub of p.trackPublications.values()) {
      const want = this.wants(p.identity, pub.source, deaf);
      if (pub.isSubscribed !== want) pub.setSubscribed(want);
    }
  }

  private wants(uid: string, src: Track.Source, deaf: boolean) {
    const v = vget();
    switch (src) {
      case Track.Source.Microphone:
        return !deaf && !v.localMutes[uid];
      case Track.Source.Camera:
        return true;
      case Track.Source.ScreenShare:
        return !!v.watching[uid];
      case Track.Source.ScreenShareAudio:
        return !!v.watching[uid] && !deaf;
      default:
        return false;
    }
  }

  watch(uid: string, on: boolean) {
    vset((s) => ({ watching: { ...s.watching, [uid]: on }, focus: on ? `${uid}:screen` : s.focus === `${uid}:screen` ? null : s.focus }));
    const p = this.room?.remoteParticipants.get(uid);
    if (p) this.subscribeDefaults(p);
    bump();
  }

  setFocus(f: string | null) {
    vset({ focus: f });
  }

  setUserVolume(uid: string, vol: number, stream = false) {
    const key = stream ? "s:" + uid : uid;
    const volumes = { ...vget().volumes, [key]: vol };
    localStorage.setItem("vc.volumes", JSON.stringify(volumes));
    vset({ volumes });
    this.applyVolumes();
  }

  setLocalMute(uid: string, m: boolean) {
    vset((s) => ({ localMutes: { ...s.localMutes, [uid]: m } }));
    const p = this.room?.remoteParticipants.get(uid);
    if (p) this.subscribeDefaults(p);
  }

  private applyVolumes() {
    const room = this.room;
    if (!room) return;
    const v = vget();
    const out = v.settings.outputVolume;
    const prioritySpeaking = [...this.priority].some((uid) => v.speaking[uid]);
    for (const p of room.remoteParticipants.values()) {
      const duck = prioritySpeaking && !this.priority.has(p.identity) ? 0.3 : 1;
      p.setVolume((v.volumes[p.identity] ?? 1) * out * duck, Track.Source.Microphone);
      p.setVolume((v.volumes["s:" + p.identity] ?? 1) * out, Track.Source.ScreenShareAudio);
    }
  }

  // Priority speakers are people whose role grants it in this channel.
  private refreshPriority() {
    this.priority.clear();
    const v = vget();
    const app = getApp();
    const g = v.guildId ? app.guilds[v.guildId] : null;
    const c = g?.channels.find((c) => c.id === v.channelId);
    if (!g || !c || !this.room) return;
    for (const p of this.room.remoteParticipants.values()) {
      if (has(channelPerms(g, p.identity, c), P.PrioritySpeaker) && p.identity !== g.owner_id) this.priority.add(p.identity);
    }
  }

  async toggleCamera() {
    const room = this.room;
    if (!room) return;
    const on = !vget().camera;
    try {
      await room.localParticipant.setCameraEnabled(on, { resolution: VideoPresets.h720.resolution, deviceId: vget().settings.videoDevice || undefined });
      vset({ camera: on });
    } catch (e: any) {
      toast(e?.name === "NotAllowedError" ? appName() + " doesn't have permission to use your camera." : "Couldn't start your camera: " + (e?.message ?? e));
    }
    bump();
  }

  async toggleShare() {
    const room = this.room;
    if (!room) return;
    if (vget().sharing) {
      await room.localParticipant.setScreenShareEnabled(false);
      vset({ sharing: false });
      playSound("streamStop");
      bump();
      return;
    }
    if (window.victorCharlieDesktop) {
      // The desktop app shows its own window picker first.
      setApp({ modal: { kind: "screenPicker" } });
      return;
    }
    await this.startShare();
  }

  async startShare() {
    const room = this.room;
    if (!room) return;
    const st = vget().settings;
    const size = shareSizes[st.shareRes];
    const capture: ScreenShareCaptureOptions = {
      audio: st.shareAudio ? { echoCancellation: false, noiseSuppression: false, autoGainControl: false } : false,
      resolution: { ...size, frameRate: st.shareFps },
      contentHint: st.shareFps >= 60 ? "motion" : "detail",
      selfBrowserSurface: "exclude",
      surfaceSwitching: "include",
      systemAudio: "include",
      suppressLocalAudioPlayback: true,
      preferCurrentTab: false,
    };
    const publish: TrackPublishOptions = {
      videoCodec: st.shareCodec,
      screenShareEncoding: { maxBitrate: shareBitrate(st.shareRes, st.shareFps), maxFramerate: st.shareFps },
      screenShareSimulcastLayers: [{ width: 1280, height: 720, resolution: { width: 1280, height: 720, frameRate: Math.min(st.shareFps, 30) }, encoding: { maxBitrate: 1_200_000, maxFramerate: Math.min(st.shareFps, 30) } } as any],
      degradationPreference: st.shareFps >= 60 ? "maintain-framerate" : "maintain-resolution",
    };
    try {
      await room.localParticipant.setScreenShareEnabled(true, capture, publish);
      vset({ sharing: true });
      playSound("streamStart");
    } catch (e: any) {
      if (e?.name !== "NotAllowedError" && e?.name !== "AbortError") toast("Couldn't share your screen: " + (e?.message ?? e));
    }
    bump();
  }

  applySettings(patch: Partial<VoiceSettings>) {
    const room = this.room;
    const st = vget().settings;
    if (!room) return;
    if (patch.inputDevice !== undefined) room.switchActiveDevice("audioinput", st.inputDevice).catch(() => {});
    if (patch.outputDevice !== undefined) room.switchActiveDevice("audiooutput", st.outputDevice).catch(() => {});
    if (patch.videoDevice !== undefined && vget().camera) room.switchActiveDevice("videoinput", st.videoDevice).catch(() => {});
    if (patch.noiseSuppression !== undefined || patch.echoCancellation !== undefined || patch.autoGain !== undefined || patch.stereo !== undefined) {
      const track = room.localParticipant.getTrackPublication(Track.Source.Microphone)?.track as LocalAudioTrack | undefined;
      track?.restartTrack(this.captureOptions(st)).then(() => this.applyMicGate(), () => {});
    }
    if (patch.outputVolume !== undefined) this.applyVolumes();
    if (patch.mode !== undefined) this.applyMicGate();
  }

  // --- push-to-talk and hotkeys ---

  setPTT(down: boolean) {
    const v = vget();
    if (v.settings.mode !== "ptt" && !v.mustPTT) return;
    clearTimeout(this.pttTimer);
    if (down) {
      if (!v.pttDown) {
        vset({ pttDown: true });
        this.applyMicGate();
      }
    } else {
      this.pttTimer = window.setTimeout(() => {
        vset({ pttDown: false });
        this.applyMicGate();
      }, v.settings.pttRelease);
    }
  }

  private bindKeys() {
    const typing = (e: Event) => {
      const t = e.target as HTMLElement | null;
      return !!t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);
    };
    window.addEventListener("keydown", (e) => {
      const st = vget().settings;
      if (window.victorCharlieDesktop) return; // the desktop app sends global keys
      if (e.code === st.pttKey && !typing(e) && !e.repeat) this.setPTT(true);
      if (vget().status === "idle") return;
      if (st.muteKey && comboOf(e) === st.muteKey) {
        e.preventDefault();
        this.toggleMute();
      }
      if (st.deafenKey && comboOf(e) === st.deafenKey) {
        e.preventDefault();
        this.toggleDeaf();
      }
    });
    window.addEventListener("keyup", (e) => {
      if (window.victorCharlieDesktop) return;
      if (e.code === vget().settings.pttKey) this.setPTT(false);
    });
    window.addEventListener("blur", () => !window.victorCharlieDesktop && this.setPTT(false));
    const mouse = (down: boolean) => (e: MouseEvent) => {
      if (window.victorCharlieDesktop) return;
      if (vget().settings.pttKey === `Mouse${e.button}` && e.button > 2) this.setPTT(down);
    };
    window.addEventListener("mousedown", mouse(true));
    window.addEventListener("mouseup", mouse(false));
    const d = window.victorCharlieDesktop;
    if (d) {
      d.onPTT((down) => this.setPTT(down));
      d.onToggle((what) => (what === "mute" ? this.toggleMute() : this.toggleDeaf()));
      const st = vget().settings;
      d.setPTTKey(st.mode === "ptt" ? st.pttKey : null);
      d.setToggleKeys({ mute: st.muteKey || null, deafen: st.deafenKey || null });
      useVoice.subscribe((s, prev) => {
        if (s.settings === prev.settings && s.mustPTT === prev.mustPTT) return;
        d.setPTTKey(s.settings.mode === "ptt" || s.mustPTT ? s.settings.pttKey : null);
        d.setToggleKeys({ mute: s.settings.muteKey || null, deafen: s.settings.deafenKey || null });
      });
    }
  }

  // --- server events ---

  resync() {
    this.sendState();
  }

  onGuildChanged(gid: ID) {
    if (vget().guildId === gid) {
      this.refreshPriority();
      bump();
    }
  }

  onOwnState(v: VoiceState) {
    const prev = this.lastOwn;
    this.lastOwn = v;
    if (!this.room || v.channel_id !== vget().channelId) return;
    if (prev && prev.mute && !v.mute) this.publishMic();
    if (prev && prev.deaf && !v.deaf) this.applyDeaf();
    if (prev && !prev.mute && v.mute) toast("A moderator server-muted you.");
    if (prev && !prev.deaf && v.deaf) toast("A moderator server-deafened you.");
  }
  private lastOwn: VoiceState | null = null;

  private wire(room: Room) {
    const isMe = () => this.room === room;
    room
      .on(RoomEvent.ParticipantConnected, (p) => {
        this.subscribeDefaults(p);
        this.refreshPriority();
        bump();
      })
      .on(RoomEvent.ParticipantDisconnected, () => {
        this.refreshPriority();
        bump();
      })
      .on(RoomEvent.TrackPublished, (pub, p) => {
        this.subscribeDefaults(p);
        if (pub.source === Track.Source.ScreenShare) bump();
      })
      .on(RoomEvent.TrackUnpublished, (pub, p) => {
        if (pub.source === Track.Source.ScreenShare) {
          vset((s) => ({ watching: { ...s.watching, [p.identity]: false }, focus: s.focus === `${p.identity}:screen` ? null : s.focus }));
        }
        bump();
      })
      .on(RoomEvent.TrackSubscribed, (track: RemoteTrack, pub: RemoteTrackPublication, p: RemoteParticipant) => {
        if (track.kind === Track.Kind.Audio) {
          const el = track.attach();
          el.dataset.sid = pub.trackSid;
          this.audioBox.appendChild(el);
          this.audioEls.set(pub.trackSid, el);
          const out = vget().settings.outputDevice;
          if (out && out !== "default" && "setSinkId" in el) (el as any).setSinkId(out).catch(() => {});
          this.applyVolumes();
        }
        void p;
        bump();
      })
      .on(RoomEvent.TrackUnsubscribed, (track: RemoteTrack, pub: RemoteTrackPublication) => {
        track.detach().forEach((el) => el.remove());
        this.audioEls.get(pub.trackSid)?.remove();
        this.audioEls.delete(pub.trackSid);
        bump();
      })
      .on(RoomEvent.TrackMuted, bump)
      .on(RoomEvent.TrackUnmuted, bump)
      .on(RoomEvent.LocalTrackPublished, bump)
      .on(RoomEvent.LocalTrackUnpublished, (pub) => {
        if (pub.source === Track.Source.ScreenShare && vget().sharing) {
          // Stopped from the browser's own "Stop sharing" bar.
          vset({ sharing: false });
          playSound("streamStop");
        }
        if (pub.source === Track.Source.Camera) vset({ camera: false });
        bump();
      })
      .on(RoomEvent.ActiveSpeakersChanged, (speakers: Participant[]) => {
        const speaking: Record<string, boolean> = {};
        for (const s of speakers) speaking[s.identity] = true;
        vset({ speaking });
        if (this.priority.size) this.applyVolumes();
      })
      .on(RoomEvent.ConnectionQualityChanged, (q: ConnectionQuality, p: Participant) => {
        vset((s) => ({ quality: { ...s.quality, [p.identity]: q } }));
      })
      .on(RoomEvent.ParticipantPermissionsChanged, (_prev, p) => {
        if (p instanceof LocalParticipant) bump();
      })
      .on(RoomEvent.Reconnecting, () => isMe() && vset({ status: "reconnecting" }))
      .on(RoomEvent.Reconnected, () => isMe() && vset({ status: "connected" }))
      .on(RoomEvent.Disconnected, (reason?: DisconnectReason) => {
        if (!isMe() || this.leaving) return;
        this.room = null;
        for (const el of this.audioEls.values()) el.remove();
        this.audioEls.clear();
        window.VictorCharlieAndroid?.voice?.(false, "");
        vset({ channelId: null, guildId: null, status: "idle", sharing: false, camera: false, watching: {}, focus: null, speaking: {} });
        playSound("disconnect");
        if (reason === DisconnectReason.PARTICIPANT_REMOVED) toast("You were disconnected from voice.");
        else if (reason === DisconnectReason.DUPLICATE_IDENTITY) toast("You joined voice from another device.");
        else if (reason !== DisconnectReason.CLIENT_INITIATED) toast("Lost the voice connection.");
        bump();
      })
      .on(RoomEvent.AudioPlaybackStatusChanged, () => {
        if (!room.canPlaybackAudio) toast("Click anywhere to allow audio playback.");
      });
    // Round-trip time for the connection badge.
    const t = window.setInterval(async () => {
      if (!isMe()) return clearInterval(t);
      try {
        const pc = (room.engine as any)?.pcManager?.subscriber?.pc as RTCPeerConnection | undefined;
        const stats = await pc?.getStats();
        stats?.forEach((r: any) => {
          if (r.type === "candidate-pair" && r.nominated && r.currentRoundTripTime !== undefined) vset({ ping: Math.round(r.currentRoundTripTime * 1000) });
        });
      } catch {
        /* stats are best-effort */
      }
    }, 3000);
  }
}

export function comboOf(e: KeyboardEvent): string {
  const parts = [];
  if (e.ctrlKey) parts.push("Ctrl");
  if (e.altKey) parts.push("Alt");
  if (e.shiftKey) parts.push("Shift");
  if (e.metaKey) parts.push("Meta");
  if (!["ControlLeft", "ControlRight", "AltLeft", "AltRight", "ShiftLeft", "ShiftRight", "MetaLeft", "MetaRight"].includes(e.code)) parts.push(e.code);
  return parts.join("+");
}

export function keyLabel(code: string): string {
  if (!code) return "Not set";
  return code
    .split("+")
    .map((c) =>
      c
        .replace(/^Key/, "")
        .replace(/^Digit/, "")
        .replace(/^Numpad/, "Num ")
        .replace("Backquote", "`")
        .replace(/^Mouse(\d)$/, (_, n) => `Mouse ${Number(n) + 1}`),
    )
    .join(" + ");
}

export const voice = new Voice();

// For tiles: everyone in the room including you, with their tracks.
export function roomParticipants(): Participant[] {
  const room = voice.room;
  if (!room) return [];
  return [room.localParticipant, ...room.remoteParticipants.values()];
}

window.addEventListener("beforeunload", () => {
  voice.room?.disconnect();
});
