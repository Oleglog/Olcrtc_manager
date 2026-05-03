package vp8channel

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	defaultMaxPayloadSize = 60 * 1024
	defaultConnectTimeout = 60 * time.Second
	rtpBufSize            = 65536
	outboundQueueSize     = 1024
	inboundQueueSize      = 1024
	canSendHighWatermark  = 90 // percent
	keepaliveIdlePeriod   = 100 * time.Millisecond
)

var (
	// ErrVideoTrackUnsupported is returned when a carrier cannot expose video tracks.
	ErrVideoTrackUnsupported = errors.New("carrier does not support video tracks")
	// ErrTransportClosed is returned when operations are attempted on a closed transport.
	ErrTransportClosed = errors.New("vp8channel transport closed")
)

//nolint:gochecknoglobals
var vp8Keepalive = []byte{
	0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a, 0x10, 0x00,
	0x10, 0x00, 0x00, 0x47, 0x08, 0x85, 0x85, 0x88,
	0x99, 0x84, 0x88, 0xfc,
}

// kcpFrameMagic marks a VP8 frame as carrying a KCP segment with our
// session-epoch header. The wire layout inside the VP8 frame is:
//
//	[0]      = kcpFrameMagic (0x4B = 'K')
//	[1..5]   = sender's session epoch (big-endian uint32)
//	[5..]    = raw KCP packet bytes
//
// The epoch lets a receiver detect that the peer has restarted its KCP
// session - typical when the SFU keeps forwarding the same remote video
// track across our process restarts, so handleRemoteTrack never fires
// again. On any epoch change we reset the local KCP session so both ends
// converge on fresh state.
const (
	kcpFrameMagic = byte(0x4B)
	epochHdrLen   = 5
)

type streamTransport struct {
	stream        carrier.VideoTrack
	track         *webrtc.TrackLocalStaticSample
	onData        func([]byte)
	outbound      chan []byte
	closeCh       chan struct{}
	writerDone    chan struct{}
	closed        atomic.Bool
	writerUp      atomic.Bool
	writerOnce    sync.Once
	kcpOnce       sync.Once
	frameInterval time.Duration
	batchSize     int

	// localEpoch is bumped on every KCP session restart and stamped into
	// every outgoing VP8 frame. peerEpoch tracks the last epoch we observed
	// from the remote so we can detect their restart and reset locally.
	localEpoch uint32
	peerEpoch  atomic.Uint32
	hadPeer    atomic.Bool

	kcp         *kcpRuntime
	kcpMu       sync.RWMutex
	reconnectMu sync.Mutex
	reconnectFn func()
}

// New creates a vp8channel transport backed by a carrier-specific provider.
func New(ctx context.Context, cfg transport.Config) (transport.Transport, error) {
	session, err := carrier.New(ctx, cfg.Carrier, carrier.Config{
		RoomURL:   cfg.RoomURL,
		Name:      cfg.Name,
		OnData:    nil,
		DNSServer: cfg.DNSServer,
		ProxyAddr: cfg.ProxyAddr,
		ProxyPort: cfg.ProxyPort,
	})
	if err != nil {
		return nil, fmt.Errorf("create provider transport: %w", err)
	}

	videoCapable, ok := session.(carrier.VideoTrackCapable)
	if !ok {
		return nil, ErrVideoTrackUnsupported
	}

	stream, err := videoCapable.OpenVideoTrack()
	if err != nil {
		return nil, fmt.Errorf("open video track: %w", err)
	}

	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90000,
		},
		"vp8channel",
		"olcrtc",
	)
	if err != nil {
		return nil, fmt.Errorf("create local video track: %w", err)
	}

	fps := cfg.VP8FPS
	batchSize := cfg.VP8BatchSize

	tr := &streamTransport{
		stream:        stream,
		track:         track,
		onData:        cfg.OnData,
		outbound:      make(chan []byte, outboundQueueSize),
		closeCh:       make(chan struct{}),
		writerDone:    make(chan struct{}),
		frameInterval: time.Second / time.Duration(fps),
		batchSize:     batchSize,
		localEpoch:    randomEpoch(),
	}

	if err := stream.AddTrack(track); err != nil {
		return nil, fmt.Errorf("attach local video track: %w", err)
	}
	stream.SetTrackHandler(tr.handleRemoteTrack)

	return tr, nil
}

