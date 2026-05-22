# Bugfix Requirements Document

## Introduction

В Android-клиенте `Exclave_FORK` (текущая директория) перестали работать транспорты `datachannel` и `telemost` (комбинация carrier `telemost` + любой транспорт), а также под вопросом находится работоспособность транспорта `seichannel`. При этом эталонный клиент в `temp-files/olcrtc-refactor-universal-carrier` и форк-сервер в `temp-files/Olcrtc_manager-refactor-universal-carrier-fork` (далее — «эталонные репозитории») собраны из новой ветки `refactor/universal-carrier` и используют переработанный слой carrier/auth/engine.

Корневая причина установлена сравнением исходников: в клиенте `Exclave_FORK` AAR-библиотека `app/libs/libsagernetcore.aar` собрана из локального Go-модуля `library/core/olcrtc_local`, который представляет собой более **раннюю** ревизию проекта olcRTC, чем код в `temp-files/olcrtc-refactor-universal-carrier`. Расхождения архитектурно несовместимы:

1. **Нет split на `auth/` + `engine/`.** В `library/core/olcrtc_local/internal/` отсутствует пакет `internal/auth/*` и пакет `internal/engine/*`. Вместо этого там пакет `internal/provider/{jazz,telemost,wbstream}` с соединёнными в одно целое API+медиа-стеком (старая модель). Сервер же собран по новой модели с `internal/auth/{salutejazz,telemost,wbstream}` + `internal/engine/{goolom,livekit,salutejazz}` и регистрацией carriers через `internal/carrier/builtin/register.go`. Старый клиент не умеет говорить с новым сервером ни на signaling-, ни на media-уровне. Это автоматически ломает все carrier-сценарии (`telemost`, `jazz`/`salutejazz`), и эффект особенно заметен на тех транспортах, чьи bring-up-шаги завязаны на carrier-канал — это `datachannel` (signaling вмонтирован в саму DataChannel-сессию) и связка `telemost` с любым транспортом.
2. **Сигнатура mobile API в AAR старее, чем у эталона.** Декомпиляция `app/libs/libsagernetcore.aar`:
   ```
   public static native void start(String, String, String, long, String, String);
   public static native void startWithTransport(String, String, String, String, long, String, String);
   ```
   В эталонных Go-репозиториях обе функции принимают **дополнительный** аргумент `clientID` сразу после `carrierName`:
   ```
   Start(carrierName, roomID, clientID, keyHex, socksPort, socksUser, socksPass)
   StartWithTransport(carrierName, transportName, roomID, clientID, keyHex, socksPort, socksUser, socksPass)
   ```
   Это означает, что AAR не валидирует/не передаёт `clientID`, который сервер требует через `validateStartArgs → errClientIDRequired`. После апдейта сервера на новую ветку любой запуск со старым AAR будет либо отвергнут на стороне сервера (нет device id), либо получит «несовпадающий device id» в handshake.
3. **Mobile API в `library/core/olcrtc_local/mobile/mobile.go` не содержит `Check`/`Ping`/`SetDebug`-в-полном-объёме**, тогда как новый Kotlin-слой (см. `OLCRTCExternalInstance.kt`) ожидает `setDebug`, `setProtector`, `setLogWriter`, `setVP8Options`, `setTransport`, `setLink`, `startWithTransport`, `waitReady`, `stop`. Это совпадает только частично: `setDebug` в локальном модуле есть, но `setVP8Options` использует старый clamp `(1..32)` для batch-size вместо `(1..64)`. Передача значения по умолчанию `8` работает, но любое значение `>32` молча округлится — это потенциальный, но не главный, источник дефектов.
4. **Устаревший Go-модуль `library/core/olcrtc_local/internal/transport/seichannel/transport.go`** отличается от эталонного на ~1225 строк (полная переработка фрейминга / SEI NALU). Если сервер обновлён на новую версию, старый клиент не сможет договориться по SEI-кадрам.
5. **`Provider` (`PROVIDER_TELEMOST`/`PROVIDER_JAZZ`) клиент в Kotlin отдаёт в Go как «`telemost`»/«`jazz`».** Эталонный сервер регистрирует carrier `jazz` через `authSaluteJazz.Provider{}` (т.е. `jazz` — алиас для `salutejazz`), и для него в Kotlin-UI должно передаваться поле `RoomURL` в формате `<roomID>:<password>` (см. `auth/salutejazz/salutejazz.go` → `Issue`: `roomID, password, hasPassword := strings.Cut(roomRef, ":")` ). Сейчас в UI клиента есть только одно поле `Room ID` и только одно поле `Pre-shared key`. Поле «пароль для salutejazz» отсутствует, поэтому сервер не сможет подключиться к комнате `salutejazz` без создания новой комнаты. Это покрывает второй пункт ТЗ пользователя — UI-доработка.

