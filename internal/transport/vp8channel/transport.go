// Package vp8channel disguises a KCP-based byte transport as a stream of
// valid VP8 keyframes so SFUs that validate bitstream conformance let the
// payload through. The package owns its own KCP framing; the per-message
// fragment/ack machinery used by videochannel/seichannel is unnecessary
// here because KCP already provides ordered, reliable delivery.
package vp8channel

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
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

var vp8Keepalive = []byte{ //nolint:gochecknoglobals // package-level state intentional
	0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a, 0x10, 0x00,
	0x10, 0x00, 0x00, 0x47, 0x08, 0x85, 0x85, 0x88,
	0x99, 0x84, 0x88, 0xfc,
}

// KCP data frames are disguised as valid VP8 frames so Telemost SFU lets them
// through. The SFU validates the VP8 bitstream and drops frames that don't
// look like real VP8 - so we prepend the keepalive keyframe and append our
// header + payload after it. Wire layout:
//
//	[0..20]    = vp8Keepalive (valid VP8 keyframe, passes SFU inspection)
//	[20..24]   = binding token derived from client-id (big-endian uint32)
//	[24..28]   = sender's session epoch (big-endian uint32)
//	[28..32]   = CRC32(token || epoch)
//	[32..]     = raw KCP packet bytes
const (
	tokenOff    = 20
	epochOff    = 24
	crcOff      = 28
	epochHdrLen = 32
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
	bindingToken uint32
	localEpoch   uint32
	peerEpoch    atomic.Uint32
	hadPeer      atomic.Bool

	kcp         *kcpRuntime
	kcpMu       sync.RWMutex
	reconnectMu sync.Mutex
	reconnectFn func()
}

// New creates a vp8channel transport backed by a carrier.
func New(ctx context.Context, cfg transport.Config) (transport.Transport, error) {
	session, err := carrier.New(ctx, cfg.Carrier, carrier.Config{
		RoomURL:   cfg.RoomURL,
		Name:      cfg.Name,
		OnData:    nil,
		DNSServer: cfg.DNSServer,
		ProxyAddr: cfg.ProxyAddr,
		ProxyPort: cfg.ProxyPort,
		Engine:    cfg.Engine,
		URL:       cfg.URL,
		Token:     cfg.Token,
	})
	if err != nil {
		return nil, fmt.Errorf("create carrier transport: %w", err)
	}

	videoCapable, ok := session.(carrier.VideoTrackCapable)
	if !ok {
		return nil, ErrVideoTrackUnsupported
	}

	stream, err := videoCapable.OpenVideoTrack()
	if err != nil {
		return nil, fmt.Errorf("open video track: %w", err)
	}

	// Stream/track IDs must be unique per peer — Jitsi rejects session-accept
	// when msid collides with another participant in the conference.
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90000,
		},
		"vp8channel-"+randomID(),
		"olcrtc-"+randomID(),
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
		bindingToken:  bindingToken(cfg.RoomURL),
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

	// Start KCP eagerly so Send/CanSend work immediately after Connect.
	// Without this, the handshake round-trip that runs right after Connect
	// would deadlock: muxconn.Write spins on CanSend (which checks kcp!=nil)
	// and KCP was only started lazily on the first incoming peer frame.
	p.kcpOnce.Do(func() {
		rt, err := startKCP(p.outbound, p.onData, p.epochHeader())
		if err != nil {
			logger.Infof("vp8channel: startKCP failed: %v", err)
			return
		}
		p.kcpMu.Lock()
		p.kcp = rt
		p.kcpMu.Unlock()
		logger.Infof("vp8channel: KCP started localEpoch=0x%08x", p.localEpoch)
	})

	p.writerOnce.Do(func() {
		p.writerUp.Store(true)
		go p.writerLoop()
	})

	return nil
}

// epochHeader returns the 5-byte VP8-frame header used to tag every KCP
// packet sent in the current local session.
func (p *streamTransport) epochHeader() [epochHdrLen]byte {
	var hdr [epochHdrLen]byte
	copy(hdr[:], vp8Keepalive)
	binary.BigEndian.PutUint32(hdr[tokenOff:epochOff], p.bindingToken)
	binary.BigEndian.PutUint32(hdr[epochOff:crcOff], p.localEpoch)
	binary.BigEndian.PutUint32(hdr[crcOff:epochHdrLen], epochCRC(p.bindingToken, p.localEpoch))
	return hdr
}

