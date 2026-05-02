<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="200" height="200">

# olcRTC — across the Sea

**Tunnel TCP traffic over WebRTC DataChannels through public Russian video-conferencing services to bypass IP-based censorship.**

![License](https://img.shields.io/badge/license-WTFPL-0D1117?style=flat-square&logo=open-source-initiative&logoColor=green&labelColor=0D1117)
![Golang](https://img.shields.io/badge/Go-1.25%2B-0D1117?style=flat-square&logo=go&logoColor=00A7D0)
![Status](https://img.shields.io/badge/status-alpha-orange?style=flat-square)

[**Русский**](#русский) | [**English**](#english) | [**Releases**](https://github.com/Oleglog/olcrtc_FORK/releases) | [**Android client**](https://github.com/Oleglog/olcrtc-android)

</div>

---

## Русский

### Что это

olcRTC — это VPN-туннель необычной конструкции. Вместо TCP/UDP-протокола (как WireGuard, OpenVPN, Shadowsocks) olcRTC заворачивает TCP-трафик в **WebRTC DataChannel** и отправляет через инфраструктуру общедоступных российских видеоконференций — **Wildberries Stream**, **Yandex Telemost**, **SaluteJazz** — которые не блокируются Роскомнадзором и используются миллионами устройств.

Поток данных:

```
[Android] ──SOCKS5──▶ [olcRTC client (на телефоне)]
                            │
                            ▼ WebRTC DataChannel
                  [SFU видеоконф-сервиса в РФ]
                            ▲
                            │ WebRTC DataChannel
                            │
            [olcRTC server на VPS вне РФ]
                            │
                            ▼
                       [Internet]
```

### Возможности этого форка

По сравнению с upstream [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc), форк добавляет:

- **SOCKS5 USER/PASSWORD аутентификация** ([RFC 1929](https://www.rfc-editor.org/rfc/rfc1929)) — обязательна для коммерческих residential-прокси, у которых нет IP-whitelist
- **Split-routing** для прокси (с v0.1.3): провайдерская сигнализация идёт через прокси, клиентский трафик — напрямую с VPS, чтобы не упираться в РФ-блокировку Telegram и других сервисов
- **systemd-инсталлер и менеджер** (`server-install/olcrtc-setup.sh`) — установка одной командой + интерактивное меню управления сервером, генерация URI-профиля и QR-кода для импорта в Android-приложение
- **Релизные тарболлы** с прекомпилированными бинарниками для linux/amd64 и linux/arm64
- **Подробная документация** по компонентам и безопасности

### Быстрый старт (сервер на VPS)

#### Способ 1 — из тарболла (самый простой)

```bash
curl -fsSL -o /tmp/olcrtc.tgz \
    https://github.com/Oleglog/olcrtc_FORK/releases/latest/download/olcrtc-server-installer-0.2.0.tgz
rm -rf /tmp/olcrtc-server-installer-*
tar -xzf /tmp/olcrtc.tgz -C /tmp
sudo /tmp/olcrtc-server-installer-*/olcrtc-setup.sh
```

С residential-прокси (если IP VPS заблокирован у wb_stream/jazz):

```bash
sudo /tmp/olcrtc-server-installer-*/olcrtc-setup.sh \
    --socks-proxy USER:PASS@HOST:PORT
```

#### Способ 2 — из клона репозитория

```bash
git clone https://github.com/Oleglog/olcrtc_FORK
cd olcrtc_FORK
sudo ./server-install/olcrtc-setup.sh
```

#### Способ 3 — собрать из исходников и установить

```bash
git clone https://github.com/Oleglog/olcrtc_FORK
cd olcrtc_FORK
./server-install/build-from-source.sh   # собирает amd64 + arm64 в server-install/bin
sudo ./server-install/olcrtc-setup.sh   # инсталлер увидит локальные бинарники
```

В конце инсталлер напечатает:
- Плашку с **Provider**, **Room ID**, **Encryption key** и **Name**
- **URI-профиль** для импорта в Android-приложение: `olcrtc://<provider>@room/<room_id>?key=<key>#<name>`
- **QR-код** этого URI прямо в терминале — отсканируйте его из Android-приложения

Подробности и все опции инсталлера — в [`server-install/README.md`](server-install/README.md).

### Сборка из исходников

Требования: **Go 1.25+**, опционально `mage` для удобного запуска тасков.

#### CLI / сервер

```bash
# Самый простой способ:
go build -o olcrtc ./cmd/olcrtc

# Или через mage:
go install github.com/magefile/mage@latest
mage buildCLI                   # CLI/сервер
mage build                      # CLI + UI
mage cross                      # кросс-компиляция под все платформы
mage test                       # тесты
mage lint                       # golangci-lint
```

Получишь бинарник `olcrtc` (~20 МБ, статически слинкован, без CGO).

#### Десктопный UI (Fyne)

```bash
mage buildUI
```

#### Android AAR (gomobile)

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android -androidapi 21 ./mobile

# или одной командой через mage:
mage mobile
```

Получишь `mobile.aar`, который можно вкомпилить в любое Android-приложение. Готовое Android-приложение со всей UI — [olcrtc-android](https://github.com/Oleglog/olcrtc-android).

#### Контейнеры

```bash
mage podman                     # podman build
mage docker                     # docker build
```

Готовый `docker-compose.server.yml` лежит в корне репозитория.

### Запуск без инсталлера

Если хочешь руками, без systemd:

```bash
# Сервер
./olcrtc -mode srv -provider wb_stream -id any -key $(openssl rand -hex 32)

# Сервер с residential-прокси
./olcrtc -mode srv -provider wb_stream -id any -key <hex> \
    -socks-proxy 1.2.3.4 -socks-proxy-port 1080 \
    -socks-proxy-user alice -socks-proxy-pass hunter2

# Клиент (на десктопе/ноуте)
./olcrtc -mode cnc -provider wb_stream -id <ROOM_ID> -key <hex> \
    -socks-host 127.0.0.1 -socks-port 1080
```

Список флагов: `./olcrtc -h`.

### Провайдеры

| Провайдер     | CLI                       | Регистрация на сервере | Подключение с устройства | Заметки                                           |
|---------------|---------------------------|------------------------|--------------------------|---------------------------------------------------|
| `wb_stream`   | `--provider wb_stream`    | HTTP API               | через WebRTC к серверу   | По умолчанию. Самый стабильный                    |
| `jazz`        | `--provider jazz`         | HTTP + WebSocket       | **WebSocket с устройства напрямую к jazz.sber.ru** | Клиент должен иметь возможность достучаться до jazz инфраструктуры |
| `telemost`    | `--provider telemost`     | HTTP + WebSocket       | WebSocket с устройства   | Аналогично jazz; user-supplied room ID            |

> **Совет:** начинай с `wb_stream`. Он работает у всех, потому что клиент общается только с твоим VPS. `jazz` и `telemost` требуют, чтобы устройство имело прямой доступ к российской инфраструктуре Сбера/Яндекса.

### SOCKS5-прокси на VPS (обходим блокировку IP)

Wildberries Stream и SaluteJazz блокируют большинство IP-адресов датацентров — нужен residential / RU-IP, чтобы зарегистрировать гостевую сессию. Если ты получаешь `i/o timeout` при `WB Stream room creation`, значит твой VPS-IP в чёрном списке. Решение — пустить **только** регистрационные запросы через residential SOCKS5-прокси:

```bash
# IP-whitelisted прокси (без логина):
sudo ./server-install/olcrtc-setup.sh --socks-proxy 1.2.3.4:1080

# С логином/паролем (RFC 1929):
sudo ./server-install/olcrtc-setup.sh --socks-proxy alice:hunter2@1.2.3.4:1080

# Поддерживаются схемы socks5:// и socks5h://:
sudo ./server-install/olcrtc-setup.sh --socks-proxy socks5://alice:hunter2@1.2.3.4:1080

# Или через интерактивное меню (если сервер уже установлен):
sudo ./server-install/olcrtc-setup.sh   # → пункт 6
```

Что идёт через прокси и что не идёт (с v0.1.3):

| Трафик                                                     | Маршрут                          |
|------------------------------------------------------------|----------------------------------|
| Регистрация в провайдере (HTTP API/WS)                      | через прокси                     |
| WebRTC media (UDP между VPS и Android)                      | напрямую (UDP не идёт через CONNECT) |
| TCP-трафик клиента через туннель (Telegram, Google и т.д.) | **напрямую с VPS, минуя прокси** |

Это важно: если бы клиентский трафик тоже шёл через RU-прокси, то Telegram (заблокированный в РФ) не открывался бы. До v0.1.2 включительно был именно такой баг. С v0.1.3 — исправлено.

### Конфигурация

После установки конфигурация хранится тут:

| Путь                                       | Содержимое                                                                |
|--------------------------------------------|---------------------------------------------------------------------------|
| `/etc/olcrtc/env`                          | `OLCRTC_PROVIDER`, `OLCRTC_ROOM_ID`, `OLCRTC_KEY`, `OLCRTC_DNS`, `OLCRTC_DEBUG`, `OLCRTC_SOCKS_PROXY`, `OLCRTC_NAME` |
| `/etc/olcrtc/key.hex`                      | 64-символьный hex-ключ шифрования                                         |
| `/var/lib/olcrtc/`                         | Per-process state                                                         |
| `/etc/systemd/system/olcrtc-server.service` | systemd unit                                                             |
| `/usr/local/bin/olcrtc`                    | Бинарник                                                                  |
| `/usr/local/bin/olcrtc-launcher`           | Launcher-скрипт, который читает env и формирует флаги                     |

Посмотреть текущие реквизиты:

```bash
sudo grep -E '^OLCRTC_(PROVIDER|ROOM_ID|KEY)=' /etc/olcrtc/env
```

### Управление сервисом

#### Интерактивный менеджер

Если сервер уже установлен, запустите скрипт без аргументов — откроется текстовое меню:

```bash
sudo ./server-install/olcrtc-setup.sh
```

```
============================================================
  olcRTC Server Manager
  Provider: wb_stream | Room: abc123xyz | IP: 1.2.3.4
============================================================

  1) Статус сервиса
  2) Показать URI / QR-код
  3) Сменить провайдера
  4) Пересоздать room ID  (--regenerate)
  5) Ротация ключа + room ID  (--regenerate-key)
  6) Настроить SOCKS5-прокси
  7) Убрать SOCKS5-прокси
  8) Включить / выключить debug-логирование
  9) Переименовать соединение (name)
  0) Выход
