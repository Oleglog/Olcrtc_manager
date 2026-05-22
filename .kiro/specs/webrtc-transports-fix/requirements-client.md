# ТЗ: клиентская сторона (Exclave_FORK — Android-клиент + Go-модуль `library/core/olcrtc_local`)

Документ описывает работы по Android-клиенту `Exclave_FORK`. Клиент состоит из трёх связанных слоёв, и все они правятся в рамках этого ТЗ:

- **Go-ядро клиента** — локальный модуль `library/core/olcrtc_local` (более ранняя ревизия olcRTC), который на сборке превращается в `app/libs/libsagernetcore.aar` через `library/core/build.sh`.
- **Kotlin-обвязка mobile API** — `app/src/main/java/io/nekohasekai/sagernet/bg/proto/OLCRTCExternalInstance.kt`, `fmt/olcrtc/*`, `database/DataStore.kt`, `Constants.kt`.
- **UI и ресурсы** — `OLCRTCSettingsActivity.kt`, `app/src/main/res/xml/olcrtc_preferences.xml`, строковые ресурсы, иконки, layout-ы Material 3.

Все правки серверной стороны вынесены в `requirements-server.md` и в скоуп этого ТЗ не входят.

## Контекст и корневая причина (релевантная клиенту)

Из `bugfix.md`:

- **Сборочная ловушка `go.mod`.** В `library/core/go.mod` присутствует директива `replace github.com/openlibrecommunity/olcrtc => ./olcrtc_local`. Это значит, что `library/core/build.sh` / `build.bat`, несмотря на то что в команде `gomobile bind` указан upstream-импорт `github.com/openlibrecommunity/olcrtc/mobile`, на самом деле собирает AAR из локальной папки `library/core/olcrtc_local/`. `BUILD.md` про этот `replace` умалчивает — любой исполнитель, прошедший по `BUILD.md` и пересобравший AAR, получит «свежий» AAR из устаревшего исходника и не поймёт почему транспорты всё ещё ломаются. Это первичное условие всех ниже-описанных дефектов: пока в `go.mod` стоит этот `replace` и сам `olcrtc_local` не обновлён, никакая пересборка по `BUILD.md` не починит `datachannel`/`telemost`/`seichannel`.
- AAR `app/libs/libsagernetcore.aar` собран из `library/core/olcrtc_local`, который представляет собой **раннюю** ревизию olcRTC и архитектурно несовместим с эталонным сервером:
  - в локальном Go-модуле нет split-а на `internal/auth/{salutejazz,telemost,wbstream}` + `internal/engine/{goolom,livekit,salutejazz}`; вместо этого — старый `internal/provider/{jazz,telemost,wbstream}`;
  - mobile API экспортирует 7-аргументные `Start`/`StartWithTransport` без `clientID`, тогда как эталон требует 8 аргументов (`carrierName, [transportName,] roomID, clientID, keyHex, socksPort, socksUser, socksPass`); сервер при пустом `clientID` возвращает `errClientIDRequired`;
  - `internal/transport/seichannel/transport.go` — старая ревизия (фрейминг + ACK не совпадают с серверным эталоном);
  - `SetVP8Options(fps, batchSize)` в локальном модуле использует устаревший clamp `(1..32)` для batch-size вместо `(1..64)`;
  - сессия `seichannel` валидирует `SEIFPS`/`SEIBatchSize`/`SEIFragmentSize`/`SEIAckTimeoutMS`, но Kotlin-слой их не выставляет.
- В UI отсутствует поле `Room password`, а `OLCRTCBean.roomId` без password не позволяет серверу `auth/salutejazz` вызвать `joinRoom` (handshake падает с `auth.ErrRoomIDRequired: expected <roomID>:<password>`).
- Kotlin-слой в `OLCRTCExternalInstance.kt` вызывает `Mobile.startWithTransport(...)` с 7 аргументами и не передаёт device id; пользователь решил, что это значение SHALL генерироваться один раз как UUID v4 и храниться локально.
- UI экрана `OLCRTCSettingsActivity` визуально устарел (нет иконок-категорий нового стиля, нет helper text, нет визуальной группировки Connection/Advanced, нет индикации активного transport).

### Решение по `clientID` (закреплено пользователем)

- `clientID` SHALL генерироваться **один раз** при первом запуске приложения как **UUID v4** (любой стандартный API: `java.util.UUID.randomUUID()`).
- `clientID` SHALL храниться в `DataStore.configurationStore` (`PublicDatabase.kvPairDao`) под ключом `olcrtc_client_id` (новая константа в `Constants.kt`, например `Key.OLCRTC_CLIENT_ID`). При следующих запусках значение читается из стора, новое не генерируется.
- `clientID` НЕ SHALL отображаться как редактируемое поле в UI. Опционально (на усмотрение исполнителя) допускается показ значения **read-only** в разделе «Advanced» / «Debug» экрана `OLCRTCSettingsActivity` для облегчения диагностики.
- При сбросе настроек приложения (factory reset / clear preferences) — значение очищается вместе с остальной конфигурацией; при следующем старте генерируется заново. Это нормальное поведение и не требует UI-флоу «сбросить ID».

## Список задач

### C1. Обновление источника olcRTC в сборке клиента

- Покрывает clauses bugfix.md: 1.2, 1.3, 1.5, 1.6, 1.7, 2.2, 2.3, 2.5, 2.6, 2.7.
- Файлы / пакеты:
  - `library/core/go.mod` — содержит директиву `replace github.com/openlibrecommunity/olcrtc => ./olcrtc_local`, которая является ключевой точкой переключения источника.
  - `library/core/go.sum` — будет обновлён автоматически после `go mod tidy`.
  - `library/core/olcrtc_local/` (вся директория целиком — заменяется или удаляется в зависимости от выбранного варианта).
  - `library/core/main.go` — проверить, что импорт `mobile/...` остаётся валидным после изменений.
  - `library/core/build.sh` / `library/core/build.bat` — НЕ редактируются (gomobile-команда уже указывает upstream-импорт `github.com/openlibrecommunity/olcrtc/mobile`).
  - `.gitmodules` (корень `Exclave_FORK`) — затрагивается только в варианте 2.
  - `.github/workflows/*.yml` — затрагивается только в варианте 2.
  - Эталон-источник: `temp-files/olcrtc-refactor-universal-carrier/` (полный snapshot) или upstream-репозиторий `github.com/openlibrecommunity/olcrtc` с ветки/тега, соответствующего `refactor/universal-carrier`.
