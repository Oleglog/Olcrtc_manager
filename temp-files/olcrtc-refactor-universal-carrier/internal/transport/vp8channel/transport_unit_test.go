package vp8channel

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

var (
	errVP8UnitBoom     = errors.New("boom")
	errVP8UnitOpenBoom = errors.New("open boom")
)

type fakeVideoSession struct {
	stream *fakeVideoStream
	err    error
}

func TestSampleIntervalWithBatch(t *testing.T) {
	tr := &streamTransport{
		frameInterval: time.Second / 60,
		batchSize:     64,
	}
	want := time.Second / 60 / 64
	if got := tr.sampleInterval(); got != want {
		t.Fatalf("sampleInterval() = %v, want %v", got, want)
	}

	tr.batchSize = 1
	if got := tr.sampleInterval(); got != tr.frameInterval {
		t.Fatalf("sampleInterval(batch=1) = %v, want %v", got, tr.frameInterval)
	}
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
func TestNewConnectSendCallbacksFeaturesAndClose(t *testing.T) {
	stream := &fakeVideoStream{canSend: true}
	name := "vp8channel-unit-new"
	carrier.Register(name, func(context.Context, carrier.Config) (carrier.Session, error) {
		return &fakeVideoSession{stream: stream}, nil
	})

	trIface, err := New(context.Background(), transport.Config{
		Carrier:      name,
		DeviceID:     "client",
		VP8FPS:       30,
		VP8BatchSize: 1,
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
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	if tr.kcp == nil || !tr.writerUp.Load() {
		t.Fatal("Connect() should eagerly initialize kcp and writer")
	}
	tr.SetReconnectCallback(func() {})
	tr.SetShouldReconnect(func() bool { return true })
	tr.SetEndedCallback(func(string) {})
	tr.WatchConnection(context.Background())
	if stream.reconnect == nil || stream.should == nil || stream.ended == nil || !stream.watched {
		t.Fatal("callbacks/watch were not forwarded")
	}

	peerEpoch := uint32(0x200)
	firstFrame := make([]byte, epochHdrLen+4)
	copy(firstFrame, vp8Keepalive)
	binary.BigEndian.PutUint32(firstFrame[tokenOff:epochOff], tr.bindingToken)
	binary.BigEndian.PutUint32(firstFrame[epochOff:crcOff], peerEpoch)
	binary.BigEndian.PutUint32(firstFrame[crcOff:epochHdrLen], epochCRC(tr.bindingToken, peerEpoch))
	copy(firstFrame[epochHdrLen:], []byte("data"))
	tr.handleIncomingFrame(firstFrame)
	if tr.kcp == nil {
		t.Fatal("kcp not initialized after first peer frame")
	}

	if !tr.CanSend() {
		t.Fatal("CanSend() = false, want true")
	}
	if features := tr.Features(); !features.Reliable || !features.Ordered || !features.MessageOriented || features.MaxPayloadSize == 0 { //nolint:lll // long test description
		t.Fatalf("Features() = %+v", features)
	}
	if err := tr.Send([]byte("payload")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	tr.drainOutbound()
	if err := tr.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := tr.Send([]byte("closed")); !errors.Is(err, ErrTransportClosed) {
		t.Fatalf("Send(closed) error = %v, want %v", err, ErrTransportClosed)
	}
}

func TestNewErrorPaths(t *testing.T) {
	carrier.Register("vp8channel-create-fails", func(context.Context, carrier.Config) (carrier.Session, error) {
		return nil, errVP8UnitBoom
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "vp8channel-create-fails"}); err == nil || err.Error() != "create carrier transport: boom" { //nolint:lll // long test description
		t.Fatalf("New() error = %v", err)
	}

	carrier.Register("vp8channel-no-video", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &nonVideoSession{}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "vp8channel-no-video"}); !errors.Is(err, ErrVideoTrackUnsupported) { //nolint:lll // long test description
		t.Fatalf("New() error = %v, want %v", err, ErrVideoTrackUnsupported)
	}

	carrier.Register("vp8channel-open-fails", func(context.Context, carrier.Config) (carrier.Session, error) {
		return &fakeVideoSession{err: errVP8UnitOpenBoom}, nil
	})
	if _, err := New(context.Background(), transport.Config{Carrier: "vp8channel-open-fails"}); err == nil || err.Error() != "open video track: open boom" { //nolint:lll // long test description
		t.Fatalf("New() error = %v", err)
	}
}

//nolint:cyclop // table-driven test naturally has many branches
func TestEpochHeaderTokenAndOutboundCapacity(t *testing.T) {
	tr := &streamTransport{
		stream:       &fakeVideoStream{canSend: true},
		outbound:     make(chan []byte, 10),
		closeCh:      make(chan struct{}),
		writerDone:   make(chan struct{}),
		bindingToken: bindingToken("client"),
		localEpoch:   0x01020304,
	}

	hdr := tr.epochHeader()
	if !bytes.Equal(hdr[:tokenOff], vp8Keepalive) ||
		binary.BigEndian.Uint32(hdr[tokenOff:epochOff]) != tr.bindingToken ||
		binary.BigEndian.Uint32(hdr[epochOff:crcOff]) != tr.localEpoch ||
		binary.BigEndian.Uint32(hdr[crcOff:epochHdrLen]) != epochCRC(tr.bindingToken, tr.localEpoch) {
		t.Fatalf("epochHeader() = %x", hdr)
	}
	if bindingToken("") == 0 || randomEpoch() == 0 {
		t.Fatal("bindingToken/randomEpoch returned zero")
	}

	rt, err := startKCP(tr.outbound, nil, tr.epochHeader())
	if err != nil {
		t.Fatalf("startKCP: %v", err)
	}
	defer rt.close()
	tr.kcpMu.Lock()
	tr.kcp = rt
	tr.kcpMu.Unlock()

	for len(tr.outbound) < cap(tr.outbound)*canSendHighWatermark/100 {
		tr.outbound <- []byte("queued")
	}
	if tr.CanSend() {
		t.Fatal("CanSend() = true at high watermark")
	}
	tr.drainOutbound()
	if !tr.CanSend() {
		t.Fatal("CanSend() = false after drain")
	}
	tr.closed.Store(true)
	if tr.CanSend() {
		t.Fatal("CanSend() = true after closed")
	}
}

func TestVP8FrameStateAssemblesAndRejectsCorruptFrames(t *testing.T) {
	frame := append(append([]byte(nil), vp8Keepalive...), bytes.Repeat([]byte{0x01}, epochHdrLen-len(vp8Keepalive))...)
	var state vp8FrameState

	got := state.processRTPPacket(&rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 10, Marker: true},
		Payload: append([]byte{0x10}, frame...),
	})
	if !bytes.Equal(got, frame) {
		t.Fatalf("single-packet frame = %x, want %x", got, frame)
	}

	state = vp8FrameState{}
	if got := state.processRTPPacket(&rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 20},
		Payload: append([]byte{0x10}, frame[:4]...),
	}); got != nil {
		t.Fatalf("partial frame = %x, want nil", got)
	}
	got = state.processRTPPacket(&rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 21, Marker: true},
		Payload: append([]byte{0x00}, frame[4:]...),
	})
	if !bytes.Equal(got, frame) {
		t.Fatalf("fragmented frame = %x, want %x", got, frame)
	}

	state = vp8FrameState{}
	_ = state.processRTPPacket(&rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 30},
		Payload: append([]byte{0x10}, frame[:4]...),
	})
	if got := state.processRTPPacket(&rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 32, Marker: true},
		Payload: append([]byte{0x00}, frame[4:]...),
	}); got != nil {
		t.Fatalf("frame after sequence gap = %x, want nil", got)
	}

	state = vp8FrameState{}
	if got := state.processRTPPacket(&rtp.Packet{
		Header:  rtp.Header{SequenceNumber: 40, Marker: true},
		Payload: []byte{},
	}); got != nil {
		t.Fatalf("bad vp8 payload = %x, want nil", got)
	}
}

