# Project Victor Charlie

A self-hosted Discord alternative for friends and family. One program runs on your home server and handles accounts, servers, text channels, DMs, file uploads, voice, webcams and screen sharing. People use it from a browser, the Windows app, or the Android app.

The name people see in the app is set by `app_name` in `victorcharlie.json` and defaults to **Project Victor Charlie**.

## What it does

- Servers with text and voice channels, categories, private channels and per-channel permissions
- Roles with colors, ordering and Discord-style permissions (admin, manage channels/roles/server, kick, ban, mute, deafen, move, priority speaker, push-to-talk only, and more)
- Messages with markdown, replies, edits, reactions, mentions (@user, @role, @everyone), pins, search, typing indicators, unread badges, image/video/file uploads, spoilers
- DMs and group DMs, with voice/video calls that ring
- Voice channels with HD voice (96 kbps Opus by default, up to 384 kbps per channel), noise suppression, echo cancellation, push-to-talk or open mic
- Webcams and screen sharing, with any number of people sharing at once. Viewers pick which streams to watch, Discord style, so nobody downloads streams they aren't looking at. Up to 1440p/source and 60 fps, with stream audio
- Moderation: server mute, server deafen, move between channels, disconnect, kick, ban. Mute and deafen are enforced by the media server, not just the app
- Per-person volume (0 to 200%), local mute, join/leave sounds, notifications
- Invite links with expiry and use limits. After the first account, registration needs an invite unless you turn on open registration

## How it's built

