// Package server implements the olcrtc tunnel server logic.
package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/handshake"
	"github.com/openlibrecommunity/olcrtc/internal/link"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/muxconn"
	"github.com/openlibrecommunity/olcrtc/internal/names"
	"github.com/openlibrecommunity/olcrtc/internal/protect"
	"github.com/xtaci/smux"
	"golang.org/x/net/proxy"
)

const connectCommand = "connect"

var (
	// ErrKeyRequired is returned when no encryption key is provided.
	ErrKeyRequired = errors.New("key required (use -key <hex>)")
	// ErrKeySize is returned when the encryption key is not 32 bytes.
	ErrKeySize = errors.New("key must be 32 bytes")
	// ErrSocks5AuthFailed is returned when SOCKS5 authentication fails.
	ErrSocks5AuthFailed = errors.New("SOCKS5 auth failed")
	// ErrSocks5ConnectFailed is returned when SOCKS5 connection fails.
	ErrSocks5ConnectFailed = errors.New("SOCKS5 connect failed")
)

// SessionOpenFunc is called after a successful handshake, before the server
// accepts tunnel streams on that session.
type SessionOpenFunc func(sessionID, deviceID string, claims map[string]any)

// SessionCloseFunc is called when a session is torn down. Possible reasons:
// "reconnect" (carrier dropped and was reestablished), "closed" (graceful
// shutdown or ctx cancel).
type SessionCloseFunc func(sessionID, reason string)

// TrafficFunc is called once per tunnel stream, after the copy loops finish.
// bytesIn counts client→target bytes; bytesOut counts target→client bytes.
type TrafficFunc func(sessionID, addr string, bytesIn, bytesOut uint64)

// Server handles incoming tunnel connections and proxies their traffic.
type Server struct {
	ln             link.Link
	cipher         *crypto.Cipher
	conn           *muxconn.Conn
	session        *smux.Session
	sessMu         sync.RWMutex
	reinstallMu    sync.Mutex
	wg             sync.WaitGroup
	authHook       handshake.AuthFunc
	onOpen         SessionOpenFunc
	onClose        SessionCloseFunc
	onTraffic      TrafficFunc
	deviceID       string
	sessionID      string
	dnsServer      string
	resolver       *net.Resolver
	socksProxyAddr string
	socksProxyPort int
	socksProxyUser string
	socksProxyPass string
	warpProxyAddr  string
	warpProxyPort  int
}

