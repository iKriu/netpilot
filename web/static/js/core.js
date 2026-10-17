// NetPilot — ядро интерфейса: разметка, запросы, уведомления, окна,
// несохранённые изменения и переходы между страницами.
// Без сборщиков и зависимостей: работает на роутере офлайн.

// ---------- иконки (линейные, 24×24) ----------

const ICONS = {
  logo: 'M5 19V5l14 14V5',
  dashboard: 'M3 3h7v9H3z M14 3h7v5h-7z M14 12h7v9h-7z M3 16h7v5H3z',
  servers: 'M3 4h18v6H3z M3 14h18v6H3z M7 7h.01 M7 17h.01',
  rules: 'M6 3v12 M18 9a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M6 21a3 3 0 1 0 0-6 3 3 0 0 0 0 6z M18 9a9 9 0 0 1-9 9',
  devices: 'M2 5h13v9H2z M6 18h5 M8.5 14v4 M18 8h4v11h-4z',
  connections: 'M4 7h13 M13 3l4 4-4 4 M20 17H7 M11 13l-4 4 4 4',
  globe: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z M2 12h20 M12 2a15 15 0 0 1 0 20 M12 2a15 15 0 0 0 0 20',
  settings: 'M4 21v-7 M4 10V3 M12 21v-9 M12 8V3 M20 21v-5 M20 12V3 M1 14h6 M9 8h6 M17 16h6',
  groups: 'M12 3l9 5-9 5-9-5z M3 13l9 5 9-5',
  logout: 'M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4 M16 17l5-5-5-5 M21 12H9',
  collapse: 'M11 17l-5-5 5-5 M18 17l-5-5 5-5',
  expand: 'M13 17l5-5-5-5 M6 17l5-5-5-5',
  search: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14z M21 21l-4.3-4.3',
  down: 'M6 9l6 6 6-6', up: 'M18 15l-6-6-6 6', right: 'M9 6l6 6-6 6', left: 'M15 18l-6-6 6-6',
  check: 'M20 6L9 17l-5-5', plus: 'M12 5v14 M5 12h14', close: 'M18 6L6 18 M6 6l12 12',
  test: 'M22 12h-4l-3 9L9 3l-3 9H2', refresh: 'M21 12a9 9 0 1 1-2.6-6.4L21 8 M21 3v5h-5',
  arrowDown: 'M12 4v12 M7 11l5 5 5-5', arrowUp: 'M12 20V8 M7 13l5-5 5 5', arrow: 'M5 12h14 M13 6l6 6-6 6',
  edit: 'M12 20h9 M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z',
  trash: 'M3 6h18 M8 6V4h8v2 M19 6l-1 14H6L5 6 M10 11v6 M14 11v6',
  copy: 'M9 9h11v11H9z M5 15H4V4h11v1',
  lock: 'M5 11h14v10H5z M8 11V7a4 4 0 0 1 8 0v4', unlock: 'M5 11h14v10H5z M8 11V7a4 4 0 0 1 7.7-1.5',
  grip: 'M9 5h.01 M15 5h.01 M9 12h.01 M15 12h.01 M9 19h.01 M15 19h.01',
  template: 'M14 3H6v18h12V7z M14 3v4h4 M12 11v6 M9 14h6',
  import: 'M12 3v12 M7 10l5 5 5-5 M5 21h14',
  play: 'M7 4l13 8-13 8z', stop: 'M6 6h12v12H6z', pause: 'M7 5h3v14H7z M14 5h3v14h-3z',
  alert: 'M12 9v4 M12 17h.01 M10.3 3.9L1.8 18a2 2 0 0 0 1.7 3h17a2 2 0 0 0 1.7-3L13.7 3.9a2 2 0 0 0-3.4 0z',
  info: 'M12 22a10 10 0 1 0 0-20 10 10 0 0 0 0 20z M12 16v-4 M12 8h.01',
  shield: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z',
  bolt: 'M13 2L4 14h7l-1 8 9-12h-7z',
  more: 'M5 12h.01 M12 12h.01 M19 12h.01',
  router: 'M3 14h18v6H3z M7 17h.01 M11 17h.01 M12 14V9 M8.5 7.5a5 5 0 0 1 7 0 M6 5a8.5 8.5 0 0 1 12 0',
  download: 'M12 4v12 M7 11l5 5 5-5 M5 21h14', upload: 'M12 20V8 M7 13l5-5 5 5 M5 3h14',
  // типы устройств
  computer: 'M3 4h18v12H3z M8 20h8 M12 16v4',
  laptop: 'M4 5h16v10H4z M2 19h20',
  phone: 'M7 2h10v20H7z M11 18h2',
  tablet: 'M5 3h14v18H5z M11 18h2',
  tv: 'M3 7h18v12H3z M8 3l4 4 4-4',
  console: 'M6 11h4 M8 9v4 M15 12h.01 M18 10h.01 M17.3 5H6.7a4 4 0 0 0-4 3.6L2 16a3 3 0 0 0 5.4 2l1.6-2h6l1.6 2a3 3 0 0 0 5.4-2l-.7-7.4A4 4 0 0 0 17.3 5z',
  printer: 'M6 9V3h12v6 M6 18H4v-7h16v7h-2 M6 14h12v7H6z',
  iot: 'M7 7h10v10H7z M10 3v4 M14 3v4 M10 17v4 M14 17v4 M3 10h4 M3 14h4 M17 10h4 M17 14h4',
  network: 'M3 14h18v6H3z M7 17h.01 M11 17h.01 M12 14V9 M8.5 7.5a5 5 0 0 1 7 0',
  other: 'M5 4h14v16H5z M9 8h6 M9 12h6 M9 16h3',
};

