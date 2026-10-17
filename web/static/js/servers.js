// Серверы: группы, подписки, список серверов с проверкой по одному
// и выбор активного сервера.

import { pingMethodName } from './settings.js';
import {
  h, fill, icon, api, get, toast, modal, confirmModal, state, stage, router, route, field, seg, opt, select,
  card, pageHead, empty, notice, arr, ago, latencyEl, groupMembers, toggleIn, plural, regexError,
} from './core.js';
import { refreshActive } from './app.js';

// removeChecked удаляет сервер, группу или подписку. Если объект где-то выбран,
// API отвечает 409 со списком мест; тогда показываем его и удаляем только после
// второго подтверждения (?force=1) — сервер сам переключит эти места на «по умолчанию».
async function removeChecked(method, path, done) {
  try {
    await api(method, path);
  } catch (e) {
    if (e.status !== 409) throw e;
    modal({
      title: 'Используется',
      body: h('div', {},
        h('p', { style: 'margin:0 0 8px' }, 'Удаляемое выбрано здесь:'),
        h('ul', { style: 'margin:0 0 8px;padding-left:20px' }, arr(e.data && e.data.references).map(r => h('li', {}, r))),
        h('p', { class: 'hint', style: 'margin:0' }, 'После удаления эти правила, устройства и настройки пойдут через сервер по умолчанию, а из групп удалённое исчезнет.')),
      actions: [{ label: 'Удалить всё равно', danger: true, onClick: async () => { await api(method, path + '?force=1'); await done(); } }],
    });
    return;
  }
  await done();
}

// ---------- выбор активного сервера ----------

// openPicker — окно «Активный сервер»: группы, быстрые серверы, все серверы.
export async function openPicker(onSaved) {
  const [settings, nodes, groups, auto] = await Promise.all(['/settings', '/nodes', '/groups', '/auto'].map(p => get(p).catch(() => null)));
  if (!settings) return;
  let picked = settings.defaultTarget || '';
  let tab = 'groups', q = '';
  const lat = Object.assign({}, state.latency, (auto && auto.latency) || {});
  // Прокручивается сам список: поиск и вкладки над ним остаются на месте.
  const list = h('div', { class: 'card list scroll' });
  const autoOf = id => auto && arr(auto.groups).find(g => g.groupId === id && (id || g.name === 'Авто'));
  const latOf = id => { const l = lat[id]; return l && l.ms ? l : null; };

  const row = (id, title, sub, right) => h('button', {
    type: 'button', class: 'row' + (picked === id ? ' picked' : ''), style: 'width:100%;border-left:0;border-right:0;border-bottom:0;background:' + (picked === id ? 'var(--rowsel)' : 'transparent') + ';text-align:left',
    onclick: () => { picked = id; draw(); },
  }, h('span', { class: 'radio' + (picked === id ? ' on' : '') }), h('span', { class: 'grow' }, h('span', { class: 'name' }, title), sub && [h('br'), h('span', { class: 'meta' }, sub)]), right);

  const draw = () => {
    const rows = [];
    const qq = q.toLowerCase();
    if (tab === 'groups') {
      const a = autoOf('');
      if (arr(nodes).length > 1 && (!qq || 'авто'.includes(qq))) {
        const cur = a && arr(nodes).find(n => n.id === a.selected);
        rows.push(row('', 'Авто — все серверы', `${plural(nodes.length, 'сервер', 'сервера', 'серверов')}${cur ? ' · сейчас ' + cur.name : ''}`, picked === '' && h('span', { class: 'chip acc' }, 'активный')));
      }
      for (const g of arr(groups).filter(g => !qq || g.name.toLowerCase().includes(qq))) {
        const ag = autoOf(g.id), m = groupMembers(g, arr(nodes));
        const cur = ag && arr(nodes).find(n => n.id === ag.selected);
        rows.push(row(g.id, g.name, [plural(m.length, 'сервер', 'сервера', 'серверов'), g.exclude && 'кроме ' + g.exclude, cur && 'сейчас ' + cur.name].filter(Boolean).join(' · '),
          ag && ag.ms ? latencyEl({ ms: ag.ms, method: 'http' }) : settings.defaultTarget === g.id ? h('span', { class: 'chip acc' }, 'активный') : null));
      }
      if (!rows.length) rows.push(empty('groups', 'Групп нет', 'Создайте группу на странице «Серверы» — например, «Европа» с автовыбором.'));
    } else {
      let ns = arr(nodes).filter(n => !qq || n.name.toLowerCase().includes(qq) || n.protocol.includes(qq));
      if (tab === 'fast') ns = ns.filter(n => latOf(n.id) && latOf(n.id).ms > 0).sort((x, y) => latOf(x.id).ms - latOf(y.id).ms);
      ns.slice(0, 60).forEach(n => rows.push(row(n.id, n.name, n.protocol + (n.security && n.security !== 'none' ? ' · ' + n.security : ''), latencyEl(latOf(n.id)))));
      if (!rows.length) rows.push(empty('servers', tab === 'fast' ? 'Нет замеров' : 'Ничего не найдено', tab === 'fast' ? 'Проверьте серверы на странице «Серверы».' : ''));
      if (ns.length > 60) rows.push(h('div', { class: 'row hint' }, `Показаны 60 из ${ns.length} — уточните поиск`));
    }
    fill(list, ...rows);
  };
  const search = h('input', { type: 'search', placeholder: 'Страна, название или протокол', 'aria-label': 'Поиск сервера', oninput: e => { q = e.target.value; draw(); } });
  draw();
  modal({
    title: 'Активный сервер', size: 'mid',
    body: [
      h('div', { class: 'hint', style: 'margin-top:-6px' }, 'Через него идёт всё, что по правилам «через прокси», если в правиле или у устройства не выбран другой.'),
      h('label', { class: 'search', style: 'width:100%;border-color:var(--line)' }, icon('search', 's'), search),
      seg([['groups', 'Группы'], ['fast', 'Быстрые'], ['all', 'Все серверы']], tab, v => { tab = v; draw(); }, 'fill'),
      list,
    ],
    actions: [{ label: 'Сделать активным', primary: true, onClick: async () => {
      if (picked === (settings.defaultTarget || '')) return;
      const cur = await get('/settings');
      await api('PUT', '/settings', { ...cur, defaultTarget: picked });
      toast('Активный сервер изменён', 'ok');
      refreshActive(); // кнопка наверху — откуда бы ни меняли
      if (onSaved) onSaved();
    } }],
  });
}

