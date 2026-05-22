# ТЗ: серверная сторона (форк `Olcrtc_manager-refactor-universal-carrier-fork`)

Документ описывает работы по нашему форку сервера olcRTC: `temp-files/Olcrtc_manager-refactor-universal-carrier-fork` (Go-модуль). Сервер уже переехал на новую ветку `refactor/universal-carrier` и архитектурно состоит из слоёв `internal/auth/{salutejazz,telemost,wbstream}` + `internal/engine/{goolom,livekit,salutejazz}` + регистрация в `internal/carrier/builtin/register.go`. Помимо ревизии и smoke-тестов совместимости с обновлённым клиентом, в рамках этого ТЗ дорабатывается **админ-панель сервера** (`olcrtc-admin`, SPA в `internal/admin/static/`): добавляются поля `Room ID` и `Room password` для carrier `jazz`/`salutejazz` и обновляется визуальная составляющая под более современный вид.

Сюда не входят клиентские правки (Kotlin/XML, Go-модуль клиента `library/core/olcrtc_local`, AAR) — они описаны в `requirements-client.md`.

## Контекст и корневая причина (релевантная серверу)

Из `bugfix.md`:

- Сервер собран по новой модели и регистрирует carrier `jazz` через `authSaluteJazz.Provider{}` (т.е. `jazz` — алиас для `salutejazz`). Поле `RoomURL` у carrier `jazz` ожидается в формате `<roomID>:<password>` (см. `internal/auth/salutejazz/salutejazz.go` → `Issue`: `roomID, password, hasPassword := strings.Cut(roomRef, ":")`). При отсутствии `:` сервер обязан вернуть `auth.ErrRoomIDRequired: expected <roomID>:<password>`.
- Mobile API сервера (`mobile/mobile.go`) требует `clientID` через `validateStartArgs → errClientIDRequired`. После апдейта клиента, который начнёт передавать `clientID`, путь должен оставаться зелёным; дополнительно нужно убедиться, что серверный путь не ломается при пустом `clientID` от старых клиентов (внятная ошибка, не паника).
- Транспорты `datachannel` и `seichannel`, а также carrier `telemost` после фикса клиента должны заработать end-to-end. На сервере нужно прогнать smoke по всем парам carrier × transport.
- Регрессии должны быть сохранены: `SOCKS5+WARP`, `admin/ports/api`, существующие `wbstream`-профили (`vp8channel`/`videochannel`), `provider != jazz` без password.

Дополнительный контекст по UI админ-панели (для задач S6–S7):

- Админка — это отдельный бинарь `olcrtc-admin` (см. `cmd/...`, systemd unit `server-install/systemd/olcrtc-admin.service`), который встраивает SPA через `//go:embed static/*` в `internal/admin/server.go`.
- Стек фронтенда: **vanilla JS** (`static/app.js`, без Vue/React/Alpine), **Tailwind CSS через CDN** (`https://cdn.tailwindcss.com` в `static/index.html`), иконки — inline SVG в стиле Feather (см. функцию `icon(name, sz)` в `app.js`), QR — `qrcodejs` через CDN. Кастомные стили — `static/style.css`.
- Конфигурация инстанса хранится в env-файле `/etc/olcrtc/<N>/env` (формат `KEY=VALUE`). API админки читает/пишет эти env-файлы через `internal/admin/env.go` (`ReadInstanceEnv`, `WriteInstanceEnv`, `SetEnvValue`). На текущий момент в env есть `OLCRTC_CARRIER`, `OLCRTC_TRANSPORT`, `OLCRTC_ROOM_ID`, `OLCRTC_KEY`, `OLCRTC_NAME`, `OLCRTC_DNS`, `OLCRTC_VP8_FPS`, `OLCRTC_VP8_BATCH`, `OLCRTC_SEI_*`, но **нет** `OLCRTC_ROOM_PASSWORD`. Это и есть причина, по которой сервер не может присоединиться к существующей комнате SaluteJazz: в админке нет места ввести password, а в env нет ключа для его хранения.
- Структура `Instance` в `internal/admin/api_instances.go` (поля `room_id`, `carrier`, `transport`, ...) отдаётся на фронт как JSON. На фронте форма редактирования инстанса (`app.js`) генерируется динамически и содержит поля Room ID, Carrier, Transport и др., но не содержит поля Room password.
- URI экспорта инстанса (`buildURI`) в админке формирует `olcrtc://<carrier>@room/<roomID>?key=...&transport=...` **без** password — это правильно, password не должен попадать в шарящийся URI.

