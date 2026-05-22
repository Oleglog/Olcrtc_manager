package seichannel

import (
	"context"
	"errors"
	"hash/crc32"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
	enginebuiltin "github.com/openlibrecommunity/olcrtc/internal/engine/builtin"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/openlibrecommunity/olcrtc/internal/transport/common"
	"github.com/pion/webrtc/v4"
)

var errBoom = errors.New("boom")

// fakeVideoStream is the stub implementation of the videoSession interface
// the seichannel transport consumes after engine.Session adaptation.
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
func (s *fakeVideoStream) Reconnect(string)                  {}
func (s *fakeVideoStream) SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	s.trackCB = cb
}

// fakeEngineSession implements engine.Session and engine.VideoTrackCapable so
// it can be returned by enginebuiltin.Open in tests. It wraps a fakeVideoStream
// for the video-track methods the real engine session exposes.
type fakeEngineSession struct {
	stream  *fakeVideoStream
	noVideo bool
}

func (s *fakeEngineSession) Capabilities() engine.Capabilities {
	if s.noVideo {
		return engine.Capabilities{}
	}
	return engine.Capabilities{VideoTrack: true}
}
func (s *fakeEngineSession) Connect(ctx context.Context) error { return s.stream.Connect(ctx) }
func (s *fakeEngineSession) Send([]byte) error                 { return nil }
func (s *fakeEngineSession) Close() error                      { return s.stream.Close() }
func (s *fakeEngineSession) SetReconnectCallback(cb func(*webrtc.DataChannel)) {
	s.stream.SetReconnectCallback(func() {
		if cb != nil {
			cb(nil)
		}
	})
}
func (s *fakeEngineSession) SetShouldReconnect(fn func() bool) { s.stream.SetShouldReconnect(fn) }
func (s *fakeEngineSession) SetEndedCallback(cb func(string))  { s.stream.SetEndedCallback(cb) }
func (s *fakeEngineSession) WatchConnection(ctx context.Context) {
	s.stream.WatchConnection(ctx)
}
func (s *fakeEngineSession) CanSend() bool                            { return s.stream.CanSend() }
func (s *fakeEngineSession) GetSendQueue() chan []byte                { return nil }
func (s *fakeEngineSession) GetBufferedAmount() uint64                { return 0 }
func (s *fakeEngineSession) Reconnect(string)                         {}
func (s *fakeEngineSession) AddVideoTrack(t webrtc.TrackLocal) error  { return s.stream.AddTrack(t) }
func (s *fakeEngineSession) SetVideoTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	s.stream.SetTrackHandler(cb)
}

//nolint:cyclop // table-driven test naturally has many branches
func TestNewConnectCallbacksAndFeatures(t *testing.T) {
	stream := &fakeVideoStream{canSend: true}
	name := "seichannel-unit-new"
	enginebuiltin.Register(name, func(context.Context, enginebuiltin.Config) (engine.Session, error) {
		return &fakeEngineSession{stream: stream}, nil
	})

	trIface, err := New(t.Context(), transport.Config{
		Carrier: name,
		Options: Options{
			FPS:          40,
			BatchSize:    3,
			FragmentSize: 512,
			AckTimeoutMS: 1500,
		},
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
	enginebuiltin.Register("seichannel-create-fails", func(context.Context, enginebuiltin.Config) (engine.Session, error) {
		return nil, errBoom
	})
	_, err := New(context.Background(), transport.Config{Carrier: "seichannel-create-fails"})
	if err == nil || err.Error() != "open engine session: boom" {
		t.Fatalf("New() error = %v", err)
	}

	enginebuiltin.Register("seichannel-no-video", func(context.Context, enginebuiltin.Config) (engine.Session, error) {
		return &fakeEngineSession{stream: &fakeVideoStream{}, noVideo: true}, nil
	})
	_, err = New(context.Background(), transport.Config{Carrier: "seichannel-no-video"})
	if !errors.Is(err, ErrVideoTrackUnsupported) {
		t.Fatalf("New() error = %v, want %v", err, ErrVideoTrackUnsupported)
	}
}

func TestSendAckAndClosePaths(t *testing.T) {
	tr := &streamTransport{
		stream:      &fakeVideoStream{canSend: true},
		outbound:    make(chan []byte, 8),
		outboundAck: make(chan []byte, 8),
		closeCh:     make(chan struct{}),
		writerDone:  make(chan struct{}),
		acks:        common.NewAckRegistry(),
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
