// Устройства: кто ходит через NetPilot, маршрут каждого и карточка устройства.

import {
  h, fill, icon, api, get, toast, modal, confirmModal, state, stage, router, route, field, seg, sw, opt, select,
  card, pageHead, empty, notice, arr, ipKey, lines, ago, mbps, bytes, isClient, typeName, DEVICE_TYPES, ROUTES,
  routeName, targetOptions, targetName, toggleIn, refreshSaveBar, pending, plural,
} from './core.js';

const devState = { filter: 'clients', query: '', selected: new Set() };
try { const f = localStorage.getItem('np-dev-filter'); if (f) devState.filter = f; } catch {}

// Убирает поля, которые считает сервер и которые не хранятся.
const stripView = d => {
  const { online, lastSeen, seenIp, randomMac, client, up, down, upRate, downRate, conns, ...rest } = d;
  return rest;
};

export async function pageDevices() {
  let [net, nodes, groups, stats] = await Promise.all(['/network', '/nodes', '/groups', '/stats'].map(p => get(p).catch(() => null)));
  nodes = arr(nodes); groups = arr(groups);
  const tOptions = targetOptions(nodes, groups);
  const root = h('div', { class: 'stack' });
  const patches = new Map(); // несохранённые правки: id → поля; переживают фоновое обновление

  const reload = async () => {
    net = await get('/network');
    for (const d of net.devices) if (patches.has(d.id)) Object.assign(d, patches.get(d.id));
    state.counts.devices = net.devices.filter(isClient).length;
    draw();
  };
  const stageDevice = (d, patch) => {
    Object.assign(d, patch);
    patches.set(d.id, { ...patches.get(d.id), ...patch });
    stage('dev:' + d.id, () => api('PUT', '/devices/' + d.id, stripView(d)));
  };

  const isNewDev = d => {
    const first = Math.min(...net.devices.map(x => x.firstSeen || Infinity));
    return d.firstSeen && d.firstSeen - first > 3600 && Date.now() / 1000 - d.firstSeen < 86400;
  };
  const filters = { clients: isClient, all: () => true, online: d => d.online, own: d => !!d.route, new: isNewDev };

  const scan = async btn => {
    btn.classList.add('busy');
    try {
      const r = await api('POST', '/network/scan');
      toast(`Найдено устройств: ${r.found}` + (r.added ? `, новых: ${r.added}` : '') + (r.removed ? `, удалено забытых: ${r.removed}` : ''), 'ok');
      await reload();
    } catch (e) { toast(e.message, 'err'); }
    btn.classList.remove('busy');
  };
  const cleanup = () => confirmModal('Удалить устройства, которых сейчас нет в сети, которые не пользовались NetPilot 30 дней и ничем не настроены? Переименованные, закреплённые и устройства из правил останутся.', async () => {
    const r = await api('POST', '/devices/cleanup');
    toast(r.removed ? `Удалено устройств: ${r.removed}` : 'Удалять нечего', 'ok');
    await reload();
  }, 'Удалить');

  const routeSelect = d => {
    const sel = select([...ROUTES.map(([v, l]) => [v, l])], d.route || '', v => {
      stageDevice(d, { route: v, routeTarget: v === 'proxy' ? d.routeTarget || '' : '' });
      sel.className = 'sel sm' + (v ? ' r-' + v : '');
      draw();
    }, { class: 'sel sm' + (d.route ? ' r-' + d.route : ''), 'aria-label': 'Маршрут ' + d.name, onclick: e => e.stopPropagation() });
    return sel;
  };

  function draw() {
    const devs = net.devices;
    for (const id of devState.selected) if (!devs.some(d => d.id === id)) devState.selected.delete(id);
    const count = f => devs.filter(filters[f]).length;
    const q = devState.query.toLowerCase();
    const list = devs.filter(filters[devState.filter] || isClient)
      .filter(d => !q || [d.name, d.ip, d.mac, d.hostname, d.vendor].some(x => (x || '').toLowerCase().includes(q)))
      .sort((a, b) => (b.online - a.online) || ((b.downRate + b.upRate) - (a.downRate + a.upRate)) || ipKey(a.ip) - ipKey(b.ip));

    const search = h('input', { type: 'search', placeholder: 'Имя, IP или MAC', value: devState.query, 'aria-label': 'Поиск устройства',
      oninput: e => { devState.query = e.target.value; drawTable(); } });
    const toolbar = h('div', { class: 'ch', style: 'gap:10px' },
      seg([['clients', 'NetPilot', count('clients')], ['all', 'Вся сеть', count('all')]], ['clients', 'all'].includes(devState.filter) ? devState.filter : '', v => setFilter(v)),
      h('div', { class: 'opts hide-m' },
        [['online', 'В сети'], ['own', 'Свой маршрут'], ['new', 'Новые']].map(([f, l]) => h('button', { class: 'opt' + (devState.filter === f ? ' on' : ''), onclick: () => setFilter(devState.filter === f ? 'clients' : f) }, `${l} · ${count(f)}`))),
      h('span', { class: 'grow hide-m' }),
      h('label', { class: 'search', style: 'flex:1 1 220px;width:auto;max-width:320px;border-color:var(--line)' }, icon('search', 's'), search));

    // Массовые действия встают на место панели фильтров.
    let bulkRoute = 'proxy';
    const bulkbar = h('div', { class: 'ch', style: 'gap:10px;background:var(--accs)' },
      h('span', { class: 'strong' }, `Выбрано: ${devState.selected.size}`),
      select(ROUTES, bulkRoute, v => bulkRoute = v, { class: 'sel sm', style: 'width:auto' }),
      h('button', { class: 'btn sm p', onclick: () => { for (const id of devState.selected) { const d = devs.find(x => x.id === id); if (d) stageDevice(d, { route: bulkRoute, routeTarget: '' }); } devState.selected.clear(); draw(); } }, 'Назначить маршрут'),
      h('span', { class: 'grow' }),
      h('button', { class: 'ib', 'aria-label': 'Закрепить IP', title: 'Закрепить IP', onclick: () => { for (const id of devState.selected) { const d = devs.find(x => x.id === id); if (d && d.ip && d.mac) stageDevice(d, { pinned: true }); } devState.selected.clear(); draw(); } }, icon('lock')),
      h('button', { class: 'ib', 'aria-label': 'Снять выделение', onclick: () => { devState.selected.clear(); draw(); } }, icon('close')));

    const tableBox = h('div', { class: 'list' });
    function drawTable() {
      const qq = devState.query.toLowerCase();
      const rows = list.filter(d => !qq || [d.name, d.ip, d.mac, d.hostname, d.vendor].some(x => (x || '').toLowerCase().includes(qq)));
      if (!devs.length) { fill(tableBox, empty('devices', 'Устройства не найдены', 'Нажмите «Найти в сети» или добавьте устройство вручную.')); return; }
      if (!rows.length && devState.filter === 'clients' && !qq) {
        fill(tableBox, empty('devices', 'Пока никто не пользуется NetPilot', 'Здесь появятся устройства, которые пускают трафик через NetPilot. Остальные — в «Вся сеть».',
          h('button', { class: 'btn', onclick: () => setFilter('all') }, 'Показать всю сеть')));
        return;
      }
      if (!rows.length) { fill(tableBox, empty('search', 'Ничего не найдено', 'Измените фильтр или строку поиска.')); return; }
      fill(tableBox, ...rows.map(d => {
        const sel = devState.selected.has(d.id);
        const sub = [d.vendor && !d.name.startsWith(d.vendor) ? d.vendor : typeName(d.type), !d.online && (d.lastSeen ? 'не в сети, ' + ago(d.lastSeen) : 'не в сети')].filter(Boolean).join(' · ');
        return h('div', { class: 'row click' + (sel ? ' picked' : '') + (d.online ? '' : ' off'), onclick: () => openDevice(d) },
          h('input', { type: 'checkbox', class: 'check hide-m', checked: sel, 'aria-label': 'Выбрать ' + d.name, onclick: e => e.stopPropagation(),
            onchange: e => { e.target.checked ? devState.selected.add(d.id) : devState.selected.delete(d.id); draw(); } }),
          h('span', { class: 'ico' }, icon(d.type || 'other'), d.online && h('span', { class: 'on' })),
          h('span', { class: 'grow' }, h('span', { class: 'name' }, d.name), isNewDev(d) && h('span', { class: 'chip acc', style: 'margin-left:8px' }, 'новое'),
            h('br'), h('span', { class: 'meta' }, sub)),
          h('span', { class: 'col hide-m', style: 'width:170px' }, h('span', { class: 'mono' }, d.ip || '—'), h('br'),
            h('span', { class: 'meta' }, d.pinned ? [icon('lock', 's'), ' IP закреплён'] : d.randomMac ? 'случайный MAC' : d.mac || '')),
          h('span', { class: 'col mono hide-t', style: 'width:150px' }, d.downRate > 1024 ? `↓ ${mbps(d.downRate)} МБ/с` : h('span', { class: 'hint' }, '—')),
          h('span', { class: 'col m-full', style: 'width:200px' }, routeSelect(d),
            d.route === 'proxy' && h('div', { class: 'meta', style: 'margin-top:3px' }, 'через ' + (d.routeTarget ? targetName(d.routeTarget, nodes, groups) : 'активный сервер'))),
          h('span', { class: 'col hide-m', style: 'color:var(--faint)' }, icon('right')));
      }));
    }
    drawTable();

    fill(root, 
      pageHead('Устройства', devState.filter === 'all' ? `Вся локальная сеть · ${plural(devs.length, 'устройство', 'устройства', 'устройств')}` : `Те, кто ходит через NetPilot или настроен вручную · во всей сети — ${plural(devs.length, 'устройство', 'устройства', 'устройств')}`,
        h('button', { class: 'btn g', onclick: cleanup }, icon('trash', 's'), 'Убрать забытые'),
        h('button', { class: 'btn', onclick: e => scan(e.currentTarget) }, icon('refresh', 's'), 'Найти в сети'),
        h('button', { class: 'btn p', onclick: () => openDevice() }, icon('plus', 's'), 'Добавить')),
      arr(net.warnings).map(w => notice('', 'alert', w)),
      h('section', { class: 'card', style: 'overflow:hidden' }, devState.selected.size ? bulkbar : toolbar,
        h('div', { class: 'thead' }, h('span', { style: 'width:18px' }), h('span', { class: 'grow', style: 'padding-left:52px' }, 'Устройство'), h('span', { class: 'col', style: 'width:170px' }, 'Адрес'),
          h('span', { class: 'col hide-t', style: 'width:150px' }, 'Трафик сейчас'), h('span', { class: 'col', style: 'width:200px' }, 'Маршрут'), h('span', { class: 'col', style: 'width:18px' })),
        tableBox));
  }

  function setFilter(f) {
    devState.filter = f;
    try { localStorage.setItem('np-dev-filter', f); } catch {}
    draw();
  }

  // ----- карточка устройства -----
  function openDevice(orig) {
    const isNew = !orig;
    const d = stripView(structuredClone(orig || { name: '', type: 'other', ip: '', mac: '', tags: [], pinned: false, route: '', routeTarget: '' }));
    d.tags = arr(d.tags);
    const today = stats && orig && stats.today.devices ? stats.today.devices[orig.ip] : null;
    const targetBox = field('Через какой сервер', select(tOptions, d.routeTarget || '', v => d.routeTarget = v));
    targetBox.hidden = d.route !== 'proxy';
    const tagInput = h('input', { class: 'inp', value: d.tags.join(', '), placeholder: 'Семья, Гости', oninput: e => d.tags = lines(e.target.value) });
    const pinNote = h('span', { class: 'hint' }, d.ip ? `Роутер всегда будет выдавать ${d.ip}` : 'Нужны IP и MAC-адрес');
    modal({
      drawer: true, title: isNew ? 'Новое устройство' : d.name,
      head: h('div', { style: 'display:flex;align-items:center;gap:14px;flex-grow:1;min-width:0' },
        h('span', { class: 'ico big' }, icon(d.type || 'other', 'l'), orig && orig.online && h('span', { class: 'on' })),
        h('div', { class: 'grow' }, h('h2', { class: 'mt ell' }, isNew ? 'Новое устройство' : d.name),
          !isNew && h('div', { class: 'hint' }, [typeName(d.type), d.vendor, orig.online ? 'в сети' : orig.lastSeen ? 'не в сети, ' + ago(orig.lastSeen) : 'не в сети'].filter(Boolean).join(' · ')))),
      body: [
        !isNew && h('div', { class: 'grid3 keep', style: 'gap:10px' },
          h('div', { class: 'card', style: 'padding:12px 14px' }, h('div', { class: 'hint' }, 'Сейчас'), h('div', { class: 'strong', style: 'font-size:18px' }, orig.downRate > 1024 ? mbps(orig.downRate) + ' МБ/с' : '—')),
          h('div', { class: 'card', style: 'padding:12px 14px' }, h('div', { class: 'hint' }, 'Сегодня'), h('div', { class: 'strong', style: 'font-size:18px' }, today ? bytes(today[0] + today[1]) : '—')),
          h('div', { class: 'card', style: 'padding:12px 14px' }, h('div', { class: 'hint' }, 'Соединений'), h('div', { class: 'strong', style: 'font-size:18px' }, orig.conns || 0))),
        field('Маршрут всего трафика', seg(ROUTES.map(([v, l]) => [v, l]), d.route || '', v => { d.route = v; targetBox.hidden = v !== 'proxy'; }, 'fill r4'),
          '«По правилам» — решают правила. Свой маршрут важнее правил.'),
        targetBox,
        h('div', { class: 'grid2', style: 'gap:12px' },
          field('Название', h('input', { class: 'inp', value: d.name, placeholder: 'Ноутбук бухгалтерии', oninput: e => d.name = e.target.value })),
          field('Тип', select(DEVICE_TYPES, d.type || 'other', v => d.type = v))),
        h('div', { class: 'grid2', style: 'gap:12px' },
          field('IP-адрес', h('input', { class: 'inp mono', value: d.ip, placeholder: '192.168.0.20', oninput: e => d.ip = e.target.value.trim() })),
          field('MAC-адрес', h('input', { class: 'inp mono', value: d.mac, placeholder: 'aa:bb:cc:dd:ee:ff', oninput: e => d.mac = e.target.value.trim() }))),
        h('div', { style: 'display:flex;align-items:center;gap:12px' }, sw(d.pinned, v => d.pinned = v, null), h('span', { class: 'grow' }, h('strong', {}, 'Закрепить IP-адрес'), h('br'), pinNote)),
        field('Группы устройства', tagInput, 'Через запятую. Группой удобно назначать правило сразу нескольким устройствам.'),
        !isNew && h('div', { class: 'card', style: 'padding:4px 16px' },
          d.hostname && h('div', { class: 'kv' }, h('span', { class: 'hint' }, 'Имя в сети'), h('span', {}, d.hostname)),
          h('div', { class: 'kv' }, h('span', { class: 'hint' }, 'MAC'), h('span', { class: 'mono' }, (d.mac || '—') + (orig.randomMac ? ' · случайный' : ''))),
          d.firstSeen && h('div', { class: 'kv' }, h('span', { class: 'hint' }, 'Впервые в сети'), h('span', {}, new Date(d.firstSeen * 1000).toLocaleString('ru-RU', { day: 'numeric', month: 'long', hour: '2-digit', minute: '2-digit' })))),
      ],
      footer: !isNew && h('button', { class: 'btn d', style: 'margin-right:auto', onclick: () => confirmModal(`Удалить устройство «${d.name}»? При следующем сканировании оно появится снова как новое.`, async () => {
        await api('DELETE', '/devices/' + d.id);
        document.querySelectorAll('.drawer-bg').forEach(x => x.remove());
        reload();
      }) }, icon('trash', 's'), 'Удалить'),
      actions: [{ label: 'Сохранить', primary: true, onClick: async () => {
        if (!d.name.trim()) throw new Error('Укажите название');
        if (d.pinned && (!d.ip || !d.mac)) throw new Error('Чтобы закрепить IP, нужны IP и MAC-адрес');
        if (d.route !== 'proxy') d.routeTarget = '';
        await (isNew ? api('POST', '/devices', d) : api('PUT', '/devices/' + d.id, d));
        if (!isNew) { patches.delete(d.id); pending.delete('dev:' + d.id); refreshSaveBar(); }
        toast('Устройство сохранено', 'ok');
        await reload();
      } }],
    });
  }

  draw();
  router.timer = setInterval(() => {
    const a = document.activeElement;
    if (document.querySelector('.modal-bg') || patches.size || (a && root.contains(a) && /INPUT|SELECT/.test(a.tagName))) return;
    reload().catch(() => {});
  }, 15000);
  return root;
}