```

Через меню можно получить URI и QR-код для импорта профиля в Android-приложение, сменить провайдера, ротировать ключи, настроить прокси и т.д.

#### Команды systemd

```bash
sudo systemctl status olcrtc-server      # статус
sudo journalctl -u olcrtc-server -f      # live логи
sudo systemctl restart olcrtc-server     # рестарт
sudo systemctl stop olcrtc-server        # остановить
sudo systemctl disable olcrtc-server     # не запускать при загрузке
```

### Удаление

```bash
sudo systemctl disable --now olcrtc-server
sudo rm -f /etc/systemd/system/olcrtc-server.service
sudo systemctl daemon-reload
sudo rm -rf /etc/olcrtc /var/lib/olcrtc /usr/local/bin/olcrtc /usr/local/bin/olcrtc-launcher
sudo userdel olcrtc 2>/dev/null || true
```

### Релизы

| Версия         | Изменения                                                                                                  |
|----------------|------------------------------------------------------------------------------------------------------------|
| **server-v0.2.0** | `olcrtc-setup.sh`: инсталлятор + интерактивный менеджер, URI-профиль, QR-код, `OLCRTC_NAME`, `--id` для Telemost. Recommended. |
| server-v0.1.3 | Split-routing: провайдер через прокси, клиентский трафик direct.                            |
| server-v0.1.2  | SOCKS5 USER/PASSWORD auth (RFC 1929). **Архитектурный баг**: клиентский трафик тоже шёл через прокси.      |
| server-v0.1.1  | SOCKS5 NO_AUTH only.                                                                                        |
| server-v0.1.0  | Первый релиз.                                                                                              |

[**Все релизы и changelogs**](https://github.com/Oleglog/olcrtc_FORK/releases)

### Безопасность

- Бинарник работает как системный пользователь `olcrtc` (не root)
- systemd unit имеет `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `MemoryDenyWriteExecute=yes`
- Ключ шифрования в `/etc/olcrtc/key.hex` — права `0640 root:olcrtc`
- Подробнее — [SECURITY.md](SECURITY.md)