export function icon(name, cls = '') {
  const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
  svg.setAttribute('viewBox', '0 0 24 24');
  svg.setAttribute('class', 'i ' + cls);
  svg.setAttribute('aria-hidden', 'true');
  for (const d of (ICONS[name] || ICONS.other).split(' M')) {
    const p = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    p.setAttribute('d', d.startsWith('M') ? d : 'M' + d);
    svg.append(p);
  }
  return svg;
}

// ---------- справочники ----------

export const DEVICE_TYPES = [
  ['computer', 'Компьютер'], ['laptop', 'Ноутбук'], ['phone', 'Телефон'], ['tablet', 'Планшет'],
  ['tv', 'Телевизор'], ['console', 'Игровая приставка'], ['printer', 'Принтер'],
  ['iot', 'Умное устройство'], ['network', 'Сетевое оборудование'], ['other', 'Другое'],
];
export const typeName = t => (DEVICE_TYPES.find(x => x[0] === t) || DEVICE_TYPES[9])[1];
export const ROUTES = [['', 'По правилам'], ['proxy', 'Через прокси'], ['direct', 'Напрямую'], ['block', 'Без интернета']];
export const routeName = r => (ROUTES.find(x => x[0] === (r || '')) || ROUTES[0])[1];

// ---------- разметка ----------

