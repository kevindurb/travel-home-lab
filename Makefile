# Podman quadlet install/uninstall for travel-home-lab.
#
# The quadlets hardcode %h/travel-home-lab/... in their Volume= paths, so the
# repo must live at $(EXPECTED) on the host that runs them. `make check`
# refuses to install anywhere else (override with FORCE=1 if you know why).

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