func (p *streamTransport) Connect(ctx context.Context) error {
	connectCtx, cancel := context.WithTimeout(ctx, defaultConnectTimeout)
	defer cancel()

	if err := p.stream.Connect(connectCtx); err != nil {
		return fmt.Errorf("connect stream: %w", err)
	}

	p.writerOnce.Do(func() {
		p.writerUp.Store(true)
		go p.writerLoop()
	})

	// Start KCP immediately. Don't wait for the peer's video track:
	// the server may legitimately come up before any client joins the
	// room, and KCP itself does not need a handshake. Once the peer
	// shows up, handleRemoteTrack starts pumping their RTP into our
	// session and the epoch-change detector handles peer restarts.
	var startErr error
	p.kcpOnce.Do(func() {
		rt, err := startKCP(p.outbound, p.onData, p.epochHeader())
		if err != nil {
			startErr = err
			return
		}
		p.kcpMu.Lock()
		p.kcp = rt
		p.kcpMu.Unlock()
	})

	return startErr
}

// epochHeader returns the 5-byte VP8-frame header used to tag every KCP
// packet sent in the current local session.
func (p *streamTransport) epochHeader() [epochHdrLen]byte {
	var hdr [epochHdrLen]byte
	hdr[0] = kcpFrameMagic
	binary.BigEndian.PutUint32(hdr[1:], p.localEpoch)
	return hdr
}

func randomEpoch() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read on Linux essentially never fails; fall back to a
		// time-derived value rather than panic.
		//nolint:gosec // intentional uint32 truncation of a nanosecond timestamp
		return uint32(time.Now().UnixNano())
	}
	e := binary.BigEndian.Uint32(b[:])
	if e == 0 {
		e = 1
	}
	return e
}

func (p *streamTransport) Send(data []byte) error {
	if p.closed.Load() {
		return ErrTransportClosed
	}

	p.kcpMu.RLock()
	rt := p.kcp
	p.kcpMu.RUnlock()
	if rt == nil {
		return ErrTransportClosed
	}

	return rt.send(data)
}

func (p *streamTransport) Close() error {
	if p.closed.CompareAndSwap(false, true) {
		close(p.closeCh)

		p.kcpMu.RLock()
		rt := p.kcp
		p.kcpMu.RUnlock()
		if rt != nil {
			rt.close()
		}

		if p.writerUp.Load() {
			<-p.writerDone
		}
		if err := p.stream.Close(); err != nil {
			return fmt.Errorf("close stream: %w", err)
		}
	}
	return nil
}

func (p *streamTransport) drainOutbound() {
	for {
		select {
		case <-p.outbound:
		default:
			return
		}
	}
}

func (p *streamTransport) SetReconnectCallback(cb func()) {
	p.reconnectMu.Lock()
	p.reconnectFn = cb
	p.reconnectMu.Unlock()
	p.stream.SetReconnectCallback(func() {
		p.resetKCP()
		if cb != nil {
			cb()
		}
	})
}

func (p *streamTransport) SetShouldReconnect(fn func() bool) {
	p.stream.SetShouldReconnect(fn)
}

func (p *streamTransport) SetEndedCallback(cb func(string)) {
	p.stream.SetEndedCallback(cb)
}

func (p *streamTransport) WatchConnection(ctx context.Context) {
	p.stream.WatchConnection(ctx)
}

func (p *streamTransport) CanSend() bool {
	return !p.closed.Load() && p.stream.CanSend() &&
		len(p.outbound) < cap(p.outbound)*canSendHighWatermark/100
}

// Features advertises reliable+ordered semantics now that KCP guarantees
// in-order delivery with retransmits. The upper layer (mux/curl tunnel)
// can rely on these properties end-to-end.
func (p *streamTransport) Features() transport.Features {
	return transport.Features{
		Reliable:        true,
		Ordered:         true,
		MessageOriented: true,
		MaxPayloadSize:  defaultMaxPayloadSize,
	}
}

func (p *streamTransport) writerLoop() {
	defer close(p.writerDone)

	// Send each sample at the wire-level rate (fps * batchSize) instead of
	// bursting batchSize samples per frame interval. Bursting makes RTP
	// timestamps disagree with wall-clock arrival, which the SFU interprets
	// as huge jitter and starts throttling the stream after a few seconds.
	sampleInterval := p.frameInterval / time.Duration(p.batchSize)
	if sampleInterval <= 0 {
		sampleInterval = p.frameInterval
	}

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	keepaliveEvery := int(keepaliveIdlePeriod / sampleInterval)
	if keepaliveEvery < 1 {
		keepaliveEvery = 1
	}
	idleTicks := 0

	for {
		select {
		case <-p.closeCh:
			return
		case <-ticker.C:
			var sample []byte
			select {
			case frame := <-p.outbound:
				sample = frame
				idleTicks = 0
			default:
				idleTicks++
				if idleTicks < keepaliveEvery {
					continue
				}
				idleTicks = 0
				sample = vp8Keepalive
			}

			_ = p.track.WriteSample(media.Sample{
				Data:     sample,
				Duration: sampleInterval,
			})
		}
	}
}

