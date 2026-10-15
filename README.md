# travel-home-lab

Rootless Podman Quadlets for the travel lab stack (kiwix, jellyfin, caddy,
gatus, glances, dozzle, whoami) on `travel-lab` (Raspberry Pi 4, aarch64).

The stack used to run under `podman compose`; see
[docs/quadlet-migration.md](docs/quadlet-migration.md) for the plan, the
verified behaviour, and the migration/rollback procedure.

## Install

The quadlets are symlinked into the user systemd generator directory:

```bash
make install      # symlink quadlets/ into ~/.config/containers/systemd + daemon-reload
```

`make install` **refuses to run unless the repo lives at `~/travel-home-lab`** —
the quadlets hardcode `%h/travel-home-lab/...` in their `Volume=` paths, because
quadlet has no variable interpolation and relative paths resolve to the quadlet
file's directory, not the repo.

## Operate

```bash
systemctl --user start kiwix jellyfin caddy gatus dozzle glances whoami
systemctl --user status caddy
journalctl --user -u kiwix -f
```

Quadlets autostart at boot via `[Install] WantedBy=default.target` plus
`Linger=yes`; do **not** run `systemctl --user enable` (generated units reject it).

## ⚠️ Do not destroy the volumes

Five named volumes hold state that is painful to recreate:

| Volume | Holds |
|---|---|
| `travel-home-lab_jellyfin_config` | Jellyfin library setup, users, watched state (~28 MB) |
| `travel-home-lab_gatus_data` | uptime history (`data.db` + WAL) |
| `travel-home-lab_caddy_data` | Caddy internal PKI / issued certs |
| `travel-home-lab_caddy_config` | Caddy autosave config |
| `travel-home-lab_dozzle_data` | (empty) |

- **Never run `podman compose down -v`.** The volumes carry
  `com.docker.compose.project=travel-home-lab` labels, so `-v` deletes all five.
  Plain `podman compose down` is safe. Use `make uninstall`, not `down -v`, to roll back.
- **Do not run `podman volume prune`** unless the quadlet containers are running
  and holding the volumes. Prune removes any volume not referenced by a *running*
  container — including these five whenever the stack is stopped.
- One extra **anonymous** hex-named volume is normal: the Jellyfin image declares
  `VOLUME /cache`, and since only `/config` is mounted explicitly, `/cache` gets an
  anonymous volume. It is throwaway transcoding cache and is removed automatically
  when the container stops (`podman rm -v`).
- **Do not run compose and the quadlets at the same time.** Port 443 and the
  container names would collide.

## Rollback

```bash
make uninstall                                        # remove symlinks + daemon-reload
git show 2b28155:compose.yml > compose.yml            # restore the compose stack
git show 2b28155:sample.env  > sample.env
podman compose up -d
```

`compose.yml` was deleted on 2026-10-15 once the quadlets were proven across a
cold boot; it and `sample.env` remain in git history at `2b28155` and earlier.
The quadlets never copy or move the volumes, so both systems see the same data.
Note that `.env` (untracked) is still on the Pi if you need the old values.
