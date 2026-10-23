// Обзор: активный сервер, трафик, устройства, состояние серверов и обновление.

import {
  h, fill, icon, api, get, state, stage, router, route, seg, sw, card, pageHead, empty, notice, arr, isClient, ipKey,
  mbps, bytes, latencyEl, activeName, typeName, refreshStatus, plural,
} from './core.js';
import { openPicker } from './servers.js';
import { torrentsOn, stageTorrents } from './rules.js';
import { coreButton, refreshActive } from './app.js';

function greeting() {
  const hr = new Date().getHours();
  return hr < 5 ? 'Доброй ночи' : hr < 12 ? 'Доброе утро' : hr < 18 ? 'Добрый день' : 'Добрый вечер';
}

export async function pageOverview() {
  await refreshStatus();
  const s = state.status;
  const [settings, nodes, groups, rules, net, conns, stats, auto, tp] = await Promise.all(
    ['/settings', '/nodes', '/groups', '/rules', '/network', '/connections', '/stats', '/auto', '/transparent'].map(p => get(p).catch(() => null)));
  const devs = arr(net && net.devices);
  const clients = devs.filter(isClient);

  // ----- активный сервер -----
  const now = h('div', { class: 'soft', style: 'display:flex;align-items:center;gap:10px;flex-wrap:wrap' });
  const drawNow = a => {
    const id = settings.defaultTarget || '';
    const ag = a && arr(a.groups).find(g => g.groupId === id && (id || g.name === 'Авто'));
    const lat = (a && a.latency) || {};
    if (ag) {
      const cur = arr(nodes).find(n => n.id === ag.selected);
      const pillCls = !ag.ms ? '' : ag.ms < 0 ? ' bad' : ag.ms > 300 ? ' warn' : '';
      fill(now, 
        ag.scanning ? h('span', {}, `Подбираю сервер: ${ag.scanning[0]} из ${ag.scanning[1]}…`)
          : cur ? h('span', {}, 'Сейчас работает ', h('strong', { style: 'color:var(--hero-strong)' }, cur.name))
          : h('span', {}, state.status && state.status.running ? 'Сервер подбирается…' : 'Сервер выберется при запуске шлюза'),
        cur && ag.ms ? h('span', { class: 'pill' + pillCls }, ag.ms > 0 ? ag.ms + ' мс' : 'не отвечает') : null,
        h('span', { style: 'color:var(--hero-mut)' }, [ag.checkedAt ? 'проверен ' + agoShort(ag.checkedAt) : null, `${plural(ag.members, 'сервер', 'сервера', 'серверов')} в группе`].filter(Boolean).join(' · ')));
    } else {
      const l = lat[id];
      fill(now, h('span', {}, 'Отдельный сервер'), l && l.ms > 0 ? h('span', { class: 'pill' }, l.ms + ' мс') : null);
    }
  };
  drawNow(auto);

  const hero = h('section', { class: 'hero span2', 'aria-label': 'Активный сервер' },
    h('div', { style: 'display:flex;align-items:center;gap:10px;flex-wrap:wrap' }, h('span', { class: 'cap grow' }, 'Активный сервер'),
      h('span', { class: 'chip hide-m' }, 'через него — всё, что «через прокси»')),
    h('div', { style: 'display:flex;align-items:center;gap:20px;flex-wrap:wrap' },
      h('div', { class: 'grow', style: 'display:flex;flex-direction:column;gap:10px' }, h('div', { class: 'big' }, activeName(settings, arr(nodes), arr(groups))), now),
      h('button', { class: 'btn light lg', disabled: !arr(nodes).length, onclick: () => openPicker(() => { refreshActive(); route(); }) }, 'Сменить', icon('down', 's'))),
    h('hr'),
    h('div', { style: 'display:flex;align-items:center;gap:14px 22px;flex-wrap:wrap' },
      h('span', { class: 'soft' }, 'Трафик без правила'),
      seg([['proxy', 'Через прокси'], ['direct', 'Напрямую']], settings.defaultAction || 'proxy', v => {
        settings.defaultAction = v;
        stage('settings', async () => { const cur = await get('/settings'); await api('PUT', '/settings', { ...cur, defaultAction: v }); });
      }, 'dk'),
      h('span', { class: 'grow' }),
      h('span', { class: 'soft' }, sw(torrentsOn(arr(rules)), on => stageTorrents(on, arr(rules)), 'Торренты мимо прокси'))));

  // ----- трафик -----
  const today = stats ? stats.today : null;
  const traffic = h('section', { class: 'card cb', 'aria-label': 'Трафик сейчас', style: 'display:flex;flex-direction:column;gap:12px' });
  const drawTraffic = c => {
    const prox = today && today.down ? Math.round(today.proxyDown / today.down * 100) : null;
    fill(traffic, 
      h('span', { class: 'cap' }, 'Трафик сейчас'),
      !c || !c.totals ? h('div', { class: 'hint' }, s.running ? 'Данные появятся через пару секунд' : 'Шлюз остановлен') : [
        h('div', { style: 'display:flex;align-items:baseline;gap:8px' }, h('span', { style: 'color:var(--acc)' }, icon('arrowDown')),
          h('span', { style: 'font-size:32px;font-weight:700;letter-spacing:-0.02em' }, mbps(c.downRate)), h('span', { class: 'hint' }, 'МБ/с')),
        h('div', { style: 'display:flex;align-items:baseline;gap:8px' }, h('span', { style: 'color:var(--mut)' }, icon('arrowUp', 's')),
          h('span', { style: 'font-size:18px;font-weight:600' }, mbps(c.upRate)), h('span', { class: 'hint' }, 'МБ/с отдача')),
      ],
      prox != null && [
        h('div', { style: 'display:flex;height:10px;border-radius:99px;overflow:hidden;gap:2px;margin-top:4px' },
          h('span', { style: `width:${prox}%;background:var(--acc)` }), h('span', { style: `width:${100 - prox}%;background:var(--split2)` })),
        h('div', { class: 'hint', style: 'display:flex;justify-content:space-between' },
          h('span', {}, h('strong', { style: 'color:var(--acc2)' }, prox + '%'), ' через прокси'), h('span', {}, h('strong', { style: 'color:var(--ink2)' }, (100 - prox) + '%'), ' напрямую')),
      ],
      today && h('div', { class: 'hint', style: 'padding-top:10px;border-top:1px solid var(--line2)' },
        `За сегодня ${bytes(today.down + today.up)}` + (today.peak ? ` · пик ${mbps(today.peak)} МБ/с в ${new Date(today.peakAt * 1000).toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit' })}` : '')));
  };
  drawTraffic(conns);

  // ----- показатели -----
  const online = clients.filter(d => d.online);
  const newCount = devs.filter(d => d.firstSeen && Date.now() / 1000 - d.firstSeen < 86400 && isClient(d)).length;
  const ag = auto && arr(auto.groups).find(g => g.groupId === (settings.defaultTarget || '') && (settings.defaultTarget || g.name === 'Авто'));
  const okServers = auto ? Object.values(auto.latency || {}).filter(l => l.ms > 0).length : 0;
  const kpi = (href, cap, v, small, hint) => h('a', { class: 'card cb kpi', href }, h('span', { class: 'cap' }, cap), h('div', { class: 'v' }, v, small && h('small', {}, ' ' + small)), h('div', { class: 'hint' }, hint));
  const kpis = h('div', { class: 'grid4 m2' },
    kpi('#/devices', 'Устройства', online.length, 'в сети из ' + clients.length, newCount ? `новых за сутки: ${newCount}` : `в локальной сети сейчас: ${devs.filter(d => d.online).length}`),
    kpi('#/connections', 'Соединения', conns && conns.available ? arr(conns.connections).length : '—', '', conns && conns.available ? 'через прокси ' + arr(conns.connections).filter(c => c.outbound !== 'Напрямую' && c.outbound !== 'Заблокировано').length : (conns && conns.reason) || ''),
    kpi('#/rules', 'Правила', arr(rules).filter(r => r.enabled).length, 'из ' + arr(rules).length + ' включены', 'проверяются сверху вниз'),
    kpi('#/nodes', 'Серверы', ag ? ag.members : arr(nodes).length, ag ? 'в активной группе' : 'всего', okServers ? `отвечают по последней проверке: ${okServers}` : 'ещё не проверялись'));

  // ----- устройства -----
  const active = clients.filter(d => d.online).sort((a, b) => ((b.downRate + b.upRate) - (a.downRate + a.upRate)) || ipKey(a.ip) - ipKey(b.ip)).slice(0, 6);
  const routeChip = d => d.route === 'proxy' ? h('span', { class: 'chip acc' }, 'Через прокси') : d.route === 'direct' ? h('span', { class: 'chip ok' }, 'Напрямую') : d.route === 'block' ? h('span', { class: 'chip bad' }, 'Без интернета') : h('span', { class: 'chip' }, 'По правилам');
  const devicesCard = card('Сейчас активны', h('a', { href: '#/devices', class: 'strong' }, 'Все устройства'),
    active.length ? h('div', { class: 'list' }, active.map(d => h('a', { class: 'row', href: '#/devices' },
      h('span', { class: 'ico' }, icon(d.type || 'other'), h('span', { class: 'on' })),
      h('span', { class: 'grow' }, h('span', { class: 'name' }, d.name), h('br'), h('span', { class: 'mono', style: 'color:var(--mut)' }, d.ip)),
      h('span', { class: 'mono col', style: 'width:110px;text-align:right' }, d.downRate > 1024 ? '↓ ' + mbps(d.downRate) + ' МБ/с' : h('span', { class: 'hint' }, '—')),
      h('span', { class: 'col hide-m', style: 'width:140px;text-align:right' }, routeChip(d)))))
      : empty('devices', 'Сейчас никого нет', clients.length ? 'Устройства, которые пользуются NetPilot, не в сети.' : 'Пока ни одно устройство не пускало трафик через NetPilot.'));
  devicesCard.classList.add('span2');

  const u = state.update;
  const side = h('div', { class: 'stack' },
    h('section', { class: 'card cb', style: 'display:flex;flex-direction:column;gap:10px' },
      h('div', { style: 'display:flex;align-items:center;gap:8px' }, h('span', { class: 'cap grow' }, 'Шлюз'), s.running ? h('span', { class: 'chip ok' }, 'работает') : h('span', { class: 'chip' }, 'остановлен')),
      h('span', {}, s.running ? `${s.core} ${s.coreVersion || ''} · ${settings.mode === 'transparent' ? 'прозрачный прокси' : 'прокси-сервер'}` : (s.error || 'Трафик устройств не обрабатывается')),
      coreButton()),
    u && u.available && h('section', { class: 'card cb acc', style: 'display:flex;flex-direction:column;gap:10px' },
      h('div', { style: 'display:flex;align-items:center;gap:8px' }, h('span', { class: 'cap grow', style: 'color:var(--acc2)' }, 'Обновление'), h('span', { class: 'chip acc' }, u.latest.version)),
      h('span', {}, 'Доступна новая версия NetPilot'),
      h('a', { class: 'btn p', href: '#/settings' }, 'Посмотреть и обновить')));

  const alerts = [];
  if (s.error) alerts.push(notice('bad', 'alert', s.error));
  // У самого сервера нет интернета (например, его шлюз по умолчанию — он сам): это причина всех остальных бед.
  arr(tp && tp.critical).forEach(p => alerts.push(notice('bad', 'alert', p)));
  arr(s.issues).forEach(i => alerts.push(notice('', 'alert', i.message)));
  if (!arr(nodes).length) alerts.push(notice('info', 'info', 'Серверы не добавлены: весь трафик идёт напрямую.', h('a', { class: 'btn sm', href: '#/nodes' }, 'Добавить серверы')));
  arr(net && net.warnings).forEach(w => alerts.push(notice('', 'alert', w)));

  const ip = arr(s.lanIps)[0];
  const root = h('div', { class: 'stack' },
    pageHead(greeting(), [ip ? `Шлюз ${ip}` : null, `через NetPilot — ${plural(clients.length, 'устройство', 'устройства', 'устройств')}`, s.running ? `ядро ${s.core}` : null].filter(Boolean).join(' · ')),
    alerts,
    h('div', { class: 'grid3' }, hero, traffic),
    kpis,
    h('div', { class: 'grid3' }, devicesCard, side));

  // Скорость и состояние группы обновляются сами.
  router.timer = setInterval(async () => {
    const [c, a] = await Promise.all([get('/connections').catch(() => null), get('/auto').catch(() => null)]);
    if (c) drawTraffic(c);
    if (a) drawNow(a);
  }, 3000);
  return root;
}

function agoShort(unix) {
  const s = Math.floor(Date.now() / 1000 - unix);
  if (s < 60) return s + ' с назад';
  if (s < 3600) return Math.floor(s / 60) + ' мин назад';
  return Math.floor(s / 3600) + ' ч назад';
}
export { typeName };
