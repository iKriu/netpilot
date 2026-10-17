// Соединения: что сейчас открывают устройства, куда это идёт и почему.

import { h, fill, icon, api, get, toast, confirmModal, router, route, seg, select, card, pageHead, empty, notice, arr, ipKey, mbps, bytes, duration } from './core.js';
import { quickRule } from './rules.js';

const cs = { device: '', query: '', kind: 'all', paused: false };

export async function pageConnections() {
  const root = h('div', { class: 'stack' });
  let data = await get('/connections');
  const isDirect = c => c.outbound === 'Напрямую';
  const isBlock = c => c.outbound === 'Заблокировано';

  function draw() {
    const all = arr(data.connections);
    const q = cs.query.toLowerCase();
    const list = all.filter(c => (!cs.device || c.sourceIp === cs.device)
      && (cs.kind === 'all' || (cs.kind === 'direct' ? isDirect(c) : !isDirect(c) && !isBlock(c)))
      && (!q || [c.host, c.destination, c.deviceName, c.sourceIp, c.outbound, c.rule].some(x => (x || '').toLowerCase().includes(q))))
      .sort((a, b) => (b.down + b.up) - (a.down + a.up));
    const sources = [...new Set(all.map(c => c.sourceIp))].sort((a, b) => ipKey(a) - ipKey(b));
    const srcName = ip => (all.find(c => c.sourceIp === ip) || {}).deviceName || ip;

    const head = pageHead('Соединения', 'Что сейчас открывают устройства и куда это идёт. Обновляется каждые 2 секунды.',
      h('button', { class: 'btn', onclick: () => { cs.paused = !cs.paused; draw(); } }, icon(cs.paused ? 'play' : 'pause', 's'), cs.paused ? 'Продолжить' : 'Пауза'),
      h('button', { class: 'btn d', disabled: !all.length, onclick: () => confirmModal('Закрыть все соединения? Приложения переподключатся сами.', async () => { await api('DELETE', '/connections'); await reload(); }, 'Закрыть все') }, icon('close', 's'), 'Закрыть все'));

    const kpi = (cap, v, cls) => h('div', { class: 'card cb kpi' }, h('span', { class: 'cap' }, cap), h('div', { class: 'v', style: cls ? 'color:var(--acc2)' : '' }, v));
    const kpis = h('div', { class: 'grid4 m2' },
      kpi('Загрузка', data.totals ? mbps(data.downRate) + ' МБ/с' : '—'),
      kpi('Отдача', data.totals ? mbps(data.upRate) + ' МБ/с' : '—'),
      kpi('Соединений', all.length),
      kpi('Через прокси', all.filter(c => !isDirect(c) && !isBlock(c)).length, true));

    if (!data.available) {
      fill(root, head, data.totals ? kpis : null,
        card(null, null, empty('connections', 'Список соединений недоступен', data.reason || '', data.reason === 'Ядро остановлено' && h('a', { class: 'btn', href: '#/' }, 'На обзор'))));
      return;
    }

    const routeOf = c => isDirect(c) ? h('span', { class: 'chip ok' }, 'Напрямую') : isBlock(c) ? h('span', { class: 'chip bad' }, 'Заблокировано') : h('span', { class: 'chip acc' }, c.outbound || 'прокси');
    const rows = list.slice(0, 150).map(c => h('div', { class: 'row' },
      h('span', { class: 'col', style: 'width:170px' }, h('span', { class: 'name ell', style: 'display:block' }, c.deviceName || c.sourceIp), h('span', { class: 'mono', style: 'color:var(--mut)' }, c.sourceIp)),
      h('span', { class: 'grow' }, h('span', { class: 'name ell', style: 'display:block', title: c.host || c.destination }, c.host || c.destination || '—'),
        h('span', { class: 'meta' }, (c.network || '').toUpperCase() + ' · ' + duration(c.start))),
      h('span', { class: 'col m-full', style: 'width:250px' }, routeOf(c), h('div', { class: 'meta ell', title: c.rule }, c.rule ? 'правило: ' + c.rule : 'нет правила')),
      h('span', { class: 'col mono hide-m', style: 'width:110px' }, '↓ ' + bytes(c.down), h('br'), h('span', { style: 'color:var(--mut)' }, '↑ ' + bytes(c.up))),
      h('span', { class: 'col', style: 'display:flex;gap:2px' },
        c.host && (isDirect(c)
          ? h('button', { class: 'ib b', style: 'color:var(--acc)', 'aria-label': 'Всегда через прокси', title: `Всегда через прокси: ${c.host}`, onclick: async () => { await quickRule(c.host.replace(/:\d+$/, ''), 'proxy'); } }, icon('servers', 's'))
          : h('button', { class: 'ib', 'aria-label': 'Всегда напрямую', title: `Всегда напрямую: ${c.host}`, onclick: async () => { await quickRule(c.host.replace(/:\d+$/, ''), 'direct'); } }, icon('arrow', 's'))),
        h('button', { class: 'ib', 'aria-label': 'Закрыть соединение', title: 'Закрыть соединение', onclick: async () => { await api('DELETE', '/connections/' + encodeURIComponent(c.id)).catch(e => toast(e.message, 'err')); reload(); } }, icon('close', 's')))));

    const search = h('input', { type: 'search', placeholder: 'Сайт, адрес или правило', value: cs.query, 'aria-label': 'Поиск соединения',
      oninput: e => { cs.query = e.target.value; draw(); const el = root.querySelector('input[type=search]'); if (el) { el.focus(); el.setSelectionRange(el.value.length, el.value.length); } } });
    const outPanel = arr(data.outbounds).length > 0 && card('Трафик по серверам', null, h('div', { class: 'list' }, data.outbounds.map(o => h('div', { class: 'row' },
      h('span', { class: 'grow name' }, o.name), h('span', { class: 'col mono', style: 'width:150px' }, '↓ ' + mbps(o.downRate) + ' МБ/с'),
      h('span', { class: 'col mono hide-m', style: 'width:150px;color:var(--mut)' }, 'за сеанс ' + bytes(o.down))))));

    fill(root, head, kpis,
      h('section', { class: 'card', style: 'overflow:hidden' },
        h('div', { class: 'ch', style: 'gap:10px' },
          h('label', { class: 'search', style: 'flex:1 1 220px;width:auto;border-color:var(--line)' }, icon('search', 's'), search),
          select([['', `Все устройства · ${sources.length}`], ...sources.map(ip => [ip, `${srcName(ip)} — ${ip}`])], cs.device, v => { cs.device = v; draw(); }, { style: 'width:auto;min-width:200px' }),
          seg([['all', 'Все'], ['proxy', 'Прокси'], ['direct', 'Напрямую']], cs.kind, v => { cs.kind = v; draw(); })),
        h('div', { class: 'thead' }, h('span', { class: 'col', style: 'width:170px' }, 'Устройство'), h('span', { class: 'grow' }, 'Сайт'), h('span', { class: 'col', style: 'width:250px' }, 'Куда и почему'),
          h('span', { class: 'col', style: 'width:110px' }, 'Трафик'), h('span', { class: 'col', style: 'width:76px' })),
        rows.length ? h('div', { class: 'list' }, rows, list.length > 150 && h('div', { class: 'row hint' }, `Показаны 150 самых крупных из ${list.length}`))
          : empty('connections', all.length ? 'Ничего не найдено' : 'Соединений нет', all.length ? 'Измените фильтр.' : 'Устройства сейчас не передают данные через NetPilot.')),
      outPanel);
  }

  async function reload() { data = await get('/connections'); draw(); }

  draw();
  router.timer = setInterval(() => {
    const a = document.activeElement;
    if (cs.paused || document.querySelector('.modal-bg') || (a && root.contains(a) && /INPUT|SELECT/.test(a.tagName) && a.type !== 'search')) return;
    reload().catch(() => {});
  }, 2000);
  return root;
}
