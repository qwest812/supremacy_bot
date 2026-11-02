'use strict';
// Интерфейс к журналам бота: заходы, решения и карта партии. Данные — то,
// что бот пишет в LOG_DIR: decisions.jsonl, snapshots/<заход>.json, map.json.

const $ = (id) => document.getElementById(id);
const SVGNS = 'http://www.w3.org/2000/svg';
const CATS = {
  build: 'Стройка', recruit: 'Войска', attack: 'Атака',
  defense: 'Оборона', market: 'Рынок', diplomacy: 'Дипломатия',
};

const S = {
  games: [], game: null,
  events: [], total: 0,
  visits: [],          // записи type=visit по порядку
  ends: {},            // номер захода → запись type=end
  map: null, paths: {}, labels: {}, centers: {},
  snaps: {}, visit: 0, snap: null,
  cats: new Set(Object.keys(CATS)), notes: true, search: '',
  sel: 0, view: null, playing: null,
  layers: { armies: true, orders: true, marches: true, labels: true },
};

// ---------- загрузка ----------

async function getJSON(url) {
  const r = await fetch(url, { cache: 'no-store' });
  if (!r.ok) throw new Error(r.status + ' ' + url);
  return r.json();
}

async function loadGames() {
  S.games = await getJSON('api/games');
  const sel = $('game');
  const cur = sel.value;
  sel.innerHTML = '';
  for (const g of S.games) {
    const o = document.createElement('option');
    let label = g.id;
    if (g.last) label += ` — ${g.last.nation || ''}, день ${g.last.day}`;
    o.value = g.id; o.textContent = `${label} (${g.visits} заход.)`;
    sel.appendChild(o);
  }
  if (!S.games.length) {
    const o = document.createElement('option');
    o.textContent = 'журналов нет — запустите бота';
    sel.appendChild(o);
    $('mapEmpty').hidden = false;
    return;
  }
  sel.value = cur && S.games.some(g => g.id === cur) ? cur : S.games[0].id;
  if (sel.value !== S.game) await loadGame(sel.value);
}

async function loadGame(id) {
  S.game = id; S.events = []; S.total = 0; S.visits = []; S.ends = {};
  S.snaps = {}; S.sel = 0; S.snap = null;
  await pullEvents();
  try {
    S.map = await getJSON(`api/games/${id}/map`);
    $('mapEmpty').hidden = true;
  } catch (e) {
    S.map = null;
    $('mapEmpty').hidden = false;
  }
  buildMap();
  const last = S.visits.length ? S.visits[S.visits.length - 1].visit : 0;
  const h = hashParams();
  await showVisit(+h.get('visit') || last, true);
  if (+h.get('p')) { centerOn(+h.get('p')); selectProvince(+h.get('p')); }
  if (h.get('tab')) switchTab(h.get('tab'));
}

// Ссылка на вид: #visit=3&p=128&tab=summary — открыть сразу нужное.
function hashParams() { return new URLSearchParams(location.hash.slice(1)); }
function saveHash() {
  const h = new URLSearchParams();
  if (S.visit) h.set('visit', S.visit);
  if (S.sel) h.set('p', S.sel);
  const tab = document.querySelector('.tabs button.on');
  if (tab && tab.dataset.tab !== 'now') h.set('tab', tab.dataset.tab);
  history.replaceState(null, '', '#' + h.toString());
}

async function pullEvents() {
  const r = await getJSON(`api/games/${S.game}/events?after=${S.total}`);
  S.total = r.total;
  let fresh = false;
  for (const e of r.events) {
    if (!e.type) e.type = 'decision';
    S.events.push(e);
    if (e.type === 'visit') { S.visits.push(e); fresh = true; }
    if (e.type === 'end') S.ends[e.visit] = e;
  }
  const slider = $('visit');
  slider.min = S.visits.length ? S.visits[0].visit : 1;
  slider.max = S.visits.length ? S.visits[S.visits.length - 1].visit : 1;
  return fresh;
}

async function showVisit(n, fit) {
  if (!n) { renderAll(); return; }
  S.visit = n;
  $('visit').value = n;
  if (!S.snaps[n]) {
    try { S.snaps[n] = await getJSON(`api/games/${S.game}/snapshots/${n}`); }
    catch (e) { S.snaps[n] = null; }
  }
  S.snap = S.snaps[n];
  // Прошлый заход нужен вкладке «Сейчас»: что изменилось с тех пор.
  const i = S.visits.findIndex(x => x.visit === n);
  const pv = i > 0 ? S.visits[i - 1].visit : 0;
  if (pv && S.snaps[pv] === undefined) {
    try { S.snaps[pv] = await getJSON(`api/games/${S.game}/snapshots/${pv}`); }
    catch (e) { S.snaps[pv] = null; }
  }
  if (fit && S.snap) fitOurs();
  renderAll();
  saveHash();
}

// ---------- карта ----------

function playerColor(id) {
  const p = S.snap && S.snap.players[id];
  if (p && p.color) return p.color;
  if (!id || id <= 0) return 'var(--land-none)';
  return `hsl(${(id * 47) % 360} 35% 62%)`;
}

