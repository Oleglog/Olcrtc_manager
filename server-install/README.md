# olcRTC server — systemd installer

> [**Русский**](#russian) ниже • Full English documentation continues below

> ### ⚠ Known issues
>
> - **WB Stream**: stream.wb.ru shut down its public room-creation API and
>   blocks guest joins. `wbstream` auto-room-gen no longer works — create the
>   room manually at <https://stream.wb.ru> and copy the room ID from the URL.
>   Pass it with `--id <room>` on first install or set it later in the Admin
>   Web UI.
> - For the fastest spin-up without manual steps, use `jitsi` (server-side
>   auto-gen still works) or `telemost` (the installer self-generates an
>   `olcrtc-XXXXXXXX` room ID).
> - **Upstream refactor**: the carrier layer is being rewritten in
>   [`openlibrecommunity/olcrtc#refactor/universal-carrier`](https://github.com/openlibrecommunity/olcrtc/tree/refactor/universal-carrier).
>   When it lands, provider APIs will change and panel + Android client will
>   need an update.

One-shot installer that drops an [olcrtc](https://github.com/openlibrecommunity/olcrtc)
server and the **Admin Web UI** onto a Linux VPS as hardened `systemd`
services. Binaries are not committed — they live in
[GitHub Releases](https://github.com/Oleglog/Olcrtc_manager/releases) and the
installer pulls them on demand. Local builds via `./build-from-source.sh`
are also supported.

What the installer does:

- detects VPS architecture (`linux/amd64` or `linux/arm64`),
- downloads matching pre-built binaries (`olcrtc`, `olcrtc-admin`) — or uses
  `server-install/bin/$arch/` if present,
- installs to `/usr/local/bin/olcrtc`, `/usr/local/bin/olcrtc-admin`, and the
  launcher to `/usr/local/bin/olcrtc-launcher`,
- creates the `olcrtc` system user,
- generates a 256-bit hex encryption key (`/etc/olcrtc/key.hex`),
- writes hardened `systemd` units (`olcrtc-server.service` and
  `olcrtc-admin.service`),
- provisions a Room ID:
    - **wbstream** — prompts for an ID created manually at
      <https://stream.wb.ru>,
    - **jitsi** — asks the carrier to auto-create a room on first start and
      scrapes the ID from `journalctl`,
    - **telemost** — generates a random `olcrtc-XXXXXXXX` ID locally,
- pins the Room ID into env so it survives restarts,
- starts both services and prints the **Admin UI URL** + default credentials.

Defaults: **carrier=`jitsi`**, **transport=`vp8channel`**, **DNS=`8.8.8.8:53`**.

After install, **all configuration goes through the Admin Web UI** — there is
no interactive shell menu anymore.

## Carrier & transport matrix

| Transport | telemost | jitsi | wbstream |
|-----------|:--------:|:-----:|:--------:|
| datachannel | ✗ | ✓ | ✓ |
| vp8channel | ✓ | ✓ | ✓ |
| seichannel | ✗ | ✓ | ✓ |
| videochannel | ✓ | ✓ | ✓ |

Speed (descending): **datachannel** (~6 MB/s) > **vp8channel** > **seichannel** > **videochannel** (~200 KB/s)

## Requirements

- Linux VPS with `systemd`, `bash`, `openssl`, `curl`, `journalctl`. Recent
  Ubuntu / Debian / Fedora / Alma / Arch all work. CGO not required.
- Outbound TCP/443 + UDP (for ICE/TURN). No inbound ports needed by the
  tunnel itself; the Admin UI listens on **8443/tcp** (auto-falls back to
  9443/8080/3000/4443 if 8443 is taken).
- `x86_64` or `aarch64` CPU.
- Recommended: 1 vCPU, 1 GB RAM, 10 GB disk. Binary ~20 MB; ~50–250 MB RAM
  under traffic.

## Quick start

**Option A — one-liner** (recommended):

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
```

**Option B — release tarball** (binaries inside, no GitHub access on the VPS
needed at install time):

```bash
curl -fsSL -o /tmp/olcrtc.tgz \
    https://github.com/Oleglog/Olcrtc_manager/releases/latest/download/olcrtc-server-installer.tgz
tar -xzf /tmp/olcrtc.tgz -C /tmp
sudo bash /tmp/olcrtc-server-installer-*/olcrtc-setup.sh
```

**Option C — build from source** (fully offline / reproducible):

```bash
git clone https://github.com/Oleglog/Olcrtc_manager
cd Olcrtc_manager
./server-install/build-from-source.sh   # → server-install/bin/olcrtc-linux-{amd64,arm64}
sudo bash server-install/olcrtc-setup.sh
```

> `build-from-source.sh` only builds the `olcrtc` server binary. The Admin UI
> binary (`olcrtc-admin`) is still pulled from GitHub Releases.

After install you'll see:

```
═══════════════════════════════════════════
  Установка завершена!
═══════════════════════════════════════════

  Admin UI:  https://<VPS-IP>:8443
  Логин:     admin
  Пароль:    admin

  ⚠  Сертификат самоподписанный.
     В браузере нажмите 'Дополнительно' → 'Перейти'.
```

Open the Admin UI, accept the self-signed cert, log in, and **change the
password** in the settings page.

### Picking carrier / transport at install time

```bash
sudo bash olcrtc-setup.sh --carrier telemost --transport vp8channel
sudo bash olcrtc-setup.sh --carrier jitsi --transport datachannel
sudo bash olcrtc-setup.sh --carrier wbstream --id <room-id-from-stream.wb.ru>
```

The legacy `--provider` is still accepted as an alias for `--carrier`.
For `telemost` / `wbstream` you can pass an explicit room ID with `--id`.

## Re-running the script

`olcrtc-setup.sh` is **install-only** since v1.0.0 — there is no interactive
management menu. Re-running without flags on an installed system prints
status and the Admin UI URL:

```
olcRTC уже установлен.
Admin UI:  https://<IP>:8443
olcrtc-server: running
olcrtc-admin:  running
```

Available CLI flags after install:

| Flag | What it does |
|------|--------------|
| `--update` | Pull the latest `olcrtc` + `olcrtc-admin` binaries and restart services |
| `--regenerate` | Regenerate Room ID (jitsi/telemost) — clients need re-link |
| `--regenerate-key` | Regenerate encryption key + Room ID — clients need re-link |
| `--show-token` | Print Admin UI login + password |
| `--status` | `systemctl status` for both services |
| `--uninstall` | Full uninstall (handles all instances) |

Everything else (carrier change, transport change, DNS, SOCKS5, WARP, debug
logging, multi-instance, subscriptions) is configured **in the Admin Web
UI**.

## Outbound SOCKS5 proxy (when your VPS IP is blocked)

WB Stream and Jitsi block many datacenter IPs and require a residential / RU
IP to register a guest session. Yandex Telemost is more permissive but can
still throttle.

If your VPS gets `i/o timeout` connecting to `stream.wb.ru` (or similar),
rent a residential SOCKS5 proxy and configure it **per instance** in the
Admin UI (instance form → field **SOCKS proxy**).

Both formats work:
- IP-whitelisted: `host:port`
- USER/PASSWORD (RFC 1929): `user:pass@host:port`
- `socks5://` and `socks5h://` schemes are accepted and stripped.

This sets `OLCRTC_SOCKS_PROXY=...` in the instance env. The launcher splits
credentials from `host:port` and writes `socks:` block into `config.yaml`.

What goes through SOCKS and what doesn't:

| Traffic | Routing |
| --- | --- |
| Carrier HTTP API (room creation, guest registration, polling) | through SOCKS5 |
| Carrier WebSocket signalling (jitsi / telemost) | through SOCKS5 |
| Client TCP tunnel traffic (Telegram, browser, etc.) | **direct from VPS, NOT through SOCKS5** |
| WebRTC media (UDP between VPS and Android) | direct, peer-to-peer |

Client TCP intentionally bypasses SOCKS — the proxy exists so the carrier
sees a residential / RU IP for registration. If everything went through
SOCKS, geo-blocked services (Telegram from RU IPs) would break inside the
tunnel.

To **also** hide the VPS IP from sites the client visits, use **WARP** below.

## WARP proxy (hide VPS IP from client traffic)

`OLCRTC_WARP_PROXY=host:port` routes the **client tunnel dial path** through
a local SOCKS5 — typically `wireproxy` or a 3X-UI inbound that egresses via
Cloudflare WARP. Result: visited sites see a Cloudflare IP, not your VPS IP.

Configure per-instance via the Admin UI (field **WARP proxy**, e.g.
`127.0.0.1:40000`).

WARP applies **only** to the dial path inside the tunnel; signalling is not
affected. WARP and SOCKS5 are independent — both can be set on the same
instance.

> WARP routing was a documented feature for a while but only became actually
> wired through to `s.dial()` in v1.8.34. If you set `OLCRTC_WARP_PROXY` on
> an older binary, it had no effect. Run `--update` to pull v1.8.34+.

Full guide: [WARP-PROXY.md](WARP-PROXY.md)

## Debug logging

Toggle via the Admin UI (instance form → **Debug** checkbox), or set
`OLCRTC_DEBUG=1` in the instance env file and restart the service. Logs land
in journald:

```bash
journalctl -u olcrtc-server -f                # main instance
journalctl -u olcrtc-server@2 -f              # additional instance #2
journalctl -u olcrtc-admin -f                 # admin UI
```

You'll see ICE candidate negotiation, DTLS state changes, and per-stream
errors. Useful for diagnosing reconnects on Telemost or one-off DTLS
timeouts.

## Manage the services

```bash
systemctl status olcrtc-server      # status
systemctl restart olcrtc-server     # restart
systemctl stop olcrtc-server        # stop until reboot
systemctl disable olcrtc-server     # don't start on boot

systemctl status olcrtc-admin       # admin UI
systemctl restart olcrtc-admin

systemctl status 'olcrtc-server@*'  # all extra instances
```

## Multiple instances

The Admin UI lets you run several independent olcRTC servers on the same VPS,
each with its own Room ID, key, carrier, transport, SOCKS, WARP, etc. Open
the **Instances** page → **Add instance**. Up to 20 additional instances
supported.

Each additional instance (`#2`, `#3`, …) gets:

| Path | Contents |
| --- | --- |
| `/etc/olcrtc/<N>/env` | Instance config |
| `/etc/olcrtc/<N>/key.hex` | Instance encryption key |
| `/var/lib/olcrtc-<N>/` | Instance state |
| `olcrtc-server@<N>.service` | Systemd template instance |

All instances share the same binary, launcher, and `olcrtc` system user.
The template unit (`olcrtc-server@.service`) is created automatically when
the first additional instance is added.

## Subscriptions

A subscription is a permanent URL (e.g. `http://<IP>:2096/sub/xJGHpw`) that
the client adds **once**. After recreating or migrating the server, import
the subscription DB, attach the new instance URI, and clients pick up the
change automatically — no QR re-scan.

### Enabling

The installer asks during first install:

```
Enable subscription server? (y/N): y
Subscription server port [Enter = 2096]: 2096
```

This sets `OLCRTC_SUB_ENABLED=1` and `OLCRTC_SUB_PORT=2096` in
`/etc/olcrtc/env`. The HTTP server starts alongside `olcrtc-server` on port
2096.

### Managing

Use the Admin UI → **Subscriptions** page to create/edit subscriptions, add
or remove instances, and export/import the JSON dump.

### Subscription data

| Path | Contents |
|------|----------|
| `/var/lib/olcrtc/subscriptions.db` | SQLite DB (created on first run) |
| `OLCRTC_SUB_ENABLED=1` | Enables the HTTP server |
| `OLCRTC_SUB_PORT=2096` | HTTP listen port |

`olcrtc-uninstall.sh` asks whether to delete the DB; saying **N** copies it
to `/tmp/olcrtc-subscriptions.db` for safe-keeping.

### HTTP API

Public:

| Method | Path | Response |
|--------|------|----------|
| `GET` | `/sub/{slug}` | Plain-text list of `olcrtc://` URIs, one per line |

Localhost-only management API (used by the Admin UI):

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/subscriptions` | List all subscriptions (JSON) |
| `POST` | `/api/subscriptions` | Create `{name, slug}` |
| `DELETE` | `/api/subscriptions/{slug}` | Delete subscription + instances |
| `DELETE` | `/api/subscriptions/{slug}?detach=true` | Drop all instances, keep slug |
| `GET` | `/api/subscriptions/{slug}/instances` | List instances |
| `POST` | `/api/subscriptions/{slug}/instances` | Add instance `{raw_uri}` |
| `DELETE` | `/api/subscriptions/{slug}/instances/{id}` | Remove instance |
| `GET` | `/api/export` | Export everything (JSON) |
| `POST` | `/api/import` | Import JSON dump |

### Custom domain for subscriptions (optional)

By default clients hit `http://<IP>:2096/sub/{slug}`. Binding a domain adds
HTTPS and hides the port: `https://sub.example.com/sub/{slug}`.

#### 1. DNS

A-record `sub.example.com → <VPS IP>`.

#### 2. TLS certificate

```bash
sudo apt install certbot python3-certbot-nginx
sudo certbot --nginx -d sub.example.com           # easiest if nginx owns 80
# or:
sudo certbot certonly --standalone -d sub.example.com
sudo certbot certonly --webroot -w /var/www/html -d sub.example.com
```

#### 3a. Plain nginx (no SNI multiplexer)

```nginx
server {
    listen 80;
    server_name sub.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name sub.example.com;

    ssl_certificate     /etc/letsencrypt/live/sub.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sub.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:2096;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

```bash
sudo ln -sf /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

#### 3b. nginx with SNI multiplexer (3x-ui / xray / reality)

If port 443 is already pre-read by an nginx `stream {}` block (typical with
3x-ui), a plain `listen 443 ssl` will never see traffic. Add the sub-domain
to the SNI map and let the http block listen on a private internal port:

Stream config (already present, shown for context):

```nginx
map $ssl_preread_server_name $sni_name {
    hostnames;
    panel.example.com   xray;
    sub.example.com     olcrtc_sub;     # ← add this line
    default             xray;
}
upstream xray       { server 127.0.0.1:8443; }
upstream olcrtc_sub { server 127.0.0.1:9443; }   # ← add this upstream

server {
    listen 443;
    proxy_pass $sni_name;
    ssl_preread on;
    proxy_protocol on;          # may or may not be present
}
```

HTTP block on the internal port:

```nginx
server {
    listen 80;
    server_name sub.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 127.0.0.1:9443 ssl http2 proxy_protocol;
    server_name sub.example.com;
    real_ip_header proxy_protocol;
    set_real_ip_from 127.0.0.1;

    ssl_certificate     /etc/letsencrypt/live/sub.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/sub.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:2096;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

> If your stream block does **not** have `proxy_protocol on;`, drop
> `proxy_protocol` from the `listen` directive and remove the
> `real_ip_header` / `set_real_ip_from` lines.

```bash
sudo nginx -t && sudo systemctl reload nginx
curl -sf https://sub.example.com/sub/{slug}
```

Existing 3x-ui / xray routes are not affected — only the new SNI entry is
added; `default` still falls through to xray.

#### 4. Optional: close port 2096 externally

Once the domain works, block direct access:

```bash
sudo ufw deny 2096/tcp        # or iptables -A INPUT -p tcp --dport 2096 -j DROP
```

nginx reaches `127.0.0.1:2096` locally — the firewall doesn't interfere.
Clients then use only `https://sub.example.com/sub/{slug}`.

## Uninstall

Recommended (handles all instances, asks about subscription DB):

```bash
sudo bash olcrtc-uninstall.sh

# or one-liner:
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
```

Manual (main instance only):

```bash
sudo systemctl disable --now olcrtc-server olcrtc-admin
sudo rm -f /etc/systemd/system/olcrtc-server.service \
           /etc/systemd/system/olcrtc-server@.service \
           /etc/systemd/system/olcrtc-admin.service
sudo systemctl daemon-reload
sudo rm -rf /etc/olcrtc /var/lib/olcrtc /var/lib/olcrtc-* \
            /usr/local/bin/olcrtc /usr/local/bin/olcrtc-launcher \
            /usr/local/bin/olcrtc-admin
sudo userdel olcrtc 2>/dev/null || true
```

## How the Room ID is allocated

- **jitsi** — server-side auto-gen. The first run uses `-id any`, the
  carrier API allocates a fresh room and emits `Jazz room created: <id>`
  into journald. The installer scrapes it and pins it to `/etc/olcrtc/env`.
- **wbstream** — auto-gen disabled by WB. You must create the room manually
  at <https://stream.wb.ru> and pass the ID via `--id` (or paste it in the
  Admin UI).
- **telemost** — no API call. The user-supplied (or generated) string is the
  room.

Subsequent restarts reuse the persisted ID. Use `--regenerate` (or the Admin
UI) to allocate a fresh one.

## Where things live

| Path | Owner | Mode | Contents |
| --- | --- | --- | --- |
| `/usr/local/bin/olcrtc` | root:root | 0755 | The Go server binary |
| `/usr/local/bin/olcrtc-admin` | root:root | 0755 | Admin Web UI binary |
| `/usr/local/bin/olcrtc-launcher` | root:root | 0755 | Bash wrapper: env → `config.yaml` |
| `/etc/olcrtc/key.hex` | root:olcrtc | 0640 | 64-char hex encryption key |
| `/etc/olcrtc/env` | root:olcrtc | 0640 | Main instance env (CARRIER, TRANSPORT, ROOM_ID, KEY, DNS, …) |
| `/etc/olcrtc/admin.env` | root:root | 0600 | Admin UI env (port, login, pass, domain) |
| `/etc/olcrtc/<N>/env` | root:olcrtc | 0640 | Env for additional instance N |
| `/etc/olcrtc/<N>/key.hex` | root:olcrtc | 0640 | Key for additional instance N |
| `/var/lib/olcrtc/` | olcrtc:olcrtc | 0750 | Main instance state |
| `/var/lib/olcrtc-<N>/` | olcrtc:olcrtc | 0750 | State for additional instance N |
| `/var/lib/olcrtc/admin-tls/` | olcrtc:olcrtc | 0750 | Self-signed TLS for Admin UI |
| `/var/lib/olcrtc/subscriptions.db` | olcrtc:olcrtc | 0640 | Subscription SQLite DB |
| `/etc/systemd/system/olcrtc-server.service` | root:root | 0644 | Hardened systemd unit (main) |
| `/etc/systemd/system/olcrtc-server@.service` | root:root | 0644 | Template unit for extra instances |
| `/etc/systemd/system/olcrtc-admin.service` | root:root | 0644 | Admin UI unit |

## License

- olcRTC is **WTFPL**.
- Binaries and installer in this repo are derivative works of
  https://github.com/openlibrecommunity/olcrtc and inherit the same license.

---

<a name="russian"></a>
## Русский

Один скрипт ставит olcRTC-сервер + Admin Web UI на Linux VPS под `systemd`.

### Самый быстрый путь

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
```

После установки скрипт напечатает URL Admin UI и креды (по умолчанию
`admin/admin`, **сразу смените пароль**). Дальнейшее управление
(carrier, транспорт, SOCKS5, WARP, инстансы, подписки, обновления, debug) —
целиком через Web UI.

### Что произойдёт

1. Скачаются бинарники `olcrtc` и `olcrtc-admin` (~20 МБ каждый,
   `linux/amd64` или `linux/arm64`).
2. Создастся системный пользователь `olcrtc`.
3. Сгенерируется 256-битный ключ в `/etc/olcrtc/key.hex`.
4. Запишется конфиг в `/etc/olcrtc/env` и `/etc/olcrtc/admin.env`.
5. Зарегистрируются hardened systemd-юниты `olcrtc-server.service` и
   `olcrtc-admin.service`.
6. Для `jitsi` — Room ID получится по API. Для `wbstream` — спросит
   вручную (создать на <https://stream.wb.ru>). Для `telemost` — сгенерится
   локальный `olcrtc-XXXXXXXX`.
7. На экране — Admin UI URL + логин/пароль.

### Флаги CLI (после установки)

```bash
sudo bash olcrtc-setup.sh                 # статус и URL Admin UI
sudo bash olcrtc-setup.sh --update        # обновить бинарники
sudo bash olcrtc-setup.sh --regenerate    # пересоздать Room ID
sudo bash olcrtc-setup.sh --regenerate-key # пересоздать ключ + Room ID
sudo bash olcrtc-setup.sh --show-token    # показать логин/пароль
sudo bash olcrtc-setup.sh --status        # systemctl status
sudo bash olcrtc-setup.sh --uninstall     # полное удаление
```

Старого интерактивного меню нет — всё через Admin UI.

### Что идёт через SOCKS5, что не идёт

| Трафик | Маршрут |
|---|---|
| Carrier HTTP API + WebSocket signalling | через SOCKS5 |
| Клиентский TCP-туннель (Telegram, браузер) | **напрямую с VPS** |
| WebRTC media (UDP) | напрямую (UDP не идёт через CONNECT) |

Клиентский TCP **не** идёт через SOCKS — иначе сервисы, заблокированные
у RU-IP (Telegram), сломались бы внутри туннеля. Чтобы скрыть IP VPS от
посещаемых сайтов — используйте **WARP** (см. ниже).

### WARP-прокси (скрыть IP VPS)

`OLCRTC_WARP_PROXY=host:port` маршрутизирует **только клиентский
tunnel-трафик** через локальный SOCKS5 (поверх Cloudflare WARP). Для сайта
выглядит как Cloudflare IP, а не как IP VPS. Настраивается на инстанс
через Admin UI (поле **WARP proxy**) или прямой правкой
`/etc/olcrtc/<N>/env`.

> До v1.8.34 настройка читалась из YAML, но не доходила до dial-функции —
> запустите `--update`, если вы устанавливались раньше.

Подробности — [WARP-PROXY.md](WARP-PROXY.md).

### Несколько инстансов

До 20 независимых olcRTC-серверов на одном VPS. Каждый — свой Room ID,
ключ, carrier, транспорт, SOCKS, WARP. Управление через Admin UI →
**Instances** → **Add instance**.

| Путь | Содержимое |
|---|---|
| `/etc/olcrtc/<N>/env` | Конфиг инстанса |
| `/etc/olcrtc/<N>/key.hex` | Ключ инстанса |
| `/var/lib/olcrtc-<N>/` | State-директория |
| `olcrtc-server@<N>.service` | Systemd template-юнит |

### Подписки

Постоянный URL вида `http://IP:2096/sub/xJGHpw` — клиент добавляет один раз,
после пересоздания сервера достаточно импортировать БД и привязать новый
инстанс. Управление через Admin UI → **Subscriptions**. Привязка домена и
nginx — см. английский раздел выше.

### Удаление

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
```

Или ручное удаление — см. английский раздел выше.

### Полная документация

Английский раздел выше — полный справочник по флагам, путям, формату
конфига, сборке из исходников и устройству systemd-юнитов. Этот раздел —
TL;DR.