Дополнительно пользователь просит:
- Доработку UI: добавить отдельные поля «ID комнаты» и «Пароль комнаты» для transport/provider `salutejazz` (в UI он называется `jazz`), которые сервер использует при подключении к существующей комнате SaluteJazz.
- Общее обновление визуального оформления панели `OLCRTCSettingsActivity` до более современного вида (Material 3 / нормальные группы / иконки / hint).

Бэгфикс затрагивает три плоскости: 
- **«сервер»** — наш форк-сервер `temp-files/Olcrtc_manager-refactor-universal-carrier-fork` (Go);
- **«ядро клиента»** — локальный Go-модуль клиента `library/core/olcrtc_local` и сборка `app/libs/libsagernetcore.aar` (тот же Go, но в Android-проекте);
- **«UI клиента»** — Kotlin/XML слой в `app/src/main/...`.

ТЗ для исполнителей по серверу и по клиенту вынесены в отдельные документы:
- `.kiro/specs/webrtc-transports-fix/requirements-server.md`
- `.kiro/specs/webrtc-transports-fix/requirements-client.md`

(оба документа создаются на фазе Design / Tasks, чтобы не дублировать содержимое требований).

## Bug Analysis

### Current Behavior (Defect)

Дефекты сгруппированы по транспорту/области. Условия (`WHEN`) описывают вход, при котором проявляется баг.

#### Транспорт `datachannel` (carrier = telemost | jazz | wbstream)

1.1 WHEN пользователь создаёт профиль `OLCRTCBean` с `provider = "telemost"` и `transport = "datachannel"` и стартует туннель THEN AAR-биндинг `mobile.Mobile.startWithTransport(...)` вызывается с 7-аргументной сигнатурой без `clientID`, и Go-уровень внутри `library/core/olcrtc_local/mobile/mobile.go` не передаёт `DeviceID` в `client.Config`, из-за чего сервер получает пустой device id и завершает handshake до открытия DataChannel.

1.2 WHEN пользователь стартует профиль с `provider = "telemost"` и `transport = "datachannel"` THEN signaling/auth идёт по старому коду из `library/core/olcrtc_local/internal/provider/telemost/peer.go` (отсутствует в эталоне), wire-формат запросов к Telemost API не совпадает с новой моделью `auth/telemost/api.go` + `engine/goolom/*`, и DataChannel сессия не открывается.

1.3 WHEN пользователь стартует профиль с `provider = "jazz"` и `transport = "datachannel"` THEN клиент использует устаревший `library/core/olcrtc_local/internal/provider/jazz/peer.go` и отправляет в SaluteJazz API запросы без новых полей (`X-Jazz-Ua`, `Origin`, `Referer`, `mediaWithoutAutoSubscribeSupport`), которые ожидает совместимая с новым сервером ветка, и handshake падает.

1.4 WHEN пользователь стартует `provider = "jazz"` без задания пароля комнаты THEN UI-слой не предоставляет способа задать пароль (поля нет в `olcrtc_preferences.xml`), `RoomURL` отправляется в формате одиночного `roomID` (а не `roomID:password`), и сервер/auth-провайдер `salutejazz` возвращает `auth.ErrRoomIDRequired: expected <roomID>:<password>`.

#### Carrier `telemost` (на любом транспорте)

1.5 WHEN пользователь стартует профиль с `provider = "telemost"` (любой транспорт) THEN из-за отсутствия в локальном Go-модуле `internal/auth/telemost/*` и `internal/engine/goolom/*` подключение не доходит до стадии получения `MediaServerURL` от Telemost-API, и канал не устанавливается.

