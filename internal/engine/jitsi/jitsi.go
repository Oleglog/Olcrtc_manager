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
// two-peer tunnel model that olcrtc already accommodates.
package jitsi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/protect"
	"github.com/pion/ice/v4"
	pioninterceptor "github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
	"github.com/zarazaex69/j"
)

const (
	defaultSendQueueSize = 10000
	// bridgeFrameHeaderLen is added by encodeBridgeFrame before the payload
	// reaches the selected Jitsi bridge path: magic + local epoch + peer epoch.
	bridgeFrameHeaderLen = 12
	// bridgeMaxMessageSize is the maximum frame for a single colibri-ws
	// EndpointMessage. The upstream j benchmark shows 8 KiB is stable at
	// ~135 Mbit/s, while 16 KiB fluctuates. 12 KiB is a conservative
	// compromise that increases effective throughput by ~50% per message
	// compared to 8 KiB while staying well below the 16 KiB instability
	// threshold. Revert to 8*1024 if JVB closes the bridge.
	bridgeMaxMessageSize = 12 * 1024
	// sctpBridgeMaxMessageSize keeps the legacy SCTP fallback behaviour unless
	// OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE is explicitly set. Lower values such as
	// 1200 are useful for speed diagnostics, but changing the default alters the
	// smux frame size seen by both peers and can break mixed-version clients.
	sctpBridgeMaxMessageSize = bridgeMaxMessageSize
	// sendBatchCap is the maximum number of frames collected in a single
	// sendLoop drain pass before flushing them to the bridge. A larger batch
	// reduces per-frame overhead (JSON serialisation, base64, WS write syscall)
	// by allowing the bridge writeLoop to pipe them back-to-back without
	// yielding to the Go scheduler between individual messages.
	sendBatchCap = 64
	bridgeOpenTimeout    = 30 * time.Second
	defaultNick          = "olcrtc"
	credentialKeyRoom    = "room"
	videoTrackName       = "videochannel"
	bridgeModeAuto       = "auto"
	bridgeModeColibriWS  = "colibri-ws"
	bridgeModeSCTP       = "sctp"
	maxReconnects        = 50
	reconnectWindow      = 5 * time.Minute
	// reconnectGrace is the window after a successful self-reconnect during
	// which incoming peer-epoch changes do not trigger another reconnect.
	// Without this, the peer's own recovery can drive both sides into an
	// infinite reconnect ping-pong loop.
	reconnectGrace = 20 * time.Second
	// xmppKeepaliveInterval keeps the underlying XMPP transport alive while
	// we wait for a peer. Some Jitsi/Prosody deployments expire idle BOSH or
	// websocket sessions after roughly a minute.
	xmppKeepaliveInterval = 25 * time.Second
	// xmppKeepaliveTimeout bounds the IQ result wait. A half-open XMPP
	// connection can accept writes while no pong ever returns; treating that as
	// a reconnectable failure avoids waiting for the transport to time out later.
	xmppKeepaliveTimeout = 15 * time.Second
	reconnectJoinTimeout  = 30 * time.Second
)

// bridgeMagic tags every EndpointMessage produced by this engine. JVB broadcasts
// EndpointMessage payloads to every occupant of the MUC; the magic lets the
// receiver discard frames from unrelated applications (or unrelated olcrtc
// processes sharing the same room) before they reach the byte-stream layer.
// Without it, a stray peer's smux/handshake bytes parse as our protocol and
// deadlock the connection. 4 bytes is enough entropy for collision avoidance
// against real-world payloads while keeping the overhead negligible.
var bridgeMagic = [4]byte{'O', 'L', 'R', '1'} //nolint:gochecknoglobals // protocol constant
var fallbackEpoch atomic.Uint32               //nolint:gochecknoglobals // crypto/rand fallback counter

// vp8Keepalive is a minimal valid VP8 keyframe. It carries no useful picture
// data but parses as a genuine VP8 bitstream, so JVB accepts it as real media
// and refreshes the endpoint's lastRtpReceived timestamp. These are the same
// bytes the vp8channel transport uses for its idle keepalive.
var vp8Keepalive = []byte{ //nolint:gochecknoglobals // protocol constant
	0x30, 0x01, 0x00, 0x9d, 0x01, 0x2a, 0x10, 0x00,
	0x10, 0x00, 0x00, 0x47, 0x08, 0x85, 0x85, 0x88,
	0x99, 0x84, 0x88, 0xfc,
}

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
	// ErrColibriWSRequired is returned when bridge_mode=colibri-ws but Jicofo/JVB
	// did not advertise a colibri-ws URL in the Jingle offer.
	ErrColibriWSRequired = errors.New("jitsi colibri-ws required but not advertised")
	// errNoPeer is returned by reconnectFull when the WaitJingle timeout
	// fires because no peer has joined the room yet. This is not a real
	// reconnect failure; the session can keep waiting for a peer.
	errNoPeer = errors.New("no peer in room")
)

