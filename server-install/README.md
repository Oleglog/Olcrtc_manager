# olcRTC server — systemd installer

> [**Русский**](#russian) ниже • Full English documentation continues below

> ### ⚠ Known issues / Известные проблемы (май 2026)
>
> - **WB Stream**: разработчики stream.wb.ru отключили публичный API создания
>   комнат и приём гостей в звонки. Автогенерация румы для `wbstream` больше
>   не работает. При выборе `wbstream` инсталлятор и админка теперь требуют
>   указать **Room ID вручную** — создайте руму на <https://stream.wb.ru> и
>   скопируйте её ID из URL.
> - Чтобы быстро запустить сервер без ручных шагов — используйте `jazz`
>   (server-side auto-gen всё ещё работает) или `telemost` (для telemost
>   инсталлятор сам генерирует ID вида `olcrtc-XXXXXXXX`).
> - **Upstream breaking refactor**: в ветке
>   [`openlibrecommunity/olcrtc#refactor/universal-carrier`](https://github.com/openlibrecommunity/olcrtc/tree/refactor/universal-carrier)
>   готовится переписывание carrier-слоя. После слияния в `master` API
>   провайдеров изменится и потребуется обновление панели + Android-клиента.

One-shot installer that puts an [olcrtc](https://github.com/openlibrecommunity/olcrtc)
server CLI on a Linux VPS with a hardened `systemd` service. The binaries
themselves are not committed to this branch — they live in
[GitHub Releases](https://github.com/Oleglog/Olcrtc_manager/releases) and the
installer fetches them on demand. You can also build them locally from source
with `./build-from-source.sh`.

What the installer does:

- detects the VPS architecture (`linux/amd64` or `linux/arm64`),
- downloads the matching pre-built binary from the GitHub Release that
  corresponds to this installer version (or uses a local `bin/$arch` if you
  built / placed one),
- installs it to `/usr/local/bin/olcrtc` and a small launcher to
  `/usr/local/bin/olcrtc-launcher`,
- creates a dedicated `olcrtc` system user,
- generates a 256-bit hex encryption key (`/etc/olcrtc/key.hex`),
- registers a hardened `systemd` service (`olcrtc-server.service`),
- provisions the Room ID:
    - **wbstream** — prompts you for a Room ID created manually at
      <https://stream.wb.ru> (WB Stream removed the public room-creation API),
    - **jazz** — asks the carrier to auto-create a room on first start and
      captures the room ID from `journalctl`,
    - **telemost** — generates a random `olcrtc-XXXXXXXX` Room ID locally;
- pins the resulting Room ID into the service environment so the same room
  is reused across restarts,
- supports an optional outbound SOCKS5 proxy (NO_AUTH or RFC 1929
  USER/PASSWORD), useful when the VPS IP is blocked by
  wbstream / jazz / telemost, and an optional `-debug` flag,
- prints the credentials you need to fill into the Android app.

Default carrier is **`wbstream`**. Default transport is **`datachannel`**.

### Carrier & transport matrix

| Transport | telemost | jazz | wbstream |
|-----------|:--------:|:----:|:--------:|
| datachannel | ✗ | ✓ | ✓ |
| vp8channel | ✓ | ✓ | ✓ |
| seichannel | ✗ | ✓ | ✓ |
| videochannel | ✓ | ✓ | ✓ |

Speed (descending): **datachannel** (~6 MB/s) > **vp8channel** > **seichannel** > **videochannel** (~200 KB/s)

## Requirements

- A Linux VPS with `systemd`, `bash`, `openssl`, `curl`, `journalctl`. Any
  recent Ubuntu / Debian / Fedora / Alma / Arch will do. CGO is NOT required.
- Outbound internet access on TCP 443 + UDP (for ICE/TURN). No inbound ports
  need to be opened.
- `x86_64` or `aarch64` CPU.
- Recommended: 1 vCPU, 1 GB RAM, 10 GB disk. The binary is ~20 MB and uses
  ~50–250 MB RAM depending on traffic.

## Quick start (default — wbstream + datachannel)

**Option A — one-liner** (recommended, downloads and runs the interactive
setup script):

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/refactor-universal-carrier-fork/server-install/olcrtc-setup.sh | sudo bash
```

**Option B — from a release tarball** (binary already inside):

```bash
curl -fsSL -o /tmp/olcrtc.tgz \
    https://github.com/Oleglog/Olcrtc_manager/releases/latest/download/olcrtc-server-installer-0.1.3.tgz
rm -rf /tmp/olcrtc-server-installer-*
tar -xzf /tmp/olcrtc.tgz -C /tmp
sudo bash /tmp/olcrtc-server-installer-*/olcrtc-setup.sh
```

**Option C — build from source** (no GitHub access on the VPS / reproducible
build required):

```bash
cd Olcrtc_manager
./server-install/build-from-source.sh   # produces server-install/bin/olcrtc-linux-{amd64,arm64}
sudo bash server-install/olcrtc-setup.sh
```

The installer prints the credentials you need to enter into the **olcRTC**
Android app at the end:

```
==========================================================
        olcRTC server is up.
==========================================================

  Carrier:         wbstream
  Transport:       datachannel (~6 МБ/с)
  Room ID:         01HZX...
  Key (hex):       7b3c1f...
  DNS:             1.1.1.1:53
  Public IP:       a.b.c.d
...
```

## Picking a different carrier / transport

Use the interactive menu after the server is installed:

```bash
sudo bash olcrtc-setup.sh   # → menu item 3) Сменить carrier
                             # → menu item 4) Сменить транспорт
```

Or pass CLI flags during initial install:

```bash
sudo bash olcrtc-setup.sh --carrier telemost --transport vp8channel
sudo bash olcrtc-setup.sh --carrier jazz --transport datachannel
sudo bash olcrtc-setup.sh --carrier wbstream                       # default transport = datachannel
```

The legacy `--provider` flag is still accepted as an alias for `--carrier`.

For **telemost**, the room ID is whatever string you choose — it is not
provisioned by Yandex. You can pass it as a CLI flag:

```bash
sudo bash olcrtc-setup.sh --carrier telemost --telemost-id my-vpn-room
```

## Re-running the setup script

The setup script is idempotent — re-running keeps the existing key, room ID,
proxy and debug settings unless you ask otherwise. The **recommended** way is
through the interactive menu:

```bash
sudo bash olcrtc-setup.sh
```

| Menu item | Equivalent CLI flag |
|-----------|---------------------|
| 3) Сменить carrier | `--carrier jazz` |
| 4) Сменить транспорт | `--transport vp8channel` |
| 5) Пересоздать room ID | `--regenerate` |
| 6) Ротация ключа + room ID | `--regenerate-key` |
| 7) Настроить SOCKS5-прокси | `--socks-proxy host:port` |
| 8) Убрать SOCKS5-прокси | `--socks-proxy ""` |
| 9) Debug-логирование | `--debug` / `--no-debug` |
| 11) Обновить бинарник | (re-run the script) |

CLI flags also work for non-interactive / scripted usage:

```bash
sudo bash olcrtc-setup.sh --regenerate                      # keep key, get a new room ID
sudo bash olcrtc-setup.sh --regenerate-key                  # rotate everything (key + room)
sudo bash olcrtc-setup.sh --carrier jazz                    # change carrier
sudo bash olcrtc-setup.sh --transport vp8channel            # change transport
sudo bash olcrtc-setup.sh --socks-proxy host:port           # route outbound through SOCKS5 (NO_AUTH)
sudo bash olcrtc-setup.sh --socks-proxy user:pass@h:port    # route outbound through SOCKS5 (USER/PASSWORD)
sudo bash olcrtc-setup.sh --socks-proxy ""                  # remove existing SOCKS5 proxy
sudo bash olcrtc-setup.sh --debug                           # enable -debug logging
sudo bash olcrtc-setup.sh --no-debug                        # disable -debug logging
```

Rotating the key invalidates every existing client; you will need to update
the Android app profile with the new key.

## Outbound SOCKS5 proxy (when your VPS IP is blocked)

Wildberries Stream and SaluteJazz block many datacenter IPs and require a
residential / Russian IP to register a guest session. Yandex Telemost is
more permissive but can still throttle or rotate sessions on suspicious IPs.

If your VPS gets `i/o timeout` connecting to `stream.wb.ru` (or similar),
or is blocked by wbstream / jazz, rent a residential SOCKS5 proxy. Both
`NO_AUTH` (IP-whitelisted) and `RFC 1929 USER/PASSWORD` are supported —
use whichever your provider gives you:

Use the interactive menu item **7) Настроить SOCKS5-прокси**, or pass flags:

```bash
# IP-whitelisted proxy (no credentials):
sudo bash olcrtc-setup.sh --socks-proxy 1.2.3.4:1080

# Username/password proxy (RFC 1929):
sudo bash olcrtc-setup.sh --socks-proxy alice:hunter2@1.2.3.4:1080

# Optional `socks5://` / `socks5h://` scheme is accepted and stripped:
sudo bash olcrtc-setup.sh --socks-proxy socks5://alice:hunter2@1.2.3.4:1080
```

This writes `OLCRTC_SOCKS_PROXY=...` into `/etc/olcrtc/env`. The launcher
splits credentials from `host:port` and invokes the binary with
`-socks-proxy <host> -socks-proxy-port <port>` (and
`-socks-proxy-user` / `-socks-proxy-pass` when credentials are present).

What goes through the proxy and what does not:

| Traffic | Routing |
| --- | --- |
| Carrier HTTP API calls (wbstream / jazz / telemost guest registration, room creation, polling) | through the SOCKS5 proxy |
| Carrier WebSocket signalling (jazz / telemost) | through the SOCKS5 proxy |
| Client TCP traffic tunnelled from the Android device (browser, Telegram, anything else) | **direct from the VPS, NOT through the proxy** |
| WebRTC media (UDP between VPS and Android) | direct, peer-to-peer (SOCKS5 cannot tunnel UDP via CONNECT) |

Client TCP traffic is intentionally **not** routed through the proxy.
The proxy exists to make the provider see a residential / RU IP for
registration; if every outbound connection were forced through it, geo-
restricted services (e.g. Telegram, which is blocked from RU IPs) would
become unreachable from the tunnel. Each tunnelled TCP connection
therefore exits straight from the VPS, with the VPS's geolocation.

## Debug logging

Use the interactive menu item **9) Включить / выключить debug-логирование**,
or pass flags:

```bash
sudo bash olcrtc-setup.sh --debug
journalctl -u olcrtc-server -f
```

You will see ICE candidate negotiation, DTLS state changes, and per-stream
errors. Useful for diagnosing reconnects on Telemost or one-off DTLS
timeouts. Re-run with `--no-debug` (or toggle via the menu) to switch back.

## Manage the service

```bash
systemctl status olcrtc-server      # status snapshot
journalctl -u olcrtc-server -f      # live logs
systemctl restart olcrtc-server     # restart
systemctl stop olcrtc-server        # stop (won't restart on reboot)
systemctl disable olcrtc-server     # don't start on reboot
```

## Multiple instances

You can run several independent olcRTC servers on the same VPS, each with its
own room ID, encryption key, carrier and transport. Use the interactive manager menu:

```bash
sudo bash olcrtc-setup.sh   # → menu item 20) Управление инстансами
```

Each additional instance (`#2`, `#3`, …) gets:

| Path | Contents |
| --- | --- |
| `/etc/olcrtc/<N>/env` | Instance config |
| `/etc/olcrtc/<N>/key.hex` | Instance encryption key |
| `/var/lib/olcrtc-<N>/` | Instance state directory |
| `olcrtc-server@<N>.service` | Systemd template unit instance |

All instances share the same binary (`/usr/local/bin/olcrtc`), launcher, and
system user (`olcrtc`). Up to 20 additional instances are supported.

The template unit (`olcrtc-server@.service`) is created automatically when
the first additional instance is added and removed when the last one is deleted.

## Subscriptions

The subscription system lets you publish a permanent URL (e.g.
`http://IP:2096/sub/xJGHpw`) that clients can add once. After recreating
the server you import the same subscriptions, add the new instance URI, and
clients pick up the change automatically — no QR re-scan needed.

### Enabling subscriptions

During first install `olcrtc-setup.sh` asks:

```
Enable subscription server? (y/N): y
Subscription server port [Enter = 2096]: 2096
```

This writes `OLCRTC_SUB_ENABLED=1` and `OLCRTC_SUB_PORT=2096` into
`/etc/olcrtc/env`. The embedded HTTP server starts alongside the main tunnel
on port **2096** (configurable).

### Managing subscriptions (menu item 30)

```bash
sudo bash olcrtc-setup.sh   # → menu item 30) Управление подписками
```

The subscription submenu offers:

| # | Action | Description |
|---|--------|-------------|
| 1 | **List subscriptions** | Show all subscriptions with their instances |
| 2 | **Create subscription** | Enter a name, optionally specify a slug (6-char random by default) |
| 3 | **Add instance** | Choose a subscription slug, paste the full `olcrtc://` URI |
| 4 | **Remove instance** | Choose a subscription, then remove a single instance by ID |
| 5 | **Detach all instances** | Remove all instances from a subscription (subscription stays, empty) |
| 6 | **Delete subscription** | Remove subscription; if it has instances, asks whether to delete or detach them first |
| 7 | **Export** | Save all subscriptions to a JSON file |
| 8 | **Import** | Load subscriptions from a previously exported JSON file |

### Typical workflow

**First-time setup:**

1. Install olcrtc via `olcrtc-setup.sh`, answer **y** to enable subscriptions.
2. After install, open the menu → **30) Управление подписками** → **2) Создать подписку** (e.g. name `my-vpn`, slug auto-generated → `xJGHpw`).
3. Copy the `olcrtc://` URI from the QR/URI output (menu item **2** in the main menu).
4. **30 → 3) Добавить инстанс** → paste the URI into subscription `xJGHpw`.
5. Client app adds the subscription URL: `http://<IP>:2096/sub/xJGHpw`.

**Server recreation (keep the same subscription URL):**

1. Before destroying the server, export: **30 → 7) Экспорт** → saves `/tmp/olcrtc-subscriptions.json`.
2. On the new server, install with subscriptions enabled, then import: **30 → 8) Импорт**.
3. The subscription `xJGHpw` is restored with the same slug.
4. Add the new instance URI: **30 → 3)**.
5. Clients refresh the subscription and get the new parameters.

