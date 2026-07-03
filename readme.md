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
server-v1.9.31+
olcrtc-2.0.22+
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

Если используется самоподписанный сертификат, в Android-клиенте включи настройку `allowInsecureOnRequest` для обновления подписок.

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
