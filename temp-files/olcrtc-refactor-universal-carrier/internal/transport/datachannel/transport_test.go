package datachannel

import (
	"context"
	"errors"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
)

var (
	errDCBoom        = errors.New("boom")
	errDCOpenBoom    = errors.New("open boom")
	errDCConnectBoom = errors.New("connect boom")
	errDCSendBoom    = errors.New("send boom")
	errDCCloseBoom   = errors.New("close boom")
)

type stubSession struct {
	stream    carrier.ByteStream
	streamErr error
}

func (s *stubSession) Capabilities() carrier.Capabilities {
	return carrier.Capabilities{ByteStream: true}
}
func (s *stubSession) OpenByteStream() (carrier.ByteStream, error) {
	if s.streamErr != nil {
		return nil, s.streamErr
	}
	return s.stream, nil
}

type nonByteStreamSession struct{}

func (s *nonByteStreamSession) Capabilities() carrier.Capabilities { return carrier.Capabilities{} }

type stubByteStream struct {
	connectErr error
	sendErr    error
	closeErr   error
	canSend    bool

	connectCalled bool
	sent          []byte
	watched       bool
	reconnectCB   func()
	shouldFn      func() bool
	endedCB       func(string)
}

func (s *stubByteStream) Connect(context.Context) error { s.connectCalled = true; return s.connectErr }
func (s *stubByteStream) Send(data []byte) error {
	s.sent = append([]byte(nil), data...)
	return s.sendErr
}
func (s *stubByteStream) Close() error                      { return s.closeErr }
func (s *stubByteStream) SetReconnectCallback(cb func())    { s.reconnectCB = cb }
func (s *stubByteStream) SetShouldReconnect(fn func() bool) { s.shouldFn = fn }
func (s *stubByteStream) SetEndedCallback(cb func(string))  { s.endedCB = cb }
func (s *stubByteStream) WatchConnection(context.Context)   { s.watched = true }
func (s *stubByteStream) CanSend() bool                     { return s.canSend }

//nolint:cyclop // table-driven test naturally has many branches
func TestNewAndFeatures(t *testing.T) {
	stream := &stubByteStream{canSend: true}
	carrier.Register("datachannel-test-new-and-features", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &stubSession{stream: stream}, nil
	})

	tr, err := New(context.Background(), transport.Config{Carrier: "datachannel-test-new-and-features"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !stream.connectCalled {
		t.Fatal("Connect() was not forwarded")
	}
	if err := tr.Send([]byte("payload")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if string(stream.sent) != "payload" {
		t.Fatalf("Send() forwarded %q, want payload", stream.sent)
	}
	tr.SetReconnectCallback(func() {})
	tr.SetShouldReconnect(func() bool { return true })
	tr.SetEndedCallback(func(string) {})
	tr.WatchConnection(context.Background())
	if stream.reconnectCB == nil || stream.shouldFn == nil || stream.endedCB == nil || !stream.watched {
		t.Fatal("callbacks/watch were not forwarded")
	}
	if !tr.CanSend() {
		t.Fatal("CanSend() = false, want true")
	}

	features := tr.Features()
	if !features.Reliable || !features.Ordered || !features.MessageOriented || features.MaxPayloadSize != defaultMaxPayloadSize { //nolint:lll // long test description
		t.Fatalf("Features() = %+v", features)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewErrorPaths(t *testing.T) {
	carrier.Register("datachannel-fail-create", func(context.Context, carrier.Config) (carrier.Session, error) {
		return nil, errDCBoom
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "datachannel-fail-create"}); err == nil || err.Error() != "create carrier transport: boom" { //nolint:lll // long test description
		t.Fatalf("New() error = %v", err)
	}

	carrier.Register("datachannel-no-stream", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &nonByteStreamSession{}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "datachannel-no-stream"}); !errors.Is(err, carrier.ErrByteStreamUnsupported) { //nolint:lll // long test description
		t.Fatalf("New() error = %v, want %v", err, carrier.ErrByteStreamUnsupported)
	}

	carrier.Register("datachannel-open-stream-fails", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &stubSession{streamErr: errDCOpenBoom}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "datachannel-open-stream-fails"}); err == nil || err.Error() != "open byte stream: open boom" { //nolint:lll // long test description
		t.Fatalf("New() error = %v", err)
	}
}

func TestStreamTransportWrapsErrors(t *testing.T) {
	tr := &streamTransport{stream: &stubByteStream{
		connectErr: errDCConnectBoom,
		sendErr:    errDCSendBoom,
		closeErr:   errDCCloseBoom,
	}}

	if err := tr.Connect(context.Background()); err == nil || err.Error() != "stream connect: connect boom" {
		t.Fatalf("Connect() error = %v", err)
	}
	if err := tr.Send([]byte("x")); err == nil || err.Error() != "stream send: send boom" {
		t.Fatalf("Send() error = %v", err)
	}
	if err := tr.Close(); err == nil || err.Error() != "stream close: close boom" {
		t.Fatalf("Close() error = %v", err)
	}
}
