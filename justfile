# Convenience wrappers around ansible-playbook for the travel lab.
#
# The Ansible project lives in ./ansible; the recipes cd into it, so they work
# from any directory and always pick up ansible.cfg, the inventory and the
# roles. See README.md.
#
#   just --list

ansible_dir := justfile_directory() / "ansible"

default:
  @just --list

# Escape hatch, e.g. `just playbook deploy.yml --check --diff`
playbook *args:
  cd {{ ansible_dir }} && ansible-playbook -i inventory/prod.yml {{ args }}

# Host prerequisites (sysctl, linger). Prompts for the Pi's sudo password.
host-prep:
  just playbook host-prep.yml --ask-become-pass

# Deploy / update the stack. No sudo.
deploy:
  just playbook deploy.yml

# host-prep + deploy in one run.
site:
  just playbook site.yml --ask-become-pass

# Dry run against the live stack.
check:
  just playbook deploy.yml --check --diff

# Edit the encrypted Wi-Fi SSID/PSK (travel_lab_wifi_ssid / travel_lab_wifi_psk).
vault-edit:
  cd {{ ansible_dir }} && ansible-vault edit host_vars/travel-lab/vault.yml

# Show the decrypted Wi-Fi placeholders/credentials.
vault-view:
  cd {{ ansible_dir }} && ansible-vault view host_vars/travel-lab/vault.yml

# Stop and remove the quadlet units this repo manages. Volumes are left alone.
teardown:
  just playbook teardown.yml

# Run a command on the Pi, e.g. `just ssh 'podman ps'`
ssh *args:
  ssh travel-lab.beaver-cloud.ts.net {{ args }}

# Container unit status on the Pi.
status:
  ssh travel-lab.beaver-cloud.ts.net 'systemctl --user --no-pager status kiwix jellyfin gatus glances dozzle homer travel-tv whoami'

# Tailscale Services the Pi is serving (name -> loopback proxy target).
serves:
  ssh travel-lab.beaver-cloud.ts.net 'tailscale serve status'

# Same, as JSON, including whether each Service listens on HTTP or HTTPS.
serves-json:
  ssh travel-lab.beaver-cloud.ts.net 'tailscale serve status --json'

# Follow a unit's journal, e.g. `just logs jellyfin`
logs unit:
  ssh -t travel-lab.beaver-cloud.ts.net 'journalctl --user -u {{ unit }} -f'

# Travel TV kiosk status and logs (it is a system unit, not a user quadlet).
tv-status:
  ssh travel-lab.beaver-cloud.ts.net 'systemctl --no-pager status tv-kiosk'

tv-logs:
  ssh -t travel-lab.beaver-cloud.ts.net 'journalctl -u tv-kiosk -f'

# Phone remote API (a user unit, proxied by the travel-tv container).
tv-remote-status:
  ssh travel-lab.beaver-cloud.ts.net 'systemctl --user --no-pager status travel-tv-remote'

tv-remote-logs:
  ssh -t travel-lab.beaver-cloud.ts.net 'journalctl --user-unit travel-tv-remote.service -f'

# One-shot health check over the Unix socket (no tailnet round-trip).
tv-remote-health:
  ssh travel-lab.beaver-cloud.ts.net 'curl -fsS --unix-socket ~/.config/travel-tv/run/remote.sock http://localhost/health'

# Restart Firefox/Cage after a page or profile change. Prompts for sudo.
tv-restart:
  ssh -t travel-lab.beaver-cloud.ts.net 'sudo systemctl restart tv-kiosk'

# Port-forward the launcher for a laptop browser preview.
tv-page:
  ssh -N -L 18085:127.0.0.1:18085 travel-lab.beaver-cloud.ts.net

# Remove the kiosk unit/PAM/supervisor. Leaves packages, the profile and the
# travel-tv container alone (use `just teardown` for the stack).
tv-teardown:
  cd {{ ansible_dir }} && ansible-playbook tv-teardown.yml --ask-become-pass
