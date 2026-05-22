# olcRTC - полная документация

> **olcRTC** (OpenLibreCommunity RTC) - инструмент обхода интернет-блокировок через паразитирование на легальных WebRTC-сервисах видеозвонков, уже находящихся в российских белых списках.
>
> Проект: [github.com/openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc)
> Лицензия: WTFPL
> Статус: **Beta**

---

## Содержание

1. [Почему olcRTC существует](#1-почему-olcrtc-существует)
2. [Идея и история создания](#2-идея-и-история-создания)
3. [Как это работает](#3-как-это-работает)
4. [Архитектура](#4-архитектура)
5. [Структура репозитория](#5-структура-репозитория)
6. [Carriers - провайдеры](#6-carriers--провайдеры)
7. [Transports - транспорты](#7-transports--транспорты)
8. [Шифрование](#8-шифрование)
9. [Мультиплексирование](#9-мультиплексирование)
10. [SOCKS5 прокси](#10-socks5-прокси)
11. [Mobile / Android](#11-mobile--android)
12. [Python PoC скрипты](#12-python-poc-скрипты)
13. [Сборка и деплой](#13-сборка-и-деплой)
14. [YAML конфигурация](#14-yaml-конфигурация)
15. [URI-формат и подписки](#15-uri-формат-и-подписки)
16. [Матрица совместимости](#16-матрица-совместимости)
17. [CI/CD](#17-cicd)
18. [Что планируется сделать - Issues](#18-что-планируется-сделать--issues)
19. [Контрибуторы](#19-контрибуторы)
20. [Частые ошибки](#20-частые-ошибки)

---

## 1. Почему olcRTC существует

В России работают ТСПУ (технические средства противодействия угрозам). В мобильных сетях  провайдеры перешли в режим **белых списков**: ТСПУ дропает все пакеты, кроме явно разрешённых IP-адресов и SNI.

Фильтрация двухуровневая:
- **L3** - по IP-адресу назначения. Не разрешён → пакет физически не уходит дальше второго хопа.
- **L7** - по SNI в TLS ClientHello. Есть в чёрном списке → RST.

Классические обходы через VPS ломаются когда VPS не попадает в белый список. Yandex Cloud, VK Cloud, Timeweb в списке - но провайдеры активно банят инстансы используемые как прокси.

**Решение olcRTC**: не пытаться попасть в белый список - использовать сервисы, которые там уже есть навсегда. Телемост, SaluteJazz и WB Stream - сервисы видеозвонков крупных российских компаний. Пока они живы, olcRTC работает. Чтобы их заблокировать - нужно заблокировать сам сервис.

Трафик идёт через WebRTC SFU этих сервисов:

```
Клиент (cnc) → SFU Яндекса/Сбера/WB → Сервер (srv, ваш VPS)
```

Для ТСПУ это выглядит как обычный видеозвонок.

---

## 2. Идея и история создания

### Хронология

**2025-04-03** - первый коммит `init repo`. Идея.

**2025-04-06** - `remove text`. Единственная правка за целый год.


**2026-04-04** - `Initialize project with base configuration and assets`. Реальный перезапуск с нуля.

**2026-04-05** - За один день появляются Python PoC:
- `telemost_poc_datachannel.py` - первое рабочее соединение через Telemost DataChannel
- `vcsend.py` - передача данных QR-кодами через видеопоток
- `flood.py` - стресс-тест соединений
- `limits.py` - обнаружен лимит Telemost DataChannel: 8KB на сообщение, всё что выше молча дропается
- `info.py` - исследование API Telemost

**2026-04-06** - QR-код двусторонняя передача (`invicible`), первые замеры: **44 Mbps** через DataChannel.

**2026-04-07** - первый Go бинарник: WebRTC туннель с ChaCha20-Poly1305 шифрованием, SOCKS5 прокси, деплой через Podman. Провайдер: только Telemost.

**2026-04-08..09** - активная Go разработка: клиент-серверная архитектура, кастомный мультиплексор с sequence numbering, имена участников из файла, graceful shutdown, DNS поддержка, Android мост.

**2026-04-10..11** - простой UI, Docker образ сервера, SaluteJazz PoC от community-контрибутора `0xcodepunk`.

**2026-04-12..14** - большой рефакторинг: golangci-lint, Jazz провайдер с protobuf-style пакетами, автогенерация Room ID для Jazz, Windows скрипты от `DeNcHiK3713`.

**2026-04-19..20** - архитектурный рефакторинг: выделение слоёв `carrier` / `transport` / `link`, WB Stream провайдер через LiveKit SDK, видеоканальный PoC на Python.

**2026-04-21..22** - `videochannel` транспорт (данные кодируются в QR-коды внутри VP8 видеопотока через ffmpeg), `vp8channel` транспорт (данные в VP8 payload), NVENC поддержка.

**2026-04-25..30** - tile кодек для videochannel с Reed-Solomon коррекцией ошибок, `vp8channel` поверх KCP для надёжной доставки, замена самописного мультиплексора на smux.

**2026-05-01..06** - `seichannel` (данные в H264 SEI NAL-юнитах), E2E тесты на реальных провайдерах, URI-формат и формат подписок, SOCKS5 аутентификация.

**2026-05-07..10** - финальная полировка: исправлен throughput bug в vp8channel (ограничение было в 32 раза ниже реального), документация, SEI конфигурация, SOCKS5 аутентификация (username/password).

**2026-05-11..14** - большой архитектурный рефакторинг `refactor/universal-carrier`:
- Разделение `internal/provider/` на `internal/engine/` (wire-level SFU протоколы) + `internal/auth/` (HTTP/API авторизация)
- Три engine: `livekit` (WB Stream), `goolom` (Telemost), `salutejazz` (Jazz)
- Три auth: `wbstream`, `telemost`, `salutejazz`
- Замена `-carrier` на `-auth`/`-engine`/`-url`/`-token`
- Публичный Go API `pkg/olcrtc` (net.Conn через Session.Dial) для встраивания в sing-box и другие
- `cmd/olcrtc-cgo` — C-shared библиотека с Ping API
- YAML конфигурация вместо CLI-флагов (`internal/config/`)
- Протокол handshake (`internal/handshake/`) с CLIENT_HELLO/SERVER_WELCOME
- Session callbacks: OnSessionOpen, OnSessionClose, OnTraffic
- Перевод документации на русский
- E2E тесты: jazz non-data транспорты помечены как expected fail

### Статья на Хабре

Проект описан в двух статьях на Хабре:
- *«Это - всё что вам надо знать о белых списках»* - технический анализ как работает фильтрация, 63k IP в белом списке из 46 млн российских, методы обхода
- *«BAREBONE2022: чтобы заблокировать этот протокол придётся запретить MAX и Yandex»* - описание идеи olcRTC, первые замеры скорости

---

## 3. Как это работает

```
Браузер/приложение
       │ (обычные TCP соединения)
       ▼
  SOCKS5 :8808          ← cnc (клиент), работает на вашей машине
       │
       │ ChaCha20-Poly1305
       │ smux поверх muxconn
       │
       ▼
  Transport (datachannel / vp8channel / seichannel / videochannel)
       │
       ▼
  Carrier (jazz / wbstream / telemost)
       │ WebRTC DataChannel или VideoTrack
       ▼
  SFU Яндекса / Сбера / WB   ← сервер в белом списке у всех провайдеров
       │
       ▼
  Transport (datachannel / vp8channel / seichannel / videochannel)
       │
       ▼
  srv (сервер), работает на вашем VPS
       │ (обычный TCP/DNS)
       ▼
  Интернет
```

Клиент (`cnc`) поднимает локальный SOCKS5. Любой браузер или приложение подключается к нему как к обычному прокси. Трафик мультиплексируется через smux, шифруется ChaCha20-Poly1305 и передаётся через выбранный транспорт поверх WebRTC SFU.

Сервер (`srv`) стоит на вашем VPS. Он подключается к той же комнате видеозвонка, получает зашифрованный поток и от своего имени делает TCP соединения к нужным адресам в интернете.

ТСПУ видит трафик к IP Яндекса/Сбера/WB с корректным TLS и SNI - ничем не отличается от обычного видеозвонка.

---

## 4. Архитектура

Проект разбит на чёткие слои. Каждый слой можно заменить независимо.

```
cmd/olcrtc/          CLI entrypoint, загрузка YAML конфига
cmd/olcrtc-cgo/      C-shared библиотека (Ping API для десктопных клиентов)
    │
pkg/olcrtc/          Публичный Go API (net.Conn через Session.Dial)
    │
internal/config/     Загрузка и маппинг YAML конфига
internal/app/session/   конфигурация, валидация, роутинг в server/client
    │               │
internal/server/    internal/client/    бизнес-логика: SOCKS5, smux
    │
internal/handshake/  Протокол handshake (CLIENT_HELLO / SERVER_WELCOME)
internal/muxconn/   io.ReadWriteCloser поверх link.Link + AEAD
    │
internal/link/direct/   pass-through, пробрасывает в transport
    │
internal/transport/     интерфейс Transport + реестр
    ├── datachannel/    WebRTC DataChannel как byte stream
    ├── vp8channel/     VP8 видео + KCP поверх него
    ├── seichannel/     H264 SEI NAL-юниты
    └── videochannel/   QR-коды / тайлы в VP8 видеофрейме через ffmpeg
    │
internal/carrier/       интерфейс Carrier + реестр
    ├── builtin/        регистрация engine+auth → carrier
    └── bytestream.go   ByteStream, VideoTrack capability
    │
internal/engine/        Wire-level SFU протоколы (URL+Token → WebRTC)
    ├── livekit/        LiveKit (WB Stream)
    ├── goolom/         Goolom (Yandex Telemost)
    └── salutejazz/     SaluteJazz (Сбер)
    │
internal/auth/          HTTP/API авторизация → Credentials для engine
    ├── wbstream/       WB Stream API (guest register, join, token)
    ├── telemost/       Yandex Telemost (connection-info)
    └── salutejazz/     SaluteJazz (create-meeting, preconnect)
    │
internal/crypto/        ChaCha20-Poly1305 AEAD
internal/names/         генератор имён участников
internal/protect/       Android VPN protect() интеграция
internal/logger/        структурированное логирование
internal/link/          интерфейс Link + реестр
internal/e2e/           E2E тесты на реальных провайдерах
```

---

## 5. Структура репозитория

### Корень

| Файл/папка | Что это |
|---|---|
| `readme.md` | Краткое описание, команды сборки, ссылки |
| `SECURITY.md` | Политика безопасности |
| `magefile.go` | Система сборки на Mage (аналог Makefile для Go). Таргеты: `build`, `buildCLI`, `cross`, `mobile`, `docker`, `podman`, `lint`, `test`, `e2e`, `deps`, `clean` |
| `Dockerfile` | Многоэтапный образ: Alpine build → Alpine runtime с непривилегированным пользователем `olcrtc` |
| `docker-compose.server.yml` | Compose для серверного режима |
| `.gitmodules` | Субмодуль `internal/transport/videochannel/gr` - кастомные кодеки QR и tile |
| `.golangci.yml` | Конфиг линтера golangci-lint v2 |
| `.github/workflows/ci.yml` | CI: тесты, покрытие, E2E, lint, сборка CLI для всех платформ, сборка Android AAR |

### `cmd/olcrtc/`

| Файл | Что делает |
|---|---|
| `main.go` | Точка входа. Загружает YAML конфиг (`olcrtc config.yaml`), настраивает логирование, подавляет шум LiveKit/pion в не-debug режиме, запускает `session.Run` или `session.Gen`. Graceful shutdown по SIGTERM/SIGINT с 5-секундным таймаутом |
| `main_test.go` | Юнит-тесты CLI: валидация конфига, режимы, edge cases |

### `cmd/olcrtc-cgo/`

| Файл | Что делает |
|---|---|
| `main.go` | C-shared библиотека. Экспортирует функцию `Ping()` для десктопных клиентов: запускает короткоживущий olcRTC клиент, ждёт SOCKS listener, делает HTTP ping через него и возвращает latency в миллисекундах. Используется для проверки связности из нативных приложений |

### `internal/app/session/`

| Файл | Что делает |
|---|---|
| `session.go` | Главная точка конфигурации. `RegisterDefaults()` регистрирует все carriers, links, transports. `Validate()` проверяет все настройки. `Run()` роутит в `server.Run` или `client.Run`. `Gen()` генерирует Room ID для auth-провайдеров с `RoomCreator` и ретраями |
| `session_test.go` | Тесты валидации конфига |

### `internal/config/`

| Файл | Что делает |
|---|---|
| `config.go` | Загрузка и парсинг YAML конфига. `Load(path)` читает файл, `Apply(dst, f)` маппит YAML поля в `session.Config`. Все структуры YAML: `File`, `Auth`, `Room`, `Crypto`, `Net`, `SOCKS`, `Engine`, `Video`, `VP8`, `SEI`, `Gen` |
| `config_test.go` | Тесты загрузки и маппинга |

### `internal/handshake/`

| Файл | Что делает |
|---|---|
| `handshake.go` | Протокол handshake на контрольном smux-стриме. Wire format: 4-byte big-endian length + JSON. Клиент шлёт `CLIENT_HELLO` (device ID, claims), сервер отвечает `SERVER_WELCOME` (session ID) или `REJECT`. После handshake контрольный стрим остаётся открытым для keepalive |
| `handshake_test.go` | Тесты |

### `internal/server/`

| Файл | Что делает |
|---|---|
| `server.go` | Серверная сторона туннеля. Подключается к комнате как второй участник звонка. Создаёт `muxconn` → `smux.Session`. Первый smux-стрим — контрольный (handshake CLIENT_HELLO / SERVER_WELCOME). Для каждого последующего стрима читает JSON `ConnectRequest` от клиента, устанавливает TCP соединение и гоняет байты. Поддерживает хуки: `OnSessionOpen`, `OnSessionClose`, `OnTraffic`. Умеет переподключаться при разрыве |
| `server_test.go` | Тесты серверной логики |

### `internal/client/`

| Файл | Что делает |
|---|---|
| `client.go` | Клиентская сторона. Поднимает SOCKS5-сервер. Для каждого входящего подключения: SOCKS5 handshake (поддержка RFC 1929 username/password auth), создаёт smux-стрим, шлёт JSON `ConnectRequest` с адресом, гоняет байты. Первый smux-стрим — контрольный (handshake). Переподключается при разрыве WebRTC сессии с retry loop |
| `client_test.go` | Тесты клиентской логики |

### `internal/muxconn/`

| Файл | Что делает |
|---|---|
| `conn.go` | Адаптер `link.Link` → `io.ReadWriteCloser`. Каждый `Write` шифрует блок ChaCha20-Poly1305 и отдаёт в link как одно сообщение. Входящие сообщения дешифруются и буферизуются; `Read` дренирует буфер в произвольных кусках (smux не знает о границах сообщений). Синхронизация через `sync.Cond` |
| `conn_test.go` | Тесты |

### `internal/link/`

| Файл | Что делает |
|---|---|
| `link.go` | Интерфейс `Link` (`Send`, `SetOnData`, `Connect`, `Close` и т.д.) + реестр |
| `link_test.go` | Тесты реестра |
| `direct/direct.go` | Единственная реализация Link. Pass-through: создаёт Transport и форвардит вызовы. Называется "direct" потому что нет промежуточного relay - данные идут прямо в transport |
| `direct/direct_test.go` | Тесты |

### `internal/transport/`

| Файл | Что делает |
|---|---|
| `transport.go` | Интерфейс `Transport` + реестр. `Features` описывает: надёжность, упорядоченность, message-oriented или stream, макс. размер payload |
| `transport_test.go` | Тесты реестра |
| `datachannel/transport.go` | Самый простой транспорт. Открывает ByteStream у carrier (DataChannel), просто форвардит байты. Лимит payload: 12KB |
| `vp8channel/transport.go` | Данные кодируются в VP8 видеофреймы. Поверх carrier строится KCP (надёжный UDP-подобный протокол) для реорганизации и ретрансмиссии. Данные батчатся по N фреймов за тик. Keepalive через keyframe |
| `vp8channel/kcp.go` | KCP сессия: conv ID = `0xC0FFEE01`, MTU 1400, окно 4096 сегментов. Length-prefix framing поверх KCP stream mode (workaround бага kcp-go с фрагментацией) |
| `vp8channel/kcpconn.go` | `io.ReadWriteCloser` адаптер для KCP |
| `seichannel/transport.go` | Данные передаются в SEI NAL-юнитах внутри H264 видеопотока. Собственный бинарный протокол с magic `OVC1`, версией, типами фреймов Data/Ack, CRC32, sequence numbers. ACK timeout, фрагментация, ретрансмиссия |
| `seichannel/h264.go` | Сборка H264 Access Unit с SEI payload. UUID для SEI: `5dc03ba8-450f-4b55-9a77-1f916c5b0739`. Статичные SPS/PPS/IDR как базовые заголовки |
| `videochannel/transport.go` | Данные визуально кодируются в кадры (QR-коды или тайлы), кадры транслируются через VP8 видеопоток. ffmpeg запускается как subprocess для кодирования/декодирования. ACK-based flow control с sequence numbers |
| `videochannel/visual.go` | Рендеринг кадров: QR-коды через `gr/qr`, тайлы через `gr/tile` с Reed-Solomon. Декодирование входящих кадров |
| `videochannel/ffmpeg.go` | ffmpeg encoder/decoder как subprocess с pipe. Поддержка VP8, H264. Hardware acceleration через NVENC. Таймаут на получение фрейма |
| `videochannel/frame.go` | Протокол фреймов videochannel |

### `internal/carrier/`

| Файл | Что делает |
|---|---|
| `carrier.go` | Интерфейс `Session` + реестр. `Capabilities` описывает что умеет carrier: ByteStream и/или VideoTrack |
| `bytestream.go` | `ByteStream` и `VideoTrack` интерфейсы |
| `carrier_test.go` | Тесты |
| `builtin/register.go` | Регистрирует jazz, telemost, wbstream, none в реестре carrier через `registerEngineAuth` (связывает auth provider с engine) и `registerDirect` (прямое подключение без auth) |
| `builtin/engine_adapter.go` | Адаптер `engine.Session` → `carrier.Session`. Связывает auth provider (Issue → Credentials) с engine (Connect с URL+Token). Поддерживает Refresh callback для engines, требующих свежие credentials при реконнекте (Goolom) |

### `internal/engine/`

| Файл | Что делает |
|---|---|
| `engine.go` | Интерфейс `Session` (Connect, Send, Close, WatchConnection, CanSend и т.д.) + `Factory` + реестр. `Config` содержит URL, Token, Extra, OnData, DNSServer, Refresh callback. `Capabilities`: ByteStream, VideoTrack |
| `livekit/engine.go` | LiveKit engine — используется WB Stream. Подключается через LiveKit SDK, публикует/подписывается на DataChannel и VideoTrack |
| `goolom/engine.go` | Goolom engine — проприетарный протокол Яндекса (Telemost). WebSocket signaling, dual pub/sub PeerConnections, DataChannel, telemetry. Использует `Refresh` callback для получения свежих credentials при реконнекте |
| `salutejazz/engine.go` | SaluteJazz engine — протокол Сбера. WebSocket + SDP signaling, pub/sub split, `_reliable` DataChannel, length-prefixed DataPacket envelope |

### `internal/auth/`

| Файл | Что делает |
|---|---|
| `auth.go` | Интерфейс `Provider` (Engine, DefaultServiceURL, Issue) + `RoomCreator` + реестр. `Credentials`: URL, Token, Extra |
| `wbstream/provider.go` | WB Stream auth: guest register → join room → token exchange. Реализует `RoomCreator`. `Engine()` → `"livekit"`, `DefaultServiceURL()` → `"https://stream.wb.ru"` |
| `telemost/provider.go` | Yandex Telemost auth: HTTP connection-info → engine credentials. `Engine()` → `"goolom"`, `DefaultServiceURL()` → `"https://telemost.yandex.ru"` |
| `salutejazz/provider.go` | SaluteJazz auth: create-meeting + preconnect flow. Реализует `RoomCreator`. `Engine()` → `"salutejazz"`. Принимает room в формате `<roomID>:<password>` |

### `internal/crypto/`

| Файл | Что делает |
|---|---|
| `chacha.go` | ChaCha20-Poly1305 AEAD. 32-байтовый ключ. Каждый Encrypt генерирует случайный nonce и prepend его к ciphertext. Decrypt проверяет AEAD тег |
| `chacha_test.go` | Тесты |

### `internal/names/`

| Файл | Что делает |
|---|---|
| `names.go` | Генератор случайных имён участников для WebRTC комнаты. Имена загружаются из `data/names` и `data/surnames` (встроены через `//go:embed`). Можно переопределить внешними файлами. `Generate()` возвращает "Имя Фамилия" с крипто-рандомом |
| `names_test.go` | Тесты |

### `internal/protect/`

| Файл | Что делает |
|---|---|
| `protect.go` | Android VPN protect() интеграция. `Protector func(fd int) bool` - если установлен, вызывается перед каждым connect чтобы сокет не роутился через VPN (нужно для корректной работы в связке с VPN-приложением на Android) |
| `protect_test.go` | Тесты |

### `internal/logger/`

| Файл | Что делает |
|---|---|
| `logger.go` | Структурированный логгер с уровнями Info/Warn/Error/Debug/Verbose. В не-debug режиме подавляет шум pion/LiveKit |
| `logger_test.go` | Тесты |

### `internal/e2e/`

| Файл | Что делает |
|---|---|
| `tunnel_test.go` | E2E тесты на реальных провайдерах. Матрица всех carrier × transport комбинаций. Запускается с флагом `-olcrtc.real-e2e`. В CI запускается на каждый push |

### `pkg/olcrtc/`

| Файл | Что делает |
|---|---|
| `olcrtc.go` | Публичный Go API для встраивания olcrtc как библиотеки. `New(ctx, Config)` создаёт `Session`. Два режима: direct engine (URL+Token) или built-in auth (Auth+RoomID). `Session.Connect()`, `Send()`, `Close()`, `WatchConnection()`, `SetEndedCallback()` |
| `conn.go` | `Session.Dial(ctx)` → `net.Conn`. Реализует `net.Conn` через `io.Pipe`: `Read` из pipe (заполняется OnData), `Write` через engine.Send. Для интеграции с sing-box и другими io.ReadWriter потребителями |
| `olcrtc_test.go` | Тесты публичного API |
| `tunnel/` | Подпакет для высокоуровневого туннелирования |

### `mobile/`

| Файл | Что делает |
|---|---|
| `mobile.go` | gomobile-совместимый API для Android/iOS. Синглтон: `Start()`, `Stop()`, `IsRunning()`. `SocketProtector` интерфейс для Android VPN bypass. `LogWriter` интерфейс для получения логов в Kotlin/Java. По умолчанию использует `vp8channel` транспорт |
| `mobile_test.go` | Тесты mobile API |

### `code/` - Python PoC скрипты

| Файл | Что делает |
|---|---|
| `telemost_poc_datachannel.py` | Базовый PoC: два гостя в одной Telemost комнате, обмен данными через DataChannel |
| `telemost_poc_videochannel.py` | Передача данных QR-кодами в видеопотоке Telemost |
| `telemost_info.py` | Сбор полной информации о Telemost конференции: участники, кодеки, ICE серверы, SDP |
| `jazz_poc_datachannel.py` | PoC DataChannel через SaluteJazz |
| `jazz_info.py` | Информация о Jazz конференции |
| `wbstream_poc_datachannel.py` | PoC DataChannel через WB Stream |
| `wbstream_poc_videochannel.py` | PoC видеоканала через WB Stream |
| `wbstream_info.py` | Информация о WB Stream комнате |
| `secretny_ddoos.py` | Утилита для стресс-тестирования (flood) |
| `init.sh` | Скрипт инициализации окружения |
| `requirements.txt` | Python зависимости: aiortc, opencv, pyzbar и др. |

### `script/`

| Файл | Что делает |
|---|---|
| `srv.sh` | Интерактивный скрипт запуска сервера через Podman. Задаёт вопросы про carrier/transport/room/key, генерирует YAML конфиг, собирает образ, запускает контейнер. Флаги: `--branch=<name>` (сменить ветку), `--no-cache` (очистить Go-кеш перед сборкой) |
| `cnc.sh` | Интерактивный скрипт запуска клиента через Podman |
| `script/docker/olcrtc-entrypoint.sh` | Docker entrypoint: читает env переменные, генерирует YAML конфиг, запускает `olcrtc` |
| `docker/olcrtc-healthcheck.sh` | Docker healthcheck: проверяет что процесс запущен |

### `data/`

| Файл | Что делает |
|---|---|
| `names` | Список русских имён для генератора имён участников |
| `surnames` | Список русских фамилий |

### `docs/`

| Файл | Что делает |
|---|---|
| `about.md` | Этот документ — полная документация проекта |
| `fast.md` | Быстрый старт через скрипты (Podman) |
| `manual.md` | Мануальная сборка: Go, mage, кросс-компиляция, все шаги |
| `settings.md` | Матрица совместимости carrier×transport, описание всех YAML полей, готовые примеры конфигов |
| `configuration.md` | Краткая справка по YAML схеме |
| `uri.md` | URI формат для клиентских приложений: `olcrtc://<Auth>?<Transport>@<RoomID>#<Key>$<MIMO>` |
| `sub.md` | Формат подписок: список серверов в одном файле с метаданными |
| `server.example.yaml` | Полный пример серверного YAML конфига |
| `client.example.yaml` | Полный пример клиентского YAML конфига |

---

## 6. Carriers - провайдеры

Carrier - это WebRTC сервис видеозвонков, через который идёт туннель. Все три в белых списках у российских провайдеров.

### SaluteJazz (`jazz`)

- Сервис видеозвонков от Сбера: `salutejazz.ru`
- Не требует регистрации для участника (только организатор)
- DataChannel работает, но Jazz **банит IP** за паттерны трафика характерные для DataChannel туннеля
- VideoTrack **не работает** для туннелирования (все non-data транспорты fail в E2E тестах)
- Поддерживает автогенерацию Room ID (`mode: gen`)
- Инициализация звонка изнутри автоматически реализована

### Yandex Telemost (`telemost`)

- Сервис видеозвонков от Яндекса: `telemost.yandex.ru`
- **Удалил DataChannel** - его больше нет в Telemost
- VideoTrack: только vp8channel стабильно работает, videochannel — best effort, seichannel не поддерживается
- Требует создания комнаты вручную через сайт (нет автогенерации)
- Двухуровневый keepalive: WebSocket ping + app-level ping

### WB Stream (`wbstream`)

- Сервис трансляций от Wildberries: `stream.wb.ru`
- **Рекомендуется** - самый стабильный
- Минимальная прослойка, почти прямой relay
- Работает с vp8channel, seichannel, videochannel
- DataChannel **не работает** в обычном guest flow: WB Stream выдаёт токены с `canPublishData=false`, DC не маршрутизирует данные (expected fail в E2E тестах)
- Room ID можно создать вручную через stream.wb.ru или через `mode: gen`
- Инициализация звонка автоматически

---

## 7. Transports - транспорты

Transport определяет как именно данные упаковываются в WebRTC поток.

### datachannel

Самый простой и быстрый. Данные идут напрямую через WebRTC DataChannel (SCTP over DTLS).

- Лимит payload: 12KB на сообщение (ограничение SFU)
- Надёжный, упорядоченный (SCTP гарантирует)
- Работает только с jazz (но Jazz банит IP за паттерны трафика)
- Telemost удалил DataChannel
- WB Stream DataChannel **не работает** в обычном guest flow — токены выдаются с `canPublishData=false`

### vp8channel

Данные упаковываются в VP8 видеофреймы. Поверх этого строится KCP - надёжный протокол с повторной передачей, работающий поверх ненадёжного канала.

- Работает с telemost и wbstream (pass в E2E тестах)
- Jazz не поддерживает VideoTrack для туннелирования (fail)
- Большой пинг из-за батчинга фреймов
- KCP параметры: MTU 1400, окно 4096, conv ID `0xC0FFEE01`
- Рекомендуется: `vp8.fps: 60`, `vp8.batch_size: 64`

### seichannel

Данные передаются в SEI (Supplemental Enhancement Information) NAL-юнитах H264 видеопотока. SEI - стандартный механизм для метаданных в H264.

- Собственный бинарный протокол: magic `OVC1` (0x4f564331), версия, тип Data/Ack, CRC32, sequence numbers
- UUID для SEI payload: `5dc03ba8-450f-4b55-9a77-1f916c5b0739`
- ACK timeout (по умолчанию 3с), фрагментация, ретрансмиссия до 4 попыток
- Работает только с wbstream (pass в E2E тестах)
- Telemost и Jazz не поддерживают (fail)
- Рекомендуется: `sei.fps: 60`, `sei.batch_size: 64`, `sei.fragment_size: 900`, `sei.ack_timeout_ms: 2000`

### videochannel

Данные визуально кодируются в видеофреймы через ffmpeg. Два визуальных кодека:

**qrcode** - данные кодируются в QR-код, QR рендерится в VP8 кадр. На приёмнике VP8 декодируется и QR сканируется. Использует библиотеку `gr/qr` (субмодуль). Настройки: разрешение, ECC уровень (`low`/`medium`/`high`/`highest`), размер фрагмента.

**tile** - тайловый кодек, только 1080x1080. Пиксели кодируют биты напрямую. Reed-Solomon коррекция ошибок. Параметры: размер тайла в пикселях (1..270), процент избыточности (0..200). Быстрее QR но нестабильнее.

Общее: ffmpeg как subprocess, поддержка NVENC, VP8 видеопоток. Самый медленный транспорт. Работает стабильно с wbstream, best effort с telemost, не работает с jazz.

---

## 8. Шифрование

Весь туннельный трафик шифруется **ChaCha20-Poly1305** (XChaCha20-Poly1305 через `golang.org/x/crypto`).

- Ключ: 32 байта, передаётся как hex строка (64 символа)
- Генерация: `openssl rand -hex 32`
- Каждое сообщение: случайный nonce (24 байта) prepend к ciphertext + AEAD тег
- Ключ должен совпадать на сервере и клиенте
- Шифрование происходит в `muxconn` - до передачи в transport/carrier

WebRTC сам по себе шифрует трафик через DTLS-SRTP, но olcRTC добавляет поверх свой слой - провайдер видит только зашифрованный blob.

---

## 9. Мультиплексирование

Через один WebRTC DataChannel / VideoTrack одновременно могут идти сотни TCP соединений браузера.

Реализация через **smux** (`github.com/xtaci/smux`) - библиотека мультиплексирования потоков, аналог HTTP/2 multiplexing.

До мая 2026 был самописный мультиплексор с sequence numbering и ручным out-of-order handling. Заменён на smux поверх KCP для vp8channel, и smux напрямую для datachannel.

`muxconn.Conn` адаптирует `link.Link` (message-oriented) в `io.ReadWriteCloser` (stream-oriented) который нужен smux. Каждый `Write` = одно зашифрованное сообщение в link.

---

## 10. SOCKS5 прокси

Клиент (`cnc`) поднимает локальный SOCKS5-сервер.

**Поддерживается:**
- SOCKS5 (RFC 1928) с командой CONNECT
- Аутентификация username/password (RFC 1929) через `socks.user`/`socks.pass` в YAML конфиге
- SOCKS5h (hostname resolution на стороне сервера) - DNS запросы идут через туннель
- Без аутентификации (по умолчанию)

**Адрес по умолчанию:** `127.0.0.1:8808`

**Использование:**
```sh
curl --socks5-hostname 127.0.0.1:8808 https://icanhazip.com
export all_proxy=socks5h://127.0.0.1:8808
export all_proxy=socks5h://user:pass@127.0.0.1:8808  # с авторизацией
```

**Сервер** (`srv`) может сам ходить через SOCKS5 прокси для исходящего трафика (`socks.proxy_addr`, `socks.proxy_port` в YAML конфиге).

---

## 11. Mobile / Android

`mobile/mobile.go` - gomobile-совместимый API.

Собирается в `olcrtc.aar` через `mage mobile` (`gomobile bind`).

Community Android клиент: [alananisimov/olcbox](https://github.com/alananisimov/olcbox)

**API:**
- `Start(carrier, roomID, keyHex string)` - запустить туннель
- `Stop()` - остановить
- `IsRunning() bool`
- `SetProtector(p SocketProtector)` - Android VPN bypass (VpnService.protect)
- `SetLogWriter(w LogWriter)` - получать логи в Kotlin/Java

По умолчанию использует `vp8channel` транспорт (наиболее совместимый).

`protect.go` - механизм Android VPN protect: перед каждым `connect()` вызывается Kotlin-коллбэк который вызывает `VpnService.protect(fd)`. Без этого трафик olcRTC может рекурсивно идти через тот же VPN.

---

## 12. Python PoC скрипты

Исторический слой - с этого всё начиналось. Используются для исследования API провайдеров и проверки гипотез.

**Telemost:**
- `telemost_poc_datachannel.py` - первый рабочий туннель, обнаружен лимит 8KB DataChannel (молча дропает больше)
- `telemost_poc_videochannel.py` - QR в видео, `vcsend.py` - передача файлов
- `telemost_info.py` - полный дамп SDP, ICE серверов, участников

**Jazz:**
- `jazz_poc_datachannel.py` - DataChannel через Jazz SFU
- `jazz_info.py` - информация о конференции

**WB Stream:**
- `wbstream_poc_datachannel.py` - DataChannel
- `wbstream_poc_videochannel.py` - видеоканал
- `wbstream_info.py` - информация

Для запуска: `pip install -r code/requirements.txt`

---

## 13. Сборка и деплой

### Зависимости

- Go 1.25+ (go.mod: `go 1.25.0`)
- Mage (`go install github.com/magefile/mage@latest`)
- ffmpeg (для videochannel транспорта)
- git с `--recurse-submodules` (субмодуль `gr` для videochannel кодеков)
- gomobile (для Android сборки)

### Mage таргеты

```sh
mage build       # текущая платформа
mage buildCLI    # только CLI бинарник
mage cross       # все платформы: linux/amd64, linux/arm64, windows/amd64,
                 #   darwin/amd64, darwin/arm64, freebsd/amd64, freebsd/arm64,
                 #   openbsd/amd64, openbsd/arm64
mage mobile      # Android AAR через gomobile
mage podman      # Docker образ через podman
mage docker      # Docker образ через docker
mage lint        # golangci-lint v2
mage test        # go test -race ./...
mage e2e         # E2E тесты (нужны реальные провайдеры)
mage deps        # go mod tidy + download
mage clean       # удалить build/
```

### Быстрый старт через скрипты (Podman)

```sh
git clone https://github.com/openlibrecommunity/olcrtc --recurse-submodules
cd olcrtc

# на сервере (VPS):
./script/srv.sh

# на клиенте:
./script/cnc.sh
```

### Мануальный запуск

```sh
# генерация ключа
openssl rand -hex 32

# создать конфиг (пример: wbstream + vp8channel)
cat > server.yaml <<EOF
mode: srv
link: direct
auth:
  provider: wbstream
room:
  id: "ROOM_ID_HERE"
crypto:
  key: "REPLACE_WITH_64_HEX"
net:
  transport: vp8channel
  dns: "1.1.1.1:53"
data: data
EOF

# запустить сервер
./olcrtc server.yaml

# конфиг клиента
cat > client.yaml <<EOF
mode: cnc
link: direct
auth:
  provider: wbstream
room:
  id: "ROOM_ID_HERE"
crypto:
  key: "REPLACE_WITH_64_HEX"
net:
  transport: vp8channel
  dns: "1.1.1.1:53"
socks:
  host: "127.0.0.1"
  port: 8808
data: data
EOF

# запустить клиент
./olcrtc client.yaml
```

### Docker

```sh
# Mount your YAML config into the container:
docker run -v ./server.yaml:/etc/olcrtc/server.yaml \
           olcrtc/server:local /etc/olcrtc/server.yaml
```

---

## 14. YAML конфигурация

olcrtc читает конфигурацию из единственного YAML-файла. CLI-флагов нет.

```sh
olcrtc config.yaml
```

Полная схема описана в [configuration.md](configuration.md). Примеры:
- [server.example.yaml](server.example.yaml)
- [client.example.yaml](client.example.yaml)

### Основные поля

| YAML путь | Описание |
|---|---|
| `mode` | `srv` - сервер, `cnc` - клиент, `gen` - генерация Room ID |
| `link` | Всегда `direct` |
| `auth.provider` | `telemost`, `jazz`, `wbstream` или `none` |
| `room.id` | Room ID |
| `crypto.key` | Ключ шифрования hex 64 символа |
| `net.transport` | `datachannel`, `vp8channel`, `seichannel`, `videochannel` |
| `net.dns` | DNS сервер, например `1.1.1.1:53` |
| `data` | Путь к директории данных |
| `debug` | Verbose логи |

### Только клиент (`mode: cnc`)

| YAML путь | Описание |
|---|---|
| `socks.host` | Адрес SOCKS5 (по умолчанию `127.0.0.1`) |
| `socks.port` | Порт SOCKS5 (по умолчанию `8808`) |
| `socks.user` | Логин (опционально) |
| `socks.pass` | Пароль (опционально) |

### Только сервер (`mode: srv`)

| YAML путь | Описание |
|---|---|
| `socks.proxy_addr` | Адрес SOCKS5 прокси для исходящего трафика |
| `socks.proxy_port` | Порт этого прокси |

### Генерация (`mode: gen`)

| YAML путь | Описание |
|---|---|
| `gen.amount` | Количество комнат для генерации |

### Транспорты

Настройки транспортов задаются в секциях `vp8`, `sei`, `video` YAML конфига. Подробнее в [configuration.md](configuration.md).

---

## 15. URI-формат и подписки

### URI формат

Соглашение для клиентских приложений. Сам `olcrtc` не парсит - используется в сторонних клиентах.

```
olcrtc://<Auth>?<Transport><payload>@<RoomID>#<Key>$<MIMO>
```

Где `<payload>` - опциональный блок `<key=value&...>` с параметрами транспорта.

**Примеры:**
```
olcrtc://wbstream?vp8channel<vp8-fps=60&vp8-batch=64>@room-01#d823fa...$RU
olcrtc://wbstream?datachannel@room-01#d823fa...$RU / DC does not work in guest flow
olcrtc://wbstream?seichannel<fps=60&batch=64&frag=900&ack-ms=2000>@room-01#d823fa...$RU
```

### Формат подписки (sub.md)

Текстовый файл со списком серверов. Хостится на сервере как plain text.

```text
#name: Zarazaex Free RU
#update: 1778011200
#refresh: 10m
#icon: 🇷🇺

olcrtc://wbstream?vp8channel<vp8-fps=60&vp8-batch=64>@room-01#key$RU / free
##name: RU-1
##ip: 1.2.3.4
##comment: basic free node
```

Клиентские приложения читают этот файл и предлагают список серверов пользователю (аналог подписок в v2ray/sing-box).

---

## 16. Матрица совместимости

| Transport | telemost | jazz | wbstream |
|---|:---:|:---:|:---:|
| datachannel | `-` | `+` | `-` |
| vp8channel | `+` | `-` | `+` |
| seichannel | `-` | `-` | `+` |
| videochannel | `~` | `-` | `+` |

- `+` работает (pass в E2E тестах)
- `-` не работает / не поддерживается (fail в E2E тестах)
- `~` best effort (может работать, но нестабильно)

**Jazz:** только datachannel проходит E2E тесты. Все non-data транспорты (vp8channel, seichannel, videochannel) помечены как expected fail — Jazz не поддерживает VideoTrack для туннелирования. Кроме того, Jazz **банит IP** за паттерны datachannel трафика.

**Telemost:** только vp8channel стабильно проходит. DataChannel удалён из Telemost. seichannel не поддерживается. videochannel — best effort.

**WBStream:** все транспорты кроме datachannel работают. DataChannel помечен как expected fail — в обычном guest flow WB Stream выдаёт токены с `canPublishData=false`, и DC не маршрутизирует данные. Для DC нужны модераторские/permission права.

**Рекомендуется:** `wbstream + vp8channel` — работает стабильно, не требует специальных прав.

**Скорость по убыванию:** `datachannel` > `vp8channel` > `seichannel` > `videochannel`



**Рекордный замер:** на связке `wbstream + datachannel` (test by `x2827262628281872727`) зафиксированы пинг **7 мс** и скорость **792.62 Mbps на вход / 749.69 Mbps на выход** — максимум, измеренный через olcRTC. Этот режим больше не работает в обычном guest flow (WB Stream выдаёт токены без `canPublishData`).

<img src="asset/speedtest.png" alt="speedtest" width="400">

---

## 17. CI/CD

`.github/workflows/ci.yml` - GitHub Actions, запускается на каждый push и PR в master.

| Job | Что делает |
|---|---|
| `test` | `go test -count=1 ./...` |
| `coverage` | `go test --cover ./...` |
| `real-e2e` | E2E матрица всех carrier×transport на реальных провайдерах (25 мин таймаут) |
| `lint` | golangci-lint v2 |
| `build-cli` | `mage cross` - кросс-компиляция для 9 платформ, артефакты в Actions |
| `build-android` | `mage mobile` - Android AAR, артефакт в Actions |

Go версия в CI: 1.25.x

---

## 18. Что планируется сделать - Issues

### Открытые

**Issue #22 - реализовать поддержку stream.wb.ru** `enhancement`

WB Stream - текущий приоритет. Основа уже реализована, остаётся:
- [ ] Симуляция XHR телеметрии (маскировка под легитимный клиент)
- [ ] Симуляция задержек и обрезание до размера реальных сообщений
- [ ] Система завершения звонка
- [ ] Авто перезапуск звонка если идёт слишком долго
- [ ] Юзать TLS стек Chrome как naiveproxy

**Issue #2 - реализовать поддержку telemost.yandex.ru** `enhancement`

- [ ] Симуляция XHR телеметрии
- [ ] Симуляция задержек
- [ ] Инициализация звонка изнутри автоматически
- [ ] Система завершения звонка
- [ ] Авто перезапуск звонка
- [ ] TLS стек Chrome

**Issue #1 - реализовать поддержку salutejazz.ru** `enhancement`

- [ ] Симуляция XHR телеметрии
- [ ] Симуляция задержек
- [ ] Система завершения звонка
- [ ] Авто перезапуск звонка
- [ ] TLS стек Chrome

### Закрытые (уже сделано)

| Issue | Что было |
|---|---|
| #44 | Very high ping - исправлен throughput bug vp8channel |
| #40 | Подключение нескольких устройств |
| #39 | Oracle VPS поддержка |
| #38 | Стандартный URI формат - реализован |
| #37 | Jitsi Meet - не планируется |
| #33 | iOS клиент - в планах |
| #27 | Инструкция - написана |
| #26 | SIP003 transport - не планируется |
| #25 | TLS/DTLS фингерпринтинг |
| #9 | Нормальный мультиплексор - реализован (smux) |
| #3 | macOS/Linux/Android/Windows поддержка - реализована |

---

## 19. Контрибуторы

| Контрибутор | Коммиты | Вклад |
|---|---|---|
| **zarazaex69** (zarazaex@tuta.io) | 417 | Автор проекта. Вся архитектура, все транспорты, carriers, crypto, mobile API, CI, документация |
| **zowue** (heminpo49@gmail.com) | 24 | Соавтор. Упомянут в оригинальной статье на Хабре |
| **TheDevisi** (devisinov@gmail.com) | 20 | UI, SOCKS5 улучшения, Windows поддержка, фиксы |
| **Qtozdec** | 10 | Фиксы, URI добавление |
| **Alexander Anisimov** / alananisimov | 6 | Android клиент [olcbox](https://github.com/alananisimov/olcbox), mobile.go фиксы, mobile provider config, cmd/olcrtc-cgo (C-shared Ping API) |
| **spkprsnts** (jectokuu@gmail.com) | 2 | Кастомный путь к ffmpeg (`-ffmpeg` flag), снижение задержки VP8 кодирования |
| **win64exe** (doost-55@yandex.ru) | 1 | Фикс srv.sh (--network host) |
| **s0me0ne-25** | 3 | Расширение датасета имён и фамилий |
| **Kot-nikot** | 3 | Фиксы |
| **HLNikNiky** / Sesdear | 2 | URI добавление, фиксы |
| **Denis Suchok** / DeNcHiK3713 | 1 | Windows Podman скрипты |
| **0xcodepunk** | 1 | SaluteJazz PoC DataChannel (issue #10) |
| **scalebb2** | 1 | - |

---

## 20. Частые ошибки

### `Connection refused` на порту SOCKS5 + `i/o timeout` при резолве

**Симптомы:**
```
curl: (7) Failed to connect to 127.0.0.1 port 8808 after 0 ms: Connection refused
```

Клиент сообщает `[+] Client started successfully!`, но SOCKS5 порт не слушает.

В логах контейнера:
```
client: failed to connect link: transport connect: stream connect: connect:
get room token: register guest: do request: Post "https://stream.wb.ru/...":
dial tcp: lookup stream.wb.ru: i/o timeout
```

**Причина:** клиент не смог зарезолвить `stream.wb.ru` через указанный DNS сервер. Соединение не установилось, SOCKS5 не поднялся.

**Решение:** указать другой DNS сервер в скрипте. Вместо дефолтного `1.1.1.1` попробовать `8.8.8.8` или `77.88.8.8`:

```sh
# при запуске cnc.sh - в поле DNS ввести:
8.8.8.8:53
# или
77.88.8.8:53
```

При ручном запуске — указать другой DNS в YAML конфиге:
```yaml
net:
  dns: "8.8.8.8:53"
```

После смены DNS в логах должна появиться строка:
```
SOCKS5 server listening on 0.0.0.0:8808
```

### `dial tcp4 : i/o timeout` на сервере (VPS блокирует исходящий трафик)

**Симптомы:**

В логах сервера появляются строки вида:
```
sid=59 dial 157.240.205.60:443 failed (10.000774052s): dial failed: dial tcp4 157.240.205.60:443: i/o timeout
sid=69 dial 194.221.250.50:443 failed (10.002092858s): dial failed: dial tcp4 194.221.250.50:443: i/o timeout
sid=81 dial 149.154.167.41:5222 failed (10.000219783s): dial failed: dial tcp4 149.154.167.41:5222: i/o timeout
```

Таймаут всегда ровно 10 секунд (это дефолтный `Timeout: 10 * time.Second` в `server.go`). Затронутые сайты открываются нормально с локального браузера через прокси, но сервер до них не добирается.

**Причина:** хостинг-провайдер или фаервол VPS блокирует исходящие соединения к определённым IP-адресам или портам. Типичные жертвы:

- `157.240.x.x` - Facebook/Meta (порты 80, 443)
- `194.221.x.x`, `149.154.x.x`, `91.108.x.x`, `91.105.x.x` - Telegram (порты 80, 443, 5222)

Российские VPS-провайдеры блокируют исходящий трафик к этим сайтам на уровне фаервола хостинга - независимо от настроек iptables на самой машине.

**Диагностика:** выполнить прямо на сервере:
```sh
curl -v --connect-timeout 5 https://157.240.205.60
curl -v --connect-timeout 5 https://149.154.167.41
```
Если таймаут - проблема на уровне хостинга.

**Решение:**

1. Сменить хостинг-провайдера или локацию на того, кто не блокирует исходящий трафик.
2. Использовать на сервере исходящий SOCKS5 прокси через YAML конфиг:
```yaml
socks:
  proxy_addr: "1.2.3.4"
  proxy_port: 1080
```

Это ошибка не на стороне olcRTC - он корректно логирует ошибки и продолжает работу. Соединения к незаблокированным адресам проходят без проблем. Проблема на стороне хостинга или фаервола.

---

## Контакты

- Telegram канал: [@openlibrecommunity](https://t.me/openlibrecommunity) - бесплатный прокси в закрепе, обновления, анонсы
- Telegram автора: [@zarazaexe](https://t.me/zarazaexe)
- Email: [zarazaex@tuta.io](mailto:zarazaex@tuta.io)
- GitHub: [openlibrecommunity](https://github.com/openlibrecommunity)
- Android клиент: [alananisimov/olcbox](https://github.com/alananisimov/olcbox)
- Белые списки (еженедельное обновление): [openlibrecommunity/twl](https://github.com/openlibrecommunity/twl)
