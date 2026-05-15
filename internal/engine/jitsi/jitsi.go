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
	"context"
	"encoding/base64"
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
)

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

	sendQueue   chan []byte
	bridgeReady atomic.Bool
	closed      atomic.Bool
	done        chan struct{}
	doneOnce    sync.Once
	cancel      context.CancelFunc
	runCtx      context.Context //nolint:containedctx // engine owns the supervisor lifetime
	wg          sync.WaitGroup

	videoTrackMu sync.RWMutex
	videoTracks  []webrtc.TrackLocal
	onVideoTrack func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
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
		host:      host,
		room:      room,
		name:      name,
		onData:    cfg.OnData,
		sendQueue: make(chan []byte, defaultSendQueueSize),
		done:      make(chan struct{}),
		cancel:    cancel,
		runCtx:    runCtx,
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

	logger.Infof("jitsi: joining %s/%s as %s …", s.host, s.room, s.name)
	jSess, err := j.Join(ctx, j.Config{
		Host:  s.host,
		Room:  s.room,
		Nick:  s.name,
		Debug: logger.IsVerbose(),
	})
	if err != nil {
		return fmt.Errorf("jitsi join: %w", err)
	}
	logger.Infof("jitsi: joined %s/%s; colibri-ws=%s", s.host, s.room, jSess.ColibriWS)
	s.jSess.Store(jSess)

	if s.onData != nil {
		bctx, bcancel := context.WithTimeout(ctx, bridgeOpenTimeout)
		err := jSess.OpenBridge(bctx)
		bcancel()
		if err != nil {
			return fmt.Errorf("open bridge: %w", err)
		}
		s.bridgeReady.Store(true)
		logger.Infof("jitsi: bridge open (endpoints=%v)", jSess.Endpoints())
	}

	if s.shouldNegotiatePC() {
		if err := s.negotiatePC(ctx, jSess); err != nil {
			return err
		}
	}

	s.wg.Add(2)
	go s.sendLoop()
	go s.recvLoop()
	return nil
}

func (s *Session) shouldNegotiatePC() bool {
	s.videoTrackMu.RLock()
	defer s.videoTrackMu.RUnlock()
	return len(s.videoTracks) > 0 || s.onVideoTrack != nil
}

func (s *Session) videoTrackHandler() func(*webrtc.TrackRemote, *webrtc.RTPReceiver) {
	s.videoTrackMu.RLock()
	defer s.videoTrackMu.RUnlock()
	return s.onVideoTrack
}

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

	// Jicofo emits Plan B style SDP with separate <content> sections per
	// media kind and SSRC-keyed source descriptors. pion's default
	// UnifiedPlan parser rejects this with "remote SessionDescription
	// semantics does not match configuration", so we explicitly request
	// Plan B for the conference PeerConnection.
	pcConfig := jSess.IceConfig()
	pcConfig.SDPSemantics = webrtc.SDPSemanticsPlanB

	pc, err := api.NewPeerConnection(pcConfig)
	if err != nil {
		return fmt.Errorf("new pc: %w", err)
	}

	s.videoTrackMu.RLock()
	for _, track := range s.videoTracks {
		if _, addErr := pc.AddTrack(track); addErr != nil {
			s.videoTrackMu.RUnlock()
			_ = pc.Close()
			return fmt.Errorf("add track: %w", addErr)
		}
	}
	s.videoTrackMu.RUnlock()

	pc.OnTrack(func(track *webrtc.TrackRemote, recv *webrtc.RTPReceiver) {
		if track.Kind() != webrtc.RTPCodecTypeVideo {
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
	if err := neg.Accept(ctx); err != nil {
		_ = pc.Close()
		return fmt.Errorf("session-accept: %w", err)
	}

	s.pcMu.Lock()
	s.pc = pc
	s.pcMu.Unlock()
	return nil
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
	if len(data) > bridgeMaxMessageSize {
		return ErrSendTooLarge
	}
	select {
	case s.sendQueue <- data:
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
			s.signalEnded("jitsi bridge closed")
		}
		return false
	}
	payload := decodeRaw(msg)
	if payload == nil {
		return true
	}
	s.onData(payload)
	return true
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
// Shutdown is performed in the order a Jitsi web client uses:
//
//  1. Mark the session closed so send/recv loops drop new work.
//  2. If a pion PeerConnection was negotiated, send Jingle
//     session-terminate to Jicofo so the conference state is updated and
//     the JVB bridge slot is freed promptly. Without this, Jicofo only
//     notices the participant is gone after the MUC presence-unavailable
//     stanza, and JVB only reclaims resources after a longer idle timeout.
//  3. Close the pion PeerConnection (stops media, sends DTLS bye).
//  4. Close the underlying j.Session, which closes the colibri-ws bridge,
//     sends MUC presence-unavailable, and tears down the XMPP transport.
//  5. Cancel the supervisor context and wait for goroutines.
func (s *Session) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}

	jSess := s.jSess.Load()
	if jSess != nil {
		s.terminateJingleSession(jSess)
	}

	s.pcMu.Lock()
	pc := s.pc
	s.pc = nil
	s.pcMu.Unlock()
	if pc != nil {
		_ = pc.Close()
	}

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

// terminateJingleSession sends a Jingle session-terminate stanza to Jicofo
// so the conference state is updated immediately. Sent even when no pion
// PeerConnection was negotiated: Jicofo allocates the JVB bridge slot the
// moment it dispatches session-initiate, regardless of whether the
// participant ever sent session-accept, and an explicit session-terminate
// frees that slot promptly.
func (s *Session) terminateJingleSession(jSess *j.Session) {
	neg := jSess.Negotiator()
	if neg == nil {
		return
	}
	if err := neg.Terminate("success"); err != nil {
		logger.Debugf("jitsi: session-terminate: %v", err)
	}
}

// SetReconnectCallback registers a callback for reconnection events.
//
// The Jitsi engine itself does not currently drive a reconnect loop; the
// callback is stored for API parity and wired through the carrier adapter
// for future use.
func (s *Session) SetReconnectCallback(cb func(*webrtc.DataChannel)) { s.onReconnect = cb }

// SetShouldReconnect stores the reconnect predicate (kept for API parity).
func (s *Session) SetShouldReconnect(fn func() bool) { s.shouldReconnect = fn }

// SetEndedCallback registers a function to call when the session ends.
func (s *Session) SetEndedCallback(cb func(string)) { s.onEnded = cb }

// WatchConnection blocks until the session is closed, the parent context
// fires, or the bridge tears down.
func (s *Session) WatchConnection(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-s.done:
		return
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