- Что сделать. Один из трёх взаимоисключающих вариантов (выбор фиксируется в commit message и в обновлённом `BUILD.md` — задача C9):
  1. **Вариант A — обновить локальную копию (минимально инвазивный).** Заменить содержимое `library/core/olcrtc_local/` на актуальный snapshot из `temp-files/olcrtc-refactor-universal-carrier/` (сохранить `.gitignore`, не тянуть `.git`-мусор). `replace`-директива в `go.mod` остаётся, `BUILD.md` дополняется секцией «Источник olcRTC», CI не меняется. Подходит, если контроль над snapshot нужен на стороне форка.
  2. **Вариант B — git submodule.** В `library/core/olcrtc_local/` заменить плоскую копию на git submodule, указывающий на upstream `github.com/openlibrecommunity/olcrtc` с явным branch/tag `refactor/universal-carrier`. Обновить `.gitmodules` в корне репозитория. В CI (`.github/workflows/release.yml`, `release_naive.yml`, `release_shadowquic.yml`, `debug.yml`) проверить и при необходимости добавить `with: submodules: recursive` к `actions/checkout`. `replace`-директива в `go.mod` остаётся как `=> ./olcrtc_local` (теперь ссылается на submodule).
  3. **Вариант C — убрать `replace`, тянуть upstream напрямую.** Удалить строку `github.com/openlibrecommunity/olcrtc => ./olcrtc_local` из `library/core/go.mod`, обновить версию `github.com/openlibrecommunity/olcrtc` в секции `require` до тега/коммита эталона ветки `refactor/universal-carrier`, удалить директорию `library/core/olcrtc_local/` целиком. `BUILD.md` пополняется секцией «Источник olcRTC» с пометкой «модуль тянется напрямую через `go.mod`». Самый чистый вариант, но требует, чтобы upstream был стабильным и опубликованным.
- Независимо от варианта:
  - Запустить `go mod tidy` в `library/core/`; коммит SHALL включать обновлённые `go.mod`/`go.sum`.
  - Проверить, что `library/core/main.go` не ломается (импорты остаются валидными).
- Acceptance criteria:
  - Команда `go build ./...` в `library/core/` отрабатывает без ошибок.
  - `go list -m github.com/openlibrecommunity/olcrtc` показывает источник, соответствующий выбранному варианту (локальная директория для A/B, конкретная версия для C).
  - В дереве модуля `github.com/openlibrecommunity/olcrtc` присутствуют пакеты `internal/auth/`, `internal/engine/`, `internal/carrier/builtin/` и **отсутствует** старый `internal/provider/` (как indication, что подмена прошла).
  - В `mobile/mobile.go` сигнатуры `Start` и `StartWithTransport` принимают `clientID` (8 аргументов).
  - В `mobile/mobile.go` `SetVP8Options` использует clamp `(1..120)` для FPS и `(1..64)` для batch size.
  - Старый `internal/transport/seichannel/transport.go` заменён на эталонную реализацию (объём ≈+1225 строк). `go test ./internal/transport/seichannel/...` зелёный.
  - При выборе варианта B — `git submodule status` показывает корректный commit hash; новый `git clone --recursive` в чистой среде успешно подтягивает submodule.
  - При выборе варианта C — директория `library/core/olcrtc_local/` отсутствует в дереве после коммита.

### C2. Пересборка AAR `app/libs/libsagernetcore.aar`

- Покрывает clauses bugfix.md: 1.1, 2.1 (вторая половина — клиентская часть передачи `clientID` через AAR).
- Файлы / пакеты:
  - `library/core/build.sh` (Linux/macOS) и `library/core/build.bat` (Windows).
  - `library/core/main.go` — entry point для gomobile bind.
  - `library/core/libsagernetcore-sources.jar` — обновляется тем же скриптом.
  - Артефакт: `app/libs/libsagernetcore.aar`.
- Что сделать:
  - Запустить `library/core/build.sh` (или `build.bat` на Windows) и собрать новый AAR из обновлённого `olcrtc_local` (после задачи C1).
  - Проверить, что декомпиляция AAR (через `apktool` или `javap mobile.Mobile`) показывает 8-аргументные сигнатуры:
    - `public static native void start(String, String, String, String, long, String, String);` (carrierName, roomID, clientID, keyHex, socksPort, socksUser, socksPass).
    - `public static native void startWithTransport(String, String, String, String, String, long, String, String);` (carrierName, transportName, roomID, clientID, keyHex, socksPort, socksUser, socksPass).
  - Проверить, что в AAR экспортированы все ожидаемые setter-ы: `setDebug`, `setProtector`, `setLogWriter`, `setVP8Options`, `setTransport`, `setLink`, `waitReady`, `stop`, и (если C3 решит выставлять SEI снаружи) `setSEIOptions`.
- Acceptance criteria:
  - Файл `app/libs/libsagernetcore.aar` обновлён, его размер изменился относительно предыдущей версии.
  - Сборка `./gradlew :app:assembleDebug` проходит без compile-time ошибок «cannot resolve method `Mobile.startWithTransport(...)`» — Kotlin-слой после задачи C4 умеет вызывать новый 8-аргументный API.
  - В Logcat при старте профиля (`adb logcat -s mobile`) виден лог инициализации новой архитектуры (наличие `internal/engine/...` сообщений).

### C3. Параметры SEI для `transport = seichannel`

- Покрывает clauses bugfix.md: 1.8, 2.8.
- Файлы / пакеты:
  - `library/core/olcrtc_local/mobile/mobile.go` (после C1 — содержит sweet-spot defaults).
  - `library/core/olcrtc_local/internal/app/session/session.go` (валидация `ErrSEIxRequired`).
  - `app/src/main/java/io/nekohasekai/sagernet/bg/proto/OLCRTCExternalInstance.kt` — точка вызова setter-ов.
