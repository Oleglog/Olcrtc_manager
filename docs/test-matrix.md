# Smoke test matrix — server side

Покрывает clauses bugfix.md 1.1–1.8 / 2.1–2.8 / 3.1 / 3.2 / 3.6.

Матрица фиксирует ожидаемое поведение для всех пар `carrier × transport`,
поддерживаемых форком `Olcrtc_manager-refactor-universal-carrier-fork`.
Прогон требует доступа к реальным сервисам (Yandex Telemost, SaluteJazz,
WB Stream) и реальной комнаты SaluteJazz с известным паролем — поэтому
выполняется в продовом окружении, не в CI.

## Подготовка

1. Соберите и установите бинари:

   ```bash
   go build -o bin/olcrtc ./cmd/olcrtc
   go build -o bin/olcrtc-admin ./cmd/olcrtc-admin
   sudo install -m0755 bin/olcrtc /usr/local/bin/olcrtc
   sudo install -m0755 bin/olcrtc-admin /usr/local/bin/olcrtc-admin
   sudo install -m0755 server-install/systemd/olcrtc-launcher /usr/local/bin/olcrtc-launcher
   sudo systemctl daemon-reload
   ```

2. В `/etc/olcrtc/env` заведите реальные значения:

   ```
   OLCRTC_CARRIER=<jazz|telemost|wbstream>
   OLCRTC_TRANSPORT=<datachannel|seichannel|vp8channel|videochannel>
   OLCRTC_ROOM_ID=...
   OLCRTC_ROOM_PASSWORD=...   # только для jazz, иначе пусто
   OLCRTC_KEY=<64-hex>
   OLCRTC_CLIENT_ID=<uuid>
   OLCRTC_DNS=1.1.1.1:53
   ```

3. Для каждой комбинации перезапустите сервис:

   ```bash
   sudo systemctl restart olcrtc-server
   sudo journalctl -u olcrtc-server -f
   ```

## Критерии «OK» для одной ячейки

- В логе видно `handshake completed` (или эквивалент: для tunnel-сервера
  это `link up`, `serving on tunnel session`).
- `transport ready` (открылся SOCKS5-листенер на стороне клиента).
- `curl --socks5 127.0.0.1:<socks_port> https://1.1.1.1` возвращает 200/204.
- `Mobile.WaitReady` отдаёт ready < 30 секунд.

При провале — фиксируется `FAIL: <reason>` и заводится подзадача в S9.

## Матрица

| carrier  | datachannel | seichannel | vp8channel | videochannel |
|----------|-------------|------------|------------|--------------|
| telemost | TBD         | TBD        | TBD        | TBD          |
| jazz     | TBD         | TBD        | TBD        | TBD          |
| wbstream | TBD         | TBD        | TBD        | TBD          |

`TBD` означает «требует прогона в проде». Ниже — ожидаемое поведение по
строкам ТЗ, к которому надо приводить ячейку при реальном smoke.

### `carrier=telemost`

`RoomURL` принимает короткий ID или полный URL `https://telemost.yandex.ru/j/<id>`
(см. `internal/auth/telemost/telemost.go::Issue`). Engine — `goolom`,
`MediaServerURL` берётся из `info.ClientConfig.MediaServerURL`. Telemost
не поддерживает создание комнат программно, ID должен существовать.

### `carrier=jazz`

`RoomURL` имеет формат `<roomID>:<password>` или одно из значений
`""`/`any`/`dummy` (создание новой комнаты). Без `:` сервер отдаёт
`auth.ErrRoomIDRequired: expected <roomID>:<password>` — это покрыто
unit-тестом `TestIssueRoomWithoutPassword` в
`internal/auth/salutejazz/salutejazz_test.go` и нет необходимости
дублировать его в smoke-сценарии. Пара `(jazz, vp8channel)` дополнительно
проверяет, что binding token VP8-канала совпадает на сервере и клиенте.

### `carrier=wbstream`

`RoomURL` — это roomID, созданный вручную на https://stream.wb.ru.
`""`/`any` всё ещё триггерит `createRoom`, но публичное API создания
комнат отключено WB Stream — поэтому в админ-UI присутствует жёсткая
валидация: для `wbstream` без roomID PUT возвращает 400 (см.
`internal/admin/api_instances.go::updateInstanceConfig`).

## Регрессионные проверки (S5)

- `curl http://127.0.0.1:<admin_port>/api/system/ports` — должен
  отдавать JSON со списком занятых портов.
- `curl http://127.0.0.1:<admin_port>/api/instances` — список инстансов;
  для каждого jazz-инстанса должен присутствовать `has_password: true|false`
  и непустой `client_id`.
- WARP / SOCKS5 (опционально, если `OLCRTC_WARP_PROXY` задан): проверить,
  что admin-API отвечает после рестарта инстанса.

## Журнал прогонов

Дата | Оператор | Версия | Результат
-----|----------|--------|----------
TBD  | TBD      | TBD    | TBD
