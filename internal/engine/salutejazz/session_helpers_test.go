package salutejazz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

const (
	testJazzGroupID = "group-1"
	testJazzRoomID  = "room-1"
)

//nolint:cyclop // table-driven test naturally has many branches
func TestSessionStateHelpers(t *testing.T) {
	s := &Session{
		reconnectCh:    make(chan struct{}, 1),
		closeCh:        make(chan struct{}),
		sessionCloseCh: make(chan struct{}),
		sendQueue:      make(chan []byte, 1),
		subscriberConn: make(chan struct{}),
		publisherConn:  make(chan struct{}),
	}

	s.resetMediaState()
	if s.subscriberReady.Load() || s.publisherReady.Load() || s.subscriberConn == nil || s.publisherConn == nil {
		t.Fatal("resetMediaState() did not reset readiness")
	}
	if s.hasLocalVideoTracks() {
		t.Fatal("hasLocalVideoTracks() = true without tracks")
	}
	if err := s.AddVideoTrack(nil); err != nil {
		t.Fatalf("AddVideoTrack(nil) error = %v", err)
	}
	if !s.hasLocalVideoTracks() {
		t.Fatal("hasLocalVideoTracks() = false after AddVideoTrack")
	}

	s.SetVideoTrackHandler(func(*webrtc.TrackRemote, *webrtc.RTPReceiver) {})
	if s.videoTrackHandler() == nil {
		t.Fatal("videoTrackHandler() = nil")
	}

	cfg := defaultWebRTCConfig()
	if cfg.SDPSemantics != webrtc.SDPSemanticsUnifiedPlan || cfg.BundlePolicy != webrtc.BundlePolicyMaxBundle {
		t.Fatalf("defaultWebRTCConfig() = %+v", cfg)
	}
	if s.buildAPI() == nil {
		t.Fatal("buildAPI() returned nil")
	}
}

func TestSessionCallbacksQueueReconnectAndClose(t *testing.T) {
	s := &Session{
		reconnectCh:    make(chan struct{}, 1),
		closeCh:        make(chan struct{}),
		sessionCloseCh: make(chan struct{}),
		sendQueue:      make(chan []byte, 1),
	}

	s.SetReconnectCallback(func(*webrtc.DataChannel) {})
	s.SetShouldReconnect(func() bool { return true })
	s.SetEndedCallback(func(string) {})
	if s.onReconnect == nil || s.shouldReconnect == nil || s.onEnded == nil {
		t.Fatal("callbacks were not stored")
	}

	s.queueReconnect()
	select {
	case <-s.reconnectCh:
	default:
		t.Fatal("queueReconnect() did not enqueue")
	}

	s.SetShouldReconnect(func() bool { return false })
	s.queueReconnect()
	select {
	case <-s.reconnectCh:
		t.Fatal("queueReconnect() enqueued despite policy=false")
	default:
	}

	done := make(chan struct{})
	go func() {
		s.WatchConnection(context.Background())
		close(done)
	}()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	<-done
	if err := s.Send([]byte("closed")); !errors.Is(err, ErrDataChannelNotReady) {
		t.Fatalf("Send() error = %v, want datachannel not ready", err)
	}
}

func TestSessionCanSendVideoOnlyModes(t *testing.T) {
	s := &Session{sendQueue: make(chan []byte, 1)}
	s.subscriberReady.Store(true)
	if !s.CanSend() {
		t.Fatal("CanSend() = false for subscriber-ready session without local video")
	}
	_ = s.AddVideoTrack(nil)
	if s.CanSend() {
		t.Fatal("CanSend() = true with local video but publisher not ready")
	}
	s.publisherReady.Store(true)
	if !s.CanSend() {
		t.Fatal("CanSend() = false with subscriber and publisher ready")
	}
	s.closed.Store(true)
	if s.CanSend() {
		t.Fatal("CanSend() = true for closed session")
	}
}

func TestSendPublisherTrackAddWritesJazzPayload(t *testing.T) {
	msgCh := make(chan map[string]any, 1)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("read json: %v", err)
			return
		}
		msgCh <- msg
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer func() { _ = resp.Body.Close() }()
	}
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	s := &Session{
		roomID:  testJazzRoomID,
		groupID: testJazzGroupID,
		ws:      conn,
	}
	if err := s.sendPublisherTrackAdd("VIDEO", "CAMERA", false); err != nil {
		t.Fatalf("sendPublisherTrackAdd() error = %v", err)
	}

	msg := <-msgCh
	assertJazzTrackAddEnvelope(t, msg)
	assertJazzTrackAddPayload(t, msg[keyPayload])
}

