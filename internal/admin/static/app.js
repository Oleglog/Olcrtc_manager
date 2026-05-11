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

function icon(name, sz) {
  const size = sz || 16;
  const svgs = {
    'settings': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>',
    'log-out': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>',
    'copy': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>',
    'qr-code': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="8" height="8"/><rect x="14" y="2" width="8" height="8"/><rect x="2" y="14" width="8" height="8"/><path d="M14 14h.01"/><path d="M18 14h.01"/><path d="M14 18h.01"/><path d="M18 18h.01"/><path d="M22 14v4a2 2 0 0 1-2 2h-2"/><path d="M10 22H6a2 2 0 0 1-2-2v-2"/></svg>',
    'refresh-cw': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M3 21v-5h5"/></svg>',
    'square': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" ry="2"/></svg>',
    'play': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>',
    'sliders': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/></svg>',
    'trash-2': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/></svg>',
    'plus': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>',
    'eye': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>',
    'arrow-left': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="19" y1="12" x2="5" y2="12"/><polyline points="12 19 5 12 12 5"/></svg>',
    'alert-circle': '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>'
  };
  return svgs[name] || '';
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
  settingsBtn.innerHTML = ''+icon('settings')+' Настройки';
  settingsBtn.onclick = () => route('/settings');
  const logoutBtn = el('button', 'btn btn-secondary btn-sm');
  logoutBtn.innerHTML = ''+icon('log-out')+' Выход';
  logoutBtn.onclick = () => { token = ''; localStorage.removeItem('olcrtc_token'); route('/login'); };
  nav.appendChild(settingsBtn);
  nav.appendChild(logoutBtn);
  header.appendChild(nav);
  wrap.appendChild(header);

  let sys = {};
  let instances = [];
  let subs = [];
  let subsError = null;

  try { sys = await api('/system/status'); } catch (e) { console.error(e); }
  try { instances = await api('/instances'); } catch (e) { console.error(e); }
  try { subs = await api('/subs'); } catch (e) {
    try {
      const errData = JSON.parse(e.message);
      if (errData.error === 'subscription_service_unavailable') {
        subsError = errData.message;
      }
    } catch { console.error(e); }
  }

  // System card
  const sysCard = el('div', 'card p-4 mb-6');
  sysCard.innerHTML = `
    <div class="grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
      <div><span class="text-gray-400">IP:</span> ${sys.public_ip || '-'}</div>
      <div><span class="text-gray-400">OS:</span> ${sys.os || '-'}</div>
      <div><span class="text-gray-400">Uptime:</span> ${sys.uptime || '-'}</div>
      <div><span class="text-gray-400">TLS:</span> ${sys.tls_mode || '-'} ${sys.domain ? '('+sys.domain+')' : ''}</div>
      <div><span class="text-gray-400">Admin:</span> ${sys.admin_port || '-'}</div>
      <div><span class="text-gray-400">Подписки:</span> ${sys.sub_domain_active ? '<span class="text-green-400">HTTPS</span> ('+sys.sub_domain+')' : sys.sub_enabled ? 'вкл ('+sys.sub_port+')' : 'выкл'}</div>
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
    uriBtn.innerHTML = ''+icon('copy')+' URI';
    uriBtn.onclick = () => { navigator.clipboard.writeText(inst.uri); showToast('URI скопирован'); };

    const qrBtn = el('button', 'btn btn-secondary btn-sm');
    qrBtn.innerHTML = ''+icon('qr-code')+' QR';
    qrBtn.onclick = () => showQRModal(inst.uri);

    const restartBtn = el('button', 'btn btn-secondary btn-sm');
    restartBtn.innerHTML = ''+icon('refresh-cw')+'';
    restartBtn.title = 'Перезапустить';
    restartBtn.onclick = async () => { await api('/instances/' + inst.id + '/restart', { method: 'POST' }); showToast('Перезапущено'); render(); };

    const stopBtn = el('button', 'btn btn-secondary btn-sm');
    stopBtn.innerHTML = ''+icon('square')+'';
    stopBtn.title = 'Остановить';
    stopBtn.onclick = async () => { await api('/instances/' + inst.id + '/stop', { method: 'POST' }); showToast('Остановлено'); render(); };

    const startBtn = el('button', 'btn btn-success btn-sm');
    startBtn.innerHTML = ''+icon('play')+'';
    startBtn.title = 'Запустить';
    startBtn.onclick = async () => { await api('/instances/' + inst.id + '/start', { method: 'POST' }); showToast('Запущено'); render(); };

    const cfgBtn = el('button', 'btn btn-secondary btn-sm');
    cfgBtn.innerHTML = ''+icon('sliders')+' Config';
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
      delBtn.innerHTML = ''+icon('trash-2')+'';
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
  addInstBtn.innerHTML = ''+icon('plus')+' Создать инстанс';
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

  if (subsError) {
    subList.appendChild(el('div', 'text-yellow-400 text-sm mb-2', 'Сервис подписок недоступен. Проверьте, что olcrtc-server запущен с OLCRTC_SUB_ENABLED=1.'));
  } else if (!subs || subs.length === 0) {
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
      viewBtn.innerHTML = ''+icon('eye')+' Просмотр';
      viewBtn.onclick = () => window.open(subURL, '_blank');

      const instBtn = el('button', 'btn btn-secondary btn-sm');
      instBtn.innerHTML = ''+icon('settings')+' Инстансы';
      instBtn.onclick = () => showSubInstancesModal(sub);

      const addBtn = el('button', 'btn btn-secondary btn-sm');
      addBtn.innerHTML = ''+icon('plus')+' Добавить';
      addBtn.onclick = () => showAddToSubModal(sub, instances);

      const delBtn = el('button', 'btn btn-danger btn-sm');
      delBtn.innerHTML = ''+icon('trash-2')+'';
      delBtn.title = 'Удалить';
      delBtn.onclick = async () => {
        if (!confirm('Удалить подписку ' + sub.slug + '?')) return;
        await api('/subs/' + sub.slug, { method: 'DELETE' });
        showToast('Подписка удалена'); render();
      };

      right.appendChild(viewBtn);
      right.appendChild(instBtn);
      right.appendChild(addBtn);
      right.appendChild(delBtn);
      row.appendChild(left);
      row.appendChild(right);
      subList.appendChild(row);
    });
  }

  const addSubBtn = el('button', 'btn btn-primary btn-sm mt-3');
  addSubBtn.innerHTML = ''+icon('plus')+' Создать подписку';
  addSubBtn.onclick = () => showCreateSubModal();

  const exportBtn = el('button', 'btn btn-secondary btn-sm mt-3 ml-2');
  exportBtn.textContent = 'Экспорт JSON';
  exportBtn.onclick = async () => {
    try {
      const data = await api('/subs/export');
      const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'olcrtc-subscriptions.json';
      a.click();
      URL.revokeObjectURL(url);
      showToast('Экспортировано');
    } catch (e) { alert('Ошибка экспорта: ' + e.message); }
  };

  const importBtn = el('button', 'btn btn-secondary btn-sm mt-3 ml-2');
  importBtn.textContent = 'Импорт JSON';
  importBtn.onclick = () => showImportSubModal();

  subSection.appendChild(subList);
  subSection.appendChild(addSubBtn);
  subSection.appendChild(exportBtn);
  subSection.appendChild(importBtn);
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

  // Domain (Admin UI)
  const domBlock = el('div', '');
  domBlock.innerHTML = '<h3 class="font-semibold mb-2">Домен Admin UI</h3>';
  const domCurrent = el('div', 'text-sm text-gray-400 mb-2', sys.domain ? 'Текущий: ' + sys.domain : 'Текущий: (не привязан)');
  const domInp = el('input', '');
  domInp.placeholder = 'admin.example.com';
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

  // Subscription Domain
  const subDomBlock = el('div', '');
  subDomBlock.innerHTML = '<h3 class="font-semibold mb-2">Домен для подписок</h3>';

  let subDomStatus = {};
  try { subDomStatus = await api('/system/sub-domain'); } catch (e) {}

  if (subDomStatus.domain) {
    const statusDiv = el('div', 'text-sm mb-2');
    statusDiv.innerHTML = '<span class="text-green-400">HTTPS активен</span> — ' + subDomStatus.domain;
    subDomBlock.appendChild(statusDiv);
    const urlDiv = el('div', 'text-sm text-gray-400 mb-2', 'URL: ' + subDomStatus.sub_url);
    subDomBlock.appendChild(urlDiv);
    if (subDomStatus.cert_expires) {
      const certDiv = el('div', 'text-sm text-gray-400 mb-2', 'Сертификат до: ' + new Date(subDomStatus.cert_expires).toLocaleDateString());
      subDomBlock.appendChild(certDiv);
    }
    const subUnbindBtn = el('button', 'btn btn-danger mt-2');
    subUnbindBtn.textContent = 'Отвязать домен подписок';
    subUnbindBtn.onclick = async () => {
      if (!confirm('Отвязать домен подписок? Подписки будут доступны только по IP.')) return;
      subUnbindBtn.disabled = true;
      subUnbindBtn.textContent = 'Отвязка...';
      try {
        const res = await api('/system/sub-domain', { method: 'DELETE' });
        showToast(res.message || 'Домен отвязан');
        render();
      } catch (e) {
        try {
          const err = JSON.parse(e.message);
          alert(err.message || e.message);
        } catch { alert(e.message); }
        subUnbindBtn.disabled = false;
        subUnbindBtn.textContent = 'Отвязать домен подписок';
      }
    };
    subDomBlock.appendChild(subUnbindBtn);
  } else {
    const noSubDom = el('div', 'text-sm text-gray-400 mb-2', 'Не привязан. Подписки доступны по: http://' + (sys.public_ip || 'IP') + ':' + (sys.sub_port || '2096') + '/sub/{slug}');
    subDomBlock.appendChild(noSubDom);

    const hintDiv = el('div', 'text-xs text-yellow-400 mb-2', 'Перед привязкой убедитесь, что A-запись домена указывает на IP этого сервера.');
    subDomBlock.appendChild(hintDiv);

    const subDomInp = el('input', '');
    subDomInp.placeholder = 'sub.example.com';
    subDomBlock.appendChild(subDomInp);

    const subDomBtn = el('button', 'btn btn-primary mt-2');
    subDomBtn.textContent = 'Привязать домен подписок';

    const progressDiv = el('div', 'mt-2 text-sm hidden');
    subDomBlock.appendChild(progressDiv);

    subDomBtn.onclick = async () => {
      if (!subDomInp.value) { alert('Укажите домен'); return; }
      subDomBtn.disabled = true;
      subDomBtn.textContent = 'Привязка...';
      progressDiv.classList.remove('hidden');
      progressDiv.innerHTML = '<div class="text-gray-400">Определение окружения...</div>';
      try {
        const res = await api('/system/sub-domain', {
          method: 'POST',
          body: JSON.stringify({ domain: subDomInp.value })
        });
        progressDiv.innerHTML = '';
        if (res.steps) {
          res.steps.forEach(function(step) {
            const s = el('div', 'text-xs text-gray-400', step);
            progressDiv.appendChild(s);
          });
        }
        const doneDiv = el('div', 'text-green-400 mt-1', res.message || 'Домен привязан!');
        progressDiv.appendChild(doneDiv);
        setTimeout(function() { render(); }, 2000);
      } catch (e) {
        let errMsg = e.message;
        let errSteps = [];
        try {
          const err = JSON.parse(e.message);
          errMsg = err.message || e.message;
          errSteps = err.steps || [];
        } catch {}
        progressDiv.innerHTML = '';
        errSteps.forEach(function(step) {
          const s = el('div', 'text-xs text-gray-400', step);
          progressDiv.appendChild(s);
        });
        const errDiv = el('div', 'text-red-400 mt-1', errMsg);
        progressDiv.appendChild(errDiv);
        subDomBtn.disabled = false;
        subDomBtn.textContent = 'Привязать домен подписок';
      }
    };
    subDomBlock.appendChild(subDomBtn);
  }
  card.appendChild(subDomBlock);

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
  backBtn.innerHTML = ''+icon('arrow-left')+' Назад';
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
  
  return overlay;
}

function closeModal(overlay) { overlay.remove(); }

function showQRModal(uri) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">QR-код</h3>';
  const qrWrap = el('div', 'qr-wrap flex justify-center mb-3');
  const qrDiv = el('div', '');
  qrWrap.appendChild(qrDiv);
  div.appendChild(qrWrap);
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
    new QRCode(qrDiv, { text: uri, width: 280, height: 280, colorDark: '#000000', colorLight: '#ffffff', correctLevel: QRCode.CorrectLevel.H });
  }, 0);
}

function showConfigModal(inst) {
  const div = el('div', '');
  div.innerHTML = `<h3 class="text-lg font-semibold mb-3">Настройка инстанса #${inst.id}</h3>`;

  const fields = [
    { key: 'carrier', label: 'Carrier', val: inst.carrier || 'wbstream', opts: ['wbstream', 'jazz', 'telemost'] },
    { key: 'transport', label: 'Transport', val: inst.transport || 'datachannel', opts: ['datachannel', 'vp8channel', 'seichannel'] },
    { key: 'name', label: 'Имя', val: inst.name || '' },
    { key: 'dns', label: 'DNS', val: inst.dns || '' },
    { key: 'socks_proxy', label: 'SOCKS', val: inst.socks_proxy || '' },
    { key: 'warp_proxy', label: 'WARP', val: inst.warp_proxy || '' },
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

  // Proxy hint
  const proxyHint = el('div', 'mb-3 text-xs text-gray-500 bg-gray-800 p-2 rounded');
  proxyHint.innerHTML = '<b>Прокси:</b><br>SOCKS — для signaling. Формат: <code>socks5://user:pass@host:port</code> или <code>user:pass@host:port</code><br>WARP — для клиентского трафика. Формат: <code>host:port</code> (например <code>127.0.0.1:40000</code>)';
  div.appendChild(proxyHint);
  const debugRow = el('div', 'mb-3 flex items-center gap-2');
  const debugCb = el('input', '');
  debugCb.type = 'checkbox';
  debugCb.checked = inst.debug || false;
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
    const row = el('label', 'radio-row');
    const rb = el('input', ''); rb.type = 'radio'; rb.name = 'inst'; rb.value = inst.id;
    row.appendChild(rb);
    row.appendChild(el('span', 'text-sm', `${inst.label} — ${inst.carrier} ${inst.transport}`));
    list.appendChild(row);
    radios.push(rb);
  });
  const manualRow = el('label', 'radio-row');
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
  const slugRow = el('div', 'slug-row mb-3');
  const slugInp = el('input', '');
  slugInp.placeholder = 'Slug (оставьте пустым для автогенерации)';
  const randBtn = el('button', 'btn btn-secondary btn-sm');
  randBtn.textContent = 'Случайный';
  randBtn.onclick = () => {
    const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
    let s = '';
    const len = 5 + Math.floor(Math.random() * 6);
    for (let i = 0; i < len; i++) s += chars[Math.floor(Math.random() * chars.length)];
    slugInp.value = s;
  };
  slugRow.appendChild(slugInp);
  slugRow.appendChild(randBtn);
  div.appendChild(nameInp);
  div.appendChild(slugRow);

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

