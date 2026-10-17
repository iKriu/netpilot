// Настройки: обновление, режим работы, ядро, прокси, DNS, списки, копия, безопасность, диагностика.

import { h, fill, icon, api, get, toast, modal, confirmModal, state, stage, guard, takeDraft, route, field, seg, sw, select, card, pageHead, notice, arr, ago, scheduleApply, refreshStatus, applyDesign, designOf, DESIGN_SCHEMES } from './core.js';
import { runInstall, waitRestart, loadUpdate } from './app.js';

const PING_METHODS = [
  ['auto', 'Автоматически', 'Через сервер, если шлюз запущен; иначе TCP'],
  ['http', 'HTTP через сервер', 'Настоящая задержка: запрос идёт через сам сервер. Нужен запущенный шлюз'],
  ['tcp', 'TCP', 'Время подключения к порту сервера. Быстро, но не проверяет, что сервер пропускает трафик; не для Hysteria2 и TUIC'],
  ['icmp', 'ICMP (ping)', 'Обычный ping адреса сервера. Подходит для любых протоколов; некоторые серверы на ping не отвечают'],
];
export const pingMethodName = v => (PING_METHODS.find(x => x[0] === (v || 'auto')) || PING_METHODS[0])[1];

const row = (title, hint, control) => h('div', { class: 'row', style: 'padding:14px 20px;flex-wrap:wrap' },
  h('span', { class: 'grow', style: 'flex:1 1 260px;min-width:0' }, h('strong', {}, title), hint && [h('br'), h('span', { class: 'hint' }, hint)]), control);

