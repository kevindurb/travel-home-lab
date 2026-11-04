# Travel TV (HDMI kiosk)

The Travel TV turns the Pi's HDMI output into a 10ft hotel-room launcher:
Jellyfin, YouTube, Netflix, Disney+, Spotify, Read (Kiwix), a Wi-Fi QR, a
weather tile with a 12-hour/5-day forecast, and a phone-remote QR. It is served
by a small Go container and displayed by Cage + Firefox on `tty1`, with no
desktop environment.

A phone remote at `/remote.html` (or the short `/r`) drives the kiosk: D-pad,
trackpad, app launch, volume and text entry. It works inside external apps too,
because it injects input at the Wayland compositor rather than into the page.

```
                    tailnet                                  HDMI
  phone/laptop ──► svc:travel-tv ──► travel-tv (Go):18085 ──► Cage ──► Firefox
                                                              │
                                                              └──► launcher page
```

The phone remote is part of the same container:

```
  phone ──► /api/ ──► travel-tv Go app ──► wtype / wlrctl ──► Cage
```

The page itself is a normal Tailscale Service, so it is also reachable at
`https://travel-tv.beaver-cloud.ts.net` from any tailnet device. The kiosk
loads that URL; it does not use `localhost`, so a broken tailnet leaves the
screen waiting rather than showing a stale local copy.

---

## 1. What gets installed

On the Pi:

| Path | Purpose |
|---|---|
| `~/.config/containers/systemd/travel-tv.container` | Go app: launcher page, phone remote and CPR News proxy |
| `~/.config/travel-tv/www/` | rendered launcher and remote pages (`index.html`, `remote.html`, CSS, JS, QR SVGs) |
| `~/.local/share/travel-tv/firefox/` | persistent Firefox profile (logins, zoom, TV mode) |
| `/etc/systemd/system/tv-kiosk.service` | Cage + Firefox supervisor on `tty1` |
| `/etc/pam.d/cage` | logind session stack for Cage |
| `/usr/local/bin/travel-tv-kiosk` | waits for HDMI + URL, then runs Cage |

The `travel-tv` Go container is part of the normal stack and publishes
`127.0.0.1:18085`; `tailscale serve` fronts it as `svc:travel-tv`. It mounts the
kiosk's runtime directory so it can reach the Wayland and PipeWire-Pulse
sockets.

## 2. Before the first deploy

1. Create `svc:travel-tv` in the Tailscale admin console (same as the other
   services). The deploy will fail to publish it otherwise.
2. Make sure `qrencode` is available on the workstation that runs Ansible.
   The QR codes are generated there and only the SVGs are copied to the Pi, so
   the Wi-Fi PSK never appears in the Pi's process list or logs. If it is not
   on `PATH`, set `travel_lab_tv_qrencode_bin`.
3. The `travel-tv` image is built by GitHub Actions and published publicly in
   GHCR (see the README); `just deploy` pulls it. The image bundles `wtype` and
   `wlrctl`, so `just site` only needs the kiosk packages (Cage, Firefox,
   PipeWire). The host role warns if the installed Cage lacks virtual-input
   support.
4. The vault password file is `~/.vault_pass`; `ansible/ansible.cfg` points at
   it. The encrypted Wi-Fi values live in
   `ansible/host_vars/travel-lab/vault.yml`.
5. Fill in the placeholders:

   ```bash
   just vault-edit
   ```

   ```yaml
   travel_lab_wifi_ssid: "YourTravelRouter"
   travel_lab_wifi_psk: "the-real-password"
   ```

   The QR and tile are regenerated on the next deploy. The placeholders are
   deliberately renderable so the page can be reviewed before the credentials
   are set; the Wi-Fi page shows a placeholder note until they are replaced.

## 3. Deploy

```bash
just site          # host packages + kiosk unit + stack/page
```

`just deploy` updates the page, QR and Firefox profile but does **not**
install the host packages or the kiosk system unit. Use `just site` on a fresh
Pi.

The kiosk system service starts at `multi-user.target`. With no HDMI display
connected it stays idle; plugging a TV in starts Cage within a few seconds.
The supervisor loops, so unplugging and replugging also works.

## 4. The tiles and forecast