async function showSubInstancesModal(sub) {
  const div = el('div', '');
  div.innerHTML = `<h3 class="text-lg font-semibold mb-3">Инстансы в подписке «${sub.name}»</h3>`;
  const list = el('div', 'space-y-2 mb-3');
  list.appendChild(el('div', 'text-sm text-gray-400', 'Загрузка...'));
  div.appendChild(list);
  const btnRow = el('div', 'flex gap-2 justify-end');
  const closeBtn = el('button', 'btn btn-primary btn-sm');
  closeBtn.textContent = 'Закрыть';
  const overlay = showModal(div);
  closeBtn.onclick = () => closeModal(overlay);
  btnRow.appendChild(closeBtn);
  div.appendChild(btnRow);

  try {
    const insts = await api('/subs/' + sub.slug + '/instances');
    list.innerHTML = '';
    if (!insts || insts.length === 0) {
      list.appendChild(el('div', 'text-gray-400 text-sm', 'Нет инстансов'));
    } else {
      insts.forEach(inst => {
        const row = el('div', 'flex items-center justify-between gap-2 p-2 rounded border border-gray-700 bg-gray-800');
        const left = el('div', 'flex-1 text-sm');
        left.innerHTML = `<div class="text-xs text-gray-400">ID: ${inst.id}</div><div class="break-all">${inst.raw_uri || inst.label || '-'}</div>`;
        const delBtn = el('button', 'btn btn-danger btn-sm');
        delBtn.innerHTML = ''+icon('trash-2')+'';
        delBtn.title = 'Убрать из подписки';
        delBtn.onclick = async () => {
          if (!confirm('Убрать инстанс из подписки?')) return;
          await api('/subs/' + sub.slug + '/instances/' + inst.id, { method: 'DELETE' });
          showToast('Убрано'); closeModal(overlay); render();
        };
        row.appendChild(left);
        row.appendChild(delBtn);
        list.appendChild(row);
      });
    }
  } catch (e) {
    list.innerHTML = '';
    list.appendChild(el('div', 'text-red-400 text-sm', 'Ошибка: ' + e.message));
  }
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

function showImportSubModal() {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Импорт подписок</h3>';
  const ta = el('textarea', 'mb-3');
  ta.placeholder = 'Вставьте JSON с подписками...';
  ta.rows = 8;
  div.appendChild(ta);

  const cbRow = el('div', 'mb-3 flex items-center gap-2');
  const owCb = el('input', '');
  owCb.type = 'checkbox';
  cbRow.appendChild(owCb);
  cbRow.appendChild(el('label', 'text-sm', 'Перезаписать существующие'));
  div.appendChild(cbRow);

  const btnRow = el('div', 'flex gap-2 justify-end');
  const impBtn = el('button', 'btn btn-primary');
  impBtn.textContent = 'Импортировать';
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const overlay = showModal(div);

  impBtn.onclick = async () => {
    try {
      const data = JSON.parse(ta.value);
      const url = '/subs/import' + (owCb.checked ? '?overwrite=true' : '');
      const res = await api(url, { method: 'POST', body: JSON.stringify(data) });
      showToast('Импортировано: ' + (res.created || 0) + ' создано, ' + (res.skipped || 0) + ' пропущено');
      closeModal(overlay); render();
    } catch (e) {
      alert('Ошибка: ' + e.message);
    }
  };
  cancelBtn.onclick = () => closeModal(overlay);
  btnRow.appendChild(impBtn);
  btnRow.appendChild(cancelBtn);
  div.appendChild(btnRow);
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
