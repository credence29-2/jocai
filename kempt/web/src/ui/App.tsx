import { useEffect, useState } from "react";
import { api } from "../api";
import { appName, setAppName } from "../brand";
import { connectGateway } from "../gateway";
import { applyLocation, inviteCodeFromURL, navigate } from "../router";
import { get, set, useStore } from "../store";
import { updateBadge } from "../notify";
import { Auth } from "./Auth";
import { GuildBar } from "./GuildBar";
import { Sidebar } from "./Sidebar";
import { Main } from "./Main";
import { Modals } from "./Modals";
import { ContextMenu, Toast } from "./common";
import { IncomingCall } from "./IncomingCall";

type Boot = { state: "loading" } | { state: "auth"; setup: boolean; openRegistration: boolean } | { state: "app" } | { state: "down" };

export function App() {
  const [boot, setBoot] = useState<Boot>({ state: "loading" });
  const invite = inviteCodeFromURL();

  const start = async () => {
    try {
      const info = await api.get<{ name: string; setup: boolean; open_registration: boolean }>("/info");
      setAppName(info.name);
      try {
        await api.get("/users/@me");
      } catch {
        setBoot({ state: "auth", setup: info.setup, openRegistration: info.open_registration });
        return;
      }
      if (invite) {
        // Already logged in: join and go there.
        try {
          const r = await api.post<{ guild_id: string }>(`/invites/${invite}`);
          localStorage.setItem("kempt.lastGuild", r.guild_id);
        } catch {
          /* bad invite; just open the app */
        }
        history.replaceState(null, "", "/");
      }
      setBoot({ state: "app" });
      connectGateway();
    } catch {
      setBoot({ state: "down" });
    }
  };

  useEffect(() => {
    start();
  }, []);

  if (boot.state === "loading") return <Splash text="Loading…" />;
  if (boot.state === "down") return <Splash text={`Can't reach the ${appName()} server. Is it running?`} retry={start} />;
  if (boot.state === "auth")
    return (
      <Auth
        setup={boot.setup}
        invite={invite}
        openRegistration={boot.openRegistration}
        onDone={() => {
          if (invite) history.replaceState(null, "", "/");
          setBoot({ state: "app" });
          connectGateway();
        }}
      />
    );
  return <Shell />;
}

function Splash({ text, retry }: { text: string; retry?: () => void }) {
  return (
    <div className="splash">
      <img src="/favicon.svg" width={64} height={64} alt="" />
      <div>{text}</div>
      {retry && (
        <button className="btn primary" onClick={retry}>
          Try again
        </button>
      )}
    </div>
  );
}

function Shell() {
  const ready = useStore((s) => s.ready);
  const connection = useStore((s) => s.connection);
  const pane = useStore((s) => s.mobilePane);
  const readStates = useStore((s) => s.readStates);

  useEffect(() => {
    if (ready && !get().channelByGuild[get().guildId]) applyLocation();
  }, [ready]);
  useEffect(updateBadge, [readStates]);

  if (!ready) return <Splash text="Connecting…" />;
  return (
    <div className={"app pane-" + pane}>
      {connection !== "ready" && <div className="conn-banner">Reconnecting to {appName()}…</div>}
      <GuildBar />
      <Sidebar />
      <Main />
      <Modals />
      <ContextMenu />
      <IncomingCall />
      <Toast />
    </div>
  );
}

// Re-exported for components that switch views.
export { navigate, set };