1.6 WHEN пользователь стартует профиль с `provider = "telemost"` и `roomId` задан без префикса `https://telemost.yandex.ru/j/` THEN локальный модуль строит URL правильно (это покрыто), но дальнейшее обращение в Telemost API использует старую обработку ответа без поля `clientConfig.mediaServerURL` (структура у эталона: `info.ClientConfig.MediaServerURL`), и инициализация `engine.goolom` не может стартовать.

#### Транспорт `seichannel`

1.7 WHEN пользователь стартует профиль с `transport = "seichannel"` (любой carrier) THEN клиент использует старую реализацию `library/core/olcrtc_local/internal/transport/seichannel/transport.go` (~1225 строк отличий от эталона), фрейминг SEI NALU и порядок ACK не совпадают с серверным эталоном, и поток данных через SEI не идёт (либо рвётся при первом фрагменте).

1.8 WHEN пользователь стартует профиль с `transport = "seichannel"` THEN валидация в `library/core/olcrtc_local/internal/app/session/session.go` требует `SEIFPS`, `SEIBatchSize`, `SEIFragmentSize`, `SEIAckTimeoutMS`, но Kotlin-слой в `OLCRTCExternalInstance.kt` их не выставляет и `Mobile` не имеет setter-ов для них, поэтому сессия отклоняется ошибкой `ErrSEIFPSRequired`/`ErrSEIBatchSizeRequired` ещё до запуска.

#### UI: salutejazz/jazz без полей пароля

1.9 WHEN пользователь выбирает `provider = "jazz"` в `OLCRTCSettingsActivity` и заполняет `Room ID` THEN в `olcrtc_preferences.xml` нет EditText для пароля комнаты, в `OLCRTCBean` нет поля `roomPassword`, и при сериализации в Go-уровень передаётся только `roomId`, поэтому сервер `salutejazz` не может присоединиться к существующей комнате (`Issue` ожидает `<roomID>:<password>`).

1.10 WHEN пользователь хочет ввести password для `provider = "jazz"` THEN в текущем UI отсутствует отдельное поле `Room password`, единственное «password-like»-поле — это `Pre-shared key (hex)`, которое логически другая сущность (ключ шифрования olcrtc).

#### UI: устаревшее визуальное оформление панели

1.11 WHEN пользователь открывает экран настройки olcRTC-профиля THEN экран использует базовые `EditTextPreference` без иконок-категорий нового стиля, без подсказок (helper text), без визуальной группировки (basic vs advanced) с разделителями Material 3, без preview активного transport, что воспринимается пользователем как «устаревший интерфейс».

### Expected Behavior (Correct)

Каждый clause в этом разделе является зеркалом соответствующего clause из 1.x.

#### Транспорт `datachannel` (carrier = telemost | jazz | wbstream)

2.1 WHEN пользователь создаёт профиль `OLCRTCBean` с `provider = "telemost"` и `transport = "datachannel"` и стартует туннель THEN AAR-биндинг `mobile.Mobile.startWithTransport(...)` SHALL принимать 8 аргументов, включающих `clientID`, и Go-уровень SHALL передавать непустой `DeviceID` в `client.Config`, чтобы сервер успешно завершал handshake и открывал DataChannel.

2.2 WHEN пользователь стартует профиль с `provider = "telemost"` и `transport = "datachannel"` THEN клиент SHALL использовать обновлённый код carrier `telemost` (новая модель `auth/telemost` + `engine/goolom`) с тем же wire-форматом, что эталонный сервер, и DataChannel-сессия SHALL открываться без ошибок.

2.3 WHEN пользователь стартует профиль с `provider = "jazz"` и `transport = "datachannel"` THEN клиент SHALL использовать обновлённый код carrier `jazz` (модель `auth/salutejazz` + `engine/salutejazz`) с актуальными HTTP-заголовками (`X-Jazz-Ua`, `Origin`, `Referer`) и payload-полями, и handshake SHALL завершаться успешно.

