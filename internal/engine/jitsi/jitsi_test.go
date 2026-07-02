package jitsi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
	"github.com/zarazaex69/j"
)

const (
	testHost      = "meet.example.com"
	testRoom      = "myroom"
	rawFieldKey   = "raw"
	classEndpoint = "EndpointMessage"
)

func TestNormaliseHost(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{testHost, testHost},
		{"https://" + testHost, testHost},
		{"https://" + testHost + "/", testHost},
		{"https://" + testHost + "/path", testHost},
		{"//" + testHost, testHost},
		{"  https://" + testHost + "  ", testHost},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := normaliseHost(tc.raw); got != tc.want {
				t.Fatalf("normaliseHost(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestXMPPDomainTargetsVirtualhost(t *testing.T) {
	const (
		xmppVHost = "meet.jitsi"
		webHost   = "meet.mamba.group"
	)
	tests := []struct {
		name     string
		jid      string
		fallback string
		want     string
	}{
		{"web host differs from xmpp domain", "8307a4f4@" + xmppVHost + "/T4i4s0jt", webHost, xmppVHost},
		{"no resource part", "uuid@" + xmppVHost, webHost, xmppVHost},
		{"empty jid falls back to host", "", webHost, webHost},
		{"jid without domain falls back", "node-only", "meet.handyweb.org", "meet.handyweb.org"},
		{"empty domain falls back", "node@/resource", "meet.small-dm.ru", "meet.small-dm.ru"},
		{"domain only with resource", "node@" + xmppVHost + "/", "fallback.host", xmppVHost},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := xmppDomain(tt.jid, tt.fallback); got != tt.want {
				t.Fatalf("xmppDomain(%q, %q) = %q, want %q", tt.jid, tt.fallback, got, tt.want)
			}
		})
	}
}

func TestDecodeRaw(t *testing.T) {
	const payload = "hello world"
	encoded := encodeForTest(t, []byte(payload))

	got := decodeRaw(makeBridgeMessage(classEndpoint, map[string]any{rawFieldKey: encoded}))
	if string(got) != payload {
		t.Fatalf("decodeRaw = %q, want %q", got, payload)
	}

	if got := decodeRaw(makeBridgeMessage("OtherClass", map[string]any{rawFieldKey: encoded})); got != nil {
		t.Fatalf("decodeRaw(other class) = %q, want nil", got)
	}
	if got := decodeRaw(makeBridgeMessage(classEndpoint, map[string]any{})); got != nil {
		t.Fatalf("decodeRaw(no raw) = %q, want nil", got)
	}
	if got := decodeRaw(makeBridgeMessage(classEndpoint, map[string]any{rawFieldKey: "not-base64!!!"})); got != nil {
		t.Fatalf("decodeRaw(bad base64) = %q, want nil", got)
	}
}

