# Moonlight (game streaming)

`moonlight-web-stream` runs on the Pi and turns a Sunshine host into a browser
stream. The kiosk's **Games** tile opens it; nothing is installed on the host
beyond Sunshine, and nothing is installed on the client beyond Firefox.

```
  gaming PC / Steam Deck                     Pi
  ┌────────────────────┐   Moonlight   ┌──────────────────────────────┐
  │ Sunshine (47984…)  │◄─────────────►│ moonlight-web-stream         │
  │ NVENC/VA-API       │  over tailnet │ :8080 → svc:travel-moonlight │
  └────────────────────┘               │        │ WebRTC/UDP          │
                                       └────────┼─────────────────────┘
                                                ▼
                                    Firefox (kiosk) 100.x:40000-40100
```

The bridge speaks the Moonlight protocol to Sunshine, then re-wraps the encoded
frames as WebRTC and hands them to the browser. It does **not** transcode, so
the Pi never encodes video; it only relays and decodes.

## 1. Ingress and the WebSocket caveat

Moonlight is a normal Tailscale Service: the container publishes
`127.0.0.1:18086`, and `tailscale serve` terminates TLS and proxies
`travel-moonlight.<tailnet>` to it. That gives the page a **secure context**,
which the Gamepad and pointer-lock APIs require (a controller is the point of
this tile), and it makes the UI reachable from any tailnet device.

The caveat is the signalling socket. `tailscale serve`'s HTTP proxy has two open
WebSocket bugs ([tailscale#20882], [tailscale#18827]); a stream may fail to
start, or drop and reconnect every few seconds. Firefox usually opens
WebSockets over HTTP/1.1, which is the working path, so this is worth trying —
but if the host list loads and a stream never starts, point the tile back at
the loopback URL:

```yaml
# group_vars/travel_lab.yml
travel_lab_moonlight_url: "http://127.0.0.1:18086"
```

and re-run `just deploy`. The loopback publish stays in place, and `127.0.0.1`
is also a secure context. The cost is that only the TV can use it, and remote
setup needs an SSH tunnel (`just ssh -N -L 18086:127.0.0.1:18086`).

[tailscale#20882]: https://github.com/tailscale/tailscale/issues/20882
[tailscale#18827]: https://github.com/tailscale/tailscale/issues/18827

## 2. Before the first deploy

1. Create **`svc:travel-moonlight`** in the Tailscale admin console, like the
   other Services, and approve the `travel-lab` proxy for it. `tailscale serve`
   applies the config immediately but prints "approval from an admin is
   required", and the name does not resolve until the proxy is approved.
2. Make sure Sunshine is installed and running on each host (the gaming PC and
   the Steam Deck) and that both are on the tailnet.
3. Allow the Pi to reach the Sunshine ports on each host: **TCP 47984, 47989,
   47990, 48010** and **UDP 47998–48010**. On Windows, the installer's firewall
   rules often only cover Private networks; the Tailscale interface is usually
   classified as Public, so add a rule for `100.64.0.0/10` (or the Pi's tailnet
   address) if pairing or streaming times out.
4. Use **MagicDNS names** (`gaming-pc.<tailnet>.ts.net`) when adding hosts, not
   LAN IPs: they survive DHCP changes on the hotel network.

Then deploy:

```bash
just site      # first time: packages, bluetooth, then the stack
just deploy    # afterwards
```

## 3. One-time setup (from a laptop, not the TV)

The kiosk has no keyboard, so do this from a laptop or phone on the tailnet:

1. Open **`https://travel-moonlight.<tailnet>`**.
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
  `https://travel-moonlight.<tailnet>`. The host list is a normal web page, so
  use the phone remote's trackpad to pick a host and press play; once the
  stream starts, the controller belongs to the game. `Alt+Home` (phone remote →
  Shortcuts) returns to the launcher.
- The **cog** in the top-right corner opens **Settings** → **Bluetooth** and
  **About**.
- A paired controller drives the launcher, Settings, Bluetooth, About, Wi-Fi,
  Radio and the forecast page: D-pad/stick to move, **A** to open, **B** to go
  back, **X**/**Start** for Settings, **LB**/**RB** for volume.
- While a Moonlight stream is open, the stream page owns the controller (it is
  a different origin, so the launcher's handler is not running).

See [hdmi-tv.md](hdmi-tv.md) for the launcher itself.

## 5. Networking details

- The UI is loopback inside the host and proxied by `tailscale serve` on
  `tailscale0:443`, like every other service.
- **WebRTC media is UDP**, which `tailscale serve` cannot proxy. The range
  (`40000-40100` by default) is published on loopback and on the Pi's **tailnet
  address**, and the server advertises the tailnet address as its ICE candidate
  (`WEBRTC_NAT_1TO1_HOST`), plus a loopback candidate by default.
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
| Games tile shows a TLS or 404 error | `just serves` should list `svc:travel-moonlight`; the Service must exist and its `travel-lab` proxy must be approved in the admin console. |
| Page loads, stream never starts / UI freezes | The `tailscale serve` WebSocket bug. Switch `travel_lab_moonlight_url` to `http://127.0.0.1:18086`, re-deploy, and use the TV (see §1). |
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