// Session is the Jitsi engine handle.
type Session struct {
	host       string
	room       string
	name       string
	insecure   bool
	bridgeMode string

	onData          func([]byte)
	onPeerData      func(peerID string, data []byte)
	onReconnect     func(*webrtc.DataChannel)
	shouldReconnect func() bool
	onEnded         func(string)

	jSess atomic.Pointer[j.Session]

	pcMu     sync.Mutex
	pc       *webrtc.PeerConnection
	pcCtx    context.Context    //nolint:containedctx // tied to PC lifetime, cancelled in teardownPC
	pcCancel context.CancelFunc // cancels pcCtx when the live PC is replaced

	sendQueue     chan []byte
	peerSendQueue chan bridgeOutbound
	bridgeReady   atomic.Bool
	sctpBridge    atomic.Bool
	closed        atomic.Bool
	reconnecting  atomic.Bool

	reconnectCh          chan struct{}
	reconnectMu          sync.Mutex // guards reconnectWindowStart, reconnectCount and lastReconnectAt
	reconnectWindowStart time.Time
	reconnectCount       int
	lastReconnectAt      atomic.Int64
	localEpoch           atomic.Uint32
	peerEpoch            atomic.Uint32

	// peerEndpoint latches the MUC nick of the first occupant whose
	// EndpointMessage passed the bridgeMagic check. Once set, all bridge
	// messages from other senders are dropped, isolating us from chatter by
	// unrelated olcrtc processes that happen to share the same room.
	peerEndpoint atomic.Pointer[string]
	peerEpochMu  sync.Mutex
	peerEpochs   map[string]uint32
	done         chan struct{}
	doneOnce     sync.Once
	cancel       context.CancelFunc
	trickleCancel context.CancelFunc
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

type bridgeOutbound struct {
	to   string
	data []byte
}

// New creates a new Jitsi engine session.
//
// cfg.URL carries the Jitsi host (e.g. "meet1.arbitr.ru") — populated by the
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

	var insecure bool
	if cfg.Extra != nil {
		if v, ok := cfg.Extra["insecure"]; ok {
			insecure = v == "true" || v == "1" || v == "yes"
		}
	}
	bridgeMode := resolveBridgeMode(cfg.Extra)
	logger.Infof("jitsi: config host=%s room=%s bridge_mode=%s insecure=%t", host, room, bridgeMode, insecure)

	runCtx, cancel := context.WithCancel(context.Background())
	s := &Session{
		host:          host,
		room:          room,
		name:          name,
		insecure:      insecure,
		bridgeMode:    bridgeMode,
		onData:        cfg.OnData,
		onPeerData:    cfg.OnPeerData,
		sendQueue:     make(chan []byte, defaultSendQueueSize),
		peerSendQueue: make(chan bridgeOutbound, defaultSendQueueSize),
		peerEpochs:    make(map[string]uint32),
		reconnectCh:   make(chan struct{}, 1),
		done:          make(chan struct{}),
		cancel:        cancel,
		runCtx:        runCtx,
	}
	s.localEpoch.Store(randomEpoch())
	return s, nil
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

func randomEpoch() uint32 {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		v := fallbackEpoch.Add(1)
		if v == 0 {
			return fallbackEpoch.Add(1)
		}
		return v
	}
	v := binary.BigEndian.Uint32(b[:])
	if v == 0 {
		return 1
	}
	return v
}

// Capabilities reports what this engine can do.
func (s *Session) Capabilities() engine.Capabilities {
	return engine.Capabilities{ByteStream: true, VideoTrack: true}
}

// Connect joins the Jitsi MUC (non-blocking) and waits for the Jingle
// session-initiate asynchronously. This avoids blocking on the timeout when
// no second participant is present — Jicofo only sends session-initiate once
// another peer joins the room.
func (s *Session) Connect(ctx context.Context) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}

	logger.Infof("jitsi: joining MUC %s/%s as %s …", s.host, s.room, s.name)
	jSess, err := j.JoinMUC(ctx, j.Config{
		Host:     s.host,
		Room:     s.room,
		Nick:     s.name,
		Debug:    logger.IsVerbose(),
		Insecure: s.insecure,
	})
	if err != nil {
		return fmt.Errorf("jitsi join muc: %w", err)
	}
	s.jSess.Store(jSess)
	logger.Infof("jitsi: MUC joined %s/%s; waiting for peer …", s.host, s.room)

	s.wg.Add(5)
	go s.sendLoop()
	go s.recvLoop()
	go s.waitForJingle()
	go s.bridgeKeepalive()
	go s.xmppKeepalive()
	return nil
}

// waitForJingle waits for Jicofo to send session-initiate (when a peer joins)
// and then opens the bridge channel and negotiates the PeerConnection.
func (s *Session) waitForJingle() {
	defer s.wg.Done()

	jSess := s.jSess.Load()
	if jSess == nil {
		return
	}

	stanza, err := jSess.Conn.WaitJingle(s.runCtx)
	if err != nil {
		if s.closed.Load() || s.runCtx.Err() != nil {
			return
		}
		logger.Warnf("jitsi: wait jingle failed: %v", err)
		s.requestReconnect("wait jingle failed: " + err.Error())
		return
	}
	_ = stanza // parsed below via completeJingleSetup path

	// Now do the full join (which will get the already-received jingle from LastJingleStanza).
	if err := s.completeJingleSetup(s.runCtx, jSess); err != nil {
		if !s.closed.Load() {
			logger.Warnf("jitsi: jingle setup failed: %v", err)
			if errors.Is(err, ErrColibriWSRequired) {
				s.signalEnded(err.Error())
				return
			}
			s.requestReconnect("jingle setup failed")
		}
	}
}

// completeJingleSetup opens the bridge and negotiates the PeerConnection after
// receiving session-initiate from Jicofo.
func (s *Session) completeJingleSetup(ctx context.Context, jSess *j.Session) error {
	s.logSessionDiagnostics("session-initiate", jSess)

	needBridge := s.onData != nil || s.onPeerData != nil
	sctpBridge, err := s.decideBridgePath("initial", needBridge, jSess)
	if err != nil {
		return err
	}

	if needBridge && !sctpBridge {
		if err := s.openBridgeWS(ctx, jSess); err != nil {
			return err
		}
	}

	if s.shouldNegotiatePC() {
		if err := s.negotiatePC(ctx, jSess, sctpBridge); err != nil {
			return err
		}
	}

	if sctpBridge {
		if err := s.openBridgeSCTP(ctx, jSess); err != nil {
			return err
		}
	}

	// Restart recvLoop now that bridge is ready.
	s.wg.Add(1)
	go s.recvLoop()
	return nil
}

func (s *Session) openBridgeWS(ctx context.Context, jSess *j.Session) error {
	start := time.Now()
	bctx, bcancel := context.WithTimeout(ctx, bridgeOpenTimeout)
	err := jSess.OpenBridge(bctx)
	bcancel()
	if err != nil {
		return fmt.Errorf("open bridge colibri-ws url=%q: %w", jSess.ColibriWS, err)
	}
	if br := jSess.Bridge(); br != nil {
		br.EnableRawMode()
	}
	s.peerEndpoint.Store(nil)
	s.peerVideoSSRC.Store(0)
	s.sctpBridge.Store(false)
	s.bridgeReady.Store(true)
	logger.Infof("jitsi: bridge path=colibri-ws open_ms=%d host=%s room=%s max_message=%d max_payload=%d url=%q endpoints=%v",
		time.Since(start).Milliseconds(), s.host, s.room, s.bridgeMaxMessageSize(), s.ByteStreamMaxPayloadSize(), jSess.ColibriWS, jSess.Endpoints())
	return nil
}

