// Правила: «Куда пойдёт сайт», общие настройки трафика, список правил
// и редактор «Кто → Что → Куда».

import {
  h, fill, icon, api, get, toast, modal, confirmModal, state, stage, route, field, seg, sw, opt, select,
  card, pageHead, empty, notice, arr, ipKey, lines, ago, toggleIn, targetOptions, targetName, activeName,
} from './core.js';
import { openPicker } from './servers.js';

const QUICK_GEOSITE = [['category-ru', 'Сайты РФ'], ['category-ads-all', 'Реклама'], ['google', 'Google'], ['category-games', 'Игры'], ['apple', 'Apple'], ['microsoft', 'Microsoft']];
const QUICK_GEOIP = [['ru', 'Россия'], ['by', 'Беларусь'], ['kz', 'Казахстан'], ['us', 'США'], ['de', 'Германия'], ['nl', 'Нидерланды']];
const PROTOCOLS = [['bittorrent', 'Торренты (BitTorrent)']];

const TEMPLATES = [
  { title: 'Торренты — напрямую, мимо прокси', desc: 'qBittorrent, Transmission и другие клиенты на любых портах идут напрямую и не нагружают серверы.',
    rules: [{ name: 'Торренты напрямую', what: { protocols: ['bittorrent'] }, where: { action: 'direct' } }], first: true },
  { title: 'Заблокированное в России — через прокси (Re:filter)', desc: 'Сайты и адреса из реестра блокировок идут через прокси, остальное — напрямую.',
    rules: [{ name: 'Заблокированное в РФ', what: { lists: ['refilter-domains', 'refilter-ips'] }, where: { action: 'proxy' } }], defaultAction: 'direct' },
  { title: 'Звонки Telegram, Discord и FaceTime — через прокси', desc: 'Мессенджеры и их голосовые и видеозвонки. В Telegram отключите «Peer-to-peer» в звонках.',
    rules: [{ name: 'Звонки мессенджеров', what: { apps: ['telegram', 'discord', 'facetime'] }, where: { action: 'proxy' } },
      { name: 'FaceTime — голос и видео', what: { ips: ['17.0.0.0/8'], network: 'udp' }, where: { action: 'proxy' } }] },
  { title: 'Всё через прокси, кроме российского', desc: 'Банки, Госуслуги и маркетплейсы — напрямую, остальное — через прокси.',
    rules: [{ name: 'Российские ресурсы напрямую', what: { geosite: ['category-ru'], geoip: ['ru'] }, where: { action: 'direct' } }], defaultAction: 'proxy' },
  { title: 'Через прокси только выбранные сервисы', desc: 'YouTube, Instagram, Discord, X, ChatGPT, Claude, Gemini, LinkedIn, Twitch.',
    rules: [{ name: 'Зарубежные сервисы', what: { apps: ['youtube', 'instagram', 'discord', 'x', 'chatgpt', 'claude', 'gemini', 'linkedin', 'twitch'] }, where: { action: 'proxy' } }], defaultAction: 'direct' },
  { title: 'Блокировка рекламы', desc: 'Рекламные и трекинговые домены блокируются для всей сети.',
    rules: [{ name: 'Реклама и трекеры', what: { geosite: ['category-ads-all'] }, where: { action: 'block' } }] },
];

export function emptyRule() {
  return { name: '', enabled: true, who: { deviceIds: [], tags: [], cidrs: [] },
    what: { apps: [], lists: [], domains: [], ips: [], geosite: [], geoip: [], ports: [], network: '', protocols: [] },
    where: { action: 'proxy', target: '' } };
}
export function normRule(r) {
  const e = emptyRule();
  const out = { ...e, ...r, who: { ...e.who, ...r.who }, what: { ...e.what, ...r.what }, where: { ...e.where, ...r.where } };
  for (const k of Object.keys(e.what)) if (k !== 'network') out.what[k] = arr(out.what[k]);
  for (const k of Object.keys(e.who)) out.who[k] = arr(out.who[k]);
  return out;
}

