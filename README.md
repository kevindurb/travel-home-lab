# travel-home-lab

Rootless Podman Quadlets for the travel lab stack (kiwix, jellyfin, gatus,
glances, dozzle, homer, travel-tv, moonlight, whoami) on `travel-lab` (Raspberry
Pi 4, aarch64), deployed with **Ansible**. The Pi's HDMI output can also run a
10ft **Travel TV** kiosk (Cage + Firefox); see
[docs/hdmi-tv.md](docs/hdmi-tv.md). Game streaming from a Sunshine host through
the kiosk is in [docs/moonlight.md](docs/moonlight.md).

Ingress is **Tailscale Services**, not a reverse proxy: each container
publishes on `127.0.0.1` only, and tailscaled terminates TLS on `tailscale0:443`
and proxies each `<service>.beaver-cloud.ts.net` name to its loopback port. The
Services are declared with `tailscale serve --service … --https=443` (the CLI
form — the huJSON config file cannot request an HTTPS listener). There is no
Caddy, no internal CA, and nothing on `.lan`. See
[docs/tailscale-serve-migration.md](docs/tailscale-serve-migration.md).

Moonlight is the one exception: its UI stays on loopback (the kiosk loads it
from `127.0.0.1`, a secure context) and its WebRTC UDP media range is also
published on the tailnet address. See [docs/moonlight.md](docs/moonlight.md).

The stack ran under `podman compose`, then under hand-written quadlets installed
by a `Makefile`. Both are retired; see
[docs/ansible-migration.md](docs/ansible-migration.md) and
[docs/quadlet-migration.md](docs/quadlet-migration.md).

Ansible runs on the workstation and pushes to the Pi over Tailscale. **The
repository no longer needs to exist on the Pi** — the quadlets and config files
are rendered onto it.

The **travel-tv** container is the one image built from this repository: a Go
app in [`travel-tv/`](travel-tv/) that serves the launcher and phone remote and
injects Wayland input. GitHub Actions publishes it to
`ghcr.io/kevindurb/travel-home-lab/travel-tv`; the Pi pulls it like any other
stack image.

## Prerequisites (workstation)

```bash
ansible-galaxy collection install -r ansible/requirements.yml
```

The Wi-Fi QR is rendered on the workstation with `qrencode`:

```bash
brew install qrencode       # or apt/dnf install qrencode
```

The encrypted Wi-Fi credentials live in
`ansible/host_vars/travel-lab/vault.yml` and are unlocked with
`~/.vault_pass` (already configured in `ansible/ansible.cfg`). Use
`just vault-edit` to change them. `just` is optional; every recipe maps to a
plain `ansible-playbook` command.

## Deploy

From the repository root (the `justfile` lives there):

```bash
just site      # host prerequisites (prompts for sudo) + deploy the stack
just deploy    # stack only, no sudo
just check     # dry run: --check --diff
```

Without `just`, run `ansible-playbook` from `ansible/` (where `ansible.cfg`
lives):

```bash
cd ansible
ansible-playbook -i inventory/prod.yml site.yml --ask-become-pass
ansible-playbook -i inventory/prod.yml deploy.yml
```

`host-prep` needs the Pi's sudo password (`--ask-become-pass`); `deploy` does
not.

If `/mnt/storage` is not mounted, the deploy still converges: Jellyfin and
Kiwix are left inactive because their units carry
`ConditionPathIsMountPoint=/mnt/storage`. The rest of the stack, including the
Travel TV service, starts normally. Connect the NVMe and re-run `just deploy`
(or start the two units manually) when you want them back.

## What lands on the Pi

```
~/.config/containers/systemd/           # 13 quadlet units: 1 network, 3 volumes, 9 containers
~/.config/gatus/config.yml
~/.config/homer/config.yml
~/.config/moonlight/                    # moonlight-web-stream data.json (users, pairings)
~/.config/systemd/user/pi-health.*      # temperature / under-voltage collector timer
~/.local/bin/pi-health                  # collector pushed to Gatus
~/.config/travel-tv/www/                # rendered page, QR codes and remote
~/.local/share/travel-tv/firefox/       # persistent kiosk profile
/etc/systemd/system/tv-kiosk.service    # HDMI kiosk (system unit)
```

All of it is Ansible-owned: edit the templates or
[`ansible/group_vars/travel_lab.yml`](ansible/group_vars/travel_lab.yml) in this
repo and re-run `deploy`. Hand-edits on the Pi are overwritten on the next run.
The units use the systemd `%h` specifier, so nothing is tied to the username.

## Travel TV

`just site` installs a Cage + Firefox kiosk on the Pi's HDMI output. It waits
for a connected display and the `https://travel-tv.<tailnet>` page, then shows
the 10ft launcher (Jellyfin, YouTube, Netflix, Disney+, Spotify, Kiwix, a
Wi-Fi QR, weather/forecast, Radio, PBS Kids, a phone-remote QR and Games). No
desktop environment is used.

A cog in the header opens Settings: a **Bluetooth** page for pairing and
connecting controllers, and an **About** page. A paired controller drives every
first-party page (D-pad/stick, A, B, Start, LB/RB) and the Moonlight stream owns
it while a game is open. The **Games** tile streams from a Sunshine host (the
gaming PC or the Steam Deck) through `moonlight-web-stream`; setup, H.264/1080p
settings and the loopback/WebRTC network details are in
[docs/moonlight.md](docs/moonlight.md).

