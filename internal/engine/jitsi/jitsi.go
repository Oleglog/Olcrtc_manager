// Package jitsi implements an engine.Session backed by the Jitsi Meet
// XMPP/Jingle/colibri-ws stack via the github.com/zarazaex69/j library.
//
// The engine speaks the wire protocol of a self-hosted Jitsi instance: it
// joins the MUC, waits for a Jingle session-initiate from Jicofo, opens the
// JVB bridge channel (colibri-ws) for byte transport, and optionally
// negotiates a pion *webrtc.PeerConnection for video tracks.
//
// Service-specific bits (URL parsing) live in the auth/jitsi package; this
// engine is told the host and room name through engine.Config (URL carries
// the host string, Extra["room"] carries the room name).
//
// The Jingle session-initiate is only delivered by Jicofo once at least one
// other participant is present in the conference, mirroring the Telemost /
// SaluteJazz two-peer requirement that olcrtc already accommodates.
package jitsi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	pioninterceptor "github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"github.com/zarazaex69/j"
)

const (
	defaultSendQueueSize = 5000
	// bridgeMaxMessageSize is the practical upper bound on a single colibri-ws
	// payload. JVB enforces a max-message-size around 16 KiB; payloads above
	// that cause the bridge to drop the websocket. The default datachannel
	// transport in olcrtc already uses 12 KiB chunks, well under this limit.
	bridgeMaxMessageSize = 16 * 1024
	bridgeOpenTimeout    = 30 * time.Second
	defaultNick          = "olcrtc"
	credentialKeyRoom    = "room"
	videoTrackName       = "videochannel"
	maxReconnects        = 5
	reconnectWindow      = 5 * time.Minute
)

// bridgeMagic tags every EndpointMessage produced by this engine. JVB broadcasts
// EndpointMessage payloads to every occupant of the MUC; the magic lets the
// receiver discard frames from unrelated applications (or unrelated olcrtc
// processes sharing the same room) before they reach the byte-stream layer.
// Without it, a stray peer's smux/handshake bytes parse as our protocol and
// deadlock the connection. 4 bytes is enough entropy for collision avoidance
// against real-world payloads while keeping the overhead negligible.
var bridgeMagic = [4]byte{'O', 'L', 'R', '1'} //nolint:gochecknoglobals // protocol constant

var (
	// ErrSessionClosed is returned when an operation is attempted on a closed session.
	ErrSessionClosed = errors.New("jitsi session closed")
	// ErrSendQueueFull is returned when the outbound queue cannot accept more data.
	ErrSendQueueFull = errors.New("jitsi send queue full")
	// ErrBridgeNotReady is returned when send is attempted before the bridge is open.
	ErrBridgeNotReady = errors.New("jitsi bridge not ready")
	// ErrSendTooLarge is returned when a single payload exceeds the JVB max-message-size limit.
	ErrSendTooLarge = errors.New("jitsi payload exceeds bridge max-message-size")
	// ErrHostRequired is returned when no Jitsi host was supplied.
	ErrHostRequired = errors.New("jitsi host required")
	// ErrRoomRequired is returned when no Jitsi room was supplied.
	ErrRoomRequired = errors.New("jitsi room required")
)

// Session is the Jitsi engine handle.
type Session struct {
	host string
	room string
	name string

	onData          func([]byte)
	onReconnect     func(*webrtc.DataChannel)
	shouldReconnect func() bool
	onEnded         func(string)

	jSess atomic.Pointer[j.Session]

	pcMu sync.Mutex
	pc   *webrtc.PeerConnection

	sendQueue    chan []byte
	bridgeReady  atomic.Bool
	closed       atomic.Bool
	reconnecting atomic.Bool

	reconnectCh    chan struct{}
	lastReconnect  time.Time
	reconnectCount int

	// peerEndpoint latches the MUC nick of the first occupant whose
	// EndpointMessage passed the bridgeMagic check. Once set, all bridge
	// messages from other senders are dropped, isolating us from chatter by
	// unrelated olcrtc processes that happen to share the same room.
	peerEndpoint atomic.Pointer[string]
	done         chan struct{}
	doneOnce     sync.Once
	cancel       context.CancelFunc
	runCtx       context.Context //nolint:containedctx // engine owns the supervisor lifetime
	wg           sync.WaitGroup

	videoTrackMu sync.RWMutex
	videoTracks  []webrtc.TrackLocal
	onVideoTrack func(*webrtc.TrackRemote, *webrtc.RTPReceiver)

	// peerVideoSSRC latches the SSRC of the first remote video track we
	// surfaced to the carrier. JVB forwards every active video source in
	// the MUC as a separate TrackRemote; without this latch a third
	// participant's video confuses the vp8channel epoch/CRC machinery on
	// the receiver side. Once set, additional video tracks are drained.
	peerVideoSSRC atomic.Uint32
}

