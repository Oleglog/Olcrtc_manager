import fs from 'node:fs';
import process from 'node:process';
import { chromium } from 'playwright';

const jobPath = process.argv[2] || '/run/olcrtc-wb/job.json';
const job = JSON.parse(fs.readFileSync(jobPath, 'utf8'));
const statePath = job.state_file || '/run/olcrtc-wb/state.json';
const controlPath = job.control_file || '/run/olcrtc-wb/control.json';
const uuidPattern = /[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}/i;
let activeContext;

function writeState(phase, message, percent, extra = {}) {
  const payload = {
    phase,
    message,
    percent,
    updated_at: Math.floor(Date.now() / 1000),
    ...extra,
  };
  const tmp = `${statePath}.tmp`;
  fs.writeFileSync(tmp, JSON.stringify(payload), { mode: 0o600 });
  fs.renameSync(tmp, statePath);
}

function jwtExpiry(token) {
  try {
    const part = token.split('.')[1];
    if (!part) return 0;
    const padded = part.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - part.length % 4) % 4);
    const payload = JSON.parse(Buffer.from(padded, 'base64').toString('utf8'));
    return Number(payload.exp) || 0;
  } catch {
    return 0;
  }
}

function readDeadline() {
  try {
    const control = JSON.parse(fs.readFileSync(controlPath, 'utf8'));
    return Number(control.deadline_unix) * 1000;
  } catch {
    return Number(job.deadline_unix) * 1000;
  }
}

function findRoomID(value) {
  if (typeof value === 'string') {
    return value.match(uuidPattern)?.[0] || '';
  }
  if (!value || typeof value !== 'object') return '';
  for (const [key, child] of Object.entries(value)) {
    if (/^(room_?id|meeting_?id|room_?code|code)$/i.test(key)) {
      const match = findRoomID(child);
      if (match) return match;
    }
  }
  for (const child of Object.values(value)) {
    const match = findRoomID(child);
    if (match) return match;
  }
  return '';
}

function isWBURL(rawURL) {
  try {
    const hostname = new URL(rawURL).hostname.toLowerCase();
    return hostname === 'wb.ru' || hostname.endsWith('.wb.ru');
  } catch {
    return false;
  }
}

async function main() {
  let accountToken = '';
  let roomID = '';
  const proxy = job.proxy?.server ? {
    server: job.proxy.server,
    username: job.proxy.username || undefined,
    password: job.proxy.password || undefined,
  } : undefined;

  writeState('starting', 'Запуск удалённого Chrome...', 5);
  const context = await chromium.launchPersistentContext(job.profile_dir, {
    headless: false,
    viewport: null,
    screen: { width: 1280, height: 800 },
    proxy,
    permissions: ['clipboard-read', 'clipboard-write'],
    args: [
      '--no-first-run',
      '--no-default-browser-check',
      '--disable-background-networking',
      '--window-size=1280,800',
    ],
  });
  activeContext = context;

  const captureRequest = request => {
    try {
      const url = new URL(request.url());
      if (!isWBURL(url.href)) return;
      const authorization = request.headers()['authorization'] || '';
      if (/^Bearer\s+\S+/i.test(authorization)) {
        accountToken = authorization.replace(/^Bearer\s+/i, '').trim();
      }
      if (/room|meeting/i.test(url.pathname + url.search)) {
        roomID ||= findRoomID(request.url());
      }
    } catch {
      // Ignore malformed third-party request URLs.
    }
  };

  const captureResponse = async response => {
    const url = response.url();
    if (!isWBURL(url) || !/room|meeting|connection/i.test(url)) return;
    roomID ||= findRoomID(url);
    const contentType = response.headers()['content-type'] || '';
    if (!contentType.includes('json')) return;
    try {
      roomID ||= findRoomID(await response.json());
    } catch {
      // Responses may be empty or already consumed by the page.
    }
  };

  context.on('request', captureRequest);
  context.on('response', captureResponse);

  let pages = context.pages();
  const page = pages[0] || await context.newPage();
  await page.goto(job.home_url || 'https://stream.wb.ru', { waitUntil: 'domcontentloaded', timeout: 60_000 });
  writeState('awaiting_login', 'Войдите в WB Stream и пройдите CAPTCHA', 20);

  for (;;) {
    if (Date.now() > readDeadline()) throw new Error('Время авторизации истекло');
    const home = page.locator('[data-test="quick-meeting-card"]');
    if (await home.count() > 0 && await home.first().isVisible().catch(() => false)) break;
    await page.waitForTimeout(1000);
  }

  writeState('authorized', 'Авторизация WB подтверждена', 45);
  if (job.action === 'create') {
    await page.locator('[data-test="quick-meeting-card"]').first().click({ timeout: 30_000 });
    writeState('creating_room', 'Создание новой комнаты WB Stream...', 65);
    await page.waitForSelector('[data-test="room-content"], [data-test="room-header"]', { timeout: 60_000 });
    roomID ||= findRoomID(page.url());

    if (!roomID) {
      const participants = page.locator('[data-test="participants-button"]');
      if (await participants.count() > 0) {
        await participants.first().click().catch(() => {});
        await page.waitForTimeout(1000);
        roomID ||= findRoomID(await page.locator('body').innerText());
      }
    }
  } else {
    writeState('refreshing_token', 'Получение свежего токена WB...', 65);
    await page.reload({ waitUntil: 'domcontentloaded', timeout: 60_000 });
    await page.waitForTimeout(3000);
    if (!accountToken && job.existing_room_id) {
      await page.goto(`https://stream.wb.ru/${encodeURIComponent(job.existing_room_id)}`, {
        waitUntil: 'domcontentloaded',
        timeout: 60_000,
      });
      await page.waitForTimeout(3000);
    }
  }

  const waitUntil = Date.now() + 45_000;
  while ((!accountToken || (job.action === 'create' && !roomID)) && Date.now() < waitUntil) {
    roomID ||= findRoomID(page.url());
    await page.waitForTimeout(500);
  }

  if (!accountToken) throw new Error('WB account Bearer не найден в сетевых запросах');
  if (job.action === 'create' && !roomID) throw new Error('Room ID новой встречи не найден');

  writeState('success', 'Данные WB Stream получены', 100, {
    token: accountToken,
    token_expires_at: jwtExpiry(accountToken),
    room_id: roomID,
  });
  await context.close();
  activeContext = undefined;
}

main().catch(async error => {
  writeState('error', error?.message || String(error), 0);
  await activeContext?.close().catch(() => {});
  process.exitCode = 1;
});