export async function pageSettings() {
  await refreshStatus();
  const [fetched, cores, u0, listsInfo, net, tp] = await Promise.all(['/settings', '/cores', '/update', '/lists', '/network', '/transparent'].map(p => get(p).catch(() => null)));
  const os = state.status.os;
  // Оформление по умолчанию — явными значениями, иначе выбор «как было» считался бы изменением.
  if (fetched) { fetched.design = fetched.design || 'classic'; fetched.designScheme = fetched.designScheme || 'indigo'; fetched.designMode = fetched.designMode || 'system'; }
  let saved = JSON.stringify(fetched);
  const s = takeDraft('settings') || fetched;
  guard.form = {
    isDirty: () => JSON.stringify(s) !== saved,
    draft: () => s,
    save: async () => {
      await api('PUT', '/settings', s);
      saved = JSON.stringify(s);
      state.savedDesign = designOf(s);
      if (state.savedDesign.design === 'm3') setTimeout(() => applyDesign(state.savedDesign), 400); // открыть новый интерфейс
      return 'Настройки сохранены';
    },
  };

  // ----- оформление: Material 3 Expressive — отдельный интерфейс, открывается после сохранения -----
  const look = h('div', { class: 'cb stack', style: 'gap:16px' });
  const swatches = colors => h('span', { style: 'display:flex;gap:4px;margin-top:10px' }, colors.map(c => h('span', { style: `width:22px;height:22px;border-radius:50%;background:${c};box-shadow:inset 0 0 0 1px rgba(0,0,0,.08)` })));
  const drawLook = () => {
    const d = designOf(s);
    const designChoice = (v, title, sub, colors) => {
      const on = d.design === v;
      return h('button', { type: 'button', class: 'choice' + (on ? ' on' : ''), 'aria-pressed': String(on), onclick: () => { s.design = v; drawLook(); } },
        h('span', { class: 'radio' + (on ? ' on' : '') }), h('span', {}, h('strong', {}, title), h('br'), h('span', { class: 'hint' }, sub), swatches(colors)));
    };
    fill(look,
      h('div', { class: 'grid2', style: 'gap:12px' },
        designChoice('classic', 'Классическое', 'Тёмное меню, светлые карточки, индиго-акцент. Легче для слабых устройств', ['#0C111B', '#3A56F5', '#F4F5F8']),
        designChoice('m3', 'Material 3 Expressive', 'Настоящие компоненты Google: пружинная анимация, превращение форм, крупные заголовки, тёмная тема', ['#4355B9', '#DEE0FF', '#FFD7F1'])),
      d.design === 'm3' && h('div', { class: 'grid2', style: 'gap:16px;align-items:start' },
        h('div', { class: 'fld' }, h('span', { class: 'lbl' }, 'Цвет'),
          h('div', { class: 'opts' }, DESIGN_SCHEMES.map(([v, label, c]) => h('button', { type: 'button', class: 'opt' + (d.scheme === v ? ' on' : ''), 'aria-pressed': String(d.scheme === v),
            onclick: () => { s.designScheme = v; drawLook(); } }, h('span', { style: `width:14px;height:14px;border-radius:50%;background:${c}` }), label)))),
        h('div', { class: 'fld' }, h('span', { class: 'lbl' }, 'Тема'),
          seg([['system', 'Как в системе'], ['light', 'Светлая'], ['dark', 'Тёмная']], d.mode, v => { s.designMode = v; drawLook(); }, 'fill'))),
      h('div', { class: 'hint' }, d.design === 'm3' && JSON.parse(saved).design !== 'm3'
        ? 'Интерфейс Material 3 Expressive откроется после сохранения. Он загружается чуть дольше: это полноценное приложение на компонентах Google.'
        : 'Оформление общее для всех устройств, с которых открывают панель.'));
  };
  drawLook();
  state.update = u0 || state.update;
  const u = state.update || {};

  // ----- обновление -----
  const upd = h('div');
  const drawUpd = () => {
    const lat = u.latest;
    fill(upd, 
      h('div', { class: 'cb', style: 'display:flex;align-items:center;gap:16px;flex-wrap:wrap' },
        h('div', { class: 'grow' }, h('div', { style: 'font-size:20px;font-weight:700' }, u.available ? `${u.current} → ${lat.version}` : u.current),
          h('div', { class: 'hint' }, [u.platform, u.checkedAt ? 'проверено ' + ago(u.checkedAt) : 'ещё не проверялось', 'сам ничего не ставит'].join(' · '))),
        h('button', { class: 'btn', onclick: async e => {
          const b = e.currentTarget; b.classList.add('busy');
          try { Object.assign(u, await api('POST', '/update/check')); toast(u.available ? `Доступна версия ${u.latest.version}` : 'Установлена последняя версия', 'ok', { title: 'Проверка обновлений' }); }
          catch (err) { toast(err.message, 'err', { title: 'Проверка не удалась' }); Object.assign(u, await get('/update').catch(() => ({}))); }
          loadUpdate(); drawUpd();
        } }, icon('refresh', 's'), 'Проверить'),
        u.available && h('button', { class: 'btn p', disabled: u.installing, onclick: () => modal({
          title: `Обновление до ${lat.version}`, size: 'mid',
          body: [h('p', { style: 'margin:0' }, `Установлена ${u.current}. Новая версия скачается с GitHub, сверится с контрольной суммой и пробно запустится. Затем программа перезапустится — сеть пропадёт на несколько секунд.`),
            h('div', { class: 'hint', style: 'display:flex;gap:10px' }, icon('shield', 's'), `Текущая версия сохранится. Если новая не запустится, NetPilot сам вернёт ${u.current}.`),
            lat.notes && h('div', { class: 'card cb', style: 'white-space:pre-wrap;font-size:13px;color:var(--ink2)' }, lat.notes)],
          actions: [{ label: 'Обновить', primary: true, onClick: () => { runInstall(lat.version); } }] }) }, icon('download', 's'), 'Обновить')),
      u.error && h('div', { style: 'padding:0 20px 14px' }, notice('bad', 'alert', u.error)),
      u.signed === false && h('div', { style: 'padding:0 20px 14px' }, notice('', 'shield', 'Сборка без проверки подписи: обновление сверяется только с контрольной суммой из того же релиза. Ставьте его, только если доверяете источнику.')),
      h('div', { class: 'list' },
        row('Проверять раз в 6 часов', 'Только сообщает о новой версии',
          sw(u.autoCheck, v => { u.autoCheck = v; stage('update-auto', async () => { await api('PUT', '/update', { repo: u.repo, autoCheck: v }); }); }, null)),
        row('Источник обновлений', `github.com/${u.repo || ''}` + (u.hasToken ? ' · с токеном' : ''),
          h('button', { class: 'btn sm', onclick: editUpdSource }, 'Изменить')),
        u.previous && row('Предыдущая версия', `${u.previous} — сохранена для отката`,
          h('button', { class: 'btn sm d', onclick: () => confirmModal(`Вернуть версию ${u.previous}? Программа перезапустится.`, async () => { await api('POST', '/update/rollback'); waitRestart(); }, 'Вернуть') }, 'Вернуть ' + u.previous))));
  };
  // Источник решает, какая программа будет установлена, поэтому сервер
  // требует текущий пароль при смене репозитория или токена.
  const editUpdSource = () => {
    let dropToken = false;
    const norm = v => v.trim().replace(/^(https:\/\/)?github\.com\//, '').replace(/^\/+|\/+$/g, '');
    const repo = h('input', { class: 'inp', value: u.repo || '', placeholder: 'владелец/имя', spellcheck: 'false', oninput: () => sync() });
    const token = h('input', { class: 'inp', type: 'password', autocomplete: 'off', placeholder: u.hasToken ? 'сохранён' : 'не задан', oninput: () => sync() });
    const pw = h('input', { class: 'inp', type: 'password', autocomplete: 'current-password' });
    const pwBox = field('Текущий пароль', pw, 'Подтверждает смену источника обновлений');
    const changed = () => norm(repo.value) !== (u.repo || '') || token.value.trim() !== '' || dropToken;
    const sync = () => { pwBox.style.display = changed() ? '' : 'none'; };
    sync();
    modal({
      title: 'Источник обновлений', size: 'mid',
      body: [field('Репозиторий GitHub', repo, 'Откуда берутся новые версии'),
        field('Токен GitHub', token, u.hasToken ? 'Пусто — оставить сохранённый' : 'Нужен только для закрытого репозитория'),
        u.hasToken && sw(false, v => { dropToken = v; token.disabled = v; sync(); }, 'Удалить сохранённый токен'),
        pwBox],
      actions: [{ label: 'Сохранить', primary: true, onClick: async () => {
        if (!changed()) return;
        if (!pw.value) { pw.focus(); throw new Error('Введите текущий пароль'); }
        const body = { repo: norm(repo.value), autoCheck: u.autoCheck, password: pw.value };
        if (dropToken) body.token = ''; else if (token.value.trim()) body.token = token.value.trim();
        Object.assign(u, await api('PUT', '/update', body));
        toast('Источник обновлений изменён', 'ok');
        loadUpdate(); drawUpd();
      } }],
    });
  };
  drawUpd();

  // ----- режим работы -----
  const tpBox = h('div', { class: 'list' });
  const drawMode = () => fill(tpBox, ...(s.mode === 'transparent' ? [
    row('Кого перехватывать', 'Все устройства, для которых NetPilot шлюз, или только с маршрутом «через прокси»',
      select([['all', 'Все устройства сети'], ['selected', 'Только с маршрутом «через прокси»']], s.transparentScope, v => s.transparentScope = v, { style: 'width:280px' })),
    row('Перехват DNS', 'Провайдер не видит, какие сайты открываются', sw(s.hijackDns, v => s.hijackDns = v, null)),
    row('IPv6', 'Блокировка — устройства сразу переходят на IPv4 и не идут мимо прокси',
      seg([['block', 'Блокировать'], ['intercept', 'Перехватывать'], ['off', 'Не трогать']], s.ipv6 || 'block', v => s.ipv6 = v)),
    row('Если ядро упало', 'Пока NetPilot перезапускает ядро: устройства идут в интернет без прокси или остаются без интернета',
      seg([['open', 'Пускать напрямую'], ['closed', 'Блокировать интернет']], s.coreFailure || 'open', v => s.coreFailure = v)),
    row('Порт перехвата (TPROXY)', 'Внутренний порт для перехваченного трафика', h('input', { class: 'inp', type: 'number', value: s.tproxyPort, style: 'width:120px', oninput: e => s.tproxyPort = +e.target.value })),
    row('Состояние перехвата', [tp && tp.error, ...arr(tp && tp.problems)].filter(Boolean).join(' · ') || null,
      !tp ? h('span', { class: 'chip' }, '—') : tp.active ? h('span', { class: 'chip ok' }, 'включён') : tp.supported ? h('span', { class: 'chip' }, 'включится при запуске шлюза') : h('span', { class: 'chip bad' }, 'недоступен на этой системе')),
  ] : [
    row('Доступ к прокси', 'Для других устройств выберите «Вся локальная сеть»',
      select([['127.0.0.1', 'Только этот компьютер'], ['0.0.0.0', 'Вся локальная сеть'], ...(['127.0.0.1', '0.0.0.0'].includes(s.listenAddr) ? [] : [[s.listenAddr, s.listenAddr]])], s.listenAddr, v => s.listenAddr = v, { style: 'width:240px' })),
    row('Порт', 'HTTP и SOCKS5 на одном порту', h('input', { class: 'inp', type: 'number', value: s.mixedPort, style: 'width:120px', oninput: e => s.mixedPort = +e.target.value })),
    (os === 'windows' || os === 'darwin') && row('Системный прокси', 'Направить через NetPilot программы этого компьютера', sw(s.systemProxy, v => s.systemProxy = v, null)),
  ]));
  drawMode();
  const modeChoice = (v, title, sub) => {
    const b = h('button', { type: 'button', class: 'choice' + (s.mode === v ? ' on' : '') }, h('span', { class: 'radio' + (s.mode === v ? ' on' : '') }), h('span', {}, h('strong', {}, title), h('br'), h('span', { class: 'hint' }, sub)));
    b.onclick = () => { s.mode = v; [...b.parentElement.children].forEach(x => { const on = x === b; x.classList.toggle('on', on); x.firstChild.classList.toggle('on', on); }); drawMode(); };
    return b;
  };

  // ----- проверка серверов -----
  const pingChoice = (v, title, sub) => {
    const cur = s.pingMethod || 'auto';
    const b = h('button', { type: 'button', class: 'choice' + (cur === v ? ' on' : '') }, h('span', { class: 'radio' + (cur === v ? ' on' : '') }), h('span', {}, h('strong', {}, title), h('br'), h('span', { class: 'hint' }, sub)));
    b.onclick = () => { s.pingMethod = v; [...b.parentElement.children].forEach(x => { const on = x === b; x.classList.toggle('on', on); x.firstChild.classList.toggle('on', on); }); };
    return b;
  };

  // ----- ядра -----
  const installCore = async (name, btn) => {
    btn.classList.add('busy');
    try { const r = await api('POST', `/cores/${name}/install`); toast(`${name} ${r.version} установлено`, 'ok'); route(); }
    catch (e) { toast(e.message, 'err'); btn.classList.remove('busy'); }
  };
  // newer(a, b) — версия a новее b («1.14.3» > «1.14.2»).
  const newer = (a, b) => {
    const p = v => String(v || '').split(/[.-]/).map(x => parseInt(x, 10) || 0);
    const x = p(a), y = p(b);
    for (let i = 0; i < Math.max(x.length, y.length); i++) if ((x[i] || 0) !== (y[i] || 0)) return (x[i] || 0) > (y[i] || 0);
    return false;
  };
  const CORE_ABOUT = {
    'sing-box': 'Встроено в NetPilot: все протоколы, списки, распознавание торрентов',
    xray: 'Отдельная программа: умеет XHTTP; нет Hysteria2 и TUIC, нет списка соединений',
    mihomo: 'Отдельная программа (Clash.Meta)',
    hiddify: 'Отдельная программа: sing-box с дополнениями Hiddify, работает так же, как встроенный',
  };
  const coreRows = arr(cores).map(c => {
    const running = state.status.running && state.status.core === c.name;
    const outdated = c.available && c.latest && c.version !== '?' && newer(c.latest, c.version);
    const ver = !c.available ? h('span', { class: 'chip' }, 'не установлено')
      : !c.latest ? null
      : outdated ? h('span', { class: 'chip warn' }, `есть ${c.latest}`)
      : h('span', { class: 'chip ok' }, 'последняя версия');
    const issues = arr(c.issues).length;
    return h('div', { class: 'row', style: 'padding:14px 20px;flex-wrap:wrap' },
      h('span', { class: 'ico' }, icon('bolt')),
      h('span', { class: 'grow', style: 'flex:1 1 260px;min-width:0' },
        h('span', { style: 'display:flex;align-items:center;gap:8px;flex-wrap:wrap' }, h('strong', {}, c.name, c.available && c.version ? ' ' + c.version : ''),
          running && h('span', { class: 'chip ok' }, 'работает'), ver),
        h('span', { class: 'hint' }, [CORE_ABOUT[c.name] || (c.installable ? 'ставится отдельно' : 'встроено'),
          issues ? `не поддерживает серверов: ${issues}` : 'поддерживает все ваши серверы',
          !c.installable && outdated ? 'новая версия придёт с обновлением NetPilot' : null].filter(Boolean).join(' · '))),
      c.installable && h('button', { class: 'btn sm' + (!c.available || outdated ? ' p' : ''), title: running ? 'Сначала выберите другое ядро или остановите шлюз' : '',
        disabled: running, onclick: e => installCore(c.name, e.currentTarget) },
        icon(c.available ? 'refresh' : 'download', 's'),
        !c.available ? (c.latest ? 'Установить ' + c.latest : 'Установить') : outdated ? 'Обновить до ' + c.latest : 'Переустановить'),
      c.installable && c.available && h('button', { class: 'ib danger', 'aria-label': 'Удалить ' + c.name, title: 'Удалить ' + c.name, disabled: running,
        onclick: () => confirmModal(`Удалить ядро ${c.name}?`, async () => { await api('DELETE', '/cores/' + c.name); route(); }) }, icon('trash', 's')));
  });

  // ----- резервная копия -----
  const fileInput = h('input', { type: 'file', accept: '.json,application/json', hidden: true, onchange: async e => {
    const file = e.target.files[0];
    if (!file) return;
    confirmModal(`Заменить текущие настройки копией «${file.name}»?`, async () => {
      const res = await fetch('/api/restore', { method: 'POST', body: await file.text() });
      const data = await res.json().catch(() => null);
      if (!res.ok) throw new Error((data && data.error) || res.statusText);
      toast('Настройки восстановлены из копии', 'ok');
      scheduleApply();
      guard.form = null; // черновик относится к прежним настройкам
      route();
    }, 'Восстановить');
    e.target.value = '';
  } });

  // ----- пароль -----
  const cur = h('input', { class: 'inp', type: 'password', autocomplete: 'current-password' });
  const next = h('input', { class: 'inp', type: 'password', autocomplete: 'new-password', placeholder: 'от 8 символов' });

  const preview = h('pre', { class: 'log', hidden: true });
  const sections = [
    ['upd', 'Обновление', u.available ? 1 : 0], ['look', 'Оформление'], ['mode', 'Режим работы'], ['core', 'Ядро'], ['ping', 'Проверка серверов'], ['dns', 'DNS'],
    ['lists', 'Списки блокировок'], ['backup', 'Резервная копия'], ['sec', 'Безопасность'], ['diag', 'Диагностика'],
  ];
  const sec = (id, title, right, ...body) => { const el = card(title, right, ...body); el.id = 'set-' + id; if (id === 'upd' && u.available) el.classList.add('acc'); return el; };
  const toc = h('nav', { class: 'hide-t', 'aria-label': 'Разделы настроек', style: 'position:sticky;top:96px;display:flex;flex-direction:column;gap:2px' },
    sections.map(([id, label, n]) => h('a', { href: '#/settings', style: 'height:38px;padding:0 12px;border-radius:9px;display:flex;align-items:center;gap:8px;color:var(--ink2);font-weight:500',
      onclick: e => { e.preventDefault(); document.getElementById('set-' + id).scrollIntoView({ behavior: 'smooth', block: 'start' }); } }, label, n ? h('span', { class: 'chip acc', style: 'margin-left:auto' }, n) : null)));

  const content = h('div', { class: 'stack', style: 'min-width:0' },
    sec('upd', 'Обновление', u.available && h('span', { class: 'chip acc' }, 'доступна ' + u.latest.version), upd),
    sec('look', 'Оформление', null, look),
    sec('mode', 'Режим работы', null,
      h('div', { class: 'cb grid2', style: 'gap:12px' },
        modeChoice('transparent', 'Прозрачный прокси', 'Устройства ничего не настраивают: достаточно указать NetPilot шлюзом (Linux)'),
        modeChoice('proxy', 'Прокси-сервер', 'Адрес прокси вписывается на каждом устройстве вручную')),
      tpBox),
    sec('core', 'Ядро', null, h('div', { class: 'list' }, coreRows,
      row('Какое ядро использовать', 'Автоматически — то, что поддерживает все серверы и расходует меньше ресурсов',
        select([['auto', 'Выбирать автоматически'], ...arr(cores).map(c => [c.name, c.name + (c.available ? '' : ' (не установлено)')])], s.core, v => s.core = v, { style: 'width:240px' })),
      row('Запускать вместе с программой', 'Для шлюза обязательно', sw(s.autoStart, v => s.autoStart = v, null)))),
    sec('ping', 'Проверка серверов', null,
      h('div', { class: 'cb stack', style: 'gap:10px' },
        h('div', { class: 'hint' }, 'Как измерять задержку кнопкой «Проверить». Автоматические группы всегда проверяют настоящим запросом через сервер — только так видно, что сервер пропускает трафик.'),
        h('div', { class: 'grid2', style: 'gap:10px' }, PING_METHODS.map(([v, title, sub]) => pingChoice(v, title, sub)))),
      h('div', { class: 'list' }, row('Адрес для проверки через сервер', 'Отвечает быстро и без содержимого', h('input', { class: 'inp mono', value: s.testUrl, style: 'width:320px;max-width:100%', oninput: e => s.testUrl = e.target.value })))),
    sec('dns', 'DNS', null, h('div', { class: 'cb grid2', style: 'gap:16px' },
      field('Для трафика через прокси', h('input', { class: 'inp mono', value: s.dnsRemote, oninput: e => s.dnsRemote = e.target.value }), 'Запросы идут через сервер. https://, tls://, quic:// или IP'),
      field('Для прямого трафика', h('input', { class: 'inp mono', value: s.dnsDirect, oninput: e => s.dnsDirect = e.target.value }), 'И для ответов устройствам. auto — Cloudflare, Google, Quad9, Яндекс последним: NetPilot берёт первый отвечающий и переключается сам; local — системный')),
      h('div', { class: 'list' }, row('Отвечать устройствам через прокси', 'Выключено (рекомендуется): DNS не зависит от серверов. Включено: если сервер упадёт, DNS пропадёт у всей сети', sw(s.dnsViaProxy, v => s.dnsViaProxy = v, null)))),
    sec('lists', 'Списки блокировок', h('button', { class: 'btn sm', onclick: async e => { e.currentTarget.classList.add('busy'); await api('POST', '/lists/update').then(() => toast('Списки обновлены', 'ok'), x => toast(x.message, 'err')); route(); } }, icon('refresh', 's'), 'Обновить'),
      h('div', { class: 'list' }, arr(listsInfo).map(l => h('div', { class: 'row', style: 'padding:14px 20px' },
        h('span', { class: 'grow' }, h('strong', {}, l.name), h('br'), h('span', { class: 'hint' }, l.description)),
        l.ready ? h('span', { class: 'chip ok' }, (l.entries ? l.entries.toLocaleString('ru-RU') + ' · ' : '') + ago(l.updated)) : h('span', { class: 'chip warn' }, l.error ? 'ошибка' : 'не скачан'))))),
    sec('backup', 'Резервная копия', null, h('div', { class: 'cb', style: 'display:flex;align-items:center;gap:12px;flex-wrap:wrap' },
      h('span', { class: 'grow hint', style: 'min-width:220px' }, 'Устройства, серверы, группы, правила, подписки и настройки. Без пароля администратора. В файле есть ключи серверов — храните его надёжно.'),
      h('a', { class: 'btn', href: '/api/backup', download: '' }, icon('download', 's'), 'Скачать'),
      h('button', { class: 'btn g', onclick: () => fileInput.click() }, icon('upload', 's'), 'Восстановить'), fileInput)),
    sec('sec', 'Безопасность', null, h('div', { class: 'cb grid3', style: 'gap:12px;align-items:end' },
      field('Текущий пароль', cur), field('Новый пароль', next),
      h('button', { class: 'btn', onclick: async () => {
        await api('POST', '/auth/password', { current: cur.value, next: next.value }).then(() => { toast('Пароль изменён, остальные сеансы завершены', 'ok'); cur.value = next.value = ''; }, e => toast(e.message, 'err'));
      } }, 'Сменить пароль'))),
    sec('diag', 'Диагностика', null, h('div', { class: 'list' },
      net && row('Платформа и DHCP', [net.environment, net.dhcp, net.dhcpManaged ? 'закреплённые IP записываются в DHCP сами' : 'закрепление IP — только при установке на OpenWrt или в режиме роутера'].filter(Boolean).join(' · '), null),
      net && row('Сканируемые сети', null, h('span', { class: 'mono' }, arr(net.networks).join(', ') || '—')),
      row('Уровень журнала', null, select(['error', 'warn', 'info', 'debug'].map(l => [l, l]), s.logLevel, v => s.logLevel = v, { style: 'width:140px' })),
      row('Журнал ядра', null, h('button', { class: 'btn sm', onclick: async () => { preview.hidden = false; preview.textContent = (await get('/core/log')).log || 'Журнал пуст'; preview.scrollTop = preview.scrollHeight; } }, 'Показать')),
      row('Итоговая конфигурация ядра', 'Во что превращаются устройства, серверы, группы и правила', h('button', { class: 'btn sm', onclick: async () => {
        preview.hidden = false;
        try { const r = await get('/core/preview'); preview.textContent = `// ядро: ${r.core} (${r.reason})\n` + JSON.stringify(r.config, null, 2); } catch (e) { preview.textContent = 'Ошибка: ' + e.message; }
      } }, 'Показать'))), preview));

  return h('div', { class: 'stack' },
    pageHead('Настройки', 'Изменения сохраняются кнопкой внизу — она появляется, когда что-то изменено'),
    h('div', { style: 'display:grid;grid-template-columns:200px minmax(0,1fr);gap:28px;align-items:start', class: 'set-grid' }, toc, content));
}
