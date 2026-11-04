# Moonlight (game streaming)

`moonlight-web-stream` runs on the Pi and turns a Sunshine host into a browser
stream. The kiosk's **Games** tile opens it; nothing is installed on the host
beyond Sunshine, and nothing is installed on the client beyond Firefox.

```
  gaming PC / Steam Deck                     Pi
  ┌────────────────────┐   Moonlight   ┌──────────────────────────────┐
  │ Sunshine (47984…)  │◄─────────────►│ moonlight-web-stream         │
  │ NVENC/VA-API       │  over tailnet │ :8080 → 127.0.0.1:18086      │
  └────────────────────┘               │        │ WebRTC/UDP          │
                                       └────────┼─────────────────────┘
                                                ▼
                                    Firefox (kiosk) 127.0.0.1 / 100.x
```

The bridge speaks the Moonlight protocol to Sunshine, then re-wraps the encoded
frames as WebRTC and hands them to the browser. It does **not** transcode, so
the Pi never encodes video; it only relays and decodes.

## 1. Why the UI is on loopback, not a Tailscale Service

Every other app in this stack is fronted by `tailscale serve`. Moonlight is the
exception: its client keeps a WebSocket open for signalling, and `serve`'s HTTP
proxy has two open WebSocket bugs ([tailscale#20882],
[tailscale#18827]) that can make the handshake fail or the socket drop every
few seconds. `serve` also cannot proxy the WebRTC media, which is UDP.

So the kiosk loads the UI from **`http://127.0.0.1:18086`**. Loopback is a
[secure context], which is all the client needs for the Gamepad, pointer-lock
and WebSocket APIs, and it never touches `tailscale serve` or a certificate.
The media ports are still published on the tailnet address, so a tailnet client
could reach them; only the UI is loopback. For playing away from the TV, use
native Moonlight on a laptop or the Deck — it is a better remote client anyway.

[secure context]: https://developer.mozilla.org/en-US/docs/Web/Security/Secure_Contexts
[tailscale#20882]: https://github.com/tailscale/tailscale/issues/20882
[tailscale#18827]: https://github.com/tailscale/tailscale/issues/18827

## 2. Before the first deploy

1. Make sure Sunshine is installed and running on each host (the gaming PC and
   the Steam Deck) and that both are on the tailnet.
2. Allow the Pi to reach the Sunshine ports on each host: **TCP 47984, 47989,
   47990, 48010** and **UDP 47998–48010**. On Windows, the installer's firewall
   rules often only cover Private networks; the Tailscale interface is usually
   classified as Public, so add a rule for `100.64.0.0/10` (or the Pi's tailnet
   address) if pairing or streaming times out.
3. Use **MagicDNS names** (`gaming-pc.<tailnet>.ts.net`) when adding hosts, not
   LAN IPs: they survive DHCP changes on the hotel network.

Then deploy:

```bash
just site      # first time: packages, bluetooth, then the stack
just deploy    # afterwards
```

No new `svc:` entry is needed in the Tailscale admin console.

## 3. One-time setup (from a laptop, not the TV)

The kiosk has no keyboard, so do this from a laptop or phone on the tailnet.
The UI is loopback, so reach it through an SSH tunnel:

```bash
just ssh -N -L 18086:127.0.0.1:18086    # or: ssh -N -L 18086:127.0.0.1:18086 travel-lab…
```

Then:

1. Open **`http://127.0.0.1:18086`** on the laptop.
2. Create the first user. The first account is the admin.
3. In **Admin**, set that user as the **default user**. The server then treats
   unauthenticated requests as that user, so the TV never shows a login wall.
   (In v3 this lives in `data.json`, not `config.json`; the admin panel is the
   supported way to set it.)
4. Add a host: the gaming PC's MagicDNS name, port empty (default 47989).
5. Click the host to pair. The server shows a **PIN**; type it into Sunshine's
   web UI at `https://<host>:47990` (or the Sunshine tray icon). Repeat for the
   Steam Deck.
6. Open a stream once from the laptop to check it works end to end, then close
   it. The pairings are server-side; the TV only ever clicks a host card.

### Stream settings for the Pi 4

The Pi 4 can only hardware-decode **H.264** (no HEVC, no AV1). Set the host and
the client to:

- codec **H.264**;
- **1080p60** (or 720p60 if it stutters);
- **20 Mbit/s** or less on hotel Wi-Fi.

The client's per-stream settings are stored server-side under the default user,
so what you pick on the laptop is what the TV uses.

## 4. The kiosk

- **Games** is the twelfth launcher tile and opens the bridge's web UI at
  `http://127.0.0.1:18086`. The host list is a normal web page, so use the
  phone remote's trackpad to pick a host and press play; once the stream starts,
  the controller belongs to the game. `Alt+Home` (phone remote → Shortcuts)
  returns to the launcher.
- The **cog** in the top-right corner opens **Settings** → **Bluetooth** and
  **About**.
- A paired controller drives the launcher, Settings, Bluetooth, About, Wi-Fi,
  Radio and the forecast page: D-pad/stick to move, **A** to open, **B** to go
  back, **X**/**Start** for Settings, **LB**/**RB** for volume.
- While a Moonlight stream is open, the stream page owns the controller (it is
  a different origin, so the launcher's handler is not running).

See [hdmi-tv.md](hdmi-tv.md) for the launcher itself.

## 5. Networking details

- The UI is published on `127.0.0.1:18086` only. The WebSocket and HTTP requests
  stay on the host.
- **WebRTC media is UDP.** The range (`40000-40100` by default) is published on
  loopback for the TV and on the Pi's **tailnet address** for tailnet clients.
  The server advertises the tailnet address as its ICE candidate
  (`WEBRTC_NAT_1TO1_HOST`) and a loopback candidate by default.
- The tailnet address is read with `tailscale ip -4` at deploy time. If it ever
  changes, re-run `just deploy` so the quadlet is re-rendered.
- Keep `travel_lab_moonlight_webrtc_port_min/max`, the published ranges and
  `WEBRTC_PORT_RANGE` in step; they are rendered from the same two values.

## 6. State and backups

Users, pairings and settings live in **`~/.config/moonlight/data.json`** on the
Pi (bind-mounted at `/moonlight-web/server`). Back it up if you would rather not
re-pair the hosts. An optional `server/config.json` also lives there, but the
container environment sets everything this stack needs.

## 7. Troubleshooting

| Symptom | Check |
|---|---|
| Games tile shows a connection error | `just moonlight-status`; the container publishes `127.0.0.1:18086`. |
| Page loads, host is unreachable | Sunshine ports blocked (Windows firewall / Public network), or the host is asleep. `just ssh 'ping <host>'`. |
| Pairing fails | Host is in Sunshine's web UI, the PIN is current, and the ports above are open. Re-pair with a fresh PIN. |
| Stream opens, then stutters or drops | 1080p60 is the Pi 4's ceiling; drop to 720p60 or lower the bitrate. Check CPU with `just ssh 'top'`. |
| Black frame / no video | Codec not H.264, or Firefox fell back to software decode. On the TV, open `about:support` and look for **H.264 Hardware Decoding**; during a stream, `about:webrtc` shows the negotiated codec. |
| Controller pairs but does not navigate | Press a button while the launcher is focused (Firefox only exposes pads after input). Check `Settings → Bluetooth` shows it **Connected**. |
| Bluetooth page says unavailable | `just ssh 'systemctl status bluetooth'`; the travel-tv container mounts `/run/dbus` and needs `bluetooth.service`. |
| Need to pair without the TV | `just bluetooth` opens `bluetoothctl` on the Pi. |

## 8. Operate

```bash
just moonlight-status
just moonlight-logs
just bluetooth          # bluetoothctl fallback
```