// ConnectRequest is a message from the client to establish a new connection.
type ConnectRequest struct {
	Cmd  string `json:"cmd"`
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

// Config holds runtime configuration for [Run].
type Config struct {
	Link            string
	Transport       string
	Carrier         string
	RoomURL         string
	KeyHex          string
	DNSServer       string
	SOCKSProxyAddr  string
	SOCKSProxyPort  int
	SOCKSProxyUser  string
	SOCKSProxyPass  string
	WarpProxyAddr   string
	WarpProxyPort   int
	VideoWidth      int
	VideoHeight     int
	VideoFPS        int
	VideoBitrate    string
	VideoHW         string
	VideoQRSize     int
	VideoQRRecovery string
	VideoCodec      string
	VideoTileModule int
	VideoTileRS     int
	VP8FPS          int
	VP8BatchSize    int
	SEIFPS          int
	SEIBatchSize    int
	SEIFragmentSize int
	SEIAckTimeoutMS int
	Engine          string
	URL             string
	Token           string

	// AuthHook is invoked after CLIENT_HELLO to authorize the client and
	// return a session ID. If nil, every client is admitted with a random UUID.
	AuthHook handshake.AuthFunc

	// OnSessionOpen fires after a successful handshake. Nil means no-op.
	OnSessionOpen SessionOpenFunc
	// OnSessionClose fires when the session is torn down (reconnect, closed). Nil means no-op.
	OnSessionClose SessionCloseFunc
	// OnTraffic fires once per tunnel stream after both copy loops finish. Nil means no-op.
	OnTraffic TrafficFunc
}

// Run starts the server with the given configuration.
func Run(ctx context.Context, cfg Config) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cipher, err := setupCipher(cfg.KeyHex)
	if err != nil {
		return fmt.Errorf("setupCipher failed: %w", err)
	}

	// Install the SOCKS5 config globally so all HTTP clients used by
	// auth providers (jazz/wbstream/telemost API calls) and outbound dials
	// route through the same proxy.
	if cfg.SOCKSProxyAddr != "" {
		protect.SetSocks5(protect.Socks5Config{
			Addr: net.JoinHostPort(cfg.SOCKSProxyAddr, strconv.Itoa(cfg.SOCKSProxyPort)),
			User: cfg.SOCKSProxyUser,
			Pass: cfg.SOCKSProxyPass,
		})
	} else {
		protect.SetSocks5(protect.Socks5Config{})
	}

	hook := cfg.AuthHook
	if hook == nil {
		hook = defaultAuthHook
	}
	onOpen := cfg.OnSessionOpen
	if onOpen == nil {
		onOpen = func(string, string, map[string]any) {}
	}
	onClose := cfg.OnSessionClose
	if onClose == nil {
		onClose = func(string, string) {}
	}
	onTraffic := cfg.OnTraffic
	if onTraffic == nil {
		onTraffic = func(string, string, uint64, uint64) {}
	}

	s := &Server{
		cipher:         cipher,
		authHook:       hook,
		onOpen:         onOpen,
		onClose:        onClose,
		onTraffic:      onTraffic,
		dnsServer:      cfg.DNSServer,
		socksProxyAddr: cfg.SOCKSProxyAddr,
		socksProxyPort: cfg.SOCKSProxyPort,
		socksProxyUser: cfg.SOCKSProxyUser,
		socksProxyPass: cfg.SOCKSProxyPass,
		warpProxyAddr:  cfg.WarpProxyAddr,
		warpProxyPort:  cfg.WarpProxyPort,
	}
	s.setupResolver()

	if err := s.bringUpLink(runCtx, cfg, cancel); err != nil {
		return err
	}

	go func() {
		<-runCtx.Done()
		s.closeSession()
	}()

	s.serve(runCtx)

	s.shutdown()
	s.wg.Wait()

	return nil
}

func setupCipher(keyHex string) (*crypto.Cipher, error) {
	if keyHex == "" {
		return nil, ErrKeyRequired
	}

	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%w, got %d", ErrKeySize, len(key))
	}

	cipher, err := crypto.NewCipher(string(key))
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	return cipher, nil
}

func (s *Server) setupResolver() {
	s.resolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, network, s.dnsServer)
		},
	}
}

// smuxConfig mirrors the client side. Both peers must agree on Version and
// MaxFrameSize.
func smuxConfig() *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.Version = 2
	cfg.KeepAliveDisabled = true
	cfg.MaxFrameSize = 32768
	cfg.MaxReceiveBuffer = 16 * 1024 * 1024
	cfg.MaxStreamBuffer = 1024 * 1024
	cfg.KeepAliveInterval = 10 * time.Second
	cfg.KeepAliveTimeout = 60 * time.Second
	return cfg
}