export function h(tag, props, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(props || {})) {
    if (v == null || v === false) continue;
    if (k === 'class') el.className = v;
    else if (k === 'style') el.style.cssText = v;
    else if (k.startsWith('on')) el.addEventListener(k.slice(2), v);
    else if (k === 'value') el.value = v;
    else if (k === 'checked') el.checked = !!v;
    else el.setAttribute(k, v === true ? '' : v);
  }
  for (const c of children.flat(Infinity)) {
    if (c == null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}

export const arr = a => a || [];
// fill заменяет содержимое элемента, пропуская пустые значения (null, false) и раскрывая списки.
export function fill(el, ...children) {
  el.replaceChildren(...children.flat(Infinity).filter(c => c != null && c !== false));
  return el;
}
export const lines = s => s.split(/[\n,]/).map(x => x.trim()).filter(Boolean);
export const ipKey = ip => (ip || '999.999.999.999').split('.').reduce((a, o) => a * 256 + (+o || 0), 0);
export const toggleIn = (list, v, on) => on ? [...new Set([...list, v])] : list.filter(x => x !== v);
// Устройство показывается среди «NetPilot»: пускает трафик через шлюз или настроено вручную.
export const isClient = d => d.client || d.configured || !!d.route;
// plural(5, 'сервер', 'сервера', 'серверов') → «5 серверов».
export function plural(n, one, few, many) {
  const m10 = n % 10, m100 = n % 100;
  const w = m10 === 1 && m100 !== 11 ? one : m10 >= 2 && m10 <= 4 && (m100 < 12 || m100 > 14) ? few : many;
  return `${n} ${w}`;
}
export const regexError = src => { if (!src) return ''; try { new RegExp(src); return ''; } catch (e) { return 'Ошибка в выражении: ' + e.message.replace(/^Invalid regular expression: /, ''); } };

// ---------- запросы ----------

export const onAuthLost = { fn: null };

export async function api(method, path, body) {
  const res = await fetch('/api' + path, {
    method,
    headers: body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => null);
  if (res.status === 401 && !path.startsWith('/auth/')) {
    if (onAuthLost.fn) onAuthLost.fn();
    throw new Error('Требуется вход');
  }
  if (!res.ok) {
    // Код и тело ответа — для случаев, где ошибку разбирают (409 со списком ссылок).
    const err = new Error((data && data.error) || res.statusText);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  // Сохранённое человеком (кнопкой, окном, удалением) применяется к ядру.
  if (method !== 'GET' && !/^\/(core|auth|nodes\/test|network\/scan|lists|router|backup|restore|update|devices\/cleanup)/.test(path)) scheduleApply();
  return data;
}
export const get = p => api('GET', p);

export async function act(fn, okMsg) {
  try {
    const r = await fn();
    if (okMsg) toast(okMsg, 'ok');
    return r;
  } catch (e) {
    toast(e.message, 'err');
    throw e;
  }
}

// ---------- применение к ядру ----------
// Только после явного сохранения. Серия запросов — одно применение.

let applyTimer = null;
let applying = false;

export function scheduleApply() {
  clearTimeout(applyTimer);
  applyTimer = setTimeout(applyNow, 600);
}

async function applyNow() {
  if (applying) { scheduleApply(); return; }
  let s;
  try { s = await get('/status'); } catch { return; }
  if (!s.running || !s.dirty) return;
  applying = true;
  try {
    await api('POST', '/core/apply');
  } catch (e) {
    toast(e.message, 'err', { title: 'Сохранено, но не применено' });
  } finally {
    applying = false;
    refreshStatus();
  }
}

// ---------- уведомления ----------
// Анимации через Web Animations API: они работают и при отключённых эффектах анимации в системе.

const TOAST_ICONS = { ok: 'check', err: 'alert', warn: 'alert', info: 'info' };
const TOAST_TITLES = { ok: 'Готово', err: 'Ошибка', warn: 'Внимание', info: 'Сообщение' };

export function toast(msg, kind = 'info', { title } = {}) {
  kind = kind || 'info';
  const box = document.getElementById('toasts');
  const same = [...box.children].find(t => !t.dataset.out && t.dataset.key === kind + msg);
  if (same) same.remove();
  const ms = kind === 'err' ? 8000 : 4500;
  const bar = h('div', { class: 'toast-bar' });
  const t = h('div', { class: 'toast ' + kind, role: kind === 'err' ? 'alert' : 'status' },
    h('span', { class: 'toast-ico' }, icon(TOAST_ICONS[kind] || 'info')),
    h('div', {}, h('div', { class: 'toast-title' }, title || TOAST_TITLES[kind]), h('div', { class: 'toast-msg' }, msg)),
    h('button', { class: 'toast-x', 'aria-label': 'Закрыть', onclick: () => dismiss() }, icon('close')),
    bar);
  t.dataset.key = kind + msg;
  box.prepend(t);
  const hgt = t.offsetHeight;
  t.animate([{ height: '0px', marginBottom: '-10px', opacity: 0 }, { height: hgt + 'px', marginBottom: '0px', opacity: 1 }], { duration: 220, easing: 'cubic-bezier(.2,.8,.2,1)' });
  t.animate([{ transform: isM3() ? 'translateY(24px) scale(.97)' : 'translateX(28px) scale(.97)' }, { transform: 'none' }], { duration: 420, easing: 'cubic-bezier(.16,1.1,.3,1)' });
  const timer = bar.animate([{ transform: 'scaleX(1)' }, { transform: 'scaleX(0)' }], { duration: ms, easing: 'linear', fill: 'forwards' });
  timer.onfinish = () => dismiss();
  t.addEventListener('mouseenter', () => timer.pause());
  t.addEventListener('mouseleave', () => { if (!t.dataset.out) timer.play(); });
  function dismiss() {
    if (t.dataset.out) return;
    t.dataset.out = '1';
    timer.cancel();
    t.animate([{ transform: 'none', opacity: 1 }, { transform: isM3() ? 'translateY(16px) scale(.96)' : 'translateX(40px) scale(.96)', opacity: 0 }], { duration: 240, easing: 'cubic-bezier(.4,0,1,1)', fill: 'forwards' })
      .onfinish = () => {
        t.animate([{ height: t.offsetHeight + 'px', marginBottom: '0px' }, { height: '0px', marginBottom: '-10px' }], { duration: 200, easing: 'cubic-bezier(.4,0,.2,1)', fill: 'forwards' })
          .onfinish = () => t.remove();
      };
  }
  [...box.children].filter(x => !x.dataset.out).slice(4).forEach(x => x.remove());
}

// ---------- окна ----------

// modal — окно по центру (на телефоне — шторка снизу). size: '' | 'mid' | 'wide'.
// actions: [{ label, primary, danger, onClick }] — onClick может вернуть false, чтобы окно осталось.
export function modal({ title, body, size = '', actions = [], cancelLabel, footer, drawer = false, head }) {
  const close = () => {
    document.removeEventListener('keydown', onKey);
    if (bg.classList.contains('closing')) return;
    bg.classList.add('closing');
    setTimeout(() => bg.remove(), 220);
  };
  const onKey = e => { if (e.key === 'Escape') close(); };
  const buttons = actions.map(a => h('button', {
    class: 'btn' + (a.primary ? ' p' : '') + (a.danger ? ' d' : ''),
    onclick: async e => {
      const btn = e.currentTarget;
      btn.disabled = true;
      try { if ((await a.onClick()) !== false) close(); }
      catch (err) { toast(err.message, 'err'); }
      finally { btn.disabled = false; }
    },
  }, a.icon && icon(a.icon, 's'), a.label));
  const box = h('div', { class: drawer ? 'drawer' : 'modal ' + size, role: 'dialog', 'aria-label': title },
    h('div', { class: 'mh' }, head || h('h2', { class: 'mt' }, title),
      h('button', { class: 'ib', 'aria-label': 'Закрыть', onclick: close }, icon('close'))),
    h('div', { class: 'mb' }, body),
    (actions.length || footer || cancelLabel !== null) && h('div', { class: 'mf' }, footer,
      cancelLabel !== null && h('button', { class: 'btn', onclick: close }, cancelLabel || (actions.length ? 'Отмена' : 'Закрыть')),
      buttons));
  const bg = h('div', { class: 'modal-bg' + (drawer ? ' drawer-bg' : ''), onmousedown: e => { if (e.target === bg) close(); } }, box);
  document.addEventListener('keydown', onKey);
  document.getElementById('modal-root').append(bg);
  return close;
}

export function confirmModal(text, onYes, label = 'Удалить') {
  return modal({ title: 'Подтверждение', body: h('p', { style: 'margin:0' }, text), actions: [{ label, danger: true, onClick: onYes }] });
}

// ---------- элементы форм ----------

// field — подпись, элемент и подсказка. В <label> оборачивается только одиночное
// поле ввода: внутри <label> клик по подписи или пустому месту нажимает первую
// кнопку блока — так незаметно сбрасывался выбор в переключателях и списках.
export function field(label, input, hint) {
  const single = input instanceof HTMLInputElement || input instanceof HTMLSelectElement || input instanceof HTMLTextAreaElement;
  return h(single ? 'label' : 'div', { class: 'fld' }, label && h('span', { class: 'lbl' }, label), input, hint && h('span', { class: 'hint' }, hint));
}

export function sw(checked, onchange, label) {
  const b = h('button', { class: 'sw' + (checked ? ' on' : ''), type: 'button', role: 'switch', 'aria-checked': String(!!checked), 'aria-label': label || 'Переключатель' });
  b.onclick = () => {
    checked = !checked;
    b.classList.toggle('on', checked);
    b.setAttribute('aria-checked', String(checked));
    onchange(checked);
  };
  return label ? h('span', { class: 'swl' }, b, h('span', {}, label)) : b;
}

export function seg(options, value, onchange, cls = '') {
  const wrap = h('div', { class: 'seg ' + cls, role: 'group' });
  const draw = () => wrap.replaceChildren(...options.map(([v, label, n]) => h('button', {
    type: 'button', class: v === value ? 'on' : '', 'aria-pressed': String(v === value),
    onclick: () => { if (value === v) return; value = v; draw(); onchange(v); },
  }, label, n != null && h('span', { class: 'n' }, n))));
  draw();
  return wrap;
}

export function select(options, value, onchange, props = {}) {
  return h('select', { class: 'sel', ...props, onchange: e => onchange(e.target.value) },
    options.map(o => o.label !== undefined && o.options
      ? h('optgroup', { label: o.label }, o.options.map(([v, l]) => h('option', { value: v, selected: v === value }, l)))
      : h('option', { value: o[0], selected: o[0] === value }, o[1])));
}

export function opt(label, on, onchange) {
  const b = h('button', { type: 'button', class: 'opt' + (on ? ' on' : ''), 'aria-pressed': String(!!on) }, label);
  b.onclick = () => { on = !on; b.classList.toggle('on', on); b.setAttribute('aria-pressed', String(on)); onchange(on); };
  return b;
}

// ---------- блоки страницы ----------

export function card(title, right, ...body) {
  return h('section', { class: 'card' },
    title && h('div', { class: 'ch' }, typeof title === 'string' ? h('h2', { class: 'ct' }, title) : title, right),
    body);
}

export function pageHead(title, sub, ...actions) {
  return h('div', { class: 'ph' },
    h('div', { class: 'grow' }, h('h1', { class: 'h1' }, title), sub && h('div', { class: 'sub' }, sub)),
    actions.length > 0 && h('div', { class: 'actions' }, actions));
}

export function empty(ic, title, text, action) {
  return h('div', { class: 'empty' }, h('span', { class: 'ico' }, icon(ic, 'l')), h('div', { class: 't' }, title), text && h('div', {}, text), action && h('div', { style: 'margin-top:12px' }, action));
}

export function notice(kind, ic, text, action) {
  return h('div', { class: 'notice ' + kind }, icon(ic), h('div', { class: 'txt' }, text), action);
}

// ---------- форматирование ----------

export function ago(unix) {
  if (!unix) return 'никогда';
  const s = Math.floor(Date.now() / 1000 - unix);
  if (s < 60) return 'только что';
  if (s < 3600) return Math.floor(s / 60) + ' мин назад';
  if (s < 86400) return Math.floor(s / 3600) + ' ч назад';
  return Math.floor(s / 86400) + ' дн назад';
}
export function uptime(unix) {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  const d = Math.floor(s / 86400), hh = Math.floor(s % 86400 / 3600), m = Math.floor(s % 3600 / 60);
  return (d ? d + ' д ' : '') + (hh ? hh + ' ч ' : '') + m + ' мин';
}
export function duration(unix) {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unix));
  if (s < 60) return s + ' с';
  if (s < 3600) return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
  return Math.floor(s / 3600) + ' ч ' + Math.floor(s % 3600 / 60) + ' мин';
}
export function bytes(n) {
  if (!n) return '0 Б';
  const u = ['Б', 'КБ', 'МБ', 'ГБ', 'ТБ'];
  const i = Math.min(u.length - 1, Math.floor(Math.log(n) / Math.log(1024)));
  return (n / 1024 ** i).toLocaleString('ru-RU', { maximumFractionDigits: i ? 1 : 0 }) + ' ' + u[i];
}
export const rate = n => bytes(n) + '/с';
// mbps — скорость числом в МБ/с, без единиц: «12,4».
export const mbps = n => (n / 1048576).toLocaleString('ru-RU', { maximumFractionDigits: n >= 10485760 ? 0 : 1 });

export function latencyEl(l) {
  if (!l) return h('span', { class: 'hint' }, '—');
  if (l === 'wait') return h('span', { class: 'lat wait' }, h('span', { class: 'ring' }), 'проверка');
  if (l.ms <= 0) return h('span', { class: 'chip bad', title: l.error || '' }, 'не отвечает');
  const cls = l.ms < 200 ? 'ok' : l.ms < 500 ? 'warn' : 'bad';
  const full = l.ms < 120 ? 3 : l.ms < 300 ? 2 : 1;
  const how = { tcp: ['TCP', 'Время TCP-подключения к серверу'], icmp: ['ICMP', 'Ping адреса сервера'] }[l.method];
  return h('span', { class: 'lat ' + cls, title: how ? how[1] : 'Настоящий запрос через сервер' },
    h('span', { class: 'bars ' + cls }, [0, 1, 2].map(i => h('span', { class: i < full ? 'f' : '' }))),
    l.ms + ' мс', how && h('span', { class: 'hint' }, ' ' + how[0]));
}

// ---------- серверы и группы ----------

export function groupMembers(g, nodes) {
  const rx = src => { if (!src) return null; try { return new RegExp(src, 'i'); } catch { return null; } };
  const re = rx(g.filter), ex = rx(g.exclude);
  const ids = arr(g.nodeIds);
  return nodes.filter(n => ((!ids.length && !g.filter) || ids.includes(n.id) || (re && re.test(n.name))) && !(ex && ex.test(n.name)));
}

// Варианты «через какой сервер» для правил и устройств: «» — активный сервер.
export function targetOptions(nodes, groups) {
  const out = [['', 'Активный сервер']];
  if (groups.length) out.push({ label: 'Группы', options: groups.map(g => [g.id, g.name]) });
  if (nodes.length) out.push({ label: 'Серверы', options: nodes.map(n => [n.id, n.name]) });
  return out;
}
export function targetName(id, nodes, groups) {
  if (!id) return '';
  return (groups.find(g => g.id === id) || nodes.find(n => n.id === id) || { name: 'удалён' }).name;
}
export function activeName(settings, nodes, groups) {
  const id = settings.defaultTarget;
  const g = groups.find(x => x.id === id), n = nodes.find(x => x.id === id);
  if (g) return g.name;
  if (n) return n.name;
  return nodes.length > 1 ? 'Авто — все серверы' : nodes.length ? nodes[0].name : 'серверов нет';
}

// ---------- оформление ----------
// design: classic | m3. Material 3 Expressive — отдельный интерфейс на /m3/
// (Compose Multiplatform); палитра и тема (scheme, mode) применяются там.

export const DESIGN_SCHEMES = [['indigo', 'Индиго', '#4355B9'], ['green', 'Изумруд', '#3B6939'], ['coral', 'Коралл', '#8F4C38']];

export function designOf(settings) {
  const s = settings || {};
  return { design: s.design === 'm3' ? 'm3' : 'classic', scheme: s.designScheme || 'indigo', mode: s.designMode || 'system' };
}

/** Запоминает сохранённое оформление; выбран M3 — переходит в его интерфейс. */
export function applyDesign(d) {
  try { localStorage.setItem('np-design', JSON.stringify(d)); } catch {}
  if (d && d.design === 'm3') location.replace('/m3/' + location.hash);
}
export const isM3 = () => false;

// ---------- общее состояние ----------

export const state = { status: null, update: null, latency: {}, counts: {}, profiles: [], hooks: [] };

export async function refreshStatus() {
  try { state.status = await get('/status'); } catch { return; }
  state.hooks.forEach(f => f(state.status));
}

// ---------- несохранённые изменения ----------
// Правки сначала попадают в черновик; сохраняет их кнопка «Сохранить» на панели внизу.
//   pending   — отложенные запросы: ключ → функция (последняя правка по ключу побеждает);
//   guard.form — форма страницы целиком: { isDirty, save }.

export const pending = new Map();
export const guard = { form: null };
// Черновики форм страниц, переживающие перерисовку той же страницы.
const drafts = new Map();
export const takeDraft = name => { const d = drafts.get(name); drafts.delete(name); return d; };

export function stage(key, fn) {
  pending.set(key, fn);
  refreshSaveBar();
}
export function isDirty() {
  return pending.size > 0 || !!(guard.form && guard.form.isDirty());
}
export async function saveAll() {
  let msg = null;
  for (const [key, fn] of [...pending]) {
    await fn();
    pending.delete(key);
  }
  if (guard.form && guard.form.isDirty()) msg = await guard.form.save();
  return msg || 'Изменения сохранены';
}

let saveBar = null;
export function refreshSaveBar() {
  const dirty = isDirty();
  document.body.classList.toggle('has-savebar', dirty);
  if (dirty && saveBar && !saveBar.isConnected) saveBar = null;
  if (dirty && !saveBar) {
    const saveBtn = h('button', { class: 'p2', onclick: async () => {
      saveBtn.disabled = true;
      try {
        toast(await saveAll(), 'ok');
        refreshSaveBar();
        route();
      } catch (e) {
        toast(e.message, 'err', { title: 'Не сохранено' });
      } finally { saveBtn.disabled = false; }
    } }, icon('check', 's'), 'Сохранить');
    saveBar = h('div', { class: 'savebar', role: 'region', 'aria-label': 'Несохранённые изменения' },
      h('span', { class: 'dot warn' }), h('span', { class: 'txt' }, 'Есть несохранённые изменения'),
      h('button', { class: 'g2', onclick: discardAll }, 'Сбросить'), saveBtn);
    document.body.append(saveBar);
    saveBar.animate([{ opacity: 0, translate: '0 24px' }, { opacity: 1, translate: '0 0' }], { duration: 320, easing: 'cubic-bezier(.16,1.1,.3,1)' });
  } else if (!dirty) {
    saveBar = null;
    // Убираем все панели: их могло остаться несколько при быстрых действиях.
    document.querySelectorAll('.savebar:not(.leaving)').forEach(bar => {
      bar.classList.add('leaving');
      bar.animate([{ opacity: 1, translate: '0 0' }, { opacity: 0, translate: '0 24px' }], { duration: 200, easing: 'ease-in', fill: 'forwards' });
      setTimeout(() => bar.remove(), 220);
    });
  }
}
function discardAll() {
  pending.clear();
  guard.form = null;
  refreshSaveBar();
  route();
}
['input', 'change', 'click'].forEach(ev => document.addEventListener(ev, () => setTimeout(refreshSaveBar), true));

// ---------- переходы между страницами ----------

export const pages = {};            // имя → async () => элемент страницы
export const router = { timer: null, onRoute: [] };
let currentHash = location.hash;
let skipHash = false;

export const pageName = () => {
  const n = (location.hash.replace(/^#\/?/, '') || 'overview').split('/')[0];
  return pages[n] ? n : 'overview';
};

export function go(name) { location.hash = '#/' + (name === 'overview' ? '' : name); }

export function onHashChange() {
  if (skipHash) { skipHash = false; return; }
  if (isDirty()) {
    const target = location.hash;
    skipHash = true;
    location.hash = currentHash;
    askLeave(() => { location.hash = target; });
    return;
  }
  route();
}

export function askLeave(proceed) {
  modal({
    title: 'Несохранённые изменения',
    body: h('p', { style: 'margin:0' }, 'На этой странице есть изменения, которые ещё не сохранены. Сохранить их перед уходом?'),
    cancelLabel: 'Остаться',
    actions: [
      { label: 'Сбросить', danger: true, onClick: () => { pending.clear(); guard.form = null; refreshSaveBar(); proceed(); } },
      { label: 'Сохранить', primary: true, onClick: async () => { toast(await saveAll(), 'ok'); guard.form = null; refreshSaveBar(); proceed(); } },
    ],
  });
}

window.addEventListener('beforeunload', e => { if (isDirty()) { e.preventDefault(); e.returnValue = ''; } });

export async function route() {
  // Та же страница перерисовывается после явного сохранения (окно, удаление):
  // отложенные правки сохраняются вместе с ним, а не теряются молча.
  if (pending.size && location.hash === currentHash) {
    try { await saveAll(); } catch (e) { toast(e.message, 'err', { title: 'Не сохранено' }); }
  }
  clearInterval(router.timer);
  pending.clear();
  const changed = location.hash !== currentHash;
  // Перерисовка той же страницы (обновили списки, поставили ядро) не должна
  // молча выбрасывать несохранённую форму: черновик достаётся странице снова.
  drafts.clear();
  if (!changed && guard.form && guard.form.draft && guard.form.isDirty()) drafts.set(pageName(), guard.form.draft());
  guard.form = null;
  refreshSaveBar();
  currentHash = location.hash;
  // Окна страницы, с которой ушли, закрываются вместе с ней.
  if (changed) document.querySelectorAll('#modal-root .modal-bg').forEach(m => m.remove());
  const name = pageName();
  router.onRoute.forEach(f => f(name));
  const root = document.getElementById('page');
  try {
    root.replaceChildren(await pages[name]());
  } catch (e) {
    root.replaceChildren(h('div', { class: 'stack' }, notice('bad', 'alert', 'Не удалось загрузить страницу: ' + e.message)));
  }
  if (changed) window.scrollTo(0, 0);
  refreshSaveBar();
  router.onRoute.forEach(f => f(name));
}

// ---------- плавная прокрутка колесом ----------

(() => {
  let target = window.scrollY, current = window.scrollY, raf = 0, last = 0;
  const TAU = 140;
  const maxY = () => document.documentElement.scrollHeight - window.innerHeight;
  const innerScrolls = (el, dy) => {
    for (; el && el !== document.body && el !== document.documentElement; el = el.parentElement) {
      const oy = getComputedStyle(el).overflowY;
      if ((oy === 'auto' || oy === 'scroll') && el.scrollHeight > el.clientHeight + 1) {
        if (dy > 0 ? el.scrollTop + el.clientHeight < el.scrollHeight - 1 : el.scrollTop > 0) return true;
      }
    }
    return false;
  };
  const step = now => {
    const dt = Math.min(64, now - (last || now - 16));
    last = now;
    current += (target - current) * (1 - Math.exp(-dt / TAU));
    if (Math.abs(target - current) < 0.5) current = target;
    window.scrollTo(0, current);
    if (current === target) { raf = 0; last = 0; } else raf = requestAnimationFrame(step);
  };
  window.addEventListener('wheel', e => {
    if (e.ctrlKey || e.shiftKey || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;
    if (e.target.closest && e.target.closest('.modal-bg, select')) return;
    const dy = e.deltaY * (e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? window.innerHeight : 1);
    if (innerScrolls(e.target, dy)) return;
    e.preventDefault();
    if (!raf) current = target = window.scrollY;
    target = Math.max(0, Math.min(maxY(), target + dy));
    if (!raf) raf = requestAnimationFrame(step);
  }, { passive: false });
  window.addEventListener('scroll', () => { if (!raf) current = target = window.scrollY; }, { passive: true });
})();
