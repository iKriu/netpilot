// NetPilot — оболочка: меню, верхняя панель, вкладки на телефоне, поиск,
// вход, обновление программы и запуск.

import {
  h, fill, icon, api, get, toast, modal, state, refreshStatus, pages, route, router, onHashChange,
  isDirty, askLeave, onAuthLost, isClient, pageName, go, uptime, arr, ago, bytes, activeName, applyDesign, designOf,
} from './core.js';
import { pageOverview } from './overview.js';
import { pageServers, openPicker } from './servers.js';
import { pageRules, explainModal } from './rules.js';
import { pageDevices } from './devices.js';
import { pageConnections } from './connections.js';
import { pageInternet } from './internet.js';
import { pageSettings } from './settings.js';

Object.assign(pages, {
  overview: pageOverview, nodes: pageServers, rules: pageRules, devices: pageDevices,
  connections: pageConnections, internet: pageInternet, settings: pageSettings,
});

const NAV = [
  [null, [['overview', 'Обзор', 'dashboard']]],
  ['Маршрутизация', [['nodes', 'Серверы', 'servers'], ['rules', 'Правила', 'rules'], ['devices', 'Устройства', 'devices'], ['connections', 'Соединения', 'connections']]],
  ['Система', [['internet', 'Интернет', 'globe'], ['settings', 'Настройки', 'settings']]],
];
const TITLES = { overview: 'Обзор', nodes: 'Серверы', rules: 'Правила', devices: 'Устройства', connections: 'Соединения', internet: 'Интернет', settings: 'Настройки', more: 'Ещё' };
const TABS = [['overview', 'Обзор', 'dashboard'], ['nodes', 'Серверы', 'servers'], ['rules', 'Правила', 'rules'], ['devices', 'Устройства', 'devices'], ['more', 'Ещё', 'more']];
const MORE = ['connections', 'internet', 'settings'];

const href = id => '#/' + (id === 'overview' ? '' : id);

// ---------- меню ----------

function drawSide(active) {
  const upd = state.update && state.update.available;
  const collapsed = document.body.classList.contains('side-collapsed');
  fill(document.getElementById('side'), ...[
    h('div', { class: 'brand' }, h('span', { class: 'logo' }, icon('logo', 's')), h('span', { class: 'bname' }, 'NetPilot'),
      h('button', { class: 'collapse', 'aria-label': collapsed ? 'Развернуть меню' : 'Свернуть меню', title: collapsed ? 'Развернуть меню' : 'Свернуть меню', onclick: () => setCollapsed(!collapsed) },
        icon(collapsed ? 'expand' : 'collapse', 's'))),
    NAV.map(([section, items]) => [
      section && h('div', { class: 'navh' }, section),
      items.map(([id, label, ic]) => h('a', { class: 'nav' + (id === active ? ' on' : ''), href: href(id), title: label },
        icon(ic), h('span', { class: 'lbl' }, label),
        id === 'settings' && upd ? h('span', { class: 'new' }, state.update.latest.version)
          : state.counts[id] != null && h('span', { class: 'n' }, state.counts[id]))),
    ]),
    h('div', { class: 'sidefoot' }, h('span', { class: 'avatar' }, 'A'), h('span', { class: 'who' }, 'Администратор'),
      h('button', { class: 'ib', 'aria-label': 'Выйти', title: 'Выйти', onclick: logout }, icon('logout')))].flat(Infinity).filter(Boolean));
}

function setCollapsed(on) {
  document.body.classList.toggle('side-collapsed', on);
  try { localStorage.setItem('np-side-collapsed', on ? '1' : ''); } catch {}
  drawSide(pageName());
}

function drawTabs(active) {
  const cur = MORE.includes(active) ? 'more' : active;
  const upd = state.update && state.update.available;
  fill(document.getElementById('tabs'), ...TABS.map(([id, label, ic]) =>
    id === 'more'
      ? h('button', { class: 'tab' + (cur === 'more' ? ' on' : ''), style: 'border:0;background:transparent', onclick: moreSheet }, icon(ic, 'l'), label, upd && h('span', { class: 'new' }))
      : h('a', { class: 'tab' + (cur === id ? ' on' : ''), href: href(id) }, icon(ic, 'l'), label)));
}

