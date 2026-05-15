package seichannel

import (
	"context"
	"errors"
	"hash/crc32"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/pion/webrtc/v4"
)

var (
	errBoom     = errors.New("boom")
	errOpenBoom = errors.New("open boom")
)

type fakeVideoSession struct {
	stream *fakeVideoStream
	err    error
}

func (s *fakeVideoSession) Capabilities() carrier.Capabilities {
	return carrier.Capabilities{VideoTrack: true}
}
func (s *fakeVideoSession) OpenVideoTrack() (carrier.VideoTrack, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.stream, nil
}

type fakeVideoStream struct {
	connectErr error
	closeErr   error
	canSend    bool

	trackAdded bool
	trackCB    func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
	reconnect  func()
	should     func() bool
	ended      func(string)
	watched    bool
	closed     bool
}

func (s *fakeVideoStream) Connect(context.Context) error { return s.connectErr }
func (s *fakeVideoStream) Close() error {
	s.closed = true
	return s.closeErr
}
func (s *fakeVideoStream) SetReconnectCallback(cb func())    { s.reconnect = cb }
func (s *fakeVideoStream) SetShouldReconnect(fn func() bool) { s.should = fn }
func (s *fakeVideoStream) SetEndedCallback(cb func(string))  { s.ended = cb }
func (s *fakeVideoStream) WatchConnection(context.Context)   { s.watched = true }
func (s *fakeVideoStream) CanSend() bool                     { return s.canSend }
func (s *fakeVideoStream) AddTrack(webrtc.TrackLocal) error  { s.trackAdded = true; return nil }
func (s *fakeVideoStream) SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	s.trackCB = cb
}

type nonVideoSession struct{}

func (s *nonVideoSession) Capabilities() carrier.Capabilities { return carrier.Capabilities{} }

//nolint:cyclop // table-driven test naturally has many branches
func TestNewConnectCallbacksAndFeatures(t *testing.T) {
	stream := &fakeVideoStream{canSend: true}
	name := "seichannel-unit-new"
	carrier.Register(name, func(context.Context, carrier.Config) (carrier.Session, error) {
		return &fakeVideoSession{stream: stream}, nil
	})

	trIface, err := New(t.Context(), transport.Config{
		Carrier:         name,
		SEIFPS:          40,
		SEIBatchSize:    3,
		SEIFragmentSize: 512,
		SEIAckTimeoutMS: 1500,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tr, ok := trIface.(*streamTransport)
	if !ok {
		t.Fatalf("New() returned %T, want *streamTransport", trIface)
	}
	if !stream.trackAdded || stream.trackCB == nil {
		t.Fatal("New() did not attach track and handler")
	}
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if !tr.writerUp.Load() {
		t.Fatal("Connect() did not start writer")
	}
	tr.SetReconnectCallback(func() {})
	tr.SetShouldReconnect(func() bool { return true })
	tr.SetEndedCallback(func(string) {})
	tr.WatchConnection(context.Background())
	if stream.reconnect == nil || stream.should == nil || stream.ended == nil || !stream.watched {
		t.Fatal("callbacks/watch were not forwarded")
	}
	if tr.CanSend() {
		t.Fatal("CanSend() = true before peer hello")
	}
	tr.handleSample(buildVideoAccessUnit(encodeHelloFrame()))
	if !tr.CanSend() {
		t.Fatal("CanSend() = false after peer hello")
	}
	if features := tr.Features(); !features.Reliable || !features.Ordered || !features.MessageOriented || features.MaxPayloadSize == 0 { //nolint:lll // long test description
		t.Fatalf("Features() = %+v", features)
	}
	if tr.fragmentSize != 512 || tr.batchSize != 3 || tr.frameInterval != 25*time.Millisecond ||
		tr.ackTimeout != 1500*time.Millisecond {
		t.Fatalf("seichannel settings fragment=%d batch=%d interval=%v ack=%v",
			tr.fragmentSize, tr.batchSize, tr.frameInterval, tr.ackTimeout)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewErrorPaths(t *testing.T) {
	carrier.Register("seichannel-create-fails", func(context.Context, carrier.Config) (carrier.Session, error) {
		return nil, errBoom
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "seichannel-create-fails"}); err == nil || err.Error() != "create carrier transport: boom" { //nolint:lll // long test description
		t.Fatalf("New() error = %v", err)
	}

	carrier.Register("seichannel-no-video", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &nonVideoSession{}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "seichannel-no-video"}); !errors.Is(err, ErrVideoTrackUnsupported) { //nolint:lll // long test description
		t.Fatalf("New() error = %v, want %v", err, ErrVideoTrackUnsupported)
	}

	carrier.Register("seichannel-open-fails", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &fakeVideoSession{err: errOpenBoom}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "seichannel-open-fails"}); err == nil || err.Error() != "open video track: open boom" { //nolint:lll // long test description
		t.Fatalf("New() error = %v", err)
	}
}

func TestSendAckAndClosePaths(t *testing.T) {
	tr := &streamTransport{
		stream:      &fakeVideoStream{canSend: true},
		outbound:    make(chan []byte, 8),
		outboundAck: make(chan []byte, 8),
		closeCh:     make(chan struct{}),
		writerDone:  make(chan struct{}),
		ackWaiters:  make(map[uint32]chan uint32),
	}

	done := make(chan error, 1)
	payload := []byte("payload")
	go func() { done <- tr.Send(payload) }()

	select {
	case frame := <-tr.outbound:
		decoded, err := decodeTransportFrame(frame)
		if err != nil {
			t.Fatalf("decodeTransportFrame() error = %v", err)
		}
		tr.resolveAck(decoded.seq, crc32.ChecksumIEEE(payload))
	case <-time.After(time.Second):
		t.Fatal("Send() did not enqueue frame")
	}

	if err := <-done; err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := tr.Send([]byte("closed")); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Send(closed) error = %v, want %v", err, ErrTransportClosed)
	}
}