function buildMap() {
  const g = $('provinces'), lg = $('labels');
  g.innerHTML = ''; lg.innerHTML = ''; S.paths = {}; S.labels = {}; S.centers = {};
  if (!S.map) return;
  for (const p of S.map.provinces) {
    const pts = p.points;
    if (!pts || pts.length < 6) continue;
    let d = `M${pts[0]},${pts[1]}`;
    for (let i = 2; i < pts.length; i += 2) d += `L${pts[i]},${pts[i + 1]}`;
    const el = document.createElementNS(SVGNS, 'path');
    el.setAttribute('d', d + 'Z');
    el.setAttribute('class', 'prov');
    el.dataset.id = p.id;
    g.appendChild(el);
    S.paths[p.id] = el;
    S.centers[p.id] = p.center;
    const t = document.createElementNS(SVGNS, 'text');
    t.setAttribute('x', p.center[0]); t.setAttribute('y', p.center[1]);
    t.setAttribute('class', 'plabel');
    t.textContent = p.name || '';
    lg.appendChild(t);
    S.labels[p.id] = t;
  }
  if (!S.view) S.view = { x: 0, y: 0, w: S.map.width, h: S.map.height };
}

function provOf(id) { return S.snap ? S.snap.provinces.find(p => p.id === id) : null; }

function renderProvinces() {
  if (!S.map) return;
  const snap = S.snap;
  const byID = {};
  if (snap) for (const p of snap.provinces) byID[p.id] = p;
  const zoomed = S.view && S.map && S.view.w < S.map.width / 3;
  const marks = $('marks');
  marks.innerHTML = '';
  for (const id in S.paths) {
    const el = S.paths[id], p = byID[id];
    const owner = p ? p.owner : 0;
    const pl = snap && snap.players[owner];
    el.style.fill = playerColor(owner);
    el.style.fillOpacity = owner === (snap && snap.me) ? 0.95 : 0.75;
    let cls = 'prov';
    if (snap && owner === snap.me) cls += ' ours';
    else if (pl && pl.hostile) cls += ' hostile';
    if (p && p.danger) cls += ' danger';
    if (+id === S.sel) cls += ' sel';
    el.setAttribute('class', cls);
    // Цель войны — штриховкой поверх.
    if (pl && pl.target) {
      const h = el.cloneNode();
      h.removeAttribute('style'); h.setAttribute('class', 'prov');
      h.style.fill = 'url(#hatch)'; h.style.pointerEvents = 'none'; h.style.stroke = 'none';
      marks.appendChild(h);
    }
    const t = S.labels[id];
    if (t) {
      const ours = snap && owner === snap.me;
      const show = S.layers.labels && (ours || zoomed || (pl && pl.hostile));
      t.style.display = show ? '' : 'none';
      let label = (S.map.provinces.find(x => x.id === +id) || {}).name || '';
      if (p && p.capital) label = '★ ' + label;
      if (p && p.double) label += ' ×2';
      t.textContent = label;
    }
  }
  const fs = S.view ? S.view.w / 110 : 9;
  $('labels').style.setProperty('--lf', Math.max(3, Math.min(fs, 40)) + 'px');
}

function pxToMap(px) {
  const svg = $('map');
  return S.view ? px * S.view.w / Math.max(svg.clientWidth, 1) : px;
}

function renderArmies() {
  const g = $('armies'), mg = $('marches');
  g.innerHTML = ''; mg.innerHTML = '';
  if (!S.snap) return;
  g.style.display = S.layers.armies ? '' : 'none';
  mg.style.display = S.layers.marches ? '' : 'none';
  const me = S.snap.me;
  for (const a of S.snap.armies) {
    const pl = S.snap.players[a.owner];
    const kind = a.owner === me ? 'ours' : (pl && pl.hostile ? 'hostile' : 'other');
    if (kind === 'other' && !S.layers.armies) continue;
    const r = pxToMap(Math.min(3 + Math.sqrt(a.power) * 0.9, 16));
    const c = document.createElementNS(SVGNS, 'circle');
    c.setAttribute('cx', a.x); c.setAttribute('cy', a.y); c.setAttribute('r', r);
    c.setAttribute('class', `army ${kind}${a.fighting ? ' fight' : ''}`);
    c.dataset.army = a.id;
    g.appendChild(c);
    if (a.to) {
      const l = document.createElementNS(SVGNS, 'line');
      l.setAttribute('x1', a.x); l.setAttribute('y1', a.y);
      l.setAttribute('x2', a.to[0]); l.setAttribute('y2', a.to[1]);
      l.setAttribute('class', 'march');
      mg.appendChild(l);
    }
  }
}

function orderKind(d) {
  if (d.rule === 'отступление') return 'retreat';
  return d.category === 'defense' ? 'defense' : 'attack';
}

function renderOrders() {
  const g = $('orders');
  g.innerHTML = '';
  if (!S.snap) return;
  g.style.display = S.layers.orders ? '' : 'none';
  S.snap.decisions.forEach((d, i) => {
    if (d.note) return;
    if (d.path && d.path.length > 1) {
      const pl = document.createElementNS(SVGNS, 'polyline');
      pl.setAttribute('points', d.path.map(p => p.join(',')).join(' '));
      let cls = 'order ' + orderKind(d);
      if (d.outcome && d.outcome.startsWith('отказ')) cls += ' refused';
      pl.setAttribute('class', cls);
      pl.dataset.i = i;
      g.appendChild(pl);
    } else if (d.province && (d.category === 'build' || d.category === 'recruit') && S.centers[d.province]) {
      const [x, y] = S.centers[d.province];
      const c = document.createElementNS(SVGNS, 'rect');
      const s = pxToMap(7);
      const off = d.category === 'recruit' ? s : 0;
      c.setAttribute('x', x - s / 2 + off); c.setAttribute('y', y - s * 1.6);
      c.setAttribute('width', s); c.setAttribute('height', s);
      c.setAttribute('rx', s / 4);
      c.setAttribute('class', d.category === 'build' ? 'buildmark' : 'recruitmark');
      c.dataset.i = i;
      g.appendChild(c);
    }
  });
}

