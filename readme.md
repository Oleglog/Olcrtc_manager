<div align="center">

<img src="https://github.com/openlibrecommunity/material/blob/master/olcrtc.png" width="220" height="220">

# olcRTC Manager

**Admin UI, installer and release builds for olcRTC server.**

</div>

## Что это

`Olcrtc_manager` разворачивает и управляет серверной стороной **olcRTC**: TCP-туннель прячется внутри WebRTC-сессий публичных видеоконференций, а наружу трафик выходит с VPS.

Поддерживаемые carrier-провайдеры:

| Provider | Лучший транспорт | Статус |
|---|---|---|
| `jitsi` | `vp8channel` | быстрый старт, хороший default |
| `telemost` | `vp8channel` | стабилизирован в `server-v1.9.31` |
| `wbstream` | `vp8channel` / `datachannel` | для стабильной работы нужен `auth.token` |

Транспорты: `datachannel`, `vp8channel`, `seichannel`, `videochannel`. Рекомендуемый default — `jitsi` или `telemost` + `vp8channel`.

## Быстрый старт

На VPS:

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-setup.sh | sudo bash
```

После установки открой `https://<VPS-IP>:8443`. При первом входе смени пароль; сертификат самоподписанный, браузер попросит подтвердить переход.

Полное удаление (сервис, бинарники, конфигурация, ключи, инстансы, подписки):

```bash
curl -fsSL https://raw.githubusercontent.com/Oleglog/Olcrtc_manager/master/server-install/olcrtc-uninstall.sh | sudo bash
```

## Возможности

- Web Admin UI: инстансы, carrier, transport, WARP, SOCKS, подписки, обновления.
- Multi-instance systemd setup и server endpoint подписок `/sub/<slug>`.
- QR/URI генерация для Android-клиента: один QR передаёт постоянный subscription URL и optional encrypted mirror metadata, сама подписка в QR не дублируется.
- Encrypted mirrors через Yandex Disk API (AES-256-GCM, файл публичный, но без ключа не читается).
- WB Stream `auth.token` для аккаунтного/модераторского доступа и автоматизация WB через временный удалённый Chromium.
- Поддержка Telemost/WB Stream через goolom WebRTC engine.
- Stabilization fixes: VP8 CRC, peer restart rebuild, WaitForPeer, RTP reorder, keyframe keepalive, RTCP drain, TURN retention.
- Prebuilt бинарники через GitHub Releases.

## Android-клиент

