# travel-home-lab

Rootless Podman Quadlets for the travel lab stack (kiwix, jellyfin, gatus,
glances, dozzle, homer, whoami) on `travel-lab` (Raspberry Pi 4, aarch64),
deployed with **Ansible**.

Ingress is **Tailscale Services**, not a reverse proxy: each container
publishes on `127.0.0.1` only, and tailscaled terminates TLS on `tailscale0:443`
and proxies each `<service>.beaver-cloud.ts.net` name to its loopback port. The
Services are declared with `tailscale serve --service … --https=443` (the CLI
form — the huJSON config file cannot request an HTTPS listener). There is no
Caddy, no internal CA, and nothing on `.lan`. See
[docs/tailscale-serve-migration.md](docs/tailscale-serve-migration.md).

The stack ran under `podman compose`, then under hand-written quadlets installed
by a `Makefile`. Both are retired; see
[docs/ansible-migration.md](docs/ansible-migration.md) and
[docs/quadlet-migration.md](docs/quadlet-migration.md).

Ansible runs on the workstation and pushes to the Pi over Tailscale. **The
repository no longer needs to exist on the Pi** — the quadlets and config files
are rendered onto it.

## Prerequisites (workstation)

```bash
ansible-galaxy collection install -r ansible/requirements.yml
```

`just` is optional; every recipe maps to a plain `ansible-playbook` command.

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

## What lands on the Pi

```
~/.config/containers/systemd/           # 11 quadlet units: 1 network, 3 volumes, 7 containers
~/.config/gatus/config.yml
~/.config/homer/config.yml
```

All of it is Ansible-owned: edit the templates or
[`ansible/group_vars/travel_lab.yml`](ansible/group_vars/travel_lab.yml) in this
repo and re-run `deploy`. Hand-edits on the Pi are overwritten on the next run.
The units use the systemd `%h` specifier, so nothing is tied to the username.

## Operate

From the repository root:

```bash
just status
just logs kiwix
just serves
just ssh 'podman ps'
```

Or on the Pi directly:

```bash
systemctl --user status jellyfin
journalctl --user -u kiwix -f
tailscale serve status
```

Quadlets autostart at boot via `[Install] WantedBy=default.target` plus
`Linger=yes`; do **not** run `systemctl --user enable` (generated units reject
it).

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
- One extra **anonymous** hex-named volume is normal: the Jellyfin image declares
  `VOLUME /cache`, and since only `/config` is mounted explicitly, `/cache` gets an
  anonymous volume. It is throwaway transcoding cache and goes away when the
  container stops.
- **Do not run compose and the quadlets at the same time.** tailscaled holds
  `:443` and the compose Caddy would want it too, plus the container names would
  collide.

## Auto-update

`travel_lab_auto_update: true` (in `group_vars`) stamps `AutoUpdate=registry`
into every unit **and** enables `podman-auto-update.timer`. This is a change
from the pre-Ansible state, where the timer was disabled and `AutoUpdate=` did
nothing. See [docs/ansible-migration.md](docs/ansible-migration.md) for the
caveat about `:latest` on the stateful services.

## Tear down

```bash
just teardown
```

Stops the stack and removes the quadlet units this repo manages. Images, the
five volumes, and the rendered config files are left alone.

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