- Что сделать:
  - Принять одно из двух решений (зафиксировать в commit message):
    1. **Дефолты в Go-уровне.** Убедиться, что в `mobile/mobile.go` есть встроенные дефолты `SEIFPS`/`SEIBatchSize`/`SEIFragmentSize`/`SEIAckTimeoutMS`, которые применяются, если Kotlin-слой не вызывает setter. Тогда в Kotlin ничего делать не нужно.
    2. **Setter в mobile API.** В `mobile/mobile.go` добавить (или подтвердить наличие) функции `SetSEIOptions(fps, batchSize, fragmentSize, ackTimeoutMs int)`, и в `OLCRTCExternalInstance.startGoClient()` вызывать её перед `startWithTransport`, когда `transport == "seichannel"`. Значения брать из дефолтов (например 30/8/900/1500), либо завести соответствующие поля в `OLCRTCBean` (опционально).
  - Если выбран путь 2 — добавить SEI-поля в `OLCRTCBean` НЕ обязательно (можно ограничиться константами в Kotlin), но если добавляются, то они идут в `Advanced`-секцию UI и попадают в Kryo-сериализацию (см. C5).
- Acceptance criteria:
  - Старт профиля с `transport = "seichannel"` для любого carrier не падает с `ErrSEIFPSRequired`/`ErrSEIBatchSizeRequired`/`ErrSEIFragmentSizeRequired`/`ErrSEIAckTimeoutMSRequired`.
  - В Logcat при старте `seichannel` виден лог с применёнными значениями SEI (хотя бы на debug-уровне).

### C4. Прокидка `clientID` (UUID v4) в Kotlin-слой и вызов 8-аргументного `Mobile.startWithTransport`

- Покрывает clauses bugfix.md: 1.1, 2.1; реализует решение пользователя по `clientID`.
- Файлы / пакеты:
  - `app/src/main/java/io/nekohasekai/sagernet/Constants.kt` — добавить `Key.OLCRTC_CLIENT_ID = "olcrtc_client_id"`.
  - `app/src/main/java/io/nekohasekai/sagernet/database/DataStore.kt` — добавить свойство:
    - `var olcrtcClientId by configurationStore.string(Key.OLCRTC_CLIENT_ID)` — глобальный (per-app), не per-profile.
  - `app/src/main/java/io/nekohasekai/sagernet/bg/proto/OLCRTCExternalInstance.kt` — точка генерации/чтения `clientID` и вызов нового API.
  - Опционально: вспомогательная функция `OlcrtcClientId.getOrCreate()` в новом файле `fmt/olcrtc/OlcrtcClientId.kt` для инкапсуляции lazy-init.
- Что сделать:
  - Реализовать lazy-инициализацию: при первом обращении к `DataStore.olcrtcClientId` (если значение пустое) сгенерировать `java.util.UUID.randomUUID().toString()` и записать в `configurationStore`. Все последующие вызовы возвращают сохранённое значение.
  - В `OLCRTCExternalInstance.startGoClient()` подставлять `clientID` четвёртым аргументом в `Mobile.startWithTransport(carrier, transport, roomId, clientID, keyHex, port, username, password)` (новая 8-аргументная сигнатура из C2).
  - Логировать сгенерированный/прочитанный `clientID` в debug-режиме под тегом `[olcrtc]` (только в `BuildConfig.DEBUG`).
  - Защитить от concurrent init (например через `synchronized` или `lazy { }` на object-уровне) — `OLCRTCExternalInstance` создаётся в IO-coroutine.
- Acceptance criteria:
  - При первом запуске нового профиля в Logcat появляется строка вида `[olcrtc] clientID generated: <uuid>` (только debug).
  - При втором/третьем запуске Logcat показывает `[olcrtc] clientID loaded: <тот же uuid>`, без новой генерации.
  - `Mobile.startWithTransport` вызывается с непустым 4-м аргументом, на стороне сервера в логах нет `errClientIDRequired`.
  - `clientID` сохраняется между перезапусками приложения (проверяется руками: kill app → cold start → тот же UUID).
  - `clientID` НЕ виден как редактируемое поле в UI (если выбрано отображение в Advanced — оно read-only, см. C7).

### C5. Поле `Room password` для carrier `jazz` (включая UI, бин и сериализацию в Go)

- Покрывает clauses bugfix.md: 1.4, 1.9, 1.10, 2.4, 2.9, 2.10.
- Файлы / пакеты:
  - `app/src/main/res/xml/olcrtc_preferences.xml` — добавить `EditTextPreference` для нового поля.
  - `app/src/main/res/values/strings.xml` (и переводы `values-*/strings.xml`) — строки `olcrtc_room_password`, `olcrtc_room_password_summary`, hint и т.п.
  - `app/src/main/java/io/nekohasekai/sagernet/Constants.kt` — `Key.SERVER_OLCRTC_ROOM_PASSWORD = "serverOlcrtcRoomPassword"`.
  - `app/src/main/java/io/nekohasekai/sagernet/database/DataStore.kt` — `var serverOlcrtcRoomPassword by profileCacheStore.string(Key.SERVER_OLCRTC_ROOM_PASSWORD)`.
  - `app/src/main/java/io/nekohasekai/sagernet/fmt/olcrtc/OLCRTCBean.java` — поле `public String roomPassword;`, миграция версии Kryo (см. ниже).
  - `app/src/main/java/io/nekohasekai/sagernet/ui/profile/OLCRTCSettingsActivity.kt` — `init/serialize` + динамическое скрытие/отключение поля при `provider != jazz`.
  - `app/src/main/java/io/nekohasekai/sagernet/bg/proto/OLCRTCExternalInstance.kt` — формирование `RoomURL` для Go-уровня.