// New creates a new Jitsi engine session.
//
// cfg.URL carries the Jitsi host (e.g. "meet.cryptopro.ru") — populated by the
// jitsi auth provider after parsing the user-supplied room URL. cfg.Extra
// must contain the room name under the "room" key.
func New(_ context.Context, cfg engine.Config) (engine.Session, error) {
	host := normaliseHost(cfg.URL)
	if host == "" {
		return nil, ErrHostRequired
	}
	var room string
	if cfg.Extra != nil {
		room = strings.TrimSpace(cfg.Extra[credentialKeyRoom])
	}
	if room == "" {
		return nil, ErrRoomRequired
	}
	name := sanitiseNick(cfg.Name)
	if name == "" {
		name = defaultNick
	}

	runCtx, cancel := context.WithCancel(context.Background())
	return &Session{
		host:        host,
		room:        room,
		name:        name,
		onData:      cfg.OnData,
		sendQueue:   make(chan []byte, defaultSendQueueSize),
		reconnectCh: make(chan struct{}, 1),
		done:        make(chan struct{}),
		cancel:      cancel,
		runCtx:      runCtx,
	}, nil
}

// cyrillicToLatin maps Cyrillic runes to their Latin transliteration strings.
var cyrillicToLatin = map[rune]string{ //nolint:gochecknoglobals // package-level lookup table
	'А': "A", 'а': "a", 'Б': "B", 'б': "b", 'В': "V", 'в': "v",
	'Г': "G", 'г': "g", 'Д': "D", 'д': "d", 'Е': "E", 'е': "e",
	'Ё': "Yo", 'ё': "yo", 'Ж': "Zh", 'ж': "zh", 'З': "Z", 'з': "z",
	'И': "I", 'и': "i", 'Й': "Y", 'й': "y", 'К': "K", 'к': "k",
	'Л': "L", 'л': "l", 'М': "M", 'м': "m", 'Н': "N", 'н': "n",
	'О': "O", 'о': "o", 'П': "P", 'п': "p", 'Р': "R", 'р': "r",
	'С': "S", 'с': "s", 'Т': "T", 'т': "t", 'У': "U", 'у': "u",
	'Ф': "F", 'ф': "f", 'Х': "Kh", 'х': "kh", 'Ц': "Ts", 'ц': "ts",
	'Ч': "Ch", 'ч': "ch", 'Ш': "Sh", 'ш': "sh", 'Щ': "Shch", 'щ': "shch",
	'Ъ': "", 'ъ': "", 'Ы': "Y", 'ы': "y", 'Ь': "", 'ь': "",
	'Э': "E", 'э': "e", 'Ю': "Yu", 'ю': "yu", 'Я': "Ya", 'я': "ya",
}