function renderLegend() {
  const el = $('legend');
  if (!S.snap) { el.innerHTML = ''; return; }
  const counts = {};
  for (const p of S.snap.provinces) counts[p.owner] = (counts[p.owner] || 0) + 1;
  const me = S.snap.me;
  const ids = Object.keys(counts).map(Number).filter(id => id > 0)
    .filter(id => id === me || S.snap.players[id]?.hostile || S.snap.players[id]?.target);
  let html = ids.map(id => {
    const p = S.snap.players[id] || {};
    const tag = id === me ? ' (мы)' : p.target ? ' — цель' : p.hostile ? ' — война' : '';
    return `<span><i style="background:${playerColor(id)}"></i>${esc(p.nation || id)}${tag}</span>`;
  }).join('');
  html += `<span><span class="ln" style="border-color:var(--attack)"></span>атака/захват</span>
    <span><span class="ln" style="border-color:var(--defense)"></span>оборона</span>
    <span><span class="ln" style="border-color:var(--retreat);border-top-style:dashed"></span>отступление</span>
    <span><i style="background:var(--build)"></i>стройка</span>
    <span><i style="background:var(--recruit)"></i>войска</span>`;
  el.innerHTML = html;
}

// ---------- вид карты: масштаб и сдвиг ----------

function applyView() {
  const svg = $('map');
  if (!S.view) return;
  const cw = svg.clientWidth || 1, ch = svg.clientHeight || 1;
  S.view.h = S.view.w * ch / cw;
  svg.setAttribute('viewBox', `${S.view.x} ${S.view.y} ${S.view.w} ${S.view.h}`);
}

function fitBox(x0, y0, x1, y1) {
  const svg = $('map');
  const cw = svg.clientWidth || 1, ch = svg.clientHeight || 1;
  let w = Math.max(x1 - x0, 50), h = Math.max(y1 - y0, 50);
  if (w / h < cw / ch) w = h * cw / ch;
  S.view = { x: (x0 + x1) / 2 - w / 2, y: (y0 + y1) / 2 - (w * ch / cw) / 2, w, h: w * ch / cw };
  applyView();
}

function fitOurs() {
  if (!S.map || !S.snap) return;
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  for (const p of S.snap.provinces) {
    if (p.owner !== S.snap.me) continue;
    const c = S.centers[p.id];
    if (!c) continue;
    x0 = Math.min(x0, c[0]); y0 = Math.min(y0, c[1]); x1 = Math.max(x1, c[0]); y1 = Math.max(y1, c[1]);
  }
  if (!isFinite(x0)) { fitBox(0, 0, S.map.width, S.map.height); return; }
  const padX = (x1 - x0) * 0.25 + 80, padY = (y1 - y0) * 0.15 + 60;
  fitBox(x0 - padX, y0 - padY, x1 + padX, y1 + padY);
}

function zoomAt(factor, cx, cy) {
  if (!S.view || !S.map) return;
  const w = Math.min(Math.max(S.view.w * factor, 60), S.map.width * 1.5);
  const k = w / S.view.w;
  S.view.x = cx - (cx - S.view.x) * k;
  S.view.y = cy - (cy - S.view.y) * k;
  S.view.w = w;
  applyView();
  renderProvinces(); renderArmies(); renderOrders();
}

function eventToMap(ev) {
  const svg = $('map'), r = svg.getBoundingClientRect();
  return [S.view.x + (ev.clientX - r.left) / r.width * S.view.w, S.view.y + (ev.clientY - r.top) / r.height * S.view.h];
}

function centerOn(id) {
  const c = S.centers[id];
  if (!c || !S.view) return;
  S.view.x = c[0] - S.view.w / 2; S.view.y = c[1] - S.view.h / 2;
  applyView();
}

function initMapEvents() {
  const svg = $('map');
  let drag = null;
  svg.addEventListener('wheel', (ev) => {
    ev.preventDefault();
    const [x, y] = eventToMap(ev);
    zoomAt(ev.deltaY > 0 ? 1.18 : 1 / 1.18, x, y);
  }, { passive: false });
  svg.addEventListener('pointerdown', (ev) => {
    drag = { x: ev.clientX, y: ev.clientY, vx: S.view?.x, vy: S.view?.y, moved: false };
    svg.setPointerCapture(ev.pointerId);
  });
  svg.addEventListener('pointermove', (ev) => {
    if (drag && S.view) {
      const dx = ev.clientX - drag.x, dy = ev.clientY - drag.y;
      if (Math.abs(dx) + Math.abs(dy) > 3) { drag.moved = true; svg.classList.add('drag'); }
      const k = S.view.w / svg.clientWidth;
      S.view.x = drag.vx - dx * k; S.view.y = drag.vy - dy * k;
      applyView();
      hideTip();
      return;
    }
    hover(ev);
  });
  svg.addEventListener('pointerup', (ev) => {
    svg.classList.remove('drag');
    const wasDrag = drag && drag.moved;
    drag = null;
    if (wasDrag) return;
    const el = document.elementFromPoint(ev.clientX, ev.clientY);
    if (el && el.dataset.id) selectProvince(+el.dataset.id);
  });
  svg.addEventListener('pointerleave', hideTip);
  $('zin').onclick = () => S.view && zoomAt(1 / 1.4, S.view.x + S.view.w / 2, S.view.y + S.view.h / 2);
  $('zout').onclick = () => S.view && zoomAt(1.4, S.view.x + S.view.w / 2, S.view.y + S.view.h / 2);
  $('zfit').onclick = () => { fitOurs(); renderAll(); };
  window.addEventListener('resize', () => { applyView(); renderArmies(); renderOrders(); });
  document.querySelectorAll('.layers input').forEach(cb => {
    cb.onchange = () => { S.layers[cb.dataset.layer] = cb.checked; renderAll(); };
  });
}

