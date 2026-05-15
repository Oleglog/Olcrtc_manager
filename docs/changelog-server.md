# Changelog — server-side work for `requirements-server.md`

Сводка изменений по форку `Olcrtc_manager-refactor-universal-carrier-fork`,
сделанных согласно `requirements-server.md`.

## S1. mobile API
**Проверено, изменений не требуется.** Сигнатуры `Start`/`StartWithTransport`
8-аргументные, валидаторы `errCarrierRequired`/`errRoomIDRequired`/
`errClientIDRequired`/`errKeyHexRequired` на месте. Существующий
`mobile_test.go::TestStartValidation` уже фиксирует, что
`startWithConfig("jazz", dataTransport, "", "", "key", ...)` возвращает
`errClientIDRequired`. `go test ./mobile/...` зелёный.

## S2. auth/salutejazz контракт
Существующий код в `internal/auth/salutejazz/salutejazz.go::Issue`
корректно разбирает все три кейса. Добавлен unit-test
`TestIssueRoomWithoutPassword` в новом файле
`internal/auth/salutejazz/salutejazz_test.go` — он подтверждает, что
`Provider.Issue` для `RoomURL: "abc-no-colon"` возвращает обёрнутый
`auth.ErrRoomIDRequired` с сообщением `expected <roomID>:<password>`.
`registerEngineAuth("jazz", authSaluteJazz.Provider{})` остаётся как было.

## S3. Smoke matrix
Матрица оформлена в `docs/test-matrix.md` со статусом `TBD` —
требует прогона в проде с реальными аккаунтами Yandex / Сбер / WB.

## S4. auth/telemost + engine/goolom
**Проверено, изменений не требуется.** `Provider.Issue` использует
`info.ClientConfig.MediaServerURL` через struct-tag `client_configuration`
с вложенным `media_server_url`. Достраивание короткого roomID до полного
URL делается проверкой `!strings.HasPrefix(roomURL, "https://")` и
конкатенацией с `roomURLPrefix`. Тесты `internal/auth/telemost/...` и
`internal/engine/goolom/...` зелёные.

## S5. auth/wbstream + регрессии
**Проверено, изменений не требуется.** Carrier `wbstream` продолжает
работать с тем же API (`registerGuest` → `createRoom` → `joinRoom` →
`getToken`). Жёсткая валидация на пустой Room ID для wbstream сохранена
в `updateInstanceConfig` (HTTP 400 с подробным сообщением).

## S6. Room password для jazz/salutejazz
Добавлено:
- В `internal/admin/api_instances.go`:
  - В struct `Instance` — поле `HasPassword bool` (`json:"has_password"`).
  - В `updateInstanceConfig` — приём `room_password` в request body, запись
    в `OLCRTC_ROOM_PASSWORD` через `WriteInstanceEnv`.
  - Хелпер `isJazzCarrier(c)` (jazz / salutejazz).
  - Новый эндпоинт `GET /api/instances/{id}/room-password` для отдачи
    raw-значения секрета (за тем же admin-token гейтом, что и остальное /api).
- В `server-install/systemd/olcrtc-launcher` — склейка
  `OLCRTC_ROOM_ID="${id}:${password}"` для carrier `jazz|salutejazz`,
  если `OLCRTC_ROOM_PASSWORD` непустой и в roomID ещё нет `:` (защита от
  двойной склейки при ручном редактировании env).
- В `internal/admin/static/app.js`:
  - В форме редактирования инстанса — поле `Room password` с toggle
    visibility, видимое только для `jazz`/`salutejazz`. Сабмит
    включает `room_password` только если пользователь его действительно
    редактировал — иначе backend сохраняет существующее значение.
  - В карточке инстанса — badge `🔒 password set` / `🔓 no password`.
- `buildURI` **не** включает password в URI — это явное требование S6.

## S7. Редизайн админ-панели
Полная переработка `internal/admin/static/{app.js,style.css}`:
- Карточный layout инстансов (`grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3`).
- Секции формы редактирования: Connection / Network / Advanced (последняя
  сворачивается).
- Цветовая схема: emerald для positive, rose для destructive, gray для
  neutral.
- Toast helper (`showToast(msg, kind)`) с success/error/info вариантами.
- Custom confirm-modal (`showConfirm(opts)`) — заменяет `confirm()`.
- QR-modal с кнопкой Download PNG (canvas → blob → download).
- Spinner на async-кнопках через `withLoading(btn, fn)`.
- Адаптивность: на <768px карточки в одну колонку, форма в одну колонку,
  все интерактивные зоны `min-h-10`.