### Multiple instances per subscription

A single subscription can hold several `olcrtc://` URIs with different
carriers or transports. The client fetches all URIs from
`GET /sub/{slug}` (plain text, one per line) and selects the best one.

### Subscription data

| Path | Contents |
|------|----------|
| `/var/lib/olcrtc/subscriptions.db` | SQLite database (created at first start) |
| `OLCRTC_SUB_ENABLED=1` in env | Enables the HTTP server |
| `OLCRTC_SUB_PORT=2096` in env | HTTP server listen port |

When uninstalling, both `olcrtc-setup.sh` (menu item **12**) and
`olcrtc-uninstall.sh` ask whether to delete the subscription database.
If you answer **N**, a copy is saved to `/tmp/olcrtc-subscriptions.db`.

### HTTP API reference

Public (open to the internet if the port is reachable):

| Method | Path | Response |
|--------|------|----------|
| `GET` | `/sub/{slug}` | Plain-text list of `olcrtc://` URIs, one per line |

Management (localhost only, used by `olcrtc-setup.sh` via `curl`):

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/api/subscriptions` | List all subscriptions (JSON) |
| `POST` | `/api/subscriptions` | Create subscription `{name, slug}` |
| `DELETE` | `/api/subscriptions/{slug}` | Delete subscription + instances |
| `DELETE` | `/api/subscriptions/{slug}?detach=true` | Remove all instances, keep subscription |
| `GET` | `/api/subscriptions/{slug}/instances` | List instances (JSON) |
| `POST` | `/api/subscriptions/{slug}/instances` | Add instance `{raw_uri}` |
| `DELETE` | `/api/subscriptions/{slug}/instances/{id}` | Remove instance |
| `GET` | `/api/export` | Export all subscriptions (JSON) |
| `POST` | `/api/import` | Import subscriptions (JSON) |

### Optional: custom domain

By default clients access subscriptions via `http://<IP>:2096/sub/{slug}`.
Binding a domain adds HTTPS and hides the port:
`https://sub.example.com/sub/{slug}`.

