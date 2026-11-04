// Controllers page. The travel-tv Go app owns BlueZ over the system D-Bus
// socket; this page only renders what it reports and asks for pair/connect/
// disconnect/forget. Pairing blocks for up to ~30s, so the pressed button
// stays disabled until the request returns.
(() => {
  const list = document.getElementById('bt-list');
  const stateEl = document.getElementById('bt-state');
  const scanButton = document.getElementById('bt-scan');
  const noteEl = document.getElementById('bt-note');

  if (!list || !stateEl) return;

  const POLL_MS = 3000;
  let status = null;
  let busy = false;
  let timer = null;

  function showNote(message) {
    if (!noteEl) return;
    if (!message) {
      noteEl.hidden = true;
      noteEl.textContent = '';
      return;
    }
    noteEl.hidden = false;
    noteEl.textContent = message;
  }

  function describe(device) {
    const parts = [];
    if (device.connected) parts.push('Connected');
    else if (device.paired) parts.push('Paired');
    else if (device.trusted) parts.push('Ready');
    else parts.push('Available');
    if (typeof device.battery === 'number' && device.battery >= 0) {
      parts.push(`${device.battery}%`);
    }
    return parts.join(' · ');
  }

  function primaryAction(device) {
    if (device.connected) return { action: 'disconnect', label: 'Disconnect' };
    if (device.paired) return { action: 'connect', label: 'Connect' };
    return { action: 'pair', label: 'Pair' };
  }

  function render() {
    if (!status) return;

    const devices = status.devices || [];
    const connected = devices.filter((device) => device.connected).length;
    const paired = devices.filter((device) => device.paired).length;

    if (!status.available) {
      stateEl.textContent = 'Bluetooth unavailable';
    } else if (!status.powered) {
      stateEl.textContent = 'Bluetooth is off';
    } else if (status.discovering) {
      stateEl.textContent = 'Scanning for controllers…';
    } else if (devices.length) {
      stateEl.textContent = `${paired} paired · ${connected} connected`;
    } else {
      stateEl.textContent = 'No controllers yet';
    }

    if (!devices.length) {
      list.dataset.html = '';
      list.innerHTML = status.available
        ? '<div class="bt-empty">No controllers yet. Put the pad in pairing mode and press Scan.</div>'
        : '<div class="bt-empty">The kiosk cannot reach BlueZ. Check that bluetooth.service is running on the Pi.</div>';
      return;
    }

    const active = document.activeElement;
    const focus = active && active.dataset
      ? { action: active.dataset.action, address: active.dataset.address }
      : null;

    const html = devices.map((device) => {
      const primary = primaryAction(device);
      const forget = device.paired
        ? `<button class="bt-action bt-action--quiet" type="button"
                   data-action="forget" data-address="${device.address}">Forget</button>`
        : '';
      return `
        <div class="bt-device${device.connected ? ' is-connected' : ''}">
          <div class="bt-device-copy">
            <div class="bt-device-name">${escapeHTML(device.name || device.address)}</div>
            <div class="bt-device-meta">${escapeHTML(describe(device))}</div>
          </div>
          <div class="bt-device-actions">
            <button class="bt-action" type="button"
                    data-action="${primary.action}"
                    data-address="${device.address}">${primary.label}</button>
            ${forget}
          </div>
        </div>`;
    }).join('');

    if (list.dataset.html === html) return;
    list.dataset.html = html;
    list.innerHTML = html;

    if (focus) {
      const again = list.querySelector(
        `button[data-action="${focus.action}"][data-address="${focus.address}"]`
      );
      if (again) again.focus();
    }
  }

  function escapeHTML(value) {
    return String(value)
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
      .replaceAll('"', '&quot;');
  }

  async function getStatus() {
    const response = await fetch('/api/bluetooth', { cache: 'no-store' });
    const data = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(data.error || `HTTP ${response.status}`);
    return data;
  }

  async function post(action, payload) {
    const response = await fetch(`/api/bluetooth/${action}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload || {})
    });
    const data = await response.json().catch(() => ({}));
    if (!response.ok || data.ok === false) {
      throw new Error(data.error || `HTTP ${response.status}`);
    }
    return data;
  }

  async function refresh() {
    try {
      status = await getStatus();
      showNote(status.error && !status.available ? status.error : '');
      render();
    } catch (error) {
      stateEl.textContent = 'Bluetooth unavailable';
      showNote(error.message || 'The TV service is not reachable.');
    }
    schedule();
  }

  function schedule() {
    clearTimeout(timer);
    timer = setTimeout(refresh, POLL_MS);
  }

  function setBusy(value) {
    busy = value;
    list.classList.toggle('is-busy', value);
    if (scanButton) scanButton.disabled = value;
  }

  async function run(action, payload, label) {
    if (busy) return;
    setBusy(true);
    showNote('');
    stateEl.textContent = label;
    try {
      await post(action, payload);
    } catch (error) {
      showNote(error.message || 'Bluetooth request failed.');
    } finally {
      setBusy(false);
      refresh();
    }
  }

  list.addEventListener('click', (event) => {
    const button = event.target.closest('button[data-action]');
    if (!button) return;
    const action = button.dataset.action;
    const address = button.dataset.address;
    if (action === 'pair') {
      run('pair', { address }, 'Pairing…');
    } else if (action === 'connect') {
      run('connect', { address }, 'Connecting…');
    } else if (action === 'disconnect') {
      run('disconnect', { address }, 'Disconnecting…');
    } else if (action === 'forget') {
      run('forget', { address }, 'Forgetting…');
    }
  });

  if (scanButton) {
    scanButton.addEventListener('click', () => {
      run('scan', { seconds: 20 }, 'Scanning for controllers…');
    });
  }

  // Stop polling while the page is hidden; refresh when it comes back.
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) {
      clearTimeout(timer);
    } else {
      refresh();
    }
  });

  refresh();
})();