- `main.go`, `internal/`: the server, in Go. It embeds [LiveKit](https://livekit.io), an open source media server, so voice and video run in the same program. SQLite (no separate database to install) holds everything else.
- `web/`: the app (React + TypeScript), built into the server binary.
- `desktop/`: the Windows app (Electron). It loads your server's app and adds push-to-talk that works while you're in a game, a window picker for screen share, a tray icon, and "start with Windows".
- `android/`: the Android app. A thin wrapper around the same web app that adds mic/camera permission handling, notifications, file uploads, and keeps voice running in the background.
- `test/e2e.mjs`: drives three people in real browsers through chat, roles, voice, two simultaneous screen shares, webcams and moderation.

## Setting it up on your home server

You need three things: the server program running, a name for your house on the internet, and a few ports forwarded on your router.

### 1. Run it once

Download `victorcharlie-server.exe` (Windows) or `victorcharlie-server-linux-amd64` (Linux) from the latest **Victor Charlie server** run under the repo's Actions tab, put it in its own folder, and run it. It creates a `victorcharlie-data` folder next to itself with `victorcharlie.json` (settings), `victorcharlie.db` (everything), `uploads/` and `victorcharlie.log`. Stop it again with Ctrl+C.

### 2. Get a free domain

Browsers only allow the mic, camera and screen share on HTTPS pages, so you need a name to get a certificate for. The easy free option is [DuckDNS](https://www.duckdns.org): sign in, pick a name like `ourplace.duckdns.org`, and copy your token.

### 3. Forward ports on your router

Point these at your server's local IP (give the server a reserved/static IP in the router first):

| Port | Protocol | What it's for |
| --- | --- | --- |
| 80 | TCP | Getting and renewing the HTTPS certificate |
| 443 | TCP | The app itself |
| 7881 | TCP | Voice and video (fallback) |
| 7882 | UDP | Voice and video (main) |

If Windows Firewall asks, allow the server on private and public networks.

### 4. Edit victorcharlie.json

```json
{
  "app_name": "Project Victor Charlie",
  "domain": "ourplace.duckdns.org",
  "https": "auto",
  "duckdns_token": "your-token-here",
  "acme_email": "you@example.com"
}
```

Leave the other settings alone unless you have a reason. `duckdns_token` makes the server keep the DuckDNS name pointed at your house when your ISP changes your IP. The server also restarts itself if your public IP changes, so voice keeps working from outside.

Start the server again and open `https://ourplace.duckdns.org`. The first account you make is the admin, and you start with a server called Home. Right-click it and pick **Invite people** to get a link for friends.

### 5. Keep it running

**Windows:** run this once in an admin PowerShell (change the path) so it starts with the PC and restarts if it ever crashes:

```powershell
$a = New-ScheduledTaskAction -Execute "C:\Victor Charlie\victorcharlie-server.exe" -WorkingDirectory "C:\Victor Charlie"
$t = New-ScheduledTaskTrigger -AtStartup
$s = New-ScheduledTaskSettingsSet -RestartCount 99 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit 0 -AllowStartIfOnBatteries
Register-ScheduledTask -TaskName "Victor Charlie" -Action $a -Trigger $t -Settings $s -User "SYSTEM" -RunLevel Highest
Start-ScheduledTask -TaskName "Victor Charlie"
```

Check `victorcharlie-data\victorcharlie.log` to see what it's doing.

**Linux:** save as `/etc/systemd/system/victorcharlie.service`, then `systemctl enable --now victorcharlie`:

```ini
[Unit]
Description=Victor Charlie chat server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/opt/victorcharlie/victorcharlie-server-linux-amd64 -data /opt/victorcharlie/victorcharlie-data
Restart=always
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

### Alternative: Tailscale instead of port forwarding

If you'd rather not open ports, everyone can install [Tailscale](https://tailscale.com) and join your tailnet. Set `"https": "proxy"`, `"http_port": 8080` and `"lan_only": true` in `victorcharlie.json`, then on the server run `tailscale serve --bg 8080`. People open the `https://<server>.<tailnet>.ts.net` address it prints. The catch is that every person needs Tailscale installed and signed in.

## Using the apps

- **Browser:** just open the address. Chrome and Edge do screen sharing with audio best. In a browser, push-to-talk only works while the tab is focused.
- **Windows app:** download `VictorCharlie-Setup-*.exe` (installer) or `VictorCharlie-Portable-*.exe` from the **Victor Charlie desktop app** Actions run. Enter your server address on first launch. Push-to-talk, mute and deafen keys work everywhere, including in games. It lives in the tray when you close the window.
- **Android:** install `VictorCharlie-*.apk` from the **Victor Charlie Android app** Actions run (allow installing from your browser or file manager). Enter the server address on first launch. Voice, video, chat and watching streams all work, and voice keeps going with the screen off. Two limits: sharing your phone's own screen isn't supported yet, and message notifications only arrive while the app is running (there's no push service).

## Settings reference (victorcharlie.json)

| Setting | Default | Meaning |
| --- | --- | --- |
| `app_name` | Project Victor Charlie | Name shown in the app |
| `domain` | | Your public name, like `ourplace.duckdns.org` |
| `https` | `off` | `auto` (Let's Encrypt on 80/443), `off` (plain HTTP on `http_port`), `proxy` (plain HTTP behind Tailscale or a reverse proxy) |
| `http_port` | 8080 | Port for `off` and `proxy` |
| `rtc_tcp_port` / `rtc_udp_port` | 7881 / 7882 | Voice and video ports to forward |
| `public_ip` | | Force the advertised IP instead of looking it up |
| `lan_only` | false | Skip the public IP lookup (home network or Tailscale only) |
| `duckdns_token` | | Keeps a DuckDNS name updated |
| `open_registration` | false | Let anyone register without an invite |
| `max_upload_mb` | 100 | Largest file people can upload |

`open_registration` and `max_upload_mb` can also be changed in the app under Settings, Server admin.

## Bandwidth

Everything goes through your server, so your home **upload** speed is the limit. Rough numbers per person listening or watching: voice about 0.1 Mbps, a webcam about 1 Mbps, a 1080p30 screen share about 3 Mbps. Five friends watching one 1080p stream is about 15 Mbps of upload from the server. Pick lower stream settings if your upload is tight.

## Development

```sh
cd web && npm install && npm run dev   # app with hot reload on :5173, talks to a server on :8080
go run . -data ./victorcharlie-data            # the server (build the web app first for the embedded copy)
./build.sh                             # web app + Windows and Linux server binaries into dist/
node test/e2e.mjs dist/victorcharlie-server-linux-amd64 shots/
```
