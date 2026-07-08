<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="220" height="220">

# olcRTC Manager

**Admin UI, installer and release builds for olcRTC server.**

</div>

## Что это

`Olcrtc_manager` разворачивает и управляет серверной стороной **olcRTC**: TCP-туннель прячется внутри WebRTC-сессий публичных видеоконференций, а наружу трафик выходит с VPS.

Поддерживаемые carrier/provider:

| Provider | Лучший транспорт | Статус |
|---|---|---|
| `jitsi` | `vp8channel` | быстрый старт, хороший default |
| `telemost` | `vp8channel` | стабилизирован в `server-v1.9.31` |
| `wbstream` | `vp8channel` / `datachannel` | для стабильной работы нужен `auth.token` |

Транспорты:

```text
datachannel, vp8channel, seichannel, videochannel
```

Рекомендуемый default сейчас:

```text
provider = jitsi или telemost
transport = vp8channel
```

## Быстрый старт

На VPS:

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
```

После установки открой Admin UI:

```text
https://<VPS-IP>:8443
```

При первом входе смени пароль. Сертификат по умолчанию самоподписанный, браузер попросит подтвердить переход.

## Что есть в этом форке

- Web Admin UI для инстансов, carrier, transport, WARP, SOCKS, подписок и обновлений.
- Multi-instance systemd setup.
- Server subscription endpoint `/sub/<slug>`.
- QR/URI генерация для Android-клиента.
- QR-бандлы подписок: один QR создаёт subscription-группу на Android и сразу кладёт в неё текущие профили. Большие QR автоматически сжимаются в формат `olcrtc+gz`, чтобы камера телефона легче их сканировала. QR подписки генерируется без сетевой синхронизации mirror, поэтому открывается быстро.
- Экспериментальные encrypted mirrors для подписок через Yandex Disk API: сервер умеет публиковать зашифрованный mirror-файл, но доступность финальных `*.storage.yandex.net` URL зависит от мобильного оператора.
- WB Stream `auth.token` для аккаунтного/модераторского доступа.
- Поддержка Telemost/WB Stream через goolom WebRTC engine.
- Stabilization fixes: VP8 CRC, peer restart rebuild, WaitForPeer, RTP reorder, keyframe keepalive, RTCP drain, TURN retention.
- Prebuilt binaries через GitHub Releases.

## Android client

Клиентский репозиторий:

```text
https://github.com/Oleglog/Exclave_olcrtc
```

Актуальная совместимая пара на момент обновления README:

```text
server-v1.9.40+
olcrtc-2.0.28+
```

Для `vp8channel` важно обновлять сервер и APK вместе: в `server-v1.9.27 / olcrtc-2.0.17` был изменён wire format из-за CRC KCP-пакетов.

## WB Stream auth.token

Для `wbstream`, особенно `datachannel`, нужен WB account/moderator token.

Где взять:

1. Открой `https://stream.wb.ru` под аккаунтом.
2. Открой комнату.
3. DevTools → Network.
4. Найди запрос WB Stream API с заголовком:

```text
Authorization: Bearer <TOKEN>
```

В Admin UI вставляй только сам `<TOKEN>`, без `Bearer`.

QR для Android может включать `auth_token`, чтобы профиль импортировался полностью.

## Подписки

Admin UI умеет создавать публичные subscription URLs:

```text
https://<domain-or-ip>:8443/sub/<slug>
```

Если домен сервера подписок не доступен с мобильной сети до поднятия туннеля, используй **QR подписки** в Admin UI. Такой QR содержит не только ссылку на подписку, но и snapshot текущих `olcrtc://` профилей. Android-клиент создаёт subscription-группу, сохраняет URL и сразу добавляет все профили из QR. Дальше обновление подписки может идти через уже поднятый olcRTC/VPN туннель.

Начиная с `server-v1.9.35` есть экспериментальная поддержка encrypted mirror через Yandex Disk API. Сервер шифрует список профилей AES-256-GCM, загружает JSON на Яндекс Диск и кладёт `mirror_url` + `mirror_key` в QR-бандл. Это безопасно для публичного файла, потому что без ключа mirror не расшифровывается. Практическое ограничение: Яндекс Диск может отдавать файл через временные `*.storage.yandex.net` URL, которые у некоторых операторов недоступны. В таком случае mirror не поможет, используйте QR bootstrap и обновление через туннель, либо прямой CDN/Object Storage mirror, когда он будет доступен.

Пример конфигурации mirror, если `config.yaml` не генерируется launcher-скриптом:

```yaml
subscription:
  enabled: true
  port: 2097
  db_path: "/var/lib/olcrtc/subscriptions.db"
  public_url: "https://myolcrtc.mooo.com"

  mirror:
    enabled: true
    provider: "yandex_disk"
    yandex_oauth_token: "YANDEX_OAUTH_TOKEN"
    yandex_base_path: "/olcrtc/subscriptions"
```

Если сервер установлен через `olcrtc-launcher`, `/var/lib/olcrtc/config.yaml` может пересоздаваться при рестарте. В этом случае mirror-параметры надо добавлять в источник генерации, например `/etc/olcrtc/env` и launcher-шаблон.

## WARP / SOCKS

- **SOCKS proxy** в инстансе используется для carrier signalling, если provider блокирует IP VPS.
- **WARP proxy** используется для клиентского туннельного egress, чтобы сайты видели WARP IP вместо VPS IP.

Документация:

```text
server-install/README.md
server-install/WARP-PROXY.md
```

## Сборка

```bash
go install github.com/magefile/mage@latest
mage test
mage lint
mage build
mage cross
```

Server installer:

```bash
sudo bash server-install/olcrtc-setup.sh --update
sudo bash server-install/olcrtc-setup.sh --status
sudo bash server-install/olcrtc-setup.sh --show-token
sudo bash server-install/olcrtc-uninstall.sh
```

## CI note

Unit/race/lint/build jobs являются основными. Real provider E2E зависит от внешних сервисов и может падать из-за carrier-side проблем.

## License

WTFPL, как upstream olcRTC.


### Jitsi presets in Admin UI

Admin UI includes a curated list of tested public Jitsi hosts such as `meet.jit.si`, `jitsi.hamburg.ccc.de`, `meet.ffmuc.net`, `meet.systemli.org`, `jitsi.debian.social`, `meet.opensuse.org` and others. Use the preset buttons when creating or editing a Jitsi instance. The check button fetches `/config.js` server-side and reports BOSH/WebSocket/Colibri hints. Colibri WS is still confirmed only at runtime after Jingle advertises a bridge URL, so use `bridge=auto` for normal operation and `bridge=colibri-ws` only for diagnostics.
