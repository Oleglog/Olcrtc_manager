# olcRTC server — systemd installer

> [**Русский**](#russian) ниже • Full English documentation continues below

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
- asks the selected carrier (Wildberries Stream / SaluteJazz / Yandex Telemost)
  to provision a room on first start,
- captures the auto-generated room ID from `journalctl` and pins it into the
  service environment so the same room is reused across restarts,
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

**Option A — from a clean checkout of master** (binary auto-downloaded from
the matching Release):

```bash
# On your VPS, as root:
git clone https://github.com/Oleglog/Olcrtc_manager
cd Olcrtc_manager
sudo ./server-install/install.sh
```

**Option B — from a release tarball** (binary already inside):

```bash
curl -fsSL -o /tmp/olcrtc.tgz \
    https://github.com/Oleglog/Olcrtc_manager/releases/latest/download/olcrtc-server-installer-0.1.3.tgz
rm -rf /tmp/olcrtc-server-installer-*
tar -xzf /tmp/olcrtc.tgz -C /tmp
sudo /tmp/olcrtc-server-installer-*/install.sh
```

**Option C — build from source** (no GitHub access on the VPS / reproducible
build required):

```bash
cd Olcrtc_manager
./server-install/build-from-source.sh   # produces server-install/bin/olcrtc-linux-{amd64,arm64}
sudo ./server-install/install.sh
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

```bash
sudo ./install.sh --carrier telemost --transport vp8channel
sudo ./install.sh --carrier jazz --transport datachannel
sudo ./install.sh --carrier wbstream                       # default transport = datachannel
```

The legacy `--provider` flag is still accepted as an alias for `--carrier`.

For **telemost**, the room ID is whatever string you choose — it is not
provisioned by Yandex. You can override the auto-generated `olcrtc-XXXX`
ID by setting `OLCRTC_ROOM_ID=...` before running the installer:

```bash
sudo OLCRTC_ROOM_ID=my-vpn-room ./install.sh --provider telemost
```

## Re-running the installer

The installer is idempotent — re-running keeps the existing key, room ID,
proxy and debug settings unless you ask otherwise:

```bash
sudo ./install.sh                                   # update binary / unit file, keep everything
sudo ./install.sh --regenerate                      # keep key, get a new room ID
sudo ./install.sh --regenerate-key                  # rotate everything (key + room)
sudo ./install.sh --carrier jazz                    # change carrier
sudo ./install.sh --transport vp8channel            # change transport
sudo ./install.sh --socks-proxy host:port           # route outbound through SOCKS5 (NO_AUTH)
sudo ./install.sh --socks-proxy user:pass@h:port    # route outbound through SOCKS5 (USER/PASSWORD)
sudo ./install.sh --socks-proxy ""                  # remove existing SOCKS5 proxy
sudo ./install.sh --debug                           # enable -debug logging
sudo ./install.sh --no-debug                        # disable -debug logging
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

```bash
# IP-whitelisted proxy (no credentials):
sudo ./install.sh --socks-proxy 1.2.3.4:1080

# Username/password proxy (RFC 1929):
sudo ./install.sh --socks-proxy alice:hunter2@1.2.3.4:1080

# Optional `socks5://` / `socks5h://` scheme is accepted and stripped:
sudo ./install.sh --socks-proxy socks5://alice:hunter2@1.2.3.4:1080
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

```bash
sudo ./install.sh --debug
journalctl -u olcrtc-server -f
```

You will see ICE candidate negotiation, DTLS state changes, and per-stream
errors. Useful for diagnosing reconnects on Telemost or one-off DTLS
timeouts. Re-run with `--no-debug` to switch back.

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

## Uninstall

Recommended — use the uninstall script (handles all instances):

```bash
# From a checkout:
sudo bash olcrtc-uninstall.sh

# Or one-liner (no checkout needed):
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
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
curl -fsSL -o /tmp/olcrtc.tgz \
    https://github.com/Oleglog/Olcrtc_manager/releases/latest/download/olcrtc-server-installer-0.1.3.tgz
rm -rf /tmp/olcrtc-server-installer-*
tar -xzf /tmp/olcrtc.tgz -C /tmp
sudo /tmp/olcrtc-server-installer-*/install.sh
```

С residential SOCKS5-прокси (если IP VPS заблокирован у wbstream / jazz / telemost):

```bash
sudo /tmp/olcrtc-server-installer-*/install.sh \
    --socks-proxy USER:PASS@HOST:PORT
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

```bash
sudo ./install.sh --carrier wbstream                          # по умолчанию
sudo ./install.sh --carrier jazz --transport datachannel      # SaluteJazz + быстрый транспорт
sudo ./install.sh --carrier telemost --transport vp8channel   # Yandex Telemost
```

Старый флаг `--provider` принимается как алиас для `--carrier`.

### Повторный запуск (идемпотентность)

Инсталлер можно запускать сколько угодно раз — он не трогает ключ/комнату/прокси, если ты не попросишь явно:

```bash
sudo ./install.sh                             # обновить бинарник/юнит, всё остальное сохранить
sudo ./install.sh --regenerate                # сменить комнату (ключ остаётся)
sudo ./install.sh --regenerate-key            # сменить и ключ, и комнату
sudo ./install.sh --carrier jazz              # сменить carrier
sudo ./install.sh --transport vp8channel      # сменить транспорт
sudo ./install.sh --socks-proxy host:port     # включить SOCKS5 (NO_AUTH)
sudo ./install.sh --socks-proxy u:p@h:port    # включить SOCKS5 (USER/PASSWORD)
sudo ./install.sh --socks-proxy ""            # выключить SOCKS5
sudo ./install.sh --debug                     # включить verbose-логирование
sudo ./install.sh --no-debug                  # выключить verbose-логирование
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

### Удаление

Рекомендуется использовать скрипт удаления (удаляет все инстансы):

```bash
# Из checkout:
sudo bash olcrtc-uninstall.sh

# Или одной командой:
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
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
