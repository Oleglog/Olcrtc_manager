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
- QR-бандлы подписок: один QR создаёт subscription-группу на Android и сразу кладёт в неё текущие профили. Большие QR автоматически сжимаются в формат `olcrtc+gz`, чтобы камера телефона легче их сканировала. QR подписки синкает Yandex mirror (если включён) и вкладывает mirror metadata в бандл — чтобы Android мог фолбэчиться на Диск при таймауте основного URL.
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

## WB Stream: комната и account token

Начиная с `server-v1.9.46` Admin UI умеет получить Room ID и WB account token через временный удалённый Chromium на VPS:

1. Открой **Настройки → WB Stream · автоматизация браузера** и один раз установи компоненты.
2. При создании `wbstream`-инстанса нажми **Получить токен и создать комнату**.
3. В открывшемся окне вручную войди в WB и пройди CAPTCHA. Автоматизация сама создаст quick meeting, перехватит Bearer и заполнит поля формы. Инстанс создаётся только после нажатия **Создать инстанс**.

Chrome-профиль хранится на VPS семь дней, поэтому повторный вход обычно не нужен. Одна сессия длится 15 минут и может быть один раз продлена ещё на 15. Одновременно запускается только одна сессия. Для браузера можно задать HTTP/HTTPS/SOCKS5 proxy; noVNC доступен только через Admin UI, отдельный публичный VNC-порт не открывается.

Кнопка **Обновить общий токен** получает свежий токен через сохранённый профиль, применяет его ко всем `wbstream`-инстансам, обновляет связанные snapshot-записи в подписках и последовательно перезапускает активные сервисы. URI, введённые в подписку вручную, не меняются. Истечение JWT отображается в Admin UI; обновление запускается пользователем вручную.

Удаление компонентов стирает Chromium, профиль, cookies WB, настройки browser proxy и служебные метаданные токена. Рабочие env-файлы инстансов и подписки остаются.

Автоматизация опциональна и рассчитана на Ubuntu/Debian x86_64. Рекомендуется 2 ГБ RAM и около 1–1.5 ГБ свободного места. При необходимости Room ID и токен по-прежнему можно ввести вручную; в Admin UI вставляется только сам токен, без префикса `Bearer`.

QR для Android включает `auth_token`, чтобы профиль импортировался полностью. Такой QR является snapshot и после смены токена может устареть — это ожидаемо.

## Подписки

Admin UI умеет создавать публичные subscription URLs:

```text
https://<domain-or-ip>:8443/sub/<slug>
```

С `server-v1.9.48` backend подписок запускается внутри отдельного процесса Admin UI и использует приватный loopback-порт. Поэтому основной инстанс `#0` можно останавливать, перенастраивать или оставлять в состоянии failed — создание, редактирование и публичные URL подписок продолжат работать. SQLite-база остаётся прежней: `/var/lib/olcrtc/subscriptions.db`.

Если домен сервера подписок не доступен с мобильной сети до поднятия туннеля, используй **QR подписки** в Admin UI. Такой QR содержит не только ссылку на подписку, но и snapshot текущих `olcrtc://` профилей. Android-клиент создаёт subscription-группу, сохраняет URL и сразу добавляет все профили из QR. Дальше обновление подписки может идти через уже поднятый olcRTC/VPN туннель.

Начиная с `server-v1.9.35` есть экспериментальная поддержка encrypted mirror через Yandex Disk API. Сервер шифрует список профилей AES-256-GCM, загружает JSON на Яндекс Диск и кладёт `mirror_url` + `mirror_key` в QR-бандл. Это безопасно для публичного файла, потому что без ключа mirror не расшифровывается. Практическое ограничение: Яндекс Диск может отдавать файл через временные `*.storage.yandex.net` URL, которые у некоторых операторов недоступны. В таком случае mirror не поможет, используйте QR bootstrap и обновление через туннель, либо прямой CDN/Object Storage mirror, когда он будет доступен.

### Настройка Yandex Disk mirror через Admin UI

`server-v1.9.41+` добавляет секцию **Yandex Disk mirror (encrypted fallback)** в Admin UI. Оператор управляет mirror-конфигом из браузера, без ручного редактирования env-файлов. Настройки сохраняются в `/etc/olcrtc/env` и переживают регенерацию `config.yaml` лаунчером.

#### Шаг 1. Создать OAuth-приложение в Яндексе

