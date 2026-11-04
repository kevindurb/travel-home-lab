(() => {
  const columns = Number(document.body.dataset.columns) || 4;
  const statusEl = document.getElementById('status');
  const toastEl = document.getElementById('toast');
  const volume = document.getElementById('volume');
  const volumeOut = document.getElementById('volume-out');
  const trackpad = document.getElementById('trackpad');
  const typeForm = document.getElementById('type-form');
  const typeInput = document.getElementById('type-input');

  let toastTimer = null;
  let volumeTimer = null;

  const buzz = (ms = 8) => {
    if (navigator.vibrate) navigator.vibrate(ms);
  };

  const toast = (message) => {
    if (!toastEl) return;
    toastEl.textContent = message;
    toastEl.classList.add('is-visible');
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => toastEl.classList.remove('is-visible'), 1900);
  };

  async function send(type, payload = {}) {
    try {
      const response = await fetch('/api/action', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ type, ...payload })
      });
      let data = null;
      try { data = await response.json(); } catch (_) { /* no body */ }
      if (!response.ok || !data || data.ok === false) {
        throw new Error((data && data.error) || `HTTP ${response.status}`);
      }
      return true;
    } catch (error) {
      toast(error.message || 'TV not reachable');
      return false;
    }
  }

  function setStatus(ready, detail) {
    if (!statusEl) return;
    statusEl.textContent = ready ? 'TV ready' : 'TV offline';
    statusEl.dataset.state = ready ? 'ready' : 'offline';
    statusEl.title = detail || '';
  }

  async function refresh() {
    try {
      const response = await fetch('/api/health', { cache: 'no-store' });
      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = await response.json();
      setStatus(Boolean(data.ok && data.wayland), data.last_error);
      if (volume && typeof data.volume === 'number' && document.activeElement !== volume) {
        volume.value = String(Math.round(data.volume * 100));
        updateVolumeLabel();
      }
    } catch (_) {
      setStatus(false, 'The remote service is not reachable.');
    }
  }

  function updateVolumeLabel() {
    if (volume && volumeOut) volumeOut.textContent = `${volume.value}%`;
  }

  // --- keypad ---------------------------------------------------------------
  const holdKeys = new Set(['Up', 'Down', 'Left', 'Right']);

  document.querySelectorAll('[data-key]').forEach((button) => {
    const key = button.dataset.key;
    let holdTimer = null;
    let repeatTimer = null;
    let repeatStarted = false;

    const stop = () => {
      clearTimeout(holdTimer);
      clearInterval(repeatTimer);
      holdTimer = null;
      repeatTimer = null;
    };

    if (holdKeys.has(key)) {
      button.addEventListener('pointerdown', (event) => {
        if (event.pointerType === 'mouse' && event.button !== 0) return;
        repeatStarted = false;
        stop();
        holdTimer = setTimeout(() => {
          repeatStarted = true;
          buzz();
          send('key', { key });
          repeatTimer = setInterval(() => send('key', { key }), 240);
        }, 400);
      });
      ['pointerup', 'pointercancel', 'pointerleave'].forEach((name) => {
        button.addEventListener(name, stop);
      });
    }

    button.addEventListener('click', () => {
      if (repeatStarted) {
        repeatStarted = false;
        return;
      }
      buzz();
      send('key', { key });
    });
  });

  document.querySelectorAll('[data-combo]').forEach((button) => {
    button.addEventListener('click', () => {
      const [mod, key] = button.dataset.combo.split(':');
      buzz();
      send('combo', { mod, key });
    });
  });

  // --- apps -----------------------------------------------------------------
  document.querySelectorAll('[data-launch]').forEach((button) => {
    button.addEventListener('click', async () => {
      const buttons = document.querySelectorAll('[data-launch]');
      buttons.forEach((item) => { item.disabled = true; });
      buzz(15);
      toast(`Opening ${button.dataset.label || 'app'}…`);
      await send('launch', { index: Number(button.dataset.launch), columns });
      buttons.forEach((item) => { item.disabled = false; });
    });
  });

  // --- pointer / trackpad ---------------------------------------------------
  document.querySelectorAll('[data-click]').forEach((button) => {
    button.addEventListener('click', () => {
      buzz();
      send('click', { button: button.dataset.click });
    });
  });

  document.querySelectorAll('[data-scroll]').forEach((button) => {
    button.addEventListener('click', () => {
      buzz();
      send('scroll', { dy: Number(button.dataset.scroll) });
    });
  });

  if (trackpad) {
    let active = false;
    let lastX = 0;
    let lastY = 0;
    let accX = 0;
    let accY = 0;
    let moved = 0;
    let startedAt = 0;
    let flushTimer = null;

    const flush = () => {
      flushTimer = null;
      const dx = Math.round(accX);
      const dy = Math.round(accY);
      if (!dx && !dy) return;
      accX -= dx;
      accY -= dy;
      send('pointer', { dx, dy });
    };

    const end = (event) => {
      if (!active) return;
      active = false;
      trackpad.classList.remove('is-active');
      clearTimeout(flushTimer);
      flushTimer = null;
      flush();
      if (moved < 6 && Date.now() - startedAt < 500) {
        buzz();
        send('click', { button: 'left' });
      }
      if (event && event.pointerId !== undefined) {
        try { trackpad.releasePointerCapture(event.pointerId); } catch (_) { /* noop */ }
      }
    };

    trackpad.addEventListener('pointerdown', (event) => {
      if (event.pointerType === 'mouse' && event.button !== 0) return;
      active = true;
      moved = 0;
      startedAt = Date.now();
      lastX = event.clientX;
      lastY = event.clientY;
      trackpad.classList.add('is-active');
      try { trackpad.setPointerCapture(event.pointerId); } catch (_) { /* noop */ }
      event.preventDefault();
    });

    trackpad.addEventListener('pointermove', (event) => {
      if (!active) return;
      const dx = event.clientX - lastX;
      const dy = event.clientY - lastY;
      lastX = event.clientX;
      lastY = event.clientY;
      accX += dx;
      accY += dy;
      moved += Math.abs(dx) + Math.abs(dy);
      if (!flushTimer) flushTimer = setTimeout(flush, 50);
      event.preventDefault();
    });

    trackpad.addEventListener('pointerup', end);
    trackpad.addEventListener('pointercancel', end);
    trackpad.addEventListener('pointerleave', (event) => { if (active) end(event); });
    trackpad.addEventListener('contextmenu', (event) => event.preventDefault());
  }

  // --- volume / media -------------------------------------------------------
  if (volume) {
    updateVolumeLabel();
    volume.addEventListener('input', () => {
      updateVolumeLabel();
      clearTimeout(volumeTimer);
      volumeTimer = setTimeout(() => {
        send('volume', { value: Number(volume.value) / 100 });
      }, 120);
    });
  }

  document.querySelectorAll('[data-mute-toggle]').forEach((button) => {
    button.addEventListener('click', () => {
      buzz();
      send('mute', { toggle: true });
    });
  });

  // --- keyboard -------------------------------------------------------------
  if (typeForm && typeInput) {
    typeForm.addEventListener('submit', async (event) => {
      event.preventDefault();
      const text = typeInput.value.trim();
      if (!text) return;
      buzz();
      const ok = await send('text', { text });
      if (ok) typeInput.select();
    });
  }

  refresh();
  setInterval(refresh, 10000);
})();