A phone remote at `https://travel-tv.<tailnet>/remote.html` controls the kiosk
with tabs for Controls, Shortcuts, Type and Trackpad: D-pad, trackpad, app
launch, volume and text entry. Scan the Remote tile's QR (or open the short
`https://travel-tv.<tailnet>/r`) to get there.

The launcher and remote are served by the `travel-tv` Go container; there is no
nginx or Python user service. The image bundles `wtype`/`wlrctl` and controls
volume through `pipewire-pulse`.

Prerequisites: create `svc:travel-tv` in the Tailscale admin console and set the
Wi-Fi vault values with `just vault-edit`. Full details, one-time
Jellyfin/Netflix setup, audio notes and troubleshooting are in
[docs/hdmi-tv.md](docs/hdmi-tv.md); game streaming is in
[docs/moonlight.md](docs/moonlight.md).

## Operate

From the repository root:

```bash
just status
just logs kiwix
just serves
just ssh 'podman ps'
just tv-status
just tv-logs
just tv-remote-status
just tv-remote-logs
just tv-remote-health
just moonlight-status
just moonlight-logs
just bluetooth
just pi-health
just pi-health-logs
just tv-test
just vault-view
```

Or on the Pi directly:

```bash
systemctl --user status jellyfin
journalctl --user -u kiwix -f
tailscale serve status
systemctl status tv-kiosk
systemctl --user status travel-tv
```

Quadlets autostart at boot via `[Install] WantedBy=default.target` plus
`Linger=yes`; do **not** run `systemctl --user enable` (generated units reject
it).

## Pi health checks

`pi-health.timer` runs every 30 seconds and pushes two results to Gatus:

- `pi-thermal` — red at 80 °C, or while the Pi reports it is throttling or at
  the soft temperature limit.
- `pi-undervolt` — red while the Pi's under-voltage alarm is active.

The collector reads `/sys` directly (`/sys/class/thermal` and the `rpi_volt`
hwmon alarm), with `vcgencmd` as a fallback, so it needs no privileges. Gatus
marks both checks unhealthy if no push arrives for five minutes, which also
catches a dead timer. No alert providers are configured, so these are
dashboard-only under the `monitors` group.

## ⚠️ Do not destroy the volumes

Five named volumes exist. Three hold state that is painful to recreate; two are
**retired Caddy volumes** that nothing references any more (kept only so a
rollback can restore the old internal CA):

| Volume | Holds |
|---|---|
| `travel-home-lab_jellyfin_config` | Jellyfin library setup, users, watched state |
| `travel-home-lab_gatus_data` | uptime history (`data.db` + WAL) |
| `travel-home-lab_dozzle_data` | (empty) |
| `travel-home-lab_caddy_data` | *(retired)* Caddy internal PKI / issued certs |
| `travel-home-lab_caddy_config` | *(retired)* Caddy autosave config |

- The live `.volume` units reuse these names, so nothing is created or migrated.
  Do not rename them.
- **Never run `podman compose down -v`.** The volumes carry
  `com.docker.compose.project=travel-home-lab` labels, so `-v` deletes all five.
  Plain `podman compose down` is safe.
- **Do not run `podman volume prune`** unless the quadlet containers are running
  and holding the volumes. Prune removes any volume not referenced by a *running*
  container — including the three live ones whenever the stack is stopped. (The
  two retired Caddy volumes are unreferenced and *are* prunable; that is fine.)
- **Jellyfin config is a named volume.** `/config` (library setup, users, watched
  state) is `travel-home-lab_jellyfin_config`; back it up. The throwaway
  `/cache` is a bind mount under `/mnt/storage/jellyfin/cache` on the USB SSD,
  keeping the heavy HLS/trickplay writes off the SD card.
- **Do not run compose and the quadlets at the same time.** tailscaled holds
  `:443` and the compose Caddy would want it too, plus the container names would
  collide.

## Auto-update

`travel_lab_auto_update: true` (in `group_vars`) stamps `AutoUpdate=registry`
into every unit **and** enables `podman-auto-update.timer`. This is a change
from the pre-Ansible state, where the timer was disabled and `AutoUpdate=` did
nothing. See [docs/ansible-migration.md](docs/ansible-migration.md) for the
caveat about `:latest` on the stateful services.

The `travel-tv` image is rebuilt by GitHub Actions on every `main` push under
`travel-tv/` and published as `:latest`. The package is public, so the Pi pulls
it anonymously and the auto-update timer picks it up. If you make the package
private, run `podman login ghcr.io` on the Pi first.

## Tear down

```bash
just teardown       # stack: stop and remove the quadlet units
just tv-teardown    # kiosk: stop/remove the kiosk; removes legacy remote artifacts
```

Stops the stack and removes the quadlet units this repo manages. Images, the
five volumes, and the rendered config files are left alone. `tv-teardown`
also leaves the Firefox profile and page files; the phone remote lives in the
`travel-tv` container, so `just teardown` removes it. See
[docs/hdmi-tv.md](docs/hdmi-tv.md#11-teardown).

## Rollback

The pre-Ansible quadlet + Makefile install is in git history (`eff8423` and
earlier), and the compose stack is recoverable from `2b28155`:

```bash
git show 2b28155:compose.yml > compose.yml
git show 2b28155:sample.env  > sample.env
# copy both to the Pi, then: podman compose up -d
```

The Pi's old clone (and its untracked `.env`) has been removed, so these steps
run from this repository. The old `.env` values were
`STORAGE_PATH=/mnt/storage`, `HOST=travel-lab.lan`, `PORT=0.0.0.0:443`.