2.4 WHEN пользователь стартует `provider = "jazz"` THEN UI SHALL предоставлять отдельное поле `Room password`, его значение SHALL объединяться с `roomId` в формате `<roomId>:<password>` и SHALL передаваться в Go-уровень как `RoomURL`, чтобы `auth/salutejazz` мог корректно вызвать `joinRoom`.

#### Carrier `telemost` (на любом транспорте)

2.5 WHEN пользователь стартует профиль с `provider = "telemost"` (любой транспорт) THEN клиент SHALL содержать актуальные пакеты `internal/auth/telemost/*` и `internal/engine/goolom/*`, эквивалентные тем, что в эталонном репозитории `temp-files/olcrtc-refactor-universal-carrier`, и SHALL получать `MediaServerURL` через `auth.Issue → engine.goolom.New`.

2.6 WHEN пользователь стартует профиль с `provider = "telemost"` и `roomId` задан без префикса `https://telemost.yandex.ru/j/` THEN клиент SHALL автоматически достраивать полный URL и SHALL корректно парсить ответ Telemost API через структуру `info.ClientConfig.MediaServerURL`, и инициализация `engine.goolom` SHALL завершаться успешно.

#### Транспорт `seichannel`

2.7 WHEN пользователь стартует профиль с `transport = "seichannel"` (любой carrier) THEN клиент SHALL использовать обновлённую реализацию `internal/transport/seichannel/*` (фрейминг + ACK + h264-обвязка), эквивалентную эталонной, и поток данных через SEI SHALL устанавливаться и работать стабильно.

2.8 WHEN пользователь стартует профиль с `transport = "seichannel"` THEN клиент SHALL получать дефолтные значения `SEIFPS`, `SEIBatchSize`, `SEIFragmentSize`, `SEIAckTimeoutMS` либо из mobile-API setter-ов (`Mobile.setSEIOptions(...)`), либо из встроенных дефолтов в Go-уровне, и сессия SHALL не падать с `ErrSEIxRequired`.

#### UI: salutejazz/jazz с полями пароля

2.9 WHEN пользователь выбирает `provider = "jazz"` в `OLCRTCSettingsActivity` THEN UI SHALL показать дополнительное поле `Room ID` и поле `Room password`, оба обязательны, в `OLCRTCBean` SHALL быть поле `roomPassword`, и сериализация в Go SHALL передавать `RoomURL = "<roomId>:<password>"`.

2.10 WHEN пользователь выбирает `provider != "jazz"` THEN поле `Room password` SHALL быть скрыто или отключено (visibility: gone / enabled: false), а семантика `Pre-shared key (hex)` SHALL быть визуально отделена от password-поля комнаты (разные иконки, разные подписи).

#### UI: современное визуальное оформление