func (s *Server) bringUpLink(
	ctx context.Context,
	cfg Config,
	cancel context.CancelFunc,
) error {
	ln, err := link.New(ctx, cfg.Link, link.Config{
		Transport:       cfg.Transport,
		Carrier:         cfg.Carrier,
		RoomURL:         cfg.RoomURL,
		Engine:          cfg.Engine,
		URL:             cfg.URL,
		Token:           cfg.Token,
		DeviceID:        "",
		Name:            names.Generate(),
		OnData:          s.onData,
		DNSServer:       s.dnsServer,
		ProxyAddr:       s.socksProxyAddr,
		ProxyPort:       s.socksProxyPort,
		VideoWidth:      cfg.VideoWidth,
		VideoHeight:     cfg.VideoHeight,
		VideoFPS:        cfg.VideoFPS,
		VideoBitrate:    cfg.VideoBitrate,
		VideoHW:         cfg.VideoHW,
		VideoQRSize:     cfg.VideoQRSize,
		VideoQRRecovery: cfg.VideoQRRecovery,
		VideoCodec:      cfg.VideoCodec,
		VideoTileModule: cfg.VideoTileModule,
		VideoTileRS:     cfg.VideoTileRS,
		VP8FPS:          cfg.VP8FPS,
		VP8BatchSize:    cfg.VP8BatchSize,
		SEIFPS:          cfg.SEIFPS,
		SEIBatchSize:    cfg.SEIBatchSize,
		SEIFragmentSize: cfg.SEIFragmentSize,
		SEIAckTimeoutMS: cfg.SEIAckTimeoutMS,
	})
	if err != nil {
		return fmt.Errorf("failed to create link: %w", err)
	}
	s.ln = ln

	ln.SetEndedCallback(func(reason string) {
		logger.Infof("Server link reported conference end: %s", reason)
		cancel()
	})
	ln.SetShouldReconnect(func() bool { return ctx.Err() == nil })
	ln.SetReconnectCallback(func() {
		if ctx.Err() != nil {
			return
		}
		s.handleReconnect()
	})

	logger.Infof("Connecting link via %s/%s/%s...", cfg.Link, cfg.Transport, cfg.Carrier)
	if err := connectWithRetry(ctx, ln, cfg); err != nil {
		return fmt.Errorf("failed to connect link: %w", err)
	}
	logger.Infof("Link connected")

	s.installSession()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ln.WatchConnection(ctx)
	}()
	return nil
}

// connectWithRetry attempts to connect the link with exponential backoff.
// It retries up to 10 times with delays from 5s to 60s. If the context is
// cancelled, it returns immediately.
func connectWithRetry(ctx context.Context, ln link.Link, cfg Config) error {
	const maxAttempts = 10
	delay := 5 * time.Second
	const maxDelay = 60 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		lastErr = ln.Connect(ctx)
		if lastErr == nil {
			return nil
		}

		if attempt == maxAttempts {
			break
		}

		logger.Warnf("connect attempt %d/%d failed: %v; retrying in %v...",
			attempt, maxAttempts, lastErr, delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		// Exponential backoff: double the delay, cap at maxDelay.
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}

	return fmt.Errorf("all %d connect attempts failed, last error: %w", maxAttempts, lastErr)
}

func (s *Server) installSession() {
	conn := muxconn.New(s.ln, s.cipher)
	sess, err := smux.Server(conn, smuxConfig())
	if err != nil {
		logger.Warnf("smux server init failed: %v", err)
		return
	}
	s.sessMu.Lock()
	s.conn = conn
	s.session = sess
	s.sessMu.Unlock()
}

func (s *Server) handleReconnect() {
	logger.Infof("server link reconnect - tearing down smux session")
	s.sessMu.RLock()
	current := s.session
	s.sessMu.RUnlock()
	s.reinstallSession(current)
}

func (s *Server) reinstallSession(dead *smux.Session) {
	s.reinstallMu.Lock()
	defer s.reinstallMu.Unlock()

	// Pre-build the replacement so we can swap atomically below.
	newConn := muxconn.New(s.ln, s.cipher)
	newSess, err := smux.Server(newConn, smuxConfig())
	if err != nil {
		logger.Warnf("smux server init failed: %v", err)
		_ = newConn.Close()
		return
	}

	s.sessMu.Lock()
	if s.session != dead {
		// Someone else already reinstalled — discard our build.
		s.sessMu.Unlock()
		_ = newSess.Close()
		_ = newConn.Close()
		return
	}
	oldSess := s.session
	oldConn := s.conn
	oldSID := s.sessionID
	s.session = newSess
	s.conn = newConn
	s.sessionID = ""
	s.deviceID = ""
	s.sessMu.Unlock()

	if oldSess != nil {
		_ = oldSess.Close()
	}
	if oldConn != nil {
		_ = oldConn.Close()
	}
	if oldSID != "" {
		s.onClose(oldSID, "reconnect")
	}
}

func (s *Server) closeSession() {
	s.sessMu.Lock()
	sess := s.session
	conn := s.conn
	s.session = nil
	s.conn = nil
	oldSID := s.sessionID
	s.sessionID = ""
	s.deviceID = ""
	s.sessMu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	if sess != nil {
		_ = sess.Close()
	}
	if oldSID != "" {
		s.onClose(oldSID, "closed")
	}
}

