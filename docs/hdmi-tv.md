# Travel TV (HDMI kiosk)

The Travel TV turns the Pi's HDMI output into a 10ft hotel-room launcher:
Jellyfin, YouTube, Netflix, Disney+, Read (Kiwix), a Wi-Fi QR, and a
weather tile with a 12-hour/5-day forecast. It is served by a small nginx
container and displayed by Cage + Firefox on `tty1`, with no desktop
environment.

```
                    tailnet                         HDMI
  phone/laptop ──► svc:travel-tv ──► nginx:18085 ──► Cage ──► Firefox
                                                        │
                                                        └──► launcher page
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
| `~/.config/travel-tv/www/` | rendered launcher (`index.html`, `wifi.html`, CSS, JS, QR) |
| `~/.config/travel-tv/nginx.conf` | nginx config (`Cache-Control: no-store`) |
| `~/.local/share/travel-tv/firefox/` | persistent Firefox profile (logins, zoom, TV mode) |
| `/etc/systemd/system/tv-kiosk.service` | Cage + Firefox supervisor on `tty1` |
| `/etc/pam.d/cage` | logind session stack for Cage |
| `/usr/local/bin/travel-tv-kiosk` | waits for HDMI + URL, then runs Cage |

The `travel-tv` nginx container is part of the normal stack and publishes
`127.0.0.1:18085`; `tailscale serve` fronts it as `svc:travel-tv`.

## 2. Before the first deploy

1. Create `svc:travel-tv` in the Tailscale admin console (same as the other
   services). The deploy will fail to publish it otherwise.
2. Make sure `qrencode` is available on the workstation that runs Ansible.
   The QR is generated there and only the SVG is copied to the Pi, so the PSK
   never appears in the Pi's process list or logs. If it is not on `PATH`,
   set `travel_lab_tv_qrencode_bin`.
3. The vault password file is `~/.vault_pass`; `ansible/ansible.cfg` points at
   it. The encrypted Wi-Fi values live in
   `ansible/host_vars/travel-lab/vault.yml`.
4. Fill in the placeholders:

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
```

The grid is **4 columns × 2 rows** at 1080p:

- Row 1: Jellyfin, YouTube, Netflix, Disney+
- Row 2: Read, Wi-Fi, Weather, **empty**

The empty eighth cell is intentional. Adding a tile fills it; more tiles create
a third row and the grid scrolls (the header and footer stay fixed). Arrow keys
move focus, Enter opens, and the focus navigation is geometry-based so the
empty cell and the weather tile do not break it.

Tile labels are text-only by design — no subtitles, descriptions or “web app”
badges. To add a tile, add an entry to the list; the order is row-major.

### Wi-Fi QR

The Wi-Fi tile is a normal one-cell tile with a ~160px (15vh) preview QR,
`WI-FI`, and the SSID. Selecting it opens `wifi.html` with a 540px code. The
password is never rendered as text; the QR payload uses the standard
`WIFI:T:WPA;S:...;P:...;H:false;;` form and is escaped by
`ansible/filter_plugins/wifi.py`.

The SSID and PSK are encrypted in `host_vars/travel-lab/vault.yml`. The
group-vars `REPLACE_ME` values are only fallbacks.

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

## 5. External services

- **YouTube** uses the unofficial Leanback/TV UI at `youtube.com/tv`. It is
  enabled by a per-site PS4/Cobalt user-agent override in the Firefox profile.
  Google restricts this UI; if it stops working, the tile still opens normal
  YouTube and you can use browser zoom.
- **Netflix** and **Disney+** have no TV web UI. `netflix.com/tv2` and
  `disneyplus.com/begin` are device-activation pages, not browsing interfaces.
  The tiles open the normal desktop web apps. Use `Ctrl++` once per site to
  zoom; Firefox stores the per-site zoom in the kiosk profile.
- Netflix/Disney+ playback needs Widevine. Raspberry Pi OS normally provides
  `libwidevinecdm0`; the deploy tries to install it and warns if it is
  unavailable. Linux browsers are commonly limited to 720p.
- If the desktop web apps feel wrong on the TV, use a streaming stick on
  another HDMI input; the Pi cannot emulate a smart-TV Netflix/Disney+ app.

## 6. One-time Firefox/Jellyfin setup

The profile is persistent, so these are done once (or after clearing it):

1. Open **Jellyfin**, log in, and set **Settings → Display → Display
   Mode → TV**. Verify this does not switch your laptop's Jellyfin web session
   to TV mode too; if it does, use a dedicated `tv` Jellyfin user.
2. Log in to Netflix, Disney+ and (if needed) YouTube.
3. On Netflix and Disney+, press `Ctrl++` once or twice to zoom the desktop UI.
4. Test Jellyfin/YouTube/Netflix/Disney+ playback and audio before travelling.

Keyboard shortcuts:

| Key | Action |
|---|---|
| Arrows | move between tiles |
| Enter | open |
| `Alt+←` | browser back |
| `Alt+Home` | launcher (homepage) |
| `Backspace` / `Esc` | back from the Wi-Fi and forecast pages |

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
just vault-view         # show the decrypted Wi-Fi values
```

The page is also at `https://travel-tv.beaver-cloud.ts.net`.

Page/QR changes are served immediately (nginx `no-store`); Firefox may still
need `just tv-restart` after a profile or `user.js` change. The supervisor
reloads the launcher when the TV is replugged.

## 9. Troubleshooting

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
- **4K TV looks cramped or slow** — the CSS scales with `vh`/`vw`, but 4K
  Firefox on a Pi 4 is heavy. Prefer the TV's 1080p mode; forcing 1080p at the
  source is the easiest fix.
- **YouTube is the normal desktop UI** — the UA override stopped working.
  The Leanback UI is unofficial; use normal YouTube with zoom, or revisit the
  UA string.
- **Netflix/Disney+ refuse to play** — check `about:support` → DRM/Widevine,
  and `dpkg -l | grep widevine`. Without the CDM, use a streaming stick.

## 10. Teardown

```bash
just tv-teardown
```

Stops and disables `tv-kiosk.service`, removes the unit, `/etc/pam.d/cage` and
the supervisor. It leaves the packages, the Firefox profile, the rendered page
and the `travel-tv` container. `just teardown` removes the stack/container;
the profile and page files are intentionally left behind.