### Roadmap / TODO

- [ ] Поддержка SOCKS5-прокси на стороне Android-клиента (для jazz/telemost, чтобы устройство могло достучаться до signalling через прокси)
- [ ] Multi-peer для увеличения throughput (сейчас один WebRTC peer)
- [ ] Рандомизация fingerprint (User-Agent, DeviceName) для обхода throttling

### Issues / Контакты

- Issues: [openlibrecommunity/olcrtc/issues](https://github.com/openlibrecommunity/olcrtc/issues) (upstream)
- Telegram: [@openlibrecommunity](https://t.me/openlibrecommunity)
- Этот форк: [Oleglog/olcrtc_FORK/issues](https://github.com/Oleglog/olcrtc_FORK/issues)

---

## English

### What is this

olcRTC is an unconventional VPN tunnel. Instead of a TCP/UDP protocol (like WireGuard, OpenVPN, Shadowsocks), olcRTC wraps TCP traffic into a **WebRTC DataChannel** and routes it through publicly-accessible Russian video-conferencing services — **Wildberries Stream**, **Yandex Telemost**, **SaluteJazz** — which are not blocked by Roskomnadzor and are used by millions of devices.

Data flow:

```
[Android] ──SOCKS5──▶ [olcRTC client on phone]
                            │
                            ▼ WebRTC DataChannel
                  [Russian video-conf SFU]
                            ▲
                            │ WebRTC DataChannel
                            │
              [olcRTC server on VPS abroad]
                            │
                            ▼
                       [Internet]
```

### What this fork adds

Compared to upstream [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc):

- **SOCKS5 USER/PASSWORD authentication** ([RFC 1929](https://www.rfc-editor.org/rfc/rfc1929)) — required for commercial residential proxies that don't support IP whitelisting
- **Split routing** for the proxy (since v0.1.3): provider signalling goes through the proxy, client TCP traffic exits direct from the VPS — so geo-restricted services (Telegram from RU IPs) remain reachable
- **systemd installer & manager** (`server-install/olcrtc-setup.sh`) — single-command deployment + interactive management menu with URI profile and QR code generation for the Android app
- **Release tarballs** with prebuilt binaries for linux/amd64 and linux/arm64
- **Detailed documentation**

### Quick start (server on VPS)

#### Option 1 — from a release tarball

```bash
curl -fsSL -o /tmp/olcrtc.tgz \
    https://github.com/Oleglog/olcrtc_FORK/releases/latest/download/olcrtc-server-installer-0.2.0.tgz
rm -rf /tmp/olcrtc-server-installer-*
tar -xzf /tmp/olcrtc.tgz -C /tmp
sudo /tmp/olcrtc-server-installer-*/olcrtc-setup.sh

# With a residential SOCKS5 proxy (if your VPS IP is blocked by wb_stream / jazz):
sudo /tmp/olcrtc-server-installer-*/olcrtc-setup.sh --socks-proxy USER:PASS@HOST:PORT
```

#### Option 2 — from a git checkout

```bash
git clone https://github.com/Oleglog/olcrtc_FORK
cd olcrtc_FORK
sudo ./server-install/olcrtc-setup.sh
```

#### Option 3 — build binaries yourself, then install

```bash
git clone https://github.com/Oleglog/olcrtc_FORK
cd olcrtc_FORK
./server-install/build-from-source.sh   # builds amd64 + arm64 into server-install/bin
sudo ./server-install/olcrtc-setup.sh   # picks up local binaries
```

After installation, the script prints:
- **Provider**, **Room ID**, **Encryption key** and **Name**
- **URI profile** for import into the Android app: `olcrtc://<provider>@room/<room_id>?key=<key>#<name>`
- **QR code** right in the terminal — scan it from the Android app

Full installer documentation: [`server-install/README.md`](server-install/README.md).

### Build from source

Requires **Go 1.25+**, optionally `mage`.

```bash
go build -o olcrtc ./cmd/olcrtc            # CLI binary

# or via mage:
go install github.com/magefile/mage@latest
mage buildCLI                              # CLI/server only
mage build                                 # CLI + Fyne UI
mage cross                                 # all platforms
mage mobile                                # Android AAR via gomobile
mage podman   /  mage docker               # container image
mage test     /  mage lint    /  mage clean
```

For the Android AAR specifically:

```bash
go install golang.org/x/mobile/cmd/gomobile@latest
gomobile init
gomobile bind -target=android -androidapi 21 ./mobile
```

The full Android client (UI, settings, package management) lives in [olcrtc-android](https://github.com/Oleglog/olcrtc-android), which embeds this AAR.

### Manual run (no installer)

```bash
# Server
./olcrtc -mode srv -provider wb_stream -id any -key $(openssl rand -hex 32)

# Server through a residential proxy:
./olcrtc -mode srv -provider wb_stream -id any -key <hex> \
    -socks-proxy 1.2.3.4 -socks-proxy-port 1080 \
    -socks-proxy-user alice -socks-proxy-pass hunter2

# Client (desktop/laptop)
./olcrtc -mode cnc -provider wb_stream -id <ROOM_ID> -key <hex> \
    -socks-host 127.0.0.1 -socks-port 1080
```

Run `./olcrtc -h` for the full flag list.

### Providers

| Provider    | CLI                     | Server-side registration | Client-side connect                                | Notes                                          |
|-------------|-------------------------|--------------------------|----------------------------------------------------|------------------------------------------------|
| `wb_stream` | `--provider wb_stream`  | HTTP API                 | WebRTC to your server                              | Default. Most reliable                         |
| `jazz`      | `--provider jazz`       | HTTP + WebSocket          | **Direct WebSocket from device to jazz.sber.ru**   | Device must reach Sber's signalling server     |
| `telemost`  | `--provider telemost`   | HTTP + WebSocket          | WebSocket from device                              | Like jazz; user-supplied room ID                |

> **Tip:** start with `wb_stream`. It works for everyone because the client only talks to your VPS. `jazz` and `telemost` require the device to have direct connectivity to Russian Sber/Yandex infrastructure.

### SOCKS5 proxy on the VPS (bypass IP-based registration block)

WB Stream and SaluteJazz block most datacenter IPs and require a residential / RU IP for guest registration. If you get `i/o timeout` at `WB Stream room creation`, your VPS IP is blacklisted. Fix: route **only** the provider registration through a residential SOCKS5 proxy:

```bash
# IP-whitelisted proxy:
sudo ./server-install/olcrtc-setup.sh --socks-proxy 1.2.3.4:1080

# Username/password (RFC 1929):
sudo ./server-install/olcrtc-setup.sh --socks-proxy alice:hunter2@1.2.3.4:1080

# Or via interactive menu (if server is already installed):
sudo ./server-install/olcrtc-setup.sh   # → option 6
```

What goes through the proxy (since v0.1.3):

| Traffic                                                | Route                            |
|--------------------------------------------------------|----------------------------------|
| Provider registration (HTTP API / WS)                  | through proxy                    |
| WebRTC media (UDP)                                      | direct (SOCKS5 doesn't tunnel UDP) |
| Tunnelled client TCP (Telegram, Google, etc.)          | **direct from VPS, NOT proxy**   |

This is critical: if client TCP went through the RU proxy, geo-blocked services like Telegram would be unreachable. Pre-v0.1.3 had this bug. v0.1.3 fixes it.

### Configuration

| Path                                          | Contents                                                                                              |
|-----------------------------------------------|-------------------------------------------------------------------------------------------------------|
| `/etc/olcrtc/env`                             | `OLCRTC_PROVIDER`, `OLCRTC_ROOM_ID`, `OLCRTC_KEY`, `OLCRTC_DNS`, `OLCRTC_DEBUG`, `OLCRTC_SOCKS_PROXY`, `OLCRTC_NAME` |
| `/etc/olcrtc/key.hex`                         | 64-char hex encryption key                                                                            |
| `/var/lib/olcrtc/`                            | Runtime state                                                                                         |
| `/etc/systemd/system/olcrtc-server.service`   | systemd unit                                                                                          |

Read your credentials:

```bash
sudo grep -E '^OLCRTC_(PROVIDER|ROOM_ID|KEY)=' /etc/olcrtc/env
```

### Service management

#### Interactive manager

If the server is already installed, run the script without arguments to open the management menu:

```bash
sudo ./server-install/olcrtc-setup.sh
```

The menu lets you view status, get URI/QR code for the Android app, switch providers, rotate keys, configure proxy, toggle debug logging, and rename the connection.

#### systemd commands

```bash
sudo systemctl status olcrtc-server
sudo journalctl -u olcrtc-server -f
sudo systemctl restart olcrtc-server
sudo systemctl stop olcrtc-server
```

### Uninstall

```bash
sudo systemctl disable --now olcrtc-server
sudo rm -f /etc/systemd/system/olcrtc-server.service
sudo systemctl daemon-reload
sudo rm -rf /etc/olcrtc /var/lib/olcrtc /usr/local/bin/olcrtc /usr/local/bin/olcrtc-launcher
sudo userdel olcrtc 2>/dev/null || true
```

### Releases

| Version           | Changes                                                                                          |
|-------------------|--------------------------------------------------------------------------------------------------|
| **server-v0.2.0** | `olcrtc-setup.sh`: installer + interactive manager, URI profile, QR code, `OLCRTC_NAME`, `--id` for Telemost. Recommended. |
| server-v0.1.3 | Split routing: provider through proxy, client traffic direct.                        |
| server-v0.1.2     | SOCKS5 USER/PASSWORD auth. **Architectural bug**: client traffic also went through proxy.         |
| server-v0.1.1     | SOCKS5 NO_AUTH only.                                                                              |
| server-v0.1.0     | Initial release.                                                                                  |

[**All releases & changelogs**](https://github.com/Oleglog/olcrtc_FORK/releases)

### Security

- Binary runs as a non-privileged system user `olcrtc`
- Hardened systemd unit: `NoNewPrivileges=yes`, `ProtectSystem=strict`, `ProtectHome=yes`, `PrivateTmp=yes`, `MemoryDenyWriteExecute=yes`
- Encryption key in `/etc/olcrtc/key.hex` is `0640 root:olcrtc`
- Details: [SECURITY.md](SECURITY.md)

### Roadmap

- [ ] Client-side SOCKS5 proxy for jazz/telemost (so the device can also reach Russian signalling via a proxy)
- [ ] Multi-peer for higher throughput
- [ ] User-agent/fingerprint randomization to avoid SFU throttling

### Contact

- Issues: [openlibrecommunity/olcrtc/issues](https://github.com/openlibrecommunity/olcrtc/issues) (upstream)
- This fork: [Oleglog/olcrtc_FORK/issues](https://github.com/Oleglog/olcrtc_FORK/issues)
- Telegram: [@openlibrecommunity](https://t.me/openlibrecommunity)

---

## License

[**WTFPL**](LICENSE) (inherited from upstream).

## Credits

- Original [olcRTC](https://github.com/openlibrecommunity/olcrtc) by [zarazaex](https://t.me/zarazaexe)
- This fork: [Oleglog](https://github.com/Oleglog)
- Logo: [openlibrecommunity/material](https://github.com/openlibrecommunity/material)

<div align="center">

Made for: [olcNG](https://github.com/zarazaex69/olcng)

</div>
