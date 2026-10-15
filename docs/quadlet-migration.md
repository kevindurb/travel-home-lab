# Converting travel-home-lab to Podman Quadlets

**Status:** plan — reviewed, not yet applied
**Target:** `travel-lab` (Raspberry Pi 4, aarch64) — Podman **5.4.2**, rootless user
`kevindurb` (uid 1000), `Linger=yes`
**Date:** 2026-10-15

---

## 1. Recommendation

Move to **rootless user quadlets** — one file per service — and install them by
**symlinking this repo into `~/.config/containers/systemd/`** with a `make install`
target.

Three decisions up front:

| Decision | Choice | Why |
|---|---|---|
| Unit type | rootless **user** quadlets | matches how the stack runs today; no sudo, no root-owned container state |
| Topology | one `.container` per service on a shared `.network` | `Caddyfile` and `gatus/config.yml` keep working unchanged (see §5) |
| Install | absolute **symlinks** + `daemon-reload` | repo stays the single source of truth, no copy drift (see §7) |

**Existing volumes are reused in place** — no data migration, nothing copied.
Quadlet emits `podman volume create --ignore`, so pointing at the current
compose-named volumes preserves Jellyfin's library state, gatus history, and
Caddy's internal CA. See §6.3.

---

## 2. Why quadlets

- **`podman generate systemd` is gone** (deprecated 4.4, removed 5.0). Quadlets are
  the supported path for "compose stack that behaves like a system service".
- **Real systemd units.** Proper `After=`/`Requires=` ordering, `Restart=`
  semantics, journald logs (`journalctl --user -u kiwix`), `systemctl` control.
  Compose containers are opaque to systemd.
- **No compose shim.** Today `podman compose` shells out to the external
  `docker-compose` provider and prints a version-skew warning on every invocation.
  Quadlets drop that layer entirely.
- **Native `AutoUpdate=registry`**, and the ability to guard on the storage mount.

**What we give up:** compose's single-file view of the stack. Seven files instead
of one. Mitigated by keeping them in this repo and installing via symlink.

---

## 3. Verified behaviour