- Что сделать:
  - В `olcrtc_preferences.xml` добавить новый `EditTextPreference` с ключом `serverOlcrtcRoomPassword`, иконкой типа `ic_settings_password` (или новой), `dialogLayout="@layout/layout_password_dialog"`. Семантически отличить от существующего `serverOlcrtcKeyHex` — другой иконкой и другим summary («Room password for SaluteJazz» vs «Pre-shared key (hex)»).
  - В `OLCRTCBean.java`:
    - добавить `public String roomPassword;`;
    - поднять Kryo-версию: в `serialize` писать `output.writeInt(3)` (вместо текущих `2`) и добавить `output.writeString(roomPassword);` в конец;
    - в `deserialize` добавить блок `if (version >= 3) roomPassword = input.readString(); else roomPassword = "";` — старые блобы (`version <= 2`) читаются как `roomPassword = ""`;
    - в `initializeDefaultValues()` добавить `if (roomPassword == null) roomPassword = "";`.
  - В `OLCRTCSettingsActivity`:
    - в `init` копировать `DataStore.serverOlcrtcRoomPassword = roomPassword`;
    - в `serialize` обратное копирование;
    - подписаться на изменения `serverOlcrtcProvider` (через `OnPreferenceChangeListener` или вручную в `onPreferenceTreeClick`) и менять `isVisible`/`isEnabled` у поля `serverOlcrtcRoomPassword` так, чтобы оно показывалось только при `provider == jazz`.
    - При первом open экрана выставлять видимость в соответствии с текущим значением `provider`.
  - В `OLCRTCExternalInstance.startGoClient()` собирать аргумент для Go:
    - если `bean.provider == "jazz"` и `bean.roomPassword.isNotEmpty()` — передавать в Go `roomId = "${bean.roomId}:${bean.roomPassword}"`;
    - иначе — передавать `bean.roomId` как сейчас.
    - Важно: подмена идёт на уровне Kotlin перед вызовом `Mobile.startWithTransport`, чтобы Go-уровень получил `RoomURL` в формате, который ждёт `auth/salutejazz`.
- Acceptance criteria:
  - Открытие экрана `OLCRTCSettingsActivity` для нового профиля с `provider = jazz` показывает два разных password-поля: `Room password` (новое) и `Pre-shared key (hex)` (существующее).
  - Смена `provider` с `jazz` на `telemost` в живом UI скрывает (или disable-ит) `Room password` без перезапуска экрана.
  - Импорт старого Kryo-блоба профиля (со схемой v2) проходит без исключений и `roomPassword` приходит пустой строкой.
  - Старт профиля с `provider = jazz`, заполненными `Room ID = abc` и `Room password = secret` приводит к Go-вызову с `roomID = "abc:secret"` (проверяется в Logcat сервера или клиента — поиск `auth/salutejazz` без `ErrRoomIDRequired`).
  - Старт профиля с `provider = telemost` и заполненным `Room password` всё равно работает (Kotlin не подмешивает `:password` для не-jazz провайдеров).

### C6. Опциональный query-параметр `room_password` в URI `olcrtc://...`

- Покрывает clauses bugfix.md: 3.3, 2.4 (часть про шаринг профиля).
- Файлы / пакеты:
  - `app/src/main/java/io/nekohasekai/sagernet/fmt/olcrtc/OLCRTCFmt.kt` — `parseOLCRTC` и `OLCRTCBean.toUri()`.
  - `app/src/main/java/io/nekohasekai/sagernet/fmt/olcrtc/parseOLCRTCJson` — JSON-вариант (`room_password` в payload).
- Что сделать:
  - В `parseOLCRTC(url)`: после существующих `link.queryParameter("...")` добавить `roomPassword = link.queryParameter("room_password") ?: ""`. Параметр **опциональный**: его отсутствие не SHALL ломать парсинг старых ссылок.
  - В `OLCRTCBean.toUri()`: добавлять `addQueryParameter("room_password", roomPassword)` только если `roomPassword.isNotEmpty()` **и** `provider == OLCRTCBean.PROVIDER_JAZZ`. В остальных случаях параметр в URI не появляется (для совместимости со старыми импортами).
  - В `parseOLCRTCJson(text)`: добавить `roomPassword = json.optString("room_password", "")`.
  - В `validate()` НЕ требовать обязательность `roomPassword` (это поле опциональное даже для `jazz`, поскольку сервер в дефолтном режиме создаст комнату сам, если `RoomURL` пустой; принудительная проверка делается уже на сервере и не должна дублироваться в клиенте).
- Acceptance criteria:
  - Существующая ссылка `olcrtc://room@room:1/<roomId>?key=<hex>` (без `room_password`) парсится без ошибок и `roomPassword` приходит `""`.
  - Профиль `provider = jazz, roomId = abc, roomPassword = secret` после `toUri()` содержит `&room_password=secret`. Профиль с пустым `roomPassword` или `provider != jazz` НЕ должен иметь этот query-параметр в URI.
  - Импорт `olcrtc://...&room_password=secret` обратно в `OLCRTCBean` восстанавливает значение `roomPassword`.

### C7. Material 3 редизайн `OLCRTCSettingsActivity`

- Покрывает clauses bugfix.md: 1.11, 2.11.
- Файлы / пакеты:
  - `app/src/main/res/xml/olcrtc_preferences.xml` — структура `PreferenceCategory`, иконки, summary.
  - `app/src/main/res/values/strings.xml` (+ переводы) — все строки helper text / hint.
  - `app/src/main/res/drawable/` — иконки `ic_olcrtc_room`, `ic_olcrtc_room_password`, `ic_olcrtc_provider`, `ic_olcrtc_transport` (или переиспользование существующих `ic_*` Material-иконок).
  - `app/src/main/java/io/nekohasekai/sagernet/ui/profile/OLCRTCSettingsActivity.kt` — индикация активного transport в summary.
- Что сделать:
  - Переразбить `olcrtc_preferences.xml` на 2 (или 3) `PreferenceCategory`:
    - `Connection` — `provider`, `transport`, `roomId`, `roomPassword` (только для `jazz`), `keyHex`.
    - `Advanced` — `dnsServer`, `vp8Fps`, `vp8BatchSize`, `keepaliveIntervalSec`. Опционально — read-only `clientID` (см. ниже).
  - На каждое ключевое поле выставить `app:icon` (из существующих иконок проекта или добавить недостающие в `drawable`). Иконки SHALL визуально отличать `keyHex` (ключ-замок) и `roomPassword` (комната с замком), чтобы не путать.
  - Для каждого `EditTextPreference` использовать `app:useSimpleSummaryProvider="true"` (где это уместно) или явный `summaryProvider` в Activity, чтобы текущее значение / hint было видно прямо в строке поля.
  - Добавить helper text / `summary` с примерами:
    - `Room ID` — пример `abcd-1234` или ссылка на Telemost.
    - `Room password` — пример `••••••` (подсказка про SaluteJazz).
    - `Pre-shared key (hex)` — `64 hex characters`.
    - `DNS server` — `1.1.1.1:53`.
  - В `OLCRTCSettingsActivity.kt` обновлять summary поля `transport` так, чтобы оно отображало активный transport читабельным текстом (например, `Datachannel (default)`, `VP8 channel`, `SEI channel`, `Video channel`).
  - **Опционально**: в категории `Advanced` показать read-only поле `Client ID` со значением из `DataStore.olcrtcClientId` (через `Preference` с `summary = clientId`, `setEnabled(false)`). Это упростит диагностику. Поле не имеет dialog'а для редактирования.
  - Material 3 совместимость: использовать актуальные drawable / theme проекта; если в проекте уже есть Material 3-настроенный `PreferenceTheme` — переиспользовать его. Не вводить ad-hoc `style="..."` без соответствия общему UI.