- Иконки расширены: добавлены `lock`, `unlock`, `key`, `wifi`, `tag`,
  `clock`, `check-circle`, `x-circle`, `chevron-down`, `download`,
  `rotate-ccw`, `shield`, `sliders-horizontal`, `eye-off`,
  `alert-triangle`. Inline Feather SVG, как было.
- aria-label на всех icon-only кнопках, tab-order формы:
  carrier → transport → name → room_id → room_password → client_id → ...
- Не введены: фреймворки, i18n, light theme.

Размер: `app.js` ~64 KB, `style.css` ~8 KB → 72 KB суммарно (лимит
ТЗ — 200 KB). Бинарь `olcrtc-admin` собирается через `go build`,
embed FS подхватывает обновлённые файлы автоматически.

## S8. Server-issued client_id
Добавлено:
- В `Instance` struct — `ClientID string` (`json:"client_id"`).
- В `createInstance` — генерация `uuid.NewString()` и запись
  `OLCRTC_CLIENT_ID` в env при создании. Inherited значение от main env
  явно сбрасывается, чтобы binding-token не совпадал у разных инстансов.
- `ensureClientID()` — lazy-миграция: если `OLCRTC_CLIENT_ID` отсутствует,
  он генерируется и персистится в env при первом обращении.
- `buildURI`/`buildURIWith` — добавляет `&client_id=<uuid>` в query, если
  client_id непустой.
- Новый эндпоинт `POST /api/instances/{id}/rotate-client-id` —
  генерирует новый UUID, пишет в env, рестартует сервис.
- В `updateInstanceConfig` `client_id` от фронта **игнорируется** —
  ротация только через dedicated endpoint.
- Frontend: в форме инстанса появляется read-only поле `Client ID` с
  кнопками `Copy` / `Rotate`. Перед ротацией — confirm-modal с
  предупреждением о необходимости импорта нового URI.

**Strict AuthHook вынесен в S9 как opt-in задача.** В текущей реализации
`OLCRTC_CLIENT_ID` не консьюмится самим tunnel-сервером
(`defaultAuthHook` принимает любой непустой DeviceID), а binding-token
VP8-канала уже синхронизирован через `cfg.RoomURL`. Включение
`Server.Config.ExpectedClientID` без обратносовместимого fallback
сломает существующие развёртывания, поэтому требует отдельного
проектирования и явного opt-in флага в YAML.

## S9. Заметки и отложенные дефекты

### S9.1 (заметка) — VP8 binding token vs naming
В `internal/transport/vp8channel/transport.go::New` вызов
`bindingToken(cfg.RoomURL)` использует `RoomURL`, не реальный clientID,
несмотря на имя параметра функции (`func bindingToken(clientID string)`).
Функционально это работает: обе стороны (сервер и клиент) видят один и
тот же `RoomURL`, поэтому FNV32-хеш совпадает. Дефекта нет, но в коде
налицо рассинхрон имени и значения. Если в будущем потребуется отделить
binding-token от RoomURL (например, чтобы один и тот же `RoomURL` мог
обслуживать несколько независимых тунней-пар) — стоит переименовать или
явно передать `cfg.DeviceID`. Вне скоупа этого ТЗ.

### S9.2 (отложено) — Strict AuthHook (S8 opt-in)
См. S8 выше. Когда понадобится — план:
1. Добавить `Server.Config.ExpectedClientID string` в
   `internal/server/server.go`.
2. В `Run()` если `ExpectedClientID != ""` — переопределить `authHook`,
   возвращая ошибку `device mismatch: expected ... got ...` при
   несовпадении.
3. Прокинуть через `internal/config/config.go` (новое поле
   `auth.client_id` или `identity.client_id`) → `session.Config.DeviceID`
   → `server.Config.ExpectedClientID`.
4. Launcher: записать `OLCRTC_CLIENT_ID` в YAML.

### S9.3 (закрыто) — дефектов в S3/S4/S5 не выявлено
Smoke-прогон по матрице carrier×transport не выполнен в текущем
окружении (требует прода с реальными аккаунтами); статус ячеек —
`TBD`. Дефектов в коде S2/S4/S5 при ревью не выявлено.

## Регрессионные гарантии (повторно из ТЗ Section 3)

- (3.1) vp8channel — параметры `VP8FPS`/`VP8BatchSize` clamp `(1..120)` /
  `(1..64)` сохранён. Wire format не менялся.
- (3.2) videochannel — не трогался.
- (3.6) wbstream auth provider не менялся; правки в jazz/salutejazz и
  telemost изолированы.
- (3.7) SOCKS5 + WARP — не менялись.
- mobile API остаётся 8-аргументным.
- Тексты ошибок `errCarrierRequired`/`errRoomIDRequired`/
  `errClientIDRequired`/`errKeyHexRequired` не менялись.
