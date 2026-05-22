package direct

import (
	"context"
	"errors"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/link"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
)

var (
	errDirectBoom        = errors.New("boom")
	errDirectConnectBoom = errors.New("connect boom")
	errDirectSendBoom    = errors.New("send boom")
	errDirectCloseBoom   = errors.New("close boom")
)

type stubTransport struct {
	connectErr error
	sendErr    error
	closeErr   error
	canSend    bool

	connectCalled bool
	sendData      []byte
	watched       bool
	reconnectCB   func()
	shouldFn      func() bool
	endedCB       func(string)
}

func (s *stubTransport) Connect(context.Context) error {
	s.connectCalled = true
	return s.connectErr
}
func (s *stubTransport) Send(data []byte) error {
	s.sendData = append([]byte(nil), data...)
	return s.sendErr
}
func (s *stubTransport) Close() error { return s.closeErr }
func (s *stubTransport) SetReconnectCallback(cb func()) {
	s.reconnectCB = cb
}
func (s *stubTransport) SetShouldReconnect(fn func() bool) { s.shouldFn = fn }
func (s *stubTransport) SetEndedCallback(cb func(string))  { s.endedCB = cb }
func (s *stubTransport) WatchConnection(context.Context)   { s.watched = true }
func (s *stubTransport) CanSend() bool                     { return s.canSend }
func (s *stubTransport) Features() transport.Features      { return transport.Features{} }

//nolint:cyclop // table-driven test naturally has many branches
func TestNewForwardsConfigAndMethods(t *testing.T) {
	name := "direct-test-forward"
	var seen transport.Config
	tr := &stubTransport{canSend: true}
	transport.Register(name, func(_ context.Context, cfg transport.Config) (transport.Transport, error) {
		seen = cfg
		return tr, nil
	})

	ln, err := New(context.Background(), link.Config{
		Transport:       name,
		Carrier:         "carrier",
		RoomURL:         "room",
		DeviceID:        "client",
		Name:            "peer",
		DNSServer:       "1.1.1.1:53",
		ProxyAddr:       "127.0.0.1",
		ProxyPort:       1080,
		VideoWidth:      640,
		VideoHeight:     480,
		VideoFPS:        30,
		VideoBitrate:    "1M",
		VideoHW:         "none",
		VideoQRSize:     4,
		VideoQRRecovery: "low",
		VideoCodec:      "qrcode",
		VideoTileModule: 3,
		VideoTileRS:     20,
		VP8FPS:          25,
		VP8BatchSize:    8,
		Traffic:         transport.TrafficConfig{MaxPayloadSize: 4096},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if seen.DeviceID != "client" || seen.ProxyPort != 1080 || seen.VideoTileRS != 20 || seen.VP8BatchSize != 8 ||
		seen.Traffic.MaxPayloadSize != 4096 {
		t.Fatalf("forwarded config = %+v", seen)
	}

	if err := ln.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !tr.connectCalled {
		t.Fatal("Connect() was not forwarded")
	}

	if err := ln.Send([]byte("payload")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if string(tr.sendData) != "payload" {
		t.Fatalf("Send() forwarded %q, want payload", tr.sendData)
	}

	ln.SetReconnectCallback(func() {})
	ln.SetShouldReconnect(func() bool { return true })
	ln.SetEndedCallback(func(string) {})
	ln.WatchConnection(context.Background())
	if tr.reconnectCB == nil || tr.shouldFn == nil || tr.endedCB == nil || !tr.watched {
		t.Fatal("callbacks/watch were not forwarded")
	}
	if !ln.CanSend() {
		t.Fatal("CanSend() = false, want true")
	}
	provider, ok := ln.(link.FeaturesProvider)
	if !ok {
		t.Fatalf("New() type = %T, want link.FeaturesProvider", ln)
	}
	if features := provider.Features(); features.MaxPayloadSize != 4096 {
		t.Fatalf("Features() = %+v, want shaped max payload 4096", features)
	}
}

func TestNewWrapsFactoryError(t *testing.T) {
	name := "direct-test-error"
	transport.Register(name, func(context.Context, transport.Config) (transport.Transport, error) {
		return nil, errDirectBoom
	})

	_, err := New(context.Background(), link.Config{Transport: name})
	if err == nil || err.Error() != "create transport for direct link: boom" {
		t.Fatalf("New() error = %v", err)
	}
}

func TestDirectLinkWrapsTransportErrors(t *testing.T) {
	ln := &directLink{transport: &stubTransport{
		connectErr: errDirectConnectBoom,
		sendErr:    errDirectSendBoom,
		closeErr:   errDirectCloseBoom,
	}}

	if err := ln.Connect(context.Background()); err == nil || err.Error() != "transport connect: connect boom" {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := ln.Send([]byte("x")); err == nil || err.Error() != "transport send: send boom" {
		t.Fatalf("Send() error = %v", err)
	}
	if err := ln.Close(); err == nil || err.Error() != "transport close: close boom" {
		t.Fatalf("Close() error = %v", err)
	}
}
