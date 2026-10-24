import { useEffect, useRef, useState } from "react";
import { api, upload } from "../api";
import { appName } from "../brand";
import { disconnectGateway } from "../gateway";
import { askNotificationPermission, notificationsEnabled } from "../notify";
import { playSound } from "../sounds";
import { toast, useStore } from "../store";
import type { User } from "../types";
import { comboOf, keyLabel, saveSettings, useVoice, voice } from "../voice";
import { Avatar, Field, hexColor, Toggle } from "./common";
import { SettingsShell } from "./GuildSettings";

export function UserSettings({ tab: initial }: { tab?: string }) {
  const me = useStore((s) => s.me)!;
  const [tab, setTab] = useState(initial ?? "account");
  const tabs = [
    { id: "account", label: "My account" },
    { id: "profile", label: "Profile" },
    { id: "voice", label: "Voice & video" },
    { id: "keys", label: "Keybinds" },
    { id: "notifications", label: "Notifications" },
    ...(me.is_admin ? [{ id: "admin", label: "Server admin" }] : []),
  ];
  return (
    <SettingsShell
      title="Settings"
      tabs={tabs}
      tab={tab}
      setTab={setTab}
      danger={{
        label: "Log out",
        onClick: async () => {
          await voice.leave();
          disconnectGateway();
          await api.post("/auth/logout").catch(() => {});
          location.href = "/";
        },
      }}
    >
      {tab === "account" && <Account me={me} />}
      {tab === "profile" && <Profile me={me} />}
      {tab === "voice" && <VoiceTab />}
      {tab === "keys" && <Keys />}
      {tab === "notifications" && <Notifications />}
      {tab === "admin" && <Admin />}
    </SettingsShell>
  );
}

function Account({ me }: { me: User }) {
  const [username, setUsername] = useState(me.username);
  const [cur, setCur] = useState("");
  const [next, setNext] = useState("");
  return (
    <>
      <h2>My account</h2>
      <Field label="Username" hint="Used to log in. Letters, numbers, dots and underscores.">
        <div className="copy-row">
          <input value={username} onChange={(e) => setUsername(e.target.value)} maxLength={32} />
          <button className="btn" disabled={username === me.username} onClick={() => api.patch("/users/@me", { username }).then(() => toast("Username changed."), (e) => toast(e.message))}>
            Save
          </button>
        </div>
      </Field>
      {window.VictorCharlieAndroid?.changeServer && (
        <>
          <h3 className="mt">Server</h3>
          <p className="muted small">Connected to {location.origin}</p>
          <button className="btn" onClick={() => window.VictorCharlieAndroid?.changeServer?.()}>
            Connect to a different server
          </button>
        </>
      )}
      <h3 className="mt">Change password</h3>
      <Field label="Current password">
        <input type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} />
      </Field>
      <Field label="New password" hint="At least 8 characters. Your other devices get logged out.">
        <input type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
      </Field>
      <button
        className="btn primary"
        disabled={!cur || next.length < 8}
        onClick={() =>
          api.post("/users/@me/password", { current: cur, new: next }).then(
            () => {
              toast("Password changed.");
              setCur("");
              setNext("");
            },
            (e) => toast(e.message),
          )
        }
      >
        Change password
      </button>
    </>
  );
}

