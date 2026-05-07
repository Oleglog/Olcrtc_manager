# olcrtc-admin — Web UI для управления сервером

## Техническое задание (ТЗ)

---

## 1. Цель

**Полностью заменить** консольное меню `olcrtc-setup.sh` веб-сервисом
`olcrtc-admin`. Скрипт `olcrtc-setup.sh` превращается в **чистый установщик**
(install & forget) — всё управление переносится в Web UI.

### Что делает `olcrtc-admin`

- Веб-панель для **всего**: инстансы, подписки, конфигурация, ключи, логи
- Работа по HTTPS без обязательного домена (self-signed для IP)
- Автоматическая привязка домена + Let's Encrypt, если домен есть
- Автоподбор свободного порта
- Раздача подписок клиентам (`/sub/{slug}`) — порт 2096 не нужен снаружи
- Настройка SOCKS5-прокси для carrier signalling и WARP-прокси для tunnel-трафика
- Настройка транспортов `datachannel`, `vp8channel`, `seichannel` с transport-specific параметрами
- Включение/выключение debug-логирования и просмотр `journalctl` логов

### Что остаётся в `olcrtc-setup.sh`

- Установка бинарников (`olcrtc` + `olcrtc-admin`)
- Создание systemd-юнитов
- Первичная генерация ключей, room ID, env-файлов
- Первичная настройка (carrier, transport) через интерактивные вопросы
- Вывод URL + токен для входа в Web UI
- **Всё.** Никакого management-меню.

---

## 2. Архитектура

```
┌─────────────────────────────────────────────────────────────┐
│  VPS                                                        │
│                                                             │
│  olcrtc (основной бинарник)                                 │
│    ├── tunnel worker(s)           порт: динамический        │
│    └── subscription HTTP API      порт: 2096 (localhost)    │
│                                                             │
│  olcrtc-admin (новый бинарник)    порт: авто (напр. 8443)   │
│    ├── /                → SPA (dashboard)                   │
│    ├── /api/instances   → управление инстансами             │
│    ├── /api/subs/*      → proxy → localhost:2096            │
│    ├── /api/system      → systemctl, логи                   │
│    └── TLS termination  → self-signed / Let's Encrypt       │
│                                                             │
│  systemd                                                    │
│    ├── olcrtc-server.service      (основной, user: olcrtc)  │
│    ├── olcrtc-server@N.service    (доп. инстансы)           │
│    └── olcrtc-admin.service       (admin, user: root)       │
└─────────────────────────────────────────────────────────────┘
```

### Почему отдельный бинарник

| Критерий | Встроенный в olcrtc | Отдельный olcrtc-admin |
|---|---|---|
| Привилегии | olcrtc работает от непривил. user | root — прямой доступ к systemctl, env, ключам |
| Изоляция | Падение UI = падение туннеля | Независимые процессы |
| Обновления | Нужно рестартить туннель | Обновление UI без даунтайма туннеля |
| Безопасность | Расширяет attack surface основного бинарника | Отдельный процесс, отдельный порт |

---

## 3. Автоматический TLS (без домена / с доменом)

### 3а. Без домена — self-signed сертификат

При первом запуске `olcrtc-admin`:

1. Генерирует CA (Certificate Authority): `ca.crt` + `ca.key`
2. Выписывает серверный сертификат с SAN:
   - `IP:<внешний IP VPS>`
   - `IP:127.0.0.1`
   - `DNS:localhost`
3. Сохраняет в `/var/lib/olcrtc/admin-tls/`:
   ```
   ca.crt          ← CA сертификат (пользователь может импортировать в браузер)
   server.crt      ← серверный сертификат
   server.key      ← приватный ключ
   ```
4. При повторных запусках — переиспользует существующие сертификаты
5. При смене IP — автоматически перегенерирует серверный сертификат

**Реализация**: `crypto/x509` + `crypto/ecdsa` (P-256), стандартная библиотека Go.