// sanitiseNick reduces a display name to a 7-bit ASCII slug acceptable to
// the j library's MUC presence helper. The helper currently uses byte-level
// slicing on the supplied name to derive a stats-id, so multi-byte UTF-8
// inputs (e.g. Cyrillic) get sliced mid-codepoint and Prosody silently
// rejects the resulting presence stanza.
//
// Cyrillic characters are transliterated; other non-ASCII characters are
// dropped; spaces and punctuation are normalised to '-'. The result is
// bounded to 16 characters.
func sanitiseNick(raw string) string {
	const maxNickLen = 16
	var b strings.Builder
	b.Grow(len(raw))
	prevDash := false
	for _, r := range raw {
		if b.Len() >= maxNickLen {
			break
		}
		if isNickRune(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if lat, ok := cyrillicToLatin[r]; ok {
			for _, lr := range lat {
				if b.Len() >= maxNickLen {
					break
				}
				b.WriteRune(lr)
			}
			prevDash = false
			continue
		}
		if !prevDash && b.Len() > 0 {
			b.WriteRune('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// isNickRune reports whether r is allowed verbatim in a sanitised nick.
func isNickRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	case r == '-' || r == '_':
		return true
	}
	return false
}

// Capabilities reports what this engine can do.
func (s *Session) Capabilities() engine.Capabilities {
	return engine.Capabilities{ByteStream: true, VideoTrack: true}
}

// Connect joins the Jitsi conference, optionally opens the bridge channel,
// and (if video tracks are pending or a remote handler is set) negotiates a
// pion PeerConnection.
func (s *Session) Connect(ctx context.Context) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}

	jSess, err := s.joinAndOpenBridge(ctx)
	if err != nil {
		return err
	}
	s.jSess.Store(jSess)

	s.wg.Add(2)
	go s.sendLoop()
	go s.recvLoop()
	return nil
}

func (s *Session) joinAndOpenBridge(ctx context.Context) (*j.Session, error) {
	logger.Infof("jitsi: joining %s/%s as %s …", s.host, s.room, s.name)
	jSess, err := j.Join(ctx, j.Config{
		Host:  s.host,
		Room:  s.room,
		Nick:  s.name,
		Debug: logger.IsVerbose(),
	})
	if err != nil {
		return nil, fmt.Errorf("jitsi join: %w", err)
	}
	logger.Infof("jitsi: joined %s/%s; colibri-ws=%s", s.host, s.room, jSess.ColibriWS)

	if s.onData != nil {
		bctx, bcancel := context.WithTimeout(ctx, bridgeOpenTimeout)
		err := jSess.OpenBridge(bctx)
		bcancel()
		if err != nil {
			_ = jSess.Close()
			return nil, fmt.Errorf("open bridge: %w", err)
		}
		// Re-latch peer on every bridge open: after a reconnect the partner's
		// MUC nick may have changed.
		s.peerEndpoint.Store(nil)
		s.peerVideoSSRC.Store(0)
		s.bridgeReady.Store(true)
		logger.Infof("jitsi: bridge open (endpoints=%v)", jSess.Endpoints())
	}

	if s.shouldNegotiatePC() {
		if err := s.negotiatePC(ctx, jSess); err != nil {
			_ = jSess.Close()
			return nil, err
		}
	}

	return jSess, nil
}

func (s *Session) shouldNegotiatePC() bool {
	s.videoTrackMu.RLock()
	defer s.videoTrackMu.RUnlock()
	return len(s.videoTracks) > 0 || s.onVideoTrack != nil
}

// drainTrack reads and discards RTP from a TrackRemote we chose to ignore so
// pion's per-track receiver buffer doesn't fill up. Returns when the track
// closes.
func drainTrack(track *webrtc.TrackRemote) {
	buf := make([]byte, 1500)
	for {
		if _, _, err := track.Read(buf); err != nil {
			return
		}
	}
}

func (s *Session) videoTrackHandler() func(*webrtc.TrackRemote, *webrtc.RTPReceiver) {
	s.videoTrackMu.RLock()
	defer s.videoTrackMu.RUnlock()
	return s.onVideoTrack
}

// negotiatePC builds the pion PeerConnection, applies Jicofo's offer,
// answers it and registers all the per-side wiring (DTLS state, ICE
// callbacks, transceiver direction). It's branchy on purpose — Jingle
// negotiation has many discrete steps that can fail and each step
// belongs to the same logical operation, so splitting it into helpers
// would obscure the wire order rather than clarify it.
//
//nolint:cyclop // sequential Jingle negotiation steps; refactoring would hide ordering
func (s *Session) negotiatePC(ctx context.Context, jSess *j.Session) error {
	settings := webrtc.SettingEngine{}
	settings.LoggerFactory = logger.NewPionLoggerFactory()

	// pion auto-registers a default interceptor chain (sender reports,
	// receiver reports, NACK, etc.) when none is supplied. Several of
	// those probe the DTLS transport on a tick — until DTLS comes up
	// (which can take seconds against Jitsi's STUN-only path, or never
	// in pathological cases) they spam logs with
	// "the DTLS transport has not started yet". JVB performs its own
	// RTCP feedback aggregation, so the conference PC does not need
	// any of those interceptors. An empty registry silences the noise.
	registry := &pioninterceptor.Registry{}
	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(settings),
		webrtc.WithInterceptorRegistry(registry),
	)

	// Jicofo emits Plan B style SDP. Explicit Plan B semantics match what
	// the j library reference setup uses; source-add renegotiation drives
	// reception of other participants' SSRCs on the same m=video section.
	pcConfig := jSess.IceConfig()
	pcConfig.SDPSemantics = webrtc.SDPSemanticsPlanB

	pc, err := api.NewPeerConnection(pcConfig)
	if err != nil {
		return fmt.Errorf("new pc: %w", err)
	}

	// Jicofo's session-initiate always includes m=audio. Without a matching
	// audio transceiver, pion's answer rejects the audio m-line and JVB may
	// not complete ICE for the second peer in the room.
	if _, err := pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeAudio,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		_ = pc.Close()
		return fmt.Errorf("add audio recvonly: %w", err)
	}

	s.videoTrackMu.RLock()
	hasLocalTracks := len(s.videoTracks) > 0
	for _, track := range s.videoTracks {
		if _, addErr := pc.AddTrack(track); addErr != nil {
			s.videoTrackMu.RUnlock()
			_ = pc.Close()
			return fmt.Errorf("add track: %w", addErr)
		}
	}
	s.videoTrackMu.RUnlock()

	// When sending video, AddTrack already creates the video m-line (sendonly).
	// When only receiving, an explicit recvonly transceiver is required so the
	// SDP answer includes a video m-line — without it JVB does not set up a
	// video forwarding path and ICE stalls. Mirrors the j library reference CLI:
	// AddTrack and AddTransceiverFromKind(video,recvonly) are mutually exclusive
	// in Plan B; using both produces a malformed SDP.
	if !hasLocalTracks {
		if _, err := pc.AddTransceiverFromKind(
			webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
		); err != nil {
			_ = pc.Close()
			return fmt.Errorf("add video recvonly: %w", err)
		}
	}

	pc.OnTrack(func(track *webrtc.TrackRemote, recv *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeVideo {
			return
		}
		ssrc := uint32(track.SSRC())
		if !s.peerVideoSSRC.CompareAndSwap(0, ssrc) && s.peerVideoSSRC.Load() != ssrc {
			// A different remote participant: drain the track so pion's
			// receiver buffer doesn't fill up and back-pressure the SFU.
			go drainTrack(track)
			return
		}
		if cb := s.videoTrackHandler(); cb != nil {
			cb(track, recv)
		}
	})
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logger.Debugf("jitsi pc state: %s", state.String())
		if state == webrtc.PeerConnectionStateFailed && !s.closed.Load() && s.onEnded != nil {
			s.onEnded("jitsi peer connection failed")
		}
	})

	neg := jSess.Negotiator()
	neg.PC = pc
	neg.OnIceConnectionStateChange = func(state webrtc.ICEConnectionState) {
		logger.Debugf("jitsi ICE state: %s", state)
	}

	// Drain XMPP stanzas BEFORE Accept. Jicofo can push transport-info
	// (trickle ICE) and source-add (other participants' SSRCs) the moment
	// it sees us reply to session-initiate. If we started the drain loop
	// only after Accept and SendSourceAdd, those stanzas would queue in
	// the 64-slot channel while RTP — which travels straight over UDP/TURN
	// and reaches us in tens of ms — arrives first. Pion then drops the
	// peer's RTP as "unhandled SSRC, media section has an explicit SSRC"
	// because HandleSourceAdd hasn't grafted the SSRC onto the remote SDP
	// yet. The peer never produces an OnTrack callback, our handshake
	// never gets an ACK, and the tunnel dies. Starting the consumer first
	// closes that race window — any source-add Jicofo emits is picked up
	// the instant it lands on the wire.
	s.wg.Add(1)
	go s.trickleDrainLoop(pc, neg, jSess.LowLevel().Stanzas())

	if err := neg.Accept(ctx); err != nil {
		_ = pc.Close()
		return fmt.Errorf("session-accept: %w", err)
	}
	logger.Debugf("jitsi: session-accept sent")

	// Announce our SSRCs explicitly via source-add. Even though session-accept
	// already carries them, Jicofo only propagates sources advertised via
	// source-add to peers that join AFTER us.
	if hasLocalTracks {
		if err := neg.SendSourceAddFromSDP(pc.LocalDescription().SDP); err != nil {
			logger.Debugf("jitsi: source-add (initial): %v", err)
		}
	}

	// Tell JVB to forward video streams to this endpoint.
	if err := jSess.RequestVideo(ctx, 720); err != nil {
		logger.Debugf("jitsi: request video: %v", err)
	}

	s.pcMu.Lock()
	s.pc = pc
	s.pcMu.Unlock()
	return nil
}

