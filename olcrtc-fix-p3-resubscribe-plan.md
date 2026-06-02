# План Fix P3: Polling-based setSlots resubscribe в olcrtc goolom engine

## Контекст (самодостаточный)

Olcrtc — проект в `C:\Users\User\Documents\Olcrtc_manager`. Сервер olcrtc использует `goolom` engine для подключения к WB Stream (Yandex Telemost/Goolom SFU). Данные VPN-туннеля передаются через VP8 video channel: сервер публикует VP8 video track, клиент подписывается на него через SFU.

### Архитектура goolom engine (ключевые файлы)

**`internal/engine/goolom/session.go`** — структура `Session`:

```go
type Session struct {
    name             string
    mediaServerURL   string
    peerID           string
    roomID           string
    credentials      string
    // ...
    ws    *websocket.Conn
    wsMu  sync.Mutex
    pcSub *webrtc.PeerConnection   // subscriber PC (получает треки от SFU)
    pcPub *webrtc.PeerConnection   // publisher PC (отправляет треки в SFU)

    subscriberReady atomic.Bool
    publisherReady  atomic.Bool
    subscriberConn  chan struct{}   // сигнал: subscriber PC Connected
    publisherConn   chan struct{}   // сигнал: publisher PC Connected

    videoTrackMu    sync.RWMutex
    videoTracks     []webrtc.TrackLocal
    onVideoTrack    func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
    // ...
    closed          atomic.Bool
    reconnecting    atomic.Bool
    reconnectCh     chan struct{}
    closeCh         chan struct{}
    // ...
}
```

**`internal/engine/goolom/lifecycle.go`** — метод `Connect`:

1. Создаёт `pcSub` и `pcPub` PeerConnections.
2. Регистрирует `pcSub.OnTrack(func(track, receiver) { ... })` — callback для remote треков.
3. Устанавливает `pcSub.OnConnectionStateChange(s.onSubscriberConnectionStateChange)`:
   - `Connected` → `subscriberReady.Store(true)`, `closeSignal(subscriberConn)`.
   - `Disconnected/Failed/Closed` → `subscriberReady.Store(false)`, `queueReconnect`.
4. Dial WebSocket к `mediaServerURL`.
5. Запускает `handleSignaling` goroutine (читает JSON из WebSocket).
6. Отправляет `sendHello` (hello message с capabilities).
7. Ждёт `subscriberConn` (media mode) или `dcReady` (data-channel mode).
8. Возвращает управление вызывающему.

Метод `Close`:
1. Отправляет `sendLeave` через WebSocket (`leave` message с UUID).
2. Закрывает `pcSub`, `pcPub`, `ws`.
3. Ожидает завершения goroutines (`wg.Wait`).

Метод `WatchConnection`:
- Мониторит `reconnectCh`. При срабатывании — `handleReconnectAttempt` → создаёт новые PCs и WebSocket.

Метод `handleSignaling` (goroutine, цикл `for`):
- `subscriberSdpOffer` → `handleSdpOffer(offer, uid, !pubSent)`.
  - При первом offer (`!pubSent`) дополнительно отправляет publisher SDP offer.
- `publisherSdpAnswer` → `handleSdpAnswer`.
- `webrtcIceCandidate` → `handleICE`.
- `serverHello` → `applyServerHelloConfig` (ICE servers), `sendAck`.
- `slotsConfig`, `slotsMeta`, `vadActivity`, `ping`, `pong` → `sendAck`.
- `conferenceClosed/Ended` → `signalEnded`.
- Ошибка чтения WebSocket → `queueReconnect`.

**`internal/engine/goolom/media.go`** — `sendSetSlots`:

```go
func (s *Session) sendSetSlots() error {
    s.wsMu.Lock()
    defer s.wsMu.Unlock()
    slots := make([]map[string]int, 0, 8)
    for range 8 {
        slots = append(slots, map[string]int{"width": 1280, "height": 720})
    }
    return s.ws.WriteJSON(map[string]any{
        keyUID: uuid.NewString(),  // keyUID = "uid" (string constant)
        "setSlots": map[string]any{
            "slots":              slots,
            "audioSlotsCount":    0,
            "key":                1,
            "shutdownAllVideo":   nil,
            "withSelfView":       false,
            "selfViewVisibility": "ON_LOADING_THEN_SHOW",
            "gridConfig":         map[string]any{},
        },
    })
}
```

Вызывается из `handleSdpOffer` при первом `subscriberSdpOffer`, если `s.onData == nil` (media mode, не data-channel).

