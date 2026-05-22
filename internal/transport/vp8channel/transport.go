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
	"errors"
	"fmt"
	"hash/crc32"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
	enginebuiltin "github.com/openlibrecommunity/olcrtc/internal/engine/builtin"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/openlibrecommunity/olcrtc/internal/transport/common"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const (
	defaultMaxPayloadSize = 60 * 1024
	defaultConnectTimeout = 60 * time.Second
	rtpBufSize            = 65536
	outboundQueueSize     = 8192
	inboundQueueSize      = 8192
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

var kcpBatchMagic = [4]byte{'O', 'L', 'K', 'B'} //nolint:gochecknoglobals // wire marker

// videoSession is the subset of engine.Session + engine.VideoTrackCapable
// the vp8channel transport relies on.
type videoSession interface {
	Connect(ctx context.Context) error
	Close() error
	SetReconnectCallback(cb func())
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
	Reconnect(reason string)
	AddTrack(track webrtc.TrackLocal) error
	SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver))
}

type streamTransport struct {
	stream        videoSession
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

	// localEpoch is stamped into every outgoing VP8 frame. Explicit
	// upper-layer resets rotate it so the peer can reset its KCP state too.
	// Peer-triggered resets keep it stable to avoid reset ping-pong.
	bindingToken uint32
	epochMu      sync.RWMutex
	localEpoch   uint32
	peerEpoch    atomic.Uint32
	hadPeer      atomic.Bool

	kcp         *kcpRuntime
	kcpMu       sync.RWMutex
	reconnectMu sync.Mutex
	reconnectFn func()
}

// New creates a vp8channel transport backed by a carrier engine.
func New(ctx context.Context, cfg transport.Config) (transport.Transport, error) {
	opts, err := optionsFrom(cfg)
	if err != nil {
		return nil, err
	}

	session, err := enginebuiltin.Open(ctx, cfg.Carrier, enginebuiltin.Config{
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
		return nil, fmt.Errorf("open engine session: %w", err)
	}

	vt, ok := session.(engine.VideoTrackCapable)
	if !ok || !session.Capabilities().VideoTrack {
		_ = session.Close()
		return nil, ErrVideoTrackUnsupported
	}
	stream := &engineVideoSession{session: session, vt: vt}

	// Stream/track IDs must be unique per peer — Jitsi rejects session-accept
	// when msid collides with another participant in the conference.
	track, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{
			MimeType:  webrtc.MimeTypeVP8,
			ClockRate: 90000,
		},
		"vp8channel-"+common.RandomID(),
		"olcrtc-"+common.RandomID(),
	)
	if err != nil {
		return nil, fmt.Errorf("create local video track: %w", err)
	}

	fps := opts.FPS
	batchSize := opts.BatchSize
	if fps <= 0 {
		fps = defaultFPS
	}
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

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
		logger.Infof("vp8channel: KCP started localEpoch=0x%08x", p.localEpochValue())
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
	p.epochMu.RLock()
	epoch := p.localEpoch
	p.epochMu.RUnlock()
	return buildEpochHeader(p.bindingToken, epoch)
}

func buildEpochHeader(token, epoch uint32) [epochHdrLen]byte {
	var hdr [epochHdrLen]byte
	copy(hdr[:], vp8Keepalive)
	binary.BigEndian.PutUint32(hdr[tokenOff:epochOff], token)
	binary.BigEndian.PutUint32(hdr[epochOff:crcOff], epoch)
	binary.BigEndian.PutUint32(hdr[crcOff:epochHdrLen], epochCRC(token, epoch))
	return hdr
}

func (p *streamTransport) rotateEpochHeader() [epochHdrLen]byte {
	p.epochMu.Lock()
	for {
		next := randomEpoch()
		if next != p.localEpoch {
			p.localEpoch = next
			break
		}
	}
	epoch := p.localEpoch
	p.epochMu.Unlock()
	return buildEpochHeader(p.bindingToken, epoch)
}