// negotiator is the subset of *peer.Negotiator we need. Defined as an
// interface here because peer is in j's internal/ tree and not importable.
type negotiator interface {
	HandleSourceAdd(stanza string) error
}

// trickleDrainLoop reads the XMPP stanza channel and feeds any
// transport-info ICE candidates into the PeerConnection. It also drains
// non-jingle stanzas so the channel never fills and blocks the read loop.
// Incoming source-add stanzas (announcing other participants' SSRCs) are
// merged into the remote SDP via neg.HandleSourceAdd so pion can route the
// inbound RTP through OnTrack.
func (s *Session) trickleDrainLoop(pc *webrtc.PeerConnection, neg negotiator, stanzas <-chan string) {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case raw, ok := <-stanzas:
			if !ok {
				return
			}
			switch {
			case strings.Contains(raw, "transport-info"):
				if err := s.applyTrickleICE(pc, raw); err != nil {
					logger.Debugf("jitsi trickle ICE: %v", err)
				}
			case strings.Contains(raw, "source-add"):
				if err := neg.HandleSourceAdd(raw); err != nil {
					logger.Debugf("jitsi source-add: %v", err)
				}
			}
		}
	}
}

// xmlCandidate is a minimal XML representation of a Jingle ICE candidate.
type xmlCandidate struct {
	Component  string `xml:"component,attr"`
	Foundation string `xml:"foundation,attr"`
	Generation string `xml:"generation,attr"`
	IP         string `xml:"ip,attr"`
	Port       string `xml:"port,attr"`
	Priority   string `xml:"priority,attr"`
	Protocol   string `xml:"protocol,attr"`
	Type       string `xml:"type,attr"`
	RelAddr    string `xml:"rel-addr,attr"`
	RelPort    string `xml:"rel-port,attr"`
}