**`internal/transport/vp8channel/transport.go`** — чтение VP8 трека:

```go
func (p *streamTransport) handleRemoteTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
    if track.Codec().MimeType != webrtc.MimeTypeVP8 {
        go p.drainTrack(track)
        return
    }
    go p.readVP8Track(track)
}

func (p *streamTransport) readVP8Track(track *webrtc.TrackRemote) {
    var state vp8FrameState
    buf := make([]byte, rtpBufSize)
    for {
        n, _, err := track.Read(buf)
        if err != nil {
            return
        }
        pkt := &rtp.Packet{}
        if pkt.Unmarshal(buf[:n]) != nil {
            continue
        }
        frame := state.processRTPPacket(pkt)
        if frame == nil {
            continue
        }
        p.handleIncomingFrame(frame)  // → KCP
    }
}
```

`track.Read(buf)` — блокирующий вызов. Если SFU перестаёт отправлять RTP packets, goroutine зависает навсегда.

---

## Проблема

WB Stream backend пересчитывает SFU routing / bandwidth allocation при изменении room state (join/leave participant'ов, включая account-бота панели). Это может инициировать **renegotiation**: SFU отправляет новый `subscriberSdpOffer` всем участникам.

Текущий goolom engine обрабатывает `subscriberSdpOffer` (`SetRemoteDescription` + `CreateAnswer` + отправка `subscriberSdpAnswer`), но **`sendSetSlots` отправляется только при первом offer**. При последующих renegotiation-offers setSlots **не отправляется повторно**.

Если SFU изменил routing треков (например, изменил SSRC/transceiver mapping, или перестал forward'ить VP8 трек на этот subscriber после room-state cleanup), olcrtc-сервер может **потерять подписку на VP8 трек без явного уведомления**.

VP8 channel transport читает video track через `track.Read()` — блокирующий вызов. Если трек "замолкает", goroutine зависает, KCP connection **starves** (не получает входящих данных), liveness probe timeout (`DefaultTimeout = 15s`) истекает, туннель помечается dead и прерывается.

---

## Решение

Добавить **watchdog goroutine** в `Session`, который периодически (каждые 5 секунд) отправляет `sendSetSlots` через WebSocket при активном subscriber connection. Это **force-re-subscribe** на все video треки, заставляя SFU подтвердить/восстановить routing.

Если `subscriberReady == false`, watchdog ничего не делает (session в процессе переподключения или отключена).

---

## Файлы и конкретные изменения

### 1. `internal/engine/goolom/session.go`

Добавить поле в `Session` struct (после `publisherConn chan struct{}`, строка ~188):

```go
    subscriptionWatchStop chan struct{} // канал для остановки watchdog
```

### 2. `internal/engine/goolom/lifecycle.go`

#### 2a. В методе `Connect` — запустить watcher после установления соединения

Текущий код `Connect` (конец метода):

```go
    if s.onData != nil {
        select {
        case <-dcReady:
            return nil
        case <-time.After(15 * time.Second):
            return ErrDataChannelTimeout
        case <-ctx.Done():
            return fmt.Errorf("connect context cancelled: %w", ctx.Err())
        }
    }
    return s.waitForMediaReady(ctx, 20*time.Second)
```

Заменить на:

```go
    if s.onData != nil {
        select {
        case <-dcReady:
            s.startSubscriptionWatcher(ctx)
            return nil
        case <-time.After(15 * time.Second):
            return ErrDataChannelTimeout
        case <-ctx.Done():
            return fmt.Errorf("connect context cancelled: %w", ctx.Err())
        }
    }
    if err := s.waitForMediaReady(ctx, 20*time.Second); err != nil {
        return err
    }
    s.startSubscriptionWatcher(ctx)
    return nil
```

#### 2b. В методе `Close` — остановить watcher перед tear-down

В начало метода `Close` (перед `alreadyClosing := s.closed.Swap(true)`):

```go
    s.stopSubscriptionWatcher()
```

#### 2c. Новые методы (добавить в файл, например после `Close`):

```go
// startSubscriptionWatcher runs a background goroutine that periodically
// sends setSlots to the SFU, ensuring VP8 video tracks are re-subscribed
// after room-state changes that may alter SFU routing.
func (s *Session) startSubscriptionWatcher(ctx context.Context) {
    s.stopSubscriptionWatcher() // prevent duplicate goroutines

    s.subscriptionWatchStop = make(chan struct{})
    go func() {
        ticker := time.NewTicker(5 * time.Second)
        defer ticker.Stop()

        for {
            select {
            case <-ctx.Done():
                return
            case <-s.subscriptionWatchStop:
                return
            case <-s.closeCh:
                return
            case <-ticker.C:
                if !s.subscriberReady.Load() {
                    continue
                }
                if s.onData != nil {
                    // data-channel mode: no video tracks to subscribe
                    continue
                }
                if err := s.sendSetSlots(); err != nil {
                    logger.Debugf("subscription watcher setSlots error: %v", err)
                } else {
                    logger.Verbosef("subscription watcher: setSlots sent")
                }
            }
        }
    }()
}

func (s *Session) stopSubscriptionWatcher() {
    if s.subscriptionWatchStop != nil {
        select {
        case <-s.subscriptionWatchStop:
            // already closed
        default:
            close(s.subscriptionWatchStop)
        }
        s.subscriptionWatchStop = nil
    }
}
```

### 3. `internal/engine/goolom/media.go`

Модифицировать `sendSetSlots` для инкрементирования `key` при повторных вызовах (см. ниже «Важные замечания»).

Если в `Session` struct добавлено поле:

```go
    subscriptionKey atomic.Int32 // инкрементируется при каждом setSlots
```

Тогда в `sendSetSlots` заменить жёсткое `"key": 1` на:

```go
            "key": int(s.subscriptionKey.Add(1)),
```

Если `subscriptionKey` не добавлять, оставить `"key": 1` и monitor — возможно, SFU принимает повторные setSlots с тем же key.

---

## Важные замечания

### Semantics поля `key` в setSlots

В текущем коде `sendSetSlots` использует `"key": 1`. Неизвестно, является ли это revision counter'ом, sequence number'ом или фиксированным идентификатором запроса. Если SFU **отвергает** повторные setSlots с тем же `key`, необходимо:

1. Добавить `subscriptionKey atomic.Int32` в `Session` struct (`session.go`).
2. Заменить `"key": 1` на `"key": int(s.subscriptionKey.Add(1))` в `sendSetSlots` (`media.go`).

Если SFU **принимает** повторные setSlots с `key: 1`, дополнительные поля не нужны.

### reconnect и watcher

При reconnect (`WatchConnection` → `handleReconnectAttempt` → `reconnect`) создаются новые PeerConnections и WebSocket, но **полный `Close()` не вызывается** — tear-down частичный. `startSubscriptionWatcher` вызывается из `Connect`, который вызывается при reconnect. `stopSubscriptionWatcher` в начале `startSubscriptionWatcher` предотвращает дублирование goroutine. Это безопасно.

При reconnect `subscriberReady.Store(false)`, watcher пропускает тики (`!s.subscriberReady.Load()`), затем `Connect` завершается, `startSubscriptionWatcher` перезапускается.

### Частота

5 секунд — приемлемо. KCP liveness timeout = 15 секунд. 3 попытки за 15 секунд достаточно для восстановления routing.

### Логирование

В логах olrtc (уровень `Verbosef` или `Debugf`) должны появляться:
- `subscription watcher: setSlots sent` — каждые 5 секунд при активном subscriber.
- `subscription watcher setSlots error: ...` — при ошибках WebSocket write.

---

## Валидация

1. `go build ./...` — должен пройти без ошибок.
2. Запустить olcrtc сервер, подключить клиент. В логах сервера должны появляться `subscription watcher: setSlots sent` каждые 5 секунд.
3. Запустить панель с account-ботом (цикл ~1 час, stay 5 сек).
4. Туннель должен оставаться стабильным >60 секунд после подключения клиента, даже если account-бот подключался/отключался незадолго до этого.
5. Если туннель всё ещё падает:
   - Проверить логи на `setSlots` errors от SFU (SFU может возвращать signaling error).
   - Проверить, помогает ли инкремент `key` в `sendSetSlots`.
   - Если не помогает — проблема глубже (возможно, нужен Fix P2: сервер olcrtc как real user через WB Stream auth).

---

## Связь с панелью (контекст)

Account-бот панели (`backend/bot/wbstream_account.go`) — real user, подключается к комнате WB Stream через LiveKit (только `JoinWithToken`, без отдельного REST API join/leave). Бот stay 5 секунд, interval ~1 час. При join/leave бота WB Stream backend пересчитывает room state. Это может вызывать renegotiation для существующих участников (включая olcrtc-сервер). Fix P3 защищает olcrtc-сервер от последствий таких renegotiation'ов.