func (s *Session) openBridgeSCTP(ctx context.Context, jSess *j.Session) error {
	start := time.Now()
	bctx, bcancel := context.WithTimeout(ctx, bridgeOpenTimeout)
	err := jSess.WaitBridgeSCTP(bctx)
	bcancel()
	if err != nil {
		return fmt.Errorf("open bridge sctp: %w", err)
	}
	if br := jSess.Bridge(); br != nil {
		br.EnableRawMode()
	}
	s.peerEndpoint.Store(nil)
	s.peerVideoSSRC.Store(0)
	s.sctpBridge.Store(true)
	s.bridgeReady.Store(true)
	logger.Infof("jitsi: bridge path=sctp open_ms=%d host=%s room=%s max_message=%d max_payload=%d colibri_ws_advertised=%t endpoints=%v",
		time.Since(start).Milliseconds(), s.host, s.room, s.bridgeMaxMessageSize(), s.ByteStreamMaxPayloadSize(), jSess.ColibriWS != "", jSess.Endpoints())
	return nil
}

func resolveBridgeMode(extra map[string]string) string {
	mode := strings.TrimSpace(strings.ToLower(os.Getenv("OLCRTC_JITSI_BRIDGE_MODE")))
	if mode == "" && extra != nil {
		mode = strings.TrimSpace(strings.ToLower(extra["jitsi_bridge_mode"]))
	}
	if mode == "" && truthy(os.Getenv("OLCRTC_JITSI_REQUIRE_COLIBRI_WS")) {
		mode = bridgeModeColibriWS
	}
	if mode == "" && truthy(os.Getenv("OLCRTC_JITSI_FORCE_SCTP_BRIDGE")) {
		mode = bridgeModeSCTP
	}
	switch mode {
	case "", bridgeModeAuto:
		return bridgeModeAuto
	case bridgeModeColibriWS, "colibri", "ws", "websocket":
		return bridgeModeColibriWS
	case bridgeModeSCTP, "datachannel", "dc":
		return bridgeModeSCTP
	default:
		logger.Warnf("jitsi: unknown bridge_mode=%q, using auto", mode)
		return bridgeModeAuto
	}
}

