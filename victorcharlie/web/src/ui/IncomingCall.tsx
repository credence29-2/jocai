import { useEffect } from "react";
import { navigate } from "../router";
import { displayName, set, useStore } from "../store";
import { voice } from "../voice";
import { playSound } from "../sounds";
import { Avatar } from "./common";
import { Icon } from "./Icon";

export function IncomingCall() {
  const call = useStore((s) => s.incomingCall);
  const user = useStore((s) => (call ? s.users[call.user_id] : undefined));
  useEffect(() => {
    if (!call) return;
    const t = setInterval(() => playSound("ring"), 2500);
    // Stop ringing after a while if nobody answers.
    const stop = setTimeout(() => set({ incomingCall: null }), 45000);
    return () => {
      clearInterval(t);
      clearTimeout(stop);
    };
  }, [call?.channel_id]);
  if (!call) return null;
  return (
    <div className="incoming-call">
      <Avatar user={user} size={64} />
      <div className="strong">{displayName(call.user_id)}</div>
      <div className="muted small">Incoming call…</div>
      <div className="call-buttons">
        <button className="round-btn hangup" title="Ignore" onClick={() => set({ incomingCall: null })}>
          <Icon name="close" />
        </button>
        <button
          className="round-btn accept"
          title="Join call"
          onClick={() => {
            set({ incomingCall: null });
            voice.join(call.channel_id);
            navigate("@me", call.channel_id);
          }}
        >
          <Icon name="phone" />
        </button>
      </div>
    </div>
  );
}