func epochCRC(token, epoch uint32) uint32 {
	var buf [8]byte
	binary.BigEndian.PutUint32(buf[0:4], token)
	binary.BigEndian.PutUint32(buf[4:8], epoch)
	return crc32.ChecksumIEEE(buf[:])
}

func parseEpochHeader(frame []byte) (uint32, uint32, bool) {
	if len(frame) < epochHdrLen {
		return 0, 0, false
	}
	token := binary.BigEndian.Uint32(frame[tokenOff:epochOff])
	epoch := binary.BigEndian.Uint32(frame[epochOff:crcOff])
	gotCRC := binary.BigEndian.Uint32(frame[crcOff:epochHdrLen])
	return token, epoch, gotCRC == epochCRC(token, epoch)
}

func bindingToken(clientID string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(clientID))
	token := h.Sum32()
	if token == 0 {
		token = 1
	}
	return token
}

// randomID returns 8 random hex characters for use as a per-peer suffix on
// track and stream IDs. Required for Jitsi: msid collisions between
// participants cause Jicofo to reject session-accept.
func randomID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func randomEpoch() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read on Linux essentially never fails; fall back to a
		// time-derived value rather than panic.
		return uint32(time.Now().UnixNano()) //nolint:gosec // G115: bounded conversion verified by surrounding logic
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
	if p.closed.Load() {
		return false
	}
	p.kcpMu.RLock()
	hasKCP := p.kcp != nil
	p.kcpMu.RUnlock()
	return hasKCP && p.stream.CanSend() &&
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

	sampleInterval := p.sampleInterval()

	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()

	keepaliveEvery := max(int(keepaliveIdlePeriod/sampleInterval), 1)
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
				hdr := p.epochHeader()
				sample = hdr[:]
			}

			_ = p.track.WriteSample(media.Sample{
				Data:     sample,
				Duration: sampleInterval,
			})
		}
	}
}

func (p *streamTransport) sampleInterval() time.Duration {
	if p.batchSize > 1 {
		return p.frameInterval / time.Duration(p.batchSize)
	}
	return p.frameInterval
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

	if len(s.frameBuf) >= epochHdrLen {
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

func (p *streamTransport) handleFirstPeer(peerEpoch uint32) {
	p.peerEpoch.Store(peerEpoch)
	logger.Infof("vp8channel: peer first seen epoch=0x%08x", peerEpoch)
}

// handleIncomingFrame parses the epoch header and either delivers the KCP
// payload to the local session or triggers a reset when the peer's epoch
// changes (peer process restart).
func (p *streamTransport) handleIncomingFrame(frame []byte) {
	frameToken, peerEpoch, ok := parseEpochHeader(frame)
	if !ok {
		logger.Debugf("vp8channel: frame header checksum mismatch")
		return
	}
	if frameToken != p.bindingToken {
		logger.Debugf("vp8channel: frame token mismatch got=0x%08x want=0x%08x (foreign client or noise)",
			frameToken, p.bindingToken)
		return
	}
	kcpPayload := frame[epochHdrLen:]
	// Some carriers/SFUs reflect our own published VP8 track back to us as a
	// remote track. Those frames carry our local epoch, not the peer's. If we
	// treat them as peer traffic, epoch tracking toggles between "self" and
	// "peer" and both sides loop forever resetting smux/KCP.
	if peerEpoch == p.localEpoch {
		logger.Debugf("vp8channel: self-echo detected epoch=0x%08x (SFU reflects our own track)", peerEpoch)
		return
	}

	if !p.hadPeer.Swap(true) {
		p.handleFirstPeer(peerEpoch)
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

	if len(kcpPayload) == 0 {
		return
	}
	p.kcpMu.RLock()
	rt := p.kcp
	p.kcpMu.RUnlock()
	if rt != nil {
		rt.deliver(kcpPayload)
	}
}

/*
ПИДОРАС ИЗ ЯНДЕКСА СУКА ПРЕЖДЕ ЧЕМ ФИКСИТЬ ЭТО ЗАДАЙСЯ ВОПРОСОМ:
ПОЧЕМУ ТВОЙ СЕРВЕР ПРИНИМАЕТ ПАКЕТЫ С SEQUENCE NUMBER ИЗ БУДУЩЕГО
БЛЯТЬ ДА ПОТОМУ ЧТО ОН ХУЙНЯ И ТВОЯ МАТЬ СОЛСЕТ МОЙ ХУЙ СУКА Я ЕЕ УБЬЮ БЛЯТЬ
ОЕСЕНЮ СДЕЛАЮ ТЕСТО А ПОТОМ ЭТО ТЕСТО ВЫЕБУ БЛЯТЬ
*/
