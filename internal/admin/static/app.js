// olcRTC Admin SPA
(function() {
'use strict';

const API = '/api';
let token = localStorage.getItem('olcrtc_token') || '';

// ── Network helper ───────────────────────────────────────────────────────────
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

// ── DOM helpers ──────────────────────────────────────────────────────────────
function el(type, cls, text) {
  const e = document.createElement(type);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

const ICONS = {
  'settings': '<path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/>',
  'log-out': '<path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/>',
  'copy': '<rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>',
  'qr-code': '<rect x="2" y="2" width="8" height="8"/><rect x="14" y="2" width="8" height="8"/><rect x="2" y="14" width="8" height="8"/><path d="M14 14h.01"/><path d="M18 14h.01"/><path d="M14 18h.01"/><path d="M18 18h.01"/><path d="M22 14v4a2 2 0 0 1-2 2h-2"/><path d="M10 22H6a2 2 0 0 1-2-2v-2"/>',
  'refresh-cw': '<path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M3 21v-5h5"/>',
  'square': '<rect x="3" y="3" width="18" height="18" rx="2" ry="2"/>',
  'play': '<polygon points="5 3 19 12 5 21 5 3"/>',
  'sliders': '<line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/>',
  'trash-2': '<polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/><line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/>',
  'plus': '<line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>',
  'eye': '<path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/>',
  'eye-off': '<path d="M17.94 17.94A10.94 10.94 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A10.94 10.94 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24"/><line x1="1" y1="1" x2="23" y2="23"/>',
  'arrow-left': '<line x1="19" y1="12" x2="5" y2="12"/><polyline points="12 19 5 12 12 5"/>',
  'alert-circle': '<circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/>',
  'alert-triangle': '<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
  'lock': '<rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/>',
  'unlock': '<rect x="3" y="11" width="18" height="11" rx="2" ry="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/>',
  'key': '<path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4"/>',
  'wifi': '<path d="M5 12.55a11 11 0 0 1 14.08 0"/><path d="M1.42 9a16 16 0 0 1 21.16 0"/><path d="M8.53 16.11a6 6 0 0 1 6.95 0"/><line x1="12" y1="20" x2="12.01" y2="20"/>',
  'tag': '<path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/>',
  'clock': '<circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>',
  'check-circle': '<path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>',
  'x-circle': '<circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/>',
  'chevron-down': '<polyline points="6 9 12 15 18 9"/>',
  'download': '<path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/>',
  'rotate-ccw': '<polyline points="1 4 1 10 7 10"/><path d="M3.51 15a9 9 0 1 0 2.13-9.36L1 10"/>',
  'shield': '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  'sliders-horizontal': '<line x1="21" y1="4" x2="14" y2="4"/><line x1="10" y1="4" x2="3" y2="4"/><line x1="21" y1="12" x2="12" y2="12"/><line x1="8" y1="12" x2="3" y2="12"/><line x1="21" y1="20" x2="16" y2="20"/><line x1="12" y1="20" x2="3" y2="20"/><line x1="14" y1="2" x2="14" y2="6"/><line x1="8" y1="10" x2="8" y2="14"/><line x1="16" y1="18" x2="16" y2="22"/>'
};

function icon(name, sz) {
  const size = sz || 16;
  const body = ICONS[name];
  if (!body) return '';
  return '<svg xmlns="http://www.w3.org/2000/svg" width="' + size + '" height="' + size + '" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">' + body + '</svg>';
}

function fmtStatusDot(st) {
  const map = { running: 'status-running', active: 'status-running', failed: 'status-failed' };
  return map[st] || 'status-inactive';
}

function fmtStatusPill(st) {
  if (st === 'running' || st === 'active') return { cls: 'status-pill-running', label: 'running' };
  if (st === 'failed') return { cls: 'status-pill-failed', label: 'failed' };
  if (st === 'unknown') return { cls: 'status-pill-inactive', label: 'unknown' };
  return { cls: 'status-pill-inactive', label: st || 'inactive' };
}

// Carrier alias used by salutejazz auth provider.
function isJazzCarrier(c) { return c === 'jazz' || c === 'salutejazz'; }

// ── Toast ────────────────────────────────────────────────────────────────────
function ensureToastContainer() {
  let c = document.getElementById('toast-container');
  if (!c) {
    c = el('div', 'toast-container');
    c.id = 'toast-container';
    document.body.appendChild(c);
  }
  return c;
}

function showToast(msg, kind) {
  const c = ensureToastContainer();
  const variant = kind || 'success';
  const t = el('div', 'toast toast-' + variant);
  const iconName = variant === 'error' ? 'x-circle' : variant === 'info' ? 'alert-circle' : 'check-circle';
  const iconSpan = el('span', 'toast-icon');
  iconSpan.innerHTML = icon(iconName, 16);
  t.appendChild(iconSpan);
  t.appendChild(el('span', '', msg));
  c.appendChild(t);
  setTimeout(() => {
    t.style.opacity = '0';
    t.style.transform = 'translateX(8px)';
    setTimeout(() => t.remove(), 250);
  }, 3000);
}

// ── Confirm modal ────────────────────────────────────────────────────────────
function showConfirm({ title, message, danger, confirmText, cancelText }) {
  return new Promise((resolve) => {
    const div = el('div', '');
    const h = el('h3', 'text-lg font-semibold mb-2');
    h.innerHTML = '<span class="inline-flex items-center gap-2">' + (danger ? icon('alert-triangle', 18) : icon('alert-circle', 18)) + '<span>' + (title || 'Подтверждение') + '</span></span>';
    div.appendChild(h);
    const body = el('div', 'text-sm text-gray-300 mb-4');
    body.textContent = message || '';
    div.appendChild(body);
    const row = el('div', 'flex gap-2 justify-end');
    const cancelBtn = el('button', 'btn btn-secondary');
    cancelBtn.textContent = cancelText || 'Отмена';
    const okBtn = el('button', danger ? 'btn btn-danger' : 'btn btn-primary');
    okBtn.textContent = confirmText || (danger ? 'Удалить' : 'OK');
    row.appendChild(cancelBtn);
    row.appendChild(okBtn);
    div.appendChild(row);

    const overlay = showModal(div, { small: true });
    function close(result) {
      document.removeEventListener('keydown', onKey);
      closeModal(overlay);
      resolve(result);
    }
    function onKey(e) {
      if (e.key === 'Escape') close(false);
      if (e.key === 'Enter') close(true);
    }
    document.addEventListener('keydown', onKey);
    overlay.dataset.onOutsideClose = 'cancel';
    overlay.addEventListener('outside-click', () => close(false));
    cancelBtn.onclick = () => close(false);
    okBtn.onclick = () => close(true);
    okBtn.focus();
  });
}

// ── Async button helper ──────────────────────────────────────────────────────
async function withLoading(btn, fn) {
  if (!btn) return fn();
  const orig = btn.innerHTML;
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span>';
  try {
    return await fn();
  } finally {
    btn.disabled = false;
    btn.innerHTML = orig;
  }
}

// ── Router ───────────────────────────────────────────────────────────────────
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
  const box = el('div', 'flex items-center justify-center min-h-screen p-4');
  const card = el('div', 'card p-8 w-full max-w-sm');
  const title = el('h1', 'text-2xl font-bold text-center mb-2');
  title.textContent = 'olcRTC Admin';
  const subtitle = el('p', 'text-center text-gray-400 text-sm mb-6');
  subtitle.textContent = 'Введите токен доступа';
  card.appendChild(title);
  card.appendChild(subtitle);

  const inp = el('input', '');
  inp.type = 'password';
  inp.placeholder = 'Токен';
  inp.setAttribute('aria-label', 'Токен доступа');
  inp.className = 'mb-3';

  const btn = el('button', 'btn btn-primary w-full');
  btn.textContent = 'Войти';

  const err = el('div', 'text-rose-400 text-sm mt-2 hidden');

  async function submit() {
    err.classList.add('hidden');
    await withLoading(btn, async () => {
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
    });
  }
  btn.onclick = submit;
  inp.onkeydown = (e) => { if (e.key === 'Enter') submit(); };

  card.appendChild(inp);
  card.appendChild(btn);
  card.appendChild(err);
  card.appendChild(el('p', 'text-gray-500 text-xs text-center mt-4', 'Токен хранится в /etc/olcrtc/admin.env'));
  box.appendChild(card);
  app.appendChild(box);
  setTimeout(() => inp.focus(), 0);
}

// ── Dashboard ────────────────────────────────────────────────────────────────
async function renderDashboard(app) {
  const wrap = el('div', 'max-w-6xl mx-auto p-4 md:p-6');

  // Header
  const header = el('div', 'flex items-center justify-between mb-6 flex-wrap gap-2');
  const titleWrap = el('div', 'flex items-center gap-2');
  titleWrap.innerHTML = '<span class="text-emerald-400">' + icon('shield', 22) + '</span><h1 class="text-xl md:text-2xl font-semibold">olcRTC Admin</h1>';
  header.appendChild(titleWrap);
  const nav = el('div', 'flex gap-2');
  const settingsBtn = el('button', 'btn btn-secondary btn-sm');
  settingsBtn.setAttribute('aria-label', 'Настройки');
  settingsBtn.innerHTML = icon('settings') + '<span class="hidden sm:inline">Настройки</span>';
  settingsBtn.onclick = () => route('/settings');
  const logoutBtn = el('button', 'btn btn-secondary btn-sm');
  logoutBtn.setAttribute('aria-label', 'Выход');
  logoutBtn.innerHTML = icon('log-out') + '<span class="hidden sm:inline">Выход</span>';
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
    <div class="grid grid-cols-2 md:grid-cols-4 gap-3 text-sm">
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">IP</div><div class="copyable">${sys.public_ip || '-'}</div></div>
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">OS</div><div>${sys.os || '-'}</div></div>
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">Uptime</div><div>${sys.uptime || '-'}</div></div>
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">TLS</div><div>${sys.tls_mode || '-'} ${sys.domain ? '('+sys.domain+')' : ''}</div></div>
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">Admin port</div><div>${sys.admin_port || '-'}</div></div>
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">Подписки</div><div>${sys.sub_enabled ? 'вкл ('+sys.sub_port+')' : 'выкл'}</div></div>
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">Инстансы</div><div>${sys.instances_running || 0}/${sys.instances_total || 0}</div></div>
      <div><div class="text-gray-500 text-xs uppercase tracking-wider mb-0.5">Версия</div><div>${sys.version || '-'}</div></div>
    </div>`;
  wrap.appendChild(sysCard);

  // Instances
  const instSection = el('div', 'mb-8');
  const instHeader = el('div', 'flex items-center justify-between mb-4');
  instHeader.innerHTML = '<h2 class="text-lg font-semibold">Инстансы</h2>';
  const addInstBtn = el('button', 'btn btn-primary btn-sm');
  addInstBtn.setAttribute('aria-label', 'Создать инстанс');
  addInstBtn.innerHTML = icon('plus') + '<span>Создать инстанс</span>';
  addInstBtn.onclick = async () => {
    await withLoading(addInstBtn, async () => {
      try {
        await api('/instances', { method: 'POST' });
        showToast('Инстанс создан');
        render();
      } catch (e) { showToast('Не удалось создать инстанс: ' + e.message, 'error'); }
    });
  };
  instHeader.appendChild(addInstBtn);
  instSection.appendChild(instHeader);

  const grid = el('div', 'grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4');
  instances.forEach(inst => grid.appendChild(renderInstanceCard(inst)));
  if (instances.length === 0) {
    const empty = el('div', 'card p-6 text-center text-gray-400 text-sm');
    empty.textContent = 'Инстансов нет. Создайте первый кнопкой выше.';
    instSection.appendChild(empty);
  } else {
    instSection.appendChild(grid);
  }
  wrap.appendChild(instSection);

  // Subscriptions
  const subSection = el('div', 'card p-4 mb-6');
  const subHeader = el('div', 'flex items-center justify-between mb-4');
  subHeader.innerHTML = '<h2 class="text-lg font-semibold">Подписки</h2>';
  const subActions = el('div', 'flex gap-2 flex-wrap');
  const addSubBtn = el('button', 'btn btn-primary btn-sm');
  addSubBtn.innerHTML = icon('plus') + '<span>Создать</span>';
  addSubBtn.onclick = () => showCreateSubModal();
  const exportBtn = el('button', 'btn btn-secondary btn-sm');
  exportBtn.innerHTML = icon('download') + '<span>Экспорт</span>';
  exportBtn.onclick = async () => {
    await withLoading(exportBtn, async () => {
      try {
        const data = await api('/subs/export');
        const blob = new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url; a.download = 'olcrtc-subscriptions.json'; a.click();
        URL.revokeObjectURL(url);
        showToast('Экспортировано');
      } catch (e) { showToast('Ошибка экспорта: ' + e.message, 'error'); }
    });
  };
  const importBtn = el('button', 'btn btn-secondary btn-sm');
  importBtn.textContent = 'Импорт';
  importBtn.onclick = () => showImportSubModal();
  subActions.appendChild(addSubBtn);
  subActions.appendChild(exportBtn);
  subActions.appendChild(importBtn);
  subHeader.appendChild(subActions);
  subSection.appendChild(subHeader);

  const subList = el('div', 'space-y-3');
  if (subsError) {
    subList.appendChild(el('div', 'text-amber-300 text-sm', 'Сервис подписок недоступен. Проверьте, что olcrtc-server запущен с OLCRTC_SUB_ENABLED=1.'));
  } else if (!subs || subs.length === 0) {
    subList.appendChild(el('div', 'text-gray-400 text-sm', 'Нет подписок'));
  } else {
    subs.forEach(sub => subList.appendChild(renderSubRow(sub, instances, sys)));
  }
  subSection.appendChild(subList);
  wrap.appendChild(subSection);

  app.appendChild(wrap);
}

function renderInstanceCard(inst) {
  const card = el('div', 'card card-hover p-4 flex flex-col gap-3');

  // Header: status + label
  const head = el('div', 'flex items-center justify-between gap-2');
  const left = el('div', 'flex items-center gap-2 min-w-0');
  const dot = el('span', 'status-dot ' + fmtStatusDot(inst.status));
  dot.setAttribute('aria-hidden', 'true');
  const labelWrap = el('div', 'flex flex-col min-w-0');
  const labelEl = el('div', 'font-semibold truncate');
  labelEl.textContent = inst.label;
  const idEl = el('div', 'text-xs text-gray-500');
  idEl.textContent = '#' + inst.id + ' · ' + (inst.name || '');
  labelWrap.appendChild(labelEl);
  labelWrap.appendChild(idEl);
  left.appendChild(dot);
  left.appendChild(labelWrap);
  const pill = fmtStatusPill(inst.status);
  const pillEl = el('span', 'status-pill ' + pill.cls);
  pillEl.textContent = pill.label;
  head.appendChild(left);
  head.appendChild(pillEl);
  card.appendChild(head);

  // Badges
  const badges = el('div', 'flex flex-wrap gap-1.5');
  const carrierBadge = el('span', 'badge badge-blue');
  carrierBadge.innerHTML = icon('tag', 12) + '<span>' + (inst.carrier || '-') + '</span>';
  const transportBadge = el('span', 'badge');
  transportBadge.innerHTML = icon('wifi', 12) + '<span>' + (inst.transport || '-') + '</span>';
  badges.appendChild(carrierBadge);
  badges.appendChild(transportBadge);
  if (isJazzCarrier(inst.carrier)) {
    const passBadge = el('span', 'badge ' + (inst.has_password ? 'badge-emerald' : 'badge-amber'));
    passBadge.innerHTML = (inst.has_password ? icon('lock', 12) : icon('unlock', 12)) +
      '<span>' + (inst.has_password ? 'password set' : 'no password') + '</span>';
    badges.appendChild(passBadge);
  }
  if (inst.uptime) {
    const upBadge = el('span', 'badge');
    upBadge.innerHTML = icon('clock', 12) + '<span>' + inst.uptime + '</span>';
    badges.appendChild(upBadge);
  }
  card.appendChild(badges);

  // Room ID + Client ID rows
  const meta = el('div', 'space-y-1.5 text-xs');
  meta.appendChild(metaRow('Room ID', inst.room_id || '—', inst.room_id));
  if (inst.client_id) {
    meta.appendChild(metaRow('Client ID', inst.client_id, inst.client_id));
  }
  card.appendChild(meta);

  // Actions
  const actions = el('div', 'flex flex-wrap gap-1.5 mt-1');
  const uriBtn = el('button', 'btn btn-secondary btn-sm');
  uriBtn.setAttribute('aria-label', 'Копировать URI');
  uriBtn.innerHTML = icon('copy') + '<span>URI</span>';
  uriBtn.onclick = () => { navigator.clipboard.writeText(inst.uri); showToast('URI скопирован'); };
  const qrBtn = el('button', 'btn btn-secondary btn-sm');
  qrBtn.setAttribute('aria-label', 'Показать QR-код');
  qrBtn.innerHTML = icon('qr-code') + '<span>QR</span>';
  qrBtn.onclick = () => showQRModal(inst.uri);
  const cfgBtn = el('button', 'btn btn-secondary btn-sm');
  cfgBtn.setAttribute('aria-label', 'Настройки инстанса');
  cfgBtn.innerHTML = icon('sliders') + '<span>Настройки</span>';
  cfgBtn.onclick = () => showConfigModal(inst);

  const startStopBtn = el('button', inst.status === 'running' ? 'btn btn-secondary btn-sm btn-icon' : 'btn btn-success btn-sm btn-icon');
  startStopBtn.setAttribute('aria-label', inst.status === 'running' ? 'Остановить' : 'Запустить');
  startStopBtn.title = inst.status === 'running' ? 'Остановить' : 'Запустить';
  startStopBtn.innerHTML = inst.status === 'running' ? icon('square') : icon('play');
  startStopBtn.onclick = async () => {
    await withLoading(startStopBtn, async () => {
      try {
        const action = inst.status === 'running' ? 'stop' : 'start';
        await api('/instances/' + inst.id + '/' + action, { method: 'POST' });
        showToast(action === 'stop' ? 'Остановлено' : 'Запущено');
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };
  const restartBtn = el('button', 'btn btn-secondary btn-sm btn-icon');
  restartBtn.setAttribute('aria-label', 'Перезапустить');
  restartBtn.title = 'Перезапустить';
  restartBtn.innerHTML = icon('refresh-cw');
  restartBtn.onclick = async () => {
    await withLoading(restartBtn, async () => {
      try {
        await api('/instances/' + inst.id + '/restart', { method: 'POST' });
        showToast('Перезапущено');
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };
  actions.appendChild(uriBtn);
  actions.appendChild(qrBtn);
  actions.appendChild(cfgBtn);
  actions.appendChild(startStopBtn);
  actions.appendChild(restartBtn);
  if (inst.id !== 0) {
    const delBtn = el('button', 'btn btn-danger btn-sm btn-icon');
    delBtn.setAttribute('aria-label', 'Удалить инстанс');
    delBtn.title = 'Удалить инстанс';
    delBtn.innerHTML = icon('trash-2');
    delBtn.onclick = async () => {
      const ok = await showConfirm({
        title: 'Удалить инстанс #' + inst.id + '?',
        message: 'Сервис будет остановлен, env-файл удалён. Это действие необратимо.',
        danger: true,
        confirmText: 'Удалить',
      });
      if (!ok) return;
      try {
        await api('/instances/' + inst.id, { method: 'DELETE' });
        showToast('Удалено');
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    };
    actions.appendChild(delBtn);
  }
  card.appendChild(actions);
  return card;
}

function metaRow(label, value, copyValue) {
  const row = el('div', 'flex items-center justify-between gap-2');
  row.appendChild(el('span', 'text-gray-500', label));
  const right = el('div', 'flex items-center gap-1.5 min-w-0');
  const valEl = el('span', 'copyable text-gray-300 truncate');
  valEl.title = value;
  valEl.textContent = value;
  right.appendChild(valEl);
  if (copyValue) {
    const cb = el('button', 'btn btn-ghost btn-sm btn-icon');
    cb.setAttribute('aria-label', 'Копировать ' + label);
    cb.title = 'Копировать';
    cb.innerHTML = icon('copy', 14);
    cb.onclick = (e) => { e.stopPropagation(); navigator.clipboard.writeText(copyValue); showToast(label + ' скопирован'); };
    right.appendChild(cb);
  }
  row.appendChild(right);
  return row;
}

function renderSubRow(sub, instances, sys) {
  const row = el('div', 'card p-3 flex flex-col md:flex-row md:items-center justify-between gap-2');
  const subURL = (sys.admin_url || location.origin) + '/sub/' + sub.slug;
  const left = el('div', 'flex-1 min-w-0');
  left.innerHTML = `
    <div class="font-medium">${sub.name} <span class="text-gray-500">[${sub.slug}]</span></div>
    <div class="text-gray-400 text-xs mt-1 copyable truncate" title="${subURL}">${subURL}</div>
  `;
  const right = el('div', 'flex gap-1.5 flex-wrap');
  const viewBtn = el('button', 'btn btn-secondary btn-sm');
  viewBtn.innerHTML = icon('eye') + '<span>Просмотр</span>';
  viewBtn.onclick = () => window.open(subURL, '_blank');
  const instBtn = el('button', 'btn btn-secondary btn-sm');
  instBtn.innerHTML = icon('settings') + '<span>Инстансы</span>';
  instBtn.onclick = () => showSubInstancesModal(sub);
  const addBtn = el('button', 'btn btn-secondary btn-sm');
  addBtn.innerHTML = icon('plus') + '<span>Добавить</span>';
  addBtn.onclick = () => showAddToSubModal(sub, instances);
  const delBtn = el('button', 'btn btn-danger btn-sm btn-icon');
  delBtn.setAttribute('aria-label', 'Удалить подписку');
  delBtn.title = 'Удалить';
  delBtn.innerHTML = icon('trash-2');
  delBtn.onclick = async () => {
    const ok = await showConfirm({
      title: 'Удалить подписку «' + sub.name + '»?',
      message: 'Все инстансы в этой подписке будут отвязаны. URL подписки перестанет работать.',
      danger: true,
    });
    if (!ok) return;
    try {
      await api('/subs/' + sub.slug, { method: 'DELETE' });
      showToast('Подписка удалена');
      render();
    } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
  };
  right.appendChild(viewBtn);
  right.appendChild(instBtn);
  right.appendChild(addBtn);
  right.appendChild(delBtn);
  row.appendChild(left);
  row.appendChild(right);
  return row;
}

// ── Settings page ────────────────────────────────────────────────────────────
async function renderSettings(app) {
  const wrap = el('div', 'max-w-2xl mx-auto p-4 md:p-6');

  const header = el('div', 'flex items-center justify-between mb-6');
  header.innerHTML = '<h1 class="text-xl md:text-2xl font-semibold">Настройки</h1>';
  const backBtn = el('button', 'btn btn-secondary btn-sm');
  backBtn.innerHTML = icon('arrow-left') + '<span>Назад</span>';
  backBtn.onclick = () => route('/');
  header.appendChild(backBtn);
  wrap.appendChild(header);

  let sys = {};
  try { sys = await api('/system/status'); } catch (e) {}

  const card = el('div', 'card p-5 space-y-6');

  // Domain
  const domBlock = el('div', '');
  domBlock.innerHTML = '<h3 class="font-semibold mb-2 inline-flex items-center gap-2">' + icon('shield', 16) + '<span>Домен</span></h3>';
  const domCurrent = el('div', 'text-sm text-gray-400 mb-2', sys.domain ? 'Текущий: ' + sys.domain : 'Текущий: (не привязан)');
  const domInp = el('input', '');
  domInp.placeholder = 'sub.example.com';
  domInp.setAttribute('aria-label', 'Домен');
  const domRow = el('div', 'flex gap-2 mt-2 flex-wrap');
  const domBtn = el('button', 'btn btn-primary');
  domBtn.textContent = 'Привязать';
  domBtn.onclick = async () => {
    await withLoading(domBtn, async () => {
      try {
        const res = await api('/system/domain', { method: 'POST', body: JSON.stringify({ domain: domInp.value }) });
        showToast(res.message || 'Домен привязан');
        render();
      } catch (e) {
        try { const err = JSON.parse(e.message); showToast(err.message || e.message, 'error'); }
        catch { showToast(e.message, 'error'); }
      }
    });
  };
  domRow.appendChild(domBtn);
  if (sys.domain) {
    const unbindBtn = el('button', 'btn btn-danger');
    unbindBtn.textContent = 'Отвязать';
    unbindBtn.onclick = async () => {
      const ok = await showConfirm({ title: 'Отвязать домен?', message: 'Сервер вернётся к self-signed сертификату после перезапуска.', danger: true });
      if (!ok) return;
      await api('/system/domain', { method: 'DELETE' });
      render();
    };
    domRow.appendChild(unbindBtn);
  }
  domBlock.appendChild(domCurrent);
  domBlock.appendChild(domInp);
  domBlock.appendChild(domRow);
  card.appendChild(domBlock);

  // Ports
  const portBlock = el('div', '');
  portBlock.innerHTML = `<h3 class="font-semibold mb-2 inline-flex items-center gap-2">${icon('wifi', 16)}<span>Порты</span></h3>
    <div class="text-sm text-gray-300">Admin UI: <span class="copyable">${sys.admin_port || '-'}</span></div>
    <div class="text-sm text-gray-300">Подписки: <span class="copyable">${sys.sub_port || '-'}</span></div>`;
  card.appendChild(portBlock);

  // Security
  const secBlock = el('div', '');
  secBlock.innerHTML = '<h3 class="font-semibold mb-2 inline-flex items-center gap-2">' + icon('key', 16) + '<span>Безопасность</span></h3>';
  const changeTokenBtn = el('button', 'btn btn-secondary');
  changeTokenBtn.textContent = 'Сменить токен';
  changeTokenBtn.onclick = async () => {
    const ok = await showConfirm({ title: 'Сменить токен?', message: 'Старый токен перестанет работать. Сохраните новый сразу — он показывается только один раз.', danger: true, confirmText: 'Сменить' });
    if (!ok) return;
    try {
      const res = await api('/auth/change-token', { method: 'POST', body: JSON.stringify({}) });
      token = res.token;
      localStorage.setItem('olcrtc_token', token);
      showTokenModal(res.token);
    } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
  };
  secBlock.appendChild(changeTokenBtn);
  card.appendChild(secBlock);

  // Logs
  const logBlock = el('div', '');
  logBlock.innerHTML = '<h3 class="font-semibold mb-2 inline-flex items-center gap-2">' + icon('sliders-horizontal', 16) + '<span>Логи</span></h3>';
  const logsWrap = el('div', 'flex gap-2 mb-2 flex-wrap');
  ['olcrtc-server', 'olcrtc-admin'].forEach(svc => {
    const btn = el('button', 'btn btn-secondary btn-sm');
    btn.textContent = svc;
    btn.onclick = () => showLogsModal(svc);
    logsWrap.appendChild(btn);
  });
  logBlock.appendChild(logsWrap);
  card.appendChild(logBlock);

  wrap.appendChild(card);
  app.appendChild(wrap);
}

function showTokenModal(tok) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Новый токен</h3><p class="text-sm text-gray-400 mb-3">Сохраните токен — он не будет показан снова.</p>';
  const inp = el('input', 'mb-3');
  inp.value = tok;
  inp.readOnly = true;
  div.appendChild(inp);
  const row = el('div', 'flex gap-2 justify-end');
  const copyBtn = el('button', 'btn btn-secondary');
  copyBtn.textContent = 'Копировать';
  copyBtn.onclick = () => { navigator.clipboard.writeText(tok); showToast('Токен скопирован'); };
  const closeBtn = el('button', 'btn btn-primary');
  closeBtn.textContent = 'Закрыть';
  row.appendChild(copyBtn);
  row.appendChild(closeBtn);
  div.appendChild(row);
  const overlay = showModal(div, { small: true });
  closeBtn.onclick = () => closeModal(overlay);
}

// ── Modals ───────────────────────────────────────────────────────────────────
function showModal(content, opts) {
  opts = opts || {};
  const overlay = el('div', 'modal-overlay');
  const modal = el('div', 'modal' + (opts.small ? ' modal-sm' : ''));
  modal.appendChild(content);
  overlay.appendChild(modal);
  document.body.appendChild(overlay);
  overlay.onclick = (e) => {
    if (e.target === overlay) {
      const evt = new Event('outside-click');
      overlay.dispatchEvent(evt);
      if (!overlay.dataset.onOutsideClose || overlay.dataset.onOutsideClose !== 'cancel') {
        overlay.remove();
      }
    }
  };
  return overlay;
}

function closeModal(overlay) { if (overlay && overlay.parentNode) overlay.remove(); }

function showQRModal(uri) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3 inline-flex items-center gap-2">' + icon('qr-code', 18) + '<span>QR-код</span></h3>';
  const qrWrap = el('div', 'qr-wrap flex justify-center mb-3 mx-auto');
  const qrDiv = el('div', '');
  qrWrap.appendChild(qrDiv);
  div.appendChild(qrWrap);
  const uriText = el('div', 'text-xs text-gray-400 break-all mb-3 copyable', uri);
  div.appendChild(uriText);
  const btnRow = el('div', 'flex gap-2 justify-end flex-wrap');
  const copyBtn = el('button', 'btn btn-secondary btn-sm');
  copyBtn.innerHTML = icon('copy') + '<span>Копировать URI</span>';
  copyBtn.onclick = () => { navigator.clipboard.writeText(uri); showToast('Скопировано'); };
  const downloadBtn = el('button', 'btn btn-secondary btn-sm');
  downloadBtn.innerHTML = icon('download') + '<span>Скачать PNG</span>';
  const closeBtn = el('button', 'btn btn-primary btn-sm');
  closeBtn.textContent = 'Закрыть';
  btnRow.appendChild(downloadBtn);
  btnRow.appendChild(copyBtn);
  btnRow.appendChild(closeBtn);
  div.appendChild(btnRow);

  const overlay = showModal(div);
  closeBtn.onclick = () => closeModal(overlay);

  setTimeout(() => {
    new QRCode(qrDiv, { text: uri, width: 280, height: 280, colorDark: '#000000', colorLight: '#ffffff', correctLevel: QRCode.CorrectLevel.H });
    downloadBtn.onclick = () => {
      const canvas = qrDiv.querySelector('canvas');
      if (!canvas) { showToast('Не удалось получить QR canvas', 'error'); return; }
      canvas.toBlob((blob) => {
        if (!blob) { showToast('Не удалось сгенерировать PNG', 'error'); return; }
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url; a.download = 'olcrtc-qr.png'; a.click();
        URL.revokeObjectURL(url);
        showToast('PNG сохранён');
      }, 'image/png');
    };
  }, 0);
}

// ── Instance config modal ────────────────────────────────────────────────────
function showConfigModal(inst) {
  const div = el('div', '');
  const titleRow = el('div', 'flex items-center gap-2 mb-4');
  titleRow.innerHTML = '<span class="text-emerald-400">' + icon('sliders', 18) + '</span><h3 class="text-lg font-semibold">Настройка инстанса #' + inst.id + '</h3>';
  div.appendChild(titleRow);

  // ── Connection section ──
  const connectionSec = el('div', 'section mb-3');
  const connTitle = el('div', 'section-title flex items-center gap-1.5');
  connTitle.innerHTML = icon('wifi', 12) + '<span>Connection</span>';
  connectionSec.appendChild(connTitle);
  const connGrid = el('div', 'grid grid-cols-1 md:grid-cols-2 gap-3');

  const carrierField = makeSelectField('Carrier', icon('tag', 14), inst.carrier || 'wbstream', ['wbstream', 'jazz', 'telemost']);
  const transportField = makeSelectField('Transport', icon('wifi', 14), inst.transport || 'datachannel', ['datachannel', 'vp8channel', 'seichannel']);
  const nameField = makeInputField('Имя', icon('tag', 14), inst.name || '', { placeholder: 'имя инстанса' });
  const roomIDField = makeInputField('Room ID', icon('tag', 14), inst.room_id || '', { placeholder: 'для wbstream — создать на stream.wb.ru' });
  // Room password — только для jazz/salutejazz, с toggle visibility и отдельным реквестом за значением
  const roomPasswordWrap = makePasswordField('Room password', icon('lock', 14), inst.has_password, async () => {
    try {
      const res = await api('/instances/' + inst.id + '/room-password');
      return res.room_password || '';
    } catch (e) {
      showToast('Не удалось получить password: ' + e.message, 'error');
      return '';
    }
  });
  // Client ID — read-only с кнопкой rotate
  const clientIDWrap = makeReadonlyWithRotate('Client ID', icon('shield', 14), inst.client_id || '(не задан)', async (rotateBtn) => {
    const ok = await showConfirm({
      title: 'Ротация Client ID?',
      message: 'Все клиенты, импортировавшие предыдущий URI, должны импортировать новый. Текущие соединения будут прерваны при перезапуске сервиса.',
      danger: true,
      confirmText: 'Ротировать',
    });
    if (!ok) return;
    await withLoading(rotateBtn, async () => {
      try {
        const res = await api('/instances/' + inst.id + '/rotate-client-id', { method: 'POST' });
        showToast('Client ID обновлён');
        closeModal(overlay);
        render();
        // Open modal again with new value? Caller decides; we just rerender.
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  });
  const keyRotateBtn = el('button', 'btn btn-danger btn-sm w-full');
  keyRotateBtn.innerHTML = icon('refresh-cw') + '<span>Пересоздать ключ</span>';
  keyRotateBtn.onclick = async () => {
    const ok = await showConfirm({
      title: 'Пересоздать ключ?',
      message: 'Старый ключ перестанет работать. Клиенты должны импортировать новый URI.',
      danger: true,
      confirmText: 'Пересоздать',
    });
    if (!ok) return;
    await withLoading(keyRotateBtn, async () => {
      try {
        await api('/instances/' + inst.id + '/rotate-key', { method: 'POST' });
        showToast('Ключ пересоздан');
        closeModal(overlay);
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };
  const roomRotateBtn = el('button', 'btn btn-secondary btn-sm w-full');
  roomRotateBtn.innerHTML = icon('rotate-ccw') + '<span>Пересоздать Room ID</span>';
  roomRotateBtn.onclick = async () => {
    const ok = await showConfirm({
      title: 'Пересоздать Room ID?',
      message: 'Сервер создаст новую комнату при следующем подключении. Для jazz пароль также будет очищен.',
      danger: true,
    });
    if (!ok) return;
    await withLoading(roomRotateBtn, async () => {
      try {
        await api('/instances/' + inst.id + '/rotate-room', { method: 'POST' });
        showToast('Room ID пересоздан');
        closeModal(overlay);
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };

  connGrid.appendChild(carrierField.field);
  connGrid.appendChild(transportField.field);
  connGrid.appendChild(nameField.field);
  connGrid.appendChild(roomIDField.field);
  connGrid.appendChild(roomPasswordWrap.field);
  connGrid.appendChild(clientIDWrap.field);
  connectionSec.appendChild(connGrid);

  const rotateRow = el('div', 'grid grid-cols-1 sm:grid-cols-2 gap-2 mt-3');
  rotateRow.appendChild(keyRotateBtn);
  rotateRow.appendChild(roomRotateBtn);
  connectionSec.appendChild(rotateRow);
  div.appendChild(connectionSec);

  // ── Network section ──
  const networkSec = el('div', 'section mb-3');
  const netTitle = el('div', 'section-title flex items-center gap-1.5');
  netTitle.innerHTML = icon('wifi', 12) + '<span>Network</span>';
  networkSec.appendChild(netTitle);
  const netGrid = el('div', 'grid grid-cols-1 md:grid-cols-2 gap-3');
  const dnsField = makeInputField('DNS', icon('wifi', 14), inst.dns || '', { placeholder: '1.1.1.1:53' });
  const socksField = makeInputField('SOCKS proxy', icon('shield', 14), inst.socks_proxy || '', { placeholder: 'socks5://user:pass@host:port' });
  const warpField = makeInputField('WARP proxy', icon('shield', 14), inst.warp_proxy || '', { placeholder: '127.0.0.1:40000' });
  netGrid.appendChild(dnsField.field);
  netGrid.appendChild(socksField.field);
  netGrid.appendChild(warpField.field);
  networkSec.appendChild(netGrid);
  const proxyHint = el('div', 'mt-3 text-xs text-gray-500');
  proxyHint.innerHTML = 'SOCKS — для signaling. WARP — для клиентского трафика (отдельный SOCKS5).';
  networkSec.appendChild(proxyHint);
  div.appendChild(networkSec);

  // ── Advanced section (transport-specific + debug) ──
  const advSec = el('div', 'section mb-3');
  const advHeader = el('div', 'flex items-center justify-between cursor-pointer');
  const advTitle = el('div', 'section-title flex items-center gap-1.5 mb-0');
  advTitle.innerHTML = icon('sliders-horizontal', 12) + '<span>Advanced</span>';
  const chevron = el('span', 'text-gray-500');
  chevron.innerHTML = icon('chevron-down', 14);
  advHeader.appendChild(advTitle);
  advHeader.appendChild(chevron);
  advSec.appendChild(advHeader);
  const advBody = el('div', 'mt-3');

  const debugRow = el('label', 'flex items-center gap-2 cursor-pointer mb-3 text-sm');
  const debugCb = el('input', '');
  debugCb.type = 'checkbox';
  debugCb.checked = inst.debug || false;
  debugCb.style.width = 'auto';
  debugCb.style.minHeight = 'auto';
  debugRow.appendChild(debugCb);
  debugRow.appendChild(el('span', '', 'Debug logging'));
  advBody.appendChild(debugRow);

  const vp8Block = el('div', 'border border-gray-700 rounded-lg p-3 mb-3 hidden');
  vp8Block.innerHTML = '<div class="text-xs text-gray-400 mb-2">VP8 параметры</div>';
  const vp8Grid = el('div', 'grid grid-cols-2 gap-2');
  const vp8FpsInp = el('input', ''); vp8FpsInp.placeholder = 'FPS (30)';
  const vp8BatchInp = el('input', ''); vp8BatchInp.placeholder = 'Batch (2)';
  vp8Grid.appendChild(vp8FpsInp);
  vp8Grid.appendChild(vp8BatchInp);
  vp8Block.appendChild(vp8Grid);
  advBody.appendChild(vp8Block);

  const seiBlock = el('div', 'border border-gray-700 rounded-lg p-3 hidden');
  seiBlock.innerHTML = '<div class="text-xs text-gray-400 mb-2">SEI параметры</div>';
  const seiGrid = el('div', 'grid grid-cols-2 gap-2');
  const seiFpsInp = el('input', ''); seiFpsInp.placeholder = 'FPS (20)';
  const seiBatchInp = el('input', ''); seiBatchInp.placeholder = 'Batch (1)';
  const seiFragInp = el('input', ''); seiFragInp.placeholder = 'Fragment (900)';
  const seiAckInp = el('input', ''); seiAckInp.placeholder = 'ACK ms (3000)';
  seiGrid.appendChild(seiFpsInp); seiGrid.appendChild(seiBatchInp);
  seiGrid.appendChild(seiFragInp); seiGrid.appendChild(seiAckInp);
  seiBlock.appendChild(seiGrid);
  advBody.appendChild(seiBlock);

  advSec.appendChild(advBody);
  let advOpen = true;
  function setAdvOpen(open) {
    advOpen = open;
    advBody.classList.toggle('hidden', !open);
    chevron.style.transform = open ? '' : 'rotate(-90deg)';
  }
  advHeader.onclick = () => setAdvOpen(!advOpen);
  setAdvOpen(true);
  div.appendChild(advSec);

  // wbstream hint
  const wbHint = el('div', 'mb-3 text-xs text-amber-300 bg-amber-900/30 border border-amber-700/40 p-3 rounded-lg');
  wbHint.innerHTML = '<b>WB Stream больше не создаёт румы автоматически.</b> Создайте руму на <a href="https://stream.wb.ru" target="_blank" rel="noopener" class="underline">stream.wb.ru</a> и вставьте её ID в поле <b>Room ID</b>.';
  div.appendChild(wbHint);

  // Conditional visibility
  function updateVisibility() {
    const t = transportField.input.value;
    const c = carrierField.input.value;
    vp8Block.classList.toggle('hidden', t !== 'vp8channel');
    seiBlock.classList.toggle('hidden', t !== 'seichannel');
    wbHint.classList.toggle('hidden', c !== 'wbstream');
    roomPasswordWrap.field.classList.toggle('hidden', !isJazzCarrier(c));
    roomRotateBtn.disabled = (c === 'wbstream');
    roomRotateBtn.title = (c === 'wbstream') ? 'WB Stream отключил автосоздание румы' : '';
  }
  carrierField.input.addEventListener('change', updateVisibility);
  transportField.input.addEventListener('change', updateVisibility);
  updateVisibility();

  // Footer actions
  const btnRow = el('div', 'flex gap-2 justify-end mt-2');
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const saveBtn = el('button', 'btn btn-primary');
  saveBtn.textContent = 'Сохранить';
  btnRow.appendChild(cancelBtn);
  btnRow.appendChild(saveBtn);
  div.appendChild(btnRow);

  const overlay = showModal(div);
  cancelBtn.onclick = () => closeModal(overlay);
  saveBtn.onclick = async () => {
    const carrier = carrierField.input.value;
    const room = roomIDField.input.value.trim();
    if (carrier === 'wbstream' && !room) {
      showToast('Для wbstream нужно указать Room ID', 'error');
      return;
    }
    if (isJazzCarrier(carrier) && !room && roomPasswordWrap.getValue()) {
      showToast('Room ID требуется при заданном Room password', 'error');
      return;
    }
    const body = {
      carrier,
      transport: transportField.input.value,
      name: nameField.input.value,
      room_id: room,
      dns: dnsField.input.value,
      socks_proxy: socksField.input.value,
      warp_proxy: warpField.input.value,
      debug: debugCb.checked,
    };
    if (isJazzCarrier(carrier)) {
      const rp = roomPasswordWrap.getValue();
      // Only include room_password if user actually edited it; otherwise the
      // backend keeps the existing value untouched.
      if (rp !== null) body.room_password = rp;
    }
    if (!vp8Block.classList.contains('hidden')) {
      if (vp8FpsInp.value) body.vp8_fps = parseInt(vp8FpsInp.value, 10);
      if (vp8BatchInp.value) body.vp8_batch = parseInt(vp8BatchInp.value, 10);
    }
    if (!seiBlock.classList.contains('hidden')) {
      if (seiFpsInp.value) body.sei_fps = parseInt(seiFpsInp.value, 10);
      if (seiBatchInp.value) body.sei_batch = parseInt(seiBatchInp.value, 10);
      if (seiFragInp.value) body.sei_frag = parseInt(seiFragInp.value, 10);
      if (seiAckInp.value) body.sei_ack_ms = parseInt(seiAckInp.value, 10);
    }
    await withLoading(saveBtn, async () => {
      try {
        await api('/instances/' + inst.id + '/config', { method: 'PUT', body: JSON.stringify(body) });
        showToast('Сохранено');
        closeModal(overlay);
        render();
      } catch (e) { showToast(e.message || 'Не удалось сохранить', 'error'); }
    });
  };
}

// ── Form-field factories ─────────────────────────────────────────────────────
function makeFieldShell(label, iconHTML) {
  const field = el('div', 'field');
  const labelEl = el('label', 'field-label');
  labelEl.innerHTML = (iconHTML || '') + '<span>' + label + '</span>';
  field.appendChild(labelEl);
  return { field, labelEl };
}

function makeInputField(label, iconHTML, value, opts) {
  const { field, labelEl } = makeFieldShell(label, iconHTML);
  const input = el('input', '');
  input.value = value || '';
  if (opts && opts.placeholder) input.placeholder = opts.placeholder;
  if (opts && opts.readonly) { input.readOnly = true; }
  const inputID = 'fld-' + Math.random().toString(36).slice(2, 9);
  input.id = inputID;
  labelEl.setAttribute('for', inputID);
  field.appendChild(input);
  return { field, input };
}

function makeSelectField(label, iconHTML, value, options) {
  const { field, labelEl } = makeFieldShell(label, iconHTML);
  const input = el('select', '');
  options.forEach(o => {
    const opt = el('option', '', o);
    opt.value = o;
    if (o === value) opt.selected = true;
    input.appendChild(opt);
  });
  const inputID = 'fld-' + Math.random().toString(36).slice(2, 9);
  input.id = inputID;
  labelEl.setAttribute('for', inputID);
  field.appendChild(input);
  return { field, input };
}

// makePasswordField returns a field for OLCRTC_ROOM_PASSWORD with a
// "show" toggle that fetches the current value lazily. The actual
// password is never embedded in the inventory JSON; the show toggle
// makes a separate authenticated request via fetchCurrent().
//
// getValue() returns:
//   null    if user did not interact with the field (caller should not
//           include room_password in the PUT body — keeps existing value)
//   string  if user typed/cleared the field (caller should include it)
function makePasswordField(label, iconHTML, hasPasswordInitially, fetchCurrent) {
  const { field, labelEl } = makeFieldShell(label, iconHTML);
  const row = el('div', 'field-row');
  const input = el('input', '');
  input.type = 'password';
  input.placeholder = hasPasswordInitially ? '••••••••' : 'без пароля';
  input.autocomplete = 'new-password';
  let dirty = false;
  let revealed = false;
  input.addEventListener('input', () => { dirty = true; });

  const toggleBtn = el('button', 'btn btn-secondary btn-icon');
  toggleBtn.type = 'button';
  toggleBtn.setAttribute('aria-label', 'Показать пароль');
  toggleBtn.title = 'Показать';
  toggleBtn.innerHTML = icon('eye', 16);
  toggleBtn.onclick = async () => {
    if (revealed) {
      input.type = 'password';
      toggleBtn.innerHTML = icon('eye', 16);
      revealed = false;
      return;
    }
    if (!dirty && !input.value && hasPasswordInitially) {
      // Fetch current value lazily.
      await withLoading(toggleBtn, async () => {
        const v = await fetchCurrent();
        input.value = v;
        // Mark NOT dirty so an unmodified reveal does not write back the
        // same value; if user edits, dirty flips to true automatically.
        dirty = false;
      });
    }
    input.type = 'text';
    toggleBtn.innerHTML = icon('eye-off', 16);
    revealed = true;
  };
  row.appendChild(input);
  row.appendChild(toggleBtn);
  field.appendChild(row);
  return {
    field,
    input,
    getValue: () => dirty ? input.value : null,
  };
}

function makeReadonlyWithRotate(label, iconHTML, value, onRotate) {
  const { field, labelEl } = makeFieldShell(label, iconHTML);
  const row = el('div', 'field-row');
  const input = el('input', '');
  input.value = value;
  input.readOnly = true;
  const copyBtn = el('button', 'btn btn-secondary btn-icon');
  copyBtn.type = 'button';
  copyBtn.title = 'Копировать';
  copyBtn.setAttribute('aria-label', 'Копировать ' + label);
  copyBtn.innerHTML = icon('copy', 16);
  copyBtn.onclick = () => { navigator.clipboard.writeText(value); showToast(label + ' скопирован'); };
  const rotateBtn = el('button', 'btn btn-secondary btn-icon');
  rotateBtn.type = 'button';
  rotateBtn.title = 'Ротация';
  rotateBtn.setAttribute('aria-label', 'Ротация ' + label);
  rotateBtn.innerHTML = icon('rotate-ccw', 16);
  rotateBtn.onclick = () => onRotate(rotateBtn);
  row.appendChild(input);
  row.appendChild(copyBtn);
  row.appendChild(rotateBtn);
  field.appendChild(row);
  const hint = el('div', 'text-xs text-gray-500 mt-1', 'Управляется кнопкой ротации');
  field.appendChild(hint);
  return { field, input };
}

// ── Subscription modals (kept conceptually identical to the old SPA) ────────
function showAddToSubModal(sub, instances) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Добавить инстанс в подписку «' + sub.name + '»</h3>';
  const list = el('div', 'space-y-2 mb-3');
  const radios = [];
  instances.forEach(inst => {
    const row = el('label', 'radio-row');
    const rb = el('input', ''); rb.type = 'radio'; rb.name = 'inst'; rb.value = inst.id;
    rb.style.width = 'auto'; rb.style.minHeight = 'auto';
    row.appendChild(rb);
    row.appendChild(el('span', 'text-sm', inst.label + ' — ' + inst.carrier + ' / ' + inst.transport));
    list.appendChild(row);
    radios.push(rb);
  });
  const manualRow = el('label', 'radio-row');
  const manualRb = el('input', ''); manualRb.type = 'radio'; manualRb.name = 'inst'; manualRb.value = 'manual';
  manualRb.style.width = 'auto'; manualRb.style.minHeight = 'auto';
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
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const addBtn = el('button', 'btn btn-primary');
  addBtn.textContent = 'Добавить';
  btnRow.appendChild(cancelBtn);
  btnRow.appendChild(addBtn);
  div.appendChild(btnRow);

  const overlay = showModal(div);
  cancelBtn.onclick = () => closeModal(overlay);
  addBtn.onclick = async () => {
    let rawUri = '';
    const checked = radios.find(r => r.checked);
    if (!checked) { showToast('Выберите инстанс', 'error'); return; }
    if (checked.value === 'manual') rawUri = manualInp.value;
    else {
      const inst = instances.find(i => String(i.id) === checked.value);
      rawUri = inst ? inst.uri : '';
    }
    if (!rawUri) { showToast('URI не может быть пустым', 'error'); return; }
    await withLoading(addBtn, async () => {
      try {
        await api('/subs/' + sub.slug + '/instances', { method: 'POST', body: JSON.stringify({ raw_uri: rawUri }) });
        showToast('Добавлено');
        closeModal(overlay);
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };
}

function showCreateSubModal() {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Создать подписку</h3>';
  const nameInp = el('input', 'mb-3');
  nameInp.placeholder = 'Имя подписки';
  const slugRow = el('div', 'slug-row mb-3');
  const slugInp = el('input', '');
  slugInp.placeholder = 'Slug (пусто = автогенерация)';
  const randBtn = el('button', 'btn btn-secondary btn-sm');
  randBtn.type = 'button';
  randBtn.textContent = 'Случайный';
  randBtn.onclick = () => {
    const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
    let s = ''; const len = 5 + Math.floor(Math.random() * 6);
    for (let i = 0; i < len; i++) s += chars[Math.floor(Math.random() * chars.length)];
    slugInp.value = s;
  };
  slugRow.appendChild(slugInp);
  slugRow.appendChild(randBtn);
  div.appendChild(nameInp);
  div.appendChild(slugRow);

  const btnRow = el('div', 'flex gap-2 justify-end');
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const createBtn = el('button', 'btn btn-primary');
  createBtn.textContent = 'Создать';
  btnRow.appendChild(cancelBtn);
  btnRow.appendChild(createBtn);
  div.appendChild(btnRow);

  const overlay = showModal(div);
  cancelBtn.onclick = () => closeModal(overlay);
  createBtn.onclick = async () => {
    if (!nameInp.value) { showToast('Введите имя', 'error'); return; }
    await withLoading(createBtn, async () => {
      try {
        await api('/subs', { method: 'POST', body: JSON.stringify({ name: nameInp.value, slug: slugInp.value || undefined }) });
        showToast('Подписка создана');
        closeModal(overlay);
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };
}

async function showSubInstancesModal(sub) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Инстансы в «' + sub.name + '»</h3>';
  const list = el('div', 'space-y-2 mb-3');
  list.appendChild(el('div', 'text-sm text-gray-400', 'Загрузка...'));
  div.appendChild(list);
  const btnRow = el('div', 'flex gap-2 justify-end');
  const closeBtn = el('button', 'btn btn-primary btn-sm');
  closeBtn.textContent = 'Закрыть';
  btnRow.appendChild(closeBtn);
  div.appendChild(btnRow);
  const overlay = showModal(div);
  closeBtn.onclick = () => closeModal(overlay);

  try {
    const insts = await api('/subs/' + sub.slug + '/instances');
    list.innerHTML = '';
    if (!insts || insts.length === 0) {
      list.appendChild(el('div', 'text-gray-400 text-sm', 'Нет инстансов'));
    } else {
      insts.forEach(inst => {
        const row = el('div', 'card p-2 flex items-center justify-between gap-2');
        const left = el('div', 'flex-1 text-sm min-w-0');
        left.innerHTML = '<div class="text-xs text-gray-500">ID: ' + inst.id + '</div><div class="copyable truncate">' + (inst.raw_uri || inst.label || '-') + '</div>';
        const delBtn = el('button', 'btn btn-danger btn-sm btn-icon');
        delBtn.setAttribute('aria-label', 'Удалить');
        delBtn.innerHTML = icon('trash-2');
        delBtn.onclick = async () => {
          const ok = await showConfirm({ title: 'Убрать инстанс?', message: 'Инстанс будет отвязан от подписки.', danger: true });
          if (!ok) return;
          await api('/subs/' + sub.slug + '/instances/' + inst.id, { method: 'DELETE' });
          showToast('Убрано');
          closeModal(overlay);
          render();
        };
        row.appendChild(left);
        row.appendChild(delBtn);
        list.appendChild(row);
      });
    }
  } catch (e) {
    list.innerHTML = '';
    list.appendChild(el('div', 'text-rose-400 text-sm', 'Ошибка: ' + e.message));
  }
}

async function showLogsModal(service) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Логи: ' + service + '</h3>';
  const pre = el('pre', 'logs');
  pre.textContent = 'Загрузка...';
  div.appendChild(pre);
  const btnRow = el('div', 'flex gap-2 justify-end mt-3');
  const refreshBtn = el('button', 'btn btn-secondary btn-sm');
  refreshBtn.innerHTML = icon('refresh-cw') + '<span>Обновить</span>';
  const closeBtn = el('button', 'btn btn-primary btn-sm');
  closeBtn.textContent = 'Закрыть';
  btnRow.appendChild(refreshBtn);
  btnRow.appendChild(closeBtn);
  div.appendChild(btnRow);
  const overlay = showModal(div);

  async function load() {
    try {
      const data = await api('/system/logs/' + service + '?lines=200');
      pre.textContent = data.logs || '(пусто)';
    } catch (e) {
      pre.textContent = 'Ошибка: ' + e.message;
    }
  }
  refreshBtn.onclick = () => withLoading(refreshBtn, load);
  closeBtn.onclick = () => closeModal(overlay);
  await load();
}

function showImportSubModal() {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3">Импорт подписок</h3>';
  const ta = el('textarea', 'mb-3');
  ta.placeholder = 'Вставьте JSON с подписками...';
  ta.rows = 8;
  div.appendChild(ta);

  const cbRow = el('label', 'mb-3 flex items-center gap-2 text-sm cursor-pointer');
  const owCb = el('input', '');
  owCb.type = 'checkbox';
  owCb.style.width = 'auto'; owCb.style.minHeight = 'auto';
  cbRow.appendChild(owCb);
  cbRow.appendChild(el('span', '', 'Перезаписать существующие'));
  div.appendChild(cbRow);

  const btnRow = el('div', 'flex gap-2 justify-end');
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const impBtn = el('button', 'btn btn-primary');
  impBtn.textContent = 'Импортировать';
  btnRow.appendChild(cancelBtn);
  btnRow.appendChild(impBtn);
  div.appendChild(btnRow);

  const overlay = showModal(div);
  cancelBtn.onclick = () => closeModal(overlay);
  impBtn.onclick = async () => {
    await withLoading(impBtn, async () => {
      try {
        const data = JSON.parse(ta.value);
        const url = '/subs/import' + (owCb.checked ? '?overwrite=true' : '');
        const res = await api(url, { method: 'POST', body: JSON.stringify(data) });
        showToast('Импортировано: ' + (res.created || 0) + ' создано, ' + (res.skipped || 0) + ' пропущено');
        closeModal(overlay);
        render();
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };
}

// ── Init ─────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', render);
})();