1. Открой [https://oauth.yandex.ru/client/new](https://oauth.yandex.ru/client/new) под любым Яндекс-аккаунтом, на Диск которого будет публиковаться mirror.
2. Заполни поля:
   - **Название сервиса** — произвольное, например `olcRTC subscription mirror`.
   - **Ссылка на сайт сервиса** — домен твоего сервера (например `https://myolcrtc.mooo.com`), либо просто любой URL.
   - **Redirect URI** — обязательно добавь `https://oauth.yandex.ru/verification_code` (для Implicit Grant flow).
   - **Доступы** — выбери **Яндекс Диск**: `Доступ к информации о Диске` и `Запись в любом месте на Диске`.
3. Нажми **Создать приложение**. Получишь **ClientID** вида `a1b2c3d4e5f6g7h8i9j0...`.

#### Шаг 2. Получить OAuth-токен

Открой в браузере URL (подставь свой ClientID):

```text
https://oauth.yandex.ru/authorize?response_type=token&client_id=<CLIENTID>
```

Яндекс попросит разрешение на доступ к Диску. После согласия браузер вернётся на `https://oauth.yandex.ru/verification_code#access_token=<TOKEN>`. Скопируй `<TOKEN>` из URL — это и есть OAuth-токен для mirror.

Токен действителен 1 год. Когда истечёт — повтори этот шаг и обнови в Admin UI.

#### Шаг 3. Настроить в Admin UI

1. Открой Admin UI → вкладка **Settings** → раздел **Yandex Disk mirror (encrypted fallback)**.
2. Включи toggle **Включить mirror**.
3. Поле `provider` заблокировано на `yandex_disk` (других провайдеров пока нет).
4. **base_path** — путь на Яндекс Диске, куда сервер будет публиковать mirror-файлы. По умолчанию `/olcrtc/subscriptions`. Если оставишь пустым, будет использован `olcrtc/subscriptions`.
5. **OAuth token** — вставь токен из Шага 2. Поле masked: после сохранения будет показываться `••••<последние 4 символа>`. Чтобы не изменять токен при правке других полей — оставляй masked значение как есть, оно не перезапишется.
6. Нажми **Test upload** — сервер сделает пробную загрузку+удаление `.olcrtc-ping-*.json` на Яндинс Диск. Покажет latency при успехе или человекочитаемый Yandex API error при неудаче (401/403 обычно означают невалидный или истекший токен).
7. Нажми **Save**. Файл `/etc/olcrtc/env` обновляется: `OLCRTC_SUB_MIRROR_ENABLED=true`, `OLCRTC_SUB_MIRROR_PROVIDER=yandex_disk`, `OLCRTC_SUB_MIRROR_YANDEX_OAUTH_TOKEN=<token>`, `OLCRTC_SUB_MIRROR_YANDEX_BASE_PATH=<path>`. Права файла `0640 root:olcrtc`.
8. **olcrtc-server перезапускается автоматически** (с `server-v1.9.45+` через detached `systemd-run`), чтобы mirror manager подхватил новые env vars. Admin UI остаётся живым, на экране тост "olcrtc-server автоматически перезапускается". Рестарт занимает ~10-15 сек.

#### Ручное редактирование env (без Admin UI)

Если Admin UI недоступен, mirror можно настроить напрямую через `/etc/olcrtc/env`:

```bash
sudo nano /etc/olcrtc/env
```

```ini
OLCRTC_SUB_MIRROR_ENABLED=true
OLCRTC_SUB_MIRROR_PROVIDER=yandex_disk
OLCRTC_SUB_MIRROR_YANDEX_OAUTH_TOKEN=<token>
OLCRTC_SUB_MIRROR_YANDEX_BASE_PATH=/olcrtc/subscriptions
```

```bash
sudo systemctl restart olcrtc-server.service
```

`enabled` пиши как `true`/`false` (не `1`/`0`): yaml.v3 парсит `1` как int, что ломает unmarshal `SubscriptionMirror.Enabled bool` при регенерации `config.yaml` лаунчером.

#### Проверка фолбэка на Android

1. Создай подписку в Admin UI, добавь инстансы.
2. Нажми **QR подписки** и отсканируй Android-клиентом. Android импортирует бандл с `m`+`mk`.
3. Сымитируй недоступность основного сервера:

```bash
sudo systemctl stop olcrtc-server.service
# nginx отдаёт 502 на /sub/...
```

4. На Android — **обновить подписку**. Через 5-30сек клиент фолбэчится на Yandex Disk, скачивает и расшифровывает mirror, обновляет профили.
5. Верни сервер: `sudo systemctl start olcrtc-server.service`.

Если фолбэк не работает — проверь, что свежий QR содержит `m:[...]` и непустой `mk` (старые QR от `server-v1.9.38` — `1.9.43` имели `m:[]` и `mk:""`). С `server-v1.9.44+` QR снова содержит mirror metadata.

#### Пример прямой конфигурации в `config.yaml`

Если сервер запускается не через `olcrtc-launcher` (например, вручную без env-генерации), mirror можно прописать прямо в `config.yaml`:

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

Если сервер установлен через `olcrtc-launcher`, `/var/lib/olcrtc/config.yaml` пересоздаётся при рестарте из `/etc/olcrtc/env`. В этом режиме править `config.yaml` напрямую бесполезно — используй Admin UI или ручное редактирование env (см. выше).

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