// xmlTransportInfo is the minimal structure needed to extract candidates
// from a <jingle action="transport-info"> stanza.
type xmlTransportInfo struct {
	XMLName xml.Name `xml:"iq"`
	Jingle  struct {
		Action   string `xml:"action,attr"`
		Contents []struct {
			Name      string `xml:"name,attr"`
			Transport struct {
				Candidates []xmlCandidate `xml:"candidate"`
			} `xml:"transport"`
		} `xml:"content"`
	} `xml:"jingle"`
}

func (s *Session) applyTrickleICE(pc *webrtc.PeerConnection, raw string) error {
	var ti xmlTransportInfo
	if err := xml.Unmarshal([]byte(raw), &ti); err != nil {
		return fmt.Errorf("parse transport-info: %w", err)
	}
	for _, content := range ti.Jingle.Contents {
		mid := content.Name
		for _, c := range content.Transport.Candidates {
			sdpLine := buildSDPCandidate(c)
			if sdpLine == "" {
				continue
			}
			init := webrtc.ICECandidateInit{
				Candidate: sdpLine,
				SDPMid:    &mid,
			}
			if err := pc.AddICECandidate(init); err != nil {
				logger.Debugf("jitsi add ICE candidate (%s): %v", mid, err)
			}
		}
	}
	return nil
}

func buildSDPCandidate(c xmlCandidate) string {
	if c.IP == "" || c.Port == "" {
		return ""
	}
	comp := c.Component
	if comp == "" {
		comp = "1"
	}
	proto := strings.ToLower(c.Protocol)
	if proto == "" {
		proto = "udp"
	}
	priority := c.Priority
	if priority == "" {
		priority = "1"
	}
	candType := c.Type
	if candType == "" {
		candType = "host"
	}
	s := fmt.Sprintf("candidate:%s %s %s %s %s %s typ %s",
		c.Foundation, comp, proto, priority, c.IP, c.Port, candType)
	if c.RelAddr != "" && c.RelPort != "" {
		s += fmt.Sprintf(" raddr %s rport %s", c.RelAddr, c.RelPort)
	}
	if c.Generation != "" {
		s += " generation " + c.Generation
	}
	return s
}

