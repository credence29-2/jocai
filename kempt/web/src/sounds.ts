// Short UI sounds, synthesized so there are no audio files to ship.

let ctx: AudioContext | null = null;

type Sound = "join" | "leave" | "mute" | "unmute" | "deafen" | "undeafen" | "message" | "ring" | "streamStart" | "streamStop" | "disconnect";

const tones: Record<Sound, [number, number, number][]> = {
  // [frequency Hz, start s, length s]
  join: [[523, 0, 0.09], [784, 0.09, 0.14]],
  leave: [[784, 0, 0.09], [523, 0.09, 0.14]],
  mute: [[440, 0, 0.07], [330, 0.07, 0.09]],
  unmute: [[330, 0, 0.07], [440, 0.07, 0.09]],
  deafen: [[392, 0, 0.08], [262, 0.08, 0.12]],
  undeafen: [[262, 0, 0.08], [392, 0.08, 0.12]],
  message: [[880, 0, 0.06], [1175, 0.06, 0.12]],
  ring: [[659, 0, 0.18], [784, 0.2, 0.18], [659, 0.6, 0.18], [784, 0.8, 0.18]],
  streamStart: [[587, 0, 0.08], [740, 0.08, 0.08], [880, 0.16, 0.12]],
  streamStop: [[880, 0, 0.08], [740, 0.08, 0.08], [587, 0.16, 0.12]],
  disconnect: [[440, 0, 0.1], [294, 0.1, 0.2]],
};

export function playSound(name: Sound) {
  if (localStorage.getItem("kempt.sounds") === "0") return;
  try {
    ctx ??= new AudioContext();
    if (ctx.state === "suspended") ctx.resume();
    const now = ctx.currentTime;
    const vol = 0.12;
    for (const [f, start, len] of tones[name]) {
      const o = ctx.createOscillator();
      const g = ctx.createGain();
      o.type = "sine";
      o.frequency.value = f;
      g.gain.setValueAtTime(0, now + start);
      g.gain.linearRampToValueAtTime(vol, now + start + 0.01);
      g.gain.exponentialRampToValueAtTime(0.0001, now + start + len);
      o.connect(g).connect(ctx.destination);
      o.start(now + start);
      o.stop(now + start + len + 0.02);
    }
  } catch {
    /* audio blocked until the user interacts; skip */
  }
}