func (s *Server) onData(data []byte) {
	s.sessMu.RLock()
	conn := s.conn
	s.sessMu.RUnlock()
	if conn != nil {
		conn.Push(data)
	}
}

// serve drives the smux Accept loop. The first accepted stream on a given
// smux session is the control stream — the handshake runs there. Subsequent
// streams are tunnel streams and proxy traffic.
func (s *Server) serve(ctx context.Context) {
	for {
		if contextDone(ctx) {
			return
		}

		s.sessMu.RLock()
		sess := s.session
		s.sessMu.RUnlock()
		if sess == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		if !s.handshakeReady() {
			if !s.acceptHandshake(ctx, sess) {
				continue
			}
		}

		stream, err := sess.AcceptStream()
		if err != nil {
			if contextDone(ctx) {
				return
			}
			logger.Debugf("AcceptStream returned %v - reinstalling session", err)
			s.reinstallSession(sess)
			continue
		}

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleStream(ctx, stream)
		}()
	}
}

func contextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// handshakeReady reports whether the current session has completed its
// handshake. The session is reset on reconnect, so this is recomputed.
func (s *Server) handshakeReady() bool {
	s.sessMu.RLock()
	defer s.sessMu.RUnlock()
	return s.sessionID != ""
}

func (s *Server) acceptHandshake(ctx context.Context, sess *smux.Session) bool {
	stream, err := sess.AcceptStream()
	if err != nil {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		logger.Debugf("AcceptStream(control) returned %v - reinstalling session", err)
		s.reinstallSession(sess)
		return false
	}
	_ = stream.SetDeadline(time.Now().Add(handshake.DefaultTimeout))
	hello, sid, err := handshake.Server(stream, s.authHook)
	_ = stream.SetDeadline(time.Time{})
	if err != nil {
		logger.Warnf("handshake failed: %v", err)
		_ = stream.Close()
		s.reinstallSession(sess)
		return false
	}
	s.sessMu.Lock()
	s.deviceID = hello.DeviceID
	s.sessionID = sid
	s.sessMu.Unlock()
	s.onOpen(sid, hello.DeviceID, hello.Claims)
	logger.Infof("session %s opened (device=%s)", sid, hello.DeviceID)
	// The control stream stays open for the lifetime of the session;
	// keep it parked in a goroutine so the smux session does not close it.
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.parkControlStream(stream)
	}()
	return true
}

// parkControlStream blocks reading from the control stream until it closes.
// Future control messages (kick, rate updates, etc.) would be dispatched here.
func (s *Server) parkControlStream(stream *smux.Stream) {
	defer func() { _ = stream.Close() }()
	buf := make([]byte, 64)
	for {
		if _, err := stream.Read(buf); err != nil {
			return
		}
	}
}

func (s *Server) shutdown() {
	s.closeSession()
	if s.ln != nil {
		_ = s.ln.Close()
	}
}

func (s *Server) handleStream(_ context.Context, stream *smux.Stream) {
	defer func() { _ = stream.Close() }()

	// Read the connect JSON. The client writes the whole JSON in one
	// stream.Write so it usually arrives intact; tolerate fragmentation
	// by reading incrementally up to a sane cap.
	const maxConnReq = 4096
	header := make([]byte, 0, 256)
	tmp := make([]byte, 256)
	_ = stream.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		n, err := stream.Read(tmp)
		if n > 0 {
			header = append(header, tmp[:n]...)
			if req, ok := parseConnectRequest(header); ok {
				_ = stream.SetReadDeadline(time.Time{})
				s.dispatch(stream, req)
				return
			}
		}
		if err != nil {
			return
		}
		if len(header) > maxConnReq {
			return
		}
	}
}

func parseConnectRequest(buf []byte) (ConnectRequest, bool) {
	var req ConnectRequest
	if err := json.Unmarshal(buf, &req); err != nil {
		return req, false
	}
	if req.Cmd != connectCommand {
		return req, false
	}
	return req, true
}

// defaultAuthHook admits every client and assigns a random session ID.
// Replace it via [Config.AuthHook] to plug in real authorization.
func defaultAuthHook(_ string, _ map[string]any) (string, error) {
	return uuid.NewString(), nil
}