func truthy(v string) bool {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

// ByteStreamMaxPayloadSize reports the largest payload callers should pass to
// Send before encodeBridgeFrame adds its bridge header. The SCTP fallback keeps
// the legacy 12 KiB bridge frame by default for mixed-version compatibility;
// smaller SCTP frames are opt-in via OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE.
func (s *Session) ByteStreamMaxPayloadSize() int {
	maxMessage := s.bridgeMaxMessageSize()
	if maxMessage <= bridgeFrameHeaderLen {
		return 1
	}
	return maxMessage - bridgeFrameHeaderLen
}

func (s *Session) bridgeMaxMessageSize() int {
	if !s.sctpBridge.Load() {
		return bridgeMaxMessageSize
	}
	return sctpBridgeMaxMessageSizeFromEnv()
}

func sctpBridgeMaxMessageSizeFromEnv() int {
	value := strings.TrimSpace(os.Getenv("OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE"))
	if value == "" {
		return sctpBridgeMaxMessageSize
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= bridgeFrameHeaderLen {
		logger.Warnf("jitsi: invalid OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE=%q, using %d", value, sctpBridgeMaxMessageSize)
		return sctpBridgeMaxMessageSize
	}
	return n
}

func (s *Session) decideBridgePath(stage string, needBridge bool, jSess *j.Session) (bool, error) {
	advertised := jSess.ColibriWS != ""
	if !needBridge {
		logger.Infof("jitsi: bridge decision stage=%s host=%s room=%s need_bridge=false pc_only=%t colibri_ws_advertised=%t",
			stage, s.host, s.room, s.shouldNegotiatePC(), advertised)
		return false, nil
	}

	switch s.bridgeMode {
	case bridgeModeColibriWS:
		if !advertised {
			logger.Warnf("jitsi: bridge decision stage=%s path=fail mode=colibri-ws host=%s room=%s reason=no-colibri-ws-in-jingle",
				stage, s.host, s.room)
			return false, ErrColibriWSRequired
		}
		logger.Infof("jitsi: bridge decision stage=%s path=colibri-ws mode=colibri-ws host=%s room=%s reason=required url=%q",
			stage, s.host, s.room, jSess.ColibriWS)
		return false, nil
	case bridgeModeSCTP:
		logger.Infof("jitsi: bridge decision stage=%s path=sctp mode=sctp host=%s room=%s reason=forced colibri_ws_advertised=%t url=%q",
			stage, s.host, s.room, advertised, jSess.ColibriWS)
		return true, nil
	default:
		if advertised {
			logger.Infof("jitsi: bridge decision stage=%s path=colibri-ws mode=auto host=%s room=%s reason=advertised url=%q",
				stage, s.host, s.room, jSess.ColibriWS)
			return false, nil
		}
		logger.Infof("jitsi: bridge decision stage=%s path=sctp mode=auto host=%s room=%s reason=no-colibri-ws-in-jingle",
			stage, s.host, s.room)
		return true, nil
	}
}

func (s *Session) logSessionDiagnostics(stage string, jSess *j.Session) {
	auth := jSess.ServerAuth
	logger.Infof("jitsi: %s host=%s room=%s jid=%s room_jid=%s colibri_ws_advertised=%t colibri_ws_url=%q ice_servers=%d candidates=%d datachannel=%t focus_ready=%t auth_required=%t guest_access=%t anonymous_xmpp=%t external_auth=%t visitors_supported=%t",
		stage, s.host, s.room, jSess.JID, jSess.RoomJID, jSess.ColibriWS != "", jSess.ColibriWS,
		len(jSess.ICEServers), len(jSess.Candidates), jSess.DataChannel != nil, auth.Ready,
		auth.AuthenticationRequired, auth.GuestAccess, auth.AnonymousXMPP, auth.ExternalAuth, auth.VisitorsSupported)
}

func (s *Session) shouldNegotiatePC() bool {
	if s.onData != nil {
		return true
	}
	if s.onPeerData != nil {
		return true
	}
	return s.shouldRequestVideo()
}

func (s *Session) shouldRequestVideo() bool {
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
func (s *Session) negotiatePC(ctx context.Context, jSess *j.Session, sctpBridge bool) error {
	settings := webrtc.SettingEngine{}
	settings.LoggerFactory = logger.NewPionLoggerFactory()

	// When Android provides a socket protector, force Pion to create ICE
	// sockets through the protected network adapter. Do not fall back to the
	// default network path if setup fails, otherwise WebRTC traffic can loop
	// back into the VPN tunnel.
	if protect.Protector != nil {
		pnet, perr := protect.NewProtectedNet()
		if perr != nil {
			return fmt.Errorf("protected net: %w", perr)
		}
		settings.SetNet(pnet)
		settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	}

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
	// When we have no local video we still need a video m-line; the choice
	// between a recvonly transceiver and a sendonly VP8 keepalive track matters
	// for endpoint liveness on JVB (see addVideoOrKeepaliveTrack).
	var kaTrack *webrtc.TrackLocalStaticSample
	if !hasLocalTracks {
		kaTrack, err = s.addVideoOrKeepaliveTrack(pc)
		if err != nil {
			_ = pc.Close()
			return err
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
	var failedAt atomic.Int64
	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		logger.Debugf("jitsi pc state: %s", state.String())
		if s.closed.Load() {
			return
		}
		switch state {
		case webrtc.PeerConnectionStateFailed:
			// Debounce: ICE/DTLS often flaps to Failed momentarily under
			// load (speedtest) and recovers on its own within a second.
			// Reconnecting on every flap caused an infinite teardown loop
			// because the new bridge sometimes flaps again before stabilising.
			// Only reconnect if Failed persists for >5s.
			now := time.Now().UnixNano()
			failedAt.Store(now)
			time.AfterFunc(5*time.Second, func() {
				if failedAt.Load() == now && !s.closed.Load() {
					s.requestReconnect("jitsi peer connection failed for >5s")
				}
			})
		case webrtc.PeerConnectionStateConnected:
			failedAt.Store(0)
		case webrtc.PeerConnectionStateUnknown,
			webrtc.PeerConnectionStateNew,
			webrtc.PeerConnectionStateConnecting,
			webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateClosed:
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
	trickleCtx, trickleCancel := context.WithCancel(ctx)
	s.trickleCancel = trickleCancel
	go s.trickleDrainLoop(trickleCtx, pc, neg, jSess.LowLevel().Stanzas())

	// If colibri-ws is unavailable, create the SCTP DataChannel on the
	// PeerConnection BEFORE Accept so it gets included in the SDP answer.
	if sctpBridge {
		if err := jSess.PrepareBridgeSCTP(pc); err != nil {
			_ = pc.Close()
			return fmt.Errorf("prepare bridge sctp: %w", err)
		}
	}

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

	if s.shouldRequestVideo() {
		// Tell JVB to forward video streams to this endpoint.
		if err := jSess.RequestVideo(ctx, 720); err != nil {
			logger.Debugf("jitsi: request video: %v", err)
		}
	}

	s.pcMu.Lock()
	s.pc = pc
	// Build a context that lives exactly as long as this PC instance.
	// teardownPC cancels pcCancel so goroutines bound to pcCtx exit before
	// a fresh PC takes its place.
	if s.pcCancel != nil {
		s.pcCancel()
	}
	s.pcCtx, s.pcCancel = context.WithCancel(s.runCtx)
	pcCtx := s.pcCtx
	s.pcMu.Unlock()

	// Keep the JVB endpoint alive on pure byte-stream paths by pumping real
	// VP8 RTP on the keepalive track. JVB tracks liveness via
	// lastIncomingActivity = max(lastRtpReceived, lastIceConsent); on TURN/SCTP
	// paths neither ICE consent nor an empty RTCP RR refreshes it (the RR writes
	// succeed yet the endpoint still expires — observed in production), so only
	// genuine RTP keeps the endpoint from being expired after
	// entity-expiration.timeout (~1 min), which is what triggers the DTLS
	// close_notify and the reconnect cascade.
	//
	// We deliberately dropped the old RTCP-RR keepalive: besides being
	// ineffective, its "give up after N write errors -> reconnect" guard fired
	// during the DTLS handshake window (WriteRTCP fails until DTLS is up) and
	// tore down connections that were still establishing, turning a slow
	// ICE/DTLS bring-up into a permanent reconnect loop. rtpKeepalive only logs
	// write failures and never self-reconnects.
	//
	// Bound to pcCtx so teardownPC stops it and the next negotiatePC (including
	// a reinitiate) starts a fresh one. Only the pure byte-stream path gets a
	// keepalive track; video-receive paths leave kaTrack nil.
	if kaTrack != nil {
		s.wg.Add(1)
		go s.rtpKeepalive(pcCtx, kaTrack) //nolint:contextcheck // pcCtx derives from s.runCtx
	}

	return nil
}

// rtpKeepalive pumps a tiny VP8 keyframe onto the sendonly keepalive track
// roughly once a second. It is the byte-stream (datachannel) path's liveness
// signal to JVB.
//
// JVB expires an endpoint once lastIncomingActivity = max(lastRtpReceived,
// lastIceConsent) goes stale for longer than entity-expiration.timeout
// (~1 min), then tears down DTLS with a close_notify. On byte-stream paths no
// media flows, ICE consent refresh is unreliable over TURN, and an empty RTCP
// RR does not refresh lastRtpReceived on this build — so without real RTP the
// endpoint dies every ~60-80s and the engine churns through an endless
// reconnect cascade (works for hours, then a single bridge blip drops us into
// the loop with no way out). A genuine VP8 packet is the one signal JVB's
// activity tracker honours.
//
// Bound to pcCtx so it exits on teardownPC; negotiatePC starts a fresh
// instance on every (re)negotiation, including a reinitiate, which is exactly
// what keeps the reconnected bridge alive instead of expiring again.
func (s *Session) rtpKeepalive(pcCtx context.Context, track *webrtc.TrackLocalStaticSample) {
	defer s.wg.Done()
	const interval = time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	sample := media.Sample{Data: vp8Keepalive, Duration: interval}
	for {
		select {
		case <-s.done:
			return
		case <-pcCtx.Done():
			return
		case <-ticker.C:
			if pcCtx.Err() != nil {
				return
			}
			if err := track.WriteSample(sample); err != nil {
				if s.closed.Load() || pcCtx.Err() != nil {
					return
				}
				// WriteSample fails until the sender is bound (DTLS/ICE up);
				// that is transient, so log and keep ticking rather than
				// tearing the bridge down.
				logger.Debugf("jitsi: rtp keepalive write: %v", err)
			}
		}
	}
}

// addVideoOrKeepaliveTrack adds the video m-line when no local tracks are
// present. It defaults to a recvonly transceiver so the SDP answer carries a
// video m-line and JVB sets up a forwarding path (known-good since v1.9.8).
//
// The sendonly VP8 keepalive (ba617b5) pumps real RTP to keep the JVB endpoint
// alive where idle endpoints expire, but a sendonly video m-line stops some
// JVBs (observed on meet.handyweb.org) from establishing a forwarding path,
// which stalls ICE/DTLS and times bring-up out at bridgeOpenTimeout. So the
// keepalive is opt-in via OLCRTC_JITSI_VIDEO_KEEPALIVE and only applies to pure
// byte-stream sessions; video-receiver paths always use recvonly. AddTrack and
// AddTransceiverFromKind(video,recvonly) are mutually exclusive in Plan B, so
// only one of the two is ever added.
func (s *Session) addVideoOrKeepaliveTrack(pc *webrtc.PeerConnection) (*webrtc.TrackLocalStaticSample, error) {
	if !s.wantsVideoReceive() && truthy(os.Getenv("OLCRTC_JITSI_VIDEO_KEEPALIVE")) {
		kaTrack, err := newKeepaliveTrack()
		if err != nil {
			return nil, fmt.Errorf("create keepalive track: %w", err)
		}
		if _, addErr := pc.AddTrack(kaTrack); addErr != nil {
			return nil, fmt.Errorf("add keepalive track: %w", addErr)
		}
		return kaTrack, nil
	}
	if _, err := pc.AddTransceiverFromKind(
		webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		return nil, fmt.Errorf("add video recvonly: %w", err)
	}
	return nil, nil //nolint:nilnil // nil track signals no keepalive needed
}

// wantsVideoReceive reports whether the carrier expects to receive remote
// video (a handler is registered). When false and there are no local tracks,
// the session is a pure byte-stream and needs the RTP keepalive track.
func (s *Session) wantsVideoReceive() bool {
	s.videoTrackMu.RLock()
	defer s.videoTrackMu.RUnlock()
	return s.onVideoTrack != nil
}

// newKeepaliveTrack builds a fresh sendonly VP8 track for the RTP keepalive.
// IDs are randomised per negotiation because Jitsi rejects session-accept when
// an msid collides with another participant in the conference.
func newKeepaliveTrack() (*webrtc.TrackLocalStaticSample, error) {
	t, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000},
		"jitsi-ka-"+randomTrackSuffix(),
		"olcrtc-ka-"+randomTrackSuffix(),
	)
	if err != nil {
		return nil, fmt.Errorf("new keepalive track: %w", err)
	}
	return t, nil
}

// randomTrackSuffix returns a short unique token for track/stream IDs.
func randomTrackSuffix() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// bridgeKeepalive sends a lightweight colibri-ws message every 10 seconds so
// JVB updates its endpoint lastActivity timestamp. Without this, JVB expires
// the endpoint after its inactivity timeout (~30-60s) when the ICE/DTLS path
// is routed through a TURN relay whose allocation silently dies.
func (s *Session) bridgeKeepalive() {
	defer s.wg.Done()
	const interval = 10 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			if !s.bridgeReady.Load() {
				continue
			}
			jSess := s.jSess.Load()
			if jSess == nil {
				continue
			}
			frame, err := s.encodeBridgeFrame(nil, "")
			if err != nil {
				continue
			}
			if err := jSess.BridgeSendRaw("", frame); err != nil {
				logger.Debugf("jitsi: bridge keepalive send: %v", err)
			}
		}
	}
}

// xmppKeepalive periodically sends an XMPP ping IQ so that the underlying
// transport keeps observing application traffic while the engine is idle.
func (s *Session) xmppKeepalive() {
	defer s.wg.Done()
	ticker := time.NewTicker(xmppKeepaliveInterval)
	defer ticker.Stop()
	var lastReconnectRequestErr string
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			jSess := s.jSess.Load()
			if jSess == nil {
				continue
			}
			conn := jSess.LowLevel()
			if conn == nil {
				continue
			}
			id := conn.NextID()
			// Target the XMPP virtualhost from the bound JID, not necessarily the
			// public web host. Some Jitsi deployments serve the web UI on one host
			// while Prosody is bound to another domain; pinging the web host can be
			// rejected with not-allowed and leave the idle keepalive ineffective.
			ping := fmt.Sprintf(
				`<iq type="get" to=%q id=%q xmlns="jabber:client"><ping xmlns="urn:xmpp:ping"/></iq>`,
				xmppDomain(conn.JID(), conn.Host()), id,
			)
			if _, err := conn.SendIQWait(ping, id, xmppKeepaliveTimeout); err != nil {
				if s.closed.Load() {
					return
				}
				logger.Debugf("jitsi: xmpp keepalive: %v", err)
				if reason := err.Error(); reason != lastReconnectRequestErr {
					s.requestReconnect("xmpp keepalive: " + reason)
					lastReconnectRequestErr = reason
				}
				continue
			}
			lastReconnectRequestErr = ""
		}
	}
}