**UX**: Браузер покажет предупреждение "небезопасный сертификат" — это ожидаемо
для self-signed. В выводе установщика показываем:
```
  Admin UI:   https://42.212.52.67:8443
  ⚠  Сертификат самоподписанный — браузер покажет предупреждение.
      Нажмите "Дополнительно" → "Перейти на сайт".
```

### 3б. С доменом — автоматический Let's Encrypt

Если пользователь указал домен при установке (или позже через UI):

1. `olcrtc-admin` дополнительно слушает порт 80 (HTTP-01 challenge) ИЛИ
   использует TLS-ALPN-01 challenge (порт 443, не требует порт 80)
2. Автоматически получает и обновляет сертификат Let's Encrypt
3. Сохраняет в `/var/lib/olcrtc/admin-tls/acme/`

**Реализация**: `golang.org/x/crypto/acme/autocert`

**Выбор challenge:**
- Если порт 80 свободен → HTTP-01 (проще)
- Если порт 80 занят (nginx/3x-ui) → TLS-ALPN-01 на порту 443
- Если и 80, и 443 заняты → пользователь привязывает вручную через nginx
  (документация уже есть в README)

### 3в. Переключение режимов

```
olcrtc-admin                          # self-signed, автопорт
olcrtc-admin -domain sub.example.com  # Let's Encrypt + авто
olcrtc-admin -domain sub.example.com -acme-port 80   # HTTP-01
```

При добавлении домена через Web UI — сервис перезапускается с новым флагом
(env переменная `OLCRTC_ADMIN_DOMAIN`).

---

## 4. Автоподбор порта

### Алгоритм

1. Определить список предпочтительных портов: `[8443, 9443, 8080, 3000, 4443]`
2. Для каждого порта проверить:
   - `net.Listen("tcp", ":PORT")` → если успешно, порт свободен
3. Выбрать первый свободный
4. Сохранить в `/etc/olcrtc/admin.env`:
   ```
   OLCRTC_ADMIN_PORT=8443
   OLCRTC_ADMIN_DOMAIN=
   OLCRTC_ADMIN_TOKEN=<random 32-byte hex>
   ```
5. При повторных запусках — использовать сохранённый порт

### Переопределение

```
olcrtc-admin -port 9999    # принудительно указать порт
```

---

## 5. Аутентификация и безопасность

### Bearer Token

- При установке генерируется случайный токен (32 байта, hex)
- Сохраняется в `/etc/olcrtc/admin.env`
- Все API-запросы требуют `Authorization: Bearer <token>`
- SPA хранит токен в `localStorage` после первого ввода
- Вход: страница `/login` с полем для токена

### Защита от перебора

- Rate limiting: max 5 неудачных попыток / минуту, затем блокировка IP на 5 минут
- Все неудачные попытки логируются

### Дополнительно (v2)

- Опциональная HTTP Basic Auth
- IP whitelist
- 2FA (TOTP)

---

## 6. API спецификация

Базовый URL: `https://<host>:<port>/api`

Все ответы — JSON. Все мутирующие запросы требуют `Content-Type: application/json`.