func (s *Server) dispatch(stream *smux.Stream, req ConnectRequest) {
	addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))
	logger.Infof("sid=%d connect %s", stream.ID(), addr)

	s.sessMu.RLock()
	sid := s.sessionID
	s.sessMu.RUnlock()

	dialStart := time.Now()
	conn, err := s.dial(req)
	dialElapsed := time.Since(dialStart)

	if err != nil {
		logger.Infof("sid=%d dial %s failed (%v): %v", stream.ID(), addr, dialElapsed, err)
		return
	}
	defer func() { _ = conn.Close() }()

	logger.Infof("sid=%d connected %s in %v", stream.ID(), addr, dialElapsed)

	if _, err := stream.Write([]byte{0x00}); err != nil {
		return
	}

	var bytesOut uint64
	done := make(chan struct{})
	go func() {
		n, _ := io.Copy(stream, conn)
		if n > 0 {
			bytesOut = uint64(n)
		}
		_ = stream.Close()
		close(done)
	}()
	in, _ := io.Copy(conn, stream)
	_ = conn.Close()
	<-done
	bytesIn := uint64(0)
	if in > 0 {
		bytesIn = uint64(in)
	}
	if s.onTraffic != nil {
		s.onTraffic(sid, addr, bytesIn, bytesOut)
	}
}

// dial opens a TCP connection to req.Addr:req.Port for client tunnel
// traffic. If a WARP proxy is configured, all client traffic is routed
// through it so the remote endpoint sees a Cloudflare WARP IP instead of
// the VPS IP. The carrier SOCKS5 proxy (used for signalling) is never
// used here.
func (s *Server) dial(req ConnectRequest) (net.Conn, error) {
	addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))

	// If WARP proxy is configured, route client traffic through it.
	if s.warpProxyAddr != "" {
		proxyAddr := net.JoinHostPort(s.warpProxyAddr, strconv.Itoa(s.warpProxyPort))
		dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, &net.Dialer{
			Timeout:  10 * time.Second,
			Resolver: s.resolver,
		})
		if err != nil {
			return nil, fmt.Errorf("warp proxy setup failed: %w", err)
		}
		conn, err := dialer.Dial("tcp4", addr)
		if err != nil {
			return nil, fmt.Errorf("dial via warp failed: %w", err)
		}
		return conn, nil
	}

	if s.socksProxyAddr == "" {
		dialer := &net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
			Resolver:  s.resolver,
		}
		conn, err := dialer.Dial("tcp4", addr)
		if err != nil {
			return nil, fmt.Errorf("dial failed: %w", err)
		}
		return conn, nil
	}

	proxyAddr := net.JoinHostPort(s.socksProxyAddr, strconv.Itoa(s.socksProxyPort))
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	conn, err := dialer.Dial("tcp4", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to dial proxy: %w", err)
	}

	if err := s.socks5Connect(conn, req.Addr, req.Port); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}

func (s *Server) socks5Connect(conn net.Conn, targetAddr string, targetPort int) error {
	if _, err := conn.Write([]byte{5, 1, 0}); err != nil {
		return fmt.Errorf("failed to write socks5 auth: %w", err)
	}

	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("failed to read socks5 auth resp: %w", err)
	}
	if resp[0] != 5 || resp[1] != 0 {
		return ErrSocks5AuthFailed
	}

	addrLen := len(targetAddr)
	if addrLen > 255 {
		addrLen = 255
		targetAddr = targetAddr[:255]
	}

	req := make([]byte, 0, 7+addrLen)
	req = append(req, 5, 1, 0, 3, byte(addrLen))
	req = append(req, []byte(targetAddr)...)
	req = append(req, byte(targetPort>>8), byte(targetPort)) //nolint:gosec,lll // G115: bounded conversion verified by surrounding logic

	if _, err := conn.Write(req); err != nil {
		return fmt.Errorf("failed to write socks5 connect req: %w", err)
	}

	resp = make([]byte, 10)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return fmt.Errorf("failed to read socks5 connect resp: %w", err)
	}
	if resp[0] != 5 || resp[1] != 0 {
		return fmt.Errorf("%w: %d", ErrSocks5ConnectFailed, resp[1])
	}

	return nil
}