function hover(ev) {
  const el = ev.target;
  const tip = $('tip');
  let html = '';
  if (el.dataset && el.dataset.army && S.snap) {
    const a = S.snap.armies.find(x => String(x.id) === el.dataset.army);
    if (a) {
      const pl = S.snap.players[a.owner] || {};
      html = `<b>${esc(pl.nation || a.owner)}</b>${a.owner === S.snap.me ? ' (мы)' : ''}<br>${esc(a.units)}<br>сила ${a.power.toFixed(0)}${a.fighting ? ' · <b>в бою</b>' : ''}`;
    }
  } else if (el.dataset && el.dataset.i !== undefined && S.snap) {
    const d = S.snap.decisions[+el.dataset.i];
    if (d) html = `<b>${esc(d.what)}</b><br>${esc(d.why)}`;
  } else if (el.dataset && el.dataset.id) {
    const id = +el.dataset.id, p = provOf(id);
    const name = (S.map.provinces.find(x => x.id === id) || {}).name || id;
    if (p) {
      const pl = S.snap.players[p.owner] || {};
      html = `<b>${esc(name)}</b>${p.capital ? ' ★' : ''}<br>${esc(pl.nation || 'ничья')}${p.owner === S.snap.me ? ' (мы)' : pl.hostile ? ' · война' : ''}<br>${esc(p.resource || '')}${p.double ? ' ×2' : ''} · ценность ${p.value.toFixed(1)}`;
    } else html = esc(name);
  }
  if (!html) { hideTip(); return; }
  const r = $('map').getBoundingClientRect();
  tip.innerHTML = html;
  tip.style.display = 'block';
  tip.style.left = Math.min(ev.clientX - r.left + 14, r.width - 290) + 'px';
  tip.style.top = (ev.clientY - r.top + 14) + 'px';
}
function hideTip() { $('tip').style.display = 'none'; }

// ---------- решения ----------

function outcomeBadge(d) {
  if (d.note) return '<span class="out note">заметка</span>';
  const o = d.outcome || 'план';
  if (o.startsWith('отказ')) return '<span class="out bad">ОТКАЗ</span>';
  if (o === 'сделано') return '<span class="out done">СДЕЛАНО</span>';
  return '<span class="out plan">ПЛАН</span>';
}

function visibleDecisions() {
  if (!S.snap) return [];
  const q = S.search.trim().toLowerCase();
  return S.snap.decisions.map((d, i) => ({ d, i })).filter(({ d }) =>
    S.cats.has(d.category) && (S.notes || !d.note) &&
    (!q || (d.what + ' ' + d.why + ' ' + d.rule).toLowerCase().includes(q)));
}

function renderChips() {
  const counts = {};
  if (S.snap) for (const d of S.snap.decisions) if (S.notes || !d.note) counts[d.category] = (counts[d.category] || 0) + 1;
  $('chips').innerHTML = Object.entries(CATS).map(([k, v]) =>
    `<span class="chip cat-${k} ${S.cats.has(k) ? 'on' : ''}" data-cat="${k}" style="color:var(--c)">${v}<b>${counts[k] || 0}</b></span>`).join('');
  $('chips').querySelectorAll('.chip').forEach(ch => {
    ch.onclick = (ev) => {
      const c = ch.dataset.cat;
      if (ev.altKey || ev.metaKey) { S.cats = new Set([c]); }
      else if (S.cats.has(c)) S.cats.delete(c); else S.cats.add(c);
      renderDecisions();
    };
  });
}

function renderDecisions() {
  renderChips();
  const list = $('list');
  const items = visibleDecisions();
  if (!S.snap) {
    list.innerHTML = '<li class="muted">Снимка этого захода нет.</li>';
    return;
  }
  list.innerHTML = items.length ? items.map(({ d, i }) => `
    <li class="item cat-${d.category} ${d.note ? 'note' : ''}" data-i="${i}">
      <div class="head">${outcomeBadge(d)} <span class="what">${esc(d.what)}</span> <span class="rule">${esc(d.rule)}</span></div>
      <div class="why">${esc(d.why)}${d.outcome && d.outcome.startsWith('отказ') ? '<br><b style="color:var(--bad)">' + esc(d.outcome) + '</b>' : ''}</div>
    </li>`).join('') : '<li class="muted">Ничего не подходит под фильтр.</li>';
  list.querySelectorAll('.item').forEach(li => {
    const d = S.snap.decisions[+li.dataset.i];
    li.onmouseenter = () => highlight(d, +li.dataset.i, true);
    li.onmouseleave = () => highlight(d, +li.dataset.i, false);
    li.onclick = () => { if (d.province) { centerOn(d.province); selectProvince(d.province, true); } };
  });
  const end = S.ends[S.visit];
  $('endline').textContent = end ? `Итог: ${end.summary}. Следующий заход ${fmtTime(end.next)} — ${end.why}` : '';
}

function highlight(d, i, on) {
  if (d.province && S.paths[d.province]) S.paths[d.province].classList.toggle('hl', on);
  const o = $('orders').querySelector(`[data-i="${i}"]`);
  if (o) o.classList.toggle('hl', on);
}

// ---------- провинция ----------

function selectProvince(id, keepTab) {
  S.sel = id;
  renderProvinces();
  renderProvinceTab();
  if (!keepTab) switchTab('province');
  saveHash();
}