- Acceptance criteria:
  - Скриншот экрана для `provider = jazz` показывает: иконку слева у каждого поля, заголовок категории «Connection», все поля видны включая `Room password`, активный transport отображён в summary читабельно.
  - Скриншот экрана для `provider = telemost` показывает: то же самое, но без `Room password`.
  - Скриншот категории `Advanced` показывает: 4 (или 5 — с `Client ID`) поля, видимый divider Material 3, summary с примерами значений.
  - Мануальный тест: переключение transport в `vp8channel` обновляет summary без перезапуска экрана.
  - Read-only `Client ID` (если реализован) не открывает dialog при тапе и не редактируется.

### C8. Регрессионная проверка

- Покрывает clauses bugfix.md: 3.1, 3.2, 3.3, 3.4, 3.5, 3.8.
- Файлы / пакеты: тестовые сценарии вручную / через юнит-тесты.
- Что сделать:
  - Прогнать ручной smoke по сценариям, описанным в bugfix.md Section 3:
    - Запуск vp8channel × {telemost, jazz, wbstream} — соединение и keepalive работают.
    - Запуск videochannel × {telemost, jazz, wbstream} — видеопоток (qrcode/tile) идёт.
    - Импорт старого `olcrtc://` URI (без `room_password`) — профиль создаётся с `roomPassword = ""`.
    - Загрузка старого Kryo-профиля (Kryo версии 2) — `roomPassword` дефолтится в `""`, остальные поля целы.
    - Tasker-интеграция и `ProxyEntity` CRUD — без изменений.
    - keepalive (`DataStore.serverOlcrtcKeepaliveInterval`) триггерит reconnect через `onSessionLost` после намеренного blackhole-теста (например, `iptables -A OUTPUT -p tcp --dport <serverPort> -j DROP` на стенде).
  - Зафиксировать результаты в commit message / PR description в виде чеклиста.
- Acceptance criteria:
  - Все пункты smoke зелёные. Если что-то падает — оформить в виде issue со ссылкой на bugfix.md.

### C9. Документирование источника olcRTC в `BUILD.md`

- Покрывает clauses bugfix.md: дополнительный контекст к 1.1–1.8, 2.1–2.8 (сборочная ловушка `replace` в `go.mod`).
- Файлы / пакеты:
  - `BUILD.md` (корень `Exclave_FORK`).
- Что сделать:
  - Добавить в `BUILD.md` новую секцию (например, между «3. Сборка AAR» и «4. Сборка APK») с заголовком «Источник olcRTC» / «olcRTC source», в которой явно зафиксировано:
    - что модуль `github.com/openlibrecommunity/olcrtc` берётся **не** напрямую из публичного репозитория, а через директиву `replace` в `library/core/go.mod` (строка `github.com/openlibrecommunity/olcrtc => ./olcrtc_local`);
    - какой из вариантов A/B/C из задачи C1 выбран в текущей сборке (нужный вариант указать после реализации C1);
    - предупреждение: «**Любая пересборка `library/core/build.sh` без обновления источника olcRTC даст AAR из устаревшего кода и НЕ починит транспорты `datachannel` / `telemost` / `seichannel`**»;
    - инструкция «как обновить источник olcRTC» — короткая команда (напр., `git submodule update --remote library/core/olcrtc_local` для варианта B, или `go get github.com/openlibrecommunity/olcrtc@<tag>` для варианта C, или ручное копирование snapshot-а для варианта A).
  - Если в C1 выбран вариант C (без `replace`), также убрать из `BUILD.md` любые упоминания каталога `olcrtc_local`, если они появятся.
  - Не править существующие секции BUILD.md без необходимости (обновляем только то, что напрямую относится к источнику olcRTC). Замечания про Go 1.25.9 / NDK / JDK 21 остаются как есть.
- Acceptance criteria:
  - В `BUILD.md` есть секция «Источник olcRTC» (или эквивалентного смысла), явно объясняющая директиву `replace` и выбранный вариант.
  - Текст содержит предупреждение про невидимую перезапись источника через `replace`.
  - После прочтения `BUILD.md` новый исполнитель понимает, какой каталог (или какой тег upstream) нужно обновлять, чтобы изменения в Go-уровне попали в AAR.
  - Никакие другие секции `BUILD.md` не изменены сверх того, что требует выбранный вариант C1.

### C10. Корректирующая задача — `clientID` приходит из URI/QR, а не генерируется на клиенте (поверх C4/C6)

> **ВАЖНО для исполнителя.** На момент создания этой задачи C1–C9 уже реализованы в коде. C10 переписывает поведение C4 и расширяет C6. Все изменения выполняются **строго поверх** существующих реализаций; задачи C1, C2, C3, C5, C7, C8, C9 **не затрагиваются**. Предусмотрена миграция: профили, импортированные старыми клиентами без `client_id` в URI, должны продолжать работать на чтение, но при первом ручном save/edit — переключаться на новую модель.