// Send queues data for transmission over the bridge.
//
// Send is non-blocking: data is enqueued onto the engine's outbound channel
// and a background goroutine pumps the queue into the colibri-ws bridge with
// the bridge's own backpressure window.
func (s *Session) Send(data []byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if !s.bridgeReady.Load() {
		return ErrBridgeNotReady
	}
	if len(data)+len(bridgeMagic) > bridgeMaxMessageSize {
		return ErrSendTooLarge
	}
	framed := make([]byte, len(bridgeMagic)+len(data))
	copy(framed, bridgeMagic[:])
	copy(framed[len(bridgeMagic):], data)
	select {
	case s.sendQueue <- framed:
		return nil
	case <-s.done:
		return ErrSessionClosed
	default:
		return ErrSendQueueFull
	}
}

func (s *Session) sendLoop() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case data, ok := <-s.sendQueue:
			if !ok {
				return
			}
			jSess := s.jSess.Load()
			if jSess == nil {
				return
			}
			if err := jSess.BridgeSendRaw("", data); err != nil {
				if s.closed.Load() {
					return
				}
				logger.Debugf("jitsi bridge send: %v", err)
			}
		}
	}
}

func (s *Session) recvLoop() {
	defer s.wg.Done()

	jSess := s.jSess.Load()
	if jSess == nil || s.onData == nil || !s.bridgeReady.Load() {
		return
	}
	msgs := jSess.BridgeMessages()
	if msgs == nil {
		return
	}
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-msgs:
			if !s.deliverBridgeMessage(msg, ok) {
				return
			}
		}
	}
}

// deliverBridgeMessage decodes a single incoming bridge message and forwards
// any raw payload to onData. Returns false to signal that the recv loop
// should exit (channel closed or session ended).
func (s *Session) deliverBridgeMessage(msg j.BridgeMessage, ok bool) bool {
	if !ok {
		if !s.closed.Load() {
			s.requestReconnect("jitsi bridge closed")
		}
		return false
	}
	payload := decodeRaw(msg)
	if payload == nil {
		return true
	}
	if len(payload) < len(bridgeMagic) || !bytes.Equal(payload[:len(bridgeMagic)], bridgeMagic[:]) {
		return true
	}
	if !s.peerLatchAccepts(msg.From) {
		return true
	}
	s.onData(payload[len(bridgeMagic):])
	return true
}

// peerLatchAccepts implements the peer-latch logic: the first sender whose
// payload survived the magic check becomes our partner; everyone else is
// ignored. Cleared on reconnect by the supervisor (peerEndpoint is reset
// whenever the bridge is reopened).
func (s *Session) peerLatchAccepts(from string) bool {
	if cur := s.peerEndpoint.Load(); cur != nil {
		return *cur == from
	}
	if from == "" {
		return true
	}
	s.peerEndpoint.CompareAndSwap(nil, &from)
	// Re-check after CAS: a concurrent latch may have picked a different
	// peer first; if so, drop this frame.
	cur := s.peerEndpoint.Load()
	return cur == nil || *cur == from
}

// decodeRaw extracts the bytes from an EndpointMessage produced by the j
// library's BridgeSendRaw helper. Mirrors the unexported colibri.DecodeRaw —
// the j library's BridgeMessage type alias keeps the necessary fields public,
// but the helper itself lives in an internal package.
func decodeRaw(m j.BridgeMessage) []byte {
	if m.Class != "EndpointMessage" {
		return nil
	}
	enc, ok := m.Fields["raw"].(string)
	if !ok {
		return nil
	}
	out, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return nil
	}
	return out
}

