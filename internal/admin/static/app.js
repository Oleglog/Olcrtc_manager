// olcRTC Admin SPA
(function() {
'use strict';

const API = '/api';
let creds = JSON.parse(localStorage.getItem('olcrtc_creds') || 'null'); // {username, password}

// ── Network helper ───────────────────────────────────────────────────────────
async function api(path, opts = {}) {
  const url = API + path;
  const headers = { 'Content-Type': 'application/json', ...opts.headers };
  if (creds) {
    headers['Authorization'] = 'Basic ' + btoa(creds.username + ':' + creds.password);
  }
  const res = await fetch(url, { headers, ...opts });
  if (res.status === 401) {
    localStorage.removeItem('olcrtc_creds');
    creds = null;
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

// ── Theme helper ─────────────────────────────────────────────────────────────
function getTheme() {
  return localStorage.getItem('olcrtc_theme') || 'dark';
}

function setTheme(theme) {
  localStorage.setItem('olcrtc_theme', theme);
  document.documentElement.setAttribute('data-theme', theme);
}

function toggleTheme() {
  const current = getTheme();
  const next = current === 'dark' ? 'light' : 'dark';
  setTheme(next);
  return next;
}

// ── DOM helpers ──────────────────────────────────────────────────────────────
function el(type, cls, text) {
  const e = document.createElement(type);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function compareSemverJS(a, b) {
  const ap = (a || '').replace(/^v/, '').split('.').map(n => parseInt(n, 10) || 0);
  const bp = (b || '').replace(/^v/, '').split('.').map(n => parseInt(n, 10) || 0);
  for (let i = 0; i < 3; i++) {
    const av = ap[i] || 0, bv = bp[i] || 0;
    if (av !== bv) return av - bv;
  }
  return 0;
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
  'sliders-horizontal': '<line x1="21" y1="4" x2="14" y2="4"/><line x1="10" y1="4" x2="3" y2="4"/><line x1="21" y1="12" x2="12" y2="12"/><line x1="8" y1="12" x2="3" y2="12"/><line x1="21" y1="20" x2="16" y2="20"/><line x1="12" y1="20" x2="3" y2="20"/><line x1="14" y1="2" x2="14" y2="6"/><line x1="8" y1="10" x2="8" y2="14"/><line x1="16" y1="18" x2="16" y2="22"/>',
  'sun': '<circle cx="12" cy="12" r="5"/><line x1="12" y1="1" x2="12" y2="3"/><line x1="12" y1="21" x2="12" y2="23"/><line x1="4.22" y1="4.22" x2="5.64" y2="5.64"/><line x1="18.36" y1="18.36" x2="19.78" y2="19.78"/><line x1="1" y1="12" x2="3" y2="12"/><line x1="21" y1="12" x2="23" y2="12"/><line x1="4.22" y1="19.78" x2="5.64" y2="18.36"/><line x1="18.36" y1="5.64" x2="19.78" y2="4.22"/>',
  'moon': '<path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z"/>'
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
  if (!creds && path !== '/login') {
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
  subtitle.textContent = 'Введите логин и пароль';
  card.appendChild(title);
  card.appendChild(subtitle);

  const userInp = el('input', 'mb-3');
  userInp.type = 'text';
  userInp.placeholder = 'Логин';
  userInp.value = 'admin';
  userInp.setAttribute('aria-label', 'Логин');

  const passInp = el('input', 'mb-3');
  passInp.type = 'password';
  passInp.placeholder = 'Пароль';
  passInp.setAttribute('aria-label', 'Пароль');

  const btn = el('button', 'btn btn-primary w-full');
  btn.textContent = 'Войти';

  const err = el('div', 'text-rose-400 text-sm mt-2 hidden');

  async function submit() {
    err.classList.add('hidden');
    await withLoading(btn, async () => {
      try {
        const u = userInp.value;
        const p = passInp.value;
        const res = await fetch(API + '/auth/login', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ username: u, password: p })
        });
        const data = await res.json();
        if (data.ok) {
          creds = { username: u, password: p };
          localStorage.setItem('olcrtc_creds', JSON.stringify(creds));
          route('/');
        } else {
          throw new Error('invalid');
        }
      } catch (e) {
        err.textContent = 'Неверный логин или пароль';
        err.classList.remove('hidden');
      }
    });
  }
  btn.onclick = submit;
  passInp.onkeydown = (e) => { if (e.key === 'Enter') submit(); };

  card.appendChild(userInp);
  card.appendChild(passInp);
  card.appendChild(btn);
  card.appendChild(err);
  box.appendChild(card);
  app.appendChild(box);
  setTimeout(() => passInp.focus(), 0);
}

// ── Dashboard ────────────────────────────────────────────────────────────────
async function renderDashboard(app) {
  const wrap = el('div', 'max-w-6xl mx-auto p-4 md:p-6');

  // Header
  const header = el('div', 'flex items-center justify-between mb-6 flex-wrap gap-2');
  const titleWrap = el('div', 'flex items-center gap-2');
  titleWrap.innerHTML = '<span style="color: var(--color-primary)">' + icon('shield', 22) + '</span><h1 class="text-xl md:text-2xl font-semibold">olcRTC Admin</h1>';
  header.appendChild(titleWrap);
  const nav = el('div', 'flex gap-2');
  const themeBtn = el('button', 'btn btn-secondary btn-sm');
  themeBtn.setAttribute('aria-label', 'Переключить тему');
  const currentTheme = getTheme();
  themeBtn.innerHTML = currentTheme === 'dark' ? icon('sun') + '<span class="hidden sm:inline">Светлая</span>' : icon('moon') + '<span class="hidden sm:inline">Тёмная</span>';
  themeBtn.onclick = () => {
    const newTheme = toggleTheme();
    themeBtn.innerHTML = newTheme === 'dark' ? icon('sun') + '<span class="hidden sm:inline">Светлая</span>' : icon('moon') + '<span class="hidden sm:inline">Тёмная</span>';
  };
  const settingsBtn = el('button', 'btn btn-secondary btn-sm');
  settingsBtn.setAttribute('aria-label', 'Настройки');
  settingsBtn.innerHTML = icon('settings') + '<span class="hidden sm:inline">Настройки</span>';
  settingsBtn.onclick = () => route('/settings');
  const logoutBtn = el('button', 'btn btn-secondary btn-sm');
  logoutBtn.setAttribute('aria-label', 'Выход');
  logoutBtn.innerHTML = icon('log-out') + '<span class="hidden sm:inline">Выход</span>';
  logoutBtn.onclick = () => { creds = null; localStorage.removeItem('olcrtc_creds'); route('/login'); };
  nav.appendChild(themeBtn);
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
  addInstBtn.onclick = () => showCreateInstanceModal();
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
  if (inst.carrier === 'jitsi') {
    const bridgeBadge = el('span', 'badge');
    bridgeBadge.innerHTML = icon('sliders-horizontal', 12) + '<span>bridge: ' + (inst.jitsi_bridge_mode || 'auto') + '</span>';
    bridgeBadge.title = 'Jitsi bridge mode: auto, colibri-ws или sctp';
    badges.appendChild(bridgeBadge);
  }
  if (inst.carrier === 'wbstream' && (!inst.room_id || inst.room_id === 'any')) {
    const noRoomBadge = el('span', 'badge badge-amber');
    noRoomBadge.innerHTML = icon('alert-triangle', 12) + '<span>Room ID required</span>';
    noRoomBadge.title = 'wbstream больше не создаёт румы автоматически — задайте Room ID в настройках инстанса';
    badges.appendChild(noRoomBadge);
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
  uriBtn.onclick = () => {
    navigator.clipboard.writeText(inst.uri);
    showToast('URI скопирован');
  };
  const qrBtn = el('button', 'btn btn-secondary btn-sm');
  qrBtn.setAttribute('aria-label', 'Показать QR-код');
  qrBtn.innerHTML = icon('qr-code') + '<span>QR</span>';
  qrBtn.onclick = () => showQRModal(inst.uri, inst);
  const pingBtn = el('button', 'btn btn-secondary btn-sm');
  pingBtn.setAttribute('aria-label', 'Проверить соединение');
  pingBtn.innerHTML = icon('wifi') + '<span>Пинг</span>';
  pingBtn.onclick = async () => {
    await withLoading(pingBtn, async () => {
      try {
        const res = await api('/instances/' + inst.id + '/ping', { method: 'POST' });
        const targetLabel = ({
          socks_proxy: 'SOCKS',
          warp_proxy: 'WARP',
          internet: 'интернет',
        })[res.target_kind] || res.target_kind || 'цель';
        if (res && res.ok) {
          const rtt = (res.rtt_ms != null) ? res.rtt_ms.toFixed(1) + ' мс' : '';
          const loss = (res.packet_loss != null && res.packet_loss > 0) ? ` · потери ${res.packet_loss}%` : '';
          showToast(`${targetLabel} ${res.target} · ${rtt}${loss}`, 'success');
        } else {
          showToast(res.message || `Не удалось пинговать ${targetLabel}`, 'error');
        }
      } catch (e) {
        showToast('Ошибка пинга: ' + e.message, 'error');
      }
    });
  };
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
  actions.appendChild(pingBtn);
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

  // Server Updates
  const updateBlock = el('div', '');
  updateBlock.innerHTML = '<h3 class="font-semibold mb-2 inline-flex items-center gap-2">' + icon('download', 16) + '<span>Обновления</span></h3>';
  const versionInfo = el('div', 'text-sm mb-3');
  const currentSysVersion = (sys.version || '').toString();
  versionInfo.innerHTML = '<div class="text-gray-300">Текущая версия: <span class="copyable">' + (currentSysVersion || '-') + '</span></div>';
  updateBlock.appendChild(versionInfo);

  const updateRow = el('div', 'flex gap-2 flex-wrap items-center');
  const checkBtn = el('button', 'btn btn-secondary');
  checkBtn.innerHTML = icon('refresh-cw') + '<span>Проверить обновления</span>';

  // Version selector + install button row (rendered after check succeeds)
  const selectorRow = el('div', 'flex gap-2 flex-wrap items-center mt-3');
  selectorRow.style.display = 'none';
  const selectorLabel = el('span', 'text-sm text-gray-300');
  selectorLabel.textContent = 'Установить версию:';
  const versionSelect = el('select', 'bg-gray-800 text-white text-sm border border-gray-700 rounded px-2 py-1');
  versionSelect.style.cssText = 'min-width:160px;';
  const installBtn = el('button', 'btn btn-primary');

  function normVer(v) { return ('' + (v || '')).replace(/^v/, ''); }

  function refreshInstallBtn() {
    const target = versionSelect.value;
    const isCurrent = normVer(target) === normVer(currentSysVersion);
    installBtn.disabled = !target || isCurrent;
    installBtn.style.opacity = installBtn.disabled ? '0.5' : '1';
    installBtn.style.cursor = installBtn.disabled ? 'not-allowed' : 'pointer';
    if (!target) {
      installBtn.innerHTML = icon('download') + '<span>Установить</span>';
    } else if (isCurrent) {
      installBtn.innerHTML = icon('check-circle') + '<span>Версия установлена</span>';
    } else {
      installBtn.innerHTML = icon('download') + '<span>Установить ' + target + '</span>';
    }
  }
  versionSelect.onchange = refreshInstallBtn;
  installBtn.onclick = async () => {
    const target = versionSelect.value;
    if (!target || normVer(target) === normVer(currentSysVersion)) return;
    const isDowngrade = compareSemverJS(normVer(target), normVer(currentSysVersion)) < 0;
    const ok = await showConfirm({
      title: isDowngrade ? 'Откатить версию?' : 'Обновить сервер?',
      message: (isDowngrade ? 'Будет установлена более старая версия ' : 'Будет установлена версия ') + target +
        '. Сервер и админка будут остановлены, заменены и перезапущены. Это займёт 1-2 минуты.',
      confirmText: isDowngrade ? 'Откатить' : 'Установить',
    });
    if (!ok) return;
    showUpdateOverlay(target);
    try {
      await api('/system/update', { method: 'POST', body: JSON.stringify({ version: target }) });
    } catch (e) {
      // expected during admin restart
    }
  };

  selectorRow.appendChild(selectorLabel);
  selectorRow.appendChild(versionSelect);
  selectorRow.appendChild(installBtn);

  async function loadReleasesIntoSelect(latestVersion) {
    try {
      const rel = await api('/system/releases');
      const list = (rel && rel.releases) || [];
      versionSelect.innerHTML = '';
      if (!list.length) {
        const opt = el('option', '');
        opt.value = '';
        opt.textContent = 'Нет доступных версий';
        versionSelect.appendChild(opt);
        return;
      }
      // Sort newest-first by semver desc
      list.sort((a, b) => compareSemverJS(normVer(b.version), normVer(a.version)));
      list.forEach((r) => {
        const opt = el('option', '');
        opt.value = r.version;
        let label = r.version;
        if (normVer(r.version) === normVer(currentSysVersion)) label += ' (текущая)';
        else if (latestVersion && normVer(r.version) === normVer(latestVersion)) label += ' (последняя)';
        opt.textContent = label;
        versionSelect.appendChild(opt);
      });
      // Default selection: latest if newer than current, else current
      const newest = list[0].version;
      versionSelect.value = (compareSemverJS(normVer(newest), normVer(currentSysVersion)) > 0) ? newest : currentSysVersion;
      refreshInstallBtn();
    } catch (e) {
      versionSelect.innerHTML = '';
      const opt = el('option', '');
      opt.value = '';
      opt.textContent = 'Не удалось загрузить список версий';
      versionSelect.appendChild(opt);
    }
  }

  checkBtn.onclick = async () => {
    await withLoading(checkBtn, async () => {
      try {
        const res = await api('/system/check-updates');
        if (res.update_available) {
          let toastMsg = 'Доступна новая версия: ' + res.latest_version;
          if (res.stale) toastMsg = 'GitHub недоступен. Последние известные данные: ' + res.latest_version;
          showToast(toastMsg, 'info');
        } else {
          let msg = 'У вас установлена последняя версия';
          if (res.stale) msg = 'GitHub недоступен, проверка по последним известным данным: версия актуальна';
          showToast(msg, 'success');
        }
        await loadReleasesIntoSelect(res.latest_version);
        selectorRow.style.display = '';
      } catch (e) {
        let errMsg = e.message;
        try {
          const parsed = JSON.parse(e.message);
          if (parsed.message) errMsg = parsed.message;
        } catch {}
        showToast('Ошибка проверки: ' + errMsg, 'error');
      }
    });
  };

  updateRow.appendChild(checkBtn);
  updateBlock.appendChild(updateRow);
  updateBlock.appendChild(selectorRow);
  card.appendChild(updateBlock);

  // Security
  const secBlock = el('div', '');
  secBlock.innerHTML = '<h3 class="font-semibold mb-2 inline-flex items-center gap-2">' + icon('key', 16) + '<span>Безопасность</span></h3>';
  const secGrid = el('div', 'grid grid-cols-1 md:grid-cols-2 gap-3 mb-3');
  const userField = makeInputField('Логин', icon('tag', 14), creds ? creds.username : 'admin', {});
  const passField = makeInputField('Пароль', icon('lock', 14), creds ? creds.password : '', { placeholder: 'Новый пароль' });
  passField.input.type = 'password';
  secGrid.appendChild(userField.field);
  secGrid.appendChild(passField.field);
  secBlock.appendChild(secGrid);
  const changeCredsBtn = el('button', 'btn btn-secondary');
  changeCredsBtn.textContent = 'Сменить логин/пароль';
  changeCredsBtn.onclick = async () => {
    const u = userField.input.value.trim();
    const p = passField.input.value.trim();
    if (!u || !p) { showToast('Логин и пароль обязательны', 'error'); return; }
    await withLoading(changeCredsBtn, async () => {
      try {
        await api('/auth/change-credentials', { method: 'POST', body: JSON.stringify({ username: u, password: p }) });
        creds = { username: u, password: p };
        localStorage.setItem('olcrtc_creds', JSON.stringify(creds));
        showToast('Логин/пароль обновлены');
      } catch (e) { showToast('Ошибка: ' + e.message, 'error'); }
    });
  };
  secBlock.appendChild(changeCredsBtn);
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

function showQRModal(uri, inst) {
  const div = el('div', '');
  div.innerHTML = '<h3 class="text-lg font-semibold mb-3 inline-flex items-center gap-2">' + icon('qr-code', 18) + '<span>QR-код</span></h3>';
  if (inst && inst.carrier === 'wbstream' && (!inst.room_id || inst.room_id === 'any')) {
    const notice = el('div', 'p-2 mb-3 text-xs rounded border border-amber-500/50 bg-amber-500/10 text-amber-200');
    notice.innerHTML =
      '<strong>Внимание:</strong> Room ID для wbstream не задан. ' +
      'WB Stream больше не создаёт румы автоматически — задайте Room ID в «Настройках» инстанса перед тем, как делиться QR.';
    div.appendChild(notice);
  }
  if (inst && inst.transport === 'datachannel' && (inst.carrier === 'telemost' || inst.carrier === 'wbstream')) {
    const dcWarn = el('div', 'p-2 mb-3 text-xs rounded border border-red-500/50 bg-red-500/10 text-red-200');
    dcWarn.innerHTML =
      '<strong>Несовместимый транспорт:</strong> DataChannel не работает с ' + inst.carrier + '. ' +
      'Goolom SFU не маршрутизирует стандартный DC (dataChannelSharing=TO_RTP). ' +
      (inst.carrier === 'wbstream' ? 'WB Stream DC требует canPublishData=true (модератор).' : '') +
      ' Смените транспорт на <b>vp8channel</b> в настройках инстанса.';
    div.appendChild(dcWarn);
  }
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
    if (!uri || uri.length > 2500) {
      qrDiv.innerHTML = '<div class="text-red-400 text-xs p-2">URI слишком длинный для QR-кода (' + (uri ? uri.length : 0) + ' символов)</div>';
      return;
    }
    try {
      new QRCode(qrDiv, { text: uri, width: 280, height: 280, colorDark: '#000000', colorLight: '#ffffff', correctLevel: QRCode.CorrectLevel.M });
    } catch (e) {
      qrDiv.innerHTML = '<div class="text-red-400 text-xs p-2">Ошибка генерации QR: ' + e.message + '</div>';
      return;
    }
    downloadBtn.onclick = () => {
      const canvas = qrDiv.querySelector('canvas');
      const img = qrDiv.querySelector('img');
      if (canvas) {
        canvas.toBlob((blob) => {
          if (!blob) { showToast('Не удалось сгенерировать PNG', 'error'); return; }
          const url = URL.createObjectURL(blob);
          const a = document.createElement('a');
          a.href = url; a.download = 'olcrtc-qr.png'; a.click();
          URL.revokeObjectURL(url);
          showToast('PNG сохранён');
        }, 'image/png');
      } else if (img) {
        const a = document.createElement('a');
        a.href = img.src; a.download = 'olcrtc-qr.png'; a.click();
        showToast('PNG сохранён');
      } else {
        showToast('Не удалось получить QR', 'error');
      }
    };
  }, 50);
}

function showCreateInstanceModal() {
  const div = el('div', '');
  const titleRow = el('div', 'flex items-center gap-2 mb-4');
  titleRow.innerHTML = '<span style="color: var(--color-primary)">' + icon('plus', 18) + '</span><h3 class="text-lg font-semibold">Создать инстанс</h3>';
  div.appendChild(titleRow);

  const connectionSec = el('div', 'section mb-3');
  const connTitle = el('div', 'section-title flex items-center gap-1.5');
  connTitle.innerHTML = icon('wifi', 12) + '<span>Connection</span>';
  connectionSec.appendChild(connTitle);
  const connGrid = el('div', 'grid grid-cols-1 md:grid-cols-2 gap-3');

  const carrierField = makeSelectField('Провайдер', icon('tag', 14), 'jitsi', ['jitsi', 'telemost', 'wbstream']);
  const transportField = makeSelectField('Транспорт', icon('wifi', 14), 'datachannel', ['datachannel', 'vp8channel', 'seichannel', 'videochannel']);
  const nameField = makeInputField('Имя', icon('tag', 14), 'jitsi_olcrtc', { placeholder: 'имя инстанса' });
  const roomIDField = makeInputField('Room ID', icon('tag', 14), '', { placeholder: 'jitsi: https://meet.small-dm.ru/yourroom · wbstream: создать на stream.wb.ru' });

  connGrid.appendChild(carrierField.field);
  connGrid.appendChild(transportField.field);
  connGrid.appendChild(nameField.field);
  connGrid.appendChild(roomIDField.field);
  connectionSec.appendChild(connGrid);

  const dcWarn = el('div', 'p-2 mb-3 text-xs rounded border border-red-500/50 bg-red-500/10 text-red-200 hidden');
  dcWarn.innerHTML = '<strong>Внимание:</strong> DataChannel может не работать с данным carrier. Рекомендуется <b>vp8channel</b>.';
  connectionSec.appendChild(dcWarn);

  const wbHint = el('div', 'mb-3 text-xs text-amber-300 bg-amber-900/30 border border-amber-700/40 p-3 rounded-lg hidden');
  wbHint.innerHTML = '<b>WB Stream больше не создаёт румы автоматически.</b> Создайте руму на <a href="https://stream.wb.ru" target="_blank" rel="noopener" class="underline">stream.wb.ru</a> и вставьте её ID в поле <b>Room ID</b>.';
  connectionSec.appendChild(wbHint);

  const jitsiPresets = el('div', 'mb-3 text-xs text-gray-400 hidden flex flex-wrap items-center gap-2');
  jitsiPresets.innerHTML = '<span>Jitsi server:</span>'
    + '<button type="button" data-host="meet.small-dm.ru" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet.small-dm.ru</button>'
    + '<button type="button" data-host="meet1.arbitr.ru" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet1.arbitr.ru</button>'
    + '<button type="button" data-host="meet.handyweb.org" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet.handyweb.org</button>'
    + '<button type="button" data-host="meet.cryptopro.ru" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet.cryptopro.ru</button>'
    + '<span class="text-gray-500">(клик подставит/заменит хост в Room ID)</span>';
  jitsiPresets.querySelectorAll('button[data-host]').forEach((btn) => {
    btn.addEventListener('mouseenter', () => {
      btn.style.borderColor = 'var(--color-primary)';
      btn.style.color = 'var(--color-primary)';
    });
    btn.addEventListener('mouseleave', () => {
      btn.style.borderColor = 'var(--color-hairline)';
      btn.style.color = '';
    });
    btn.addEventListener('click', () => {
      const host = btn.dataset.host;
      const current = roomIDField.input.value.trim();
      // If current value looks like a URL, swap the host. Otherwise prefill template.
      const m = current.match(/^https?:\/\/[^\/]+(\/.*)?$/);
      if (m) {
        const tail = m[1] || '/';
        roomIDField.input.value = 'https://' + host + tail;
      } else if (current && !current.includes('/')) {
        // Looks like just a room name — promote to full URL.
        roomIDField.input.value = 'https://' + host + '/' + current;
      } else {
        roomIDField.input.value = 'https://' + host + '/';
      }
      roomIDField.input.focus();
    });
  });
  connectionSec.appendChild(jitsiPresets);

  const jitsiBlock = el('div', 'border border-gray-700 rounded-lg p-3 mb-3 hidden');
  jitsiBlock.innerHTML = '<div class="text-xs text-gray-400 mb-2">Jitsi DataChannel / SCTP</div>';
  const jitsiGrid = el('div', 'grid grid-cols-1 md:grid-cols-2 gap-2');
  const bridgeModeField = makeSelectField('Bridge mode', icon('sliders-horizontal', 14), 'auto', ['auto', 'sctp', 'colibri-ws']);
  const jitsiSCTPMaxMessageField = makeInputField('SCTP max message', icon('sliders-horizontal', 14), '', { placeholder: 'empty = legacy 12288' });
  const trafficPayloadField = makeInputField('Transport payload cap', icon('sliders-horizontal', 14), '', { placeholder: 'empty, 1188, 4096, 8192' });
  const trafficMinDelayField = makeInputField('Min delay', icon('clock', 14), '', { placeholder: 'empty или 1ms' });
  const trafficMaxDelayField = makeInputField('Max delay', icon('clock', 14), '', { placeholder: 'empty или 3ms' });
  jitsiGrid.appendChild(bridgeModeField.field);
  jitsiGrid.appendChild(jitsiSCTPMaxMessageField.field);
  jitsiGrid.appendChild(trafficPayloadField.field);
  jitsiGrid.appendChild(trafficMinDelayField.field);
  jitsiGrid.appendChild(trafficMaxDelayField.field);
  jitsiBlock.appendChild(jitsiGrid);
  const jitsiHint = el('div', 'mt-2 text-xs text-gray-500 leading-relaxed');
  jitsiHint.innerHTML = 'Для публичных Jitsi обычно надёжнее <b>SCTP</b>. ' +
    '<b>auto</b> выбирает Colibri WS только если он advertised, иначе SCTP. ' +
    '<b>colibri-ws</b> нужен для диагностики и завершит запуск, если WS не advertised. ' +
    'Пусто сохраняет legacy SCTP frame для совместимости. Для диагностики скорости можно явно поставить SCTP max message <b>1200</b> и payload cap <b>1188</b>, но только когда обе стороны обновлены.';
  jitsiBlock.appendChild(jitsiHint);
  connectionSec.appendChild(jitsiBlock);

  div.appendChild(connectionSec);

  // VP8 params
  const vp8Block = el('div', 'border border-gray-700 rounded-lg p-3 mb-3');
  vp8Block.innerHTML = '<div class="text-xs text-gray-400 mb-2">VP8 параметры</div>';
  const vp8Grid = el('div', 'grid grid-cols-2 gap-2');
  const vp8FpsInp = el('input', ''); vp8FpsInp.type = 'number'; vp8FpsInp.min = '0'; vp8FpsInp.step = '1'; vp8FpsInp.placeholder = 'FPS (empty=120, 0=core default)'; vp8FpsInp.value = '120';
  const vp8BatchInp = el('input', ''); vp8BatchInp.type = 'number'; vp8BatchInp.min = '0'; vp8BatchInp.step = '1'; vp8BatchInp.placeholder = 'Batch (empty=64, 0=core default)'; vp8BatchInp.value = '64';
  vp8Grid.appendChild(vp8FpsInp);
  vp8Grid.appendChild(vp8BatchInp);
  vp8Block.appendChild(vp8Grid);
  div.appendChild(vp8Block);

  // Network section: DNS / SOCKS / WARP
  const netSec = el('div', 'section mb-3');
  const netTitle = el('div', 'section-title flex items-center gap-1.5');
  netTitle.innerHTML = icon('shield', 12) + '<span>Сеть</span>';
  netSec.appendChild(netTitle);
  const netGrid = el('div', 'grid grid-cols-1 md:grid-cols-3 gap-3');
  const dnsField = makeInputField('DNS', icon('wifi', 14), '', { placeholder: '8.8.8.8:53' });
  const socksField = makeInputField('SOCKS proxy', icon('shield', 14), '', { placeholder: 'socks5://user:pass@host:port' });
  const warpField = makeInputField('WARP proxy', icon('shield', 14), '', { placeholder: '127.0.0.1:40000' });
  netGrid.appendChild(dnsField.field);
  netGrid.appendChild(socksField.field);
  netGrid.appendChild(warpField.field);
  netSec.appendChild(netGrid);
  div.appendChild(netSec);

  function getTransportOptionsForCreate(carrier) {
    // Всегда возвращаем все транспорты
    return ['datachannel', 'vp8channel', 'seichannel', 'videochannel'];
  }

  function isTransportCompatibleForCreate(carrier, transport) {
    if (carrier === 'jitsi') {
      return true;
    } else if (carrier === 'telemost') {
      return transport === 'vp8channel' || transport === 'videochannel';
    } else if (carrier === 'wbstream') {
      return transport === 'vp8channel' || transport === 'seichannel' || transport === 'videochannel';
    }
    return true;
  }

  function updateVisibility() {
    const t = transportField.input.value;
    const c = carrierField.input.value;

    // Update available transports with warnings
    const allTransports = getTransportOptionsForCreate(c);
    const currentTransport = transportField.input.value;

    // Rebuild transport select with warnings
    transportField.input.innerHTML = '';
    allTransports.forEach(tr => {
      const opt = el('option', '', tr);
      opt.value = tr;
      if (!isTransportCompatibleForCreate(c, tr)) {
        opt.textContent = tr + ' ⚠️ (несовместим)';
        opt.style.color = '#f59e0b';
      }
      transportField.input.appendChild(opt);
    });

    // Restore selection
    if (allTransports.includes(currentTransport)) {
      transportField.input.value = currentTransport;
    }

    const finalTransport = transportField.input.value;
    const isCompatible = isTransportCompatibleForCreate(c, finalTransport);

    vp8Block.classList.toggle('hidden', finalTransport !== 'vp8channel');
    jitsiBlock.classList.toggle('hidden', !(c === 'jitsi' && finalTransport === 'datachannel'));
    dcWarn.classList.toggle('hidden', isCompatible || finalTransport !== 'datachannel');
    wbHint.classList.toggle('hidden', c !== 'wbstream');
    jitsiPresets.classList.toggle('hidden', c !== 'jitsi');

    // Auto-rename
    const carriers = { jitsi: 'jitsi', telemost: 'telemost', wbstream: 'wbstream' };
    const cp = carriers[c] || c;
    nameField.input.value = cp + '_olcrtc' + (finalTransport && finalTransport !== 'vp8channel' ? '_' + finalTransport : '');
  }
  carrierField.input.addEventListener('change', updateVisibility);
  transportField.input.addEventListener('change', updateVisibility);

  // Footer
  const btnRow = el('div', 'flex gap-2 justify-end mt-2');
  const cancelBtn = el('button', 'btn btn-secondary');
  cancelBtn.textContent = 'Отмена';
  const createBtn = el('button', 'btn btn-primary');
  createBtn.textContent = 'Создать инстанс';
  btnRow.appendChild(cancelBtn);
  btnRow.appendChild(createBtn);
  div.appendChild(btnRow);

  const overlay = showModal(div);
  cancelBtn.onclick = () => closeModal(overlay);
  createBtn.onclick = async () => {
    const carrier = carrierField.input.value;
    const room = roomIDField.input.value.trim();
    if (carrier === 'wbstream' && !room) {
      showToast('Для wbstream нужно указать Room ID', 'error');
      return;
    }
    const body = {
      carrier,
      transport: transportField.input.value,
      name: nameField.input.value,
      room_id: room,
      vp8_fps: vp8FpsInp.value.trim(),
      vp8_batch: vp8BatchInp.value.trim(),
      dns: dnsField.input.value.trim(),
      socks_proxy: socksField.input.value.trim(),
      warp_proxy: warpField.input.value.trim(),
      jitsi_bridge_mode: bridgeModeField.input.value,
      jitsi_sctp_max_message_size: jitsiSCTPMaxMessageField.input.value.trim(),
      traffic_max_payload_size: trafficPayloadField.input.value.trim(),
      traffic_min_delay: trafficMinDelayField.input.value.trim(),
      traffic_max_delay: trafficMaxDelayField.input.value.trim(),
    };
    await withLoading(createBtn, async () => {
      try {
        await api('/instances', { method: 'POST', body: JSON.stringify(body) });
        showToast('Инстанс создан');
        closeModal(overlay);
        render();
      } catch (e) { showToast('Не удалось создать инстанс: ' + e.message, 'error'); }
    });
  };
}

// ── Instance config modal ────────────────────────────────────────────────────
function showConfigModal(inst) {
  const div = el('div', '');
  const titleRow = el('div', 'flex items-center gap-2 mb-4');
  titleRow.innerHTML = '<span style="color: var(--color-primary)">' + icon('sliders', 18) + '</span><h3 class="text-lg font-semibold">Настройка инстанса #' + inst.id + '</h3>';
  div.appendChild(titleRow);

  // ── Connection section ──
  const connectionSec = el('div', 'section mb-3');
  const connTitle = el('div', 'section-title flex items-center gap-1.5');
  connTitle.innerHTML = icon('wifi', 12) + '<span>Connection</span>';
  connectionSec.appendChild(connTitle);
  const connGrid = el('div', 'grid grid-cols-1 md:grid-cols-2 gap-3');

  const carrierField = makeSelectField('Провайдер', icon('tag', 14), inst.carrier || 'jitsi', ['jitsi', 'telemost', 'wbstream']);
  const transportField = makeSelectField('Транспорт', icon('wifi', 14), inst.transport || 'vp8channel', getTransportOptions(inst.carrier || 'jitsi'));
  const nameField = makeInputField('Имя', icon('tag', 14), inst.name || '', { placeholder: 'имя инстанса' });
  const roomIDField = makeInputField('Room ID', icon('tag', 14), inst.room_id || '', { placeholder: 'jitsi: https://meet.small-dm.ru/yourroom · wbstream: создать на stream.wb.ru' });
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
      message: 'Сервер создаст новую комнату при следующем подключении.',
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
  const dnsField = makeInputField('DNS', icon('wifi', 14), inst.dns || '', { placeholder: '8.8.8.8:53' });
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

  const jitsiBlock = el('div', 'border border-gray-700 rounded-lg p-3 mb-3 hidden');
  jitsiBlock.innerHTML = '<div class="text-xs text-gray-400 mb-2">Jitsi DataChannel / SCTP</div>';
  const jitsiGrid = el('div', 'grid grid-cols-1 md:grid-cols-2 gap-2');
  const bridgeModeField = makeSelectField('Bridge mode', icon('sliders-horizontal', 14), inst.jitsi_bridge_mode || 'auto', ['auto', 'sctp', 'colibri-ws']);
  const jitsiSCTPMaxMessageField = makeInputField('SCTP max message', icon('sliders-horizontal', 14), inst.jitsi_sctp_max_message_size || '', { placeholder: 'empty = legacy 12288' });
  const trafficPayloadField = makeInputField('Transport payload cap', icon('sliders-horizontal', 14), inst.traffic_max_payload_size || '', { placeholder: 'empty, 1188, 4096, 8192' });
  const trafficMinDelayField = makeInputField('Min delay', icon('clock', 14), inst.traffic_min_delay || '', { placeholder: 'empty или 1ms' });
  const trafficMaxDelayField = makeInputField('Max delay', icon('clock', 14), inst.traffic_max_delay || '', { placeholder: 'empty или 3ms' });
  jitsiGrid.appendChild(bridgeModeField.field);
  jitsiGrid.appendChild(jitsiSCTPMaxMessageField.field);
  jitsiGrid.appendChild(trafficPayloadField.field);
  jitsiGrid.appendChild(trafficMinDelayField.field);
  jitsiGrid.appendChild(trafficMaxDelayField.field);
  jitsiBlock.appendChild(jitsiGrid);
  const jitsiHint = el('div', 'mt-2 text-xs text-gray-500 leading-relaxed');
  jitsiHint.innerHTML = 'Для проверенных публичных Jitsi обычно доступен только <b>SCTP</b>. ' +
    '<b>auto</b> выбирает Colibri WS только если он advertised, иначе SCTP. ' +
    '<b>colibri-ws</b> нужен только для диагностики и завершит запуск, если WS не advertised. ' +
    'Пусто сохраняет legacy SCTP frame для совместимости. Для диагностики скорости можно явно поставить SCTP max message <b>1200</b> и payload cap <b>1188</b>, но только когда обе стороны обновлены.';
  jitsiBlock.appendChild(jitsiHint);
  advBody.appendChild(jitsiBlock);

  const vp8Block = el('div', 'border border-gray-700 rounded-lg p-3 mb-3 hidden');
  vp8Block.innerHTML = '<div class="text-xs text-gray-400 mb-2">VP8 параметры</div>';
  const vp8Grid = el('div', 'grid grid-cols-2 gap-2');
  const vp8FpsInp = el('input', ''); vp8FpsInp.type = 'number'; vp8FpsInp.min = '0'; vp8FpsInp.step = '1'; vp8FpsInp.placeholder = 'FPS (empty=120, 0=core default)'; vp8FpsInp.value = inst.vp8_fps || '';
  const vp8BatchInp = el('input', ''); vp8BatchInp.type = 'number'; vp8BatchInp.min = '0'; vp8BatchInp.step = '1'; vp8BatchInp.placeholder = 'Batch (empty=64, 0=core default)'; vp8BatchInp.value = inst.vp8_batch || '';
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

  // Jitsi server presets (shown only when carrier=jitsi)
  const jitsiPresets = el('div', 'mb-3 text-xs text-gray-400 hidden flex flex-wrap items-center gap-2');
  jitsiPresets.innerHTML = '<span>Jitsi server:</span>'
    + '<button type="button" data-host="meet.small-dm.ru" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet.small-dm.ru</button>'
    + '<button type="button" data-host="meet1.arbitr.ru" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet1.arbitr.ru</button>'
    + '<button type="button" data-host="meet.handyweb.org" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet.handyweb.org</button>'
    + '<button type="button" data-host="meet.cryptopro.ru" class="px-2 py-0.5 rounded border" style="border-color: var(--color-hairline); transition: all 0.15s;">meet.cryptopro.ru</button>'
    + '<span class="text-gray-500">(клик меняет хост в Room ID)</span>';
  jitsiPresets.querySelectorAll('button[data-host]').forEach((btn) => {
    btn.addEventListener('mouseenter', () => {
      btn.style.borderColor = 'var(--color-primary)';
      btn.style.color = 'var(--color-primary)';
    });
    btn.addEventListener('mouseleave', () => {
      btn.style.borderColor = 'var(--color-hairline)';
      btn.style.color = '';
    });
    btn.addEventListener('click', () => {
      const host = btn.dataset.host;
      const current = roomIDField.input.value.trim();
      const m = current.match(/^https?:\/\/[^\/]+(\/.*)?$/);
      if (m) {
        const tail = m[1] || '/';
        roomIDField.input.value = 'https://' + host + tail;
      } else if (current && !current.includes('/')) {
        roomIDField.input.value = 'https://' + host + '/' + current;
      } else {
        roomIDField.input.value = 'https://' + host + '/';
      }
      roomIDField.input.focus();
    });
  });
  div.appendChild(jitsiPresets);

  // Conditional visibility
  function getTransportOptions(carrier) {
    // Всегда возвращаем все транспорты, но помечаем несовместимые
    return ['datachannel', 'vp8channel', 'seichannel', 'videochannel'];
  }

  function isTransportCompatible(carrier, transport) {
    // Проверка совместимости carrier/transport
    if (carrier === 'jitsi') {
      return true; // jitsi поддерживает все транспорты
    } else if (carrier === 'telemost') {
      return transport === 'vp8channel' || transport === 'videochannel';
    } else if (carrier === 'wbstream') {
      return transport === 'vp8channel' || transport === 'seichannel' || transport === 'videochannel';
    }
    return true;
  }

  function updateTransportOptions() {
    const c = carrierField.input.value;
    const currentTransport = transportField.input.value;

    // Rebuild transport select options with warnings
    transportField.input.innerHTML = '';
    const allTransports = ['datachannel', 'vp8channel', 'seichannel', 'videochannel'];
    allTransports.forEach(t => {
      const opt = el('option', '', t);
      opt.value = t;
      if (!isTransportCompatible(c, t)) {
        opt.textContent = t + ' ⚠️ (несовместим)';
        opt.style.color = '#f59e0b';
      }
      transportField.input.appendChild(opt);
    });

    // Restore current selection
    if (allTransports.includes(currentTransport)) {
      transportField.input.value = currentTransport;
    }

    // Auto-rename instance when carrier changes
    const curName = nameField.input.value;
    const carriers = { jitsi: 'jitsi', telemost: 'telemost', wbstream: 'wbstream' };
    const carrierPrefix = carriers[c] || c;
    // If current name matches a known carrier pattern, update it
    if (/^(jitsi|telemost|wbstream)_olcrtc/.test(curName) || curName === '') {
      const t = transportField.input.value;
      nameField.input.value = carrierPrefix + '_olcrtc' + (t && t !== 'vp8channel' ? '_' + t : '');
    }
    updateVisibility();
  }
  function updateNameFromTransport() {
    const c = carrierField.input.value;
    const t = transportField.input.value;
    const carriers = { jitsi: 'jitsi', telemost: 'telemost', wbstream: 'wbstream' };
    const carrierPrefix = carriers[c] || c;
    const curName = nameField.input.value;
    if (/^(jitsi|telemost|wbstream)_olcrtc/.test(curName) || curName === '') {
      nameField.input.value = carrierPrefix + '_olcrtc' + (t && t !== 'vp8channel' ? '_' + t : '');
    }
    updateVisibility();
  }
  // datachannel warning
  const dcWarn = el('div', 'p-2 mb-3 text-xs rounded border border-red-500/50 bg-red-500/10 text-red-200 hidden');
  dcWarn.innerHTML = '<strong>Внимание:</strong> DataChannel не работает с данным провайдером. Используйте <b>vp8channel</b>.';
  div.appendChild(dcWarn);
  function updateVisibility() {
    const t = transportField.input.value;
    const c = carrierField.input.value;
    vp8Block.classList.toggle('hidden', t !== 'vp8channel');
    seiBlock.classList.toggle('hidden', t !== 'seichannel');
    jitsiBlock.classList.toggle('hidden', !(c === 'jitsi' && t === 'datachannel'));
    wbHint.classList.toggle('hidden', c !== 'wbstream');
    jitsiPresets.classList.toggle('hidden', c !== 'jitsi');
    roomRotateBtn.disabled = (c === 'wbstream');
    roomRotateBtn.title = (c === 'wbstream') ? 'WB Stream отключил автосоздание румы' : '';
    // Show datachannel warning only for non-jitsi carriers
    dcWarn.classList.toggle('hidden', !(t === 'datachannel' && c !== 'jitsi'));
  }
  carrierField.input.addEventListener('change', () => { updateTransportOptions(); });
  transportField.input.addEventListener('change', () => { updateNameFromTransport(); });
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
    const body = {
      carrier,
      transport: transportField.input.value,
      name: nameField.input.value,
      room_id: room,
      dns: dnsField.input.value,
      socks_proxy: socksField.input.value,
      warp_proxy: warpField.input.value,
      debug: debugCb.checked,
      jitsi_bridge_mode: bridgeModeField.input.value,
      jitsi_sctp_max_message_size: jitsiSCTPMaxMessageField.input.value.trim(),
      traffic_max_payload_size: trafficPayloadField.input.value.trim(),
      traffic_min_delay: trafficMinDelayField.input.value.trim(),
      traffic_max_delay: trafficMaxDelayField.input.value.trim(),
    };
    if (!vp8Block.classList.contains('hidden')) {
      body.vp8_fps = vp8FpsInp.value.trim();
      body.vp8_batch = vp8BatchInp.value.trim();
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

// ── Update overlay ───────────────────────────────────────────────────────────
const UPDATE_STEPS = [
  { id: 'download',    label: 'Скачивание бинарников',  phases: ['queued', 'starting', 'downloading_server', 'downloading_admin', 'verifying'] },
  { id: 'stopping',    label: 'Остановка сервисов',     phases: ['stopping'] },
  { id: 'replacing',   label: 'Замена бинарников',      phases: ['replacing'] },
  { id: 'starting',    label: 'Запуск сервера и админки', phases: ['starting_server', 'starting_admin'] },
  { id: 'ready',       label: 'Готовность к работе',    phases: ['completed'] },
];

function phaseToStepIndex(phase) {
  for (let i = 0; i < UPDATE_STEPS.length; i++) {
    if (UPDATE_STEPS[i].phases.includes(phase)) return i;
  }
  return -1;
}

function showUpdateOverlay(targetVersion) {
  const existing = document.getElementById('update-overlay');
  if (existing) existing.remove();

  const overlay = el('div', '');
  overlay.id = 'update-overlay';
  overlay.style.cssText = 'position:fixed;inset:0;background:rgba(1,1,2,0.95);backdrop-filter:blur(12px);display:flex;align-items:center;justify-content:center;z-index:9999;animation:fadeIn 0.3s ease-out;';

  const content = el('div', '');
  content.style.cssText = 'text-align:center;max-width:520px;padding:48px 32px;width:100%;';

  const spinnerWrap = el('div', '');
  spinnerWrap.style.cssText = 'margin-bottom:28px;display:flex;justify-content:center;';
  spinnerWrap.innerHTML = '<div id="update-spinner" style="width:64px;height:64px;border:4px solid var(--color-hairline);border-top-color:var(--color-primary);border-radius:50%;animation:spin 1s linear infinite;"></div>';

  const title = el('h2', '');
  title.style.cssText = 'font-size:28px;font-weight:600;letter-spacing:-0.6px;color:var(--color-ink);margin-bottom:10px;';
  title.textContent = 'Обновление сервера';

  const subtitle = el('p', '');
  subtitle.style.cssText = 'font-size:15px;color:var(--color-ink-muted);margin-bottom:28px;line-height:1.5;';
  subtitle.textContent = 'Устанавливается версия v' + targetVersion;

  const stepsList = el('div', '');
  stepsList.id = 'update-steps';
  stepsList.style.cssText = 'text-align:left;background:rgba(255,255,255,0.03);border:1px solid var(--color-hairline);border-radius:12px;padding:16px 20px;margin-bottom:20px;';
  UPDATE_STEPS.forEach((step, idx) => {
    const row = el('div', '');
    row.id = 'update-step-' + step.id;
    row.style.cssText = 'display:flex;align-items:center;gap:12px;padding:8px 0;font-size:14px;color:var(--color-ink-subtle);';
    const marker = el('div', '');
    marker.className = 'update-step-marker';
    marker.style.cssText = 'width:20px;height:20px;border-radius:50%;border:2px solid var(--color-hairline);flex-shrink:0;display:flex;align-items:center;justify-content:center;font-size:12px;color:var(--color-ink-tertiary);transition:all 0.3s;';
    marker.textContent = String(idx + 1);
    const label = el('span', '');
    label.className = 'update-step-label';
    label.textContent = step.label;
    row.appendChild(marker);
    row.appendChild(label);
    stepsList.appendChild(row);
  });

  const status = el('div', '');
  status.id = 'update-status';
  status.style.cssText = 'font-size:13px;color:var(--color-ink-subtle);margin-bottom:8px;font-family:ui-monospace,monospace;min-height:18px;';
  status.textContent = 'Подготовка...';

  const progressBar = el('div', '');
  progressBar.style.cssText = 'width:100%;height:4px;background:var(--color-hairline);border-radius:2px;margin-top:8px;overflow:hidden;';
  const progressFill = el('div', '');
  progressFill.id = 'update-progress';
  progressFill.style.cssText = 'height:100%;background:var(--color-primary);width:1%;transition:width 0.6s ease;';
  progressBar.appendChild(progressFill);

  const meta = el('div', '');
  meta.style.cssText = 'display:flex;justify-content:space-between;font-size:12px;color:var(--color-ink-tertiary);margin-top:12px;';
  const elapsedEl = el('span', ''); elapsedEl.id = 'update-elapsed'; elapsedEl.textContent = 'Прошло: 0s';
  const percentEl = el('span', ''); percentEl.id = 'update-percent'; percentEl.textContent = '1%';
  meta.appendChild(elapsedEl);
  meta.appendChild(percentEl);

  content.appendChild(spinnerWrap);
  content.appendChild(title);
  content.appendChild(subtitle);
  content.appendChild(stepsList);
  content.appendChild(status);
  content.appendChild(progressBar);
  content.appendChild(meta);
  overlay.appendChild(content);
  document.body.appendChild(overlay);

  const startTime = Date.now();
  let lastPhase = 'queued';
  let lastMessage = 'Подготовка обновления...';
  let lastPercent = 1;
  let adminWentDown = false;
  let finishing = false;

  function applyStepIndex(activeIdx) {
    UPDATE_STEPS.forEach((step, idx) => {
      const row = document.getElementById('update-step-' + step.id);
      if (!row) return;
      const marker = row.querySelector('.update-step-marker');
      if (idx < activeIdx) {
        marker.style.borderColor = 'var(--color-success, #22c55e)';
        marker.style.background = 'var(--color-success, #22c55e)';
        marker.style.color = '#fff';
        marker.textContent = '✓';
        row.style.color = 'var(--color-ink)';
      } else if (idx === activeIdx) {
        marker.style.borderColor = 'var(--color-primary)';
        marker.style.background = 'var(--color-primary)';
        marker.style.color = '#fff';
        marker.textContent = String(idx + 1);
        row.style.color = 'var(--color-ink)';
      } else {
        marker.style.borderColor = 'var(--color-hairline)';
        marker.style.background = 'transparent';
        marker.style.color = 'var(--color-ink-tertiary)';
        marker.textContent = String(idx + 1);
        row.style.color = 'var(--color-ink-subtle)';
      }
    });
  }

  function applyState(phase, message, percent, adminDown) {
    const stepIdx = phaseToStepIndex(phase);
    if (stepIdx >= 0) applyStepIndex(stepIdx);
    if (typeof percent === 'number' && percent >= lastPercent) {
      lastPercent = percent;
      progressFill.style.width = percent + '%';
      percentEl.textContent = percent + '%';
    }
    if (adminDown) {
      status.textContent = (message || lastMessage) + ' (админка перезапускается...)';
    } else {
      status.textContent = message || lastMessage;
    }
  }

  function fail(msg) {
    finishing = true;
    clearInterval(elapsedInterval);
    clearInterval(pollInterval);
    const spinner = document.getElementById('update-spinner');
    if (spinner) {
      spinner.style.animation = 'none';
      spinner.style.borderColor = 'var(--color-danger, #ef4444)';
      spinner.style.borderTopColor = 'var(--color-danger, #ef4444)';
    }
    title.textContent = 'Ошибка обновления';
    status.textContent = msg;
    status.style.color = 'var(--color-danger, #ef4444)';
    const closeBtn = el('button', 'btn btn-secondary');
    closeBtn.textContent = 'Закрыть';
    closeBtn.style.cssText = 'margin-top:20px;';
    closeBtn.onclick = () => overlay.remove();
    content.appendChild(closeBtn);
  }

  function complete() {
    if (finishing) return;
    finishing = true;
    applyStepIndex(UPDATE_STEPS.length); // all checked
    progressFill.style.width = '100%';
    percentEl.textContent = '100%';
    status.textContent = 'Обновление завершено! Перезагрузка...';
    clearInterval(elapsedInterval);
    clearInterval(pollInterval);
    setTimeout(() => { location.reload(); }, 1500);
  }

  const elapsedInterval = setInterval(() => {
    const elapsed = Math.floor((Date.now() - startTime) / 1000);
    elapsedEl.textContent = 'Прошло: ' + elapsed + 's';
  }, 1000);

  async function pollOnce() {
    if (finishing) return;
    const elapsed = Math.floor((Date.now() - startTime) / 1000);

    // Try to read real progress from admin
    let progressData = null;
    let adminReachable = false;
    try {
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 2500);
      const res = await fetch(API + '/system/update-progress', {
        headers: creds ? { 'Authorization': 'Basic ' + btoa(creds.username + ':' + creds.password) } : {},
        signal: controller.signal,
      });
      clearTimeout(timeoutId);
      if (res.ok) {
        progressData = await res.json();
        adminReachable = true;
      }
    } catch (e) {
      adminWentDown = true;
    }

    if (progressData) {
      lastPhase = progressData.phase || lastPhase;
      lastMessage = progressData.message || lastMessage;
      const percent = typeof progressData.percent === 'number' ? progressData.percent : lastPercent;
      applyState(lastPhase, lastMessage, percent, false);

      if (lastPhase === 'error') {
        fail(lastMessage || 'Произошла ошибка во время обновления');
        return;
      }

      // Real "completed" from script — verify admin actually serves new version before reload
      if (lastPhase === 'completed') {
        try {
          const sres = await fetch(API + '/system/status', {
            headers: creds ? { 'Authorization': 'Basic ' + btoa(creds.username + ':' + creds.password) } : {},
          });
          if (sres.ok) {
            const sd = await sres.json();
            const v = (sd.version || '').replace(/^v/, '');
            const t = (targetVersion || '').replace(/^v/, '');
            if (v && v === t) { complete(); return; }
            // version still old — admin is up but binary not yet swapped from its perspective
            status.textContent = 'Завершение обновления... (версия: ' + (sd.version || 'неизвестно') + ')';
          }
        } catch (e) { /* ignore */ }
      }
    } else {
      // Admin is unreachable — we're in stop/replace/restart window. Show last known phase.
      // If we haven't seen any phase past "verifying", assume we just hit "stopping".
      const lastIdx = phaseToStepIndex(lastPhase);
      if (lastIdx <= 0 && elapsed > 5) {
        // No state file yet but admin is down — assume stopping
        applyState('stopping', 'Остановка сервисов...', Math.max(lastPercent, 45), true);
      } else if (lastIdx === 1) {
        // We were at "stopping", now admin is gone — likely "replacing"
        applyState('replacing', 'Замена бинарников...', Math.max(lastPercent, 60), true);
      } else {
        applyState(lastPhase, lastMessage, lastPercent, true);
      }
    }

    // Safety net: reload after 3 minutes regardless
    if (elapsed > 180) {
      status.textContent = 'Время ожидания истекло. Перезагрузка...';
      clearInterval(elapsedInterval);
      clearInterval(pollInterval);
      setTimeout(() => { location.reload(); }, 1500);
    }
  }

  // Initial poll immediately, then every 1.5s
  pollOnce();
  const pollInterval = setInterval(pollOnce, 1500);
}

// ── Init ─────────────────────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  // Initialize theme
  setTheme(getTheme());
  render();
});
})();