`travel_lab_tv_tiles` in `ansible/group_vars/travel_lab.yml` is the single
source of truth:

```yaml
travel_lab_tv_tiles:
  - { label: Jellyfin, icon: jellyfin, accent: "#AA5CC3", url: "https://travel-jellyfin...." }
  - { label: YouTube, icon: youtube, accent: "#FF0033", url: "https://www.youtube.com/tv#/" }
  - { label: Netflix, icon: netflix, accent: "#E50914", url: "https://www.netflix.com/" }
  - { label: Disney+, icon: disney, accent: "#113CCF", url: "https://www.disneyplus.com/" }
  - { label: Read, icon: kiwix, accent: "#00A6A6", url: "https://travel-kiwix...." }
  - { label: Wi-Fi, icon: wifi, accent: "#FFC46B", action: wifi, url: "wifi.html" }
  - { label: Spotify, icon: spotify, accent: "#1DB954", url: "https://open.spotify.com/" }
  - { label: Weather, accent: "#7CC7FF", action: weather, url: "forecast.html" }
  - { label: Radio, icon: radio, accent: "#E76F51", url: "radio.html" }
  - { label: PBS Kids, icon: pbskids, accent: "#2638C4", url: "https://pbskids.org/videos/" }
  - { label: Remote, icon: phone, accent: "#5CC8FF", action: remote, url: "remote-qr.html" }
```

The grid is **4 columns × 3 rows** at 1080p:

- Row 1: Jellyfin, YouTube, Netflix, Disney+
- Row 2: Read, Wi-Fi, Spotify, Weather
- Row 3: Radio, PBS Kids, Remote, empty

All eleven tiles fit on one screen; the last cell is intentionally empty for a
future app. Adding one more fills it; a thirteenth tile starts a fourth row and
the grid scrolls (the header and footer stay fixed). Arrow keys move focus,
Enter opens, and the focus navigation is geometry-based so the full grid and the
weather tile do not break it.

Tile labels are text-only by design — no subtitles, descriptions or “web app”
badges. To add a tile, add an entry to the list; the order is row-major. The
phone remote uses this same list for its app grid, so the order also determines
which tile a remote button launches.

### Wi-Fi QR

The Wi-Fi tile is a normal one-cell tile with a ~100px (9.5vh) preview QR,
`WI-FI`, and the SSID. Selecting it opens `wifi.html` with a 540px code. The
password is never rendered as text; the QR payload uses the standard
`WIFI:T:WPA;S:...;P:...;H:false;;` form and is escaped by
`ansible/filter_plugins/wifi.py`.

The SSID and PSK are encrypted in `host_vars/travel-lab/vault.yml`. The
group-vars `REPLACE_ME` values are only fallbacks.

### Phone remote tile

The Remote tile is a normal one-cell tile with a preview of the remote URL's
QR. Selecting it opens `remote-qr.html`, which shows the same code at 540px
plus the short URL. The QR encodes
`https://travel-tv.<tailnet>/r`, and the Go app serves `remote.html` for `/r`,
so the phone lands on the controller with no extra redirect.

The QR is generated every deploy (it contains no secrets), independently of
the Wi-Fi QR.

### Weather and forecast

The weather tile is a normal focusable tile and opens `forecast.html`.

`forecast.html` shows:

1. current icon, temperature, condition, city, and today's high/low;
2. a **12-hour smooth line graph** with an icon per hour and hour labels —
   no per-hour temperature labels, no hourly precipitation;
3. **five daily cards** with day name, icon, high/low, and max precipitation
   chance.

The home and forecast pages share one Open-Meteo request (current, 5 daily
rows, 5 days of hourly data). The graph starts at the current hour and takes
the next 12 hourly entries. The result is cached in `localStorage` for 30
minutes; opening the forecast page renders from that cache immediately and
refreshes when stale.

Weather settings live in `group_vars/travel_lab.yml`:

```yaml
travel_lab_weather_units: fahrenheit   # or celsius
travel_lab_weather_latitude: ""
travel_lab_weather_longitude: ""
travel_lab_weather_city: ""
```