### 6.1. Инстансы

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/instances` | Список всех инстансов с текущим статусом |
| `GET` | `/api/instances/{n}` | Детали инстанса #n |
| `GET` | `/api/instances/{n}/uri` | Полная olcrtc:// URI инстанса |
| `GET` | `/api/instances/{n}/qr` | QR-код (PNG) для olcrtc:// URI |
| `POST` | `/api/instances/{n}/restart` | Перезапустить инстанс (systemctl restart) |
| `POST` | `/api/instances/{n}/stop` | Остановить инстанс |
| `POST` | `/api/instances/{n}/start` | Запустить инстанс |
| `PUT` | `/api/instances/{n}/config` | Обновить конфигурацию (carrier, transport, name, dns, proxy, debug, etc.) |
| `POST` | `/api/instances/{n}/rotate-key` | Сгенерировать новый ключ шифрования |
| `POST` | `/api/instances/{n}/rotate-room` | Пересоздать room ID |
| `DELETE` | `/api/instances/{n}` | Удалить доп. инстанс (нельзя удалить #0) |
| `POST` | `/api/instances` | Создать новый доп. инстанс |

#### GET /api/instances — пример ответа

```json
[
  {
    "id": 0,
    "label": "Основной",
    "carrier": "wbstream",
    "transport": "datachannel",
    "room_id": "019df923-2f78-7b76-ad82-acc8c59697ad",
    "name": "wbstream_olcrtc",
    "status": "running",
    "uptime": "2d 5h 12m",
    "uri": "olcrtc://wbstream@room/019df923-...?key=...#wbstream_olcrtc"
  },
  {
    "id": 1,
    "label": "Доп. #1",
    "carrier": "wbstream",
    "transport": "vp8channel",
    "room_id": "019df925-f80d-728f-8fd9-eaa83d939b79",
    "name": "wbstream_olcrtc_2",
    "status": "running",
    "uptime": "2d 5h 10m",
    "uri": "olcrtc://wbstream@room/019df925-...?key=...#wbstream_olcrtc_2"
  }
]
```

#### PUT /api/instances/{n}/config — тело запроса

```json
{
  "carrier": "wbstream",
  "transport": "vp8channel",
  "name": "my_server",
  "vp8_fps": 30,
  "vp8_batch": 2,
  "sei_fps": 20,
  "sei_batch": 1,
  "sei_frag": 900,
  "sei_ack_ms": 3000,
  "socks_proxy": "",
  "warp_proxy": "",
  "debug": false,
  "dns": "1.1.1.1:53"
}
```

После изменения конфигурации сервис автоматически перезапускается.

### 6.2. Подписки (proxy к subscription API)

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/subs` | Список подписок (proxy → 2096) |
| `POST` | `/api/subs` | Создать подписку |
| `GET` | `/api/subs/{slug}` | Подписка по slug |
| `DELETE` | `/api/subs/{slug}` | Удалить подписку |
| `DELETE` | `/api/subs/{slug}?detach=true` | Открепить инстансы |
| `GET` | `/api/subs/{slug}/instances` | Инстансы подписки |
| `POST` | `/api/subs/{slug}/instances` | Добавить инстанс в подписку |
| `DELETE` | `/api/subs/{slug}/instances/{id}` | Убрать инстанс из подписки |
| `GET` | `/api/subs/export` | Экспорт (JSON) |
| `POST` | `/api/subs/import` | Импорт (JSON) |

Admin-сервис проксирует эти запросы к `http://127.0.0.1:<sub_port>/api/subscriptions/...`,
транслируя пути.

### 6.3. Системные

| Метод | Путь | Описание |
|---|---|---|
| `GET` | `/api/system/status` | Общая информация: версия, uptime VPS, IP, порты |
| `GET` | `/api/system/logs/{service}?lines=100` | Последние N строк journalctl |
| `POST` | `/api/system/domain` | Привязать домен (перегенерирует TLS) |
| `DELETE` | `/api/system/domain` | Убрать домен (вернуться к self-signed) |
| `GET` | `/api/system/ports` | Список занятых портов |

#### GET /api/system/status — пример ответа

```json
{
  "version": "0.4.0",
  "admin_version": "0.1.0",
  "hostname": "vps-123",
  "public_ip": "42.212.52.67",
  "os": "Ubuntu 24.04",
  "uptime": "15d 3h",
  "admin_port": 8443,
  "sub_port": 2096,
  "sub_enabled": true,
  "socks_proxy": "",
  "warp_proxy": "",
  "domain": "",
  "tls_mode": "self-signed",
  "tls_expires": "2027-05-05T00:00:00Z",
  "instances_total": 2,
  "instances_running": 2
}
```

### 6.4. Аутентификация

| Метод | Путь | Описание |
|---|---|---|
| `POST` | `/api/auth/login` | `{"token": "..."}` → `{"ok": true}` или 401 |
| `POST` | `/api/auth/change-token` | Сменить токен |

---

## 7. Web UI — страницы и макеты

### Технологии

