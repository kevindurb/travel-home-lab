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

# Stop and remove the quadlet units this repo manages. Volumes are left alone.
teardown:
  just playbook teardown.yml

# Run a command on the Pi, e.g. `just ssh 'podman ps'`
ssh *args:
  ssh travel-lab.beaver-cloud.ts.net {{ args }}

# Container unit status on the Pi.
status:
  ssh travel-lab.beaver-cloud.ts.net 'systemctl --user --no-pager status kiwix jellyfin gatus glances dozzle homer whoami'

# Tailscale Services the Pi is serving (name -> loopback proxy target).
serves:
  ssh travel-lab.beaver-cloud.ts.net 'tailscale serve status'

# Same, as JSON, including whether each Service listens on HTTP or HTTPS.
serves-json:
  ssh travel-lab.beaver-cloud.ts.net 'tailscale serve status --json'

# Follow a unit's journal, e.g. `just logs jellyfin`
logs unit:
  ssh -t travel-lab.beaver-cloud.ts.net 'journalctl --user -u {{ unit }} -f'