func TestHandleParticipantsUpdateUnmutesCameraTrack(t *testing.T) {
	msgCh := make(chan map[string]any, 1)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()

		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Errorf("read json: %v", err)
			return
		}
		msgCh <- msg
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer func() { _ = conn.Close() }()

	s := &Session{
		roomID:      testJazzRoomID,
		groupID:     testJazzGroupID,
		ws:          conn,
		videoTracks: []webrtc.TrackLocal{nil},
	}
	s.videoOffered.Store(true)
	s.handleParticipantsUpdate(map[string]any{
		"update": map[string]any{
			"participants": []any{
				map[string]any{
					"isPublisher": true,
					"tracks": []any{
						map[string]any{
							"sid":        "TR_CAMERA_1",
							"type":       "VIDEO",
							"source":     "CAMERA",
							payloadMuted: true,
						},
					},
				},
			},
		},
	})

	msg := <-msgCh
	assertJazzTrackAddEnvelope(t, msg)
	assertJazzTrackMutedPayload(t, msg[keyPayload])
}

func TestJazzICECandidatePayload(t *testing.T) {
	sdpMid := "0"
	sdpMLineIndex := uint16(1)
	usernameFragment := "ufrag-1"

	got := jazzICECandidatePayload(webrtc.ICECandidateInit{
		Candidate:        "candidate:1 1 udp 1 127.0.0.1 12345 typ host",
		SDPMid:           &sdpMid,
		SDPMLineIndex:    &sdpMLineIndex,
		UsernameFragment: &usernameFragment,
	}, "PUBLISHER")

	if got["candidate"] != "candidate:1 1 udp 1 127.0.0.1 12345 typ host" {
		t.Fatalf("candidate = %v", got["candidate"])
	}
	if got["sdpMid"] != "0" {
		t.Fatalf("sdpMid = %v, want 0", got["sdpMid"])
	}
	if got["sdpMLineIndex"] != uint16(1) {
		t.Fatalf("sdpMLineIndex = %v, want 1", got["sdpMLineIndex"])
	}
	if got["usernameFragment"] != "ufrag-1" {
		t.Fatalf("usernameFragment = %v, want ufrag-1", got["usernameFragment"])
	}
	if got["target"] != "PUBLISHER" {
		t.Fatalf("target = %v, want PUBLISHER", got["target"])
	}
}

func assertJazzTrackAddEnvelope(t *testing.T, msg map[string]any) {
	t.Helper()

	if msg[keyRoomID] != testJazzRoomID {
		t.Fatalf("roomId = %v, want %s", msg[keyRoomID], testJazzRoomID)
	}
	if msg[keyEvent] != eventMediaIn {
		t.Fatalf("event = %v, want %s", msg[keyEvent], eventMediaIn)
	}
	if msg[keyGroupID] != testJazzGroupID {
		t.Fatalf("%s = %v, want %s", keyGroupID, msg[keyGroupID], testJazzGroupID)
	}
}

func assertJazzTrackAddPayload(t *testing.T, raw any) {
	t.Helper()

	payload, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("payload missing or wrong type: %+v", raw)
	}
	if payload[payloadMethod] != "rtc:track:add" {
		t.Fatalf("%s = %v, want rtc:track:add", payloadMethod, payload[payloadMethod])
	}

	track, ok := payload[payloadTrack].(map[string]any)
	if !ok {
		t.Fatalf("track missing or wrong type: %+v", payload[payloadTrack])
	}
	if track[payloadType] != "VIDEO" {
		t.Fatalf("%s = %v, want VIDEO", payloadType, track[payloadType])
	}
	if track["source"] != "CAMERA" {
		t.Fatalf("source = %v, want CAMERA", track["source"])
	}
	if track[payloadMuted] != false {
		t.Fatalf("muted = %v, want false", track[payloadMuted])
	}
}

func assertJazzTrackMutedPayload(t *testing.T, raw any) {
	t.Helper()

	payload, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("payload missing or wrong type: %+v", raw)
	}
	if payload[payloadMethod] != "rtc:track:muted" {
		t.Fatalf("%s = %v, want rtc:track:muted", payloadMethod, payload[payloadMethod])
	}

	mute, ok := payload["mute"].(map[string]any)
	if !ok {
		t.Fatalf("mute missing or wrong type: %+v", payload["mute"])
	}
	if mute["sid"] != "TR_CAMERA_1" {
		t.Fatalf("sid = %v, want TR_CAMERA_1", mute["sid"])
	}
	if mute[payloadMuted] != false {
		t.Fatalf("muted = %v, want false", mute[payloadMuted])
	}
}
