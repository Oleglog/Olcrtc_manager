// olcRTC Admin SPA
(function() {
'use strict';

const API = '/api';
let token = localStorage.getItem('olcrtc_token') || '';

// ── Helpers ──────────────────────────────────────────────────────────────────
async function api(path, opts = {}) {
  const url = API + path;
  const res = await fetch(url, {
    headers: {
      'Authorization': 'Bearer ' + token,
      'Content-Type': 'application/json',
      ...opts.headers
    },
    ...opts
  });
  if (res.status === 401) {
    localStorage.removeItem('olcrtc_token');
    token = '';
    route('/login');
    throw new Error('Unauthorized');
  }
  if (!res.ok) {
    const text = await res.text().catch(() => res.statusText);
    throw new Error(text);
  }
  if (res.status === 204) return null;
  return res.json();
}

function el(type, cls, text) {
  const e = document.createElement(type);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function icon(name) {
  const i = el('i', 'w-4 h-4');
  i.setAttribute('data-lucide', name);
  return i;
}

function fmtStatus(st) {
  const map = { running: 'status-running', active: 'status-running', failed: 'status-failed' };
  return map[st] || 'status-inactive';
}

// ── Router ───────────────────────────────────────────────────────────────────
let currentView = null;
function route(path) {
  history.pushState({}, '', path);
  render();
}

function render() {
  const path = location.pathname;
  const app = document.getElementById('app');
  app.innerHTML = '';
  if (!token && path !== '/login') {
    route('/login');
    return;
  }
  if (path === '/login') {
    renderLogin(app);
  } else if (path === '/settings') {
    renderSettings(app);
  } else {
    renderDashboard(app);
  }
  if (window.lucide) lucide.createIcons();
}

window.addEventListener('popstate', render);

// ── Login ────────────────────────────────────────────────────────────────────
function renderLogin(app) {
  const box = el('div', 'flex items-center justify-center min-h-screen');
  const card = el('div', 'card p-8 w-full max-w-sm');
  card.innerHTML = '<h1 class="text-2xl font-bold text-center mb-6">olcRTC Admin</h1>';

  const inp = el('input', '');
  inp.type = 'password';
  inp.placeholder = 'Токен доступа';
  inp.className = 'mb-4';

  const btn = el('button', 'btn btn-primary w-full justify-center');
  btn.innerHTML = '<span>Войти</span>';

  const err = el('div', 'text-red-400 text-sm mt-2 hidden');

  btn.onclick = async () => {
    err.classList.add('hidden');
    try {
      const res = await fetch(API + '/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ token: inp.value })
      });
      const data = await res.json();
      if (data.ok) {
        token = inp.value;
        localStorage.setItem('olcrtc_token', token);
        route('/');
      } else {
        throw new Error('invalid');
      }
    } catch (e) {
      err.textContent = 'Неверный токен';
      err.classList.remove('hidden');
    }
  };

  card.appendChild(inp);
  card.appendChild(btn);
  card.appendChild(err);
  card.appendChild(el('p', 'text-gray-400 text-xs text-center mt-4', 'Токен показан при установке сервера'));
  box.appendChild(card);
  app.appendChild(box);
}

// ── Dashboard ────────────────────────────────────────────────────────────────
async function renderDashboard(app) {
  const wrap = el('div', 'max-w-6xl mx-auto p-4');

  // Header
  const header = el('div', 'flex items-center justify-between mb-6');
  header.innerHTML = '<h1 class="text-xl font-bold">olcRTC Admin</h1>';
  const nav = el('div', 'flex gap-2');
  const settingsBtn = el('button', 'btn btn-secondary btn-sm');
  settingsBtn.innerHTML = '<i data-lucide="settings" class="w-4 h-4"></i> Настройки';
  settingsBtn.onclick = () => route('/settings');
  const logoutBtn = el('button', 'btn btn-secondary btn-sm');
  logoutBtn.innerHTML = '<i data-lucide="log-out" class="w-4 h-4"></i> Выход';
  logoutBtn.onclick = () => { token = ''; localStorage.removeItem('olcrtc_token'); route('/login'); };
  nav.appendChild(settingsBtn);
  nav.appendChild(logoutBtn);
  header.appendChild(nav);
  wrap.appendChild(header);

  let sys = {};
  let instances = [];
  let subs = [];

  try { sys = await api('/system/status'); } catch (e) { console.error(e); }
  try { instances = await api('/instances'); } catch (e) { console.error(e); }
  try { subs = await api('/subs'); } catch (e) { console.error(e); }

  // System card
  const sysCard = el('div', 'card p-4 mb-6');
  sysCard.innerHTML = `
    <div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
      <div><span class="text-gray-400">IP:</span> ${sys.public_ip || '-'}</div>
      <div><span class="text-gray-400">OS:</span> ${sys.os || '-'}</div>
      <div><span class="text-gray-400">Uptime:</span> ${sys.uptime || '-'}</div>
      <div><span class="text-gray-400">TLS:</span> ${sys.tls_mode || '-'} ${sys.domain ? '('+sys.domain+')' : ''}</div>
      <div><span class="text-gray-400">Admin:</span> ${sys.admin_port || '-'}</div>
      <div><span class="text-gray-400">Подписки:</span> ${sys.sub_enabled ? 'вкл ('+sys.sub_port+')' : 'выкл'}</div>
      <div><span class="text-gray-400">Инстансы:</span> ${sys.instances_running || 0}/${sys.instances_total || 0}</div>
      <div><span class="text-gray-400">Версия:</span> ${sys.version || '-'}</div>
    </div>`;
  wrap.appendChild(sysCard);

  // Instances section
  const instSection = el('div', 'card p-4 mb-6');
  instSection.innerHTML = '<h2 class="text-lg font-semibold mb-4">Инстансы</h2>';
  const instList = el('div', 'space-y-3');

  instances.forEach(inst => {
    const row = el('div', 'flex flex-col md:flex-row md:items-center justify-between gap-2 p-3 rounded border border-gray-700 bg-gray-800');
    const left = el('div', 'flex-1');
    left.innerHTML = `
      <div class="flex items-center gap-2">
        <span class="status-dot ${fmtStatus(inst.status)}"></span>
        <span class="font-medium">${inst.label}</span>
        <span class="text-gray-400 text-sm">#${inst.id}</span>
        <span class="text-gray-400 text-sm">${inst.carrier || '-'} / ${inst.transport || '-'}</span>
      </div>
      <div class="text-gray-400 text-xs mt-1">Room: ${inst.room_id || '-'} | Uptime: ${inst.uptime || '-'} | Name: ${inst.name || '-'}</div>
    `;
    const right = el('div', 'flex gap-2 flex-wrap');

    const uriBtn = el('button', 'btn btn-secondary btn-sm');
    uriBtn.innerHTML = '<i data-lucide="copy" class="w-3 h-3"></i> URI';
    uriBtn.onclick = () => { navigator.clipboard.writeText(inst.uri); showToast('URI скопирован'); };

    const qrBtn = el('button', 'btn btn-secondary btn-sm');
    qrBtn.innerHTML = '<i data-lucide="qr-code" class="w-3 h-3"></i> QR';
    qrBtn.onclick = () => showQRModal(inst.uri);

    const restartBtn = el('button', 'btn btn-secondary btn-sm');
    restartBtn.innerHTML = '<i data-lucide="refresh-cw" class="w-3 h-3"></i>';
    restartBtn.title = 'Перезапустить';
    restartBtn.onclick = async () => { await api('/instances/' + inst.id + '/restart', { method: 'POST' }); showToast('Перезапущено'); render(); };

    const stopBtn = el('button', 'btn btn-secondary btn-sm');
    stopBtn.innerHTML = '<i data-lucide="square" class="w-3 h-3"></i>';
    stopBtn.title = 'Остановить';
    stopBtn.onclick = async () => { await api('/instances/' + inst.id + '/stop', { method: 'POST' }); showToast('Остановлено'); render(); };

    const startBtn = el('button', 'btn btn-success btn-sm');
    startBtn.innerHTML = '<i data-lucide="play" class="w-3 h-3"></i>';
    startBtn.title = 'Запустить';
    startBtn.onclick = async () => { await api('/instances/' + inst.id + '/start', { method: 'POST' }); showToast('Запущено'); render(); };

    const cfgBtn = el('button', 'btn btn-secondary btn-sm');
    cfgBtn.innerHTML = '<i data-lucide="sliders" class="w-3 h-3"></i> Config';
    cfgBtn.onclick = () => showConfigModal(inst);

    right.appendChild(uriBtn);
    right.appendChild(qrBtn);
    if (inst.status === 'running') {
      right.appendChild(stopBtn);
    } else {
      right.appendChild(startBtn);
    }
    right.appendChild(restartBtn);
    right.appendChild(cfgBtn);

    if (inst.id !== 0) {
      const delBtn = el('button', 'btn btn-danger btn-sm');
      delBtn.innerHTML = '<i data-lucide="trash-2" class="w-3 h-3"></i>';
      delBtn.title = 'Удалить';
      delBtn.onclick = async () => {
        if (!confirm('Удалить инстанс #' + inst.id + '?')) return;
        await api('/instances/' + inst.id, { method: 'DELETE' });
        showToast('Удалено'); render();
      };
      right.appendChild(delBtn);
    }

    row.appendChild(left);
    row.appendChild(right);
    instList.appendChild(row);
  });

  const addInstBtn = el('button', 'btn btn-primary btn-sm mt-3');
  addInstBtn.innerHTML = '<i data-lucide="plus" class="w-4 h-4"></i> Создать инстанс';
  addInstBtn.onclick = async () => {
    await api('/instances', { method: 'POST' });
    showToast('Инстанс создан'); render();
  };

  instSection.appendChild(instList);
  instSection.appendChild(addInstBtn);
  wrap.appendChild(instSection);

  // Subscriptions section
  const subSection = el('div', 'card p-4 mb-6');
  subSection.innerHTML = '<h2 class="text-lg font-semibold mb-4">Подписки</h2>';
  const subList = el('div', 'space-y-3');

  if (subs.length === 0) {
    subList.appendChild(el('div', 'text-gray-400 text-sm', 'Нет подписок'));
  } else {
    subs.forEach(sub => {
      const row = el('div', 'flex flex-col md:flex-row md:items-center justify-between gap-2 p-3 rounded border border-gray-700 bg-gray-800');
      const subURL = `${sys.admin_url || location.origin}/sub/${sub.slug}`;
      const left = el('div', 'flex-1');
      left.innerHTML = `
        <div class="font-medium">${sub.name} <span class="text-gray-400">[${sub.slug}]</span></div>
        <div class="text-gray-400 text-xs mt-1">URL: ${subURL}</div>
      `;
      const right = el('div', 'flex gap-2 flex-wrap');

      const viewBtn = el('button', 'btn btn-secondary btn-sm');
      viewBtn.innerHTML = '<i data-lucide="eye" class="w-3 h-3"></i> Просмотр';
      viewBtn.onclick = () => window.open(subURL, '_blank');

      const addBtn = el('button', 'btn btn-secondary btn-sm');
      addBtn.innerHTML = '<i data-lucide="plus" class="w-3 h-3"></i> Инстанс';
      addBtn.onclick = () => showAddToSubModal(sub, instances);

      const delBtn = el('button', 'btn btn-danger btn-sm');
      delBtn.innerHTML = '<i data-lucide="trash-2" class="w-3 h-3"></i>';
      delBtn.title = 'Удалить';
      delBtn.onclick = async () => {
        if (!confirm('Удалить подписку ' + sub.slug + '?')) return;
        await api('/subs/' + sub.slug, { method: 'DELETE' });
        showToast('Подписка удалена'); render();
      };

      right.appendChild(viewBtn);
      right.appendChild(addBtn);
      right.appendChild(delBtn);
      row.appendChild(left);
      row.appendChild(right);
      subList.appendChild(row);
    });
  }

  const addSubBtn = el('button', 'btn btn-primary btn-sm mt-3');
  addSubBtn.innerHTML = '<i data-lucide="plus" class="w-4 h-4"></i> Создать подписку';
  addSubBtn.onclick = () => showCreateSubModal();

  subSection.appendChild(subList);
  subSection.appendChild(addSubBtn);
  wrap.appendChild(subSection);

  app.appendChild(wrap);
}

// ── Settings ─────────────────────────────────────────────────────────────────
async function renderSettings(app) {
  const wrap = el('div', 'max-w-2xl mx-auto p-4');
  wrap.innerHTML = '<h1 class="text-xl font-bold mb-4">Настройки</h1>';

  let sys = {};
  try { sys = await api('/system/status'); } catch (e) {}

  const card = el('div', 'card p-4 space-y-6');

  // Domain
  const domBlock = el('div', '');
  domBlock.innerHTML = '<h3 class="font-semibold mb-2">Домен</h3>';
  const domCurrent = el('div', 'text-sm text-gray-400 mb-2', sys.domain ? 'Текущий: ' + sys.domain : 'Текущий: (не привязан)');
  const domInp = el('input', '');
  domInp.placeholder = 'sub.example.com';
  const domBtn = el('button', 'btn btn-primary mt-2');
  domBtn.textContent = 'Привязать домен';
  domBtn.onclick = async () => {
    try {
      const res = await api('/system/domain', {
        method: 'POST',
        body: JSON.stringify({ domain: domInp.value })
      });
      alert(res.message || 'Домен привязан');
      render();
    } catch (e) {
      try {
        const err = JSON.parse(e.message);
        alert(err.message || e.message);
      } catch { alert(e.message); }
    }
  };
  const unbindBtn = el('button', 'btn btn-danger mt-2 ml-2');
  unbindBtn.textContent = 'Отвязать домен';
  unbindBtn.onclick = async () => {
    if (!confirm('Отвязать домен?')) return;
    await api('/system/domain', { method: 'DELETE' });
    render();
  };
  domBlock.appendChild(domCurrent);
  domBlock.appendChild(domInp);
  domBlock.appendChild(domBtn);
  if (sys.domain) domBlock.appendChild(unbindBtn);
  card.appendChild(domBlock);

  // Port
  const portBlock = el('div', '');
  portBlock.innerHTML = `<h3 class="font-semibold mb-2">Порты</h3>
    <div class="text-sm">Admin UI: <span class="font-mono">${sys.admin_port || '-'}</span></div>
    <div class="text-sm">Подписки: <span class="font-mono">${sys.sub_port || '-'}</span></div>`;
  card.appendChild(portBlock);

  // Security
  const secBlock = el('div', '');
  secBlock.innerHTML = '<h3 class="font-semibold mb-2">Безопасность</h3>';
  const changeTokenBtn = el('button', 'btn btn-secondary');
  changeTokenBtn.textContent = 'Сменить токен';
  changeTokenBtn.onclick = async () => {
    if (!confirm('Сгенерировать новый токен? Старый перестанет работать.')) return;
    const res = await api('/auth/change-token', { method: 'POST', body: JSON.stringify({}) });
    token = res.token;
    localStorage.setItem('olcrtc_token', token);
    alert('Новый токен: ' + res.token);
  };
  secBlock.appendChild(changeTokenBtn);
  card.appendChild(secBlock);

  // Logs
  const logBlock = el('div', '');
  logBlock.innerHTML = '<h3 class="font-semibold mb-2">Логи</h3>';
  const logsWrap = el('div', 'flex gap-2 mb-2');
  ['olcrtc-server', 'olcrtc-admin'].forEach(svc => {
    const btn = el('button', 'btn btn-secondary btn-sm');
    btn.textContent = svc;
    btn.onclick = () => showLogsModal(svc);
    logsWrap.appendChild(btn);
  });
  logBlock.appendChild(logsWrap);
  card.appendChild(logBlock);

  // Back
  const backBtn = el('button', 'btn btn-secondary mt-4');
  backBtn.innerHTML = '<i data-lucide="arrow-left" class="w-4 h-4"></i> Назад';
  backBtn.onclick = () => route('/');
  card.appendChild(backBtn);

  wrap.appendChild(card);
  app.appendChild(wrap);
}

// ── Modals ───────────────────────────────────────────────────────────────────
function showModal(content) {
  const overlay = el('div', 'modal-overlay');
  const modal = el('div', 'modal');
  modal.appendChild(content);
  overlay.appendChild(modal);
  document.body.appendChild(overlay);
  overlay.onclick = (e) => { if (e.target === overlay) overlay.remove(); };
  if (window.lucide) lucide.createIcons();
  return overlay;
}

function closeModal(overlay) { overlay.remove(); }

function showQRModal(uri) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">QR-код</h3>';
  const qrDiv = el('div', 'flex justify-center mb-3');
  div.appendChild(qrDiv);
  const uriText = el('div', 'text-xs text-gray-400 break-all mb-3', uri);
  div.appendChild(uriText);
  const btnRow = el('div', 'flex gap-2 justify-end');
  const copyBtn = el('button', 'btn btn-secondary btn-sm');
  copyBtn.textContent = 'Копировать URI';
  copyBtn.onclick = () => { navigator.clipboard.writeText(uri); showToast('Скопировано'); };
  const closeBtn = el('button', 'btn btn-primary btn-sm');
  closeBtn.textContent = 'Закрыть';
  const overlay = showModal(div);
  closeBtn.onclick = () => closeModal(overlay);
  btnRow.appendChild(copyBtn);
  btnRow.appendChild(closeBtn);
  div.appendChild(btnRow);

  // Generate QR after append
  setTimeout(() => {
    new QRCode(qrDiv, { text: uri, width: 200, height: 200, colorDark: '#000', colorLight: '#fff' });
  }, 0);
}

function showConfigModal(inst) {
  const div = el('div', '');
  div.innerHTML = `<h3 class="text-lg font-semibold mb-3">Настройка инстанса #${inst.id}</h3>`;

  const fields = [
    { key: 'carrier', label: 'Carrier', val: inst.carrier || 'wbstream', opts: ['wbstream', 'jazz', 'telemost'] },
    { key: 'transport', label: 'Transport', val: inst.transport || 'datachannel', opts: ['datachannel', 'vp8channel', 'seichannel'] },
    { key: 'name', label: 'Имя', val: inst.name || '' },
    { key: 'dns', label: 'DNS', val: '' },
    { key: 'socks_proxy', label: 'SOCKS', val: '' },
    { key: 'warp_proxy', label: 'WARP', val: '' },
  ];

  const inputs = {};
  fields.forEach(f => {
    const row = el('div', 'mb-3');
    row.innerHTML = `<label class="block text-sm text-gray-400 mb-1">${f.label}</label>`;
    let inp;
    if (f.opts) {
      inp = el('select', '');
      f.opts.forEach(o => {
        const opt = el('option', '', o);
        if (o === f.val) opt.selected = true;
        inp.appendChild(opt);
      });
    } else {
      inp = el('input', '');
      inp.value = f.val;
    }
    inputs[f.key] = inp;
    row.appendChild(inp);
    div.appendChild(row);
  });

  // Debug checkbox
  const debugRow = el('div', 'mb-3 flex items-center gap-2');
  const debugCb = el('input', '');
  debugCb.type = 'checkbox';
  debugRow.appendChild(debugCb);
  debugRow.appendChild(el('label', 'text-sm', 'Включить debug-логирование'));
  div.appendChild(debugRow);

  // VP8 params (conditional)
  const vp8Block = el('div', 'mb-3 border border-gray-700 rounded p-3 hidden');
  vp8Block.innerHTML = '<div class="text-sm text-gray-400 mb-2">VP8 параметры</div>';
  const vp8Fps = el('input', 'mb-2'); vp8Fps.placeholder = 'FPS (30)';
  const vp8Batch = el('input', ''); vp8Batch.placeholder = 'Batch (2)';
  vp8Block.appendChild(vp8Fps);
  vp8Block.appendChild(vp8Batch);
  div.appendChild(vp8Block);

  // SEI params (conditional)
  const seiBlock = el('div', 'mb-3 border border-gray-700 rounded p-3 hidden');
  seiBlock.innerHTML = '<div class="text-sm text-gray-400 mb-2">SEI параметры</div>';
  const seiFps = el('input', 'mb-2'); seiFps.placeholder = 'FPS (20)';
  const seiBatch = el('input', 'mb-2'); seiBatch.placeholder = 'Batch (1)';
  const seiFrag = el('input', 'mb-2'); seiFrag.placeholder = 'Fragment (900)';
  const seiAck = el('input', ''); seiAck.placeholder = 'ACK ms (3000)';
  seiBlock.appendChild(seiFps); seiBlock.appendChild(seiBatch);
  seiBlock.appendChild(seiFrag); seiBlock.appendChild(seiAck);
  div.appendChild(seiBlock);

  function updateBlocks() {
    const t = inputs.transport.value;
    vp8Block.classList.toggle('hidden', t !== 'vp8channel');
    seiBlock.classList.toggle('hidden', t !== 'seichannel');
  }
  inputs.transport.onchange = updateBlocks;
  updateBlocks();

  // Key rotation buttons
  const keyRow = el('div', 'flex gap-2 mb-3');
  const rotKeyBtn = el('button', 'btn btn-danger btn-sm');
  rotKeyBtn.textContent = 'Пересоздать ключ + Room ID';
  rotKeyBtn.onclick = async () => {
    if (!confirm('Пересоздать ключ и Room ID? Клиентам придётся перенастроиться.')) return;
    await api('/instances/' + inst.id + '/rotate-key', { method: 'POST' });
    showToast('Ключ пересоздан'); closeModal(overlay); render();
  };
  const rotRoomBtn = el('button', 'btn btn-danger btn-sm');
  rotRoomBtn.textContent = 'Пересоздать Room ID';
  rotRoomBtn.onclick = async () => {
    if (!confirm('Пересоздать Room ID?')) return;
    await api('/instances/' + inst.id + '/rotate-room', { method: 'POST' });
    showToast('Room ID пересоздан'); closeModal(overlay); render();
  };
  keyRow.appendChild(rotKeyBtn);
  keyRow.appendChild(rotRoomBtn);
  div.appendChild(keyRow);

  // Actions
  const btnRow = el('div', 'flex gap-2 justify-end mt-4');
  const saveBtn = el('button', 'btn btn-primary');
  saveBtn.textContent = 'Сохранить';
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const overlay = showModal(div);

  saveBtn.onclick = async () => {
    const body = {
      carrier: inputs.carrier.value,
      transport: inputs.transport.value,
      name: inputs.name.value,
      dns: inputs.dns.value,
      socks_proxy: inputs.socks_proxy.value,
      warp_proxy: inputs.warp_proxy.value,
      debug: debugCb.checked,
    };
    if (!vp8Block.classList.contains('hidden')) {
      if (vp8Fps.value) body.vp8_fps = parseInt(vp8Fps.value);
      if (vp8Batch.value) body.vp8_batch = parseInt(vp8Batch.value);
    }
    if (!seiBlock.classList.contains('hidden')) {
      if (seiFps.value) body.sei_fps = parseInt(seiFps.value);
      if (seiBatch.value) body.sei_batch = parseInt(seiBatch.value);
      if (seiFrag.value) body.sei_frag = parseInt(seiFrag.value);
      if (seiAck.value) body.sei_ack_ms = parseInt(seiAck.value);
    }
    await api('/instances/' + inst.id + '/config', { method: 'PUT', body: JSON.stringify(body) });
    showToast('Сохранено'); closeModal(overlay); render();
  };
  cancelBtn.onclick = () => closeModal(overlay);
  btnRow.appendChild(saveBtn);
  btnRow.appendChild(cancelBtn);
  div.appendChild(btnRow);
}

function showAddToSubModal(sub, instances) {
  const div = el('div', '');
  div.innerHTML = `<h3 class="text-lg font-semibold mb-3">Добавить инстанс в подписку ${sub.name}</h3>`;
  const list = el('div', 'space-y-2 mb-3');
  const radios = [];
  instances.forEach(inst => {
    const row = el('label', 'flex items-center gap-2 cursor-pointer p-2 rounded hover:bg-gray-700');
    const rb = el('input', ''); rb.type = 'radio'; rb.name = 'inst'; rb.value = inst.id;
    row.appendChild(rb);
    row.appendChild(el('span', 'text-sm', `${inst.label} — ${inst.carrier} ${inst.transport}`));
    list.appendChild(row);
    radios.push(rb);
  });
  const manualRow = el('label', 'flex items-center gap-2 cursor-pointer p-2 rounded hover:bg-gray-700');
  const manualRb = el('input', ''); manualRb.type = 'radio'; manualRb.name = 'inst'; manualRb.value = 'manual';
  manualRow.appendChild(manualRb);
  manualRow.appendChild(el('span', 'text-sm', 'Ввести URI вручную'));
  list.appendChild(manualRow);
  radios.push(manualRb);

  const manualInp = el('input', 'mb-3');
  manualInp.placeholder = 'olcrtc://...';
  manualInp.classList.add('hidden');
  manualRb.onchange = () => { manualInp.classList.remove('hidden'); };
  radios.filter(r => r !== manualRb).forEach(r => {
    r.onchange = () => { manualInp.classList.add('hidden'); };
  });

  div.appendChild(list);
  div.appendChild(manualInp);

  const btnRow = el('div', 'flex gap-2 justify-end');
  const addBtn = el('button', 'btn btn-primary');
  addBtn.textContent = 'Добавить';
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const overlay = showModal(div);

  addBtn.onclick = async () => {
    let rawUri = '';
    const checked = radios.find(r => r.checked);
    if (!checked) { alert('Выберите инстанс'); return; }
    if (checked.value === 'manual') {
      rawUri = manualInp.value;
    } else {
      const inst = instances.find(i => String(i.id) === checked.value);
      rawUri = inst ? inst.uri : '';
    }
    if (!rawUri) { alert('URI не может быть пустым'); return; }
    await api('/subs/' + sub.slug + '/instances', {
      method: 'POST',
      body: JSON.stringify({ raw_uri: rawUri })
    });
    showToast('Добавлено'); closeModal(overlay); render();
  };
  cancelBtn.onclick = () => closeModal(overlay);
  btnRow.appendChild(addBtn);
  btnRow.appendChild(cancelBtn);
  div.appendChild(btnRow);
}

function showCreateSubModal() {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Создать подписку</h3>';
  const nameInp = el('input', 'mb-3');
  nameInp.placeholder = 'Имя подписки';
  const slugInp = el('input', 'mb-3');
  slugInp.placeholder = 'Slug (оставьте пустым для автогенерации)';
  div.appendChild(nameInp);
  div.appendChild(slugInp);

  const btnRow = el('div', 'flex gap-2 justify-end');
  const createBtn = el('button', 'btn btn-primary');
  createBtn.textContent = 'Создать';
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const overlay = showModal(div);

  createBtn.onclick = async () => {
    if (!nameInp.value) { alert('Введите имя'); return; }
    const body = { name: nameInp.value, slug: slugInp.value || undefined };
    await api('/subs', { method: 'POST', body: JSON.stringify(body) });
    showToast('Подписка создана'); closeModal(overlay); render();
  };
  cancelBtn.onclick = () => closeModal(overlay);
  btnRow.appendChild(createBtn);
  btnRow.appendChild(cancelBtn);
  div.appendChild(btnRow);
}

async function showLogsModal(service) {
  const div = el('div', '');
  div.innerHTML = `<h3 class="text-lg font-semibold mb-3">Логи: ${service}</h3>`;
  const pre = el('pre', 'logs');
  pre.textContent = 'Загрузка...';
  div.appendChild(pre);
  const btnRow = el('div', 'flex gap-2 justify-end mt-3');
  const refreshBtn = el('button', 'btn btn-secondary btn-sm');
  refreshBtn.textContent = 'Обновить';
  const closeBtn = el('button', 'btn btn-primary btn-sm');
  closeBtn.textContent = 'Закрыть';
  const overlay = showModal(div);

  async function load() {
    try {
      const data = await api('/system/logs/' + service + '?lines=200');
      pre.textContent = data.logs || '(пусто)';
    } catch (e) {
      pre.textContent = 'Ошибка: ' + e.message;
    }
  }
  refreshBtn.onclick = load;
  closeBtn.onclick = () => closeModal(overlay);
  btnRow.appendChild(refreshBtn);
  btnRow.appendChild(closeBtn);
  div.appendChild(btnRow);
  await load();
}

function showToast(msg) {
  const t = el('div', 'fixed bottom-4 right-4 bg-green-600 text-white px-4 py-2 rounded shadow-lg text-sm transition-opacity');
  t.textContent = msg;
  document.body.appendChild(t);
  setTimeout(() => { t.style.opacity = '0'; setTimeout(() => t.remove(), 300); }, 2000);
}

// ── Init ─────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', render);
})();