With latitude/longitude empty, the page resolves the location from the travel
router's public IP via `ipwho.is`; set all three to pin a location (useful for
VPNs, CGNAT, or privacy). The public IP is sent to `ipwho.is`, and coordinates
are sent to Open-Meteo. A laptop preview resolves the laptop's public IP, not
the Pi's.

`Backspace` / `Esc` / `Alt+←` return from the forecast page; `Alt+Home` returns
to the launcher.

### Radio

The Radio tile opens `radio.html`, a local page with a curated six-station
lineup. No external app or login is involved; playback uses the browser's
`<audio>` element and the Pi's HDMI audio path.

Stations are defined in `group_vars/travel_lab.yml`:

```yaml
travel_lab_radio_stations:
  - { name: Groove Salad, subtitle: SomaFM, accent: "#7FB2A6",
      url: "https://ice1.somafm.com/groovesalad-128-mp3" }
  - { name: Drone Zone, subtitle: SomaFM, accent: "#6C8EBF",
      url: "https://ice1.somafm.com/dronezone-128-mp3" }
  - { name: NTS 1, subtitle: London, accent: "#E4572E",
      url: "https://stream-relay-geo.ntslive.net/stream" }
  - { name: Radio Paradise, subtitle: Eclectic, accent: "#3A7CA5",
      url: "https://stream.radioparadise.com/aac-320" }
  - { name: CPR News, subtitle: Colorado, accent: "#1B998B",
      url: "/radio/cpr-news" }
  - { name: KUVO Jazz, subtitle: Denver, accent: "#8E6C88",
      url: "https://kuvo.streamguys1.com/kuvo-aac-128" }
```

The page has a large play/pause button, previous/next station, a volume slider,
and the six station tiles. Enter plays the focused station, Space toggles
play/pause, `+` / `-` adjusts volume, and `Backspace` / `Esc` returns.

CPR News is HTTP-only upstream, so the `travel-tv` Go app proxies
`/radio/cpr-news` to `stream2.cprnetwork.org`; the other stations are direct
HTTPS streams. All stations require internet, but none require an account or
DRM.

## 5. External services

- **YouTube** uses the unofficial Leanback/TV UI at `youtube.com/tv`. It is
  enabled by a per-site PS4/Cobalt user-agent override in the Firefox profile.
  Google restricts this UI; if it stops working, the tile still opens normal
  YouTube and you can use browser zoom.
- **Netflix** and **Disney+** have no TV web UI. `netflix.com/tv2` and
  `disneyplus.com/begin` are device-activation pages, not browsing interfaces.
  The tiles open the normal desktop web apps. Use `Ctrl++` once per site to
  zoom; Firefox stores the per-site zoom in the kiosk profile.
- **PBS Kids** opens `pbskids.org/videos/` — free, no login, full episodes
  (Daniel Tiger, Wild Kratts, etc.). It is the closest web equivalent of the
  Roku PBS Kids app, though it may not have every episode. The adult PBS site
  is `pbs.org/video/`; some episodes are free, full seasons usually need PBS
  Passport, and Live TV is at `pbs.org/livestream/`.
- **Spotify** opens the web player at `open.spotify.com`; log in once. It is a
  desktop layout and playback needs Widevine. Use `Ctrl++` if the player is too
  small on the TV.
- Netflix/Disney+/Spotify playback needs Widevine. Raspberry Pi OS normally provides
  `libwidevinecdm0`; the deploy tries to install it and warns if it is
  unavailable. Linux browsers are commonly limited to 720p.
- If the desktop web apps feel wrong on the TV, use a streaming stick on
  another HDMI input; the Pi cannot emulate a smart-TV Netflix/Disney+ app.

## 6. One-time Firefox/Jellyfin setup

The profile is persistent, so these are done once (or after clearing it):

1. Open **Jellyfin**, log in, and set **Settings → Display → Display
   Mode → TV**. Verify this does not switch your laptop's Jellyfin web session
   to TV mode too; if it does, use a dedicated `tv` Jellyfin user.
2. Log in to Netflix, Disney+, Spotify and (if needed) YouTube.
3. On Netflix, Disney+ and Spotify, press `Ctrl++` once or twice to zoom the desktop UI.
4. Test Jellyfin/YouTube/Netflix/Disney+/Spotify, PBS Kids and the Radio page before travelling.

Keyboard shortcuts:

| Key | Action |
|---|---|
| Arrows | move between tiles |
| Enter | open |
| `Alt+←` | browser back |
| `Alt+Home` | launcher (homepage) |
| `Backspace` / `Esc` | back from the Wi-Fi, forecast and radio pages |
| Radio: Enter | play the focused station |
| Radio: Space | play/pause |
| Radio: `+` / `-` | volume |

A later `keyd` mapping can translate an air-mouse's Back/Home buttons if the
remote does not send the browser shortcuts.

## 7. Audio

Firefox needs a PulseAudio/PipeWire endpoint. The deploy installs PipeWire,
PipeWire-Pulse and WirePlumber, enables their user units, and leaves the
default sink to WirePlumber. HDMI audio appears only while a TV is connected.

Check and fix the sink on the Pi:

```bash
systemctl --user status pipewire wireplumber
wpctl status
wpctl set-default <HDMI-sink-id>
```

If playback is silent, restart the kiosk after the TV is connected:
`just tv-restart`.

## 8. Operate

```bash
just tv-status          # systemctl status tv-kiosk
just tv-logs            # journalctl -u tv-kiosk -f
just tv-restart         # sudo systemctl restart tv-kiosk (after page/profile edits)
just tv-page            # port-forward 18085 for a laptop preview
just tv-remote-status   # systemctl --user status travel-tv
just tv-remote-logs     # journalctl --user-unit travel-tv.service -f
just tv-remote-health   # curl http://127.0.0.1:18085/api/health
just vault-view         # show the decrypted Wi-Fi values
```

The page is also at `https://travel-tv.beaver-cloud.ts.net`.

Page/QR changes are served immediately (`Cache-Control: no-store`); Firefox may
still need `just tv-restart` after a profile or `user.js` change. The supervisor
reloads the launcher when the TV is replugged. The Go app is a normal stack
quadlet and restarts independently of the kiosk.

## 9. Phone remote

Open `https://travel-tv.<tailnet>/remote.html` on a phone on the tailnet, or
scan the **Remote** tile on the TV (the QR encodes the short
`https://travel-tv.<tailnet>/r`). It is a normal static page, so it can be
bookmarked or added to the home screen.

The page has four tabs:

- **Controls** — the D-pad, Back (`Alt+Left`), Home (`Alt+Home`), click/scroll
  quick actions, Play/Pause and the TV volume slider;
- **Shortcuts** — the app grid built from `travel_lab_tv_tiles`; tapping an app
  sends `Alt+Home`, waits, walks to the tile with arrow keys, and presses Enter;
- **Type** — a text composer plus Enter, Backspace, Space, Tab, Escape and
  Clear. The phone keyboard only appears on this tab;
- **Trackpad** — a larger, last-resort pointer pad with click and scroll
  actions.

The page is a fixed `100dvh` shell with no page scroll; the tabs sit at the
top. It is sized for an iPhone 13 mini (375×812) and respects the safe-area
insets. The static layout mockup lives at
`docs/mockups/tv-remote-layout.html`; it is not deployed.

It works in Jellyfin, YouTube, Netflix, Spotify and PBS Kids as well as the
launcher, because the API injects input at the Wayland compositor. It is not
limited to pages this repository owns.

### How it works

- `travel-tv` is one Go container in the normal rootless Podman stack. It
  serves the rendered pages and `/api/` from the same port (`8080` in the
  container, `127.0.0.1:18085` on the host).
- It mounts the kiosk runtime directory so it can connect to Cage's Wayland
  socket and the host's `pipewire-pulse` socket. No Unix socket or extra
  container is involved.
- Keyboard and text use the bundled `wtype`; pointer actions use the bundled
  `wlrctl`. All helpers are invoked without a shell and the API checks every
  value against a small allowlist.
- Volume is controlled directly over the PulseAudio protocol by the Go app,
  which prefers the sink whose name/description contains `hdmi` and falls back
  to the default sink. If the TV's audio is silent, check
  `wpctl status` / `wpctl set-default <HDMI-sink-id>` (section 7).

### Requirements and limits

- The image is built by GitHub Actions from [`travel-tv/`](../travel-tv/) and
  published to GHCR. The host role still warns if the installed Cage lacks
  virtual-input support; Cage 0.2.0+ (the Pi has 0.3.1) advertises the virtual
  keyboard and pointer protocols.