function Profile({ me }: { me: User }) {
  const [name, setName] = useState(me.display_name);
  const [about, setAbout] = useState(me.about);
  const [status, setStatus] = useState(me.custom_status);
  const [accent, setAccent] = useState(me.accent);
  const save = () => api.patch("/users/@me", { display_name: name, about, custom_status: status, accent }).then(() => toast("Profile saved."), (e) => toast(e.message));
  const avatar = (f: File) => {
    const form = new FormData();
    form.append("file", f);
    upload("/users/@me/avatar", form).catch((e) => toast(e.message));
  };
  return (
    <>
      <h2>Profile</h2>
      <div className="row gap-lg">
        <label className="icon-upload round" title="Change avatar">
          <Avatar user={me} size={96} />
          <span>Change</span>
          <input type="file" accept="image/*" hidden onChange={(e) => e.target.files?.[0] && avatar(e.target.files[0])} />
        </label>
        <div className="grow">
          <Field label="Display name">
            <input value={name} onChange={(e) => setName(e.target.value)} maxLength={32} />
          </Field>
          <Field label="Custom status">
            <input value={status} onChange={(e) => setStatus(e.target.value)} maxLength={64} placeholder="What are you up to?" />
          </Field>
        </div>
      </div>
      <Field label="About me" hint="Markdown works here.">
        <textarea value={about} onChange={(e) => setAbout(e.target.value)} maxLength={190} rows={3} />
      </Field>
      <Field label="Banner color">
        <input type="color" value={hexColor(accent) ?? "#3d7f6e"} onChange={(e) => setAccent(parseInt(e.target.value.slice(1), 16))} />
      </Field>
      <button className="btn primary" onClick={save}>
        Save profile
      </button>
    </>
  );
}

function useDevices() {
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([]);
  useEffect(() => {
    const load = () => navigator.mediaDevices?.enumerateDevices().then(setDevices).catch(() => {});
    load();
    navigator.mediaDevices?.addEventListener("devicechange", load);
    return () => navigator.mediaDevices?.removeEventListener("devicechange", load);
  }, []);
  return devices;
}

