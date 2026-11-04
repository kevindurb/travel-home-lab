(() => {
  const pad = (n) => String(n).padStart(2, '0');
  const WEATHER_KEY = 'travel-tv-weather-v2';
  const WEATHER_TTL = 30 * 60 * 1000;

  const page = document.body.dataset.page;
  const weatherConfig = {
    units: document.body.dataset.weatherUnits || 'fahrenheit',
    lat: document.body.dataset.weatherLat || '',
    lon: document.body.dataset.weatherLon || '',
    city: document.body.dataset.weatherCity || ''
  };

  function tick() {
    const now = new Date();
    const clock = document.getElementById('clock');
    const date = document.getElementById('date');
    if (clock) clock.textContent = `${pad(now.getHours())}:${pad(now.getMinutes())}`;
    if (date) {
      date.textContent = now.toLocaleDateString(undefined, {
        weekday: 'long',
        day: 'numeric',
        month: 'long'
      });
    }
  }

  tick();
  setInterval(tick, 10000);

  // --- weather ------------------------------------------------------------
  const WX = {
    sun: 'wx-sun',
    moon: 'wx-moon',
    partly: 'wx-partly',
    partlyNight: 'wx-partly-night',
    cloud: 'wx-cloud',
    fog: 'wx-fog',
    rain: 'wx-rain',
    snow: 'wx-snow',
    thunder: 'wx-thunder'
  };

  const CONDITIONS = {
    0: 'Clear', 1: 'Mainly clear', 2: 'Partly cloudy', 3: 'Overcast',
    45: 'Fog', 48: 'Freezing fog',
    51: 'Light drizzle', 53: 'Drizzle', 55: 'Heavy drizzle',
    56: 'Freezing drizzle', 57: 'Freezing drizzle',
    61: 'Light rain', 63: 'Rain', 65: 'Heavy rain',
    66: 'Freezing rain', 67: 'Freezing rain',
    71: 'Light snow', 73: 'Snow', 75: 'Heavy snow', 77: 'Snow grains',
    80: 'Light showers', 81: 'Showers', 82: 'Heavy showers',
    85: 'Snow showers', 86: 'Snow showers',
    95: 'Thunderstorm', 96: 'Thunderstorm', 99: 'Thunderstorm'
  };

  function iconFor(code, isDay) {
    const c = Number(code);
    if (c === 0) return isDay ? 'sun' : 'moon';
    if (c === 1 || c === 2) return isDay ? 'partly' : 'partlyNight';
    if (c === 3) return 'cloud';
    if (c === 45 || c === 48) return 'fog';
    if ((c >= 51 && c <= 67) || (c >= 80 && c <= 82)) return 'rain';
    if ((c >= 71 && c <= 77) || c === 85 || c === 86) return 'snow';
    if (c >= 95) return 'thunder';
    return 'cloud';
  }

  const conditionLabel = (code) => CONDITIONS[Number(code)] || 'Weather';

  function dayLabel(iso) {
    const d = new Date(`${iso}T12:00:00`);
    return d.toLocaleDateString(undefined, { weekday: 'short' }).toUpperCase();
  }

  function hourLabel(iso) {
    const h = Number(iso.slice(11, 13));
    const suffix = h >= 12 ? 'PM' : 'AM';
    const h12 = h % 12 === 0 ? 12 : h % 12;
    return `${h12}${suffix}`;
  }

  function readCache() {
    try {
      const raw = localStorage.getItem(WEATHER_KEY);
      if (!raw) return null;
      const cached = JSON.parse(raw);
      if (!cached || !cached.at || !cached.data) return null;
      return cached;
    } catch (_) {
      return null;
    }
  }

  function writeCache(data) {
    try {
      localStorage.setItem(WEATHER_KEY, JSON.stringify({ at: Date.now(), data }));
    } catch (_) {
      // localStorage can be unavailable in private/restricted contexts.
    }
  }

  async function resolveLocation() {
    if (weatherConfig.lat && weatherConfig.lon) {
      return { lat: weatherConfig.lat, lon: weatherConfig.lon, city: weatherConfig.city };
    }
    const res = await fetch('https://ipwho.is/', { cache: 'no-store' });
    if (!res.ok) throw new Error('location');
    const geo = await res.json();
    if (!geo.success || !geo.latitude || !geo.longitude) throw new Error('location');
    return {
      lat: geo.latitude,
      lon: geo.longitude,
      city: weatherConfig.city || geo.city || geo.region || geo.country || ''
    };
  }

  async function fetchWeather() {
    const location = await resolveLocation();
    const url = new URL('https://api.open-meteo.com/v1/forecast');
    url.search = new URLSearchParams({
      latitude: location.lat,
      longitude: location.lon,
      current: 'temperature_2m,weather_code,is_day',
      daily: 'temperature_2m_max,temperature_2m_min,weather_code,precipitation_probability_max',
      hourly: 'temperature_2m,weather_code,precipitation_probability,is_day',
      timezone: 'auto',
      forecast_days: '5',
      temperature_unit: weatherConfig.units
    });
    const res = await fetch(url, { cache: 'no-store' });
    if (!res.ok) throw new Error('weather');
    return { location, weather: await res.json() };
  }

  function currentHourIndex(hourly, currentTime) {
    const times = hourly.time || [];
    const anchor = currentTime ? `${currentTime.slice(0, 13)}:00` : '';
    const idx = times.findIndex((t) => t >= anchor);
    return idx < 0 ? 0 : idx;
  }

  function setText(id, text) {
    const el = document.getElementById(id);
    if (el) el.textContent = text;
  }

  function renderHomeWeather(payload) {
    if (!document.getElementById('weather')) return;
    const { location, weather } = payload;
    const current = weather.current || {};
    const daily = weather.daily || {};
    setText('wx-temp', `${Math.round(current.temperature_2m)}°`);
    setText(
      'wx-range',
      `H ${Math.round(daily.temperature_2m_max[0])}° · L ${Math.round(daily.temperature_2m_min[0])}°`
    );
    setText('wx-place', location.city || '');
    const use = document.getElementById('wx-icon-use');
    if (use) use.setAttribute('href', `#${WX[iconFor(current.weather_code, current.is_day)]}`);
  }

  function renderForecast(payload) {
    const { location, weather } = payload;
    const current = weather.current || {};
    const daily = weather.daily || {};
    const hourly = weather.hourly || {};

    const use = document.getElementById('fc-icon-use');
    if (use) use.setAttribute('href', `#${WX[iconFor(current.weather_code, current.is_day)]}`);

    setText('fc-temp', `${Math.round(current.temperature_2m)}°`);
    setText('fc-condition', conditionLabel(current.weather_code));
    setText('fc-place', location.city || '');
    setText(
      'fc-range',
      `H ${Math.round(daily.temperature_2m_max[0])}° · L ${Math.round(daily.temperature_2m_min[0])}°`
    );

    renderGraph(hourly, current);
    renderDaily(daily);
  }

  function smoothPath(points) {
    if (!points.length) return '';
    let d = `M ${points[0].x},${points[0].y}`;
    for (let i = 0; i < points.length - 1; i++) {
      const p0 = points[i - 1] || points[i];
      const p1 = points[i];
      const p2 = points[i + 1];
      const p3 = points[i + 2] || p2;
      const cp1x = p1.x + (p2.x - p0.x) / 6;
      const cp1y = p1.y + (p2.y - p0.y) / 6;
      const cp2x = p2.x - (p3.x - p1.x) / 6;
      const cp2y = p2.y - (p3.y - p1.y) / 6;
      d += ` C ${cp1x},${cp1y} ${cp2x},${cp2y} ${p2.x},${p2.y}`;
    }
    return d;
  }

  function renderGraph(hourly, current) {
    const svg = document.getElementById('hourly-graph');
    if (!svg) return;

    const W = 1200;
    const padL = 30;
    const padR = 30;
    const iconY = 12;
    const iconSize = 36;
    const top = 78;
    const bottom = 158;
    const baseline = 164;
    const hourY = 205;

    const times = hourly.time || [];
    const temps = hourly.temperature_2m || [];
    const codes = hourly.weather_code || [];
    const days = hourly.is_day || [];
    const start = currentHourIndex(hourly, current.time);
    const count = Math.min(12, times.length - start);
    if (count < 2) return;

    const points = [];
    for (let i = 0; i < count; i++) {
      const idx = start + i;
      points.push({
        x: padL + (i * (W - padL - padR)) / (count - 1),
        temp: Number(temps[idx]),
        code: codes[idx],
        isDay: days[idx],
        time: times[idx]
      });
    }

    const values = points.map((p) => p.temp);
    let min = Math.min(...values);
    let max = Math.max(...values);
    if (max - min < 8) {
      const mid = (max + min) / 2;
      min = mid - 4;
      max = mid + 4;
    }
    const yFor = (t) => bottom - ((t - min) / (max - min)) * (bottom - top);
    const coords = points.map((p) => ({ x: p.x, y: yFor(p.temp) }));
    const line = smoothPath(coords);
    const area = `${line} L ${coords[coords.length - 1].x},${baseline} L ${coords[0].x},${baseline} Z`;

    const parts = [
      '<defs><linearGradient id="wx-area" x1="0" y1="0" x2="0" y2="1">' +
        '<stop offset="0%" stop-color="#5cc8ff" stop-opacity=".35"/>' +
        '<stop offset="100%" stop-color="#5cc8ff" stop-opacity="0"/>' +
        '</linearGradient></defs>',
      `<path d="${area}" fill="url(#wx-area)"/>`,
      `<path d="${line}" fill="none" stroke="#5cc8ff" stroke-width="4" stroke-linecap="round" stroke-linejoin="round"/>`
    ];

    points.forEach((p, i) => {
      const c = coords[i];
      const icon = WX[iconFor(p.code, p.isDay)];
      const isNow = i === 0;
      parts.push(
        `<use href="#${icon}" x="${p.x - iconSize / 2}" y="${iconY}" width="${iconSize}" height="${iconSize}"/>`
      );
      parts.push(
        `<circle cx="${c.x}" cy="${c.y}" r="${isNow ? 7 : 4}" fill="${isNow ? '#ffffff' : '#0b1016'}" stroke="#5cc8ff" stroke-width="${isNow ? 4 : 3}"/>`
      );
      parts.push(
        `<text x="${p.x}" y="${hourY}" text-anchor="middle" class="wx-hour">${hourLabel(p.time)}</text>`
      );
    });

    svg.innerHTML = parts.join('');
  }

  function renderDaily(daily) {
    const root = document.getElementById('daily-row');
    if (!root) return;
    const days = daily.time || [];
    const highs = daily.temperature_2m_max || [];
    const lows = daily.temperature_2m_min || [];
    const codes = daily.weather_code || [];
    const precs = daily.precipitation_probability_max || [];

    root.innerHTML = days.slice(0, 5).map((day, i) => {
      const icon = WX[iconFor(codes[i], 1)];
      return `
        <div class="day-card">
          <div class="day-name">${i === 0 ? 'TODAY' : dayLabel(day)}</div>
          <svg class="day-icon" viewBox="0 0 24 24" aria-hidden="true">
            <use href="#${icon}"></use>
          </svg>
          <div class="day-temp">${Math.round(highs[i])}° <span class="day-low">/ ${Math.round(lows[i])}°</span></div>
          <div class="day-precip">${Math.round(precs[i] ?? 0)}%</div>
        </div>`;
    }).join('');
  }

  function loadWeather(force = false) {
    const cached = readCache();
    if (cached && !force && Date.now() - cached.at < WEATHER_TTL) {
      if (page === 'home') renderHomeWeather(cached.data);
      if (page === 'forecast') renderForecast(cached.data);
      return Promise.resolve();
    }

    return fetchWeather()
      .then((payload) => {
        writeCache(payload);
        if (page === 'home') renderHomeWeather(payload);
        if (page === 'forecast') renderForecast(payload);
      })
      .catch(() => {
        if (cached) {
          if (page === 'home') renderHomeWeather(cached.data);
          if (page === 'forecast') renderForecast(cached.data);
          return;
        }
        if (page === 'home') {
          setText('wx-temp', '--°');
          setText('wx-range', 'Weather unavailable');
          setText('wx-place', '');
        }
        if (page === 'forecast') {
          setText('fc-temp', '--°');
          setText('fc-condition', 'Weather unavailable');
          setText('fc-place', '');
          setText('fc-range', '');
        }
      });
  }

  // --- page wiring --------------------------------------------------------
  if (page === 'wifi' || page === 'forecast') {
    const back = document.querySelector('[data-back]');
    if (back) requestAnimationFrame(() => back.focus());
    document.addEventListener('keydown', (event) => {
      if (event.key === 'Backspace' || event.key === 'Escape') {
        event.preventDefault();
        window.location.href = 'index.html';
      }
    });
    if (page === 'forecast') {
      loadWeather();
      setInterval(() => loadWeather(true), WEATHER_TTL);
    }
    return;
  }

  // home
  loadWeather();
  setInterval(() => loadWeather(true), WEATHER_TTL);

  const tiles = Array.from(document.querySelectorAll('[data-tile]'));
  if (!tiles.length) return;
  let index = 0;

  function focusTile(next, scroll = true) {
    if (!next || next === tiles[index]) return;
    tiles[index].tabIndex = -1;
    index = tiles.indexOf(next);
    next.tabIndex = 0;
    next.focus();
    if (scroll) next.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  tiles.forEach((tile, i) => {
    tile.tabIndex = i === 0 ? 0 : -1;
    tile.addEventListener('focus', () => { index = i; });
  });

  function closestInDirection(current, key) {
    const cr = current.getBoundingClientRect();
    const cx = cr.left + cr.width / 2;
    const cy = cr.top + cr.height / 2;
    let best = null;

    for (const tile of tiles) {
      if (tile === current) continue;
      const r = tile.getBoundingClientRect();
      const dx = r.left + r.width / 2 - cx;
      const dy = r.top + r.height / 2 - cy;
      let primary;
      let cross;

      if (key === 'ArrowRight') {
        if (dx <= 0) continue;
        primary = dx;
        cross = Math.abs(dy);
      } else if (key === 'ArrowLeft') {
        if (dx >= 0) continue;
        primary = -dx;
        cross = Math.abs(dy);
      } else if (key === 'ArrowDown') {
        if (dy <= 0) continue;
        primary = dy;
        cross = Math.abs(dx);
      } else {
        if (dy >= 0) continue;
        primary = -dy;
        cross = Math.abs(dx);
      }

      const score = primary + cross * 2;
      if (!best || score < best.score) best = { tile, score };
    }

    return best ? best.tile : null;
  }

  document.addEventListener('keydown', (event) => {
    if (!['ArrowRight', 'ArrowLeft', 'ArrowUp', 'ArrowDown'].includes(event.key)) return;
    event.preventDefault();
    const current = document.activeElement.closest('[data-tile]') || tiles[index];
    const next = closestInDirection(current, event.key);
    if (next) focusTile(next);
  });

  requestAnimationFrame(() => tiles[0].focus());
})();