// ---------- интервал автообновления подписки ----------

const INTERVALS = [['0', 'Вручную'], ['1', 'Каждый час'], ['3', 'Каждые 3 часа'], ['6', 'Каждые 6 часов'], ['12', 'Каждые 12 часов'],
  ['24', 'Раз в сутки'], ['72', 'Раз в 3 дня'], ['168', 'Раз в неделю'], ['custom', 'Свой интервал…']];

function intervalControl(hours, onChange) {
  const isPreset = INTERVALS.some(([v]) => v === String(hours));
  const num = h('input', { class: 'inp', type: 'number', min: 1, max: 720, value: hours || 24, 'aria-label': 'Часов' });
  const custom = h('span', { class: 'interval', hidden: isPreset }, num, h('span', { class: 'hint' }, 'ч'));
  num.addEventListener('change', () => {
    const v = Math.round(+num.value);
    if (!(v >= 1 && v <= 720)) { num.classList.add('invalid'); toast('Интервал — от 1 до 720 часов', 'warn'); return; }
    num.classList.remove('invalid');
    onChange(v);
  });
  const sel = select(INTERVALS, isPreset ? String(hours) : 'custom', v => {
    if (v === 'custom') { custom.hidden = false; num.focus(); return; }
    custom.hidden = true;
    onChange(+v);
  });
  return h('div', { class: 'interval' }, sel, custom);
}

// ---------- страница ----------