function VoiceTab() {
  const st = useVoice((s) => s.settings);
  const devices = useDevices();
  const [listening, setListening] = useState(false);
  const inputs = devices.filter((d) => d.kind === "audioinput");
  const outputs = devices.filter((d) => d.kind === "audiooutput");
  const cams = devices.filter((d) => d.kind === "videoinput");
  const unlabeled = devices.length > 0 && devices.every((d) => !d.label);
  return (
    <>
      <h2>Voice & video</h2>
      {unlabeled && (
        <div className="notice">
          Your browser hides device names until you allow the microphone.{" "}
          <a onClick={() => navigator.mediaDevices.getUserMedia({ audio: true }).then((s) => (s.getTracks().forEach((t) => t.stop()), location.reload()), () => toast("Microphone access was blocked."))}>Allow it</a>.
        </div>
      )}
      <div className="row gap">
        <Field label="Microphone">
          <select value={st.inputDevice} onChange={(e) => saveSettings({ inputDevice: e.target.value })}>
            <option value="default">Default</option>
            {inputs
              .filter((d) => d.deviceId !== "default")
              .map((d) => (
                <option key={d.deviceId} value={d.deviceId}>
                  {d.label || "Microphone"}
                </option>
              ))}
          </select>
        </Field>
        <Field label="Speakers">
          <select value={st.outputDevice} onChange={(e) => saveSettings({ outputDevice: e.target.value })} disabled={!outputs.length}>
            <option value="default">Default</option>
            {outputs
              .filter((d) => d.deviceId !== "default")
              .map((d) => (
                <option key={d.deviceId} value={d.deviceId}>
                  {d.label || "Speakers"}
                </option>
              ))}
          </select>
        </Field>
      </div>
      <Field label={`Output volume: ${Math.round(st.outputVolume * 100)}%`}>
        <input type="range" min={0} max={200} value={Math.round(st.outputVolume * 100)} onChange={(e) => saveSettings({ outputVolume: Number(e.target.value) / 100 })} />
      </Field>
      <div className="mic-test">
        <button className="btn" onClick={() => setListening(!listening)}>
          {listening ? "Stop test" : "Test microphone"}
        </button>
        {listening ? <MicMeter device={st.inputDevice} /> : <span className="muted small">Talk and watch the bar move. Uses your current settings.</span>}
      </div>

      <h3 className="mt">Input mode</h3>
      <div className="type-pick">
        <label className={st.mode === "vad" ? "on" : ""}>
          <input type="radio" checked={st.mode === "vad"} onChange={() => saveSettings({ mode: "vad" })} />
          <div>
            <div className="strong">Voice activity</div>
            <div className="muted small">Open mic. Sends when you talk.</div>
          </div>
        </label>
        <label className={st.mode === "ptt" ? "on" : ""}>
          <input type="radio" checked={st.mode === "ptt"} onChange={() => saveSettings({ mode: "ptt" })} />
          <div>
            <div className="strong">Push to talk</div>
            <div className="muted small">Hold a key to talk.</div>
          </div>
        </label>
      </div>
      {st.mode === "ptt" && (
        <>
          <Field label="Push-to-talk key" hint={window.victorCharlieDesktop ? `Works even while ${appName()} is in the background.` : `In a browser this only works while the ${appName()} tab is focused. The Windows app makes it work everywhere.`}>
            <KeyCapture value={st.pttKey} onChange={(k) => saveSettings({ pttKey: k })} single />
          </Field>
          <Field label={`Release delay: ${st.pttRelease} ms`} hint="Keeps the mic open briefly after you let go so the end of your sentence isn't cut off.">
            <input type="range" min={0} max={1000} step={20} value={st.pttRelease} onChange={(e) => saveSettings({ pttRelease: Number(e.target.value) })} />
          </Field>
        </>
      )}

      <h3 className="mt">Audio processing</h3>
      <ToggleRow label="Noise suppression" desc="Cuts keyboard clatter, fans and background noise." on={st.noiseSuppression} set={(v) => saveSettings({ noiseSuppression: v })} />
      <ToggleRow label="Echo cancellation" desc="Stops others hearing themselves through your speakers. Leave on unless you use headphones and want raw audio." on={st.echoCancellation} set={(v) => saveSettings({ echoCancellation: v })} />
      <ToggleRow label="Automatic gain" desc="Evens out your volume when you lean in or away." on={st.autoGain} set={(v) => saveSettings({ autoGain: v })} />
      <ToggleRow label="Stereo" desc="Send stereo audio. Useful for music with a stereo mic or interface. Turn off the three options above for the purest sound." on={st.stereo} set={(v) => saveSettings({ stereo: v })} />

      <h3 className="mt">Camera</h3>
      <Field label="Camera">
        <select value={st.videoDevice} onChange={(e) => saveSettings({ videoDevice: e.target.value })}>
          <option value="">Default</option>
          {cams.map((d) => (
            <option key={d.deviceId} value={d.deviceId}>
              {d.label || "Camera"}
            </option>
          ))}
        </select>
      </Field>

      <h3 className="mt">Screen share</h3>
      <div className="row gap">
        <Field label="Resolution">
          <select value={st.shareRes} onChange={(e) => saveSettings({ shareRes: e.target.value as any })}>
            <option value="720">720p</option>
            <option value="1080">1080p</option>
            <option value="1440">1440p</option>
            <option value="source">Source</option>
          </select>
        </Field>
        <Field label="Frame rate">
          <select value={st.shareFps} onChange={(e) => saveSettings({ shareFps: Number(e.target.value) as any })}>
            <option value={15}>15 fps</option>
            <option value={30}>30 fps</option>
            <option value={60}>60 fps</option>
          </select>
        </Field>
        <Field label="Codec">
          <select value={st.shareCodec} onChange={(e) => saveSettings({ shareCodec: e.target.value as any })}>
            <option value="vp8">VP8 (works everywhere)</option>
            <option value="h264">H.264 (uses your GPU)</option>
            <option value="vp9">VP9 (sharper text)</option>
            <option value="av1">AV1 (best quality, newest PCs)</option>
          </select>
        </Field>
      </div>
      <ToggleRow label="Share audio with your screen" desc="Sends the sound from the tab, window or whole system, where your browser allows it." on={st.shareAudio} set={(v) => saveSettings({ shareAudio: v })} />
      <p className="muted small">Higher settings need more upload speed from you and from the {appName()} server. 1080p at 30 fps uses about 3 Mbps per viewer from the server.</p>
    </>
  );
}