func TestNewRequiresHost(t *testing.T) {
	_, err := New(context.Background(), engine.Config{
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if !errors.Is(err, ErrHostRequired) {
		t.Fatalf("err = %v, want ErrHostRequired", err)
	}
}

func TestNewRequiresRoom(t *testing.T) {
	_, err := New(context.Background(), engine.Config{
		URL: testHost,
	})
	if !errors.Is(err, ErrRoomRequired) {
		t.Fatalf("err = %v, want ErrRoomRequired", err)
	}
}

func TestNewSucceeds(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   "https://" + testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
		Name:  "olcrtc-test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()
	caps := sess.Capabilities()
	if !caps.ByteStream || !caps.VideoTrack {
		t.Fatalf("Capabilities = %+v, want ByteStream && VideoTrack", caps)
	}
}

func TestSCTPBridgeKeepsLegacyMaxMessageSizeByDefault(t *testing.T) {
	t.Setenv("OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE", "")

	var sess Session
	sess.sctpBridge.Store(true)

	if got := sess.bridgeMaxMessageSize(); got != bridgeMaxMessageSize {
		t.Fatalf("bridgeMaxMessageSize() = %d, want legacy %d", got, bridgeMaxMessageSize)
	}
	if got, want := sess.ByteStreamMaxPayloadSize(), bridgeMaxMessageSize-bridgeFrameHeaderLen; got != want {
		t.Fatalf("ByteStreamMaxPayloadSize() = %d, want %d", got, want)
	}
}

func TestSCTPBridgeMaxMessageSizeEnvIsExplicitOptIn(t *testing.T) {
	t.Setenv("OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE", "1200")

	var sess Session
	sess.sctpBridge.Store(true)

	if got := sess.bridgeMaxMessageSize(); got != 1200 {
		t.Fatalf("bridgeMaxMessageSize() = %d, want explicit env override 1200", got)
	}
	if got, want := sess.ByteStreamMaxPayloadSize(), 1200-bridgeFrameHeaderLen; got != want {
		t.Fatalf("ByteStreamMaxPayloadSize() = %d, want %d", got, want)
	}
}

func TestSCTPBridgeInvalidEnvFallsBackToLegacyMaxMessageSize(t *testing.T) {
	t.Setenv("OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE", "8")

	var sess Session
	sess.sctpBridge.Store(true)

	if got := sess.bridgeMaxMessageSize(); got != bridgeMaxMessageSize {
		t.Fatalf("bridgeMaxMessageSize() = %d, want legacy %d", got, bridgeMaxMessageSize)
	}
}

func TestByteStreamNegotiatesPeerConnectionWithoutRequestingVideo(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:    testHost,
		Extra:  map[string]string{credentialKeyRoom: testRoom},
		OnData: func([]byte) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	if !js.shouldNegotiatePC() {
		t.Fatal("shouldNegotiatePC() = false for bytestream session")
	}
	if js.shouldRequestVideo() {
		t.Fatal("shouldRequestVideo() = true for bytestream-only session")
	}
}

func TestVideoSessionNegotiatesPeerConnectionAndRequestsVideo(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	if js.shouldNegotiatePC() {
		t.Fatal("shouldNegotiatePC() = true before bytestream/video is configured")
	}
	if err := js.AddVideoTrack(nil); err != nil {
		t.Fatalf("AddVideoTrack(nil): %v", err)
	}
	if !js.shouldNegotiatePC() {
		t.Fatal("shouldNegotiatePC() = false for video session")
	}
	if !js.shouldRequestVideo() {
		t.Fatal("shouldRequestVideo() = false for video session")
	}
}

func TestSendBeforeConnect(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:    testHost,
		Extra:  map[string]string{credentialKeyRoom: testRoom},
		OnData: func([]byte) {},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()
	if err := sess.Send([]byte("data")); !errors.Is(err, ErrBridgeNotReady) {
		t.Fatalf("Send err = %v, want ErrBridgeNotReady", err)
	}
}

func TestSendAfterClose(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := sess.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sess.Send([]byte("data")); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("Send err = %v, want ErrSessionClosed", err)
	}
}

func TestSanitiseNick(t *testing.T) {
	tests := []struct {
		raw  string
		want string
	}{
		{"alice", "alice"},
		{"Alice Smith", "Alice-Smith"},
		{"Конрад Олег", "Konrad-Oleg"},
		{"olcrtc-bot42", "olcrtc-bot42"},
		{"  bob  ", "bob"},
		{"$$$ %%%", ""},
		{"verylongnicknamethatexceedslimit", "verylongnicknamet"[:16]},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			if got := sanitiseNick(tc.raw); got != tc.want {
				t.Fatalf("sanitiseNick(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDeliverBridgeMessageMagicAndPeerLatch(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	var received [][]byte
	js.onData = func(b []byte) {
		received = append(received, append([]byte(nil), b...))
	}

	good := makeBridgeFrame(t, []byte("alpha"))
	bad := encodeForTest(t, []byte("alpha")) // no magic prefix

	// First valid frame from peerA latches the peer and is delivered.
	if !js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: good}), true) {
		t.Fatal("deliverBridgeMessage returned false on valid frame")
	}
	// Frame without magic is dropped.
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: bad}), true)
	// Frame from a different sender after latch is dropped even with magic.
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{rawFieldKey: good}), true)
	// Another frame from latched peer still flows.
	beta := makeBridgeFrame(t, []byte("beta"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: beta}), true)

	if len(received) != 2 {
		t.Fatalf("received frames = %d, want 2 (%q)", len(received), received)
	}
	if string(received[0]) != "alpha" || string(received[1]) != "beta" {
		t.Fatalf("received = %q, want [alpha beta]", received)
	}
}