// xmppDomain extracts the XMPP domain from a bound JID of the form
// "node@domain/resource". It returns fallback when jid has no domain part, so
// malformed or empty JIDs degrade to the previous web-host target instead of an
// empty to-address.
func xmppDomain(jid, fallback string) string {
	_, rest, ok := strings.Cut(jid, "@")
	if !ok || rest == "" {
		return fallback
	}
	if domain, _, found := strings.Cut(rest, "/"); found {
		rest = domain
	}
	if rest == "" {
		return fallback
	}
	return rest
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
func (s *Session) trickleDrainLoop(ctx context.Context, pc *webrtc.PeerConnection, neg negotiator, stanzas <-chan string) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
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
	framed, err := s.encodeBridgeFrame(data, "")
	if err != nil {
		return err
	}
	return s.enqueueBridgeFrame(framed)
}

// SendTo queues data for transmission to a specific Jitsi endpoint.
func (s *Session) SendTo(peerID string, data []byte) error {
	if peerID == "" {
		return s.Send(data)
	}
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if !s.bridgeReady.Load() {
		return ErrBridgeNotReady
	}
	framed, err := s.encodeBridgeFrame(data, peerID)
	if err != nil {
		return err
	}
	return s.enqueuePeerBridgeFrame(peerID, framed)
}

func (s *Session) encodeBridgeFrame(data []byte, peerID string) ([]byte, error) {
	const epochHeaderLen = 8
	if len(data)+bridgeFrameHeaderLen > s.bridgeMaxMessageSize() {
		return nil, ErrSendTooLarge
	}
	framed := make([]byte, len(bridgeMagic)+epochHeaderLen+len(data))
	copy(framed, bridgeMagic[:])
	off := len(bridgeMagic)
	binary.BigEndian.PutUint32(framed[off:off+4], s.localEpoch.Load())
	binary.BigEndian.PutUint32(framed[off+4:off+epochHeaderLen], s.peerEpochFor(peerID))
	copy(framed[off+epochHeaderLen:], data)
	return framed, nil
}