- App launching is a timing-based key sequence. The default wait after
  `Alt+Home` is 2 seconds; raise `travel_lab_tv_remote_settle_ms` in
  `group_vars/travel_lab.yml` if a slow TV page causes missed launches.
- The remote is tailnet-only but **unauthenticated**, like the rest of the
  stack. Anyone who can reach `svc:travel-tv` can type into the TV.

## 10. Troubleshooting

- **Black screen with a TV connected** — `just tv-logs`. Check that
  `/sys/class/drm/card*-HDMI-A-*/status` says `connected`, that
  `dtoverlay=vc4-kms-v3d` is in `/boot/firmware/config.txt`, and that
  `curl -I https://travel-tv...` works on the Pi.
- **Jellyfin/Kiwix tiles fail** — `/mnt/storage` is not mounted. Their units
  carry `ConditionPathIsMountPoint=/mnt/storage`, so they stay inactive; the
  launcher and the other tiles still work. Connect the NVMe and re-run
  `just deploy` (or start both units manually).
- **`cage` exits immediately / permission denied on `/dev/dri`** — the
  `PAMName=cage` session or `video`/`render` groups are not taking effect.
  Re-run `just site` (or `host-prep`) and reboot; the service must run on
  `tty1`, not as a bare user process.
- **Firefox shows chrome or a first-run page** — the profile prefs did not
  apply. Check `~/.local/share/travel-tv/firefox/user.js`, then
  `just tv-restart`. `userChrome.css` is a backup for `--kiosk`.
- **Wi-Fi QR wrong** — `just vault-view`; edit with `just vault-edit` and
  redeploy. The QR is generated on the workstation with `qrencode`.
- **Remote says TV offline** — `just tv-remote-status` and
  `just tv-remote-health`. With no HDMI display connected, Cage is not running,
  so there is no Wayland socket and the API correctly reports offline;
  `just tv-status` confirms the supervisor is idle. If `health` says
  `"wtype": false` or `"wlrctl": false`, the image is missing the helpers;
  redeploy the `travel-tv` container.
- **Remote buttons do nothing but health is OK** — `just tv-remote-logs` and
  check `last_error`. A missing virtual-input protocol or a lost Wayland socket
  is reported there. From the Pi, verify with
  `podman exec travel-tv wtype -k Right` while the launcher is visible.
- **Remote app launch misses** — the sequence waits
  `travel_lab_tv_remote_settle_ms` after `Alt+Home`. Raise it and redeploy; a
  slow page unload is the usual cause. D-pad navigation still works normally.
- **Trackpad moves but the TV cursor is invisible** — Cage shows the cursor
  only while a pointer device is present; the virtual pointer should make it
  appear. Check the TV input and `podman exec travel-tv wlrctl pointer move 20 0`.
- **Remote volume does nothing** — the app prefers an HDMI sink, otherwise the
  default sink. Check `wpctl status`; if HDMI audio is not the default, set it
  once with `wpctl set-default <HDMI-sink-id>`. Also check
  `systemctl --user status pipewire-pulse`; the app needs its Pulse socket.
- **4K TV looks cramped or slow** — the CSS scales with `vh`/`vw`, but 4K
  Firefox on a Pi 4 is heavy. Prefer the TV's 1080p mode; forcing 1080p at the
  source is the easiest fix.
- **YouTube is the normal desktop UI** — the UA override stopped working.
  The Leanback UI is unofficial; use normal YouTube with zoom, or revisit the
  UA string.
- **Netflix/Disney+ refuse to play** — check `about:support` → DRM/Widevine,
  and `dpkg -l | grep widevine`. Without the CDM, use a streaming stick.

## 11. Teardown

```bash
just tv-teardown
```

Stops and disables `tv-kiosk.service`, removes the unit, `/etc/pam.d/cage` and
the supervisor. It also removes any legacy `travel-tv-remote` user unit and
script left from the pre-Go install. It leaves the packages, the Firefox
profile, the rendered page and the `travel-tv` container. `just teardown`
removes the stack/container (including the phone remote); the profile and page
files are intentionally left behind.