## Список задач

### S1. Ревью совместимости mobile API сервера с обновлённым клиентом

- Покрывает clauses bugfix.md: 1.1, 2.1.
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/mobile/mobile.go`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/mobile/mobile_test.go`
- Что сделать:
  - Перепроверить, что сигнатуры `Start(carrierName, roomID, clientID, keyHex string, socksPort int, socksUser, socksPass string)` и `StartWithTransport(carrierName, transportName, roomID, clientID, keyHex string, socksPort int, socksUser, socksPass string)` соответствуют тем, которые после фикса будет вызывать клиент через AAR.
  - Убедиться, что `validateStartArgs` возвращает понятные ошибки `errCarrierRequired`, `errRoomIDRequired`, `errClientIDRequired`, `errKeyHexRequired`. Сообщения логируются при неуспешном handshake (а не глотаются).
  - Если вдруг обнаружится расхождение с эталоном `temp-files/olcrtc-refactor-universal-carrier/mobile/mobile.go` (наш форк должен быть бинарно совместим по сигнатуре с исходной веткой `refactor/universal-carrier`) — задокументировать и поправить.
- Acceptance criteria:
  - `go build ./...` и `go test ./mobile/...` проходят без ошибок в `temp-files/Olcrtc_manager-refactor-universal-carrier-fork`.
  - В тесте `mobile_test.go::TestStartValidation` подтверждено, что вызов `startWithConfig("jazz", dataTransport, "", "", "key", 1080, "", "", mobileConfig{})` возвращает `errClientIDRequired`, а вызов с непустым `clientID` доходит до карьера.
  - Если правки не понадобились — в этой задаче явно зафиксировать «проверено, изменений не требуется» в commit message / changelog.

### S2. Ревью контракта `auth/salutejazz` (`<roomID>:<password>`)

- Покрывает clauses bugfix.md: 1.4, 2.4, 1.9, 2.9.
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/auth/salutejazz/salutejazz.go`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/auth/salutejazz/*_test.go`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/carrier/builtin/register.go`
- Что сделать:
  - Подтвердить, что `Provider.Issue` корректно разбирает три кейса:
    - пустая строка / `"any"` / `"dummy"` → `createRoom(ctx)` (создание новой комнаты);
    - `"<roomID>:<password>"` → `joinRoom(ctx, roomID, password)`;
    - `"<roomID>"` без `:` → возвращается `fmt.Errorf("%w: expected <roomID>:<password>", auth.ErrRoomIDRequired)` (а не паника / 500).
  - Убедиться, что `Register()` в `internal/carrier/builtin/register.go` биндит `jazz` именно на `authSaluteJazz.Provider{}` (а не на `wbstream`/`telemost`).
  - Проверить, что в логах при неуспешной авторизации видна явная причина `expected <roomID>:<password>` — это упростит диагностику со стороны Android-клиента до момента, пока он не начнёт правильно собирать `RoomURL`.
- Acceptance criteria:
  - Существующие unit-тесты `internal/auth/salutejazz/...` зелёные.
  - Добавлен (или существует) кейс «room без пароля → `ErrRoomIDRequired` с подсказкой про формат». Если кейса нет — добавить.
  - В smoke-сценарии S5 carrier=`jazz` без `:` отдаёт 4xx с описанной причиной, а не подвисает.

### S3. Smoke-тесты по всем парам carrier × transport

- Покрывает clauses bugfix.md: 1.1–1.8, 2.1–2.8, 3.1, 3.2, 3.6.
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/e2e/...` (если применимо).
  - Скрипты в `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/script/`.
  - Конфигурационные YAML в `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/data/` (если используется dev-конфиг).
- Что сделать:
  - Прогнать сервер локально (`docker-compose.server.yml` или прямой запуск `cmd/...`) и проверить все 12 комбинаций:
    - carrier ∈ `{telemost, jazz, wbstream}`,
    - transport ∈ `{datachannel, seichannel, vp8channel, videochannel}`.
  - Для каждой комбинации зафиксировать ожидаемое поведение в виде матрицы (таблица в commit message или отдельный файл `docs/test-matrix.md`):
    - handshake завершается без ошибок;
    - SOCKS5-листенер открывается;
    - тестовый трафик через локальный SOCKS5 (`curl --socks5 127.0.0.1:<port> https://1.1.1.1`) возвращает 200/204;
    - `Mobile.WaitReady` отдаёт ready < 30 секунд.
  - Для `carrier=jazz` использовать `RoomURL = "<roomID>:<password>"` (новый формат). Для `carrier=telemost` — короткий roomID или полный URL `https://telemost.yandex.ru/j/<roomID>`. Для `carrier=wbstream` — текущий формат, что и сейчас.
  - При обнаружении проблемы (любая комбинация падает) — оформить отдельной задачей S9 (см. ниже) с описанием stack trace и предложением фикса.