function renderProvinceTab() {
  const el = $('tab-province');
  const id = S.sel;
  if (!id || !S.snap) { el.innerHTML = '<p class="muted">Нажмите на провинцию на карте.</p>'; return; }
  const p = provOf(id) || {};
  const name = (S.map.provinces.find(x => x.id === id) || {}).name || id;
  const pl = S.snap.players[p.owner] || {};
  const rel = p.owner === S.snap.me ? 'мы' : pl.target ? 'цель войны' : pl.hostile ? 'война' : pl.bot ? 'бот' : pl.human ? 'игрок' : '';
  let html = `<h3>${esc(name)}${p.capital ? ' ★ столица' : ''}</h3>
  <table class="kv">
    <tr><td>Владелец</td><td>${esc(pl.nation || 'ничья')} ${rel ? '· ' + rel : ''}</td></tr>
    <tr><td>Ресурс</td><td>${esc(p.resource || '—')}${p.double ? ' · <b>двойной</b>' : ''}</td></tr>
    <tr><td>Ценность</td><td>${(p.value || 0).toFixed(1)}</td></tr>
    <tr><td>Боевой дух</td><td>${p.morale ? Math.round(p.morale) + '%' : '—'}</td></tr>
    ${p.danger ? '<tr><td>Угроза</td><td style="color:var(--defense)"><b>под угрозой</b></td></tr>' : ''}
  </table>`;
  if (p.buildings && p.buildings.length) html += `<h3>Здания</h3>` + p.buildings.map(b => `<span class="pill">${esc(b)}</span>`).join('');
  if (p.building && p.building.length) html += `<h3>Строится</h3>` + p.building.map(b => `<span class="pill">${esc(b)}</span>`).join('');
  const armies = S.snap.armies.filter(a => a.location === id);
  if (armies.length) {
    html += `<h3>Армии здесь</h3><table class="kv">` + armies.map(a => {
      const o = S.snap.players[a.owner] || {};
      return `<tr><td>${esc(o.nation || a.owner)}${a.owner === S.snap.me ? ' (мы)' : ''}</td><td>${esc(a.units)} · сила ${a.power.toFixed(0)}${a.fighting ? ' · в бою' : ''}</td></tr>`;
    }).join('') + '</table>';
  }
  const now = S.snap.decisions.filter(d => d.province === id);
  html += `<h3>Решения в этом заходе</h3>` + (now.length ? '<ol class="list">' + now.map(d => `
    <li class="item cat-${d.category} ${d.note ? 'note' : ''}"><div class="head">${outcomeBadge(d)} <span class="what">${esc(d.what)}</span></div><div class="why">${esc(d.why)}</div></li>`).join('') + '</ol>'
    : '<p class="muted">Нет.</p>');
  const hist = S.events.filter(e => e.type === 'decision' && e.province === id && !e.note && e.visit !== S.visit).slice(-25).reverse();
  if (hist.length) {
    html += `<h3>История</h3><ol class="list">` + hist.map(e => `
      <li class="item cat-${e.category}"><div class="head"><span class="rule">заход ${e.visit} · день ${e.day}</span> <span class="what">${esc(e.what)}</span></div><div class="why">${esc(e.why)}</div></li>`).join('') + '</ol>';
  }
  el.innerHTML = html;
}


// ---------- сейчас: что в партии и что изменилось ----------

function provName(id) {
  const p = S.map && S.map.provinces.find(x => x.id === id);
  return p && p.name ? p.name : 'провинция ' + id;
}
function nationName(snap, id) {
  const p = snap && snap.players[id];
  return p && p.nation ? p.nation : (id > 0 ? 'игрок ' + id : 'никто');
}
// nearestProv — провинция, чей центр ближе всего к точке (куда идёт армия).
function nearestProv(pt) {
  let best = 0, bd = Infinity;
  for (const id in S.centers) {
    const c = S.centers[id], d = (c[0] - pt[0]) ** 2 + (c[1] - pt[1]) ** 2;
    if (d < bd) { bd = d; best = +id; }
  }
  return best;
}
function plink(id) { return `<a href="#" class="plink" data-p="${id}">${esc(provName(id))}</a>`; }
function num(v) { return Math.round(v).toLocaleString('ru'); }

// changes — события между прошлым и этим заходом, словами.
function changes(prev, cur) {
  const out = [];
  if (!prev) return out;
  const me = cur.me;
  const was = {};
  for (const p of prev.provinces) was[p.id] = p;
  let others = 0;
  for (const p of cur.provinces) {
    const o = was[p.id];
    if (!o || o.owner === p.owner) continue;
    if (p.owner === me) out.push({ kind: 'good', text: `Взяли ${plink(p.id)} у ${esc(nationName(prev, o.owner))}` });
    else if (o.owner === me) out.push({ kind: 'bad', text: `Потеряли ${plink(p.id)} — теперь у ${esc(nationName(cur, p.owner))}` });
    else others++;
  }
  for (const p of cur.provinces) {
    const o = was[p.id];
    if (!o || p.owner !== me || o.owner !== me) continue;
    const before = new Set(o.buildings || []);
    for (const b of p.buildings || []) {
      if (!before.has(b) && !b.includes('недострой')) out.push({ kind: 'good', text: `Достроено: ${esc(b)} в ${plink(p.id)}` });
    }
  }
  const ours = (s) => s.armies.filter(a => a.owner === s.me);
  const pw = (list) => list.reduce((t, a) => t + a.power, 0);
  const a0 = ours(prev), a1 = ours(cur);
  const d = pw(a1) - pw(a0);
  if (Math.abs(d) >= 1) out.push({ kind: d > 0 ? 'good' : 'bad',
    text: `Сила наших армий ${d > 0 ? 'выросла' : 'упала'} на ${num(Math.abs(d))}: ${num(pw(a0))} → ${num(pw(a1))} (армий ${a0.length} → ${a1.length})` });
  const ids = new Set(a1.map(a => a.id));
  const gone = a0.filter(a => !ids.has(a.id));
  if (gone.length) out.push({ kind: 'info', text: `Армий больше нет: ${gone.length} (${gone.map(a => `${esc(a.units)} в ${plink(a.location)}`).join('; ')}) — слились в стак или погибли` });
  const fights = a1.filter(a => a.fighting);
  if (fights.length) out.push({ kind: 'bad', text: `В бою: ${fights.map(a => `${esc(a.units)} в ${plink(a.location)}`).join('; ')}` });
  if (others) out.push({ kind: 'info', text: `У других стран сменили хозяина провинций: ${others}` });
  return out;
}