- Покрывает clauses bugfix.md: 1.1, 2.1; пересмотр решения «генерировать UUID на клиенте при первом старте» в пользу схемы «server-issued, передаётся через URI».
- **Корневая причина пересмотра:**
  - В сервере (см. `requirements-server.md` S8) `clientID` теперь генерируется в админке и записывается в env-файл инстанса как `OLCRTC_CLIENT_ID`. URI экспорта инстанса формируется как `olcrtc://<carrier>@room/<roomID>?key=...&transport=...&client_id=<uuid>...`.
  - На сервере `clientID` используется в `internal/transport/vp8channel/transport.go` для генерации `bindingToken := fnv32(clientID)`. VP8-кадры с другим binding token отбрасываются. Поэтому `clientID` на сервере и на клиенте **обязан совпадать** для одной сессии — иначе VP8-канал не работает.
  - Если клиент будет генерировать UUID локально (как описано в C4), то его `clientID` никогда не совпадёт с серверным, и `vp8channel` сломается на стороне фильтрации binding token. Это **подтверждённое поведение в коде** (`TestHandleIncomingFrameIgnoresForeignBindingToken`).
  - Из этого следует: `clientID` — это атрибут профиля (per-OLCRTCBean), а не глобальное per-app значение. Каждый импортированный URI несёт свой `clientID`, и клиент SHALL использовать именно его.
- Файлы / пакеты (что меняется):
  - `app/src/main/java/io/nekohasekai/sagernet/Constants.kt` — **удалить** константу `Key.OLCRTC_CLIENT_ID = "olcrtc_client_id"` (она была глобальной); добавить `Key.SERVER_OLCRTC_CLIENT_ID = "serverOlcrtcClientId"` для per-profile поля.
  - `app/src/main/java/io/nekohasekai/sagernet/database/DataStore.kt` — **удалить** свойство `var olcrtcClientId by configurationStore.string(...)`; **добавить** `var serverOlcrtcClientId by profileCacheStore.string(Key.SERVER_OLCRTC_CLIENT_ID)` (per-profile, по аналогии с `serverOlcrtcRoomPassword` из C5).
  - `app/src/main/java/io/nekohasekai/sagernet/fmt/olcrtc/OLCRTCBean.java` — добавить `public String clientId;`. Поднять Kryo-версию с `3` (после C5) до `4`: в `serialize` писать `output.writeInt(4)` и добавить `output.writeString(clientId);`. В `deserialize` добавить `if (version >= 4) clientId = input.readString(); else clientId = "";` — старые блобы (v1/v2/v3) читаются с `clientId = ""`. В `initializeDefaultValues()` — `if (clientId == null) clientId = "";`.
  - `app/src/main/java/io/nekohasekai/sagernet/fmt/olcrtc/OlcrtcClientId.kt` — **удалить файл целиком**, если он был создан в C4. Lazy-генерация через `getOrCreate()` больше не нужна.
  - `app/src/main/java/io/nekohasekai/sagernet/fmt/olcrtc/OLCRTCFmt.kt`:
    - В `parseOLCRTC(url)` — добавить `clientId = link.queryParameter("client_id") ?: ""`. Параметр **опциональный** для обратной совместимости со старыми ссылками.
    - В `OLCRTCBean.toUri()` — добавлять `addQueryParameter("client_id", clientId)` только если `clientId.isNotEmpty()`. Не делать обязательным, чтобы не ломать reshare URI без `client_id` (например, если пользователь экспортирует пустой профиль).
    - В `parseOLCRTCJson(text)` — добавить `clientId = json.optString("client_id", "")`.
  - `app/src/main/java/io/nekohasekai/sagernet/bg/proto/OLCRTCExternalInstance.kt`:
    - **Удалить** обращения к `DataStore.olcrtcClientId` и lazy-init через `OlcrtcClientId.getOrCreate()` (всё, что появилось в C4).
    - В `startGoClient()` использовать **`bean.clientId`** четвёртым аргументом в `Mobile.startWithTransport(carrier, transport, roomId, bean.clientId, keyHex, port, username, password)`.
    - Если `bean.clientId.isEmpty()` (старый профиль, импортированный до этой задачи) — **fallback**: показать пользователю явную ошибку через `onSessionLost(SessionError.MissingClientId)` или эквивалент, с сообщением «Profile is missing Client ID. Re-import the URI from the server admin panel». Не генерировать UUID на лету — это сломает `bindingToken`.
  - `app/src/main/java/io/nekohasekai/sagernet/ui/profile/OLCRTCSettingsActivity.kt` — в `init` копировать `DataStore.serverOlcrtcClientId = clientId`; в `serialize` обратное копирование. Поле SHALL отображаться как **read-only** в секции «Connection» (или «Identity»), с возможностью copy-to-clipboard. Редактирование запрещено: запретить через `setEnabled(false)` или через preference без dialog.
  - `app/src/main/res/xml/olcrtc_preferences.xml` — добавить read-only `Preference` (или `EditTextPreference` с `android:enabled="false"`) с key `serverOlcrtcClientId`, иконкой типа `ic_olcrtc_client_id` (или существующей `ic_perm_identity`), summary показывает текущее значение.
  - `app/src/main/res/values/strings.xml` (+ переводы) — строки `olcrtc_client_id`, `olcrtc_client_id_summary` («Generated by server, imported via URI/QR»), `olcrtc_client_id_missing` (для error-toast).
  - **Удалить** опциональный read-only `Client ID` блок в категории «Advanced», если он был добавлен в C7 — `clientId` теперь принадлежит секции «Connection», как и `roomId`.
- Что сделать (пошаговый протокол для исполнителя):
  1. Прочитать текущую реализацию C4 в коде клиента (поиск `Key.OLCRTC_CLIENT_ID`, `olcrtcClientId`, `OlcrtcClientId.getOrCreate`). Зафиксировать список затронутых файлов.
  2. Удалить глобальное `clientID`-хранилище (Constants, DataStore, OlcrtcClientId.kt). Все коммиты делать атомарно, чтобы сборка не ломалась — обычно это один коммит «remove global clientID».
  3. Добавить per-profile поле `clientId` в `OLCRTCBean` с миграцией Kryo v3 → v4. Старые блобы читаются с пустой строкой.
  4. Дополнить `parseOLCRTC` / `toUri` / `parseOLCRTCJson` опциональным `client_id` query-параметром.
  5. Перевести `OLCRTCExternalInstance.startGoClient` на `bean.clientId`. Логику lazy-генерации удалить.
  6. Добавить fallback-ошибку «Missing Client ID» при пустом `bean.clientId`, чтобы пользователь увидел проблему сразу, а не через зависший VP8-канал.
  7. Перенести отображение `Client ID` из секции «Advanced» в «Connection» как read-only поле с copy-to-clipboard. Если в C7 такого поля не было — добавить заново.
  8. Прогнать smoke C8 заново на всех transport × carrier комбинациях с **новым** URI, который включает `client_id`. Дополнительно проверить:
     - старый URI без `client_id` импортируется без падений (профиль создаётся с пустым `clientId`);
     - попытка старта такого профиля показывает явную ошибку «Missing Client ID», а не зависает;
     - `vp8channel` для пары `(jazz, vp8channel)` и `(telemost, vp8channel)` теперь работает (binding token совпадает).
