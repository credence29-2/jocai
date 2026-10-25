import { useEffect, useState } from "react";
import { api } from "../api";
import { appName } from "../brand";
import { GuildIcon, PasswordInput } from "./common";

interface InviteInfo {
  code: string;
  guild: { id: string; name: string; icon: string };
  members: number;
  inviter: string;
}

export function Auth({ setup, invite, openRegistration, onDone }: { setup: boolean; invite: string | null; openRegistration: boolean; onDone: () => void }) {
  const [mode, setMode] = useState<"login" | "register">(setup || invite ? "register" : "login");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [inv, setInv] = useState<InviteInfo | null>(null);

  useEffect(() => {
    if (invite) api.get<InviteInfo>(`/invites/${invite}`).then(setInv, () => setError("That invite link is expired or invalid."));
  }, [invite]);

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError("");
    if (mode === "register" && password !== confirm) {
      setError("The passwords don't match.");
      return;
    }
    setBusy(true);
    try {
      if (mode === "register") await api.post("/auth/register", { username, password, invite: invite ?? "" });
      else {
        await api.post("/auth/login", { username, password });
        if (invite) await api.post(`/invites/${invite}`).catch(() => {});
      }
      onDone();
    } catch (err: any) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };

  const canRegister = setup || !!invite || openRegistration;

  return (
    <div className="auth-screen">
      <form className="auth-card" onSubmit={submit}>
        {inv ? (
          <div className="auth-invite">
            <GuildIcon name={inv.guild.name} icon={inv.guild.icon} size={64} />
            <div className="muted">{inv.inviter ? `${inv.inviter} invited you to join` : "You've been invited to join"}</div>
            <h1>{inv.guild.name}</h1>
            <div className="muted small">{inv.members} {inv.members === 1 ? "member" : "members"}</div>
          </div>
        ) : setup ? (
          <>
            <h1>Set up {appName()}</h1>
            <p className="muted">Make the first account. It becomes the admin for this server, and you'll start with a server called Home that you can rename.</p>
          </>
        ) : (
          <>
            <h1>{mode === "login" ? "Welcome back" : "Create an account"}</h1>
            <p className="muted">{mode === "login" ? `Log in to ${appName()}.` : "Pick a username and password."}</p>
          </>
        )}
        <label className="field">
          <span className="field-label">Username</span>
          <input autoFocus autoComplete="username" value={username} onChange={(e) => setUsername(e.target.value)} maxLength={32} required />
        </label>
        <label className="field">
          <span className="field-label">Password</span>
          <PasswordInput autoComplete={mode === "login" ? "current-password" : "new-password"} value={password} onChange={(e) => setPassword(e.target.value)} required minLength={mode === "register" ? 8 : 1} />
        </label>
        {mode === "register" && (
          <label className="field">
            <span className="field-label">Confirm password</span>
            <PasswordInput autoComplete="new-password" value={confirm} onChange={(e) => setConfirm(e.target.value)} required />
          </label>
        )}
        {error && <div className="error">{error}</div>}
        <button className="btn primary block" disabled={busy}>
          {busy ? "One sec…" : mode === "login" ? (invite ? "Log in and join" : "Log in") : invite ? "Create account and join" : setup ? "Create admin account" : "Create account"}
        </button>
        {!setup && (
          <div className="auth-switch">
            {mode === "login" ? (
              canRegister ? (
                <>
                  Need an account? <a onClick={() => setMode("register")}>Register</a>
                </>
              ) : (
                <span className="muted">New here? Ask someone for an invite link.</span>
              )
            ) : (
              <>
                Already have an account? <a onClick={() => setMode("login")}>Log in</a>
              </>
            )}
          </div>
        )}
      </form>
    </div>
  );
}