function renderNow() {
  const el = $('tab-now');
  const snap = S.snap;
  const v = S.visits.find(x => x.visit === S.visit);
  if (!snap || !v) { el.innerHTML = '<p class="muted">Заходов ещё не было: запустите бота.</p>'; return; }
  const i = S.visits.findIndex(x => x.visit === S.visit);
  const prevVisit = i > 0 ? S.visits[i - 1] : null;
  const prev = prevVisit ? S.snaps[prevVisit.visit] : null;
  const end = S.ends[S.visit];
  const last = i === S.visits.length - 1;
  let html = `<div class="now-head">
    <div><b>Заход №${v.visit}</b> · день ${v.day} · ${fmtTime(v.time)} ${v.dry ? '<span class="badge dry">только смотрим</span>' : '<span class="badge live">играем</span>'}</div>
    ${end && end.next ? `<div class="muted">${last ? 'Следующий заход' : 'Следующий был намечен'}: ${fmtTime(end.next)} — ${esc(end.why || '')}</div>` : ''}
    ${end && end.summary ? `<div class="muted">Сделано: ${esc(end.summary)}</div>` : ''}
  </div>`;

  // Что изменилось.
  const ch = changes(prev, snap);
  html += `<h3>Что изменилось${prevVisit ? ' с захода №' + prevVisit.visit + ' (' + fmtTime(prevVisit.time) + ')' : ''}</h3>`;
  if (!prevVisit) html += '<p class="muted">Это первый заход — сравнивать не с чем.</p>';
  else if (!ch.length) html += '<p class="muted">Ничего заметного.</p>';
  else html += '<ul class="changes">' + ch.map(c => `<li class="${c.kind}">${c.text}</li>`).join('') + '</ul>';

  // Отказы игры в этом заходе.
  const refused = snap.decisions.filter(d => !d.note && (d.outcome || '').startsWith('ОТКАЗ'));
  if (refused.length) {
    html += '<h3>Игра отказала</h3><ul class="changes">' + refused.map(d =>
      `<li class="bad">${esc(d.what)} — ${esc(d.outcome)}</li>`).join('') + '</ul>';
  }

  // Угрозы.
  const danger = snap.provinces.filter(p => p.owner === snap.me && p.danger);
  html += '<h3>Угрозы</h3>' + (danger.length
    ? '<ul class="changes">' + danger.map(p => {
        const near = snap.armies.filter(a => a.owner !== snap.me && snap.players[a.owner] && snap.players[a.owner].hostile &&
          (a.location === p.id)).reduce((t, a) => t + a.power, 0);
        return `<li class="bad">${plink(p.id)} под угрозой${near ? ' — враг внутри, сила ' + num(near) : ' — враг у границы'}</li>`;
      }).join('') + '</ul>'
    : '<p class="muted">Нет: врагов в наших и соседних провинциях не видно.</p>');

  // Войны.
  const wars = Object.entries(snap.players).filter(([, p]) => p.hostile);
  html += '<h3>Войны</h3>' + (wars.length
    ? '<p>' + wars.map(([, p]) => `<span class="pill${p.target ? ' target' : ''}">${esc(p.nation)}${p.target ? ' · цель' : ''}</span>`).join('') + '</p>'
    : '<p class="muted">Ни с кем.</p>');

  // Наши армии.
  const ours = snap.armies.filter(a => a.owner === snap.me).sort((a, b) => b.power - a.power);
  html += `<h3>Наши армии (${ours.length}, сила ${num(ours.reduce((t, a) => t + a.power, 0))})</h3>`;
  html += ours.length ? '<table class="kv armies">' + ours.map(a => {
    let st = 'стоит';
    if (a.fighting) st = '<b class="bad">в бою</b>';
    else if (a.to) st = 'идёт → ' + plink(nearestProv(a.to));
    return `<tr><td>${plink(a.location)}</td><td>${esc(a.units)}<br><span class="muted">сила ${num(a.power)} · ${st}</span></td></tr>`;
  }).join('') + '</table>' : '<p class="muted">Армий нет.</p>';

  // Стройка и производство.
  const busy = snap.provinces.filter(p => p.owner === snap.me && ((p.building || []).length || (p.producing || []).length));
  html += '<h3>Стройка и войска</h3>' + (busy.length ? '<table class="kv">' + busy.map(p =>
    `<tr><td>${plink(p.id)}</td><td>${(p.building || []).map(b => `<span class="pill build">${esc(b)}</span>`).join('')}${(p.producing || []).map(b => `<span class="pill unit">${esc(b)}</span>`).join('')}</td></tr>`).join('') + '</table>'
    : '<p class="muted">Ничего не строится и не производится.</p>');

  // Запасы: на сколько хватит того, что убывает.
  html += '<h3>Запасы</h3><table class="kv">' + (v.resources || []).map(r => {
    let left = '';
    if (r.daily < 0) {
      const days = r.amount / -r.daily;
      left = ` · <span class="${days < 2 ? 'bad' : 'warn'}">хватит на ${days.toFixed(1)} сут</span>`;
    }
    return `<tr><td>${esc(r.name)}</td><td>${num(r.amount)} <span class="muted">(${r.daily >= 0 ? '+' : ''}${num(r.daily)}/сут)</span>${left}</td></tr>`;
  }).join('') + '</table>';

  el.innerHTML = html;
  el.querySelectorAll('.plink').forEach(a => a.onclick = (e) => {
    e.preventDefault();
    const id = +a.dataset.p;
    centerOn(id);
    selectProvince(id, true);
  });
}