//nolint:cyclop // table-driven test naturally has many branches
func TestHandleIncomingFrameEpochFilteringAndReconnect(t *testing.T) {
	called := 0
	tr := &streamTransport{
		stream:       &fakeVideoStream{canSend: true},
		outbound:     make(chan []byte, 16),
		closeCh:      make(chan struct{}),
		writerDone:   make(chan struct{}),
		bindingToken: bindingToken("client"),
		localEpoch:   0x100,
		onData:       func([]byte) { called++ },
	}
	defer func() {
		_ = tr.Close()
	}()

	mkFrame := func(token, epoch uint32, payload []byte) []byte {
		frame := make([]byte, epochHdrLen+len(payload))
		copy(frame, vp8Keepalive)
		binary.BigEndian.PutUint32(frame[tokenOff:epochOff], token)
		binary.BigEndian.PutUint32(frame[epochOff:crcOff], epoch)
		binary.BigEndian.PutUint32(frame[crcOff:epochHdrLen], epochCRC(token, epoch))
		copy(frame[epochHdrLen:], payload)
		return frame
	}

	tr.handleIncomingFrame(mkFrame(bindingToken("other"), 1, []byte("x")))
	tr.handleIncomingFrame(mkFrame(tr.bindingToken, tr.localEpoch, []byte("self")))
	if tr.hadPeer.Load() || called != 0 {
		t.Fatal("filtered frames changed peer state")
	}

	tr.handleIncomingFrame(mkFrame(tr.bindingToken, 1, nil))
	if !tr.hadPeer.Load() || tr.peerEpoch.Load() != 1 {
		t.Fatalf("peer state after first frame: had=%v epoch=%d", tr.hadPeer.Load(), tr.peerEpoch.Load())
	}

	reconnected := false
	tr.SetReconnectCallback(func() { reconnected = true })
	stream, ok := tr.stream.(*fakeVideoStream)
	if !ok {
		t.Fatalf("stream type = %T, want *fakeVideoStream", tr.stream)
	}
	if stream.reconnect == nil {
		t.Fatal("SetReconnectCallback did not install stream callback")
	}
	stream.reconnect()
	if !reconnected || tr.kcp == nil {
		t.Fatalf("stream reconnect did not reset/callback: reconnected=%v kcp=%v", reconnected, tr.kcp)
	}
	reconnected = false
	tr.handleIncomingFrame(mkFrame(tr.bindingToken, 2, []byte("after-restart")))
	if !reconnected || tr.peerEpoch.Load() != 2 || tr.kcp == nil {
		t.Fatalf("epoch change did not reset/reconnect: reconnected=%v epoch=%d kcp=%v", reconnected, tr.peerEpoch.Load(), tr.kcp) //nolint:lll // long test description
	}
}