- Acceptance criteria:
  - Создана матрица `12/12` с явным статусом каждой ячейки (`OK` / `FAIL: <reason>`).
  - Для `OK` есть лог запуска со словами `handshake completed` и `transport ready`.
  - Для `FAIL` создана подзадача в S9.

### S4. Проверка `auth/telemost` и `engine/goolom`

- Покрывает clauses bugfix.md: 1.2, 1.5, 1.6, 2.2, 2.5, 2.6.
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/auth/telemost/`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/engine/goolom/`
- Что сделать:
  - Подтвердить, что `auth/telemost.Provider.Issue` парсит ответ с полем `info.ClientConfig.MediaServerURL` (а не устаревший `clientConfig.mediaServerURL`).
  - Подтвердить, что `engine/goolom.New` корректно стартует на `MediaServerURL` после `Issue`.
  - Подтвердить, что обработка короткого `roomId` (без префикса `https://telemost.yandex.ru/j/`) работает: входной `RoomURL` достраивается до полного URL до отправки в Telemost API.
  - Если в коде есть TODO/устаревшие пути (старые имена полей, старые URL) — починить их и обновить тесты.
- Acceptance criteria:
  - `go test ./internal/auth/telemost/... ./internal/engine/goolom/...` зелёный.
  - Smoke-тест S3 для пары `(telemost, datachannel)` и `(telemost, vp8channel)` — `OK`.

### S5. Проверка `auth/wbstream` и регрессии существующих профилей

- Покрывает clauses bugfix.md: 3.6, 3.7.
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/auth/wbstream/`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/protect/`
- Что сделать:
  - Подтвердить, что carrier `wbstream` со всеми текущими транспортами продолжает работать без изменений в API.
  - Подтвердить совместимость с `protect.SetSocks5(...)` и WARP-маршрутизацией (ports/api сервера остаются прежними, новые поля не ломают сериализацию).
  - Убедиться, что в `internal/admin/...` HTTP-эндпоинты (`/ports`, `/api/...`) отдают корректные ответы на запросы вида `GET`/`POST` (как минимум — health-check).
- Acceptance criteria:
  - Smoke S3 для всех `(wbstream, *)` — `OK`.
  - Запрос `curl http://127.0.0.1:<admin_port>/ports` возвращает JSON с актуальными портами.
  - Регрессионный тест на существующий wbstream-профиль (импорт старого config-файла, если есть в `data/`) проходит без ошибок.

### S6. Поля `Room ID` и `Room password` для carrier `jazz`/`salutejazz` в админ-панели