function ToggleRow({ label, desc, on, set }: { label: string; desc: string; on: boolean; set: (v: boolean) => void }) {
  return (
    <div className="toggle-row">
      <div>
        <div className="strong">{label}</div>
        <div className="muted small">{desc}</div>
      </div>
      <Toggle on={on} onChange={set} />
    </div>
  );
}

function MicMeter({ device }: { device: string }) {
  const bar = useRef<HTMLDivElement>(null);
  const st = useVoice((s) => s.settings);
  useEffect(() => {
    let stop = false;
    let stream: MediaStream | null = null;
    let ctx: AudioContext | null = null;
    navigator.mediaDevices
      .getUserMedia({ audio: { deviceId: device === "default" ? undefined : device, noiseSuppression: st.noiseSuppression, echoCancellation: st.echoCancellation, autoGainControl: st.autoGain } })
      .then((s) => {
        if (stop) return s.getTracks().forEach((t) => t.stop());
        stream = s;
        ctx = new AudioContext();
        const an = ctx.createAnalyser();
        an.fftSize = 512;
        ctx.createMediaStreamSource(s).connect(an);
        const buf = new Float32Array(an.fftSize);
        const tick = () => {
          if (stop) return;
          an.getFloatTimeDomainData(buf);
          let peak = 0;
          for (const x of buf) peak = Math.max(peak, Math.abs(x));
          if (bar.current) bar.current.style.width = Math.min(100, Math.sqrt(peak) * 140) + "%";
          requestAnimationFrame(tick);
        };
        tick();
      })
      .catch(() => toast("Couldn't open your microphone."));
    return () => {
      stop = true;
      stream?.getTracks().forEach((t) => t.stop());
      ctx?.close();
    };
  }, [device, st.noiseSuppression, st.echoCancellation, st.autoGain]);
  return (
    <div className="meter">
      <div ref={bar} />
    </div>
  );
}

function KeyCapture({ value, onChange, single }: { value: string; onChange: (k: string) => void; single?: boolean }) {
  const [rec, setRec] = useState(false);
  useEffect(() => {
    if (!rec) return;
    const key = (e: KeyboardEvent) => {
      e.preventDefault();
      if (e.key === "Escape") return setRec(false);
      if (single) {
        onChange(e.code);
        setRec(false);
        return;
      }
      const c = comboOf(e);
      if (c && !["Ctrl", "Alt", "Shift", "Meta"].includes(c)) {
        onChange(c);
        setRec(false);
      }
    };
    const mouse = (e: MouseEvent) => {
      if (single && e.button > 2) {
        e.preventDefault();
        onChange(`Mouse${e.button}`);
        setRec(false);
      }
    };
    window.addEventListener("keydown", key, true);
    window.addEventListener("mousedown", mouse, true);
    return () => {
      window.removeEventListener("keydown", key, true);
      window.removeEventListener("mousedown", mouse, true);
    };
  }, [rec]);
  return (
    <div className="copy-row">
      <button className={"key-capture" + (rec ? " rec" : "")} onClick={() => setRec(!rec)}>
        {rec ? "Press a key…" : keyLabel(value)}
      </button>
      {!single && value && (
        <button className="btn small" onClick={() => onChange("")}>
          Clear
        </button>
      )}
    </div>
  );
}

function Keys() {
  const st = useVoice((s) => s.settings);
  return (
    <>
      <h2>Keybinds</h2>
      <p className="muted">{window.victorCharlieDesktop ? `These work anywhere on your PC while the ${appName()} app is running.` : `In the browser these work while ${appName()} is focused. Install the Windows app for keys that work in games.`}</p>
      <Field label="Toggle mute">
        <KeyCapture value={st.muteKey} onChange={(k) => saveSettings({ muteKey: k })} />
      </Field>
      <Field label="Toggle deafen">
        <KeyCapture value={st.deafenKey} onChange={(k) => saveSettings({ deafenKey: k })} />
      </Field>
      <Field label="Push to talk">
        <KeyCapture value={st.pttKey} onChange={(k) => saveSettings({ pttKey: k })} single />
      </Field>
    </>
  );
}