- Acceptance criteria:
  - Глобальное хранилище `Key.OLCRTC_CLIENT_ID` / `DataStore.olcrtcClientId` / `OlcrtcClientId.getOrCreate()` отсутствует в коде после C10 (`grep -r "OLCRTC_CLIENT_ID" app/src/main/java` не находит).
  - В `OLCRTCBean.java` есть поле `public String clientId`. Kryo-сериализация поднята до v4. Импорт старого блоба v3 проходит без исключений и `clientId` приходит пустой строкой.
  - `parseOLCRTC` корректно извлекает `client_id` из URI вида `olcrtc://...&client_id=<uuid>`. Отсутствие параметра в URI не ломает парсинг.
  - `OLCRTCBean.toUri()` для профиля с непустым `clientId` содержит `&client_id=<uuid>` в query-string. Для пустого `clientId` параметр не добавляется.
  - При старте профиля через `OLCRTCExternalInstance.startGoClient`, `Mobile.startWithTransport` вызывается с `bean.clientId` четвёртым аргументом. На сервере (если он на той же машине, см. S8 strict-AuthHook) handshake проходит без `device mismatch`. На клиенте `bindingToken` для `vp8channel` совпадает с серверным.
  - Старт профиля с пустым `bean.clientId` (импортированного по старой схеме) НЕ генерирует UUID на лету; вместо этого пользователь видит явное сообщение «Profile is missing Client ID. Re-import the URI from the server admin panel».
  - В `OLCRTCSettingsActivity` поле `Client ID` показано в секции «Connection» как read-only с возможностью copy-to-clipboard. Дублирующее поле в «Advanced» (если было в C7) удалено.
  - Smoke C8 зелёный для всех 12 пар carrier × transport на свежем URI с `client_id=`. Дополнительно: смок «старый URI без client_id» показывает понятную ошибку, а не зависание.

#### Промпт для AI-исполнителя задачи C10

Скопируй текст ниже целиком и подай AI-агенту как самостоятельный prompt. Промпт намеренно строгий — он запрещает трогать другие задачи и требует от агента работать только в рамках C10.

```
ЗАДАЧА: реализовать корректирующую задачу C10 из файла .kiro/specs/webrtc-transports-fix/requirements-client.md.

КОНТЕКСТ:
- Это форк Android-клиента Exclave_FORK. Задачи C1..C9 уже реализованы в коде. Не повторяй их и не правь файлы, которые они затронули, кроме случаев, явно требуемых описанием C10.
- Серверная сторона (см. requirements-server.md S8) уже умеет: генерировать OLCRTC_CLIENT_ID на сервере, отдавать его в JSON и включать в URI экспорта как query-параметр client_id. Серверные правки делать НЕ надо.
- Корневая причина: clientID на клиенте и на сервере должен совпадать, иначе vp8channel отбрасывает кадры по binding token.

ТВОЯ РАБОТА:
1. Прочти файл .kiro/specs/webrtc-transports-fix/requirements-client.md, секцию C10 — это твоё единственное ТЗ.
2. Прочти текущую реализацию C4 в коде клиента: найди и зафиксируй все упоминания Key.OLCRTC_CLIENT_ID, olcrtcClientId, OlcrtcClientId.getOrCreate (если есть).
3. Удали глобальное per-app хранилище clientID:
   - Удали константу Key.OLCRTC_CLIENT_ID из Constants.kt.
   - Удали свойство var olcrtcClientId из DataStore.kt.
   - Удали файл fmt/olcrtc/OlcrtcClientId.kt, если он существует.
4. Добавь per-profile поле:
   - Новая константа Key.SERVER_OLCRTC_CLIENT_ID = "serverOlcrtcClientId" в Constants.kt.
   - Новое свойство var serverOlcrtcClientId by profileCacheStore.string(...) в DataStore.kt.
   - Поле public String clientId в OLCRTCBean.java.
5. Подними Kryo-версию OLCRTCBean с 3 на 4. В serialize пиши output.writeInt(4) и добавь output.writeString(clientId). В deserialize: if (version >= 4) clientId = input.readString(); else clientId = "". В initializeDefaultValues добавь null-check.
6. В fmt/olcrtc/OLCRTCFmt.kt:
   - parseOLCRTC: добавь clientId = link.queryParameter("client_id") ?: "" (после существующих queryParameter-вызовов).
   - OLCRTCBean.toUri(): добавь addQueryParameter("client_id", clientId) только при clientId.isNotEmpty().
   - parseOLCRTCJson: добавь clientId = json.optString("client_id", "").
7. В bg/proto/OLCRTCExternalInstance.kt:
   - Удали все обращения к DataStore.olcrtcClientId и OlcrtcClientId.getOrCreate.
   - В startGoClient() передавай bean.clientId четвёртым аргументом в Mobile.startWithTransport.
   - Если bean.clientId пустой — НЕ генерируй UUID. Вместо этого вызови onSessionLost (или эквивалент) с сообщением "Profile is missing Client ID. Re-import the URI from the server admin panel". Эту ветку оформи отдельным методом или ранним return со внятным комментарием.
8. В ui/profile/OLCRTCSettingsActivity.kt:
   - Скопируй DataStore.serverOlcrtcClientId = clientId в init.
   - Скопируй обратно в serialize.
   - Перенеси (или добавь) read-only поле Client ID в категорию Connection (не Advanced). Реализуй copy-to-clipboard по тапу.
9. В res/xml/olcrtc_preferences.xml:
   - Добавь read-only Preference с key="serverOlcrtcClientId" в категорию Connection.
   - Если в Advanced было дублирующее поле Client ID (от C7) — удали его.
10. В res/values/strings.xml (и переводах) добавь строки olcrtc_client_id, olcrtc_client_id_summary, olcrtc_client_id_missing.

ПРОВЕРКА:
- Запусти ./gradlew :app:assembleOssDebug — должен собраться без ошибок.
- Запусти grep -rn "OLCRTC_CLIENT_ID\|olcrtcClientId\|OlcrtcClientId\.getOrCreate" app/src/main/java — должен вернуть НОЛЬ совпадений (всё перенесено в per-profile).
- Запусти grep -rn "serverOlcrtcClientId\|client_id\|bean\.clientId" app/src/main/java — должно быть несколько совпадений в DataStore, Constants, OLCRTCBean, OLCRTCFmt, OLCRTCExternalInstance, OLCRTCSettingsActivity.
- Прогон ручного smoke (см. acceptance criteria C10) делает пользователь. От тебя — только код и фактологическое подтверждение, что сборка зелёная.

ОГРАНИЧЕНИЯ:
- НЕ трогай задачи C1, C2, C3, C5, C6, C7, C8, C9.
- НЕ трогай серверные файлы (temp-files/Olcrtc_manager-refactor-universal-carrier-fork/...).
- НЕ генерируй UUID на клиенте — это зашитая в задачу архитектурная инвариант.
- НЕ ломай Kryo-сериализацию: миграция строго v3 → v4, старые v1/v2/v3 блобы должны читаться без исключений.
- Сохраняй стиль существующего кода (kotlin coding conventions, java public fields для OLCRTCBean — как в текущем форке).
- В каждом коммите указывай "[C10]" в заголовке для трассируемости.

ЕСЛИ ВОЗНИКНУТ ВОПРОСЫ:
- Если в коде уже нет реализации C4 (никаких Key.OLCRTC_CLIENT_ID и т.д.) — это значит C4 был реализован по новой модели изначально или ещё не реализован. В этом случае пропусти шаги 2-3 (удаление) и переходи к шагу 4 (добавление per-profile поля). Зафиксируй это в commit message: "C4 was not yet applied; C10 implements final shape directly".
- Если в коде есть и старая (глобальная), и новая (per-profile) реализации одновременно — оставь только per-profile, удали глобальную. В commit message: "Removed legacy global clientID storage in favor of per-profile (C10)".
- Все остальные неоднозначности решай в пользу описания C10 в requirements-client.md, секция Acceptance criteria.
```