2.11 WHEN пользователь открывает экран настройки olcRTC-профиля THEN экран SHALL использовать Material 3-совместимый стиль (карточки/divider'ы Material 3, `app:icon` для всех ключевых полей, helper text/summary с примерами значений, group label «Connection» / «Advanced» c `PreferenceCategory`, заметная индикация активного transport), и не SHALL использовать базовые недекорированные `EditTextPreference` без иконок и без summary.

### Unchanged Behavior (Regression Prevention)

3.1 WHEN пользователь стартует профиль с `transport = "vp8channel"` (любой carrier) THEN клиент SHALL CONTINUE TO работать как сейчас и устанавливать соединение через KCP/vp8 (в текущей сборке этот путь работоспособен и его поведение не должно деградировать).

3.2 WHEN пользователь стартует профиль с `transport = "videochannel"` (любой carrier) THEN клиент SHALL CONTINUE TO передавать видеопоток (qrcode/tile codec) с теми же параметрами `VideoWidth/Height/FPS/Bitrate/HW/Codec`, что задаются сейчас.

3.3 WHEN пользователь импортирует или экспортирует профиль через URL `olcrtc://...` THEN парсер `parseOLCRTC` и `OLCRTCBean.toUri()` SHALL CONTINUE TO принимать и формировать URI с тем же набором query-параметров (`key`, `dns`, `transport`, `vp8_fps`, `vp8_batch`), и обратной совместимости со старыми ссылками SHALL быть сохранена. Новое поле `room_password` SHALL добавляться как опциональный query-параметр.

3.4 WHEN пользователь использует profile-keepalive (DataStore.serverOlcrtcKeepaliveInterval) THEN keepalive-логика в `OLCRTCExternalInstance.kt` SHALL CONTINUE TO работать (TCP-проверка через локальный SOCKS до `1.1.1.1:53`) и SHALL CONTINUE TO триггерить переподключение через `onSessionLost`.

3.5 WHEN пользователь сохраняет/загружает существующий профиль `OLCRTCBean` (Kryo-сериализация версия 2) THEN миграция на новую версию SHALL CONTINUE TO читать старые сериализованные блобы (без поля `roomPassword`) и SHALL не терять `provider/transport/roomId/keyHex/dnsServer/vp8Fps/vp8BatchSize/keepaliveIntervalSec`.

3.6 WHEN сервер запускается с `provider = "wbstream"` и любым транспортом, который сейчас работает (например, `vp8channel`) THEN серверный путь SHALL CONTINUE TO работать без изменений (тестово известно, что wbstream-сценарий идёт через тот же стек, что vp8channel).

3.7 WHEN сервер обслуживает профиль через SOCKS5 + WARP (admin/ports/api) THEN логика `protect.SetSocks5(...)` и WARP-маршрутизация SHALL CONTINUE TO работать так же, как сейчас, и не SHALL ломаться обновлением carrier/transport-слоёв.

3.8 WHEN пользователь использует Android UI элементы вне `OLCRTCSettingsActivity` (Tasker, ConfigurationFragment, ProxyEntity-CRUD) THEN они SHALL CONTINUE TO взаимодействовать с `OLCRTCBean` через тот же интерфейс (`init/serialize`, `toUri`, `applyFeatureSettings`).

## Деривация bug condition (методология)

Из приведённых выше требований выводится следующая bug condition и property:

### Bug Condition Function

```pascal
FUNCTION isBugCondition(X)
  INPUT: X of type OLCRTCSessionInput  // (provider, transport, roomId, password?)
  OUTPUT: boolean

  // Дефект проявляется:
  // 1. на любом транспорте, чья реализация в локальном модуле устарела
  //    (datachannel или seichannel),
  // 2. либо на любом carrier, чей signaling в локальном модуле устарел
  //    (telemost или jazz),
  // 3. либо когда пользователь выбрал jazz и не имеет UI-поля для password.
  RETURN
    (X.transport ∈ {"datachannel", "seichannel"})
    OR (X.provider ∈ {"telemost", "jazz"})
    OR (X.provider = "jazz" AND X.password = ∅ AND X.roomId ≠ ∅)
END FUNCTION
```

### Property — Fix Checking

```pascal
// Property: Fix Checking — корректное поведение для buggy inputs.
FOR ALL X WHERE isBugCondition(X) DO
  result ← startTunnel'(X)   // F' — клиент после фикса
  ASSERT result.handshake_completed = TRUE
  ASSERT result.transport_open = TRUE
  ASSERT result.error = NIL
  ASSERT no_crash(result)
END FOR
```

### Property — Preservation Checking

```pascal
// Property: Preservation Checking — для не-buggy входов поведение не меняется.
FOR ALL X WHERE NOT isBugCondition(X) DO
  ASSERT startTunnel(X) = startTunnel'(X)
  // т.е. vp8channel/videochannel + wbstream продолжают работать так же,
  // существующие olcrtc:// URI парсятся идентично, keepalive не меняется,
  // сериализация старых профилей читается корректно.
END FOR
```

**Определения:**
- `startTunnel` (`F`) — текущая реализация цепочки `OLCRTCSettingsActivity → OLCRTCBean → OLCRTCExternalInstance → mobile.Mobile → library/core/olcrtc_local → temp-files/Olcrtc_manager-...-fork` со всеми описанными выше дефектами.
- `startTunnel'` (`F'`) — реализация после применения фиксов из `requirements-server.md` и `requirements-client.md`.
- `OLCRTCSessionInput X` — кортеж входных параметров профиля: `(provider, transport, roomId, password, keyHex, vp8Fps, vp8Batch, ...)`.