function Notifications() {
  const [on, setOn] = useState(notificationsEnabled());
  const [sounds, setSounds] = useState(localStorage.getItem("vc.sounds") !== "0");
  const blocked = "Notification" in window && Notification.permission === "denied";
  return (
    <>
      <h2>Notifications</h2>
      <ToggleRow
        label="Desktop notifications"
        desc={`For DMs and mentions while ${appName()} isn't focused.`}
        on={on}
        set={(v) => {
          setOn(v);
          localStorage.setItem("vc.notifications", v ? "1" : "0");
          if (v) askNotificationPermission();
        }}
      />
      {blocked && <div className="notice">Your browser is blocking notifications for this site. Allow them in the site settings (the lock icon next to the address).</div>}
      <ToggleRow
        label="Sounds"
        desc="Join, leave, mute and message sounds."
        on={sounds}
        set={(v) => {
          setSounds(v);
          localStorage.setItem("vc.sounds", v ? "1" : "0");
          if (v) playSound("message");
        }}
      />
      <p className="muted small">Set your status to Do Not Disturb to silence everything for a while.</p>
    </>
  );
}

function Admin() {
  const [settings, setSettings] = useState<{ open_registration: boolean; max_upload_mb: number } | null>(null);
  const [users, setUsers] = useState<(User & { disabled: boolean; online: boolean })[]>([]);
  const me = useStore((s) => s.me)!;
  const load = () => {
    api.get<typeof settings>("/admin/settings").then(setSettings, (e) => toast(e.message));
    api.get<typeof users>("/admin/users").then(setUsers, (e) => toast(e.message));
  };
  useEffect(load, []);
  const patch = (body: object) => api.patch("/admin/settings", body).then((s: any) => setSettings(s), (e) => toast(e.message));
  return (
    <>
      <h2>Server admin</h2>
      <p className="muted">Settings for this whole {appName()} install, not one server. You're the admin because you made the first account.</p>
      {settings && (
        <>
          <ToggleRow label="Anyone can register" desc="Off: new people need an invite link. On: anyone who can reach the site can make an account." on={settings.open_registration} set={(v) => patch({ open_registration: v })} />
          <Field label="Upload limit per file (MB)">
            <input type="number" min={1} max={2048} defaultValue={settings.max_upload_mb} onBlur={(e) => patch({ max_upload_mb: Number(e.target.value) })} />
          </Field>
        </>
      )}
      <h3 className="mt">Accounts ({users.length})</h3>
      <div className="member-table">
        {users
          .sort((a, b) => a.username.localeCompare(b.username))
          .map((u) => (
            <div key={u.id} className={"person-row" + (u.disabled ? " offline" : "")}>
              <Avatar user={u} size={32} status={u.online ? "online" : "offline"} />
              <div className="grow">
                <div className="strong">
                  {u.display_name} {u.is_admin && <span className="tag">admin</span>} {u.disabled && <span className="tag danger">disabled</span>}
                </div>
                <div className="muted small">{u.username}</div>
              </div>
              <button
                className="btn small"
                onClick={() => {
                  const pw = prompt(`New password for ${u.username} (at least 8 characters). They'll be logged out everywhere.`);
                  if (pw) api.patch(`/admin/users/${u.id}`, { password: pw }).then(() => toast("Password reset."), (e) => toast(e.message));
                }}
              >
                Reset password
              </button>
              {u.id !== me.id && (
                <button className={"btn small" + (u.disabled ? "" : " danger")} onClick={() => api.patch(`/admin/users/${u.id}`, { disabled: !u.disabled }).then(load, (e) => toast(e.message))}>
                  {u.disabled ? "Enable" : "Disable"}
                </button>
              )}
            </div>
          ))}
      </div>
    </>
  );
}