- **Фреймворк**: нет (vanilla JS) — для простоты сборки и встраивания
- **Стили**: Tailwind CSS (через CDN)
- **Иконки**: Lucide (через CDN)
- **QR-коды**: JS-библиотека `qrcode.js` (CDN)
- **Встраивание**: Go `embed.FS` — вся статика внутри бинарника

> Альтернатива: React + Vite (если нужна более сложная интерактивность в будущем).
> В v1 vanilla JS достаточно.

### 7.1. Страница входа `/login`

```
┌──────────────────────────────────────────┐
│            olcRTC Admin                  │
│                                          │
│   ┌──────────────────────────────────┐   │
│   │  Токен доступа: [____________]   │   │
│   │           [  Войти  ]            │   │
│   └──────────────────────────────────┘   │
│                                          │
│   Токен показан при установке сервера    │
└──────────────────────────────────────────┘
```

### 7.2. Dashboard `/`

```
┌─ olcRTC Admin ──────────────────────────── [⚙ Settings] [↪ Logout] ─┐
│                                                                       │
│  ┌─ Система ───────────────────────────────────────────────────────┐  │
│  │  IP: 42.212.52.67    OS: Ubuntu 24.04    Uptime: 15d 3h        │  │
│  │  TLS: self-signed    Домен: —            Подписки: вкл (2096)  │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                       │
│  ┌─ Инстансы ──────────────────────────────────────────────────────┐  │
│  │                                                                 │  │
│  │  ● Основной (#0)              wbstream / datachannel            │  │
│  │    Room: 019df923-2f78-...    Uptime: 2d 5h                     │  │
│  │    [📋 URI] [📱 QR] [🔄 Restart] [⚙ Config]                    │  │
│  │                                                                 │  │
│  │  ● Доп. #1                    wbstream / vp8channel             │  │
│  │    Room: 019df925-f80d-...    Uptime: 2d 5h                     │  │
│  │    [📋 URI] [📱 QR] [🔄 Restart] [⚙ Config] [🗑 Delete]        │  │
│  │                                                                 │  │
│  │  [+ Создать инстанс]                                           │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│                                                                       │
│  ┌─ Подписки ──────────────────────────────────────────────────────┐  │
│  │                                                                 │  │
│  │  📦 Ijigvo [zjRHbM]                         2 инстанса          │  │
│  │    URL: https://42.212.52.67:8443/sub/zjRHbM                    │  │
│  │    [👁 Просмотр] [+ Добавить инстанс] [🗑 Удалить]              │  │
│  │                                                                 │  │
│  │  [+ Создать подписку]                                           │  │
│  └─────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────────────────────────────────────────────┘
```

### 7.3. Модальное окно: QR-код

```
┌─ QR-код ──────────────────────────────┐
│                                        │
│   olcrtc://wbstream@room/019df9...     │
│                                        │
│       ┌───────────────────┐            │
│       │   █▀▀▀█ ▀█▀ █▀▀  │            │
│       │   █ ▀ █  █  █▀▀  │            │
│       │   ▀▀▀▀▀ ▀▀▀ ▀▀▀  │            │
│       └───────────────────┘            │
│                                        │
│  [📋 Копировать URI]    [✕ Закрыть]    │
└────────────────────────────────────────┘
```

### 7.4. Модальное окно: Настройка инстанса

```
┌─ Настройка инстанса #0 ──────────────┐
│                                        │
│  Carrier:    [wbstream     ▾]          │
│  Transport:  [datachannel  ▾]          │
│  Имя:        [wbstream_olcrtc   ]      │
│  DNS:        [1.1.1.1:53        ]      │
│  SOCKS:      [                  ]      │
│  WARP:       [                  ]      │
│  Debug:      [ ] включить              │
│                                        │
│  ── VP8 (если transport=vp8channel) ── │
│  FPS:        [30   ]                   │
│  Batch:      [2    ]                   │
│                                        │
│  ── SEI (если transport=seichannel) ── │
│  FPS:        [20   ]                   │
│  Batch:      [1    ]                   │
│  Fragment:   [900  ]                   │
│  ACK ms:     [3000 ]                   │
│                                        │
│  ── Ключи ──────────────────────────── │
│  [🔑 Пересоздать ключ + Room ID]      │
│  [🔄 Пересоздать Room ID]             │
│                                        │
│  [💾 Сохранить]          [✕ Отмена]    │
└────────────────────────────────────────┘
```

