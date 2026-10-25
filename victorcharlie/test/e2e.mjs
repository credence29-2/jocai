// End-to-end test: starts a fresh server, then drives three people in real
// Chromium with fake mics and cameras through chat, roles, voice, two
// simultaneous screen shares, webcams and moderator mute/deafen.
//
//   node test/e2e.mjs path/to/victorcharlie-server [screenshot-dir]

import { spawn } from "node:child_process";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { createRequire } from "node:module";

const require = createRequire(import.meta.url);
const { chromium } = require("playwright");

const bin = process.argv[2];
const shots = process.argv[3];
if (!bin) {
  console.error("usage: node test/e2e.mjs <server binary> [screenshot dir]");
  process.exit(2);
}
if (shots) mkdirSync(shots, { recursive: true });

const port = 18080 + Math.floor(Math.random() * 500);
const base = `http://localhost:${port}`;
const data = mkdtempSync(join(tmpdir(), "vc-e2e-"));
writeFileSync(join(data, "victorcharlie.json"), JSON.stringify({ http_port: port, lan_only: true, rtc_tcp_port: 17881, rtc_udp_port: 17882 }));

let failures = 0;
const check = (ok, what) => {
  console.log(`${ok ? "PASS" : "FAIL"}  ${what}`);
  if (!ok) failures++;
};

const server = spawn(bin, ["-data", data], { stdio: ["ignore", "pipe", "pipe"] });
let log = "";
server.stdout.on("data", (d) => (log += d));
server.stderr.on("data", (d) => (log += d));

async function waitFor(fn, ms = 15000, step = 200) {
  const end = Date.now() + ms;
  let last;
  while (Date.now() < end) {
    try {
      last = await fn();
      if (last) return last;
    } catch (e) {
      last = e;
    }
    await new Promise((r) => setTimeout(r, step));
  }
  return false;
}

const browser = await chromium.launch({
  args: ["--use-fake-ui-for-media-stream", "--use-fake-device-for-media-stream", "--auto-select-desktop-capture-source=Entire screen", "--autoplay-policy=no-user-gesture-required", "--enable-usermedia-screen-capturing", "--allow-http-screen-capture"],
});

async function person(name) {
  const ctx = await browser.newContext({ viewport: { width: 1400, height: 860 } });
  await ctx.grantPermissions(["microphone", "camera"], { origin: base });
  const page = await ctx.newPage();
  page.on("pageerror", (e) => console.log(`[${name}] page error: ${e.message}`));
  page.on("console", (m) => m.type() === "error" && !/favicon|401|404/.test(m.text()) && console.log(`[${name}] console: ${m.text()}`));
  page.name = name;
  return page;
}