- Покрывает clauses bugfix.md: 1.4, 1.9, 1.10, 2.4, 2.9, 2.10.
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/api_instances.go` — структура `Instance`, обработчик `updateInstanceConfig`, `buildInstance`.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/env.go` — чтение/запись env-файла.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/static/app.js` — форма редактирования инстанса.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/cmd/...` (запускающий код, который превращает env-переменные в аргументы `Mobile.Start`/`StartWithTransport`) — точка склейки `<roomID>:<password>` → `RoomURL`.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/server-install/systemd/olcrtc-server*.service` — env-файл, прокидываемый сервису. Самописанные юниты переменную подхватят автоматически (через `EnvironmentFile`), но проверить, что переменная `OLCRTC_ROOM_PASSWORD` не фильтруется.
- Что сделать:
  - **Backend (Go).**
    - Добавить новый env-ключ `OLCRTC_ROOM_PASSWORD` (значение по умолчанию — пустая строка). Он SHALL храниться в том же `/etc/olcrtc/<N>/env`, что и существующие `OLCRTC_*`.
    - В `Instance` struct (`api_instances.go`) добавить JSON-поле `RoomPassword string `json:"room_password"`` — но **только** для исходящих ответов админки на тот же origin (loopback / админ-токен). Не включать его в `buildURI`.
    - В `updateInstanceConfig` добавить ветку:
      ```
      if v, ok := req["room_password"].(string); ok {
          updates["OLCRTC_ROOM_PASSWORD"] = v
      }
      ```
      `TrimSpace` применяется аналогично существующему `OLCRTC_ROOM_ID`.
    - В `buildInstance` заполнять поле `RoomPassword: vals["OLCRTC_ROOM_PASSWORD"]` (только при чтении админкой; если паттерн «не возвращать секреты на фронт» уже соблюдается для `OLCRTC_KEY` — применить тот же подход: показывать на фронте маску `••••` или флаг `has_password: true`, а реальное значение возвращать только при специальном эндпоинте `GET /api/instances/{id}/room-password`, защищённом тем же admin-токеном).
    - В точке склейки `RoomURL` (там, где из env-переменных собирается аргумент `Start`/`StartWithTransport`): если `carrier ∈ {jazz, salutejazz}` и `OLCRTC_ROOM_PASSWORD` не пуст — формировать `roomURL = roomID + ":" + password`. Иначе — передавать `roomID` как сейчас. Эта логика SHALL быть в одном месте (server-side), чтобы клиент по протоколу olcrtc → SOCKS5 не зависел от знания пароля комнаты.
  - **Frontend (`static/app.js`).**
    - Добавить в форму редактирования инстанса поле «Room password» (тип `<input type="password">`, рядом с переключателем «показать»). Поле SHALL отображаться **только** когда `carrier === 'jazz'` или `carrier === 'salutejazz'`. Для других carrier — скрыто (либо через `display: none`, либо не рендерить вовсе).
    - Сабмит формы SHALL передавать новое поле в `PUT /api/instances/{id}/config` как ключ `room_password`.
    - В блоке отображения инстанса (карточка / list item) **не** показывать значение `room_password` в открытом виде; разрешено отображать только индикатор «🔒 password set» / «no password». Иконка — реюз существующего набора (`alert-circle`/`eye`/добавить новую `lock`).
    - Кнопка «Show password» — опционально, по такой же кнопке-«глазу», как у остальных секретов (если такой паттерн уже есть в админке — переиспользовать).
  - **Валидация.**
    - При carrier=`jazz`/`salutejazz` и попытке старта инстанса с пустым `OLCRTC_ROOM_ID` — отдавать 400 с понятным сообщением (как уже сделано для `wbstream`). Пустой password при заданном `room_id` — допустим (создаётся новая комната или используется без пароля).
    - При carrier ≠ jazz/salutejazz и заданном `OLCRTC_ROOM_PASSWORD` — НЕ отбрасывать значение (хранится как есть, но не используется в `RoomURL`). Это упрощает переключение carrier без потери данных в env.
- Acceptance criteria:
  - В env-файле инстанса с `OLCRTC_CARRIER=jazz` после сохранения формы появляется ключ `OLCRTC_ROOM_PASSWORD=<value>`.
  - `GET /api/instances/{id}` возвращает либо `room_password` напрямую, либо `has_password: true` (в зависимости от выбранного паттерна, см. выше) — но никогда не пустую строку, если password установлен.
  - В админ-UI на форме инстанса с `carrier=jazz` есть поле `Room password` рядом с `Room ID`. При смене carrier на `telemost` или `wbstream` поле скрывается без перезагрузки страницы.
  - Запуск инстанса с `carrier=jazz`, `OLCRTC_ROOM_ID=abc` и `OLCRTC_ROOM_PASSWORD=secret` приводит к успешному `auth/salutejazz.Issue → joinRoom("abc", "secret")` (проверяется логом сервиса).
  - Запуск инстанса с `carrier=jazz`, `OLCRTC_ROOM_ID=abc` без password приводит либо к успешному `joinRoom("abc", "")` (если SaluteJazz позволяет), либо к понятной ошибке `auth.ErrRoomIDRequired: expected <roomID>:<password>` (а не 500/паника).
  - Запуск инстанса с `carrier=telemost` и непустым `OLCRTC_ROOM_PASSWORD` работает идентично сценарию без password — значение не подмешивается в `RoomURL`.
  - URI, возвращаемый `GET /api/instances/{id}/uri`, **не содержит** password ни в каком виде. Импорт URI клиентом продолжает работать как сейчас.

### S7. Редизайн админ-панели под более современный вид

- Покрывает clauses bugfix.md: 1.11, 2.11.
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/static/index.html`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/static/app.js`
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/static/style.css`
  - При необходимости — добавление новых SVG-иконок в `icon(name, sz)` в `app.js`.