Everything below was tested against a live Podman (6.1.2 on `dev`, plus a smoke test
on the Pi's 5.4.2) before being written down.

| Behaviour | Result |
|---|---|
| Quadlets in `~/.config/containers/systemd/` generate services | ✅ works |
| **Autostart** from `[Install] WantedBy=default.target` | ✅ generator creates `default.target.wants/` symlinks |
| `systemctl --user enable <quadlet>.service` | ❌ **fails** — `transient or generated`. Not needed. |
| Symlinked quadlet files (not real files) | ✅ generate fine |
| Relative `Volume=./x` | ⚠️ resolves to **the quadlet file's directory**, not the repo — **do not use** |
| `%h` in `Volume=` | ✅ expands to `/home/kevindurb` at runtime |
| `%t` in `Volume=` | ✅ expands to `/run/user/1000` |
| `.volume` → `Volume=x.volume:/p` | ✅ → `-v <VolumeName>:/p` |
| **`.volume` naming an existing volume** | ✅ **reused, data intact** — generates `podman volume create --ignore <name>` |
| `.network` → `Network=x.network` | ✅ → `--network x`, deps auto-wired |
| `PublishPort=443:443` | ✅ → `--publish 443:443` |
| `restart:` needs explicit `[Service] Restart=` | ✅ otherwise systemd defaults to `no` |
| Podman emits `Type=notify`, `KillMode=mixed`, `ExecStop=podman rm -v -f -i` | automatic |

**The autostart model, plainly:** the generator reads `[Install] WantedBy=default.target`
from the `.container` file and writes the service plus a symlink into
`default.target.wants/`. So quadlets start at boot **without `enable`** — and
`enable` is actively rejected. Combined with the existing `Linger=yes`, that is the
whole mechanism.

Two environment facts that make this work on the Pi:

- `Linger=yes` (already set) — the user manager starts at boot with no login.
- `net.ipv4.ip_unprivileged_port_start=80` — rootless Caddy can bind `:443`.

Also confirmed on the Pi: guardrail ordering is already in our favour.
`mnt-storage.mount` is `WantedBy=local-fs.target`, and `user@1000.service` runs
after `basic.target` (which pulls in `local-fs.target`), so `/mnt/storage` is
mounted before the user manager starts.

### Confirmed against the live Pi (2026-10-15)

Read-only recon on `travel-lab.beaver-cloud.ts.net` before writing any unit. Every
value the plan had to hardcode was checked against the running config:

| Source | Value | Used as |
|---|---|---|
| `.env` `STORAGE_PATH` | `/mnt/storage` | literal `Volume=` source |
| `.env` `HOST` | `travel-lab.lan` | `Environment=HOST=` in `caddy.container` |
| `.env` `PORT` | `0.0.0.0:443` | `PublishPort=443:443` |
| `podman compose config` | `published: "443"`, `host_ip: 0.0.0.0` | confirms the above |
| repo location | `/home/kevindurb/travel-home-lab` | the `%h/travel-home-lab/...` paths |
| `/etc/sysctl.d/99-podman-ports.conf` | `net.ipv4.ip_unprivileged_port_start=80` | rootless Caddy can bind `:443` |
| `podman volume ls` | the five `travel-home-lab_*` names | every `VolumeName=` |
| `podman ps -a` | all 7 containers `Exited` | prune hazard, §11.10 |
| `findmnt /mnt/storage` | ext4 on `/dev/sda1`, 476 G, 80 G free | §9 check 4 |
| `ls /mnt/storage/zim` | 7 `.zim` archives | §11.3 baseline |

`podman.socket` is active, `Linger=yes`, and `/usr/bin/mountpoint` exists (so the
`ExecStartPre` guard works). `~/.config/containers/systemd/` exists and is empty.

---

## 4. compose → quadlet mapping

| compose | quadlet |
|---|---|
| `services.kiwix` | `quadlets/kiwix.container` |
| `container_name: kiwix` | implicit from filename (`ContainerName=` optional) |
| `image: x` | `Image=x` |
| `command: '*.zim'` | `Exec=*.zim` (both override CMD, not ENTRYPOINT — equivalent) |
| `restart: unless-stopped` | `[Service] Restart=always` |
| `volumes: - x:/y:z` | `Volume=x:/y:z` |
| `environment: K: V` | `Environment=K=V` |
| `ports: - '443:443'` | `PublishPort=443:443` |
| `pid: host` | `PodmanArgs=--pid=host` |
| named volume `foo` | `foo.volume` with `VolumeName=` |
| implicit default network | `.network` unit + `Network=` |
| `depends_on` | `[Unit] After=` / `Wants=` |
| `${STORAGE_PATH}` interpolation | ❌ **not supported** — paths must be literal (§6.4) |

---

## 5. Topology: network, not a pod

Podman offers `.pod` units, where containers share a network namespace and talk
over `127.0.0.1`. **We are not using a pod.** Reasons:

- A pod would force rewriting `Caddyfile` (`kiwix:8080` → `127.0.0.1:8080`) and
  every `url:` in `gatus/config.yml` — unnecessary churn.
- `glances` runs with `pid: host`. Mixing host PID namespace into a pod is
  awkward at best.
- Per-service control (restart one thing without touching the rest) is lost.

With a shared `.network`, **aardvark-dns resolves container names**, so `kiwix`,
`jellyfin`, etc. resolve exactly as they do under compose. **Both config files work
unchanged.** Backend is `netavark` on the Pi.

---

## 6. Proposed repo layout and file contents

### 6.1 Layout

```
travel-home-lab/
├── Makefile                    # ← new: install/uninstall of quadlets
├── README.md                   # ← currently 0 bytes; add a pointer
├── docs/
│   └── quadlet-migration.md    # ← this document
├── quadlets/                   # ← new
│   ├── travel-home-lab.network
│   ├── caddy_data.volume
│   ├── caddy_config.volume
│   ├── jellyfin_config.volume
│   ├── gatus_data.volume
│   ├── dozzle_data.volume
│   ├── caddy.container
│   ├── dozzle.container
│   ├── gatus.container
│   ├── glances.container
│   ├── jellyfin.container
│   ├── kiwix.container
│   └── whoami.container
├── caddy/Caddyfile             # unchanged
├── gatus/config.yml            # unchanged
├── compose.yml                 # keep until migration is proven, then delete
└── sample.env                  # obsolete after migration (§6.4)
```

### 6.2 Network and volumes

Named volumes keep their **existing** compose names via `VolumeName=`, so no data
is lost — Jellyfin in particular keeps its library config and watched state.

```ini
# quadlets/travel-home-lab.network
[Network]
NetworkName=travel-home-lab
```

```ini
# quadlets/caddy_data.volume
[Volume]
VolumeName=travel-home-lab_caddy_data
```

...and the same for `caddy_config`, `jellyfin_config`, `gatus_data`, `dozzle_data`
(`travel-home-lab_<name>`). These names deliberately match the volumes that already
exist, which makes the migration a pure reuse — see §6.3.

### 6.3 Volumes: reuse the existing ones, don't migrate

**No volume migration is needed.** Verified end-to-end rather than assumed.

The mechanism: quadlet generates

```
ExecStart=/usr/bin/podman volume create --ignore <VolumeName>
```

`--ignore` means *"Don't fail if volume already exists"* (confirmed in
`podman volume create --help` on the Pi's 5.4.2). So a `.volume` unit naming a
volume that already exists is a **no-op that reuses it with data intact**.

Tested on the Pi: created a volume, seeded a canary file, `daemon-reload`,
started the generated volume unit, started a container against it — **canary
readable from inside the container**. Nothing was recreated.

So `VolumeName=` just needs to match the **existing compose-prefixed names**:

| `.volume` file | `VolumeName=` | Outcome |
|---|---|---|
| `caddy_data.volume` | `travel-home-lab_caddy_data` | reused |
| `caddy_config.volume` | `travel-home-lab_caddy_config` | reused |
| `jellyfin_config.volume` | `travel-home-lab_jellyfin_config` | reused |
| `gatus_data.volume` | `travel-home-lab_gatus_data` | reused |
| `dozzle_data.volume` | `travel-home-lab_dozzle_data` | reused |

#### What's actually at stake

| Volume | Size | Contents |
|---|---|---|
| `jellyfin_config` | **28 MB** | `config/`, `data/` (playlists, `SQLiteBackups`, `ScheduledTasks`), `metadata/library`, `plugins/`, `root/` — library setup, users, watched state |
| `gatus_data` | **4.3 MB** | `data.db` + `-shm` + a 4 MB `-wal` (uptime history) |
| `caddy_data` | 296 KB | internal PKI — `instance.uuid` plus leaf certs for all six subdomains |
| `caddy_config` | 12 KB | Caddy autosave config |
| `dozzle_data` | 4 KB | empty |

Losing these means re-adding the Jellyfin library and re-importing it, and Caddy
re-issuing its internal CA so every client must re-trust it. Reuse beats migrate.

#### ⚠️ Never run `podman compose down -v`

The volumes carry compose provenance labels:

```
com.docker.compose.project=travel-home-lab
com.docker.compose.version=2.26.1
com.docker.compose.volume=jellyfin_config
```

`-v` deletes volumes belonging to the compose project — so `podman compose down -v`
**would destroy all five**, including Jellyfin's 28 MB of state. Plain
`podman compose down` is safe and is what §8 specifies.

#### Notes

- These volumes live on the **Pi's SD card**
  (`~/.local/share/containers/storage/volumes/`), *not* the NVMe. The storage
  migration didn't touch them and this one won't either. ~32 MB total.
- `caddy_data` holds certs for **both** `*.travel-lab.local` and `*.travel-lab.lan`.
  The `.local` set is stale from an earlier `HOST` value (see the git history:
  "back to subdomains"). Caddy issues on demand, so it's harmless clutter.
- There are also 7 orphaned hex-named volumes from earlier compose churn, not
  referenced by this stack. `podman volume prune` clears them — but **only after
  the quadlet containers are running and holding the five real volumes**. Prune
  does not care about compose configs; it deletes anything no *running* container
  references, which while the stack is stopped means all five of ours. See §11.10.
- Renaming a volume would be a data copy (`podman volume create` + `rsync` between
  mountpoints). Not recommended here.

### 6.4 Why `%h/travel-home-lab/...` appears in the paths

Two verified constraints force this:

1. **Relative paths resolve to the quadlet file's directory** — i.e.
   `~/.config/containers/systemd/`, not the repo. So `./caddy` would break.
2. **Quadlet does no variable interpolation** — there is no `${STORAGE_PATH}`.

So config paths are written as `%h/travel-home-lab/...`, and `/mnt/storage` is
hardcoded. `%h`/`%t` are expanded by **systemd at runtime**, verified.

> **Constraint:** this hardcodes the repo location. **The repo must live at
> `~/travel-home-lab` on the Pi.** `make install` errors out if it does not
> (override with `FORCE=1`). This is the price of symlinks over templating, and
> it's the one real wart here.

### 6.5 Service quadlets

```ini
# quadlets/kiwix.container
[Unit]
Description=Kiwix offline ZIM server

[Container]
Image=ghcr.io/kiwix/kiwix-serve:latest
ContainerName=kiwix
Exec=*.zim
Network=travel-home-lab.network
Volume=/mnt/storage/zim:/data:z
AutoUpdate=registry

[Service]
Restart=always
# Refuse to start on an empty /mnt/storage (see §9)
ExecStartPre=/usr/bin/mountpoint -q /mnt/storage

[Install]
WantedBy=default.target
```

```ini
# quadlets/jellyfin.container
[Unit]
Description=Jellyfin media server

[Container]
Image=docker.io/jellyfin/jellyfin:latest
ContainerName=jellyfin
Network=travel-home-lab.network
Volume=/mnt/storage/media:/media:z
Volume=jellyfin_config.volume:/config
AutoUpdate=registry

[Service]
Restart=always
ExecStartPre=/usr/bin/mountpoint -q /mnt/storage

[Install]
WantedBy=default.target
```

```ini
# quadlets/caddy.container
[Unit]
Description=Caddy reverse proxy (internal TLS)

[Container]
Image=docker.io/library/caddy:latest
ContainerName=caddy
Network=travel-home-lab.network
Environment=HOST=travel-lab.lan
PublishPort=443:443
Volume=%h/travel-home-lab/caddy:/etc/caddy:z
Volume=caddy_data.volume:/data
Volume=caddy_config.volume:/config
AutoUpdate=registry

[Service]
Restart=always

[Install]
WantedBy=default.target
```

```ini
# quadlets/gatus.container
[Unit]
Description=Gatus uptime dashboard

[Container]
Image=ghcr.io/twin/gatus:latest
ContainerName=gatus
Network=travel-home-lab.network
Environment=GATUS_LOG_LEVEL=DEBUG
Environment=GATUS_CONFIG_PATH=/config
Volume=gatus_data.volume:/data
Volume=%h/travel-home-lab/gatus/config.yml:/config/config.yaml:z
AutoUpdate=registry

[Service]
Restart=always

[Install]
WantedBy=default.target
```

```ini
# quadlets/whoami.container
[Unit]
Description=whoami test endpoint

[Container]
Image=docker.io/traefik/whoami:latest
ContainerName=whoami
Network=travel-home-lab.network
AutoUpdate=registry

[Service]
Restart=always

[Install]
WantedBy=default.target
```

```ini
# quadlets/glances.container
[Unit]
Description=Glances system monitor (web mode)
# Needs the podman API socket to report container state
Requires=podman.socket
After=podman.socket

[Container]
Image=docker.io/nicolargo/glances:latest
ContainerName=glances
Network=travel-home-lab.network
Environment=GLANCES_OPT=-w
PodmanArgs=--pid=host
Volume=%t/podman/podman.sock:/run/user/1000/podman/podman.sock:roz
Volume=/etc/os-release:/etc/os-release:roz
AutoUpdate=registry

[Service]
# compose had no restart policy for glances; recommend adding one
Restart=always

[Install]
WantedBy=default.target
```

```ini
# quadlets/dozzle.container
[Unit]
Description=Dozzle container log viewer
Requires=podman.socket
After=podman.socket

[Container]
Image=docker.io/amir20/dozzle:latest
ContainerName=dozzle
Network=travel-home-lab.network
Volume=dozzle_data.volume:/data
Volume=%t/podman/podman.sock:/var/run/docker.sock:roz
AutoUpdate=registry

[Service]
Restart=always

[Install]
WantedBy=default.target
```

**Notes on the above**

- `Environment=HOST=travel-lab.lan` and `PublishPort=443:443` are the Pi's
  **confirmed** `.env` values (§3), not guesses.
- `%t/podman/podman.sock` replaces the hardcoded `/run/user/1000/podman/podman.sock`.
  Verified to expand to `/run/user/1000`. Same file, no hardcoded uid.
- `:z` (SELinux relabel) is **a no-op** — the Pi has no SELinux. Kept for parity.
- `glances` and `dozzle` gain an explicit `Requires=podman.socket`; today they
  depend on it implicitly being up.
- `AutoUpdate=registry` is inert until `podman-auto-update.timer` is enabled — see §9.

---

## 7. The install decision

**Chosen: absolute symlinks from `quadlets/` into `~/.config/containers/systemd/`,
driven by `make install`.**

```make
# Makefile
REPO        := $(shell realpath -m '$(CURDIR)')
EXPECTED    := $(shell realpath -m '$(HOME)/travel-home-lab')
QUADLET_SRC := $(REPO)/quadlets
UNIT_DIR    ?= $(HOME)/.config/containers/systemd

.PHONY: check install uninstall

check:
	@if [ "$(REPO)" != "$(EXPECTED)" ]; then \
		echo "ERROR: repo is at $(REPO)"; \
		echo "       quadlets hardcode $(EXPECTED) in their Volume= paths."; \
		if [ -z "$(FORCE)" ]; then \
			echo "       refusing to install; re-run with FORCE=1 to override."; \
			exit 1; \
		fi; \
	fi

install: check
	@mkdir -p "$(UNIT_DIR)"
	@for f in "$(QUADLET_SRC)"/*; do \
		ln -sfn "$$f" "$(UNIT_DIR)/$$(basename "$$f")"; \
		echo "linked  $$(basename "$$f")"; \
	done
	@# prune links of ours whose source was deleted
	@for l in "$(UNIT_DIR)"/*; do \
		[ -L "$$l" ] || continue; \
		case "$$(readlink -m "$$l")" in "$(REPO)"/*) ;; *) continue;; esac; \
		[ -e "$$l" ] || { rm -f "$$l"; echo "pruned  $$(basename "$$l")"; }; \
	done
	@systemctl --user daemon-reload
	@echo "--- installed ---"
	@systemctl --user list-unit-files --no-legend 2>/dev/null \
		| grep -E '^(kiwix|jellyfin|caddy|gatus|dozzle|glances|whoami|travel-home-lab)' || true

uninstall:
	@for f in "$(QUADLET_SRC)"/*; do rm -f "$(UNIT_DIR)/$$(basename "$$f")"; done
	@systemctl --user daemon-reload
	@echo "uninstalled (quadlet units only; volumes and images are left alone)"
```

`check` **errors out** rather than warning, so `make install` on a dev box can't
scatter broken units into the wrong user manager; `FORCE=1` overrides. This is
stricter than the earlier draft (which only warned), which matters because a
wrong-path install would mount empty directories and *appear* healthy.

### Why symlinks

1. **No drift.** Edit in the repo, `make install`, done. A copy-based install
   silently diverges the moment someone edits the repo and forgets to re-copy.
2. **Verified to work** — the generator accepts symlinked unit files.
3. **Idempotent and reviewable** — the whole install is a git-tracked Makefile.
4. **Trivial rollback** — `make uninstall` + `daemon-reload`.

### Alternatives considered and rejected

| Option | Verdict |
|---|---|
| **Copy files** (`cp`) | Rejected — silent drift between repo and installed units. |
| **`QUADLET_UNIT_DIRS` env var** | Rejected. The variable is real (present in the generator binary, and running the generator directly with it set produces units). But getting it into the **systemd user manager's generator environment** is the problem — a plain `daemon-reload` did **not** pick it up. Only injecting via `~/.config/environment.d/` appeared to work, and I could not confirm that survives a reboot or user-manager restart. Undocumented-ish and fragile. Not worth it to avoid 12 symlinks. |
| **`containers.conf [quadlet] unit_dirs`** | Rejected — tested, no effect. |
| **Ansible** | Plausible given this box already has `~/.ansible`. Deferred as overkill for 12 files; worth revisiting if the fleet grows. |
| **`.kube` unit + kompose** | Rejected — collapses to one file, but it's a different dialect, per-service tuning is clumsier, and the whole stack would be rewritten for no gain. |

> `podman-restart.service` is **not** part of this. It only starts containers with
> `restart-policy=always`, and every service here is `unless-stopped`, so it would
> do nothing. Leave it disabled.

---

## 8. Migration procedure

Assumes `/mnt/storage` already mounts (done), the repo is at `~/travel-home-lab`,
and the checkout actually contains `quadlets/` and the `Makefile` (step 0).

```bash
cd ~/travel-home-lab

# 0. Make sure the Pi has the quadlets. The docs/Makefile/quadlets live in this
#    repo; on 2026-10-15 the Pi's checkout was still at eb029b0 ("add dozzle")
#    and contained none of them, and the doc commits were unpushed. Push from
#    the dev checkout first, then pull here — otherwise `make install` has
#    nothing to link.
git pull --ff-only

# 1. Tear down compose
#    NOTE: plain `down`, NOT `down -v`. The -v flag would delete all five
#    volumes (they carry compose project labels) including 28 MB of Jellyfin state.
podman compose down

# 2. Install quadlets
#    (requires the quadlets/ dir and Makefile from §6 and §7 to exist first)
make install

# 3. DRY CHECK — inspect generated units BEFORE starting anything
systemctl --user list-unit-files | grep -E 'kiwix|jellyfin|caddy|gatus|dozzle|glances|whoami'
systemctl --user cat kiwix.service        # confirm -v /mnt/storage/zim:/data
systemctl --user cat caddy.service        # confirm --publish 443:443, %h expanded

# 3b. Confirm the volumes are being REUSED, not recreated — each volume unit
#     should read `podman volume create --ignore <existing name>`
systemctl --user cat jellyfin_config-volume.service

# 4. Start
systemctl --user start kiwix jellyfin caddy gatus dozzle glances whoami

# 5. Verify (see §9)
podman ps
```

Step 3 matters: it catches path/expansion mistakes without touching a running stack.
Step 3b is the cheap guard against accidentally pointing at a *new* volume name —
if `--ignore` is absent or the name is wrong, stop before Jellyfin starts with an
empty config.

**Volumes are reused in place** (§6.3) — nothing is copied or moved, and Jellyfin's
library setup, gatus history, and Caddy's internal CA all carry over. Expect
Jellyfin to come up already configured.

---

## 9. Verification

| # | Check | Command | Expect |
|---|---|---|---|
| 1 | All units generated | `systemctl --user list-unit-files \| grep generated` | 7 services + 5 volumes + 1 network |
| 2 | Wired for autostart | `ls /run/user/1000/systemd/generator/default.target.wants/` | 7 symlinks |
| 3 | All running | `podman ps` | 7 containers up |
| 4 | **Storage actually mounted** | `findmnt /mnt/storage` | 475 G, ~80 G free |
| 5 | Kiwix sees ZIMs | `curl -skI --resolve kiwix.travel-lab.lan:443:127.0.0.1 https://kiwix.travel-lab.lan/` | `200` |
| 6 | Jellyfin sees movies | `curl -skI --resolve jellyfin.travel-lab.lan:443:127.0.0.1 https://jellyfin.travel-lab.lan/` | `302`→`200` |
| 7 | gatus green | `curl -s http://localhost` via caddy, or the gatus UI | apps group all up |
| 8 | **Cold boot** | `sudo reboot`, then repeat 2–7 | everything returns unattended |

Check 8 is the one that matters. `[Install] WantedBy=default.target` + `Linger=yes`
should handle it, but only a real reboot proves it.

---

## 10. Rollback

```bash
cd ~/travel-home-lab
make uninstall          # removes symlinks + daemon-reload
podman compose up -d    # back to the old stack
```

Volumes are shared by name via `VolumeName=`, so both systems see the same data —
rollback loses nothing, because the quadlets never moved or copied it.

Two cautions:

- **Do not run compose and quadlets simultaneously** — port 443 would collide and
  container names would clash.
- **Never `podman compose down -v`**, at any point, including during rollback. The
  volumes are labelled with the compose project, so `-v` deletes all five (§6.3).

After migration is proven, `podman network rm travel-home-lab_default` reclaims the
orphaned compose network, and `compose.yml` + `sample.env` can be deleted. Do that
last — it's your rollback path.

---

## 11. Risks and open questions

1. **Repo path is hardcoded** (§6.4). Repo must be at `~/travel-home-lab`. Called
   out by `make install`.
2. **Empty-mount hazard.** The fstab entry has `nofail`, and USB enumeration can
   lag `local-fs.target`. If the drive is missing, `nofail` lets boot continue and
   a bind mount would create an empty directory — kiwix and jellyfin would serve
   nothing while *appearing* healthy. Hence `ExecStartPre=/usr/bin/mountpoint -q /mnt/storage`
   on both, which fails loudly instead. (Ordering already favours us; this is
   belt-and-braces.) The `x-systemd.device-timeout=60` now in fstab also helps.
3. **`Exec=*.zim` for kiwix** — a faithful translation of compose's
   `command: '*.zim'` (both override CMD, not ENTRYPOINT), but it depends on the
   image handling the glob. Worth confirming kiwix still lists all 7 archives
   after migration.
4. **`AutoUpdate=registry` does nothing** until `podman-auto-update.timer` is
   enabled. Decide whether you want unattended image updates on a travel box.
   Deliberately left inert.
5. **`glances` had no restart policy** in compose. Recommendation is to add
   `Restart=always`; flagging because it's a behaviour change.
6. **Podman version skew.** Verified on 5.4.2 (the Pi) and 6.1.2 (dev). The Pi's
   generated units include `--cidfile=%t/%N.cid`, dev's don't — harmless, but a
   reminder that generated output differs across versions.
7. **`glances` and `dozzle` need the podman socket.** `podman.socket` is already
   enabled and active; the quadlets now make that dependency explicit.
8. **Clock.** `podman ps` reports the legacy containers as `Exited (0) 292 years ago`
   — the Pi has no RTC and had a bad clock at creation time. Caddy's `tls internal`
   certificates issued during that window may have odd validity. Cosmetic, but
   worth knowing if you get TLS warnings.
9. **`README.md` is empty** (0 bytes). Worth adding a short pointer to this doc.
10. **`podman compose down -v` is destructive here.** All five volumes carry
    `com.docker.compose.project=travel-home-lab` labels, so `-v` would delete them
    — 28 MB of Jellyfin state, gatus history, and Caddy's internal CA. Plain
    `down` is safe. **`podman volume prune` is even more dangerous**, because it
    is *not* scoped to the compose project: it removes any volume not referenced
    by a **running** container. On 2026-10-15 all seven containers were `Exited`,
    so every one of the five volumes was prunable — including with compose configs
    still present. The only safe order is: get the quadlet containers running and
    holding the volumes **first**, then prune the orphans last (after §9 check 8).
    Warnings added to the README.
11. **Volume reuse depends on the names matching exactly.** `VolumeName=` must be
    `travel-home-lab_<name>`; a typo silently creates a *new empty* volume and
    Jellyfin would come up unconfigured. Verification step 3b exists to catch this.