func (p *streamTransport) resetKCP() {
	p.drainOutbound()
	p.kcpMu.Lock()
	old := p.kcp
	p.kcp = nil
	p.kcpMu.Unlock()
	if old != nil {
		old.close()
	}
	// Note: localEpoch is intentionally NOT bumped here. The epoch is a
	// per-process identifier set once in New(). If we changed it on every
	// peer-triggered reset, the peer would see a "new" epoch from us, reset
	// itself, send back its (unchanged) epoch which we'd then see as "new"
	// again - and the two sides would loop forever tearing down smux.
	rt, err := startKCP(p.outbound, p.onData, p.epochHeader())
	if err != nil {
		return
	}
	p.kcpMu.Lock()
	p.kcp = rt
	p.kcpMu.Unlock()
}

func (p *streamTransport) handleRemoteTrack(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
	if track.Codec().MimeType != webrtc.MimeTypeVP8 {
		go p.drainTrack(track)
		return
	}

	// We don't reset KCP here. Peer restarts are detected by the epoch
	// header on incoming frames, which works even when the SFU keeps
	// forwarding the same track across our restarts.
	go p.readVP8Track(track)
}

func (p *streamTransport) drainTrack(track *webrtc.TrackRemote) {
	buf := make([]byte, rtpBufSize)
	for {
		if _, _, err := track.Read(buf); err != nil {
			return
		}
	}
}

type vp8FrameState struct {
	vp8Pkt      codecs.VP8Packet
	frameBuf    []byte
	lastSeq     uint16
	haveLastSeq bool
	frameValid  bool
}

// processRTPPacket returns a complete VP8 frame payload when fully assembled,
// nil otherwise. Detects packet loss/reordering to avoid silently corrupting
// fragmented VP8 frames.
func (s *vp8FrameState) processRTPPacket(pkt *rtp.Packet) []byte {
	if s.haveLastSeq && pkt.SequenceNumber != s.lastSeq+1 {
		s.frameValid = false
		s.frameBuf = s.frameBuf[:0]
	}
	s.lastSeq = pkt.SequenceNumber
	s.haveLastSeq = true

	vp8Payload, err := s.vp8Pkt.Unmarshal(pkt.Payload)
	if err != nil {
		s.frameValid = false
		s.frameBuf = s.frameBuf[:0]
		return nil
	}

	if s.vp8Pkt.S == 1 {
		s.frameBuf = s.frameBuf[:0]
		s.frameValid = true
	}

	if !s.frameValid {
		return nil
	}

	s.frameBuf = append(s.frameBuf, vp8Payload...)

	if !pkt.Marker {
		return nil
	}

	defer func() {
		s.frameBuf = s.frameBuf[:0]
		s.frameValid = false
	}()

	if len(s.frameBuf) >= epochHdrLen && s.frameBuf[0] == kcpFrameMagic {
		frame := make([]byte, len(s.frameBuf))
		copy(frame, s.frameBuf)
		return frame
	}
	return nil
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

		p.handleIncomingFrame(frame)
	}
}

// handleIncomingFrame parses the epoch header and either delivers the KCP
// payload to the local session or triggers a reset when the peer's epoch
// changes (peer process restart).
func (p *streamTransport) handleIncomingFrame(frame []byte) {
	peerEpoch := binary.BigEndian.Uint32(frame[1:epochHdrLen])
	kcpPayload := frame[epochHdrLen:]
	if len(kcpPayload) == 0 {
		return
	}

	if !p.hadPeer.Swap(true) {
		p.peerEpoch.Store(peerEpoch)
	} else if prev := p.peerEpoch.Load(); prev != peerEpoch {
		// Peer restarted its KCP session. Reset ours so the conv state
		// machines re-converge. CAS guards against double-reset when
		// fragmented frames straddle the epoch boundary.
		if p.peerEpoch.CompareAndSwap(prev, peerEpoch) {
			p.resetKCP()
			p.reconnectMu.Lock()
			fn := p.reconnectFn
			p.reconnectMu.Unlock()
			if fn != nil {
				fn()
			}
		}
		// Drop this packet: it predates our fresh KCP session.
		return
	}

	p.kcpMu.RLock()
	rt := p.kcp
	p.kcpMu.RUnlock()
	if rt != nil {
		rt.deliver(kcpPayload)
	}
}