func (s *Session) peerEpochFor(peerID string) uint32 {
	if peerID == "" || s.onPeerData == nil {
		return s.peerEpoch.Load()
	}
	s.peerEpochMu.Lock()
	defer s.peerEpochMu.Unlock()
	return s.peerEpochs[peerID]
}

func (s *Session) enqueueBridgeFrame(framed []byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if !s.bridgeReady.Load() {
		return ErrBridgeNotReady
	}
	if len(framed) > s.bridgeMaxMessageSize() {
		return ErrSendTooLarge
	}
	select {
	case s.sendQueue <- framed:
		return nil
	case <-s.done:
		return ErrSessionClosed
	default:
		return ErrSendQueueFull
	}
}

func (s *Session) enqueuePeerBridgeFrame(peerID string, framed []byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	if !s.bridgeReady.Load() {
		return ErrBridgeNotReady
	}
	if len(framed) > s.bridgeMaxMessageSize() {
		return ErrSendTooLarge
	}
	select {
	case s.peerSendQueue <- bridgeOutbound{to: peerID, data: framed}:
		return nil
	case <-s.done:
		return ErrSessionClosed
	default:
		return ErrSendQueueFull
	}
}

func (s *Session) sendLoop() {
	defer s.wg.Done()
	// Pre-allocate a reusable batch slice. Each drain pass appends pending
	// frames here, flushes them, then resets the slice for the next round.
	// Cap is fixed so the slice never needs to grow on the hot path.
	batch := make([][]byte, 0, sendBatchCap)
	peerBatch := make([]bridgeOutbound, 0, sendBatchCap)

	for {
		select {
		case <-s.done:
			return
		case data, ok := <-s.sendQueue:
			if !ok {
				return
			}
			batch = append(batch[:0], data)
			// Drain any additional frames that are already queued. This
			// converts what would be N separate WS writes (each with its
			// own JSON serialisation + base64 + syscall) into a single
			// burst of writes that the bridge writeLoop can pipe through
			// the TCP socket back-to-back, minimising inter-frame gaps.
		drain:
			for {
				select {
				case more, ok := <-s.sendQueue:
					if !ok {
						break drain
					}
					batch = append(batch, more)
					if len(batch) >= cap(batch) {
						break drain
					}
				default:
					break drain
				}
			}
			for _, frame := range batch {
				s.sendBridgeFrame("", frame)
			}
		case frame, ok := <-s.peerSendQueue:
			if !ok {
				return
			}
			peerBatch = append(peerBatch[:0], frame)
		peerDrain:
			for {
				select {
				case more, ok := <-s.peerSendQueue:
					if !ok {
						break peerDrain
					}
					peerBatch = append(peerBatch, more)
					if len(peerBatch) >= cap(peerBatch) {
						break peerDrain
					}
				default:
					break peerDrain
				}
			}
			for _, pf := range peerBatch {
				s.sendBridgeFrame(pf.to, pf.data)
			}
		}
	}
}

func (s *Session) sendBridgeFrame(to string, data []byte) {
	if !s.outboundFrameCurrent(data) {
		return
	}
	jSess := s.waitJSession()
	if jSess == nil {
		return
	}
	if !s.outboundFrameCurrent(data) {
		return
	}
	if err := jSess.BridgeSendRaw(to, data); err != nil {
		if s.closed.Load() {
			return
		}
		logger.Debugf("jitsi bridge send: %v", err)
	}
}

func (s *Session) waitJSession() *j.Session {
	const retryDelay = 10 * time.Millisecond
	for {
		if s.closed.Load() {
			return nil
		}
		jSess := s.jSess.Load()
		if jSess != nil {
			return jSess
		}
		select {
		case <-s.done:
			return nil
		case <-time.After(retryDelay):
		}
	}
}

func (s *Session) outboundFrameCurrent(frame []byte) bool {
	const epochHeaderLen = 8
	if len(frame) < len(bridgeMagic)+epochHeaderLen {
		return false
	}
	off := len(bridgeMagic)
	return binary.BigEndian.Uint32(frame[off:off+4]) == s.localEpoch.Load()
}