// «Ещё» на телефоне — шторка с остальными разделами.
function moreSheet() {
  const s = state.status || {};
  const upd = state.update && state.update.available;
  let close;
  const item = (id, ic, title, sub) => h('a', { class: 'row', href: href(id), onclick: () => close() },
    h('span', { class: 'ico' }, icon(ic)), h('span', { class: 'grow' }, h('span', { class: 'name' }, title), h('br'), h('span', { class: 'meta' }, sub)), icon('right'));
  close = modal({
    title: 'Ещё', cancelLabel: null,
    body: [
      upd && h('a', { class: 'card acc', href: href('settings'), onclick: () => close(), style: 'padding:14px;display:flex;align-items:center;gap:12px' },
        h('span', { class: 'logo' }, icon('download', 's')), h('span', { class: 'grow' }, h('strong', {}, 'Доступна версия ' + state.update.latest.version), h('br'), h('span', { class: 'meta' }, 'Сейчас ' + state.update.current)), icon('right')),
      h('div', { class: 'card list' },
        item('connections', 'connections', 'Соединения', 'что открывают устройства'),
        item('internet', 'globe', 'Интернет', 'как NetPilot стоит в сети'),
        item('settings', 'settings', 'Настройки', 'режим, ядро, DNS, обновление')),
      h('div', { class: 'card list' },
        h('div', { class: 'row' }, h('span', { class: 'grow' }, h('span', { class: 'name' }, 'Шлюз'), h('br'), h('span', { class: 'meta' }, s.running ? `${s.core} · работает ${uptime(s.startedAt)}` : 'остановлен')),
          coreButton(true)),
        h('button', { class: 'row', style: 'width:100%;border:0;background:transparent;color:var(--bad);text-align:left', onclick: () => { close(); logout(); } }, icon('logout'), h('span', { class: 'grow strong' }, 'Выйти'))),
    ],
  });
}

// ---------- верхняя панель ----------

let activeCache = null; // имя активного сервера для кнопки наверху
export async function refreshActive() {
  try {
    const [settings, nodes, groups] = await Promise.all([get('/settings'), get('/nodes'), get('/groups')]);
    activeCache = activeName(settings, nodes, groups);
    // Сохранённое оформление; в настройках до сохранения показывается черновик.
    state.savedDesign = designOf(settings);
    if (pageName() !== 'settings') applyDesign(state.savedDesign);
  } catch {}
  drawTop(pageName());
}

function coreButton(small) {
  const s = state.status || {};
  return h('button', { class: 'btn' + (small ? ' sm' : '') + (s.running ? '' : ' p'), onclick: async e => {
    const b = e.currentTarget;
    b.classList.add('busy');
    try {
      await api('POST', s.running ? '/core/stop' : '/core/start');
      toast(s.running ? 'Шлюз остановлен' : 'Шлюз запущен', 'ok');
    } catch (err) { toast(err.message, 'err'); }
    await refreshStatus();
    route();
  } }, icon(s.running ? 'stop' : 'play', 's'), s.running ? 'Остановить' : 'Запустить');
}
export { coreButton };

function stateChip() {
  const s = state.status;
  if (!s) return h('span', { class: 'state off' }, h('span', { class: 'dot' }), '…');
  // Ядро упало, и сторож перезапускает его сам: это не остановка и ещё не ошибка.
  // Отдельного класса .state.warn в стилях нет — цвета предупреждения задаём здесь.
  if (s.restarting) return h('span', { class: 'state', style: 'background:var(--warns);color:var(--warn)', title: s.error || '' }, h('span', { class: 'dot warn' }), h('span', { class: 'lbl-long' }, 'Ядро перезапускается…'), h('span', { class: 'lbl-short' }, 'Перезапуск…'));
  if (s.running) return h('span', { class: 'state', title: `${s.core} ${s.coreVersion || ''}` }, h('span', { class: 'dot ok' }), h('span', { class: 'lbl-long' }, 'Шлюз работает'), h('span', { class: 'lbl-short' }, 'Работает'), h('span', { class: 'lbl-long' }, ' · ' + uptime(s.startedAt)));
  if (s.error) return h('span', { class: 'state bad', title: s.error }, h('span', { class: 'dot bad' }), 'Ошибка ядра');
  return h('span', { class: 'state off' }, h('span', { class: 'dot' }), h('span', { class: 'lbl-long' }, 'Шлюз остановлен'), h('span', { class: 'lbl-short' }, 'Остановлен'));
}