### 7.5. Модальное окно: Добавить инстанс в подписку

```
┌─ Добавить инстанс в подписку ─────────┐
│                                        │
│  Подписка: Ijigvo [zjRHbM]             │
│                                        │
│  Выберите инстанс:                     │
│  ○ Основной (#0) — wbstream            │
│  ○ Доп. #1 — wbstream vp8channel       │
│  ○ Ввести URI вручную                  │
│                                        │
│  [+ Добавить]            [✕ Отмена]    │
└────────────────────────────────────────┘
```

### 7.6. Настройки (Settings)

```
┌─ Настройки ───────────────────────────┐
│                                        │
│  ── Домен ─────────────────────────    │
│  Текущий: (не привязан)                │
│  Домен: [sub.example.com        ]      │
│  [🔗 Привязать домен]                  │
│                                        │
│  ── Порт ──────────────────────────    │
│  Admin UI: 8443                        │
│  Подписки: 2096                        │
│                                        │
│  ── Безопасность ──────────────────    │
│  [🔑 Сменить токен доступа]           │
│                                        │
│  ── Логи ──────────────────────────    │
│  [📜 olcrtc-server]                    │
│  [📜 olcrtc-admin]                     │
│                                        │
│  [✕ Закрыть]                           │
└────────────────────────────────────────┘
```

---

## 8. Привязка домена через UI

### Сценарий: пользователь нажимает "Привязать домен"

1. UI показывает поле ввода домена
2. `POST /api/system/domain` `{"domain": "sub.example.com"}`
3. Бэкенд:
   a. Проверяет DNS: `net.LookupHost("sub.example.com")` → IP совпадает с VPS
   b. Если нет — ошибка: "DNS не настроен, добавьте A-запись"
   c. Определяет стратегию получения сертификата:
      - Порт 443 свободен → TLS-ALPN-01 (слушает на 443)
      - Порт 80 свободен → HTTP-01 (слушает на 80)
      - Оба заняты → ошибка + инструкция ручной привязки через nginx
   d. Получает сертификат Let's Encrypt
   e. Сохраняет `OLCRTC_ADMIN_DOMAIN=sub.example.com` в `/etc/olcrtc/admin.env`
   f. Перезапускает HTTPS listener с новым сертификатом
4. UI показывает: "Домен привязан ✓ — https://sub.example.com:8443"

### Сценарий: сервер с 3x-ui (порты 80 и 443 заняты)

1. Бэкенд обнаруживает, что оба порта заняты
2. Возвращает ошибку с инструкцией:
   ```json
   {
     "error": "ports_busy",
     "message": "Порты 80 и 443 заняты. Настройте reverse-proxy вручную.",
     "hint": "Обнаружен nginx с SNI multiplexer (stream). Инструкция: ...",
     "docs_url": "https://github.com/Oleglog/Olcrtc_manager/blob/master/server-install/README.md#3b-nginx-with-sni-multiplexer"
   }
   ```
3. Или (v2): автоматически определяет nginx stream config и добавляет
   upstream + SNI запись сам

---

## 9. Установка и первый запуск

### Новый `olcrtc-setup.sh` (installer-only)

Скрипт полностью переписывается. Консольное меню управления **удаляется**.
Остаётся только линейный процесс установки:

```
$ curl -fsSL .../olcrtc-setup.sh | sudo bash

  ╔═══════════════════════════════════════════╗
  ║  olcRTC Installer v1.0.0                  ║
  ╚═══════════════════════════════════════════╝

  [1/7] Проверка системы...              ✓
  [2/7] Скачивание olcrtc binary...      ✓
  [3/7] Скачивание olcrtc-admin...       ✓
  [4/7] Настройка:
        Carrier [wbstream]:              wbstream
        Transport [datachannel]:         datachannel
        Имя инстанса [wbstream_olcrtc]:  my_server
        Подписки [Y/n]:                  y
  [5/7] Генерация ключей и Room ID...    ✓
  [6/7] Создание systemd-юнитов...       ✓
  [7/7] Запуск сервисов...               ✓

  ═══════════════════════════════════════════
  Установка завершена!
  ═══════════════════════════════════════════

  Admin UI:  https://42.212.52.67:8443
  Токен:     a3f8c1d9e7b2f4a6...

  ⚠  Сертификат самоподписанный.
     В браузере нажмите "Дополнительно" → "Перейти".

  Дальнейшее управление — через Web UI.
  ═══════════════════════════════════════════
```

### Что удаляется из `olcrtc-setup.sh`

| Было (консольное меню) | Куда переносится |
|---|---|
| Показать URI / QR-код | Web UI → Dashboard |
| Сменить carrier / transport | Web UI → Настройки инстанса |
| Пересоздать Room ID | Web UI → Настройки инстанса |
| Пересоздать ключ | Web UI → Настройки инстанса |
| Настроить SOCKS5-прокси | Web UI → Настройки инстанса |
| Настроить WARP-прокси | Web UI → Настройки инстанса |
| Включить / выключить debug | Web UI → Настройки инстанса |
| Переименовать соединение | Web UI → Настройки инстанса |
| Управление подписками (8 пунктов) | Web UI → Подписки |
| Создать / удалить доп. инстанс | Web UI → Инстансы |
| Просмотр логов | Web UI → Настройки → Логи |
| Рестарт сервиса | Web UI → Dashboard |
| Переключение инстансов | Web UI → Dashboard |
| Обновить бинарник olcRTC | Web UI → Settings / CLI `--update` |
| Полное удаление | CLI `--uninstall` |

### Что остаётся в `olcrtc-setup.sh`

- Проверка ОС, архитектуры, зависимостей
- Скачивание и установка бинарников с GitHub Releases
- Интерактивный выбор carrier / transport (только при первой установке)
- Генерация ключа, room ID, env-файла
- Создание systemd-юнитов (`olcrtc-server.service`, `olcrtc-admin.service`)
- Автоподбор порта для admin UI
- Генерация admin-токена
- Запуск обоих сервисов
- Вывод URL + токена
- Повторный запуск без флагов: только статус + URL, **без интерактивного меню**

### Повторный запуск `olcrtc-setup.sh`

При повторном запуске на уже установленной системе:

```
$ sudo bash olcrtc-setup.sh

  olcRTC уже установлен.

  Admin UI:  https://42.212.52.67:8443
  Статус:    olcrtc-server ● running, olcrtc-admin ● running

  Для управления откройте Admin UI в браузере.

  Дополнительные действия:
    --update          Обновить бинарники до последней версии
    --regenerate      Пересоздать Room ID
    --regenerate-key  Пересоздать ключ + Room ID
    --uninstall       Полное удаление
    --show-token      Показать токен для входа в Admin UI
    --status          Показать статус без интерактивного меню
```

То есть повторный запуск **без флагов** — просто показывает статус и URL.
Никакого интерактивного меню.

### Размер скрипта

Текущий `olcrtc-setup.sh` — ~2700 строк (из них ~1800 — management menu).
Новый installer — ожидаемо **~400–600 строк**: чистая установка без UI-логики.

### Файлы

| Путь | Описание |
|---|---|
| `/usr/local/bin/olcrtc-admin` | Бинарник |
| `/etc/olcrtc/admin.env` | `PORT`, `DOMAIN`, `TOKEN` |
| `/var/lib/olcrtc/admin-tls/` | Сертификаты (self-signed или ACME) |
| `/etc/systemd/system/olcrtc-admin.service` | Systemd unit |

### Миграция существующих установок

При обновлении с текущего menu-based `olcrtc-setup.sh` установщик должен:

1. Найти существующие env-файлы:
   - `/etc/olcrtc/env`
   - `/etc/olcrtc/{n}/env`