// ---------- торренты одной кнопкой ----------

const isTorrentRule = r => arr(r.what && r.what.protocols).includes('bittorrent') && r.where && r.where.action === 'direct';
export const torrentsOn = rules => rules.some(r => r.enabled && isTorrentRule(r));

// stageTorrents ставит в черновик включение или выключение правила «Торренты напрямую».
export function stageTorrents(on, rules) {
  stage('torrents', async () => {
    const all = await get('/rules');
    const ex = all.find(isTorrentRule);
    if (ex) { if (ex.enabled !== on) await api('PUT', '/rules/' + ex.id, { ...ex, enabled: on }); return; }
    if (!on) return;
    const r = await api('POST', '/rules', normRule(TEMPLATES[0].rules[0]));
    await api('PUT', '/rules/order', { ids: [r.id, ...all.map(x => x.id)] }); // первым — раньше списков блокировок
  });
}

// ---------- куда пойдёт сайт ----------

function explainView(ex) {
  const tagFor = ex.action === 'proxy' ? h('span', { class: 'chip acc' }, 'через прокси · ' + ex.target)
    : ex.action === 'block' ? h('span', { class: 'chip bad' }, 'блокировка') : h('span', { class: 'chip ok' }, 'напрямую');
  return h('div', { style: 'display:flex;flex-direction:column;gap:8px' },
    h('div', { style: 'display:flex;align-items:center;gap:8px;flex-wrap:wrap' }, h('strong', {}, ex.host), h('span', { class: 'hint' }, 'пойдёт'), tagFor),
    h('div', { class: 'hint' }, 'Почему: ' + ex.reason + (arr(ex.ips).length ? ' · адрес ' + ex.ips.slice(0, 2).join(', ') : '')),
    arr(ex.notes).length > 0 && h('div', { class: 'hint', style: 'color:var(--warn)' }, 'Учтите: ' + ex.notes.join('; ')));
}

// quickRule создаёт правило для сайта и ставит его первым.
export async function quickRule(host, action) {
  const all = await get('/rules');
  const r = await api('POST', '/rules', normRule({ name: `${host} — ${action === 'direct' ? 'напрямую' : 'через прокси'}`, what: { domains: [host] }, where: { action } }));
  await api('PUT', '/rules/order', { ids: [r.id, ...all.map(x => x.id)] });
  toast(`Правило для ${host} создано и стоит первым`, 'ok');
}

export function explainModal(host) {
  const input = h('input', { class: 'inp', value: host || '', placeholder: 'parsec.app или адрес страницы', 'aria-label': 'Адрес сайта' });
  const out = h('div', { class: 'stack', style: 'gap:12px' });
  const check = async () => {
    if (!input.value.trim()) return;
    fill(out, h('span', { class: 'lat wait' }, h('span', { class: 'ring' }), 'проверяю…'));
    try {
      const ex = await get('/route/explain?host=' + encodeURIComponent(input.value.trim()));
      fill(out, h('div', { class: 'card cb' }, explainView(ex)),
        h('div', { style: 'display:flex;gap:10px;flex-wrap:wrap' },
          ex.action !== 'proxy' && h('button', { class: 'btn p', onclick: async () => { await quickRule(ex.host, 'proxy'); route(); check(); } }, 'Всегда через прокси'),
          ex.action !== 'direct' && h('button', { class: 'btn', onclick: async () => { await quickRule(ex.host, 'direct'); route(); check(); } }, 'Всегда напрямую')));
    } catch (e) { fill(out, notice('bad', 'alert', e.message)); }
  };
  input.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); check(); } });
  modal({ title: 'Куда пойдёт сайт', size: 'mid', body: [h('div', { style: 'display:flex;gap:10px' }, input, h('button', { class: 'btn p', onclick: check }, 'Проверить')), out] });
  if (host) check();
  else input.focus();
}

// ---------- страница ----------