Клиентский репозиторий: [Oleglog/Olcrtc_client](https://github.com/Oleglog/Olcrtc_client) (современный клиент). Прежний рабочий форк — [Oleglog/Exclave_olcrtc](https://github.com/Oleglog/Exclave_olcrtc).

> Важно: для `vp8channel` обновляй сервер и APK **вместе** — в `server-v1.9.27 / olcrtc-2.0.17` менялся wire format из-за CRC KCP-пакетов, рассинхрон их ломает.

## WB Stream: токен и комната

`server-v1.9.46+`: Admin UI может получить Room ID и WB account token через временный удалённый Chromium на VPS.

1. **Настройки → WB Stream · автоматизация браузера** — один раз установи компоненты.
2. При создании `wbstream`-инстанса нажми **Получить токен и создать комнату**.
3. В открывшемся окне вручную войди в WB и пройди CAPTCHA; автоматизация создаст quick meeting, перехватит Bearer и заполнит форму. Инстанс создаётся кнопкой **Создать инстанс**.

Chrome-профиль хранится неделю, повторный вход обычно не нужен; одна сессия — 15 минут (+один раз ещё 15), одновременно запускается только одна. Браузер можно пустить через HTTP/HTTPS/SOCKS5 proxy; noVNC доступен только из Admin UI, отдельный публичный VNC-порт не открывается. **Обновить общий токен** применяет свежий токен ко всем `wbstream`-инстансам, обновляет snapshot-записи в подписках и перезапускает активные сервисы; ручные URI не трогает.

Автоматизация опциональна (Ubuntu/Debian x86_64, ~2 ГБ RAM, 1–1.5 ГБ места). При необходимости Room ID и токен вводятся вручную — вставляется только сам токен, без префикса `Bearer`.

## Подписки

Admin UI создаёт публичные subscription URLs: `https://<domain-or-ip>:8443/sub/<slug>`.

- Сервер подписок живёт в отдельном процессе Admin UI (с `server-v1.9.48`) и приватном loopback-порту: основной инстанс `#0` можно останавливать/перенастраивать, а создание и публичные URL подписок продолжают работать. База — `/var/lib/olcrtc/subscriptions.db`.
- Кнопка **Состав** (`v1.9.49+`) показывает все подключённые инстансы и позволяет одним сохранением добавить/убрать несколько записей и ручные `olcrtc://` URI (по одному на строку).
- Удаление подписки/инстанса каскадно чистит связанные записи, JSON-mirror на Yandex Disk и instance-specific systemd unit/state (`v1.9.50`–`v1.9.52`). Ручные URI и выключенные, но оставшиеся в панели инстансы не затрагиваются.
- **QR подписки** передаёт клиенту постоянный URL + mirror metadata; подключения в QR не вкладываются, поэтому его размер не растёт с подпиской. Если основной URL недоступен и mirror настроен — клиент фолбэчится на Yandex Disk.

## Yandex Disk mirror (encrypted fallback)

Зеркало публикует зашифрованный AES-256-GCM файл профилей на Яндекс Диск, чтобы публичный `https://.../sub/<slug>` работал, когда основной сервер недоступен. Без `mirror_key` файл бесполезен; практическое ограничение — временные `*.storage.yandex.net` URL у некоторых операторов недоступны, тогда mirror не поможет.

Настройка через Admin UI — **Settings → Yandex Disk mirror (encrypted fallback)**:

1. Создай OAuth-приложение в Яндексе ([oauth.yandex.ru/client/new](https://oauth.yandex.ru/client/new)): Redirect URI `https://oauth.yandex.ru/verification_code`, доступы — **Яндекс Диск** (информация о Диске + запись в любом месте). Получи ClientID.
2. Получи OAuth-токен по `https://oauth.yandex.ru/authorize?response_type=token&client_id=<CLIENTID>` (токен действует 1 год).
3. В Admin UI включи toggle **Включить mirror**, вставь токен и base path (по умолчанию `/olcrtc/subscriptions`), нажми **Test upload** для пробы, затем **Save**. Settings пишутся в `/etc/olcrtc/env` (`0640 root:olcrtc`), `olcrtc-server` перезапускается автоматически (~10–15 c).

Если Admin UI недоступен, mirror настраивается прямо в `/etc/olcrtc/env`:

```ini
OLCRTC_SUB_MIRROR_ENABLED=true
OLCRTC_SUB_MIRROR_PROVIDER=yandex_disk
OLCRTC_SUB_MIRROR_YANDEX_OAUTH_TOKEN=<token>
OLCRTC_SUB_MIRROR_YANDEX_BASE_PATH=/olcrtc/subscriptions
```

`enabled` пиши как `true`/`false` (не `1`/`0`) — yaml.v3 парсит `1` как int и ломает unmarshal при регенерации `config.yaml`. После правки: `sudo systemctl restart olcrtc-server.service`. Если сервер запускают не через `olcrtc-launcher` (без env-генерации), mirror прописывается прямо в `config.yaml` → `subscription.mirror`; при launcher-режиме правка `config.yaml` напрямую бесполезна.

## WARP / SOCKS

- **SOCKS proxy** в инстансе — для carrier signalling, если provider банит IP VPS.
- **WARP proxy** — для клиентского туннельного egress, чтобы сайты видели WARP IP вместо VPS IP.

Документация: `server-install/README.md`, `server-install/WARP-PROXY.md`.

## Jitsi presets

Admin UI содержит список проверенных публичных Jitsi-хостов (`meet.jit.si`, `meet.ffmuc.net`, `meet.systemli.org` и др.). Кнопка проверки запрашивает серверный `/config.js` и показывает BOSH/WebSocket/Colibri подсказки. Colibri WS подтверждается только в рантайме, поэтому используй `bridge=auto` нормально и `bridge=colibri-ws` только для диагностики.

## Сборка

```bash
go install github.com/magefile/mage@latest
mage test && mage lint && mage build && mage cross
```

Установщик сервера: `sudo bash server-install/olcrtc-setup.sh --update|--status|--show-token`; удаление — `--update` → `olcrtc-uninstall.sh`.

## CI note

Основные jobs — unit/race/lint/build/cross. Real provider E2E зависит от внешних сервисов и может падать из-за carrier-side проблем.

## License

WTFPL, как upstream olcRTC.

---

<div align="center">

## ❤️ Поддержать разработку

olcRTC Manager — свободное ПО под лицензией WTFPL. Проект развивается в свободное время, без грантов и коммерческого заказчика. WebRTC-инженерия, стабилизация провайдеров (Jitsi, Telemost, WB Stream), новые транспорты и быстрые фиксы требуют реальных часов работы и тестов на реальном железе.

Если проект приносит пользу — любая поддержка помогает тратить больше времени на разработку, а не на сторонние подработки. Средства идут на хостинг для тестов, обновление зависимостей и новые релизы.

<br/>

<a href="https://pay.cloudtips.ru/p/6e5e5a94">
  <img src="https://img.shields.io/badge/♥-Поддержать_разработчика-FF4F8B?style=for-the-badge" alt="Поддержать разработчика — CloudTips" />
</a>

<br/>

<a href="https://pay.cloudtips.ru/p/6e5e5a94">pay.cloudtips.ru/p/6e5e5a94</a>

<br/>

_Спасибо, что пользуетесь olcRTC и доверяете ему свой трафик. Звезда на GitHub ⭐ и репост — тоже поддержка._

</div>