func (p *streamTransport) localEpochValue() uint32 {
	p.epochMu.RLock()
	defer p.epochMu.RUnlock()
	return p.localEpoch
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

// ResetPeer drops queued KCP traffic and starts a fresh KCP state machine while
// keeping the carrier connection alive. The client/server liveness layer calls
// this before rebuilding smux so replacement handshakes are not parsed behind
// stale bytes from streams that were active when the old session died.
func (p *streamTransport) ResetPeer() {
	p.restartKCP(p.rotateEpochHeader())
}

// Reconnect forwards to the underlying engine session.
func (p *streamTransport) Reconnect(reason string) {
	p.stream.Reconnect(reason)
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

	ticker := time.NewTicker(p.frameInterval)
	defer ticker.Stop()

	keepaliveEvery := max(int(keepaliveIdlePeriod/p.frameInterval), 1)
	idleTicks := 0

	for {
		select {
		case <-p.closeCh:
			return
		case <-ticker.C:
			var sample []byte
			select {
			case frame := <-p.outbound:
				sample = p.batchSample(frame)
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
				Duration: p.frameInterval,
			})
		}
	}
}

func (p *streamTransport) batchSample(first []byte) []byte {
	if len(first) <= epochHdrLen || p.batchSize <= 1 {
		return first
	}

	sample := make([]byte, 0, defaultMaxPayloadSize)
	sample = append(sample, first[:epochHdrLen]...)
	sample = append(sample, kcpBatchMagic[:]...)
	sample = appendBatchPacket(sample, first[epochHdrLen:])

	for packets := 1; packets < p.batchSize; packets++ {
		select {
		case frame := <-p.outbound:
			if len(frame) <= epochHdrLen {
				continue
			}
			payload := frame[epochHdrLen:]
			if len(sample)+2+len(payload) > defaultMaxPayloadSize {
				return sample
			}
			sample = appendBatchPacket(sample, payload)
		default:
			return sample
		}
	}
	return sample
}

func appendBatchPacket(dst, packet []byte) []byte {
	if len(packet) > 0xffff {
		return dst
	}
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(packet))) //nolint:gosec // bounded above
	dst = append(dst, lenBuf[:]...)
	return append(dst, packet...)
}

func (p *streamTransport) resetKCP() {
	p.restartKCP(p.epochHeader())
}

func (p *streamTransport) restartKCP(epochHdr [epochHdrLen]byte) {
	p.drainOutbound()
	p.kcpMu.Lock()
	old := p.kcp
	p.kcp = nil
	p.kcpMu.Unlock()
	if old != nil {
		old.close()
	}
	rt, err := startKCP(p.outbound, p.onData, epochHdr)
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
	if peerEpoch == p.localEpochValue() {
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
		deliverKCPPayload(rt, kcpPayload)
	}
}

func deliverKCPPayload(rt *kcpRuntime, payload []byte) {
	if rt == nil || len(payload) == 0 {
		return
	}
	splitKCPPayload(payload, rt.deliver)
}

func splitKCPPayload(payload []byte, deliver func([]byte)) {
	if len(payload) < len(kcpBatchMagic) ||
		string(payload[:len(kcpBatchMagic)]) != string(kcpBatchMagic[:]) {
		deliver(payload)
		return
	}

	rest := payload[len(kcpBatchMagic):]
	for len(rest) > 0 {
		if len(rest) < 2 {
			return
		}
		size := int(binary.BigEndian.Uint16(rest[:2]))
		rest = rest[2:]
		if size == 0 || len(rest) < size {
			return
		}
		deliver(rest[:size])
		rest = rest[size:]
	}
}

/*
ПИДОРАС ИЗ ЯНДЕКСА СУКА ПРЕЖДЕ ЧЕМ ФИКСИТЬ ЭТО ЗАДАЙСЯ ВОПРОСОМ:
ПОЧЕМУ ТВОЙ СЕРВЕР ПРИНИМАЕТ ПАКЕТЫ С SEQUENCE NUMBER ИЗ БУДУЩЕГО
БЛЯТЬ ДА ПОТОМУ ЧТО ОН ХУЙНЯ И ТВОЯ МАТЬ СОЛСЕТ МОЙ ХУЙ СУКА Я ЕЕ УБЬЮ БЛЯТЬ
ОЕСЕНЮ СДЕЛАЮ ТЕСТО А ПОТОМ ЭТО ТЕСТО ВЫЕБУ БЛЯТЬ
*/
