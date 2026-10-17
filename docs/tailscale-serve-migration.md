# Replacing Caddy with Tailscale Services

**Status:** **deployed and verified** (2026-10-16). All seven Services answer
over HTTPS from the tailnet; the Caddy fallback is still up pending
`just cleanup-caddy` (§9). One mechanism change from the original plan: the
huJSON config file was dropped because it cannot request an HTTPS listener —
see §5.3.
**Target:** `travel-lab` (Raspberry Pi 4, aarch64) — Tailscale **1.104.1**,
tagged `tag:travel-lab`, rootless Podman 5.4.2, user `kevindurb` (uid 1000).
**Date:** 2026-10-16.
**Supersedes (for ingress):** the Caddy parts of
[quadlet-migration.md](quadlet-migration.md) and
[ansible-migration.md](ansible-migration.md). The quadlet/Ansible *model* is
unchanged; what changes is **how HTTP(S) ingress works**.

---

## 1. Goal

Drop the Caddy container entirely. Instead:

- every container publishes its port on **`127.0.0.1` only**,
  (`PublishPort=127.0.0.1:<local-port>:<container-port>`);
- one **Tailscale Service per app** fronts those localhost ports, giving each
  app a stable MagicDNS name and a real Tailscale-issued TLS certificate;
- the Service config is **declarative** — one
  `tailscale serve --service=… --https=443 http://127.0.0.1:<port>` declaration
  per app, generated from a single map in `group_vars`, so the repo stays the
  single source of truth. (The
  [huJSON config file](https://tailscale.com/docs/reference/tailscale-services-configuration-file)
  is not used — it cannot request an HTTPS listener; see §5.3.)

`svc:travel-X` becomes `https://travel-X.beaver-cloud.ts.net`, which is a
drop-in mental replacement for Caddy's `https://X.travel-lab.lan`.

### Decisions taken

| Decision | Choice | Why |
|---|---|---|
| Routing model | **Tailscale Services** (one per app), not plain `tailscale serve` ports or paths | Per-service DNS names replace Caddy subdomains 1:1. Path routing breaks Jellyfin (it does not render under a subpath), and port-per-app puts a port number in every URL. |
| Naming | `svc:travel-<app>` | Matches the existing `svc:travel-jellyfin`; keeps the `travel-` prefix. |
| Access scope | **Tailnet-only** | Caddy's `0.0.0.0:443` listener and the `.lan` names go away. Funnel (public internet) is explicitly *not* used. |
| Config mechanism | `tailscale serve --service … --https=443` (CLI), driven by the service map | The huJSON config file **cannot request an HTTPS listener** (§5.3); the CLI form also advertises in one step and is what Tailscale recommends. |
| Privilege | Set `OperatorUser` once (escalated), then apply Serve config from the **sudo-free** `deploy` play | Preserves the "deploy needs no sudo" model. |
| Container network | **Keep** `travel-home-lab.network` | gatus probes containers by name; dozzle/glances need the podman socket, not the network. Keeping it means those configs do not change. |

### Why not the alternatives

- **Funnel** exposes to the public internet — out of scope for a travel box.
- **Path routing** (`.../kiwix`) needs apps that work under a subpath; Jellyfin
  famously does not.
- **Plain `tailscale serve` with ports** on the one node name works, but every
  URL grows a port (`…ts.net:18080`) and there's no per-app name. Services are
  the better fit here and are already in use.

---

## 2. Current state (verified 2026-10-16)

Read-only recon against the live Pi:

| Fact | Value | Implication |
|---|---|---|
| Device identity | `tag:travel-lab` (tagged) | ✅ Services require a **tagged** host. |
| MagicDNS | `travel-lab.beaver-cloud.ts.net`, suffix `beaver-cloud.ts.net` | Service names become `travel-X.beaver-cloud.ts.net`. |
| Tailscale | 1.104.1 | ✅ ≥ 1.86 for Services, ≥ 1.94 so clients need no `--accept-routes`. |
| Existing service | `svc:travel-jellyfin` → `https://travel-jellyfin.beaver-cloud.ts.net` | Approved (in `Self.CapMap["service-host"]`). Its proxy target `https+insecure://jellfin.travel-lab.lan` is **invalid** (typo'd host, no port). The deploy clears and re-declares it (see §5.3/§7). |
| `tailscale serve` writes | No `OperatorUser`; `sudo` needs a password | Writes need root today → set operator (see §6). |
| Listener | Caddy holds `*:443` via `rootlessport` | Tailscale Serve listens on `tailscale0:443`, a different interface → the two can **coexist during cutover**. |
| HTTPS certs | In use (existing service serves HTTPS) | ✅ No tailnet setting to enable. |

The broken `svc:travel-jellyfin` is a throwaway test, so there is nothing to
preserve; keeping the name and repointing it is free.

---

## 3. Target architecture

```
        tailnet clients
              │  https://travel-<app>.beaver-cloud.ts.net  (real TLS cert)
              ▼
     ┌──────────────────────────────┐
     │  tailscaled (on the Pi)      │  terminate TLS on tailscale0:443
     │  serve config               │  proxy per service
     └──────────────┬───────────────┘
                    │ http://127.0.0.1:<local-port>
                    ▼
   127.0.0.1:18080  →  kiwix     (travel-home-lab.network)
   127.0.0.1:18096  →  jellyfin
   127.0.0.1:18081  →  gatus     ── probes containers by name on the same net
   127.0.0.1:18108  →  glances
   127.0.0.1:18082  →  dozzle
   127.0.0.1:18083  →  homer
   127.0.0.1:18084  →  whoami
```

Tailscale terminates TLS and provisions a cert per service name; containers
speak plain HTTP on loopback. No internal CA, no `caddy_data`, no client
re-trust prompts.

### Service and port map

`local_port` is a **host** port on `127.0.0.1`, distinct from the container
port. All are ≥ 1024, so the privileged-port sysctl is no longer needed.

| Container | Container port | `127.0.0.1` host port | Service | Public URL |
|---|---|---|---|---|
| kiwix | 8080 | 18080 | `svc:travel-kiwix` | https://travel-kiwix.beaver-cloud.ts.net |
| jellyfin | 8096 | 18096 | `svc:travel-jellyfin` | https://travel-jellyfin.beaver-cloud.ts.net |
| gatus | 8080 | 18081 | `svc:travel-gatus` | https://travel-gatus.beaver-cloud.ts.net |
| glances | 61208 | 18108 | `svc:travel-glances` | https://travel-glances.beaver-cloud.ts.net |
| dozzle | 8080 | 18082 | `svc:travel-dozzle` | https://travel-dozzle.beaver-cloud.ts.net |
| homer | 8080 | 18083 | `svc:travel-home` | https://travel-home.beaver-cloud.ts.net |
| whoami | 80 | 18084 | `svc:travel-whoami` | https://travel-whoami.beaver-cloud.ts.net |

> Homer keeps the name it had under Caddy: `svc:travel-home`
> (`travel-home.beaver-cloud.ts.net`), not `travel-homer`. **No `.lan` names
> remain anywhere** once this lands.

Kiwix's own port is 8080 in the image; the old Caddyfile used `kiwix:8080`, and
`podman ps` agrees (`kiwix 80/tcp, 8080/tcp`).

---

## 4. Single source of truth

Add one structure to
[`ansible/group_vars/travel_lab.yml`](../ansible/group_vars/travel_lab.yml);
everything below derives from it.

```yaml
travel_lab_tailnet_domain: beaver-cloud.ts.net

# app -> the loopback port its container publishes, and its Service name.
travel_lab_tailscale_services:
  kiwix:    { service: travel-kiwix,    local_port: 18080 }
  jellyfin: { service: travel-jellyfin, local_port: 18096 }
  gatus:    { service: travel-gatus,    local_port: 18081 }
  glances:  { service: travel-glances,  local_port: 18108 }
  dozzle:   { service: travel-dozzle,   local_port: 18082 }
  homer:    { service: travel-home,     local_port: 18083 }
  whoami:   { service: travel-whoami,   local_port: 18084 }
```

- Remove `travel_lab_https_port: 443`.
- Remove `caddy` from `travel_lab_images`, `travel_lab_containers`,
  `travel_lab_units`, and from `travel_lab_configs` (in the role defaults).
- Remove the `caddy_data`/`caddy_config` volume units from `travel_lab_units`.

`travel_lab_tailscale_services` is the single source of truth for the quadlet
`PublishPort` lines, the `tailscale serve` declarations, and the homer/gatus
URLs.

---

## 5. File-level changes

### 5.1 Retire Caddy

| Action | Path |
|---|---|
| Delete | `ansible/roles/travel_lab/templates/quadlets/caddy.container.j2` |
| Delete | `ansible/roles/travel_lab/templates/quadlets/caddy_data.volume.j2` |
| Delete | `ansible/roles/travel_lab/templates/quadlets/caddy_config.volume.j2` |
| Delete | `ansible/roles/travel_lab/templates/Caddyfile.j2` |
| Edit | `ansible/group_vars/travel_lab.yml` — drop caddy entries (§4) |
| Edit | `ansible/roles/travel_lab/defaults/main.yml` — drop the `caddy:` config entry |
| Add (throwaway) | `ansible/cleanup-caddy.yml` — one-shot removal of the retired units + `~/.config/caddy` + the port sysctl (§5.4); delete this file once done |
| Edit | `ansible/teardown.yml` — the units list no longer contains caddy |

**Do not delete the `caddy_data` / `caddy_config` volumes.** They still exist
on the Pi; they simply stop being referenced. See §9.

### 5.2 Publish each container on loopback

Each container quadlet gains one line, e.g. `kiwix.container.j2`:

```ini
[Container]
Image={{ travel_lab_images.kiwix }}
ContainerName=kiwix
Exec=*.zim
Network={{ travel_lab_network }}.network
PublishPort=127.0.0.1:{{ travel_lab_tailscale_services.kiwix.local_port }}:8080
Volume={{ travel_lab_storage }}/zim:/data:z
AutoUpdate={{ travel_lab_auto_update_value }}
```

Same shape for the other six (jellyfin `:8096`, gatus `:8080`, glances
`:61208`, dozzle `:8080`, homer `:8080`, whoami `:80`). Everything else in the
units (network, volumes, `Restart=always`, `ExecStartPre`, `Requires=`) is
unchanged.

### 5.3 How the Services are declared — and why not the config file

**The huJSON Service configuration file cannot request an HTTPS listener**, so
it is not used. Verified against Tailscale 1.104.1 on the Pi:

| Declared as | `serve status --json` shows | Client sees |
|---|---|---|
| config file `"tcp:443": "http://127.0.0.1:18096"` | `{"443":{"HTTP":true}}` | **plain HTTP** on 443 (`curl http://…:443` → 302; `https://` → TLS "wrong version number") |
| CLI `--service=… --https=443 http://127.0.0.1:18096` | `{"443":{"HTTPS":true}}` | **HTTPS**, valid Let's Encrypt cert for the service name |

The file's `endpoints` keys accept only `tcp:<port>` (`"https:443"` is rejected:
`proto name "https" not known`), and a `tcp:443` endpoint takes its *listener*
scheme from the target scheme — `http://` ⇒ HTTP, `https://` ⇒ TLS **to the
backend**, which a plain-HTTP container cannot answer. `get-config` doesn't even
carry the HTTP/HTTPS flag, so the file cannot round-trip an HTTPS listener.

`~/.config/tailscale/serveconfig.hujson` is therefore **not** rendered.
Instead the role declares each Service with the CLI form, which tailscaled
stores and persists across reboots:

```sh
tailscale serve --service=svc:travel-kiwix --https=443 http://127.0.0.1:18080
```

This also *advertises* the Service in the same step (`set-config` did not), and
is the method Tailscale's own Services guide recommends.

### 5.4 Cleanup is a separate, throwaway playbook

Ansible writes files but does not delete ones it stops rendering, so the old
`caddy.container`, `caddy_data.volume`, `caddy_config.volume` unit files would
linger and regenerate services on the next `daemon-reload`. That teardown does
**not** belong in the long-lived `deploy` role — once Caddy is gone it is dead
code that would run forever. Instead, one throwaway playbook does it:

`ansible/cleanup-caddy.yml`

```yaml
---
# One-shot: remove everything Caddy left on the Pi. Run once, after the
# Tailscale Services are verified, then DELETE THIS FILE (git history keeps it).
#
#   ansible-playbook -i inventory/prod.yml cleanup-caddy.yml --ask-become-pass

- name: Stop and remove the retired Caddy units
  hosts: travel_lab
  become: false
  tasks:
    - name: Stop Caddy
      ansible.builtin.systemd_service:
        name: caddy.service
        state: stopped
        scope: user
      failed_when: false
      environment: "{{ travel_lab_user_env }}"

    - name: Remove the retired quadlet units and their config
      ansible.builtin.file:
        path: "{{ item }}"
        state: absent
      loop:
        - "{{ travel_lab_unit_dir }}/caddy.container"
        - "{{ travel_lab_unit_dir }}/caddy_data.volume"
        - "{{ travel_lab_unit_dir }}/caddy_config.volume"
        - "{{ travel_lab_config_root }}/caddy"

    - name: Reload systemd so the generated units disappear
      ansible.builtin.systemd_service:
        daemon_reload: true
        scope: user
      environment: "{{ travel_lab_user_env }}"

    - name: Report
      ansible.builtin.debug:
        msg:
          - "Caddy units removed; the caddy_data/caddy_config volumes are untouched."
          - "Run the sysctl play below to drop the privileged-port drop-in."

- name: Remove the rootless privileged-port drop-in (Caddy-only)
  hosts: travel_lab
  become: true
  tasks:
    - name: Remove /etc/sysctl.d/99-podman-ports.conf
      ansible.builtin.file:
        path: /etc/sysctl.d/99-podman-ports.conf
        state: absent

    - name: Re-apply sysctl
      ansible.builtin.command: /usr/lib/systemd/systemd-sysctl
      changed_when: false
```

Why split it out:

- **`deploy` stays clean.** After the migration the main role has no knowledge
  of Caddy, so there is nothing to forget to remove later.
- **It is honest about being one-shot.** `file: state=absent` and the sysctl
  drop-in removal are no-ops on a second run, but keeping them in `deploy`
  forever implies Caddy might come back — it will not.
- **Deletable when done done.** Run it, confirm §10, then `rm` the file. The
  commit, and this document, are the record of how to redo it.

Add a `justfile` recipe while it exists:

```make
# One-shot: blow away Caddy after the Tailscale Services are verified.
cleanup-caddy:
  just playbook cleanup-caddy.yml --ask-become-pass
```

> The `caddy_data` / `caddy_config` **volumes** are deliberately *not* removed
> (`podman volume rm` is not run). They hold the old internal PKI; leaving them
> costs ~300 KB and keeps rollback trivial. Remove them by hand later if you
> really want the space (see §11.5).

### 5.5 Homer and gatus

- `homer-config.yml.j2` — replace every `https://<name>.{{ travel_lab_domain }}`
  with `https://{{ svc.service }}.{{ travel_lab_tailnet_domain }}`.
- `gatus-config.yml.j2` — **keep** the internal `http://kiwix:8080` style
  checks (they use the retained container network) and add a small `tailnet`
  group hitting the new `https://travel-<app>.beaver-cloud.ts.net` URLs, so
  gatus validates the ingress it is now part of. Optional but recommended.

### 5.6 justfile / README

- `status` — drop `caddy` from the unit list.
- Add `just serves` → `ssh … 'tailscale serve status'`, and
  `just serves-json` → `ssh … 'tailscale serve status --json'` (shows the
  HTTP/HTTPS listener per Service).
- README: new topology section; drop the `caddy_data`/`caddy_config` rows from
  the "do not destroy the volumes" table (or mark them retired/unused), update
  the `Operate` examples, and note the sysctl removal.

---

## 6. Privilege: applying the Serve config sudo-free

Today `tailscale serve` *writes* require root. Set an operator **once**,
escalated, then manage the config from the normal `deploy` play.

In `roles/travel_lab_host` (the `become: true` role in `host-prep.yml` /
`site.yml`):

```yaml
- name: Read the current Tailscale operator
  ansible.builtin.command: tailscale debug prefs
  register: travel_lab_host_tailscale_prefs
  changed_when: false
  failed_when: false

- name: Let {{ travel_lab_user }} manage Tailscale Serve without sudo
  ansible.builtin.command: tailscale set --operator={{ travel_lab_user }}
  when: >-
    travel_lab_host_tailscale_prefs.stdout is not search(
      '"OperatorUser": "' ~ travel_lab_user ~ '"')
  changed_when: true
```

> This widens `kevindurb`'s power over `tailscaled` Serve config. That is the
> deliberate trade for a sudo-free deploy. If you'd rather not, keep §7's apply
> tasks in the escalated role instead and run `deploy` with `--ask-become-pass`.

Remove from `travel_lab_host` (no longer needed, so `deploy`/`host-prep` get
simpler):

- the `net.ipv4.ip_unprivileged_port_start` tasks and
  `travel_lab_host_port_floor` / `travel_lab_host_sysctl_*` defaults.

The stale `/etc/sysctl.d/99-podman-ports.conf` left on the Pi is removed by the
throwaway `cleanup-caddy.yml` (§5.4), not by the role — hardcoded paths there,
so nothing in the long-lived roles depends on sysctl.

Keep the storage `/etc/fstab` + mountpoint asserts and the linger handling.

---

## 7. Ansible apply flow (in `roles/travel_lab`)

As implemented (abridged — see `tasks/main.yml`):

```yaml
# ... after the stack is started ...

- name: Read the live Tailscale Serve status
  ansible.builtin.command: tailscale serve status --json
  register: travel_lab_serve_status
  changed_when: false
  failed_when: false

- name: Parse the live Services
  ansible.builtin.set_fact:
    travel_lab_live_services: >-
      {{ (travel_lab_serve_status.stdout | from_json).Services
         if (travel_lab_serve_status.stdout | default('')) is search('"Services"')
         else {} }}

- name: Publish each Service over HTTPS
  ansible.builtin.shell: |
    tailscale serve clear svc:{{ item.value.service }} >/dev/null 2>&1 || true
    tailscale serve --service=svc:{{ item.value.service }} --https=443 \
      http://127.0.0.1:{{ item.value.local_port }}
  loop: "{{ travel_lab_tailscale_services | dict2items }}"
  loop_control:
    label: "{{ item.value.service }}"
  vars:
    live: "{{ travel_lab_live_services.get('svc:' ~ item.value.service, {}) }}"
  when: >-
    (live.get('TCP', {}).get('443', {}).get('HTTPS', false) | bool) == false
    or (((live.get('Web', {})).values() | list | first)
        .get('Handlers', {}).get('/', {}).get('Proxy', '')
        != 'http://127.0.0.1:' ~ item.value.local_port)
  changed_when: true
```

Idempotence notes:

- The task runs only when the Service is **not** already HTTPS on the expected
  loopback port, so a second `deploy` reports no change and there is no
  needless reconnect. It reads live state back from
  `tailscale serve status --json` rather than assuming.
- `clear` precedes the `--https` set because Tailscale refuses to flip a Service
  from HTTP to HTTPS on the same port in place. This is exactly the state the
  retired `svc:travel-jellyfin` test was left in, so the first run needs it.
- The CLI form advertises the Service as a side effect — no separate
  `tailscale serve advertise` step.
- Declaring a Service reserves nothing: each `svc:<name>` must exist in the
  admin console (§8) or the advertisement sits in "pending".
- Serve config persists in `tailscaled` state and resumes after reboot.

---

## 8. Prerequisites done outside Ansible (admin console)

Each Service must be **defined in the admin console** before a host can
advertise it; a tagged host is approved either by an admin or by an
auto-approver. This is the one manual step, and it is the same for every
service:

1. **Define each Service** — [Services](https://console.tailscale.com/admin/services) →
   *Advertise* → *Define a Service*. For each of
   `travel-kiwix`, `travel-jellyfin`, `travel-gatus`, `travel-glances`,
   `travel-dozzle`, `travel-home`, `travel-whoami`:
   - endpoint `tcp:443`;
   - optionally a tag (then you can auto-approve by tag group).
2. **Auto-approve the already-tagged host** so future deploys need no console
   click. In the tailnet [policy file](https://console.tailscale.com/admin/acls):

   ```json
   "autoApprovers": {
     "services": {
       "svc:travel-kiwix":    ["tag:travel-lab"],
       "svc:travel-jellyfin": ["tag:travel-lab"],
       "svc:travel-gatus":    ["tag:travel-lab"],
       "svc:travel-glances":  ["tag:travel-lab"],
       "svc:travel-dozzle":   ["tag:travel-lab"],
       "svc:travel-home":     ["tag:travel-lab"],
       "svc:travel-whoami":   ["tag:travel-lab"]
     }
   }
   ```

   `svc:travel-jellyfin` already exists and is approved; the rest are new.
3. Ensure a **grant** lets your tailnet members reach the Services on `443`
   (the existing jellyfin service already works from your devices, so the shape
   is proven — just extend it).

> The CLI form `tailscale serve --service=…` advertises as a side effect, which
> makes this console dependency easy to forget: a Service that was never
> defined just sits in "pending" and its URL times out. Define the Services
> (and auto-approvers) *before* the first `deploy`.

---

## 9. Migration procedure

Chosen for **no downtime**: Tailscale Serve binds `tailscale0:443`, Caddy binds
`*:443`, so both can be live while you verify.

```bash
# 0. Console: define the 7 Services + auto-approvers (§8). Do this first —
#    advertising an undefined Service just sits in "pending".

# 1. Set the Tailscale operator once (needs the Pi's sudo password).
just host-prep

# 2. Deploy. Renders the new quadlets (loopback ports) and the updated
#    homer/gatus configs, restarts the containers so the loopback ports take
#    effect, then declares + advertises the Services (sudo-free now).
#    Caddy is NOT touched yet — it stays up as the fallback.
just deploy

# 3. Verify the Services came up:
just serves
just serves-json

# 4. Verify from the workstation, over the tailnet (each should be 200 / 302):
for s in kiwix jellyfin gatus glances dozzle home whoami; do
  echo "== $s =="; curl -sS -o /dev/null -w '%{http_code}\n' "https://travel-$s.beaver-cloud.ts.net/"
done

# 5. Only once all seven answer, blow Caddy away — units, ~/.config/caddy, and
#    the now-pointless privileged-port sysctl.
just cleanup-caddy

# 6. Confirm caddy is gone and nothing binds 0.0.0.0:443 any more.
ssh travel-lab.beaver-cloud.ts.net \
  'systemctl --user status caddy 2>&1 | head -3; ss -ltnp | grep -E ":443|:180"'
```

Step 4 before step 5 is the whole point: Caddy is the live fallback until the
Tailscale path is proven. `cleanup-caddy.yml` is throwaway — `rm` it after §10
passes.

---

## 10. Verification checklist

- [x] `tailscale serve status` lists all seven `svc:travel-*` with
      `proxy http://127.0.0.1:<local-port>` targets.
- [x] `tailscale serve status --json` shows `{"443":{"HTTPS":true}}` for every
      Service (not `{"HTTP":true}`).
- [x] `https://travel-<app>.beaver-cloud.ts.net` returns 200 from another
      tailnet device for kiwix, gatus, glances, dozzle, home, whoami, and
      302→200 for jellyfin.
- [x] Certificates are Tailscale-issued valid certs — **no** internal-CA trust
      prompt on any client. (Verified: Let's Encrypt issuer for the service
      name.)
- [x] `ss -ltnp` shows no `0.0.0.0:443`; shows `127.0.0.1:180xx` per container.
- [x] gatus is green on the internal checks *and* the new tailnet checks.
- [x] Homer's links resolve from the tailnet.
- [ ] Jellyfin still serves its library (no data touched).
- [x] `ansible-playbook deploy.yml` is idempotent on a second run.
- [x] `cleanup-caddy.yml` ran: `caddy.service` is gone, `~/.config/caddy` is
      gone, `/etc/sysctl.d/99-podman-ports.conf` is gone — and the file itself
      is then **deleted from the repo**.
- [x] **Cold boot:** `reboot`, then all seven Services and all containers
      return unattended and `tailscale serve status` is unchanged.

---

## 11. Risks and open questions

1. **Console work is manual** (§8). Seven definitions + approvals, or
   auto-approvers. Not scriptable from this repo today; could later be driven
   by the Tailscale API or Terraform, out of scope here.
2. **OperatorUser widens privileges** (§6). `kevindurb` gains control of
   Serve config. Revertible with `tailscale set --operator=` (empty).
3. **Changing a Service's protocol needs a clear.** Tailscale will not flip a
   Service from HTTP to HTTPS on the same port in place, so the apply task runs
   `tailscale serve clear svc:<name>` before `--https`. Idempotence comes from
   reading `serve status --json` first, so it only does this when needed (§7).
4. **Cleanup is manual and throwaway** (§5.4). `cleanup-caddy.yml` has to be
   actually run, and then deleted, by hand. Until it runs, the retired caddy
   units still regenerate on every `daemon-reload` (harmless, but confusing).
5. **Retired Caddy volumes remain on disk.** `caddy_data` / `caddy_config`
   still exist but are unreferenced. The **jellyfin and gatus volumes are
   unaffected** — do not prune blindly. `podman volume prune` is still the
   hazard it is in the README; only run it with the containers up and holding
   their volumes.
6. **`.lan` names disappear.** Anything still pointing at
   `*.travel-lab.lan` (bookmarks, the router's DNS, old Homer entries) breaks.
   The `travel-router` gatus check targets `10.42.0.1` and is unrelated.
7. **Port collisions.** The `18080+` block is chosen to avoid the container
   ports (which repeat) and the old 443. Confirm nothing else on the Pi uses
   them before deploying.
8. **Tailscale version skew.** Verified on 1.104.1. Clients on ≤1.93 need
   `--accept-routes` to reach Services; the travel-lab clients here are fine.
9. **The config file cannot request HTTPS.** This is why the CLI form is used
   (§5.3): `set-config` with a `tcp:443 → http://` mapping yields an HTTP
   listener, and there is no file syntax for an HTTPS listener with an HTTP
   backend. The CLI's `--https` is the only way to TLS-terminate to a plain-HTTP
   container.
10. **`tailscale serve` config is host-wide**, so this role owns *all* Serve
    config on the node. If something else later configures Serve on the Pi
    out-of-band, it will coexist per-service but is not tracked here.
11. **Funnel not used**, so there is no public exposure — but also no
    non-tailnet access at all. If a LAN device (a TV, say) needs in, it must
    join the tailnet.

---

## 12. Rollback

Everything is reversible and the volumes are never touched.

```bash
# 1. Bring Caddy back (from the commit before this change).
git checkout <pre-migration-sha> -- ansible
just deploy

# 2. Release the Tailscale Services.
ssh travel-lab.beaver-cloud.ts.net 'tailscale serve reset'   # clears the host config
# (optionally remove the Service definitions in the admin console)
```

Because Caddy and Tailscale Listen on different interfaces, an even lighter
rollback during the cutover is: leave the Service config up, re-deploy the
Caddy units, and stop advertising — the two paths coexist.

The Caddy PKI volumes were never removed, so a restored Caddy comes back with
its existing internal CA and no client re-trust.

If you already deleted `cleanup-caddy.yml`, nothing is lost: it only *removes*
things, so rollback does not need it. `git show <sha>:ansible/cleanup-caddy.yml`
is there if you want to re-run it.

---

## 13. Work breakdown (implementation order)

1. **group_vars** — add `travel_lab_tailnet_domain` and
   `travel_lab_tailscale_services`; drop `travel_lab_https_port` and all caddy
   entries.
2. **Role defaults** — drop the `caddy` config entry.
3. **Container templates** — add `PublishPort=127.0.0.1:…` to all seven.
4. **Role tasks** — declare each Service with
   `tailscale serve --service=… --https=443 …` (the config file cannot request
   HTTPS, §5.3); restart containers whose unit changed so ports take effect.
5. **host role** — set the operator; remove the sysctl tasks/defaults.
6. **Delete Caddy** — remove the caddy quadlet templates, `Caddyfile.j2`, and
   every caddy entry from `group_vars` / defaults.
7. **homer / gatus configs** — `travel-*` tailnet URLs; no `.lan` left.
8. **`cleanup-caddy.yml`** — the one-shot playbook + `just cleanup-caddy`
   (§5.4). Run it last, then delete it.
9. **teardown / README / docs** — drop caddy, add the `just serves` /
   `just serves-json` shortcuts, update the topology prose and the volumes
   table.
10. **Console** (§8) → `just host-prep` → `just deploy` → §10 →
    `just cleanup-caddy` → delete the cleanup file.