#### 1. DNS

Add an **A record** `sub.example.com → <VPS IP>` at your DNS provider.

#### 2. TLS certificate (Let's Encrypt)

```bash
sudo apt install certbot python3-certbot-nginx   # if not installed

# Option A — certbot nginx plugin (easiest when port 80 is free or nginx owns it):
sudo certbot --nginx -d sub.example.com

# Option B — standalone (stop whatever uses port 80 first):
sudo systemctl stop nginx
sudo certbot certonly --standalone -d sub.example.com
sudo systemctl start nginx

# Option C — webroot (nginx is running, no plugin):
sudo certbot certonly --webroot -w /var/www/html -d sub.example.com
```

#### 3a. Standard nginx (no SNI multiplexer)

If your nginx does **not** use a `stream {}` SNI pre-read block (i.e. no
3x-ui / xray / reality on port 443), a simple `http {}` server block is
enough:

```bash
sudo tee /etc/nginx/sites-available/olcrtc-sub <<'EOF'
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
EOF

sudo ln -sf /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

#### 3b. nginx with SNI multiplexer (3x-ui / xray / reality)

If your server already runs **3x-ui** or another TLS service that uses an
nginx `stream {}` block with `ssl_preread` to route connections by SNI, all
TLS traffic on port 443 is intercepted **before** it reaches `http {}`
server blocks. A regular `listen 443 ssl` will never see traffic.

Typical stream config (`/etc/nginx/stream-enabled/*.conf` or similar):

```nginx
map $ssl_preread_server_name $sni_name {
    hostnames;
    panel.example.com        xray;
    www.example.com          www;
    default                  xray;      # unknown SNI → xray
}
upstream xray { server 127.0.0.1:8443; }
upstream www  { server 127.0.0.1:7443; }

server {
    listen 443;
    proxy_pass $sni_name;
    ssl_preread on;
    proxy_protocol on;        # may or may not be present
}
```

**Steps:**

1. **Add upstream** for the subscription server (pick a free local port,
   e.g. 9443):

   ```bash
   # Add upstream + SNI entry (adjust the stream config path)
   sudo sed -i '/upstream www {/i\upstream olcrtc_sub {\n    server 127.0.0.1:9443;\n}\n' \
       /etc/nginx/stream-enabled/*.conf
   sudo sed -i '/default/i\    sub.example.com            olcrtc_sub;' \
       /etc/nginx/stream-enabled/*.conf
   ```

2. **Create the HTTP server block** listening on the internal port:

   ```bash
   sudo tee /etc/nginx/sites-available/olcrtc-sub <<'EOF'
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
   EOF

   sudo ln -sf /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/
   ```

   > **Note**: if your stream block does NOT have `proxy_protocol on;`,
   > remove `proxy_protocol` from the `listen` directive and remove the
   > `real_ip_header` / `set_real_ip_from` lines.

3. **Test and reload:**

   ```bash
   sudo nginx -t && sudo systemctl reload nginx
   ```

4. **Verify:**

   ```bash
   curl -sf https://sub.example.com/sub/{slug}
   ```

Existing 3x-ui / xray routes are **not affected** — only the new SNI entry
is added; `default` still falls through to xray.

#### 4. Optional: close port 2096 externally

Once the domain works, you can block direct access to the subscription port:

```bash
sudo ufw deny 2096/tcp    # or: iptables -A INPUT -p tcp --dport 2096 -j DROP
```

nginx reaches `127.0.0.1:2096` locally — the firewall does not interfere.

Clients then use only: `https://sub.example.com/sub/{slug}`

## Uninstall

Recommended — use the uninstall script (handles all instances):

```bash
# From a checkout:
sudo bash olcrtc-uninstall.sh

# Or one-liner (no checkout needed):
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/refactor-universal-carrier-fork/server-install/olcrtc-uninstall.sh | sudo bash
```

Manual (main instance only):

```bash
sudo systemctl disable --now olcrtc-server
sudo rm -f /etc/systemd/system/olcrtc-server.service
sudo systemctl daemon-reload
sudo rm -rf /etc/olcrtc /var/lib/olcrtc /var/lib/olcrtc-* /usr/local/bin/olcrtc /usr/local/bin/olcrtc-launcher
sudo rm -f /etc/systemd/system/olcrtc-server@.service
sudo userdel olcrtc 2>/dev/null || true
```

## How it picks the room ID

For `wbstream` and `jazz`, the room is allocated server-side by the
respective provider when the olcrtc binary calls their REST API on startup.
The first run uses `-id any`, which makes the upstream API allocate a fresh
room and log a line of the form:

    WB Stream room created: 01HZX...
    Jazz room created: ...

The installer scrapes that line out of `journalctl`, persists the value to
`/etc/olcrtc/env`, and restarts the service so subsequent restarts pin the
same room until you explicitly rotate it with `--regenerate`.

For `telemost`, no API call is needed — the user-supplied ID is the room.

## Where things live

| Path | Owner | Mode | Contents |
| --- | --- | --- | --- |
| `/usr/local/bin/olcrtc` | root:root | 0755 | The Go binary |
| `/usr/local/bin/olcrtc-launcher` | root:root | 0755 | Bash wrapper that translates env to flags |
| `/etc/olcrtc/key.hex` | root:olcrtc | 0640 | 64-char hex encryption key |
| `/etc/olcrtc/env` | root:olcrtc | 0640 | EnvironmentFile read by systemd (CARRIER, TRANSPORT, LINK, ROOM_ID, KEY, DNS, etc.) |
| `/var/lib/olcrtc/` | olcrtc:olcrtc | 0750 | Per-process state directory |
| `/etc/systemd/system/olcrtc-server.service` | root:root | 0644 | Hardened systemd unit |
| `/etc/olcrtc/<N>/env` | root:olcrtc | 0640 | Config for additional instance N |
| `/etc/olcrtc/<N>/key.hex` | root:olcrtc | 0640 | Key for additional instance N |
| `/var/lib/olcrtc-<N>/` | olcrtc:olcrtc | 0750 | State directory for instance N |
| `/etc/systemd/system/olcrtc-server@.service` | root:root | 0644 | Systemd template unit (auto-created) |
| `/var/lib/olcrtc/subscriptions.db` | olcrtc:olcrtc | 0750 | Subscription SQLite database (if enabled) |

## Licenses

- olcrtc itself is **WTFPL**.
- The binaries and installer in this repository are derivative works of
  https://github.com/openlibrecommunity/olcrtc and inherit its license.

---

<a name="russian"></a>
## Русский

Этот скрипт ставит olcRTC-сервер на Linux VPS под `systemd` за одну команду.

### Самый быстрый путь

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/refactor-universal-carrier-fork/server-install/olcrtc-setup.sh | sudo bash
```

С residential SOCKS5-прокси (если IP VPS заблокирован у wbstream / jazz / telemost):

```bash
sudo bash olcrtc-setup.sh --socks-proxy USER:PASS@HOST:PORT
```

### Что произойдёт

1. Скачается бинарник `olcrtc` (~20 МБ, под `linux/amd64` или `linux/arm64`)
2. Создастся системный пользователь `olcrtc`
3. Сгенерируется 256-битный ключ шифрования в `/etc/olcrtc/key.hex`
4. Запишется конфиг в `/etc/olcrtc/env`
5. Зарегистрируется hardened `systemd`-юнит `olcrtc-server.service`
6. Wildberries Stream / SaluteJazz / Telemost создадут комнату при первом старте
7. Инсталлер выведет на экран **Provider**, **Room ID** и **Encryption key** — эти три значения нужно ввести в Android-приложение

### Сменить carrier / транспорт

Через интерактивное меню (пункты **3** и **4**), или флагами CLI:

```bash
sudo bash olcrtc-setup.sh --carrier wbstream                          # по умолчанию
sudo bash olcrtc-setup.sh --carrier jazz --transport datachannel      # SaluteJazz + быстрый транспорт
sudo bash olcrtc-setup.sh --carrier telemost --transport vp8channel   # Yandex Telemost
```

Старый флаг `--provider` принимается как алиас для `--carrier`.

### Повторный запуск (идемпотентность)

Скрипт можно запускать сколько угодно раз. Рекомендуется использовать интерактивное меню:

```bash
sudo bash olcrtc-setup.sh   # открывается интерактивное меню управления
```

Для скриптового использования доступны CLI-флаги:

```bash
sudo bash olcrtc-setup.sh --regenerate                # сменить комнату (ключ остаётся)
sudo bash olcrtc-setup.sh --regenerate-key            # сменить и ключ, и комнату
sudo bash olcrtc-setup.sh --carrier jazz              # сменить carrier
sudo bash olcrtc-setup.sh --transport vp8channel      # сменить транспорт
sudo bash olcrtc-setup.sh --socks-proxy host:port     # включить SOCKS5 (NO_AUTH)
sudo bash olcrtc-setup.sh --socks-proxy u:p@h:port    # включить SOCKS5 (USER/PASSWORD)
sudo bash olcrtc-setup.sh --socks-proxy ""            # выключить SOCKS5
sudo bash olcrtc-setup.sh --debug                     # включить verbose-логирование
sudo bash olcrtc-setup.sh --no-debug                  # выключить verbose-логирование
```

### Проверка после установки

```bash
sudo systemctl status olcrtc-server               # сервис должен быть active
sudo journalctl -u olcrtc-server -n 50 --no-pager # должна быть строка "room created"
sudo grep -E '^OLCRTC_(CARRIER|TRANSPORT|ROOM_ID|KEY)=' /etc/olcrtc/env
```

Эти значения (Carrier, Transport, Room ID, Key) вводятся в Android-приложение в настройках профиля **olcRTC**.

### Что идёт через прокси, что не идёт (с v0.1.3)

| Трафик                                                       | Маршрут                          |
|--------------------------------------------------------------|----------------------------------|
| Регистрация в carrier (HTTP API + WebSocket signalling)      | через прокси                     |
| WebRTC media (UDP между VPS и Android)                        | напрямую (UDP не идёт через CONNECT) |
| TCP-трафик клиента через туннель (Telegram, браузер и т.д.)  | **напрямую с VPS, минуя прокси** |

Это важно: до v0.1.3 (включая v0.1.2) клиентский TCP-трафик тоже шёл через прокси, из-за чего Telegram и другие сервисы, заблокированные в РФ, не работали через туннель. **Используй v0.1.3 или выше.**

### Несколько инстансов

На одном VPS можно запустить несколько независимых olcRTC-серверов, каждый со
своим room ID, ключом, carrier и транспортом. Управление через интерактивное меню:

```bash
sudo bash olcrtc-setup.sh   # → пункт 20) Управление инстансами
```

Каждый дополнительный инстанс (`#2`, `#3`, …) получает свой конфиг
(`/etc/olcrtc/<N>/env`), ключ (`/etc/olcrtc/<N>/key.hex`) и state-директорию
(`/var/lib/olcrtc-<N>/`). Все инстансы используют один бинарник и одного
системного пользователя. Лимит — 20 дополнительных инстансов.

### Подписки

Система подписок позволяет создать постоянный URL (например
`http://IP:2096/sub/xJGHpw`), который клиент добавляет один раз. После
пересоздания сервера достаточно импортировать подписки, добавить новый
инстанс — и клиент подхватит новые параметры без повторного сканирования QR.

**Включение при установке:**

При первом запуске `olcrtc-setup.sh` спросит:

```
Включить сервер подписок? (y/N): y
Порт сервера подписок [Enter = 2096]: 2096
```

**Управление подписками:**

```bash
sudo bash olcrtc-setup.sh   # → пункт 30) Управление подписками
```

Подменю:

| # | Действие | Описание |
|---|----------|----------|
| 1 | **Список подписок** | Все подписки с инстансами |
| 2 | **Создать подписку** | Ввести имя, slug генерируется автоматически (6 символов) |
| 3 | **Добавить инстанс** | Выбрать подписку, вставить полную `olcrtc://` URI |
| 4 | **Убрать инстанс** | Выбрать подписку, убрать один инстанс по ID |
| 5 | **Открепить все инстансы** | Убрать все инстансы из подписки (подписка остаётся пустой) |
| 6 | **Удалить подписку** | Удалить подписку; если есть инстансы — спросит удалить или открепить |
| 7 | **Экспорт** | Сохранить подписки в JSON-файл |
| 8 | **Импорт** | Загрузить подписки из JSON-файла |

**Сценарий «обновление сервера»:**

1. Экспорт: пункт **30 → 7** → сохраняет `/tmp/olcrtc-subscriptions.json`.
2. На новом сервере: установить с подписками, импорт: **30 → 8**.
3. Slug `xJGHpw` восстановлен → добавить новый инстанс через **30 → 3**.
4. Клиент обновляет подписку — получает новые параметры.

**Привязка домена (опционально):**

По умолчанию клиент обращается к `http://<IP>:2096/sub/{slug}`.
С доменом: `https://sub.example.com/sub/{slug}` — HTTPS, без порта.

1. **DNS** — A-запись `sub.example.com → IP VPS`.
2. **Сертификат** — `sudo certbot --nginx -d sub.example.com`
   (или `certbot certonly --standalone` / `--webroot`).
3. **nginx** — два варианта:

   **3а. Обычный nginx** (порт 443 свободен, нет 3x-ui):

   ```bash
   sudo tee /etc/nginx/sites-available/olcrtc-sub <<'EOF'
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
   EOF
   sudo ln -sf /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/
   sudo nginx -t && sudo systemctl reload nginx
   ```

   **3б. nginx + SNI мультиплексор (3x-ui / xray / reality):**

   Если на сервере 3x-ui, порт 443 перехватывает `stream {}` блок с
   `ssl_preread` → обычный `listen 443 ssl` не получит трафик.

   Решение: добавить upstream + SNI-правило в stream-конфиг, а HTTP-блок
   слушает на внутреннем порту (например 9443):

   ```bash
   # 1. Upstream + SNI запись в stream-конфиге
   sudo sed -i '/upstream www {/i\upstream olcrtc_sub {\n    server 127.0.0.1:9443;\n}\n' \
       /etc/nginx/stream-enabled/*.conf
   sudo sed -i '/default/i\    sub.example.com            olcrtc_sub;' \
       /etc/nginx/stream-enabled/*.conf

   # 2. HTTP server block на внутреннем порту
   sudo tee /etc/nginx/sites-available/olcrtc-sub <<'EOF'
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
   EOF
   sudo ln -sf /etc/nginx/sites-available/olcrtc-sub /etc/nginx/sites-enabled/
   sudo nginx -t && sudo systemctl reload nginx
   ```

   > Если в stream-блоке **нет** `proxy_protocol on;`, уберите
   > `proxy_protocol` из `listen` и строки `real_ip_header` /
   > `set_real_ip_from`.

   Маршруты 3x-ui **не затрагиваются** — добавляется только новое
   SNI-правило; `default` по-прежнему уходит в xray.

4. **Закрыть порт 2096 снаружи (опционально):**
   `sudo ufw deny 2096/tcp` — nginx ходит к `127.0.0.1:2096` локально.

### Удаление

Рекомендуется использовать скрипт удаления (удаляет все инстансы):

```bash
# Из checkout:
sudo bash olcrtc-uninstall.sh

# Или одной командой:
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/refactor-universal-carrier-fork/server-install/olcrtc-uninstall.sh | sudo bash
```

Ручное удаление:

```bash
sudo systemctl disable --now olcrtc-server
sudo rm -f /etc/systemd/system/olcrtc-server.service /etc/systemd/system/olcrtc-server@.service
sudo systemctl daemon-reload
sudo rm -rf /etc/olcrtc /var/lib/olcrtc /var/lib/olcrtc-* /usr/local/bin/olcrtc /usr/local/bin/olcrtc-launcher
sudo userdel olcrtc 2>/dev/null || true
```

### Полная документация

Английская версия выше содержит подробности по всем флагам, путям, формату конфига, сборке из исходников и устройству systemd-юнита. Русский раздел — это TL;DR.