// ---------- сводка и графики ----------

function chart(series, current) {
  // series: [{name, color, values:[{x,y}]}]
  const W = 400, H = 120, P = { l: 38, r: 8, t: 8, b: 18 };
  const xs = series.flatMap(s => s.values.map(v => v.x));
  const ys = series.flatMap(s => s.values.map(v => v.y));
  if (!xs.length) return '<p class="muted">Пока один заход — графику не из чего строиться.</p>';
  const x0 = Math.min(...xs), x1 = Math.max(...xs, x0 + 1);
  const y0 = Math.min(0, ...ys), y1 = Math.max(...ys, 1);
  const X = x => P.l + (x - x0) / (x1 - x0) * (W - P.l - P.r);
  const Y = y => H - P.b - (y - y0) / (y1 - y0) * (H - P.t - P.b);
  let svg = `<svg class="chart" viewBox="0 0 ${W} ${H}" preserveAspectRatio="none">`;
  svg += `<line class="axis" x1="${P.l}" y1="${H - P.b}" x2="${W - P.r}" y2="${H - P.b}"/>`;
  svg += `<text class="tick" x="2" y="${Y(y1) + 4}">${short(y1)}</text><text class="tick" x="2" y="${H - P.b}">${short(y0)}</text>`;
  svg += `<text class="tick" x="${P.l}" y="${H - 4}">№${x0}</text><text class="tick" x="${W - P.r - 24}" y="${H - 4}">№${x1}</text>`;
  if (current) svg += `<line class="now" x1="${X(current)}" y1="${P.t}" x2="${X(current)}" y2="${H - P.b}"/>`;
  for (const s of series) {
    if (!s.values.length) continue;
    const d = s.values.map((v, i) => `${i ? 'L' : 'M'}${X(v.x).toFixed(1)},${Y(v.y).toFixed(1)}`).join('');
    svg += `<path d="${d}" style="stroke:${s.color}"/>`;
  }
  svg += '</svg>';
  svg += '<div class="series">' + series.map(s => `<span><i style="background:${s.color}"></i>${esc(s.name)}</span>`).join('') + '</div>';
  return svg;
}

function short(v) {
  const a = Math.abs(v);
  if (a >= 1e6) return (v / 1e6).toFixed(1) + 'M';
  if (a >= 1e3) return (v / 1e3).toFixed(0) + 'k';
  return v.toFixed(0);
}

function renderSummary() {
  const el = $('tab-summary');
  const V = S.visits;
  if (!V.length) { el.innerHTML = '<p class="muted">Заходов ещё не было.</p>'; return; }
  const line = (name, color, f) => ({ name, color, values: V.map(v => ({ x: v.visit, y: f(v) })) });
  const res = (id) => (v) => { const r = (v.resources || []).find(r => r.id === id); return r ? r.amount : 0; };
  let html = '<h3>Территория</h3>' + chart([
    line('провинций', 'var(--ours)', v => v.provinces),
    line('двойных', 'var(--build)', v => v.doubles),
  ], S.visit);
  html += '<h3>Сила армий</h3>' + chart([line('сила', 'var(--accent)', v => v.power || 0), line('армий', 'var(--muted)', v => v.armies)], S.visit);
  html += '<h3>Деньги</h3>' + chart([line('деньги', 'var(--recruit)', res(20))], S.visit);
  html += '<h3>Сырьё</h3>' + chart([
    line('зерно', '#c9a227', res(0)), line('руда', '#8a8f98', res(2)), line('дерево', '#6a994e', res(3)),
    line('уголь', '#444', res(4)), line('нефть', '#8250df', res(5)),
  ], S.visit);
  // Сделано за заход по категориям.
  const done = {};
  for (const e of S.events) if (e.type === 'decision' && !e.note) {
    done[e.category] = done[e.category] || {};
    done[e.category][e.visit] = (done[e.category][e.visit] || 0) + 1;
  }
  const colors = { build: 'var(--build)', recruit: 'var(--recruit)', attack: 'var(--attack)', defense: 'var(--defense)', market: 'var(--market)', diplomacy: 'var(--diplomacy)' };
  html += '<h3>Приказов за заход</h3>' + chart(Object.keys(CATS).filter(k => done[k]).map(k =>
    ({ name: CATS[k], color: colors[k], values: V.map(v => ({ x: v.visit, y: done[k][v.visit] || 0 })) })), S.visit);
  const cur = V.find(v => v.visit === S.visit) || V[V.length - 1];
  html += `<h3>Заход №${cur.visit}</h3><table class="kv">
    <tr><td>Время</td><td>${fmtTime(cur.time)}</td></tr>
    <tr><td>Страна</td><td>${esc(cur.nation)}</td></tr>
    <tr><td>Воюем с</td><td>${cur.wars && cur.wars.length ? esc(cur.wars.join(', ')) : 'ни с кем'}</td></tr>
    <tr><td>Цель войны</td><td>${esc(cur.target || '—')}</td></tr>
    <tr><td>Очки</td><td>${cur.points || 0}</td></tr>
  </table>
  <h3>Запасы</h3><table class="kv">` + (cur.resources || []).map(r =>
    `<tr><td>${esc(r.name)}</td><td>${Math.round(r.amount).toLocaleString('ru')} <span class="muted">(${r.daily >= 0 ? '+' : ''}${Math.round(r.daily).toLocaleString('ru')}/сут)</span></td></tr>`).join('') + '</table>';
  el.innerHTML = html;
}