2. Сохранить все текущие параметры:
   - `OLCRTC_CARRIER`
   - `OLCRTC_TRANSPORT`
   - `OLCRTC_ROOM_ID`
   - `OLCRTC_KEY`
   - `OLCRTC_DNS`
   - `OLCRTC_SOCKS_PROXY`
   - `OLCRTC_WARP_PROXY`
   - `OLCRTC_NAME`
   - `OLCRTC_DEBUG`
   - `OLCRTC_VP8_FPS`
   - `OLCRTC_VP8_BATCH`
   - `OLCRTC_SEI_FPS`
   - `OLCRTC_SEI_BATCH`
   - `OLCRTC_SEI_FRAG`
   - `OLCRTC_SEI_ACK`
   - `OLCRTC_SUB_ENABLED`
   - `OLCRTC_SUB_PORT`
3. Установить `olcrtc-admin` и создать `/etc/olcrtc/admin.env`
4. Не пересоздавать ключи и room ID без явного флага
5. Не останавливать рабочие туннели дольше, чем нужно для `systemctl daemon-reload`

### Systemd unit

```ini
[Unit]
Description=olcRTC Admin Web UI
After=network-online.target olcrtc-server.service
Wants=network-online.target

[Service]
Type=simple
EnvironmentFile=/etc/olcrtc/admin.env
ExecStart=/usr/local/bin/olcrtc-admin \
    -port ${OLCRTC_ADMIN_PORT} \
    -token ${OLCRTC_ADMIN_TOKEN} \
    -domain ${OLCRTC_ADMIN_DOMAIN:-} \
    -sub-port ${OLCRTC_SUB_PORT:-2096} \
    -tls-dir /var/lib/olcrtc/admin-tls
Restart=on-failure
RestartSec=5

# Запуск от root для доступа к systemctl и env-файлам
# (можно ограничить через capabilities в будущем)

[Install]
WantedBy=multi-user.target
```

---

## 10. Флаги командной строки olcrtc-admin

| Флаг | Default | Описание |
|---|---|---|
| `-port` | auto | Порт HTTPS |
| `-token` | (required) | Bearer token для API |
| `-domain` | "" | Домен для Let's Encrypt (пусто = self-signed) |
| `-sub-port` | 2096 | Порт subscription API для проксирования |
| `-tls-dir` | `/var/lib/olcrtc/admin-tls` | Каталог для сертификатов |
| `-acme-email` | "" | Email для Let's Encrypt (опционально) |
| `-config-dir` | `/etc/olcrtc` | Каталог с env-файлами инстансов |
| `-show-token` | false | Показать токен из `admin.env` и выйти |

---

## 11. Структура проекта (Go)

```
cmd/
  olcrtc-admin/
    main.go                 # точка входа, флаги, запуск сервера

internal/
  admin/
    server.go               # HTTP server, роутинг, middleware (auth, CORS, logging)
    tls.go                  # генерация self-signed, autocert, определение стратегии
    ports.go                # сканирование свободных портов
    api_instances.go        # CRUD инстансов, чтение env, systemctl
    api_subscriptions.go    # reverse proxy к subscription API
    api_system.go           # статус, логи, домен
    api_auth.go             # login, смена токена
    env.go                  # чтение/запись /etc/olcrtc/*/env
    systemctl.go            # обёртка вокруг systemctl (start/stop/restart/status)
    static/                 # embedded SPA
      index.html
      app.js
      style.css
      favicon.ico
    embed.go                # //go:embed static/*
```

---

## 12. Сборка

### Из исходников

```bash
# Linux (на VPS)
CGO_ENABLED=0 go build -o olcrtc-admin ./cmd/olcrtc-admin

# Cross-compilation (с Windows)
set GOOS=linux
set GOARCH=amd64
go build -o olcrtc-admin ./cmd/olcrtc-admin
```

### GitHub Release

В релиз добавляются оба бинарника:
- `olcrtc-linux-amd64` (основной)
- `olcrtc-admin-linux-amd64` (admin UI)