// Close terminates the session and releases resources.
//
// Shutdown follows the lib-jitsi-meet JitsiConference.leave() contract:
//
//  1. Mark the session closed so send/recv loops drop new work.
//  2. Close the pion PeerConnection (stops media, sends DTLS bye). This
//     mirrors jvbJingleSession.close() in lib-jitsi-meet — note that
//     graceful leave there does NOT send Jingle session-terminate; Jicofo
//     learns of the departure from the MUC presence-unavailable stanza
//     and only then frees the JVB bridge slot.
//  3. Close the underlying j.Session, which closes the colibri-ws bridge,
//     performs the MUC presence-unavailable handshake (LeaveMUCWait
//     waits for Prosody to echo our own unavailable presence — the
//     XMPP-level equivalent of XMPPEvents.MUC_LEFT — with a 5s cap),
//     and only then tears down the websocket.
//  4. Cancel the supervisor context and wait for goroutines.
//
// Why no session-terminate: empirically, when the application layer (e.g.
// seichannel) wedges and the test fails before clean shutdown, Jicofo
// stops replying to our session-terminate IQ. TerminateWait then ate its
// 3s budget and we still left ghost participants behind. lib-jitsi-meet
// avoids this entirely by relying on MUC presence as the single source of
// truth for departure — Prosody's MUC layer is far more reliable than
// Jicofo's IQ handler under load.
func (s *Session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	jSess := s.jSess.Load()

	// Close PC first so DTLS goes out and the bridge sees media stop;
	// this ordering matches lib-jitsi-meet's leave() and lets the
	// follow-up MUC presence unavailable hit Jicofo with PC already
	// torn down (no session-terminate dance is involved).
	s.pcMu.Lock()
	pc := s.pc
	s.pc = nil
	s.pcMu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}

	// jSess.Close() performs the MUC unavailable handshake and only then
	// tears down the websocket. It logs the handshake outcome itself so
	// we can distinguish "Prosody confirmed leave" from "5s timeout,
	// fell back to fire-and-forget" in failure-mode investigations.
	if jSess != nil {
		_ = jSess.Close()
	}
	s.jSess.Store(nil)
	s.bridgeReady.Store(false)

	if s.cancel != nil {
		s.cancel()
	}
	s.doneOnce.Do(func() { close(s.done) })

	stopped := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
	}
	return nil
}

// SetReconnectCallback registers a callback for reconnection events.
func (s *Session) SetReconnectCallback(cb func(*webrtc.DataChannel)) { s.onReconnect = cb }

// SetShouldReconnect stores the reconnect predicate.
func (s *Session) SetShouldReconnect(fn func() bool) { s.shouldReconnect = fn }

// SetEndedCallback registers a function to call when the session ends.
func (s *Session) SetEndedCallback(cb func(string)) { s.onEnded = cb }

// WatchConnection monitors bridge lifecycle and reconnects when JVB closes
// the endpoint's colibri-ws without ending the XMPP conference.
func (s *Session) WatchConnection(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.done:
			return
		case <-s.reconnectCh:
			if s.handleReconnectAttempt(ctx) {
				return
			}
		}
	}
}

func (s *Session) requestReconnect(reason string) {
	s.bridgeReady.Store(false)
	if s.closed.Load() || s.reconnecting.Load() {
		return
	}
	if s.shouldReconnect != nil && !s.shouldReconnect() {
		s.signalEnded(reason)
		return
	}
	logger.Infof("jitsi reconnect requested: %s", reason)
	select {
	case s.reconnectCh <- struct{}{}:
	default:
	}
}

func (s *Session) handleReconnectAttempt(ctx context.Context) bool {
	if time.Since(s.lastReconnect) > reconnectWindow {
		s.reconnectCount = 0
	}
	s.reconnectCount++
	s.lastReconnect = time.Now()

	if s.reconnectCount > maxReconnects {
		s.signalEnded("jitsi reconnect limit reached")
		return true
	}

	backoff := time.Duration(s.reconnectCount) * 2 * time.Second
	if backoff > 30*time.Second {
		backoff = 30 * time.Second
	}

	for {
		if err := s.reconnect(ctx); err != nil {
			logger.Warnf("jitsi reconnect failed: %v", err)
			select {
			case <-ctx.Done():
				return true
			case <-s.done:
				return true
			case <-time.After(backoff):
				continue
			}
		}
		s.drainReconnectQueue()
		return false
	}
}