const api = (page, method, path, body) =>
  page.evaluate(
    async ([method, path, body]) => {
      const r = await fetch("/api" + path, { method, headers: { "X-VC": "1", "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
      const t = await r.text();
      return { status: r.status, body: t ? JSON.parse(t) : null };
    },
    [method, path, body],
  );

const st = (page) => page.evaluate(() => window.vc.state());
const vs = (page, expr) => page.evaluate(expr);

async function register(page, username, invite) {
  await page.goto(invite ? `${base}/invite/${invite}` : base);
  await page.fill('input[autocomplete="username"]', username);
  const pw = page.locator('input[type="password"]');
  await pw.nth(0).fill("correct horse");
  await pw.nth(1).fill("correct horse");
  await page.click("button.btn.primary");
  await page.waitForSelector(".app", { timeout: 15000 });
}

async function shot(page, name) {
  if (shots) await page.screenshot({ path: join(shots, name + ".png") });
}

try {
  check(await waitFor(async () => (await fetch(base + "/api/info")).ok), "server starts");

  const a = await person("alice");
  const b = await person("bob");
  const c = await person("carol");

  await register(a, "alice");
  const ready = await st(a);
  const guild = Object.values(ready.guilds)[0];
  check(!!guild && ready.me.is_admin, "first account is admin and gets a Home server");
  const text = guild.channels.find((x) => x.type === "text");
  const voiceCh = guild.channels.find((x) => x.type === "voice");

  const inv = await api(a, "POST", `/guilds/${guild.id}/invites`, { max_uses: 0, max_hours: 24 });
  check(inv.status === 200 && inv.body.code, "admin creates an invite");

  // Without an invite, registration is closed.
  const closed = await api(b, "GET", "/info").catch(() => null);
  await b.goto(base);
  const noInvite = await b.evaluate(async () => (await fetch("/api/auth/register", { method: "POST", headers: { "X-VC": "1", "Content-Type": "application/json" }, body: JSON.stringify({ username: "mallory", password: "password123" }) })).status);
  check(noInvite === 403, "registration needs an invite after the first account");
  void closed;

  await register(b, "bob", inv.body.code);
  await register(c, "carol", inv.body.code);
  check(await waitFor(async () => Object.values((await st(a)).guilds)[0].members.length === 3), "invited people join the server");

  // --- chat ---
  await a.goto(`${base}/channels/${guild.id}/${text.id}`);
  await b.goto(`${base}/channels/${guild.id}/${text.id}`);
  await a.waitForSelector(".composer textarea");
  await b.waitForSelector(".composer textarea");
  const bobId = (await st(b)).me.id;
  await a.fill(".composer textarea", `hey <@${bobId}> **welcome** to the den`);
  await a.keyboard.press("Enter");
  check(await waitFor(() => b.locator(".message .content strong", { hasText: "welcome" }).count()), "message shows up live for others, with formatting");
  check(await waitFor(() => b.locator(".message.mentioned").count()), "mention is highlighted for the person mentioned");
  const msgId = await waitFor(async () => (await st(b)).messages[text.id]?.list.at(-1)?.id);
  const rx = await api(b, "PUT", `/channels/${text.id}/messages/${msgId}/reactions/${encodeURIComponent("👍")}`);
  check(rx.status === 204 && (await waitFor(() => a.locator(".reaction", { hasText: "👍" }).count())), "reactions sync");
  await b.fill(".composer textarea", "typing test");
  check(await waitFor(() => a.locator(".typing b", { hasText: "bob" }).count(), 5000), "typing indicator shows");
  await b.fill(".composer textarea", "");

  // Upload an image.
  const png = Buffer.from("89504e470d0a1a0a0000000d4948445200000002000000020802000000fdd49a730000001549444154789c63f8cfc0f01f0c18fe33fc0700224001fe7c5d3a1c0000000049454e44ae426082", "hex");
  await a.setInputFiles('.composer input[type="file"]', { name: "dot.png", mimeType: "image/png", buffer: png });
  await a.fill(".composer textarea", "a picture");
  await a.keyboard.press("Enter");
  check(await waitFor(() => b.locator("img.attachment-image").count()), "image uploads and shows inline");

  // --- roles ---
  const mod = await api(a, "POST", `/guilds/${guild.id}/roles`, { name: "Mod", color: 0x3498db, hoist: true, permissions: (1 << 11) | (1 << 12) | (1 << 13) });
  check(mod.status === 200, "admin creates a Mod role");
  const assign = await api(a, "PATCH", `/guilds/${guild.id}/members/${bobId}`, { roles: [mod.body.id] });
  check(assign.status === 204, "admin gives bob the Mod role");
  const carolId = (await st(c)).me.id;
  const denied = await api(c, "PATCH", `/guilds/${guild.id}/members/${bobId}`, { mute: true });
  check(denied.status === 403, "people without Mute Members can't server-mute");

  // A private channel only Mods can see.
  const priv = await api(a, "POST", `/guilds/${guild.id}/channels`, { type: "text", name: "mods only", overwrites: [{ id: guild.id, allow: 0, deny: 1 }, { id: mod.body.id, allow: 1, deny: 0 }] });
  check(priv.status === 200, "admin makes a private channel");
  check(await waitFor(async () => Object.values((await st(b)).guilds)[0].channels.some((x) => x.id === priv.body.id)), "mod can see the private channel");
  const carolSees = Object.values((await st(c)).guilds)[0].channels.some((x) => x.id === priv.body.id);
  const carolRead = await api(c, "GET", `/channels/${priv.body.id}/messages`);
  check(!carolSees && carolRead.status === 404, "everyone else can't see or read it");

  // --- voice ---
  for (const p of [a, b, c]) {
    await p.goto(`${base}/channels/${guild.id}/${voiceCh.id}`);
    await p.waitForSelector(".voice-lobby .btn.primary, .voice-view");
    await p.click(".voice-lobby .btn.primary");
  }
  const roomSize = (p) => p.evaluate(() => (window.vc.voice.room ? window.vc.voice.room.remoteParticipants.size : -1));
  check(await waitFor(async () => (await roomSize(a)) === 2 && (await roomSize(b)) === 2 && (await roomSize(c)) === 2, 20000), "three people connect to the voice channel");
  check(
    await waitFor(async () => Object.values((await st(a)).voiceStates).filter((v) => v.channel_id === voiceCh.id).length === 3),
    "everyone shows in the channel list as in voice",
  );
  const micPublished = (p) => p.evaluate(() => !!window.vc.voice.room?.localParticipant.getTrackPublication("microphone")?.track);
  check(await waitFor(async () => (await micPublished(a)) && (await micPublished(b)) && (await micPublished(c))), "everyone's mic is published");
  const hearing = (p) => p.evaluate(() => [...window.vc.voice.room.remoteParticipants.values()].filter((rp) => rp.getTrackPublication("microphone")?.isSubscribed).length);
  check(await waitFor(async () => (await hearing(c)) === 2), "carol receives both other mics");
  const bitrate = await a.evaluate(async () => {
    const pub = window.vc.voice.room.localParticipant.getTrackPublication("microphone");
    const sender = pub?.track?.sender;
    return sender?.getParameters().encodings?.[0]?.maxBitrate ?? 0;
  });
  check(bitrate >= 90000, `HD voice: mic encodes at ${Math.round(bitrate / 1000)} kbps`);

  // Self mute.
  await a.click('.user-panel button[title="Mute"]');
  check(await waitFor(async () => Object.values((await st(b)).voiceStates).find((v) => v.user_id === ready.me.id)?.self_mute), "self-mute shows for others");
  const aMuted = await a.evaluate(() => window.vc.voice.room.localParticipant.getTrackPublication("microphone").isMuted);
  check(aMuted, "self-mute actually stops sending audio");
  await a.click('.user-panel button[title="Unmute"]');

  // Two people share screens at once.
  await a.click('.voice-controls button[title="Share your screen"]');
  await b.click('.voice-controls button[title="Share your screen"]');
  const sharing = (p) => p.evaluate(() => !!window.vc.voice.room?.localParticipant.getTrackPublication("screen_share")?.track);
  check(await waitFor(async () => (await sharing(a)) && (await sharing(b)), 15000), "two people share their screens at the same time");
  check(await waitFor(async () => (await c.locator(".tile.screen-offer").count()) === 2), "carol sees two Watch stream offers");
  check(await waitFor(async () => Object.values((await st(c)).voiceStates).filter((v) => v.streaming).length === 2), "LIVE badges come from server voice state");
  await shot(c, "1-stream-offers");
  const offers = c.locator(".tile.screen-offer .btn.primary");
  await offers.nth(0).click();
  await c.click(".voice-view .tile.focused"); // back to grid
  await waitFor(async () => (await c.locator(".tile.screen-offer").count()) === 1);
  await c.locator(".tile.screen-offer .btn.primary").first().click();
  await c.click(".voice-view .tile.focused");
  const playing = await waitFor(
    () =>
      c.evaluate(() => {
        const vids = [...document.querySelectorAll(".tile.screen video")];
        return vids.length === 2 && vids.every((v) => v.videoWidth > 0 && !v.paused);
      }),
    20000,
  );
  check(playing, "carol watches both screen shares at once, both playing video");
  const res = await c.evaluate(() => [...document.querySelectorAll(".tile.screen video")].map((v) => `${v.videoWidth}x${v.videoHeight}`).join(", "));
  console.log(`      stream sizes: ${res}`);
  await shot(c, "2-watching-two-streams");

  // Webcam.
  await c.click('.voice-controls button[title="Camera"]');
  check(await waitFor(() => a.evaluate(() => [...document.querySelectorAll(".tile.user video")].some((v) => v.videoWidth > 0))), "webcam video reaches others");
  await shot(a, "3-camera-and-shares");

  // Moderator server-mutes carol.
  const m1 = await api(b, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { mute: true });
  check(m1.status === 204, "mod server-mutes carol");
  check(await waitFor(async () => !(await micPublished(c)), 10000), "server mute cuts carol's mic at the media server");
  check(await waitFor(async () => Object.values((await st(a)).voiceStates).find((v) => v.user_id === carolId)?.mute), "server mute shows for everyone");
  const rejoinTry = await c.evaluate(async () => {
    try {
      await window.vc.voice.room.localParticipant.setMicrophoneEnabled(true);
      return "published";
    } catch (e) {
      return "blocked";
    }
  });
  await new Promise((r) => setTimeout(r, 1500));
  const stillOff = await b.evaluate((cid) => !window.vc.voice.room.remoteParticipants.get(cid)?.getTrackPublication("microphone")?.track, carolId);
  check(stillOff, `a server-muted person can't get around it from their browser (${rejoinTry})`);
  await api(b, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { mute: false });
  check(await waitFor(async () => micPublished(c), 10000), "unmuting gives carol her mic back");

  // Server deafen.
  await api(b, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { deaf: true });
  check(await waitFor(async () => (await hearing(c)) === 0, 10000), "server deafen stops carol receiving audio");
  await api(b, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { deaf: false });
  check(await waitFor(async () => (await hearing(c)) === 2, 10000), "undeafen restores it");

  // Move and disconnect.
  const second = await api(a, "POST", `/guilds/${guild.id}/channels`, { type: "voice", name: "Gaming", bitrate: 128 });
  await waitFor(async () => Object.values((await st(b)).guilds)[0].channels.some((x) => x.id === second.body.id));
  await api(b, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { channel_id: second.body.id });
  check(await waitFor(async () => (await c.evaluate(() => window.vc.voiceState().channelId)) === second.body.id, 15000), "mod moves carol to another voice channel");
  await api(b, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { channel_id: null });
  check(await waitFor(async () => (await c.evaluate(() => window.vc.voiceState().status)) === "idle", 10000), "mod disconnects carol");

  // Security regressions from the review.
  // 1. A private voice channel in a server you're not in must not break your app.
  const cg = await api(c, "POST", "/guilds", { name: "Carol's place" });
  await waitFor(async () => (await st(c)).guilds[cg.body.id]);
  const cgRole = await api(c, "POST", `/guilds/${cg.body.id}/roles`, { name: "Crew", permissions: 0 });
  const secret = await api(c, "POST", `/guilds/${cg.body.id}/channels`, { type: "voice", name: "Hideout", overwrites: [{ id: cg.body.id, allow: 0, deny: 1 }, { id: cgRole.body.id, allow: 1, deny: 0 }] });
  await c.evaluate((id) => window.vc.voice.join(id), secret.body.id);
  await waitFor(async () => (await c.evaluate(() => window.vc.voiceState().status)) === "connected", 15000);
  await b.reload();
  check(await waitFor(() => b.locator(".app").count(), 15000) && !(await st(b)).guilds[cg.body.id], "someone else's private voice channel doesn't break your app");
  await c.evaluate(() => window.vc.voice.leave());
  // 2. Manage Channels alone can't create a channel that grants extra permissions.
  const chMgr = await api(a, "POST", `/guilds/${guild.id}/roles`, { name: "Channel helper", permissions: 1 << 17 });
  await api(a, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { roles: [chMgr.body.id] });
  await waitFor(async () => Object.values((await st(c)).guilds).find((g) => g.id === guild.id)?.permissions & (1 << 17));
  const sneaky = await api(c, "POST", `/guilds/${guild.id}/channels`, { type: "text", name: "sneaky", overwrites: [{ id: carolId, allow: 1 << 5, deny: 0 }] });
  check(sneaky.status === 403, "creating a channel can't grant permissions you don't have");
  await api(a, "PATCH", `/guilds/${guild.id}/members/${carolId}`, { roles: [] });

  // Push to talk.
  await a.evaluate(() => {
    const s = JSON.parse(localStorage.getItem("vc.voice") || "{}");
    s.mode = "ptt";
    s.pttKey = "KeyV";
    s.pttRelease = 0;
    localStorage.setItem("vc.voice", JSON.stringify(s));
  });
  await a.reload();
  await a.waitForSelector(".app");
  await a.goto(`${base}/channels/${guild.id}/${voiceCh.id}`);
  await a.click(".voice-lobby .btn.primary");
  await waitFor(() => micPublished(a));
  const pttIdle = await a.evaluate(() => window.vc.voice.room.localParticipant.getTrackPublication("microphone").isMuted);
  await a.keyboard.down("KeyV");
  const pttOn = await waitFor(() => a.evaluate(() => !window.vc.voice.room.localParticipant.getTrackPublication("microphone").isMuted), 3000);
  await a.keyboard.up("KeyV");
  const pttOff = await waitFor(() => a.evaluate(() => window.vc.voice.room.localParticipant.getTrackPublication("microphone").isMuted), 3000);
  check(pttIdle && pttOn && pttOff, "push-to-talk opens the mic only while the key is held");

  // DM and call ring.
  const dm = await api(b, "POST", "/users/@me/channels", { recipients: [carolId] });
  check(dm.status === 200, "bob opens a DM with carol");
  await b.evaluate(() => window.vc.voice.leave());
  await b.evaluate((id) => window.vc.voice.join(id), dm.body.id);
  check(await waitFor(() => c.locator(".incoming-call").count(), 15000), "carol gets an incoming call");
  await shot(c, "4-incoming-call");
  await c.click(".incoming-call .round-btn.accept");
  check(await waitFor(async () => (await roomSize(c)) === 1 && (await roomSize(b)) === 1, 15000), "the DM call connects");

  await shot(a, "5-alice");
  await b.goto(`${base}/channels/${guild.id}/${text.id}`);
  await b.waitForSelector("img.attachment-image");
  await shot(b, "6-chat");
  // Phone-sized layout.
  await b.setViewportSize({ width: 390, height: 844 });
  await shot(b, "7-phone-chat");
  await b.click(".main-head .mobile-only");
  await shot(b, "8-phone-channels");
  await b.click('.user-panel button[title="Settings"]');
  check(await b.locator(".settings-danger-mobile", { hasText: "Log out" }).isVisible(), "log out is reachable on a phone-sized screen");
  await b.click(".settings-danger-mobile");
  check(await waitFor(() => b.locator(".auth-card").count(), 10000), "logging out returns to the login screen");
} catch (e) {
  console.log("FAIL  test crashed:", e);
  failures++;
} finally {
  await browser.close();
  server.kill();
}

if (failures) {
  console.log(`\n${failures} check(s) failed. Server log:\n${log.split("\n").filter((l) => !/udp_mux|connWorker|^\s/.test(l)).join("\n").slice(-4000)}`);
  process.exit(1);
}
console.log("\nAll checks passed.");