`olcrtc-setup.sh` скачивает оба при установке.

---

## 13. Этапы реализации

### v0.1 — MVP (admin-сервер + базовый UI)

- [ ] Go сервер с self-signed TLS + автопорт
- [ ] Аутентификация по токену
- [ ] API: `/api/instances` (список + статус)
- [ ] API: `/api/instances/{n}/uri` (URI + QR)
- [ ] API: `/api/instances/{n}/restart`, `/stop`, `/start`
- [ ] API: `/api/system/logs/{service}?lines=100`
- [ ] SPA: dashboard с инстансами, URI, QR-код, статус
- [ ] Systemd unit для olcrtc-admin

### v0.2 — Подписки + раздача через admin

- [ ] API: proxy `/api/subs/*` → subscription API
- [ ] Публичный эндпоинт: `/sub/{slug}` через admin (порт 2096 закрыть)
- [ ] SPA: управление подписками (CRUD)
- [ ] SPA: добавление инстансов в подписки (выбор из списка)

### v0.3 — Полное управление инстансами

- [ ] API: изменение конфигурации (carrier, transport, name, dns, SOCKS5, WARP, debug)
- [ ] API/UI: VP8 параметры (`vp8_fps`, `vp8_batch`)
- [ ] API/UI: SEI параметры (`sei_fps`, `sei_batch`, `sei_frag`, `sei_ack_ms`)
- [ ] API: ротация ключей и room ID
- [ ] API: создание / удаление доп. инстансов
- [ ] SPA: формы настройки, создание/удаление инстансов

### v0.4 — Домены + Let's Encrypt

- [ ] Let's Encrypt через autocert
- [ ] UI: привязка домена одной кнопкой
- [ ] Автодетект занятых портов (80/443)
- [ ] Автодетект nginx + SNI (v2)

### v0.5 — Переписать olcrtc-setup.sh

- [ ] Удалить management menu из olcrtc-setup.sh (~1800 строк)
- [ ] Оставить только установщик (~400–600 строк)
- [ ] Установщик скачивает и настраивает оба бинарника
- [ ] Повторный запуск → показывает статус + URL (без меню)
- [ ] Флаги: `--update`, `--uninstall`, `--show-token`, `--regenerate`

### v0.6 — Полировка

- [ ] Просмотр логов в реальном времени (SSE/WebSocket)
- [ ] Тёмная тема
- [ ] Мобильная адаптация
- [ ] Rate limiting, IP whitelist
- [ ] Обновление бинарников через UI (скачать + рестарт)
- [ ] Telegram-уведомления при падении

---

## 14. Открытые вопросы

1. ~~**Подписки через admin UI**~~ → **Решено**: admin проксирует `/sub/{slug}`.
   Единый URL `https://host:8443/sub/{slug}`. Порт 2096 остаётся только
   для localhost (внутренняя связь olcrtc ↔ admin).

2. **Обновление бинарника через UI** — позволить обновлять olcrtc из панели?
   (скачать новый бинарник, заменить, рестартить). Удобно, но рискованно.
   Запланировано в v0.6.

3. **Уведомления** — Telegram-бот / webhook при падении инстанса?
   Запланировано в v0.6.

4. **Мультиязычность** — UI на русском или EN + RU?
   Рекомендация: RU по умолчанию, переключатель EN/RU.

5. **Миграция** — при обновлении с текущей версии (menu-based) на новую
   (admin UI), нужен одноразовый скрипт миграции:
   - Обнаружить существующие env-файлы и инстансы
   - Установить olcrtc-admin
   - Сгенерировать admin.env + токен
   - Не ломать работающие туннели

6. **Fallback** — если olcrtc-admin упал/недоступен, пользователь может
   подключиться по SSH и выполнить базовые действия:
   - `olcrtc-admin -show-token` — показать токен
   - `systemctl restart olcrtc-server` — рестартить вручную
   - `olcrtc-setup.sh --update` — обновить бинарники
   Полноценное консольное меню **не нужно** — это edge case.