func (s *Session) reconnect(ctx context.Context) error {
	if !s.reconnecting.CompareAndSwap(false, true) {
		return nil
	}
	defer s.reconnecting.Store(false)

	s.bridgeReady.Store(false)
	if old := s.jSess.Swap(nil); old != nil {
		_ = old.Close()
	}
	s.pcMu.Lock()
	oldPC := s.pc
	s.pc = nil
	s.pcMu.Unlock()
	if oldPC != nil {
		_ = oldPC.Close()
	}

	logger.Infof("jitsi: reconnecting %s/%s as %s ...", s.host, s.room, s.name)
	jSess, err := s.joinAndOpenBridge(ctx)
	if err != nil {
		return err
	}
	s.jSess.Store(jSess)
	s.peerEndpoint.Store(nil)
	s.peerVideoSSRC.Store(0)
	s.bridgeReady.Store(true)

	s.wg.Add(1)
	go s.recvLoop()

	if s.onReconnect != nil {
		s.onReconnect(nil)
	}
	logger.Infof("jitsi: reconnected %s/%s; colibri-ws=%s", s.host, s.room, jSess.ColibriWS)
	return nil
}

func (s *Session) drainReconnectQueue() {
	for {
		select {
		case <-s.reconnectCh:
		default:
			return
		}
	}
}

// CanSend reports whether the session is ready to accept new data.
func (s *Session) CanSend() bool {
	if s.closed.Load() {
		return false
	}
	if s.onData == nil {
		// pure video mode — readiness driven by PC connection state
		s.pcMu.Lock()
		ready := s.pc != nil && s.pc.ConnectionState() == webrtc.PeerConnectionStateConnected
		s.pcMu.Unlock()
		return ready
	}
	return s.bridgeReady.Load()
}

// GetSendQueue exposes the outbound queue for upstream metrics.
func (s *Session) GetSendQueue() chan []byte { return s.sendQueue }

// GetBufferedAmount returns a coarse estimate of bytes pending on the wire.
//
// The j library's bridge connection only exposes message-count depth, so we
// approximate bytes by multiplying queue depth by the bridge max-message-size.
// This is enough for upper-layer pacing heuristics; engines that need
// byte-accurate pressure should consult GetSendQueue directly.
func (s *Session) GetBufferedAmount() uint64 {
	jSess := s.jSess.Load()
	if jSess == nil {
		return 0
	}
	depth := jSess.BridgeSendQueueDepth()
	if depth <= 0 {
		return 0
	}
	return uint64(depth) * uint64(bridgeMaxMessageSize)
}

// AddVideoTrack publishes a video track to the Jitsi conference.
//
// Tracks added before Connect are sent as part of the session-accept SDP
// (so Jicofo announces them to other participants automatically). Tracks
// added afterwards are attached to the live PeerConnection — Jitsi's
// source-add flow is not yet implemented in this engine, so late tracks
// will only be visible on the next reconnect.
func (s *Session) AddVideoTrack(track webrtc.TrackLocal) error {
	s.videoTrackMu.Lock()
	s.videoTracks = append(s.videoTracks, track)
	s.videoTrackMu.Unlock()

	s.pcMu.Lock()
	pc := s.pc
	s.pcMu.Unlock()
	if pc == nil {
		return nil
	}
	if _, err := pc.AddTrack(track); err != nil {
		return fmt.Errorf("add track: %w", err)
	}
	return nil
}

// SetVideoTrackHandler registers a callback invoked on every remote video
// track received from the conference.
func (s *Session) SetVideoTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	s.videoTrackMu.Lock()
	defer s.videoTrackMu.Unlock()
	s.onVideoTrack = cb
}

func (s *Session) signalEnded(reason string) {
	s.bridgeReady.Store(false)
	if s.onEnded != nil {
		s.onEnded(reason)
	}
}

// normaliseHost strips an optional scheme and trailing slashes off a Jitsi
// host string. The j library expects a bare host; auth providers might pass
// a full URL through verbatim.
func normaliseHost(raw string) string {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, "://"); idx >= 0 {
		raw = raw[idx+3:]
	}
	raw = strings.TrimPrefix(raw, "//")
	raw = strings.TrimSuffix(raw, "/")
	if i := strings.Index(raw, "/"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

func init() { //nolint:gochecknoinits // engine registration is the canonical Go pattern for plugins
	engine.Register("jitsi", New)
}