func TestDeliverBridgeMessageWithPeerDataDoesNotLatchSinglePeer(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	got := make(map[string]string)
	js.onPeerData = func(peerID string, b []byte) {
		got[peerID] = string(b)
	}

	frameA := makeBridgeFrameForEpoch(t, 0x1111, 0, []byte("alpha"))
	frameB := makeBridgeFrameForEpoch(t, 0x2222, 0, []byte("beta"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: frameA}), true)
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{rawFieldKey: frameB}), true)

	if got["peerA"] != "alpha" || got["peerB"] != "beta" {
		t.Fatalf("peer data = %#v, want both peers delivered", got)
	}
}

func TestDeliverBridgeMessageDropsStalePeerEpoch(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	js.localEpoch.Store(0x2222)
	delivered := false
	js.onData = func([]byte) { delivered = true }

	stale := makeBridgeFrameForEpoch(t, 0x1111, 0xaaaa, []byte("old-smux"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: stale}), true)
	if delivered {
		t.Fatal("stale peer-epoch frame was delivered")
	}
}

func TestReconnectEpochAnnounceWithZeroPeerEpochIsAccepted(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	js.localEpoch.Store(0x2222)

	announce := makeBridgeFrameForEpoch(t, 0x1111, 0, nil)
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: announce}), true)
	if got := js.peerEpoch.Load(); got != 0x1111 {
		t.Fatalf("peerEpoch = 0x%08x, want announce epoch", got)
	}
}

// TestDeliverBridgeMessagePeerEpochChangeAcceptsFreshPeer locks in the 1.9.8
// design (149a521): a changed peer epoch is a deduplication marker, not a
// reconnect trigger. When the peer emits a fresh epoch, acceptEpochFrame must
// adopt it in place (peerEpoch updated) without signaling reconnect — the old
// "request reconnect on epoch change" behavior caused a reconnect ping-pong.
func TestDeliverBridgeMessagePeerEpochChangeAcceptsFreshPeer(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	js.localEpoch.Store(0x3333)
	js.SetShouldReconnect(func() bool { return true })
	var received [][]byte
	js.onData = func(b []byte) {
		received = append(received, append([]byte(nil), b...))
	}

	first := makeBridgeFrameForEpoch(t, 0x1111, 0, []byte("first"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: first}), true)
	changed := makeBridgeFrameForEpoch(t, 0x2222, 0x3333, nil)
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: changed}), true)

	if len(received) != 1 || string(received[0]) != "first" {
		t.Fatalf("received = %q, want only first payload", received)
	}
	if got := js.peerEpoch.Load(); got != 0x2222 {
		t.Fatalf("peerEpoch = 0x%08x, want fresh epoch 0x2222", got)
	}
	select {
	case <-js.reconnectCh:
		t.Fatal("peer epoch change must not request reconnect (1.9.8 design)")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestBridgeCloseRequestsReconnect(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	var ended string
	js.SetEndedCallback(func(reason string) { ended = reason })
	js.SetShouldReconnect(func() bool { return true })

	if js.deliverBridgeMessage(j.BridgeMessage{}, false) {
		t.Fatal("deliverBridgeMessage returned true on closed bridge")
	}
	select {
	case <-js.reconnectCh:
	case <-time.After(time.Second):
		t.Fatal("bridge close did not request reconnect")
	}
	if ended != "" {
		t.Fatalf("ended = %q, want empty", ended)
	}
}

func TestBridgeCloseEndsWhenReconnectDisabled(t *testing.T) {
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = sess.Close() }()

	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	var ended string
	js.SetEndedCallback(func(reason string) { ended = reason })
	js.SetShouldReconnect(func() bool { return false })

	if js.deliverBridgeMessage(j.BridgeMessage{}, false) {
		t.Fatal("deliverBridgeMessage returned true on closed bridge")
	}
	if ended != "jitsi bridge closed" {
		t.Fatalf("ended = %q, want bridge close reason", ended)
	}
}

func TestEngineRegistration(t *testing.T) {
	if _, err := engine.New(context.Background(), "jitsi", engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	}); err != nil {
		t.Fatalf("engine.New(jitsi) = %v, want nil", err)
	}
}
