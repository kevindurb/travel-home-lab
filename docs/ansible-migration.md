# Converting travel-home-lab to Ansible

**Status:** built, not yet deployed.
**Target:** `travel-lab` (Raspberry Pi 4, aarch64) — Podman **5.4.2**, rootless
user `kevindurb` (uid 1000), `Linger=yes`.
**Date:** 2026-10-16.

This replaces the `Makefile` + hand-written-quadlet install described in
[quadlet-migration.md](quadlet-migration.md). The quadlet *model* is unchanged;
what changes is **how the units and config files get onto the Pi**.

---

## 1. What changed and why

| Before | After |
|---|---|
| `make install` symlinked `quadlets/` into `~/.config/containers/systemd/` | Ansible **renders** real unit files into `~/.config/containers/systemd/` |
| Repo had to live at `~/travel-home-lab` on the Pi (hardcoded `%h/travel-home-lab/...`) | Repo is not needed on the Pi at all; units use `%h/.config/...` |
| `HOST=travel-lab.lan`, `/mnt/storage`, image tags hardcoded across files | All in `ansible/group_vars/travel_lab.yml` |
| Config files bind-mounted from the repo clone | Rendered to `~/.config/{caddy,gatus,homer}` and bind-mounted from there |
| No install-time checks | `ansible-playbook --check`, plus checks on podman version, storage mount and port floor |

The two things this buys: **one place to change a value** (the domain appears
once), and **no repo-on-Pi coupling** (deploy from the workstation).

## 2. Layout

```
ansible/
├── ansible.cfg                 # mirrors k8s/ansible + networking/ansible
├── requirements.yml            # community.general, ansible.posix, containers.podman
├── justfile                    # deploy / site / host-prep / check / teardown / status / logs
├── inventory/prod.yml          # travel-lab -> travel-lab.beaver-cloud.ts.net
├── group_vars/travel_lab.yml   # domain, storage, port, images, service list
├── site.yml                    # host-prep + deploy
├── host-prep.yml               # escalated prerequisites
├── deploy.yml                  # the stack, as the user
├── teardown.yml                # stop + remove managed units
└── roles/
    ├── travel_lab_host/        # sysctl drop-in, linger, assertions (become)
    └── travel_lab/             # render + start the stack (no become)
        └── templates/
            ├── quadlets/*.j2   # 14 units: 1 network, 5 volumes, 8 containers
            ├── Caddyfile.j2
            ├── gatus-config.yml.j2
            └── homer-config.yml.j2
```

## 3. What lands on the Pi

```
~/.config/containers/systemd/   # 14 quadlet units (real files, not symlinks)
~/.config/caddy/Caddyfile
~/.config/gatus/config.yml
~/.config/homer/config.yml
```

The container units bind-mount these with the systemd `%h` specifier, e.g.
`Volume=%h/.config/caddy:/etc/caddy:z`. `%h` is expanded by systemd at runtime,
so the units are username-agnostic; Ansible writes the files using the real home
path (from `ansible_facts.user_dir`).

## 4. Behaviour changes (deliberate)

1. **`podman-auto-update.timer` is now enabled.** Previously disabled, so
   `AutoUpdate=registry` was inert. With the timer on, podman will pull `:latest`
   on its schedule — including **jellyfin and caddy**. If that is unwelcome for a
   stateful service, pin its tag in `travel_lab_images` (that is the reason the
   image tags are variables). Set `travel_lab_auto_update: false` to revert to
   the old behaviour entirely.
2. **The `Environment=HOST` coupling is gone.** The old `caddy.container` set
   `HOST` and the Caddyfile used `{$HOST}`. The domain is now rendered straight
   into `Caddyfile.j2` from `travel_lab_domain`; the env var was removed so there
   is a single source of truth.
3. **`sysctl` is applied by writing the drop-in and running
   `/usr/lib/systemd/systemd-sysctl` by absolute path,** not via
   `ansible.posix.sysctl`. systemd's helper is not on PATH (and `sysctl` itself
   sits in `/usr/sbin`, absent from the login shell's PATH on the Pi).
4. **`podman.socket` is now ensured** (enabled + started, user scope) rather than
   merely assumed. dozzle and glances need it; on the Pi it was already enabled,
   so this is a no-op there.

Everything else is a faithful port: `Restart=always`, `AutoUpdate=`,
`ExecStartPre=/usr/bin/mountpoint`, the `%t/podman/podman.sock` binds, and the
`travel-home-lab_*` `VolumeName=` values (so the existing volumes are reused in
place — no migration).

## 5. The one-time migration step (completed 2026-10-16)

The Pi previously had the old install: 14 **symlinks** in
`~/.config/containers/systemd/` pointing into `~/travel-home-lab/quadlets/`.
The first `deploy` replaced them with real unit files (real files and unrelated
links were left alone), and the now-unused `~/travel-home-lab` clone was deleted
from the Pi.

The throwaway code that did this has since been **removed** from
`roles/travel_lab`, so nothing in the project knows about the old layout any
more. A fresh host is deployed simply by rendering the units.

## 6. How to run

From the repository root (the `justfile` lives there):

```bash
just site          # host-prep (sudo prompt) + deploy
just deploy        # stack only
just check         # dry run
just teardown      # stop + remove managed units; volumes untouched
```

## 7. Verification checklist (before trusting it)

- [ ] `ansible-playbook deploy.yml --check` reports no unexpected changes.
- [ ] A real `deploy` run, then a **second** run, is idempotent (no changes).
- [ ] `systemctl --user list-units '*.service'` shows the 8 containers active.
- [ ] `podman volume ls` lists the same five `travel-home-lab_*` volumes; none
      recreated.
- [ ] Jellyfin still comes up configured (library/users/watched state intact).
- [ ] `https://{kiwix,jellyfin,whoami,gatus,glances,dozzle,home}.<domain>` serve
      with the existing internal CA (no re-trust prompt).
- [ ] Cold boot: `reboot`, then confirm the stack returns with `just status`.

## 8. Rollback

The Pi no longer holds a clone of this repo, so rollback is driven from the
workstation (all the history is here):

```bash
# back to the quadlet + Makefile install: re-clone the old commit onto the Pi
ssh travel-lab.beaver-cloud.ts.net \
  'git clone https://github.com/kevindurb/travel-home-lab.git ~/travel-home-lab &&
   git -C ~/travel-home-lab checkout eff8423'
# then, on the Pi: cd ~/travel-home-lab && make install

# or back to compose: fetch the two files from history and copy them to the Pi
git show 2b28155:compose.yml > compose.yml
git show 2b28155:sample.env  > sample.env
# on the Pi: podman compose up -d
```

The quadlets never copy or move the volumes, so every path above sees the same
data.