function drawTop(active) {
  fill(document.getElementById('top'), 
    h('span', { class: 'logo mlogo' }, icon('logo', 's')),
    h('span', { class: 'mtitle' }, TITLES[active] || ''),
    h('button', { class: 'search', onclick: openPalette, 'aria-label': 'Поиск' }, icon('search', 's'),
      h('span', { class: 'grow' }, 'Устройство, сервер, правило или сайт'), h('span', { class: 'kbd' }, 'Ctrl K')),
    h('span', { class: 'grow' }),
    h('button', { class: 'ib mbtn', 'aria-label': 'Поиск', onclick: openPalette, style: 'display:none' }, icon('search')),
    stateChip(),
    h('button', { class: 'srv', onclick: () => openPicker(() => { refreshActive(); route(); }) },
      h('span', { class: 'dot' + (state.status && state.status.running ? ' ok' : '') }), h('span', { class: 't' }, h('span', { class: 'k' }, 'Активный сервер'), h('span', { class: 'v' }, activeCache || '…')),
      icon('down', 's')));
}

// ---------- поиск по панели (Ctrl K) ----------

async function openPalette() {
  const [net, nodes, rules, groups] = await Promise.all(['/network', '/nodes', '/rules', '/groups'].map(p => get(p).catch(() => null)));
  const input = h('input', { type: 'search', placeholder: 'Устройство, сервер, правило или адрес сайта', 'aria-label': 'Поиск' });
  const res = h('div', { class: 'res' });
  let items = [], cur = 0;
  const close = () => { bg.remove(); document.removeEventListener('keydown', onKey); };
  const run = it => { close(); it.go(); };
  const draw = () => {
    const q = input.value.trim().toLowerCase();
    const groupsOut = [];
    const add = (title, list) => { if (list.length) groupsOut.push([title, list.slice(0, 6)]); };
    const m = s => (s || '').toLowerCase().includes(q);
    if (q) {
      const host = q.replace(/^https?:\/\//, '').split('/')[0];
      if (/^[a-z0-9.-]+\.[a-z]{2,}$/.test(host) || /^\d+\.\d+\.\d+\.\d+$/.test(host)) {
        add('Сайт', [{ ic: 'globe', t: 'Куда пойдёт ' + host, s: 'проверить по правилам', go: () => { go('rules'); setTimeout(() => explainModal(host), 300); } }]);
      }
      add('Устройства', arr(net && net.devices).filter(d => m(d.name) || m(d.ip) || m(d.mac)).map(d => ({ ic: d.type || 'other', t: d.name, s: d.ip || '', go: () => go('devices') })));
      add('Серверы и группы', [...arr(groups).filter(g => m(g.name)).map(g => ({ ic: 'groups', t: g.name, s: 'группа', go: () => go('nodes') })),
        ...arr(nodes).filter(n => m(n.name) || m(n.server)).map(n => ({ ic: 'servers', t: n.name, s: n.protocol, go: () => go('nodes') }))]);
      add('Правила', arr(rules).filter(r => m(r.name)).map(r => ({ ic: 'rules', t: r.name, s: r.enabled ? 'включено' : 'выключено', go: () => go('rules') })));
    }
    add('Разделы', Object.entries(TITLES).filter(([id, t]) => id !== 'more' && (!q || t.toLowerCase().includes(q))).map(([id, t]) => ({ ic: 'right', t, s: '', go: () => go(id) })));
    items = groupsOut.flatMap(g => g[1]);
    cur = Math.min(cur, Math.max(0, items.length - 1));
    let i = 0;
    fill(res, ...(items.length ? groupsOut.map(([title, list]) => [
      h('div', { class: 'cap grp' }, title),
      list.map(it => { const idx = i++; return h('button', { class: 'it' + (idx === cur ? ' on' : ''), onclick: () => run(it), onmouseenter: () => { cur = idx; draw(); } },
        h('span', { class: 'ico', style: 'width:32px;height:32px' }, icon(it.ic, 's')), h('span', { class: 'grow' }, h('span', { class: 'name' }, it.t), it.s && h('span', { class: 'meta' }, ' · ' + it.s))); }),
    ]) : [h('div', { class: 'empty' }, 'Ничего не найдено')]));
  };
  const onKey = e => {
    if (e.key === 'Escape') close();
    else if (e.key === 'ArrowDown') { cur = Math.min(items.length - 1, cur + 1); draw(); e.preventDefault(); }
    else if (e.key === 'ArrowUp') { cur = Math.max(0, cur - 1); draw(); e.preventDefault(); }
    else if (e.key === 'Enter' && items[cur]) { run(items[cur]); e.preventDefault(); }
  };
  input.addEventListener('input', () => { cur = 0; draw(); });
  const bg = h('div', { class: 'modal-bg palette-bg', onmousedown: e => { if (e.target === bg) close(); } },
    h('div', { class: 'palette', role: 'dialog', 'aria-label': 'Поиск' }, h('label', { class: 'pin' }, icon('search'), input, h('span', { class: 'kbd' }, 'Esc')), res));
  document.getElementById('modal-root').append(bg);
  document.addEventListener('keydown', onKey);
  draw();
  input.focus();
}

document.addEventListener('keydown', e => {
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k' && !document.querySelector('.auth')) { e.preventDefault(); openPalette(); }
});

// ---------- обновление программы ----------

export async function loadUpdate() {
  try { state.update = await get('/update'); } catch { return; }
  drawSide(pageName());
  drawTabs(pageName());
  const u = state.update;
  if (u.available) {
    let seen = null;
    try { seen = localStorage.getItem('np-update-seen'); } catch {}
    if (seen !== u.latest.version) {
      toast(`Доступна версия ${u.latest.version}. Установить можно в настройках.`, 'info', { title: 'Обновление NetPilot' });
      try { localStorage.setItem('np-update-seen', u.latest.version); } catch {}
    }
  }
}

const UPDATE_STAGES = [['prepare', 'Подготовка'], ['download', 'Скачивание с GitHub'], ['verify', 'Проверка контрольной суммы'],
  ['probe', 'Пробный запуск новой версии'], ['install', 'Установка'], ['restart', 'Перезапуск — сеть пропадёт на 5–10 секунд']];

function updateScreen(target) {
  const fill = h('div', { class: 'upd-fill' });
  const pct = h('span', { class: 'upd-pct' });
  const stageEl = h('span', { class: 'upd-stage' }, 'Подготовка…');
  const note = h('div', { class: 'hint', style: 'display:flex;gap:10px' }, icon('shield', 's'), h('span', {}, 'Текущая версия сохранится. Если новая не запустится, NetPilot вернёт её сам.'));
  const steps = h('ol', { class: 'upd-steps' }, UPDATE_STAGES.map(([id, label]) => h('li', {}, label)));
  const scr = h('div', { class: 'restart-screen' }, h('div', { class: 'upd-card' },
    h('div', { style: 'display:flex;align-items:center;gap:14px' }, h('span', { class: 'logo', style: 'width:44px;height:44px;border-radius:12px' }, icon('logo')),
      h('div', { class: 'grow' }, h('h2', { class: 'mt' }, target ? `Обновление до ${target}` : 'Перезапуск'), h('div', { class: 'hint' }, 'Не закрывайте страницу'))),
    h('div', { class: 'upd-head' }, stageEl, pct), h('div', { class: 'upd-bar' }, fill), steps, note));
  document.body.append(scr);
  const set = (stage, done, total) => {
    const idx = UPDATE_STAGES.findIndex(x => x[0] === stage);
    [...steps.children].forEach((li, i) => { li.className = i < idx ? 'done' : i === idx ? 'now' : ''; });
    let p = null, label = (UPDATE_STAGES[idx] || [0, 'Подготовка'])[1];
    if (stage === 'download' && total) { p = Math.min(100, Math.round(done / total * 100)); label = `Скачивание: ${bytes(done || 0)} из ${bytes(total)}`; }
    stageEl.textContent = label;
    const overall = p != null ? 5 + p * 0.8 : { prepare: 3, verify: 87, probe: 91, install: 95, restart: 100 }[stage] || 0;
    fill.style.width = overall + '%';
    pct.textContent = Math.round(overall) + '%';
    fill.classList.toggle('pulse', stage === 'restart');
  };
  set(target ? 'prepare' : 'restart');
  const waitUp = () => {
    set('restart');
    const from = state.status && state.status.version;
    const started = Date.now();
    let wentDown = false;
    const tick = async () => {
      try {
        const st = await fetch('/api/status', { cache: 'no-store' }).then(r => r.ok ? r.json() : Promise.reject());
        if (st.version !== from || (wentDown && Date.now() - started > 4000)) {
          try { sessionStorage.setItem('np-restarted', st.version); } catch {}
          location.reload();
          return;
        }
      } catch { wentDown = true; }
      if (Date.now() - started > 120000) { note.textContent = 'Программа долго не отвечает. Проверьте службу на устройстве или обновите страницу позже.'; return; }
      setTimeout(tick, 1500);
    };
    setTimeout(tick, 1500);
  };
  const close = () => scr.remove();
  return { set, waitUp, close };
}

export function runInstall(target) {
  const scr = updateScreen(target);
  let finished = false;
  const poll = async () => {
    if (finished) return;
    try { const u = await get('/update'); if (!finished && u.progress) scr.set(u.progress.stage, u.progress.done, u.progress.total); } catch {}
    if (!finished) setTimeout(poll, 400);
  };
  poll();
  api('POST', '/update/install').then(() => { finished = true; scr.waitUp(); },
    e => { finished = true; scr.close(); toast(e.message, 'err', { title: 'Обновление не установлено' }); });
}

export function waitRestart() { updateScreen(null).waitUp(); }

// ---------- вход ----------

// needCode — первая настройка не с этого компьютера: сервер требует код из журнала.
function showAuth(mode, needCode) {
  if (document.querySelector('.auth')) return;
  document.getElementById('shell').hidden = true;
  document.querySelectorAll('.modal-bg').forEach(m => m.remove());
  const setup = mode === 'setup';
  const pw = h('input', { class: 'inp', type: 'password', autocomplete: setup ? 'new-password' : 'current-password', style: 'height:48px' });
  const pw2 = h('input', { class: 'inp', type: 'password', autocomplete: 'new-password', style: 'height:48px' });
  const withCode = setup && needCode;
  const code = h('input', { class: 'inp mono', autocomplete: 'off', autocapitalize: 'characters', spellcheck: 'false', placeholder: 'XXXX-XXXX-XXXX', style: 'height:48px' });
  const err = h('div', { class: 'err' });
  const btn = h('button', { class: 'btn p lg', type: 'submit', style: 'width:100%' }, setup ? 'Сохранить и войти' : 'Войти');
  const scr = h('div', { class: 'auth' },
    h('div', { class: 'promo' },
      h('div', { style: 'display:flex;align-items:center;gap:12px' }, h('span', { class: 'logo', style: 'width:40px;height:40px;border-radius:11px' }, icon('logo')), h('span', { style: 'font-size:20px;font-weight:700' }, 'NetPilot')),
      h('div', { class: 't' }, 'Вся сеть — через правила, которые вы задали'),
      h('div', { class: 'ver', style: 'color:var(--sidem);font-size:13px' }, 'Шлюз, прокси и маршрутизация для дома и офиса')),
    h('div', { class: 'pane' }, h('form', { class: 'card', 'aria-label': 'Вход', onsubmit: async e => {
      e.preventDefault();
      err.textContent = '';
      if (setup && pw.value !== pw2.value) { err.textContent = 'Пароли не совпадают'; return; }
      if (withCode && !code.value.trim()) { err.textContent = 'Введите код первой настройки'; code.focus(); return; }
      btn.disabled = true;
      try {
        await api('POST', setup ? '/auth/setup' : '/auth/login', withCode ? { password: pw.value, code: code.value.trim() } : { password: pw.value });
        scr.remove();
        document.getElementById('shell').hidden = false;
        startApp();
      } catch (x) { err.textContent = x.message; pw.select(); }
      finally { btn.disabled = false; }
    } },
      h('div', {}, h('h1', { class: 'h1', style: 'font-size:24px' }, setup ? 'Задайте пароль' : 'Вход'),
        h('div', { class: 'sub' }, setup ? 'Первый запуск: придумайте пароль администратора, не короче 8 символов.' : 'Пароль администратора')),
      h('label', { class: 'fld' }, h('span', { class: 'lbl' }, 'Пароль'), pw),
      setup && h('label', { class: 'fld' }, h('span', { class: 'lbl' }, 'Ещё раз'), pw2),
      withCode && h('label', { class: 'fld' }, h('span', { class: 'lbl' }, 'Код первой настройки'), code,
        h('span', { class: 'hint' }, 'Код напечатан в журнале программы')),
      err, btn,
      !setup && h('div', { class: 'hint', style: 'text-align:center' }, 'Забыли пароль — удалите файл ', h('span', { class: 'mono', style: 'color:var(--ink)' }, 'auth.json'), ' в папке данных NetPilot и перезапустите его: появится код первой настройки'))));
  document.body.append(scr);
  pw.focus();
}
onAuthLost.fn = () => showAuth('login');

export async function logout() {
  if (isDirty()) { askLeave(logout); return; }
  await api('POST', '/auth/logout').catch(() => {});
  location.hash = '';
  location.reload();
}

// ---------- запуск ----------

async function refreshCounts() {
  const [n, nodes, rules] = await Promise.all(['/network', '/nodes', '/rules'].map(p => get(p).catch(() => null)));
  state.counts = { devices: n ? n.devices.filter(isClient).length : null, nodes: nodes && nodes.length, rules: rules && rules.length };
  drawSide(pageName());
}
state.refreshCounts = refreshCounts;

let started = false;
function startApp() {
  document.getElementById('shell').hidden = false;
  if (started) { route(); return; }
  started = true;
  try { if (localStorage.getItem('np-side-collapsed') === '1') document.body.classList.add('side-collapsed'); } catch {}
  try {
    const v = sessionStorage.getItem('np-restarted');
    if (v) { sessionStorage.removeItem('np-restarted'); setTimeout(() => toast(`Работает версия ${v}`, 'ok', { title: 'Перезапуск завершён' }), 300); }
  } catch {}
  router.onRoute.push(name => { drawSide(name); drawTop(name); drawTabs(name); document.title = (TITLES[name] || 'NetPilot') + ' — NetPilot'; });
  // Ушли из настроек, не сохранив оформление, — возвращаем сохранённое.
  router.onRoute.push(name => { if (name !== 'settings' && state.savedDesign) applyDesign(state.savedDesign); });
  state.hooks.push(() => drawTop(pageName()));
  window.addEventListener('hashchange', onHashChange);
  refreshStatus();
  setInterval(refreshStatus, 5000);
  loadUpdate();
  setInterval(loadUpdate, 30 * 60 * 1000);
  refreshCounts();
  refreshActive();
  route();
}

get('/auth/state').then(s => {
  if (s.setup) showAuth('setup', s.setupCode);
  else if (!s.authenticated) showAuth('login');
  else startApp();
}).catch(() => showAuth('login'));

export { ago };
