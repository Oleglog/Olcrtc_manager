<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="250" height="250">

![License](https://img.shields.io/badge/license-WTFPL-0D1117?style=flat-square&logo=open-source-initiative&logoColor=green&labelColor=0D1117)
![Golang](https://img.shields.io/badge/-Golang-0D1117?style=flat-square&logo=go&logoColor=00A7D0)

</div>

> ### ⚠ Known issues / Известные проблемы
>
> - **WB Stream**: stream.wb.ru отключил публичный API создания комнат и приём
>   гостей. Для `wbstream` нужно создать руму вручную на
>   <https://stream.wb.ru> и указать её ID при создании инстанса в Admin UI.
> - Самый быстрый старт — `jitsi` (auto-gen Room ID работает) или `telemost`
>   (инсталлер сам генерирует ID `olcrtc-XXXXXXXX`).

> ### Падение звонков/комнат
> Для поддержки живых комнат используйте
> [infinity-room-panel](https://github.com/juushimatsu/infinity-room-panel).

## About

olcRTC — across the sea.

Туннель TCP через WebRTC поверх «легальных» конференц-сервисов
(Yandex Telemost, Jitsi Meet, WB Stream): для внешнего наблюдателя это
обычный звонок, внутри — зашифрованный поток до приложения.

Этот форк ([Oleglog/Olcrtc_manager](https://github.com/Oleglog/Olcrtc_manager))
надстраивает над upstream
[openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc):

- one-command systemd-инсталлер (`server-install/olcrtc-setup.sh`),
- **Admin Web UI** на 8443 — все настройки, инстансы, подписки, обновления,
- мульти-инстансы (до 20 на VPS),
- сервер подписок (постоянный URL → клиент подхватывает изменения),
- pre-built бинарники для `linux/amd64` и `linux/arm64`,
- WARP-прокси для скрытия публичного IP VPS от клиентского трафика.

## Quick links

| | |
|---|---|
| **Server install (one command)** | [`server-install/`](server-install/) — [README](server-install/README.md) |
| **WARP proxy (hide VPS IP)** | [server-install/WARP-PROXY.md](server-install/WARP-PROXY.md) |
| **Android client** | [Oleglog/Exclave_FORK](https://github.com/Oleglog/Exclave_FORK) |
| **Upstream project** | [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc) |

## Server — quick start

Одна команда, без `git`:

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
```

Из чекаута:

```bash
git clone https://github.com/Oleglog/Olcrtc_manager
cd Olcrtc_manager
sudo bash server-install/olcrtc-setup.sh
```

После установки скрипт печатает URL Admin UI и креды:

```
Admin UI:  https://<VPS-IP>:8443
Логин:     admin
Пароль:    admin
```

Дальнейшее управление (carrier, транспорт, SOCKS, WARP, инстансы,
подписки, обновления) — целиком через web-интерфейс.

> ⚠ Сертификат самоподписанный — в браузере подтвердите «Перейти».
> При первом входе **смените пароль** в настройках.

См. полную документацию по серверу: **[server-install/README.md](server-install/README.md)**.

[About](docs/about.md) · [Client URI format](docs/uri.md)

## Carrier & transport matrix

| Transport | telemost | wbstream | jitsi |
|-----------|:--------:|:--------:|:-----:|
| datachannel | ✗ | ✓ | ✓ |
| vp8channel | ✓ | ✓ | ✓ |
| seichannel | ✗ | ✓ | ✓ |
| videochannel | ✓ | ✓ | ✓ |

Speed: **datachannel** (~6 MB/s) > **vp8channel** > **seichannel** > **videochannel** (~200 KB/s)

Defaults: **carrier=`jitsi`**, **transport=`vp8channel`**.

## Server management

Скрипт `olcrtc-setup.sh` — install-only. После установки повторный запуск
без флагов покажет статус и URL Admin UI. Управление — через Admin UI или
эти флаги:

```bash
sudo bash olcrtc-setup.sh --update           # обновить бинарники
sudo bash olcrtc-setup.sh --status           # статус сервисов
sudo bash olcrtc-setup.sh --show-token       # показать логин/пароль
sudo bash olcrtc-setup.sh --regenerate       # пересоздать Room ID
sudo bash olcrtc-setup.sh --regenerate-key   # пересоздать ключ + Room ID
sudo bash olcrtc-setup.sh --uninstall        # полное удаление
```

Флаги `--carrier`, `--transport`, `--name`, `--id` — только для первичной
установки (после установки меняются через Admin UI).

### Uninstall

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
```

## SOCKS5 для signalling

Если IP VPS заблокирован у wbstream / jitsi / telemost (нужен
residential / RU-IP), укажите SOCKS5 в карточке инстанса в Admin UI
(поле **SOCKS proxy**: `host:port` или `user:pass@host:port`,
RFC 1929 поддерживается).

Через SOCKS идёт **только carrier-трафик** (HTTP API + WebSocket
signalling). Клиентский TCP-туннель уходит напрямую с VPS — иначе
гео-блокированные сервисы (Telegram и т.п.) перестали бы работать.

## WARP proxy (hide VPS IP)

Чтобы посещаемые сайты видели Cloudflare WARP-IP вместо IP вашего VPS,
поднимите локальный SOCKS5 поверх WARP (через `wireproxy` или
3X-UI inbound) и укажите его в инстансе:

```
OLCRTC_WARP_PROXY=127.0.0.1:40000
```

Через Admin UI: поле **WARP proxy** в карточке инстанса. Через env: правка
`/etc/olcrtc/<N>/env` + `systemctl restart olcrtc-server@<N>`.

WARP применяется **только** к клиентскому туннельному трафику; signalling
остаётся через прямое подключение или через SOCKS5 (см. выше).

Полное руководство: **[server-install/WARP-PROXY.md](server-install/WARP-PROXY.md)**

## Build from source

Требуется Go 1.22+ и [mage](https://magefile.org/):

```bash
go install github.com/magefile/mage@latest

mage build         # cli + ui
mage buildCLI      # только cli
mage cross         # cross-compile linux/windows/darwin
mage mobile        # Android .aar (gomobile)
mage podman        # container image
mage docker        # container image
mage lint
mage test
mage e2e
mage clean
```

Только серверные бинарники для двух Linux-архитектур:

```bash
./server-install/build-from-source.sh   # → server-install/bin/olcrtc-linux-{amd64,arm64}
```

После этого `olcrtc-setup.sh` подхватит локальные бинарники и не пойдёт в
GitHub Releases.

## Docs

- [About](docs/about.md)
- [Quick start with containers](docs/fast.md)
- [Manual setup](docs/manual.md)
- [Settings matrix](docs/settings.md)
- [Client URI format](docs/uri.md)
- [Subscriptions](docs/sub.md)

## License

olcRTC — **WTFPL**. Этот форк — derivative work upstream-проекта и
наследует ту же лицензию. См. `LICENSE`.

<div align="center">

---

Telegram: [zarazaex](https://t.me/zarazaexe)
<br>
Email: [zarazaex@tuta.io](mailto:zarazaex@tuta.io)
<br>
Site: [zarazaex.xyz](https://zarazaex.xyz)
<br>
Made for: [olcNG](https://github.com/zarazaex69/olcng)

</div>