export async function pageRules() {
  const [rules, nodes, groups, net, settings, listsInfo] = await Promise.all(['/rules', '/nodes', '/groups', '/network', '/settings', '/lists'].map(p => get(p).catch(() => [])));
  const devices = arr(net.devices);
  if (!state.profiles.length) state.profiles = await get('/profiles').catch(() => []);
  const profiles = state.profiles;
  state.counts.rules = rules.length;

  const label = (list, id) => (list.find(x => x[0] === id) || [0, id])[1];
  const whoTags = r => {
    const out = [];
    r.who.deviceIds.forEach(id => out.push(h('span', { class: 'tag' }, (devices.find(x => x.id === id) || { name: 'удалено' }).name)));
    r.who.tags.forEach(t => out.push(h('span', { class: 'tag' }, 'группа ' + t)));
    r.who.cidrs.forEach(c => out.push(h('span', { class: 'tag mono' }, c)));
    return out.length ? out : [h('span', { class: 'tag' }, 'все устройства')];
  };
  const whatTags = r => {
    const w = r.what, out = [];
    w.apps.forEach(id => out.push(h('span', { class: 'tag' }, (profiles.find(x => x.id === id) || { name: id }).name)));
    w.lists.forEach(id => { const l = listsInfo.find(x => x.id === id); out.push(h('span', { class: 'tag' + (l && !l.ready ? ' warn' : ''), title: l && !l.ready ? 'Список ещё не скачан' : '' }, l ? l.name.replace(/ — заблокированные /, ' — ') : id)); });
    w.protocols.forEach(p => out.push(h('span', { class: 'tag' }, label(PROTOCOLS, p).replace(/ \(.*\)/, ''))));
    w.geosite.forEach(g => out.push(h('span', { class: 'tag' }, label(QUICK_GEOSITE, g))));
    w.geoip.forEach(g => out.push(h('span', { class: 'tag' }, 'страна ' + label(QUICK_GEOIP, g))));
    if (w.domains.length) out.push(h('span', { class: 'tag', title: w.domains.join('\n') }, w.domains[0] + (w.domains.length > 1 ? ` +${w.domains.length - 1}` : '')));
    if (w.ips.length) out.push(h('span', { class: 'tag mono', title: w.ips.join('\n') }, w.ips.length === 1 ? w.ips[0] : `${w.ips.length} подсетей`));
    if (w.ports.length) out.push(h('span', { class: 'tag' }, 'порт ' + w.ports.join(', ')));
    if (w.network) out.push(h('span', { class: 'tag' }, w.network.toUpperCase()));
    return out.length ? out : [h('span', { class: 'tag' }, 'весь трафик')];
  };
  const whereTag = r => r.where.action === 'block' ? h('span', { class: 'tag bad' }, 'блокировать')
    : r.where.action === 'direct' ? h('span', { class: 'tag ok' }, 'напрямую')
    : h('span', { class: 'tag acc' }, 'через прокси · ' + (r.where.target ? targetName(r.where.target, nodes, groups) : 'активный'));

  // ----- куда пойдёт сайт -----
  const exInput = h('input', { type: 'search', placeholder: 'Куда пойдёт сайт? Например, parsec.app', 'aria-label': 'Куда пойдёт сайт' });
  const exOut = h('div', { hidden: true, style: 'padding:0 16px 14px' });
  const exCheck = async () => {
    const host = exInput.value.trim();
    if (!host) { exOut.hidden = true; return; }
    exOut.hidden = false;
    fill(exOut, h('span', { class: 'lat wait' }, h('span', { class: 'ring' }), 'проверяю…'));
    try {
      const ex = await get('/route/explain?host=' + encodeURIComponent(host));
      fill(exOut, h('div', { style: 'display:flex;align-items:flex-start;gap:12px;flex-wrap:wrap' }, h('div', { class: 'grow' }, explainView(ex)),
        ex.action !== 'proxy' && h('button', { class: 'btn sm p', onclick: async () => { await quickRule(ex.host, 'proxy'); route(); } }, 'Пустить через прокси'),
        ex.action !== 'direct' && h('button', { class: 'btn sm', onclick: async () => { await quickRule(ex.host, 'direct'); route(); } }, 'Всегда напрямую')));
    } catch (e) { fill(exOut, h('span', { class: 'hint', style: 'color:var(--bad)' }, e.message)); }
  };
  exInput.addEventListener('keydown', e => { if (e.key === 'Enter') { e.preventDefault(); exCheck(); } });
  const explainBox = h('section', { class: 'card' },
    h('label', { style: 'display:flex;align-items:center;gap:10px;padding:0 8px 0 16px;height:56px;color:var(--mut)' }, icon('globe'),
      h('span', { class: 'grow', style: 'display:flex' }, exInput), h('button', { class: 'btn sm', onclick: exCheck }, 'Проверить')),
    exOut);
  exInput.style.cssText = 'border:0;outline:0;background:transparent;flex-grow:1;font-size:15px;min-width:0';

  // ----- общие настройки -----
  const saveSettings = patch => {
    Object.assign(settings, patch);
    stage('settings', async () => { const cur = await get('/settings'); await api('PUT', '/settings', { ...cur, defaultAction: settings.defaultAction, bypassLan: settings.bypassLan }); });
  };
  const general = h('section', { class: 'card grid3', style: 'gap:0' },
    h('div', { class: 'cb', style: 'display:flex;flex-direction:column;gap:10px;border-right:1px solid var(--line2)' },
      h('span', { class: 'cap' }, 'Без подходящего правила'),
      seg([['proxy', 'Через прокси'], ['direct', 'Напрямую']], settings.defaultAction || 'proxy', v => saveSettings({ defaultAction: v }), 'fill')),
    h('div', { class: 'cb', style: 'display:flex;flex-direction:column;gap:6px;border-right:1px solid var(--line2)' },
      h('span', { class: 'cap' }, '«Через прокси» — это'),
      h('button', { style: 'display:flex;align-items:center;gap:8px;border:0;background:transparent;padding:0;text-align:left', onclick: () => openPicker(() => route()) },
        h('strong', { style: 'font-size:16px' }, activeName(settings, nodes, groups)), h('span', { style: 'color:var(--acc)' }, icon('right', 's'))),
      h('span', { class: 'hint' }, 'Активный сервер. В правиле можно выбрать другой.')),
    h('div', { class: 'cb', style: 'display:flex;flex-direction:column;gap:10px' },
      h('span', { class: 'cap' }, 'Всегда'),
      sw(settings.bypassLan, v => saveSettings({ bypassLan: v }), 'Локальная сеть напрямую'),
      sw(torrentsOn(rules), on => stageTorrents(on, rules), 'Торренты мимо прокси')));

  // ----- список -----
  let dragId = null;
  const listEl = h('div', { class: 'list' });
  const renumber = () => [...listEl.children].forEach((el, i) => { const n = el.querySelector('.idx'); if (n) n.textContent = String(i + 1).padStart(2, '0'); });
  rules.map(normRule).forEach((r, i) => {
    const row = h('div', { class: 'row' + (r.enabled ? '' : ' off'), draggable: 'true', 'data-id': r.id, style: 'padding:14px 20px;align-items:center',
      ondragstart: e => { dragId = r.id; e.dataTransfer.effectAllowed = 'move'; },
      ondragover: e => { e.preventDefault(); row.style.boxShadow = 'inset 0 2px 0 var(--acc)'; },
      ondragleave: () => { row.style.boxShadow = ''; },
      ondrop: e => {
        e.preventDefault(); row.style.boxShadow = '';
        if (!dragId || dragId === r.id) return;
        const dragged = listEl.querySelector(`[data-id="${dragId}"]`);
        if (!dragged) return;
        listEl.insertBefore(dragged, row);
        renumber();
        dragged.animate([{ background: 'var(--accs)' }, { background: 'transparent' }], { duration: 700 });
        stage('rules-order', () => api('PUT', '/rules/order', { ids: [...listEl.children].map(el => el.dataset.id) }));
      } },
      h('span', { class: 'hide-m', style: 'color:var(--faint);cursor:grab' }, icon('grip')),
      h('span', { class: 'mono idx', style: 'color:var(--faint)' }, String(i + 1).padStart(2, '0')),
      h('span', { class: 'grow', style: 'display:flex;flex-direction:column;gap:8px' }, h('span', { class: 'name' }, r.name),
        h('span', { class: 'flow' }, whoTags(r), h('span', { class: 'arr' }, '→'), whatTags(r), h('span', { class: 'arr' }, '→'), whereTag(r))),
      sw(r.enabled, v => { r.enabled = v; row.classList.toggle('off', !v); stage('rule:' + r.id, () => api('PUT', '/rules/' + r.id, r)); }, null),
      h('button', { class: 'ib', 'aria-label': 'Изменить', title: 'Изменить', onclick: () => edit(r) }, icon('edit')),
      h('button', { class: 'ib hide-m', 'aria-label': 'Копировать', title: 'Копировать', onclick: () => edit(null, { ...structuredClone(r), id: undefined, name: r.name + ' (копия)' }) }, icon('copy')),
      h('button', { class: 'ib danger hide-m', 'aria-label': 'Удалить', title: 'Удалить', onclick: () => confirmModal(`Удалить правило «${r.name}»?`, async () => { await api('DELETE', '/rules/' + r.id); route(); }) }, icon('trash')));
    listEl.append(row);
  });

  // ----- редактор -----
  function edit(orig, preset) {
    const isNew = !orig;
    const r = normRule(structuredClone(orig || preset || emptyRule()));
    const tags = [...new Set(devices.flatMap(d => arr(d.tags)))];
    let whoMode = r.who.deviceIds.length + r.who.tags.length + r.who.cidrs.length ? 'pick' : 'all';
    const whoPick = h('div', { class: 'stack', style: 'gap:10px' });
    const drawWho = () => {
      whoPick.hidden = whoMode === 'all';
      fill(whoPick, 
        devices.length ? h('div', { class: 'card list', style: 'max-height:260px;overflow-y:auto' }, devices.slice().sort((a, b) => ipKey(a.ip) - ipKey(b.ip)).map(d => h('label', { class: 'row', style: 'min-height:48px;padding:8px 14px' },
          h('input', { type: 'checkbox', class: 'check', checked: r.who.deviceIds.includes(d.id), onchange: e => r.who.deviceIds = toggleIn(r.who.deviceIds, d.id, e.target.checked) }),
          icon(d.type || 'other', 's'), h('span', { class: 'grow ell' }, d.name), h('span', { class: 'mono', style: 'color:var(--mut)' }, d.ip)))) : h('div', { class: 'hint' }, 'Устройств нет'),
        tags.length > 0 && field('Группы устройств', h('div', { class: 'opts' }, tags.map(t => opt(t, r.who.tags.includes(t), on => r.who.tags = toggleIn(r.who.tags, t, on))))),
        field('Адреса и подсети', h('textarea', { class: 'inp mono', rows: 2, placeholder: '192.168.0.0/24', oninput: e => r.who.cidrs = lines(e.target.value) }, r.who.cidrs.join('\n'))));
    };
    drawWho();

    const geoInput = (key, ph) => h('input', { class: 'inp mono', value: r.what[key].join(', '), placeholder: ph, oninput: e => r.what[key] = lines(e.target.value) });
    const geosite = geoInput('geosite', 'youtube, category-ru'), geoip = geoInput('geoip', 'ru, by');
    const quick = (items, key, input) => h('div', { class: 'opts' }, items.map(([v, l]) => opt(l, r.what[key].includes(v), on => { r.what[key] = toggleIn(r.what[key], v, on); input.value = r.what[key].join(', '); })));
    const extra = h('div', { class: 'stack', style: 'gap:14px', hidden: !(r.what.geosite.length + r.what.geoip.length + r.what.ips.length + r.what.ports.length || r.what.network) },
      field('Категории сайтов', h('div', { class: 'stack', style: 'gap:8px' }, quick(QUICK_GEOSITE, 'geosite', geosite), geosite)),
      field('Страны по IP', h('div', { class: 'stack', style: 'gap:8px' }, quick(QUICK_GEOIP, 'geoip', geoip), geoip)),
      field('IP-адреса и подсети', h('textarea', { class: 'inp mono', rows: 2, placeholder: '1.2.3.0/24', oninput: e => r.what.ips = lines(e.target.value) }, r.what.ips.join('\n'))),
      h('div', { class: 'grid2', style: 'gap:12px' },
        field('Порты', h('input', { class: 'inp', value: r.what.ports.join(', '), placeholder: '443, 27000-27100', oninput: e => r.what.ports = lines(e.target.value) })),
        field('Протокол', select([['', 'TCP и UDP'], ['tcp', 'Только TCP'], ['udp', 'Только UDP']], r.what.network, v => r.what.network = v))));
    const extraBtn = h('button', { type: 'button', class: 'btn sm g', style: 'align-self:flex-start;color:var(--acc2)', hidden: !extra.hidden, onclick: () => { extra.hidden = false; extraBtn.hidden = true; } }, icon('plus', 's'), 'Страны, категории, IP и порты');
    const shownApps = profiles.slice();

    const targetSel = select(targetOptions(nodes, groups), r.where.target, v => r.where.target = v);
    const targetBox = field('Через какой сервер', targetSel, 'Активный сервер — тот, что выбран на «Обзоре». Выберите другой, если этому правилу нужен свой.');
    targetBox.hidden = r.where.action !== 'proxy';
    const choice = (v, title, sub) => {
      const b = h('button', { type: 'button', class: 'choice' + (r.where.action === v ? ' on' : '') }, h('span', { class: 'radio' + (r.where.action === v ? ' on' : '') }), h('span', {}, h('strong', {}, title), h('br'), h('span', { class: 'hint' }, sub)));
      b.onclick = () => { r.where.action = v; [...b.parentElement.children].forEach(x => { const on = x === b; x.classList.toggle('on', on); x.firstChild.classList.toggle('on', on); }); targetBox.hidden = v !== 'proxy'; };
      return b;
    };
    const col = (n, title, ...body) => h('div', { class: 'stack', style: 'gap:14px;min-width:0' }, h('div', { style: 'display:flex;align-items:center;gap:10px' }, h('span', { class: 'step' }, n), h('span', { style: 'font-size:17px;font-weight:700' }, title)), body);
    const nameInput = h('input', { class: 'inp', value: r.name, placeholder: 'Например: Parsec через прокси', oninput: e => r.name = e.target.value });

    modal({
      title: isNew ? 'Новое правило' : 'Правило', size: 'wide',
      body: [
        field('Название', nameInput),
        h('div', { class: 'grid3', style: 'gap:24px;align-items:start' },
          col(1, 'Кто', h('div', { class: 'hint' }, 'Для каких устройств действует правило.'),
            seg([['all', 'Все устройства'], ['pick', 'Выбрать']], whoMode, v => { whoMode = v; if (v === 'all') { r.who = { deviceIds: [], tags: [], cidrs: [] }; } drawWho(); }, 'fill'), whoPick),
          col(2, 'Что',
            field('Сайты', h('textarea', { class: 'inp', rows: 3, placeholder: 'parsec.app\nfull:api.example.com\nkeyword:tracker', oninput: e => r.what.domains = lines(e.target.value) }, r.what.domains.join('\n')),
              'С поддоменами. full: — точный адрес, keyword: — по слову.'),
            field('Сервисы', h('div', { class: 'opts' }, shownApps.map(p => opt(p.name, r.what.apps.includes(p.id), on => r.what.apps = toggleIn(r.what.apps, p.id, on))))),
            field('Списки и протоколы', h('div', { class: 'opts' },
              listsInfo.map(l => opt(l.name.replace(/ — заблокированные /, ' — '), r.what.lists.includes(l.id), on => r.what.lists = toggleIn(r.what.lists, l.id, on))),
              PROTOCOLS.map(([id, l]) => opt(l, r.what.protocols.includes(id), on => r.what.protocols = toggleIn(r.what.protocols, id, on))))),
            extraBtn, extra),
          col(3, 'Куда',
            h('div', { class: 'stack', style: 'gap:8px' },
              choice('proxy', 'Через прокси', 'Через сервер — по умолчанию активный'),
              choice('direct', 'Напрямую', 'Мимо прокси, через домашний интернет'),
              choice('block', 'Блокировать', 'Устройства не смогут открыть')),
            targetBox,
            h('div', { class: 'hint' }, 'Правила проверяются сверху вниз, срабатывает первое подходящее. Маршрут устройства важнее правил.'))),
      ],
      actions: [{ label: isNew ? 'Создать правило' : 'Сохранить', primary: true, onClick: async () => {
        if (!r.name.trim()) {
          const apps = r.what.apps.map(id => (profiles.find(p => p.id === id) || {}).name).filter(Boolean);
          r.name = apps.slice(0, 3).join(', ') || r.what.domains[0] || 'Правило';
        }
        if (isNew) {
          delete r.id;
          const created = await api('POST', '/rules', r);
          // Новое правило с сайтами ставим первым: иначе его перекроют общие списки.
          if (r.what.domains.length) await api('PUT', '/rules/order', { ids: [created.id, ...rules.map(x => x.id)] });
        } else await api('PUT', '/rules/' + r.id, r);
        toast(isNew ? 'Правило создано' : 'Правило сохранено', 'ok');
        route();
      } }],
    });
    setTimeout(() => nameInput.focus(), 50);
  }

  function templates() {
    let close;
    const body = h('div', { class: 'card list' }, TEMPLATES.map(t => h('div', { class: 'row', style: 'align-items:flex-start;padding:14px 16px' },
      h('div', { class: 'grow' }, h('div', { class: 'name' }, t.title), h('div', { class: 'hint' }, t.desc),
        t.defaultAction && h('div', { class: 'hint', style: 'margin-top:4px' }, 'Трафик без правила: ', h('strong', {}, t.defaultAction === 'proxy' ? 'через прокси' : 'напрямую'))),
      h('button', { class: 'btn sm', onclick: async e => {
        e.currentTarget.classList.add('busy');
        try {
          const added = [];
          for (const rule of t.rules) added.push(await api('POST', '/rules', normRule({ ...rule, enabled: true })));
          if (t.first) { const all = await get('/rules'); const ids = added.map(x => x.id); await api('PUT', '/rules/order', { ids: [...ids, ...all.map(x => x.id).filter(id => !ids.includes(id))] }); }
          if (t.defaultAction) { const cur = await get('/settings'); await api('PUT', '/settings', { ...cur, defaultAction: t.defaultAction }); }
          toast('Сценарий добавлен', 'ok');
          close(); route();
        } catch (err) { toast(err.message, 'err'); }
      } }, 'Добавить'))));
    close = modal({ title: 'Готовые сценарии', size: 'mid', body });
  }

  const devRoutes = devices.filter(d => d.route).length;
  return h('div', { class: 'stack' },
    pageHead('Правила', 'Проверяются сверху вниз, срабатывает первое подходящее. Порядок меняется перетаскиванием.',
      h('button', { class: 'btn', onclick: templates }, icon('template', 's'), 'Сценарии'),
      h('button', { class: 'btn p', onclick: () => edit() }, icon('plus', 's'), 'Новое правило')),
    explainBox,
    general,
    devRoutes > 0 && notice('info', 'info', `Устройств с собственным маршрутом: ${devRoutes}. Для них этот маршрут важнее правил.`, h('a', { class: 'btn sm', href: '#/devices' }, 'Устройства')),
    card(h('h2', { class: 'ct' }, 'Правила ', h('span', { class: 'hint', style: 'font-weight:400' }, `· ${rules.length}`)), null,
      rules.length ? listEl : empty('rules', 'Правил пока нет', 'Начните с готового сценария или создайте правило вручную.', h('button', { class: 'btn', onclick: templates }, icon('template', 's'), 'Сценарии'))));
}