func (s *Session) recvLoop() {
	defer s.wg.Done()

	jSess := s.jSess.Load()
	if jSess == nil || (s.onData == nil && s.onPeerData == nil) || !s.bridgeReady.Load() {
		return
	}
	if br := jSess.Bridge(); br != nil {
		rawFrames := br.RawFrames()
		for {
			select {
			case <-s.done:
				return
			case frame, ok := <-rawFrames:
				if !s.deliverRawBridgeFrame(frame, ok) {
					return
				}
			}
		}
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
	payload, valid := bridgePayload(msg)
	if !valid {
		return true
	}
	if s.onPeerData != nil && msg.From != "" {
		return s.deliverPeerBridgePayload(msg.From, payload)
	}
	if !s.peerLatchAccepts(msg.From) {
		return true
	}
	data, ok := s.acceptEpochFrame(payload)
	if !ok {
		return true
	}
	if len(data) == 0 {
		return true
	}
	s.onData(data)
	return true
}

type bridgeRawFrame struct {
	Class string `json:"colibriClass"`
	From  string `json:"from"`
	Raw   string `json:"raw"`
}

func (s *Session) deliverRawBridgeFrame(frame []byte, ok bool) bool {
	if !ok {
		if !s.closed.Load() {
			s.requestReconnect("jitsi bridge closed")
		}
		return false
	}
	var msg bridgeRawFrame
	if err := json.Unmarshal(frame, &msg); err != nil || msg.Class != "EndpointMessage" || msg.Raw == "" {
		return true
	}
	payload, err := base64.StdEncoding.DecodeString(msg.Raw)
	if err != nil || len(payload) < len(bridgeMagic) || !bytes.Equal(payload[:len(bridgeMagic)], bridgeMagic[:]) {
		return true
	}
	if s.onPeerData != nil && msg.From != "" {
		return s.deliverPeerBridgePayload(msg.From, payload)
	}
	if !s.peerLatchAccepts(msg.From) {
		return true
	}
	data, ok := s.acceptEpochFrame(payload)
	if !ok || len(data) == 0 {
		return true
	}
	s.onData(data)
	return true
}

func bridgePayload(msg j.BridgeMessage) ([]byte, bool) {
	payload := decodeRaw(msg)
	if payload == nil {
		return nil, false
	}
	if len(payload) < len(bridgeMagic) || !bytes.Equal(payload[:len(bridgeMagic)], bridgeMagic[:]) {
		return nil, false
	}
	return payload, true
}

func (s *Session) deliverPeerBridgePayload(from string, payload []byte) bool {
	data, ok := s.acceptPeerEpochFrame(from, payload)
	if !ok || len(data) == 0 {
		return true
	}
	s.onPeerData(from, data)
	return true
}

func (s *Session) acceptPeerEpochFrame(from string, payload []byte) ([]byte, bool) {
	const epochHeaderLen = 8
	if len(payload) < len(bridgeMagic)+epochHeaderLen {
		return nil, false
	}
	off := len(bridgeMagic)
	senderEpoch := binary.BigEndian.Uint32(payload[off : off+4])
	receiverEpoch := binary.BigEndian.Uint32(payload[off+4 : off+epochHeaderLen])
	if senderEpoch == 0 || senderEpoch == s.localEpoch.Load() {
		return nil, false
	}
	if receiverEpoch != 0 && receiverEpoch != s.localEpoch.Load() {
		logger.Debugf("jitsi: drop stale bridge frame peerEpoch=0x%08x localEpoch=0x%08x",
			receiverEpoch, s.localEpoch.Load())
		return nil, false
	}
	s.peerEpochMu.Lock()
	prev := s.peerEpochs[from]
	if prev == 0 || prev != senderEpoch {
		s.peerEpochs[from] = senderEpoch
	}
	s.peerEpochMu.Unlock()
	return payload[off+epochHeaderLen:], true
}

func (s *Session) acceptEpochFrame(payload []byte) ([]byte, bool) {
	const epochHeaderLen = 8
	if len(payload) < len(bridgeMagic)+epochHeaderLen {
		return nil, false
	}
	off := len(bridgeMagic)
	senderEpoch := binary.BigEndian.Uint32(payload[off : off+4])
	receiverEpoch := binary.BigEndian.Uint32(payload[off+4 : off+epochHeaderLen])
	if senderEpoch == 0 || senderEpoch == s.localEpoch.Load() {
		return nil, false
	}
	if receiverEpoch != 0 && receiverEpoch != s.localEpoch.Load() {
		logger.Debugf("jitsi: drop stale bridge frame peerEpoch=0x%08x localEpoch=0x%08x",
			receiverEpoch, s.localEpoch.Load())
		return nil, false
	}
	// Epoch is a deduplication marker for stale frames during reconnect, not
	// a signal that this side must reconnect. If the peer recovered and emits
	// a fresh epoch, accept the new epoch instead of starting a reconnect ping-
	// pong cycle.
	prev := s.peerEpoch.Load()
	if prev == 0 {
		s.peerEpoch.Store(senderEpoch)
	} else if prev != senderEpoch {
		s.peerEpoch.CompareAndSwap(prev, senderEpoch)
		if s.inReconnectGrace() {
			logger.Debugf("jitsi: peer epoch changed during grace period (0x%08x -> 0x%08x)",
				prev, senderEpoch)
		} else {
			logger.Debugf("jitsi: peer epoch changed (0x%08x -> 0x%08x), accepting fresh peer state",
				prev, senderEpoch)
		}
	}
	return payload[off+epochHeaderLen:], true
}

func (s *Session) inReconnectGrace() bool {
	last := s.lastReconnectAt.Load()
	if last == 0 {
		return false
	}
	return time.Since(time.Unix(0, last)) < reconnectGrace
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

// ResetPeer clears endpoint/epoch binding after an upper-layer handshake
// failure so the next fresh peer in the room is not ignored because a stale
// participant spoke first.
func (s *Session) ResetPeer() {
	s.peerEndpoint.Store(nil)
	s.peerEpoch.Store(0)
	s.resetPeerEpochs()
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

// Reconnect asks the jitsi session to tear down its bridge connection and
// re-establish it. Triggered by upper layers when liveness probes declare the
// carrier dead before jitsi has noticed.
func (s *Session) Reconnect(reason string) { s.requestReconnect(reason) }

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
	// Track consecutive failures, not total reconnect events over the session.
	// A successful reconnect resets the counter so long-lived sessions can
	// recover from legitimate peer churn and JVB restarts.
	for {
		s.reconnectMu.Lock()
		failures := s.reconnectCount
		s.reconnectMu.Unlock()
		if failures >= maxReconnects {
			s.signalEnded("jitsi reconnect limit reached")
			return true
		}

		backoff := time.Duration(failures) * 2 * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		err := s.reconnect(ctx)
		if err == nil {
			s.reconnectMu.Lock()
			s.reconnectCount = 0
			s.reconnectWindowStart = time.Time{}
			s.reconnectMu.Unlock()
			s.drainReconnectQueue()
			return false
		}

		if errors.Is(err, ErrColibriWSRequired) {
			logger.Warnf("jitsi: non-retriable bridge configuration error: %v", err)
			s.signalEnded(err.Error())
			s.drainReconnectQueue()
			return true
		}

		if errors.Is(err, errNoPeer) {
			logger.Infof("jitsi: waiting for peer in room (not a failure)")
			s.reconnectMu.Lock()
			s.reconnectCount = 0
			s.reconnectWindowStart = time.Time{}
			s.reconnectMu.Unlock()
			s.drainReconnectQueue()
			return false
		}

		logger.Warnf("jitsi reconnect failed: %v", err)
		s.reconnectMu.Lock()
		s.reconnectCount++
		if s.reconnectWindowStart.IsZero() {
			s.reconnectWindowStart = time.Now()
		}
		s.reconnectMu.Unlock()

		select {
		case <-ctx.Done():
			return true
		case <-s.done:
			return true
		case <-time.After(backoff):
		}
	}
}

func (s *Session) reconnect(ctx context.Context) error {
	if !s.reconnecting.CompareAndSwap(false, true) {
		return nil
	}
	defer s.reconnecting.Store(false)

	s.bridgeReady.Store(false)
	s.teardownPC()

	s.localEpoch.Store(randomEpoch())
	s.peerEpoch.Store(0)
	s.resetPeerEpochs()
	s.drainSendQueue()

	// Re-establish the XMPP/MUC session from scratch instead of using the
	// lightweight Rejoin path. Fresh JoinMUC re-runs focus allocation, which is
	// more reliable after the room was left idle and Jicofo expired state.
	if old := s.jSess.Swap(nil); old != nil {
		_ = old.Close()
	}

	logger.Infof("jitsi: rejoin %s/%s (non-blocking) ...", s.host, s.room)
	joinCtx, joinCancel := context.WithTimeout(ctx, reconnectJoinTimeout)
	jSess, err := j.JoinMUC(joinCtx, j.Config{
		Host:     s.host,
		Room:     s.room,
		Nick:     s.name,
		Debug:    logger.IsVerbose(),
		Insecure: s.insecure,
	})
	joinCancel()
	if err != nil {
		logger.Warnf("jitsi: rejoin failed: %v, full reconnect", err)
		return s.reconnectFull(ctx)
	}
	s.jSess.Store(jSess)

	const reinitiateTimeout = 30 * time.Second
	reinitCtx, reinitCancel := context.WithTimeout(ctx, reinitiateTimeout)
	_, err = jSess.WaitJingleReinitiate(reinitCtx)
	reinitCancel()
	if err != nil {
		logger.Warnf("jitsi: wait reinitiate failed: %v, full reconnect", err)
		return s.reconnectFull(ctx)
	}

	if err := s.reinitiateBridge(ctx, jSess); err != nil {
		return err
	}

	s.peerEndpoint.Store(nil)
	s.peerVideoSSRC.Store(0)
	s.bridgeReady.Store(true)

	s.wg.Add(1)
	go s.recvLoop()

	if err := s.Send(nil); err != nil {
		logger.Debugf("jitsi: epoch announce failed: %v", err)
	}
	if s.onReconnect != nil {
		s.onReconnect(nil)
	}
	s.lastReconnectAt.Store(time.Now().UnixNano())
	logger.Infof("jitsi: reconnected %s/%s (reinitiate); colibri-ws=%s", s.host, s.room, jSess.ColibriWS)
	return nil
}

// teardownPC closes the current PeerConnection and cancels the trickle loop.
// Keeps the XMPP session alive.
func (s *Session) teardownPC() {
	s.pcMu.Lock()
	oldPC := s.pc
	s.pc = nil
	pcCancel := s.pcCancel
	s.pcCancel = nil
	s.pcCtx = nil
	s.pcMu.Unlock()
	if pcCancel != nil {
		pcCancel()
	}
	if s.trickleCancel != nil {
		s.trickleCancel()
		s.trickleCancel = nil
	}
	if oldPC != nil {
		_ = oldPC.Close()
	}
}

// reinitiateBridge negotiates a new PeerConnection and opens the bridge channel
// (SCTP or WS depending on jSess.ColibriWS). On any failure falls back to a
// full reconnect.
func (s *Session) reinitiateBridge(ctx context.Context, jSess *j.Session) error {
	s.logSessionDiagnostics("reinitiate", jSess)
	sctpBridge, err := s.decideBridgePath("reinitiate", true, jSess)
	if err != nil {
		return err
	}
	if err := s.negotiatePC(ctx, jSess, sctpBridge); err != nil {
		logger.Warnf("jitsi: negotiate after reinitiate failed: %v — full reconnect", err)
		return s.reconnectFull(ctx)
	}
	if sctpBridge {
		if err := s.openBridgeSCTP(ctx, jSess); err != nil {
			logger.Warnf("jitsi: bridge after reinitiate failed: %v — full reconnect", err)
			return s.reconnectFull(ctx)
		}
	} else {
		if err := s.openBridgeWS(ctx, jSess); err != nil {
			logger.Warnf("jitsi: bridge after reinitiate failed: %v — full reconnect", err)
			return s.reconnectFull(ctx)
		}
	}
	return nil
}

// reconnectFull tears down everything and does a full rejoin (blocking on session-initiate).
func (s *Session) reconnectFull(ctx context.Context) error {
	if old := s.jSess.Swap(nil); old != nil {
		_ = old.Close()
	}
	s.localEpoch.Store(randomEpoch())
	s.peerEpoch.Store(0)
	s.resetPeerEpochs()
	s.drainSendQueue()

	const fullReconnectTimeout = 60 * time.Second

	logger.Infof("jitsi: full reconnect %s/%s as %s ...", s.host, s.room, s.name)

	joinCtx, joinCancel := context.WithTimeout(ctx, reconnectJoinTimeout)
	jSess, err := j.JoinMUC(joinCtx, j.Config{
		Host:     s.host,
		Room:     s.room,
		Nick:     s.name,
		Debug:    logger.IsVerbose(),
		Insecure: s.insecure,
	})
	joinCancel()
	if err != nil {
		return fmt.Errorf("jitsi join: %w", err)
	}

	bctx, bcancel := context.WithTimeout(ctx, fullReconnectTimeout)
	_, err = jSess.Conn.WaitJingle(bctx)
	bcancel()
	if err != nil {
		// No peer may be present yet. Park the fresh MUC session and restart the
		// async waiter instead of counting this as a reconnect failure.
		s.jSess.Store(jSess)
		s.wg.Add(1)
		go s.waitForJingle()
		return errNoPeer
	}

	if err := s.completeJingleSetup(ctx, jSess); err != nil {
		_ = jSess.Close()
		return fmt.Errorf("jitsi setup after full reconnect: %w", err)
	}
	s.jSess.Store(jSess)
	s.peerEndpoint.Store(nil)
	s.peerVideoSSRC.Store(0)
	s.bridgeReady.Store(true)

	s.wg.Add(1)
	go s.recvLoop()

	if err := s.Send(nil); err != nil {
		logger.Debugf("jitsi: epoch announce failed: %v", err)
	}
	if s.onReconnect != nil {
		s.onReconnect(nil)
	}
	s.lastReconnectAt.Store(time.Now().UnixNano())
	logger.Infof("jitsi: reconnected %s/%s (full); colibri-ws=%s", s.host, s.room, jSess.ColibriWS)
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

func (s *Session) drainSendQueue() {
	for {
		select {
		case <-s.sendQueue:
		case <-s.peerSendQueue:
		default:
			return
		}
	}
}

func (s *Session) resetPeerEpochs() {
	s.peerEpochMu.Lock()
	clear(s.peerEpochs)
	s.peerEpochMu.Unlock()
}

// CanSend reports whether the session is ready to accept new data.
func (s *Session) CanSend() bool {
	if s.closed.Load() {
		return false
	}
	if s.onData == nil && s.onPeerData == nil {
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
	return uint64(depth) * uint64(s.bridgeMaxMessageSize())
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