## Out of scope

- Любые правки на серверной стороне (`temp-files/Olcrtc_manager-refactor-universal-carrier-fork/...`). Все серверные работы — в `requirements-server.md`.
- Перевод проекта на Material 3 целиком (только экран `OLCRTCSettingsActivity`).
- Отдельный экран «настройки olcRTC ядра» вне профиля (общесистемный) — `clientID` хранится глобально, но UI для его управления **не делаем**.
- Рефакторинг существующих профилей других протоколов (Shadowsocks, Trojan, и т.д.).
- Добавление новых carrier / transport за пределами уже существующих 3×4.
- Изменение схемы `olcrtc://...` сверх добавления опционального `room_password` (никаких новых обязательных полей).
- Принудительная миграция старых профилей (Kryo v2 → v3 без `roomPassword`) — миграция автоматическая через `if (version >= 3) ... else default`.
- Полный рефакторинг `BUILD.md` (изменения только в части, относящейся к источнику olcRTC; всё остальное — Go 1.25.9, NDK, JDK 21, Known Issues — остаётся как есть).

## Регрессионные гарантии

Из `bugfix.md` Section 3, отфильтровано до клиентской части:

- (3.1) Профиль с `transport = vp8channel` для любого carrier продолжает работать после фикса. Параметры `vp8Fps` и `vp8BatchSize` SHALL сохраняться, текущие дефолты `60`/`8` остаются. Если в C1 поднялся clamp с `(1..32)` до `(1..64)`, то значения от 9 до 32 SHALL продолжать работать как раньше; значения 33..64 — новый допустимый диапазон.
- (3.2) Профиль с `transport = videochannel` для любого carrier продолжает передавать видеопоток с теми же параметрами `VideoWidth`/`Height`/`FPS`/`Bitrate`/`HW`/`Codec`. В этом ТЗ их новых setter-ов не вводим.
- (3.3) Парсер `parseOLCRTC` и `OLCRTCBean.toUri()` SHALL сохранять обратную совместимость со старыми ссылками без `room_password`. Все существующие query-параметры (`key`, `dns`, `transport`, `vp8_fps`, `vp8_batch`, `keepalive`) парсятся и формируются как сейчас.
- (3.4) Keepalive-логика в `OLCRTCExternalInstance.kt` (TCP-проверка через локальный SOCKS до `1.1.1.1:53` каждые `keepaliveIntervalSec` секунд) SHALL CONTINUE работать после правок C4. Reconnect через `onSessionLost` SHALL триггериться при ошибках сети и SHALL проходить с обновлённым `clientID` (тот же UUID, не генерируется заново).
- (3.5) Kryo-сериализация: новая версия `3` SHALL читать старые блобы версий `1` и `2` без потерь полей `provider/transport/roomId/keyHex/dnsServer/vp8Fps/vp8BatchSize/keepaliveIntervalSec`. Поле `roomPassword` для старых блобов SHALL заполняться пустой строкой.
- (3.8) Tasker, `ConfigurationFragment`, `ProxyEntity` CRUD SHALL продолжать взаимодействовать с `OLCRTCBean` через тот же интерфейс (`init/serialize`, `toUri`, `applyFeatureSettings`). Подписи методов и набор полей **расширяются** (добавляется `roomPassword`), но не ломаются.
- `clientID` SHALL быть стабильным между перезапусками приложения (хранится в `configurationStore`). Перезапуск Activity / процесса / устройства не SHALL приводить к новому UUID.
- Существующий путь `wbstream + datachannel` (если работает в текущей сборке) SHALL CONTINUE работать после фикса; в частности, передача `clientID` SHALL не ломать wbstream-сценарий.
- Команда `library/core/build.sh` (и `build.bat`) SHALL продолжать собирать AAR одной командой без необходимости передавать переменные окружения сверх тех, что уже описаны в `BUILD.md`. Если C1 выбрал вариант C (удаление `replace`), `gomobile bind` SHALL уметь скачать `github.com/openlibrecommunity/olcrtc` напрямую с заданной версией, и сборка SHALL не требовать наличия `library/core/olcrtc_local/`.