// ---------- шапка и общий рендер ----------

function renderHeader() {
  const v = S.visits.find(x => x.visit === S.visit);
  const i = S.visits.findIndex(x => x.visit === S.visit);
  const prev = i > 0 ? S.visits[i - 1] : null;
  $('visitLabel').textContent = v ? `заход №${v.visit} из ${S.visits.length} · день ${v.day} · ${fmtTime(v.time)}` : '';
  const mode = $('mode');
  if (v) { mode.textContent = v.dry ? 'только смотрим (DRY_RUN)' : 'играет'; mode.className = 'badge ' + (v.dry ? 'dry' : 'live'); }
  else { mode.textContent = ''; mode.className = 'badge'; }
  const k = $('kpis');
  if (!v) { k.innerHTML = ''; return; }
  const delta = (a, b) => (b === undefined || a === b) ? '' : `<small class="${a > b ? 'up' : 'down'}">${a > b ? '+' : ''}${short(a - b)}</small>`;
  const money = (v.resources || []).find(r => r.id === 20);
  const pm = prev && (prev.resources || []).find(r => r.id === 20);
  k.innerHTML = `
    <div class="kpi"><b>${esc(v.nation)}</b><span>страна</span></div>
    <div class="kpi"><b>${v.day}</b><span>день · x${v.speed}</span></div>
    <div class="kpi"><b>${v.provinces}${delta(v.provinces, prev?.provinces)}</b><span>провинций, двойных ${v.doubles}</span></div>
    <div class="kpi"><b>${Math.round(v.power || 0)}${delta(Math.round(v.power || 0), prev ? Math.round(prev.power || 0) : undefined)}</b><span>сила армий (${v.armies})</span></div>
    <div class="kpi"><b>${money ? short(money.amount) : '—'}${money && pm ? delta(money.amount, pm.amount) : ''}</b><span>деньги ${money ? (money.daily >= 0 ? '+' : '') + short(money.daily) + '/сут' : ''}</span></div>
    <div class="kpi"><b>${v.wars && v.wars.length ? v.wars.length : 0}</b><span>${v.target ? 'цель: ' + esc(v.target) : 'войн'}</span></div>`;
}

function renderAll() {
  renderHeader();
  renderProvinces();
  renderArmies();
  renderOrders();
  renderLegend();
  renderDecisions();
  renderNow();
  renderSummary();
  renderProvinceTab();
}

function switchTab(name) {
  document.querySelectorAll('.tabs button').forEach(b => b.classList.toggle('on', b.dataset.tab === name));
  for (const t of ['now', 'decisions', 'summary', 'province']) $('tab-' + t).hidden = t !== name;
  saveHash();
}

function esc(s) {
  return String(s ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
function fmtTime(t) {
  if (!t) return '';
  const d = new Date(t);
  return d.toLocaleString('ru', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function step(dir) {
  const i = S.visits.findIndex(x => x.visit === S.visit);
  const j = Math.max(0, Math.min(S.visits.length - 1, i + dir));
  if (S.visits[j]) showVisit(S.visits[j].visit);
}

function init() {
  initMapEvents();
  $('game').onchange = (e) => loadGame(e.target.value);
  $('visit').oninput = (e) => showVisit(+e.target.value);
  $('prev').onclick = () => step(-1);
  $('next').onclick = () => step(1);
  $('play').onclick = () => {
    if (S.playing) { clearInterval(S.playing); S.playing = null; $('play').textContent = '▶▶'; return; }
    if (S.visits.length && S.visit === S.visits[S.visits.length - 1].visit) showVisit(S.visits[0].visit);
    $('play').textContent = '❚❚';
    S.playing = setInterval(() => {
      const i = S.visits.findIndex(x => x.visit === S.visit);
      if (i >= S.visits.length - 1) { $('play').click(); return; }
      step(1);
    }, 1300);
  };
  $('search').oninput = (e) => { S.search = e.target.value; renderDecisions(); };
  $('notes').onchange = (e) => { S.notes = e.target.checked; renderDecisions(); };
  document.querySelectorAll('.tabs button').forEach(b => b.onclick = () => switchTab(b.dataset.tab));
  document.addEventListener('keydown', (e) => {
    if (e.target.tagName === 'INPUT' && e.target.type !== 'range') return;
    if (e.key === 'ArrowLeft') step(-1);
    if (e.key === 'ArrowRight') step(1);
  });
  // Слежение: новые заходы подтягиваются сами.
  setInterval(async () => {
    if (!S.game || !$('live').checked) return;
    try {
      const atLast = !S.visits.length || S.visit === S.visits[S.visits.length - 1].visit;
      const fresh = await pullEvents();
      if (fresh && atLast) await showVisit(S.visits[S.visits.length - 1].visit);
      else renderHeader();
      if (!S.map) { try { S.map = await getJSON(`api/games/${S.game}/map`); $('mapEmpty').hidden = true; buildMap(); renderAll(); } catch (e) {} }
    } catch (e) { /* бот перезапускается — попробуем в следующий раз */ }
  }, 10000);
  loadGames().catch(err => { $('list').innerHTML = `<li class="muted">Не удалось загрузить: ${esc(err.message)}</li>`; });
}

init();
