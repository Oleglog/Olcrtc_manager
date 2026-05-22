package videochannel

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
	errVideoUnitBoom     = errors.New("boom")
	errVideoUnitOpenBoom = errors.New("open boom")
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

func (s *fakeVideoStream) Connect(context.Context) error     { return nil }
func (s *fakeVideoStream) Close() error                      { s.closed = true; return s.closeErr }
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
func TestNewCallbacksFeaturesAndClose(t *testing.T) {
	stream := &fakeVideoStream{canSend: true}
	name := "videochannel-unit-new"
	carrier.Register(name, func(context.Context, carrier.Config) (carrier.Session, error) {
		return &fakeVideoSession{stream: stream}, nil
	})

	trIface, err := New(context.Background(), transport.Config{
		Carrier:         name,
		VideoWidth:      320,
		VideoHeight:     240,
		VideoFPS:        30,
		VideoBitrate:    "1M",
		VideoCodec:      "qrcode",
		VideoTileModule: -1,
		VideoTileRS:     -1,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	tr, ok := trIface.(*streamTransport)
	if !ok {
		t.Fatalf("transport type = %T, want *streamTransport", trIface)
	}
	if !stream.trackAdded || stream.trackCB == nil {
		t.Fatal("New() did not attach track and handler")
	}
	tr.SetReconnectCallback(func() {})
	tr.SetShouldReconnect(func() bool { return true })
	tr.SetEndedCallback(func(string) {})
	tr.WatchConnection(context.Background())
	if stream.reconnect == nil || stream.should == nil || stream.ended == nil || !stream.watched {
		t.Fatal("callbacks/watch were not forwarded")
	}
	if !tr.CanSend() {
		t.Fatal("CanSend() = false, want true")
	}
	if features := tr.Features(); !features.Reliable || !features.Ordered || !features.MessageOriented || features.MaxPayloadSize == 0 { //nolint:lll // long test description
		t.Fatalf("Features() = %+v", features)
	}
	if tr.videoQRSize != defaultFragmentSize || tr.videoTileModule != 4 || tr.videoTileRS != 20 {
		t.Fatalf("defaults qr=%d tileModule=%d tileRS=%d", tr.videoQRSize, tr.videoTileModule, tr.videoTileRS)
	}
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewErrorPaths(t *testing.T) {
	carrier.Register("videochannel-create-fails", func(context.Context, carrier.Config) (carrier.Session, error) {
		return nil, errVideoUnitBoom
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "videochannel-create-fails"}); err == nil || err.Error() != "create carrier transport: boom" { //nolint:lll // long test description
		t.Fatalf("New() error = %v", err)
	}

	carrier.Register("videochannel-no-video", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &nonVideoSession{}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "videochannel-no-video"}); !errors.Is(err, ErrVideoTrackUnsupported) { //nolint:lll // long test description
		t.Fatalf("New() error = %v, want %v", err, ErrVideoTrackUnsupported)
	}

	carrier.Register("videochannel-open-fails", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &fakeVideoSession{err: errVideoUnitOpenBoom}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "videochannel-open-fails"}); err == nil || err.Error() != "open video track: open boom" { //nolint:lll // long test description
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
		videoQRSize: 4,
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

//nolint:cyclop // table-driven test naturally has many branches
func TestOutboundPriorityRenderAndClosedEnqueue(t *testing.T) {
	tr := &streamTransport{
		stream:          &fakeVideoStream{canSend: true},
		outbound:        make(chan []byte, 2),
		outboundAck:     make(chan []byte, 2),
		closeCh:         make(chan struct{}),
		writerDone:      make(chan struct{}),
		videoW:          16,
		videoH:          16,
		videoQRRecovery: "highest",
		videoCodec:      "qrcode",
		videoTileModule: 4,
		videoTileRS:     20,
	}

	if err := tr.enqueueFrame([]byte("data"), false); err != nil {
		t.Fatalf("enqueueFrame(data) error = %v", err)
	}
	if err := tr.enqueueFrame([]byte("ack"), true); err != nil {
		t.Fatalf("enqueueFrame(ack) error = %v", err)
	}
	if got, ok := tr.nextOutboundFrame(); !ok || string(got) != "ack" {
		t.Fatalf("first nextOutboundFrame() = %q/%v, want ack/true", got, ok)
	}
	if got, ok := tr.nextOutboundFrame(); !ok || string(got) != "data" {
		t.Fatalf("second nextOutboundFrame() = %q/%v, want data/true", got, ok)
	}
	if got, ok := tr.nextOutboundFrame(); !ok || got != nil {
		t.Fatalf("idle nextOutboundFrame() = %q/%v, want nil/true", got, ok)
	}

	idle, err := tr.renderFrame(nil)
	if err != nil {
		t.Fatalf("renderFrame(nil) error = %v", err)
	}
	if len(idle) != tr.videoW*tr.videoH {
		t.Fatalf("idle frame len = %d, want %d", len(idle), tr.videoW*tr.videoH)
	}
	if features := tr.Features(); features.MaxPayloadSize != defaultMaxPayloadSize {
		t.Fatalf("Features() = %+v", features)
	}

	tr.videoQRSize = defaultMaxPayloadSize
	if features := tr.Features(); features.MaxPayloadSize <= defaultMaxPayloadSize {
		t.Fatalf("Features(large qr) = %+v", features)
	}

	tr.closed.Store(true)
	if err := tr.enqueueFrame([]byte("closed"), false); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("enqueueFrame(closed) error = %v, want %v", err, ErrTransportClosed)
	}
}

func TestNextOutboundFrameStopsWhenClosed(t *testing.T) {
	tr := &streamTransport{
		outbound:    make(chan []byte, 1),
		outboundAck: make(chan []byte, 1),
		closeCh:     make(chan struct{}),
	}
	close(tr.closeCh)
	if got, ok := tr.nextOutboundFrame(); ok || got != nil {
		t.Fatalf("nextOutboundFrame(closed) = %q/%v, want nil/false", got, ok)
	}
}