- Что сделать:
  - **Сохранить технологический стек.** Не вводить новые runtime-зависимости (Vue/React/Alpine/jQuery). Остаёмся на vanilla JS + Tailwind CDN + inline SVG. Это снижает риск регресса embed-сборки (`go:embed static/*`) и не требует bundler-а.
  - **Layout и иерархия.**
    - На странице списка инстансов — карточный layout (`grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4`), каждая карточка показывает: статус (цветной индикатор + текст), label («Основной» / «Доп. #N»), carrier+transport как badge'ы, room id (с кнопкой copy), uptime, и горизонтальный ряд actions (`Start`/`Stop`/`Restart`/`Edit`/`Delete`). Фон карточки `bg-gray-800`, hover-state с лёгкой подсветкой и `transition`.
    - На форме редактирования — двухколоночная сетка с группировкой полей по секциям: «Connection» (carrier, transport, room_id, room_password, key), «Network» (dns, socks_proxy, warp_proxy), «Advanced» (vp8_*, sei_*, debug). Каждая секция — со своим заголовком и тонкой границей `border-gray-700`.
    - На странице логина — центрированная карточка с лого/заголовком, полем для admin-token и кнопкой «Sign in». Опционально — link «Forgot token?» с подсказкой про `/etc/olcrtc/admin.env`.
  - **Цветовая схема и состояния.**
    - Полностью dark theme (соответствует текущему `bg-gray-900`), но с более выраженной контрастностью: основные кнопки — `bg-emerald-600 hover:bg-emerald-500`, деструктивные — `bg-rose-600 hover:bg-rose-500`, secondary — `bg-gray-700 hover:bg-gray-600`. Исходная схема `status-running`/`status-failed`/`status-inactive` остаётся в `style.css`, но цвета приводятся в соответствие (running → emerald, failed → rose, inactive → gray).
    - Обновить `style.css` так, чтобы для всех `<input>`/`<select>` был единый стиль (focus ring `ring-emerald-500/30`, плейсхолдер `text-gray-500`).
  - **Обратная связь и микро-UX.**
    - Toast-уведомления при успешных и неудачных операциях (создание/удаление инстанса, копирование URI, ротация ключа). Реализовать через лёгкий self-hosted helper в `app.js` (примерно 30 строк, без библиотеки).
    - Кнопки `Start`/`Stop`/`Restart` SHALL переходить в `disabled + spinner` на время запроса, чтобы не было двойных кликов.
    - Confirmation-modal для деструктивных операций (delete, rotate-key, rotate-room) — простой кастомный modal на vanilla JS, без `confirm()`. Текст описывает последствия (ротация ключа = клиенты должны импортировать новый URI).
    - QR-код инстанса показывать в modal при клике на иконку `qr-code`, с возможностью скачать PNG (canvas → blob → `download`).
  - **Иконки.**
    - Расширить набор в функции `icon(name, sz)` (`app.js`) добавлением: `lock`, `unlock`, `wifi`, `wifi-off`, `network`, `tag`, `clock`, `check-circle`, `x-circle`, `chevron-down`. Все — Feather Icons inline SVG (24×24 viewBox, `currentColor`).
    - У каждого ключевого поля формы редактирования инстанса вывести иконку слева от label (как в вёрстке Material 3-like форм).
  - **Адаптивность.**
    - Sidebar/header SHALL сжиматься на экранах <768px (Tailwind `md:` breakpoint). Карточки инстансов схлопываются в одну колонку. Форма редактирования — в одну колонку. Все интерактивные зоны достаточного размера (`min-h-10`).
  - **Доступность (минимум).**
    - Все `<button>` имеют `aria-label`, если содержат только иконку.
    - Tab-order формы редактирования инстанса логичный: carrier → transport → room_id → room_password → key → ...
    - Контраст текста в карточках статуса не ниже WCAG AA (точечная проверка на эмодзи/badge'ах).
  - **Что НЕ делается.**
    - Не вводить роутер с фреймворком (текущий `route()` на `history.pushState` остаётся).
    - Не вводить i18n-инфраструктуру (тексты остаются на русском там, где сейчас русские, и на английском там, где сейчас английские).
    - Не делать тёмная/светлая тема — только dark theme (соответствует текущей схеме).
- Acceptance criteria:
  - Скриншот списка инстансов (3+ инстанса) демонстрирует карточный layout с цветным статус-индикатором, бэйджами carrier/transport, и горизонтальным action-row.
  - Скриншот формы редактирования инстанса с `carrier=jazz` показывает три секции (Connection / Network / Advanced) с group-заголовками и иконками рядом с полями. Поле `Room password` видно. После переключения carrier на `telemost` поле `Room password` скрывается без перезагрузки.
  - Toast появляется и исчезает через 3–5 секунд при успешном создании инстанса.
  - При попытке удалить инстанс открывается confirmation-modal с текстом и двумя кнопками; `Esc`/клик вне modal закрывает его без удаления.
  - При нажатии на иконку QR показывается modal с QR-кодом и кнопкой «Download PNG», файл скачивается корректно (PNG валиден).
  - На экране 360×640 (mobile) интерфейс не ломается: карточки в одну колонку, форма читаема, все кнопки кликабельны.
  - Размер бандла `static/app.js` + `static/style.css` после правок не превышает 200 KB суммарно (мониторинг: `wc -c`). Если превышает — задокументировать причину.
  - Бинарь `olcrtc-admin` пересобирается командой из `magefile.go` или `go build` без ошибок; embed-FS подхватывает обновлённые статические файлы.

### S8. Server-issued `clientID`: генерация в админке, прокидка в URI, опциональный strict AuthHook

- Покрывает clauses bugfix.md: 1.1, 2.1 (контракт mobile API + симметрия `clientID` между сервером и клиентом).
- Файлы / пакеты:
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/api_instances.go` — `Instance` struct, `createInstance`, `updateInstanceConfig`, `buildInstance`, `buildURI`, новый эндпоинт `rotate-client-id`.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/env.go` — чтение/запись env-файла (без структурных правок, просто новый ключ).
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/admin/static/app.js` — UI для отображения и ротации `client_id`.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/cmd/olcrtc/main.go` или `internal/config/*.go` (или `internal/app/session/*.go`) — точка, где env/YAML конвертируется в аргументы серверного процесса; сюда нужно прокинуть `clientID` для `bindingToken`.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/server/server.go` — `defaultAuthHook` и `Server.Config.AuthHook` (опциональная strict-проверка).
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/internal/transport/vp8channel/transport.go` — здесь не правится, но **должно** иметь актуальный `bindingToken := bindingToken(clientID)`, что упоминается в комментариях кода.
  - `temp-files/Olcrtc_manager-refactor-universal-carrier-fork/server-install/systemd/olcrtc-server*.service` — env-файл подхватывает новый ключ через `EnvironmentFile` (никаких правок не требуется, но смок-проверить).
- Контекст (важно для исполнителя):
  - В текущем коде `defaultAuthHook` в `internal/server/server.go` принимает любой `deviceID` и возвращает случайный session UUID. То есть `clientID` на сервере **не сравнивается** ни с чем — он просто требуется непустым (`validateStartArgs → errClientIDRequired`).
  - `clientID` действительно используется в `internal/transport/vp8channel/transport.go` для генерации `bindingToken` (FNV32 от строки). Кадры с чужим binding token отбрасываются (`TestHandleIncomingFrameIgnoresForeignBindingToken`). Поэтому **`clientID` на клиенте и на сервере для одной сессии должен совпадать**, иначе VP8-канал работать не будет, даже если handshake прошёл.
  - Логичная схема: `clientID` — это «идентификатор пары инстанс-сервер ↔ клиент-профиль». Генерируется один раз на сервере (в админке) и передаётся клиенту через тот же канал, что и `key`/`carrier`/`roomID` — то есть через QR/`olcrtc://` URI. Каждый инстанс админки имеет **свой** `clientID`; разные клиенты, импортировавшие один и тот же URI, будут иметь одинаковый `clientID` (это ОК для текущей модели — мы не различаем устройства, мы различаем «инстансы»).
  - Если в будущем потребуется отделить устройства друг от друга — это уже задача расширения `AuthHook` (например, проверять `claims["device"]` отдельно от `deviceID`); вне скоупа этого ТЗ.
- Что сделать:
  - **Backend (Go).**
    - Добавить новый env-ключ `OLCRTC_CLIENT_ID` (значение по умолчанию — пустая строка). Хранится в `/etc/olcrtc/<N>/env` рядом с `OLCRTC_KEY`.
    - В `Instance` struct (`api_instances.go`) добавить JSON-поле `ClientID string `json:"client_id"``. В отличие от `room_password`, `client_id` НЕ является секретом (его всё равно увидит каждый клиент, импортирующий URI), поэтому отдавать на фронт можно в открытом виде.
    - В `createInstance`: если `OLCRTC_CLIENT_ID` отсутствует или пуст — сгенерировать `uuid.NewString()` (используется уже существующий `github.com/google/uuid`, см. `internal/server/server.go:defaultAuthHook`) и записать в env. **Существующие** инстансы при первом обращении админки к ним тоже SHALL получить `OLCRTC_CLIENT_ID` (lazy-миграция в `buildInstance` или `getInstance`: если в env-файле нет ключа — сгенерировать, записать через `SetEnvValue`, перечитать).
    - В `updateInstanceConfig` НЕ принимать `client_id` от фронта в виде произвольной строки. Если фронт прислал `client_id` — игнорировать (значение управляется только через `rotate-client-id`). Это защищает от опечаток и от ситуации, когда юзер случайно ввёл невалидный UUID.
    - Добавить новый эндпоинт `POST /api/instances/{id}/rotate-client-id` (по аналогии с `rotate-key`):
      - сгенерировать новый `uuid.NewString()`,
      - записать в env как `OLCRTC_CLIENT_ID`,
      - перезапустить инстанс (`SystemctlRestart`),
      - вернуть `{"ok": true, "client_id": "<new>"}`.
      - В UI это кнопка с явным confirmation-modal: «Ротация Client ID отвяжет всех существующих клиентов, им придётся импортировать новый URI».
    - В `buildURI`: добавить новый query-параметр `client_id=<uuid>`. **Только** если `OLCRTC_CLIENT_ID` непустой (защита от инстансов, которые ещё не прошли lazy-миграцию).
    - В точке склейки `Mobile.Start*` на сервере (мы запускаем сервер через `cmd/olcrtc <config.yaml>`, а не через mobile API; но для server-side Mobile-флоу — например в e2e-тестах или если используется `cmd/olcrtc-cgo` — нужно подставить `OLCRTC_CLIENT_ID` четвёртым аргументом). Если серверный путь не использует mobile API — задача сводится к тому, чтобы `client.Config.DeviceID = OLCRTC_CLIENT_ID` (а оттуда `bindingToken(clientID)` уйдёт в `vp8channel.transport`).
  - **Frontend (`static/app.js`).**
    - В карточке инстанса (см. S7) показывать `Client ID` как отдельное поле в секции «Connection» (или «Identity»), с кнопкой `Copy` и кнопкой `Rotate` (вызывает `POST /api/instances/{id}/rotate-client-id`).
    - В форме редактирования инстанса показывать `Client ID` как **read-only** строку (рядом с `Room ID`/`Room password`) с пометкой «Управляется кнопкой Rotate». Поле НЕ редактируется через input.
    - В URI-modal (показ QR/copy URI) `client_id` уже включён в URI через `buildURI` — на фронте дополнительно ничего делать не нужно.
  - **Опциональный strict AuthHook (рекомендовано, но можно вынести в отдельную задачу S9b).**
    - В `internal/server/server.go` или в точке склейки YAML→`Server.Config` добавить параметр `Server.Config.ExpectedClientID string`.
    - Если `ExpectedClientID != ""` — переопределить `AuthHook`:
      ```go
      authHook = func(deviceID string, claims map[string]any) (string, error) {
          if deviceID != cfg.ExpectedClientID {
              return "", fmt.Errorf("device mismatch: expected %q, got %q", cfg.ExpectedClientID, deviceID)
          }
          return uuid.NewString(), nil
      }
      ```
    - Прокинуть `OLCRTC_CLIENT_ID` из env в `Server.Config.ExpectedClientID` через `internal/config/*.go`.
    - В YAML-конфиге сервера `cmd/olcrtc <config.yaml>` поддержать новое поле `client_id: "..."` на верхнем уровне (или nested под `auth:`/`identity:`). Дефолт — пусто (= нет strict-проверки, поведение не меняется).
    - Это поведение SHALL быть **opt-in** — по умолчанию (`ExpectedClientID == ""`) сервер работает как сейчас, принимая любой непустой `deviceID`. Иначе мы сломаем все существующие развёртывания.
- Acceptance criteria:
  - При создании нового инстанса через `POST /api/instances` env-файл содержит ключ `OLCRTC_CLIENT_ID=<uuid>`. Значение валидно по формату UUID v4.
  - `GET /api/instances/{id}` возвращает поле `client_id` с тем же значением.
  - URI, возвращаемый `GET /api/instances/{id}/uri`, содержит `&client_id=<uuid>` в query-string.
  - Существующий инстанс **без** `OLCRTC_CLIENT_ID` в env-файле при первом открытии в админке получает сгенерированный UUID и продолжает работать (lazy-миграция).
  - Кнопка `Rotate` в UI вызывает `POST /api/instances/{id}/rotate-client-id`, env-файл обновляется новым UUID, инстанс перезапускается.
  - VP8-канал между сервером и клиентом, импортировавшим URI с `client_id=X`, корректно работает: handshake завершается, кадры не отбрасываются как «foreign binding token». Проверяется в smoke S3 для пары `(jazz, vp8channel)` и `(telemost, vp8channel)`.
  - Если выбран strict AuthHook (опционально): запуск клиента с `clientID` ≠ `OLCRTC_CLIENT_ID` сервера приводит к ошибке handshake `device mismatch: expected ... got ...` (а не к тихому падению на VP8-фрейме). Запуск с правильным `clientID` проходит как раньше.
  - URI **не** содержит `OLCRTC_ROOM_PASSWORD` (см. S6) — `client_id` присутствует, password — нет.

### S9. Заглушка под обнаруженные дефекты

- Покрывает: задачи открытые после S3.
- Файлы / пакеты: зависит от дефекта.
- Что сделать:
  - Если в ходе S3/S4/S5 обнаружились реальные баги на стороне сервера — каждый оформить отдельным пунктом с указанием:
    - точный путь файла;
    - воспроизведение (входная команда / комбинация carrier×transport);
    - ожидаемое поведение (со ссылкой на bugfix.md clause);
    - предложенный фикс.
  - Если ничего не обнаружено — зафиксировать в commit message / changelog: «S9: дефектов не выявлено».
- Acceptance criteria:
  - Все обнаруженные дефекты закрыты ПР-ами или записаны в issue tracker.
  - В случае «ничего не обнаружено» — явная отметка в финальной сводке исполнителя.

## Out of scope

- Любые правки в Android-клиенте (`Exclave_FORK/app/src/...`).
- Любые правки в Go-модуле клиента (`Exclave_FORK/library/core/olcrtc_local/...`) и пересборка `app/libs/libsagernetcore.aar`.
- Изменения схемы URI `olcrtc://...` на стороне сервера сверх того, что уже формирует `buildURI` (сервер не парсит этот URI; парсинг — задача клиента).
- Включение `OLCRTC_ROOM_PASSWORD` или иных секретов в URI экспорта инстанса.
- Миграции существующих сериализованных Kryo-профилей (это клиентская задача).
- Изменения версии Go, обновление зависимостей `go.mod` (если этого явно не потребует фикс из S9).
- Переход на Vue/React/Alpine/jQuery в админ-панели (остаёмся на vanilla JS).
- Внедрение i18n / светлой темы / переключателя темы.

## Регрессионные гарантии

Из `bugfix.md` Section 3, отфильтровано до серверной части:

- (3.1) `transport = vp8channel` для всех carrier продолжает работать через KCP/vp8 без изменений в wire-формате и без изменения параметров `VP8FPS`/`VP8BatchSize`. Серверный clamp `(1..64)` для `vp8BatchSize` и `(1..120)` для `vp8FPS` сохраняется.
- (3.2) `transport = videochannel` для всех carrier продолжает передавать видеопоток (qrcode/tile codec) с теми же параметрами.
- (3.6) carrier `wbstream` на любом транспорте продолжает работать без правок в auth-провайдере и engine — изменения в `auth/salutejazz` и `auth/telemost` не должны цеплять `auth/wbstream`.
- (3.7) SOCKS5 + WARP-маршрутизация (`protect.SetSocks5(...)`, `internal/admin/...`, ports/api) работают как сейчас, без регрессий после фиксов в smoke S3–S5.
- Контракт mobile API остаётся 8-аргументным (`Start` / `StartWithTransport`); сигнатура не должна меняться, иначе сломаются эталонные клиенты на ветке `refactor/universal-carrier`.
- Серверные ошибки `errCarrierRequired`/`errRoomIDRequired`/`errClientIDRequired`/`errKeyHexRequired` остаются с теми же текстами (или дополняются, но не переименовываются), чтобы клиент мог различать причины отказа в логах.