export async function pageServers() {
  const [nodes, subs, groups, settings, auto] = await Promise.all(['/nodes', '/subscriptions', '/groups', '/settings', '/auto'].map(p => get(p).catch(() => null)));
  if (auto) for (const [id, l] of Object.entries(auto.latency || {})) if (!state.latency[id]) state.latency[id] = l;
  const autoOf = id => auto && arr(auto.groups).find(g => g.groupId === id);
  const nodeName = id => (nodes.find(n => n.id === id) || {}).name;
  const subName = id => (subs.find(s => s.id === id) || {}).name;

  // ----- проверка по одному -----
  let run = null;
  const progress = h('div', { class: 'card', hidden: true, style: 'display:flex;align-items:center;gap:16px;padding:14px 20px;background:var(--accbg);border-color:var(--accl);flex-wrap:wrap' });
  const testBtn = h('button', { class: 'btn', disabled: !nodes.length });
  const drawRun = () => {
    fill(testBtn, ...(run ? [icon('stop', 's'), 'Остановить'] : [icon('test', 's'), 'Проверить все']));
    testBtn.classList.toggle('d', !!run);
    progress.hidden = !run;
    if (run) {
      const ok = nodes.filter(n => state.latency[n.id] && state.latency[n.id].ms > 0).length;
      fill(progress, h('span', { class: 'ring' }), h('span', { class: 'strong', style: 'color:var(--acc2)' }, `Проверка по одному: ${run.done} из ${run.total}`),
        h('div', { class: 'progress grow', style: 'min-width:120px' }, h('span', { style: `width:${Math.round(run.done / run.total * 100)}%` })),
        h('span', { class: 'hint' }, `отвечают ${ok}`, run.cur ? [' · сейчас ', h('strong', { style: 'color:var(--ink)' }, run.cur)] : ''));
    }
  };
  const setLat = id => { const el = listBox.querySelector(`[data-lat="${id}"]`); if (el) fill(el, latencyEl(state.latency[id])); };
  const testOne = async id => {
    state.latency[id] = 'wait';
    setLat(id);
    try { const [l] = await api('POST', '/nodes/test', { ids: [id] }); state.latency[id] = l; }
    catch (e) { delete state.latency[id]; setLat(id); toast(e.message, 'err'); return false; }
    setLat(id);
    return true;
  };
  testBtn.onclick = async () => {
    if (run) { run.stop = true; return; }
    const ids = visible().map(n => n.id);
    const me = run = { stop: false, done: 0, total: ids.length };
    drawRun();
    for (const id of ids) {
      if (me.stop || !document.body.contains(listBox)) break;
      me.cur = nodeName(id);
      drawRun();
      if (!(await testOne(id))) break;
      me.done++;
    }
    const ok = ids.filter(id => state.latency[id] && state.latency[id].ms > 0).length;
    if (document.body.contains(listBox)) toast(`Проверено: ${me.done} из ${me.total}, отвечают: ${ok}`, 'ok', { title: me.stop ? 'Проверка остановлена' : 'Проверка завершена' });
    run = null;
    drawRun();
  };
  drawRun();

  // ----- список серверов -----
  let filter = 'all', q = '';
  const visible = () => nodes.filter(n => {
    if (q && !(n.name.toLowerCase().includes(q) || n.server.includes(q) || n.protocol.includes(q))) return false;
    if (filter === 'ok') return state.latency[n.id] && state.latency[n.id].ms > 0;
    if (filter === 'nolte') return !/lte/i.test(n.name);
    return true;
  });
  const listBox = h('div', { class: 'list' });
  let shown = 80;
  const drawList = () => {
    const vs = visible();
    const sel = new Set(arr(auto && auto.groups).map(g => g.selected).filter(Boolean));
    fill(listBox, ...(vs.length ? [
      ...vs.slice(0, shown).map(n => h('div', { class: 'row' + (sel.has(n.id) ? ' picked' : '') },
        h('span', { class: 'grow' }, h('span', { class: 'name' }, n.name),
          sel.has(n.id) ? [h('br'), h('span', { style: 'font-size:12.5px;color:var(--acc2);font-weight:500' }, 'работает в группе')]
            : n.subscriptionId ? [h('br'), h('span', { class: 'meta' }, subName(n.subscriptionId))] : null),
        h('span', { class: 'col hide-m', style: 'width:150px' }, h('span', { class: 'tag' }, [n.protocol, n.security && n.security !== 'none' ? n.security : n.transport && n.transport !== 'tcp' ? n.transport : ''].filter(Boolean).join(' · '))),
        h('span', { class: 'col', style: 'width:150px', 'data-lat': n.id }, latencyEl(state.latency[n.id])),
        h('span', { class: 'col acts', style: 'width:116px' },
          h('button', { class: 'ib', 'aria-label': 'Проверить', title: 'Проверить', onclick: () => testOne(n.id) }, icon('test', 's')),
          h('button', { class: 'ib hide-m', 'aria-label': 'Изменить', title: 'Изменить', onclick: () => editNode(n) }, icon('edit', 's')),
          h('button', { class: 'ib danger hide-m', 'aria-label': 'Удалить', title: 'Удалить', onclick: () => confirmModal(`Удалить сервер «${n.name}»?`, () => removeChecked('DELETE', '/nodes/' + n.id, () => { state.refreshCounts && state.refreshCounts(); refreshActive(); route(); })) }, icon('trash', 's'))))),
      vs.length > shown && h('button', { class: 'row', style: 'width:100%;border-left:0;border-right:0;border-bottom:0;background:transparent;justify-content:center;color:var(--acc2);font-weight:600', onclick: () => { shown += 200; drawList(); } }, `Показать ещё ${Math.min(200, vs.length - shown)} из ${vs.length - shown}`),
    ] : [empty('servers', nodes.length ? 'Ничего не найдено' : 'Серверов нет', nodes.length ? 'Измените фильтр или поиск.' : 'Импортируйте ссылки или добавьте подписку провайдера.',
      !nodes.length && h('button', { class: 'btn p', onclick: importModal }, icon('import', 's'), 'Добавить серверы'))]));
  };
  drawList();
  const counts = { ok: () => nodes.filter(n => state.latency[n.id] && state.latency[n.id].ms > 0).length };
  const listCard = h('section', { class: 'card tbl', style: 'overflow:hidden' },
    h('div', { class: 'ch', style: 'gap:10px' },
      h('label', { class: 'search', style: 'flex:1 1 220px;width:auto;border-color:var(--line)' }, icon('search', 's'),
        h('input', { type: 'search', placeholder: 'Страна, название, протокол', 'aria-label': 'Поиск сервера', oninput: e => { q = e.target.value.toLowerCase(); shown = 80; drawList(); } })),
      seg([['all', 'Все', nodes.length], ['ok', 'Отвечают', counts.ok()], ['nolte', 'Без LTE']], filter, v => { filter = v; shown = 80; drawList(); })),
    h('div', { class: 'thead' }, h('span', { class: 'grow' }, 'Сервер'), h('span', { class: 'col', style: 'width:150px' }, 'Протокол'), h('span', { class: 'col', style: 'width:150px' }, 'Задержка'), h('span', { class: 'col', style: 'width:116px' })),
    listBox);

  // ----- группы и подписки -----
  const groupCard = g => {
    const m = groupMembers(g, nodes), ag = autoOf(g.id), cur = ag && nodeName(ag.selected);
    const isActive = settings.defaultTarget === g.id;
    const manual = g.type === 'select';
    return h('article', { class: 'card' + (isActive ? ' picked' : ''), style: 'padding:16px 18px;display:flex;flex-direction:column;gap:10px' },
      h('div', { style: 'display:flex;align-items:center;gap:8px' }, h('span', { class: 'name grow', style: 'font-size:15.5px' }, g.name),
        isActive ? h('span', { class: 'chip acc' }, 'активный') : null,
        h('button', { class: 'ib', 'aria-label': 'Изменить группу', title: 'Изменить группу', onclick: () => editGroup(g) }, icon('edit', 's'))),
      g.filter && h('div', { class: 'hint ell' }, g.filter.split('|').join(' · ')),
      h('div', { style: 'display:flex;gap:6px;flex-wrap:wrap' },
        m.length ? h('span', { class: 'chip' }, plural(m.length, 'сервер', 'сервера', 'серверов')) : h('span', { class: 'chip warn' }, 'пустая'),
        g.exclude && h('span', { class: 'chip bad' }, 'кроме ' + g.exclude), h('span', { class: 'chip' }, manual ? 'выбор вручную' : 'автовыбор')),
      m.length > 0 && h('div', { style: 'display:flex;align-items:center;gap:8px;padding-top:10px;border-top:1px solid var(--line2);flex-wrap:wrap' },
        manual || (ag && ag.inUse)
          ? [h('span', { class: 'dot ' + (ag && ag.ms > 0 ? 'ok' : ag && ag.ms < 0 ? 'bad' : '') }),
            h('span', { class: 'grow' }, ag && ag.scanning ? `подбор: ${ag.scanning[0]} из ${ag.scanning[1]}` : ['Сейчас ', h('strong', {}, cur || 'выбирается')]),
            ag && ag.ms ? latencyEl({ ms: ag.ms, method: 'http' }) : null]
          : h('span', { class: 'hint grow' }, 'не используется — не проверяется'),
        h('button', { class: 'btn sm g', onclick: () => pickInGroup(g) }, 'Выбрать сервер'),
        !isActive && h('button', { class: 'btn sm', onclick: () => makeActive(g.id) }, 'Сделать активной')));
  };
  const makeActive = async id => {
    const cur = await get('/settings');
    await api('PUT', '/settings', { ...cur, defaultTarget: id });
    toast('Активный сервер изменён', 'ok');
    refreshActive();
    route();
  };

  const subCard = s => h('article', { class: 'card', style: 'padding:16px 18px;display:flex;flex-direction:column;gap:10px' },
    h('div', { style: 'display:flex;align-items:center;gap:8px' }, h('span', { class: 'name grow' }, s.name), s.lastError ? h('span', { class: 'chip bad' }, 'ошибка') : h('span', { class: 'chip ok' }, 'обновлена')),
    h('div', { class: 'hint' }, `${plural(nodes.filter(n => n.subscriptionId === s.id).length, 'сервер', 'сервера', 'серверов')} · ${ago(s.updatedAt)} · ` + (s.interval ? INTERVALS.find(x => x[0] === String(s.interval))?.[1]?.toLowerCase() || `каждые ${s.interval} ч` : 'вручную')),
    s.lastError && h('div', { class: 'hint', style: 'color:var(--bad)' }, s.lastError),
    h('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' },
      h('button', { class: 'btn sm', onclick: async e => { e.currentTarget.classList.add('busy'); await updateSub(s.id); route(); } }, icon('refresh', 's'), 'Обновить'),
      h('button', { class: 'btn sm g', onclick: () => subModal(s) }, 'Изменить'),
      h('button', { class: 'btn sm g', style: 'color:var(--bad)', onclick: () => confirmModal(`Удалить подписку «${s.name}» вместе с её серверами?`, () => removeChecked('POST', `/subscriptions/${s.id}/purge`, () => { refreshActive(); route(); })) }, 'Удалить')));

  const updateSub = async id => {
    try { const r = await api('POST', `/subscriptions/${id}/update`); toast(`Загружено серверов: ${r.nodes}`, 'ok', { title: 'Подписка обновлена' }); }
    catch (e) { toast(e.message, 'err', { title: 'Подписка не обновилась' }); }
  };

  // ----- окна -----
  function importModal() {
    const ta = h('textarea', { class: 'inp', rows: 8, placeholder: 'vless://…\nvmess://…\ntrojan://…\nss://…\nhysteria2://…\ntuic://…' });
    modal({ title: 'Добавить серверы', size: 'mid',
      body: [
        h('div', { class: 'grid2', style: 'gap:10px' },
          h('button', { class: 'choice on', type: 'button' }, h('span', { class: 'radio on' }), h('span', {}, h('strong', {}, 'Ссылки'), h('br'), h('span', { class: 'hint' }, 'vless://, vmess://, trojan://…'))),
          h('button', { class: 'choice', type: 'button', onclick: () => { document.querySelector('.modal-bg:last-child .mh .ib').click(); setTimeout(() => subModal(), 220); } }, h('span', { class: 'radio' }), h('span', {}, h('strong', {}, 'Подписка'), h('br'), h('span', { class: 'hint' }, 'адрес от провайдера')))),
        field('Ссылки на серверы, по одной в строке', ta, 'VLESS, VMess, Trojan, Shadowsocks, Hysteria2, TUIC. Можно вставить содержимое подписки в base64.')],
      actions: [{ label: 'Добавить', primary: true, onClick: async () => {
        const r = await api('POST', '/nodes/import', { text: ta.value });
        toast(`Добавлено серверов: ${r.added}`, r.added ? 'ok' : 'warn');
        arr(r.errors).slice(0, 3).forEach(e => toast(e, 'err'));
        state.refreshCounts && state.refreshCounts();
        route();
      } }] });
  }

  function subModal(s0) {
    const isNew = !s0;
    const name = h('input', { class: 'inp', placeholder: 'Основной провайдер', value: s0 ? s0.name : '' });
    const url = h('input', { class: 'inp mono', placeholder: 'https://…', value: s0 ? s0.url : '' });
    let interval = s0 ? s0.interval || 0 : 24;
    modal({ title: isNew ? 'Новая подписка' : 'Подписка', size: 'mid',
      body: [field('Название', name), field('Адрес подписки', url),
        field('Автообновление', intervalControl(interval, v => interval = v), 'Если список серверов изменился, конфигурация применяется сама; правила и группы сохраняются.')],
      actions: [{ label: isNew ? 'Добавить' : 'Сохранить', primary: true, onClick: async () => {
        if (!url.value.trim()) throw new Error('Укажите адрес подписки');
        const body = { ...(s0 || {}), name: name.value.trim() || 'Подписка', url: url.value.trim(), interval };
        if (isNew) { const sub = await api('POST', '/subscriptions', body); await updateSub(sub.id); }
        else { await api('PUT', '/subscriptions/' + s0.id, body); if (body.url !== s0.url) await updateSub(s0.id); toast('Подписка сохранена', 'ok'); }
        state.refreshCounts && state.refreshCounts();
        route();
      } }] });
  }

  function editNode(n) {
    const name = h('input', { class: 'inp', value: n.name });
    const server = h('input', { class: 'inp mono', value: n.server });
    const port = h('input', { class: 'inp', type: 'number', value: n.port });
    const { id, name: _n, server: _s, port: _p, ...rest } = n;
    const raw = h('textarea', { class: 'inp mono', rows: 12 }, JSON.stringify(rest, null, 2));
    modal({ title: 'Сервер', size: 'mid',
      body: [field('Название', name), h('div', { class: 'grid2', style: 'gap:12px' }, field('Адрес', server), field('Порт', port)),
        field('Параметры подключения', raw, 'Протокол, ключи, транспорт и TLS в формате JSON')],
      actions: [{ label: 'Сохранить', primary: true, onClick: async () => {
        let extra;
        try { extra = JSON.parse(raw.value); } catch { throw new Error('Ошибка в JSON'); }
        await api('PUT', '/nodes/' + id, { ...extra, name: name.value, server: server.value, port: +port.value });
        route();
      } }] });
  }

  // ----- редактор группы -----
  // В группу попадают отмеченные серверы и серверы, подошедшие под фильтр,
  // кроме исключённых. Ничего не отмечено и фильтр пуст — все серверы.
  function editGroup(g0) {
    const isNew = !g0;
    const g = structuredClone(g0 || { name: '', type: 'auto', nodeIds: [], filter: '', exclude: '' });
    g.nodeIds = arr(g.nodeIds).filter(id => nodes.some(n => n.id === id)); // удалённые серверы
    let q = '', show = 'all', shown = 150;
    const rx = src => { try { return src ? new RegExp(src, 'i') : null; } catch { return null; } };

    const preview = h('div', { class: 'card', style: 'padding:12px 14px;display:flex;flex-direction:column;gap:4px' });
    const filterErr = h('span', { class: 'hint', style: 'color:var(--bad)', hidden: true });
    const excludeErr = h('span', { class: 'hint', style: 'color:var(--bad)', hidden: true });
    const listBox = h('div', { class: 'card list', style: 'max-height:360px;overflow-y:auto' });
    const showSeg = h('div');

    const visible = () => {
      const ms = new Set(groupMembers(g, nodes).map(n => n.id));
      return nodes.filter(n => (!q || n.name.toLowerCase().includes(q) || n.server.includes(q) || n.protocol.includes(q))
        && (show === 'all' || (show === 'picked' ? g.nodeIds.includes(n.id) : ms.has(n.id))));
    };
    const drawList = () => {
      const ms = new Set(groupMembers(g, nodes).map(n => n.id));
      const re = rx(g.filter), ex = rx(g.exclude);
      const vs = visible();
      fill(listBox, ...(vs.length ? [
        vs.slice(0, shown).map(n => {
          const picked = g.nodeIds.includes(n.id);
          const why = ex && ex.test(n.name) ? h('span', { class: 'chip bad' }, 'исключён')
            : !picked && re && re.test(n.name) ? h('span', { class: 'chip acc' }, 'по фильтру')
            : ms.has(n.id) && !picked && !g.filter && !g.nodeIds.length ? h('span', { class: 'chip' }, 'все серверы') : null;
          return h('label', { class: 'row', style: 'min-height:46px;padding:6px 14px;cursor:pointer' },
            h('input', { type: 'checkbox', class: 'check', checked: picked, onchange: e => { g.nodeIds = toggleIn(g.nodeIds, n.id, e.target.checked); upd(); } }),
            h('span', { class: 'grow ell', style: ms.has(n.id) ? '' : 'color:var(--mut)' }, n.name),
            why, h('span', { class: 'meta hide-m' }, n.protocol), latencyEl(state.latency[n.id]));
        }),
        vs.length > shown && h('button', { type: 'button', class: 'row', style: 'width:100%;border-left:0;border-right:0;border-bottom:0;background:transparent;justify-content:center;color:var(--acc2);font-weight:600',
          onclick: () => { shown += 300; drawList(); } }, `Показать ещё ${Math.min(300, vs.length - shown)} из ${vs.length - shown}`),
      ] : [h('div', { class: 'row hint' }, 'Ничего не найдено')]));
    };
    const upd = () => {
      const fe = regexError(g.filter), ee = regexError(g.exclude);
      filterErr.hidden = !fe; filterErr.textContent = fe;
      excludeErr.hidden = !ee; excludeErr.textContent = ee;
      const m = groupMembers(g, nodes);
      const re = rx(g.filter), ex = rx(g.exclude);
      const byFilter = re ? nodes.filter(n => re.test(n.name) && !g.nodeIds.includes(n.id) && !(ex && ex.test(n.name))).length : 0;
      const excluded = ex ? nodes.filter(n => ex.test(n.name)).length : 0;
      fill(preview,
        h('strong', {}, m.length ? `В группе ${plural(m.length, 'сервер', 'сервера', 'серверов')}` : 'Группа пустая'),
        h('div', { class: 'hint' }, !g.nodeIds.length && !g.filter ? 'Ничего не отмечено и фильтр пуст — в группе все серверы' + (excluded ? `, кроме исключённых (${excluded})` : '')
          : [g.nodeIds.length && `отмечено: ${g.nodeIds.length}`, byFilter && `по фильтру: ${byFilter}`, excluded && `исключено: ${excluded}`].filter(Boolean).join(' · ')),
        !m.length && h('div', { class: 'hint', style: 'color:var(--warn)' }, 'Пустая группа в ядро не попадёт: правила и устройства с ней пойдут через активный сервер.'));
      fill(showSeg, seg([['all', 'Все', nodes.length], ['picked', 'Отмеченные', g.nodeIds.length], ['members', 'В группе', m.length]], show, v => { show = v; shown = 150; drawList(); }));
      drawList();
    };
    upd();

    const search = h('input', { type: 'search', placeholder: 'Найти сервер: страна, название, протокол', 'aria-label': 'Поиск сервера',
      oninput: e => { q = e.target.value.toLowerCase(); shown = 150; drawList(); } });
    const markAll = on => {
      const ids = visible().map(n => n.id);
      g.nodeIds = on ? [...new Set([...g.nodeIds, ...ids])] : g.nodeIds.filter(id => !ids.includes(id));
      upd();
    };

    modal({ title: isNew ? 'Новая группа' : 'Группа', size: 'wide',
      body: [
        h('div', { class: 'grid2', style: 'gap:16px;align-items:start' },
          field('Название', h('input', { class: 'inp', value: g.name, placeholder: 'Европа', oninput: e => g.name = e.target.value })),
          field('Выбор сервера в группе', seg([['auto', 'Автоматически'], ['select', 'Вручную']], g.type, v => g.type = v, 'fill'),
            'Автоматически — NetPilot проверяет серверы по одному и держится за рабочий. Вручную — сервер выбираете вы на странице «Серверы».')),
        h('div', { class: 'fld' }, h('span', { class: 'lbl' }, 'Серверы группы'),
          h('div', { style: 'display:flex;gap:10px;flex-wrap:wrap;align-items:center' },
            h('label', { class: 'search', style: 'flex:1 1 260px;width:auto;border-color:var(--line)' }, icon('search', 's'), search),
            showSeg),
          h('div', { style: 'display:flex;gap:8px;flex-wrap:wrap' },
            h('button', { type: 'button', class: 'btn sm', onclick: () => markAll(true) }, icon('check', 's'), 'Отметить найденные'),
            h('button', { type: 'button', class: 'btn sm g', onclick: () => markAll(false) }, 'Снять отметки с найденных')),
          listBox),
        h('div', { class: 'grid2', style: 'gap:16px;align-items:start' },
          h('div', { class: 'fld' }, h('span', { class: 'lbl' }, 'И по названию (фильтр)'),
            h('input', { class: 'inp mono', value: g.filter, placeholder: 'Германия|Нидерланды', oninput: e => { g.filter = e.target.value; upd(); } }),
            h('span', { class: 'hint' }, 'Необязательно. Новые серверы из подписок с таким названием попадут в группу сами.'), filterErr),
          h('div', { class: 'fld' }, h('span', { class: 'lbl' }, 'Исключить по названию'),
            h('input', { class: 'inp mono', value: g.exclude || '', placeholder: 'LTE', oninput: e => { g.exclude = e.target.value; upd(); } }),
            h('span', { class: 'hint' }, 'Эти серверы не попадут в группу, даже если отмечены или подошли по фильтру.'), excludeErr)),
        preview],
      footer: !isNew && h('button', { class: 'btn d', style: 'margin-right:auto', onclick: () => deleteGroup(g) }, icon('trash', 's'), 'Удалить'),
      actions: [{ label: isNew ? 'Создать группу' : 'Сохранить', primary: true, onClick: async () => {
        if (!g.name.trim()) throw new Error('Укажите название группы');
        const err = regexError(g.filter) || regexError(g.exclude);
        if (err) throw new Error(err);
        await (isNew ? api('POST', '/groups', g) : api('PUT', '/groups/' + g.id, g));
        toast(isNew ? 'Группа создана. Чтобы она заработала, сделайте её активным сервером или укажите в правиле.' : 'Группа сохранена', 'ok');
        route();
      } }] });
    setTimeout(() => search.focus(), 50);
  }

  // Удаление группы: где она используется, сообщит API (409), а с force=1
  // сам сбросит ссылки на неё, включая активный сервер.
  function deleteGroup(g) {
    confirmModal(`Удалить группу «${g.name}»?`, () => removeChecked('DELETE', '/groups/' + g.id, () => {
      document.querySelectorAll('#modal-root .modal-bg').forEach(m => m.remove());
      refreshActive();
      route();
    }));
  }

  // Выбор сервера внутри группы: у ручной — единственный способ, у автоматической
  // NetPilot держится за выбранный, пока тот отвечает.
  function pickInGroup(g) {
    const ag = autoOf(g.id);
    const members = groupMembers(g, nodes);
    let picked = (ag && ag.selected) || (members[0] && members[0].id) || '';
    let q = '';
    const list = h('div', { class: 'card list', style: 'max-height:420px;overflow-y:auto' });
    const draw = () => {
      const vs = members.filter(n => !q || n.name.toLowerCase().includes(q) || n.protocol.includes(q));
      fill(list, ...(vs.length ? vs.slice(0, 300).map(n => h('button', {
        type: 'button', class: 'row' + (picked === n.id ? ' picked' : ''), style: 'width:100%;border-left:0;border-right:0;border-bottom:0;background:' + (picked === n.id ? 'var(--rowsel)' : 'transparent') + ';text-align:left',
        onclick: () => { picked = n.id; draw(); },
      }, h('span', { class: 'radio' + (picked === n.id ? ' on' : '') }), h('span', { class: 'grow ell' }, n.name), latencyEl(state.latency[n.id]))) : [h('div', { class: 'row hint' }, 'Ничего не найдено')]));
    };
    draw();
    modal({ title: `Сервер в группе «${g.name}»`, size: 'mid',
      body: [
        h('div', { class: 'hint', style: 'margin-top:-6px' }, g.type === 'select' ? 'Группа с ручным выбором: работает выбранный здесь сервер.' : 'Автоматическая группа будет держаться за выбранный сервер, пока он отвечает.'),
        h('label', { class: 'search', style: 'width:100%;border-color:var(--line)' }, icon('search', 's'),
          h('input', { type: 'search', placeholder: 'Найти сервер', 'aria-label': 'Поиск сервера', oninput: e => { q = e.target.value.toLowerCase(); draw(); } })),
        list],
      actions: [{ label: 'Выбрать', primary: true, onClick: async () => {
        if (!picked) return;
        await api('POST', `/groups/${g.id}/select`, { nodeId: picked });
        toast(`В группе «${g.name}» выбран ${nodeName(picked)}`, 'ok');
        route();
      } }] });
  }

  const builtin = autoOf('');
  const builtinCard = nodes.length > 1 && h('article', { class: 'card' + (!settings.defaultTarget ? ' picked' : ''), style: 'padding:16px 18px;display:flex;flex-direction:column;gap:8px' },
    h('div', { style: 'display:flex;align-items:center;gap:8px' }, h('span', { class: 'name grow', style: 'font-size:15.5px' }, 'Авто — все серверы'),
      !settings.defaultTarget ? h('span', { class: 'chip acc' }, 'активный') : h('button', { class: 'btn sm', onclick: () => makeActive('') }, 'Сделать активной')),
    h('div', { class: 'hint' }, `${plural(nodes.length, 'сервер', 'сервера', 'серверов')}, включая Москву и LTE` + (builtin && builtin.selected && !settings.defaultTarget ? ' · сейчас ' + nodeName(builtin.selected) : '')));

  const left = h('div', { class: 'stack', style: 'gap:14px' },
    h('div', { style: 'display:flex;align-items:center' }, h('h2', { class: 'ct' }, 'Группы'), h('button', { class: 'btn sm g', onclick: () => editGroup() }, icon('plus', 's'), 'Новая')),
    groups.map(groupCard), builtinCard,
    h('div', { style: 'display:flex;align-items:center;margin-top:8px' }, h('h2', { class: 'ct' }, 'Подписки'), h('button', { class: 'btn sm g', onclick: () => subModal() }, icon('plus', 's'), 'Новая')),
    subs.length ? subs.map(subCard) : h('div', { class: 'card cb hint' }, 'Подписка — адрес от провайдера, по которому список серверов обновляется сам.'));

  return h('div', { class: 'stack' },
    pageHead('Серверы', [`${plural(nodes.length, 'сервер', 'сервера', 'серверов')} · ${plural(subs.length, 'подписка', 'подписки', 'подписок')} · ${plural(groups.length, 'группа', 'группы', 'групп')} · проверка: `, h('a', { href: '#/settings', onclick: () => setTimeout(() => { const el = document.getElementById('set-ping'); if (el) el.scrollIntoView({ behavior: 'smooth' }); }, 600) }, pingMethodName(settings.pingMethod))],
      h('button', { class: 'btn', onclick: importModal }, icon('plus', 's'), 'Добавить'), testBtn),
    progress,
    h('div', { style: 'display:grid;grid-template-columns:minmax(0,340px) minmax(0,1fr);gap:20px;align-items:start', class: 'srv-grid' }, left, listCard));
}
