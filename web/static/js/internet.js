// Интернет: как NetPilot стоит в сети — шлюзом за роутером или сам роутер.

import { h, fill, icon, api, get, toast, state, guard, takeDraft, router, route, field, seg, sw, select, card, pageHead, notice, arr, lines, bytes, uptime, refreshStatus, isClient } from './core.js';

const spanned = el => { el.classList.add('span2'); return el; };
const WAN_STATES = { up: ['ok', 'Подключено'], connecting: ['warn', 'Подключение…'], error: ['bad', 'Ошибка'], down: ['bad', 'Нет подключения'], disabled: ['', 'Выключено'] };

export async function pageInternet() {
  await refreshStatus();
  let [v, tp, net, settings] = await Promise.all(['/router', '/transparent', '/network', '/settings'].map(p => get(p).catch(() => null)));
  const root = h('div', { class: 'stack' });
  const fresh = structuredClone(v.config);
  fresh.wan.dns = arr(fresh.wan.dns);
  fresh.wan.password = '';
  let saved = JSON.stringify(fresh);
  const c = takeDraft('internet') || fresh;
  guard.form = {
    isDirty: () => JSON.stringify(c) !== saved,
    draft: () => c,
    save: async () => {
      const body = structuredClone(c);
      body.wan.mtu = +body.wan.mtu || 0;
      body.lan.leaseHours = +body.lan.leaseHours || 12;
      await api('PUT', '/router', body);
      c.wan.password = '';
      saved = JSON.stringify(c);
      return c.enabled ? 'Настройки применены на пробу — проверьте интернет и подтвердите' : 'Сохранено';
    },
  };
  const s = state.status || {};
  const ip = arr(s.lanIps)[0] || '—';
  const gw = arr(net && net.devices).find(d => d.gateway);
  const clients = arr(net && net.devices).filter(isClient).length;

  const node = (ic, title, sub, accent) => h('div', { style: 'flex:1;display:flex;flex-direction:column;align-items:center;gap:8px;text-align:center;min-width:90px' },
    h('span', { class: 'ico big', style: accent ? 'background:var(--acc);color:var(--onacc)' : '' }, icon(ic, 'l')), h('strong', {}, title), h('span', { class: 'meta mono' }, sub));
  const link = ok => h('span', { style: `flex:0 0 40px;height:2px;background:${ok ? 'var(--okd)' : 'var(--line3)'};margin-top:-36px` });

  const checks = [];
  const chk = (title, ok, okText, badText) => checks.push(h('div', { class: 'kv' }, h('span', {}, title), ok ? h('span', { class: 'chip ok' }, okText) : h('span', { class: 'chip ' + (ok === null ? '' : 'warn') }, badText)));
  if (settings.mode === 'transparent') {
    chk('Перехват трафика', tp.active, 'включён', tp.supported ? 'включится при запуске' : 'недоступен');
    chk('Перехват DNS', settings.hijackDns, 'включён', 'выключен');
    chk('IPv6', settings.ipv6 !== 'off' ? true : null, settings.ipv6 === 'intercept' ? 'перехватывается' : 'заблокирован', 'не трогается');
  } else {
    chk('Режим', null, '', 'прокси-сервер: шлюзом не работает');
  }
  chk('Шлюз', s.running, 'работает', 'остановлен');
  arr(tp.problems).forEach(p => checks.push(h('div', { class: 'hint', style: 'color:var(--warn);padding:6px 0' }, p)));

  const copy = () => { navigator.clipboard && navigator.clipboard.writeText(ip).then(() => toast('Адрес скопирован', 'ok')); };
  const gatewayView = () => [
    h('div', { class: 'grid3' },
      spanned(card('Схема сети', s.running && settings.mode === 'transparent' ? h('span', { class: 'chip ok' }, 'всё работает') : h('span', { class: 'chip warn' }, 'проверьте'),
        h('div', { class: 'cb', style: 'display:flex;align-items:center;gap:8px;padding:28px 20px;flex-wrap:wrap;justify-content:center' },
          node('globe', 'Провайдер', 'интернет'), link(true),
          node('router', 'Роутер', gw ? gw.ip : '—'), link(true),
          node('logo', 'NetPilot', ip, true), link(s.running),
          node('devices', `${clients} устр.`, 'шлюз и DNS — NetPilot')))),
      card('Проверка', null, h('div', { class: 'cb', style: 'padding-top:6px;padding-bottom:6px' }, checks))),
    card('Что указать в роутере', h('button', { class: 'btn sm', onclick: copy }, icon('copy', 's'), 'Скопировать'),
      h('div', { class: 'cb grid3', style: 'gap:16px' },
        h('div', { class: 'fld' }, h('span', { class: 'hint' }, 'Основной шлюз'), h('span', { class: 'mono', style: 'font-size:18px;color:var(--ink)' }, ip)),
        h('div', { class: 'fld' }, h('span', { class: 'hint' }, 'DNS-сервер'), h('span', { class: 'mono', style: 'font-size:18px;color:var(--ink)' }, ip)),
        h('div', { class: 'fld' }, h('span', { class: 'hint' }, 'Где искать'), h('span', {}, 'Настройки роутера → LAN → DHCP-сервер')))),
  ];

  // ----- режим роутера -----
  function routerView() {
    const w = c.wan, l = c.lan;
    const st = v.status || {};
    const [cls, label] = WAN_STATES[st.state] || ['', st.state || '—'];
    const ifaceOptions = [['', '— выберите порт —'], ...arr(v.interfaces).map(i => [i.name, `${i.name} · ${i.mac}${i.carrier ? '' : ' · кабель не подключён'}`])];
    const protoBox = h('div', { class: 'stack', style: 'gap:14px' });
    const drawProto = () => fill(protoBox, ...(w.proto === 'pppoe' ? [
      h('div', { class: 'grid2', style: 'gap:12px' },
        field('Логин', h('input', { class: 'inp', value: w.username || '', autocomplete: 'off', oninput: e => w.username = e.target.value })),
        field('Пароль', h('input', { class: 'inp', type: 'password', autocomplete: 'new-password', placeholder: v.passwordSet ? 'сохранён' : '', oninput: e => w.password = e.target.value }), v.passwordSet ? 'Пусто — не менять' : null)),
      field('Имя сервиса', h('input', { class: 'inp', value: w.service || '', oninput: e => w.service = e.target.value }), 'Обычно не нужно'),
    ] : w.proto === 'static' ? [
      h('div', { class: 'grid2', style: 'gap:12px' },
        field('IP-адрес с маской', h('input', { class: 'inp mono', value: w.address || '', placeholder: '203.0.113.10/24', oninput: e => w.address = e.target.value.trim() })),
        field('Шлюз провайдера', h('input', { class: 'inp mono', value: w.gateway || '', oninput: e => w.gateway = e.target.value.trim() }))),
    ] : [h('div', { class: 'hint' }, 'Адрес, шлюз и DNS выдаёт провайдер автоматически.')]));
    drawProto();
    const lanBox = h('div', { class: 'grid3', style: 'gap:12px', hidden: !l.dhcp },
      field('Раздавать с', h('input', { class: 'inp mono', value: l.rangeStart, oninput: e => l.rangeStart = e.target.value.trim() })),
      field('по', h('input', { class: 'inp mono', value: l.rangeEnd, oninput: e => l.rangeEnd = e.target.value.trim() })),
      field('Аренда, часов', h('input', { class: 'inp', type: 'number', value: l.leaseHours, oninput: e => l.leaseHours = e.target.value })));
    return [
      v.pending && notice('', 'alert', `Настройки применены на пробу. Если интернет и панель работают — подтвердите. Иначе через ${Math.max(0, Math.round(v.pending - Date.now() / 1000))} с вернутся прежние.`,
        h('button', { class: 'btn sm p', onclick: async () => { await api('POST', '/router/confirm'); toast('Настройки сохранены', 'ok'); route(); } }, icon('check', 's'), 'Подтвердить')),
      card('Подключение к провайдеру', st.state && st.state !== 'disabled' && h('button', { class: 'btn sm', onclick: async () => { await api('POST', '/router/reconnect').catch(e => toast(e.message, 'err')); route(); } }, icon('refresh', 's'), 'Переподключить'),
        h('div', { class: 'cb' },
          h('div', { style: 'display:flex;align-items:center;gap:8px;margin-bottom:8px' }, h('span', { class: 'dot ' + cls }), h('strong', {}, label), st.since && h('span', { class: 'hint' }, '· ' + uptime(st.since))),
          st.message && notice(st.state === 'error' ? 'bad' : '', 'alert', st.message),
          st.state && st.state !== 'disabled' && [['Интерфейс', st.device], ['IP-адрес', st.ip], ['Шлюз провайдера', st.gateway], ['DNS', arr(st.dns).join(', ')], ['Получено / отправлено', `${bytes(st.rxBytes)} / ${bytes(st.txBytes)}`]]
            .map(([k, val]) => h('div', { class: 'kv' }, h('span', { class: 'hint' }, k), h('span', { class: 'mono' }, val || '—'))))),
      card('Провайдер (WAN)', null, h('div', { class: 'cb stack', style: 'gap:14px' },
        field('Порт провайдера', select(ifaceOptions, w.interface || '', x => w.interface = x), 'Порт, в который включён кабель провайдера'),
        field('Тип подключения', seg([['pppoe', 'PPPoE'], ['dhcp', 'Динамический IP'], ['static', 'Статический IP']], w.proto || 'pppoe', x => { w.proto = x; drawProto(); }, 'fill')),
        protoBox,
        h('div', { class: 'grid3', style: 'gap:12px' },
          field('DNS-серверы', h('input', { class: 'inp mono', value: w.dns.join(', '), placeholder: 'провайдера', oninput: e => w.dns = lines(e.target.value) })),
          field('MTU', h('input', { class: 'inp', type: 'number', value: w.mtu || 0, oninput: e => w.mtu = e.target.value }), '0 — автоматически'),
          field('Клонировать MAC', h('input', { class: 'inp mono', value: w.mac || '', placeholder: 'aa:bb:cc:dd:ee:ff', oninput: e => w.mac = e.target.value.trim() }))))),
      card('Локальная сеть (LAN)', null, h('div', { class: 'cb stack', style: 'gap:14px' },
        h('div', { class: 'grid2', style: 'gap:12px' },
          field('Порт локальной сети', select(ifaceOptions, l.interface || '', x => l.interface = x)),
          field('Адрес роутера', h('input', { class: 'inp mono', value: l.address, oninput: e => l.address = e.target.value.trim() }), 'С маской, например 192.168.10.1/24')),
        sw(l.dhcp, x => { l.dhcp = x; lanBox.hidden = !x; }, 'Раздавать адреса (DHCP)'),
        lanBox)),
      h('div', { class: 'hint' }, 'Новые настройки применяются на пробу: если за 90 секунд их не подтвердить, вернутся прежние — доступ к панели не потеряется.'),
    ];
  }

  function draw() {
    const sup = v.support || {};
    const body = h('div', { class: 'stack' });
    const drawBody = () => fill(body, ...(c.enabled ? (sup.available ? routerView() : [notice('bad', 'alert', 'Режим роутера недоступен на этой системе: ' + arr(sup.problems).join('; '))]) : gatewayView()));
    const choice = (on, title, sub, extra) => {
      const b = h('button', { type: 'button', class: 'choice' + (c.enabled === on ? ' on' : '') }, h('span', { class: 'radio' + (c.enabled === on ? ' on' : '') }),
        h('span', { style: 'display:flex;flex-direction:column;gap:6px' }, h('strong', { style: 'font-size:16px' }, title), h('span', { class: 'hint' }, sub), extra));
      b.onclick = () => { c.enabled = on; draw(); };
      return b;
    };
    drawBody();
    fill(root, 
      pageHead('Интернет', 'Как NetPilot стоит в сети: шлюзом за роутером или сам роутер'),
      h('div', { class: 'grid2' },
        choice(false, 'Шлюз за роутером', [`Интернет приходит на ваш роутер. В его DHCP укажите NetPilot основным шлюзом и DNS: `, h('span', { class: 'mono', style: 'color:var(--ink)' }, ip)],
          !JSON.parse(saved).enabled && h('span', { class: 'chip ok', style: 'align-self:flex-start' }, 'так сейчас')),
        choice(true, 'NetPilot — роутер', 'Кабель провайдера включён прямо в это устройство: NetPilot подключается к провайдеру и раздаёт адреса сам.',
          JSON.parse(saved).enabled && h('span', { class: 'chip ok', style: 'align-self:flex-start' }, 'так сейчас'))),
      body,
      card('Журнал подключения', h('button', { class: 'btn sm', onclick: async e => { const pre = e.currentTarget.closest('.card').querySelector('pre'); pre.hidden = false; pre.textContent = (await get('/router/log')).log || 'Журнал пуст'; } }, 'Показать'),
        h('pre', { class: 'log', hidden: true })));
  }

  draw();
  router.timer = setInterval(async () => {
    if (!c.enabled || document.querySelector('.modal-bg')) return;
    const a = document.activeElement;
    if (a && root.contains(a) && /INPUT|SELECT/.test(a.tagName)) return;
    try { v = await get('/router'); draw(); } catch {}
  }, 4000);
  return root;
}
