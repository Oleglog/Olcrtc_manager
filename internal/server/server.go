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

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/link"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/muxconn"
	"github.com/openlibrecommunity/olcrtc/internal/names"
	"github.com/openlibrecommunity/olcrtc/internal/protect"
	"github.com/xtaci/smux"
	"golang.org/x/net/proxy"
)

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

// Server handles incoming tunnel connections and proxies their traffic.
type Server struct {
	links          []link.Link
	cipher         *crypto.Cipher
	conn           *muxconn.Bonder
	session        *smux.Session
	sessMu         sync.RWMutex
	reinstallMu    sync.Mutex
	wg             sync.WaitGroup
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

// Run starts the server with the specified parameters.
//
//nolint:funlen // long parameter list mirrors the historical CLI surface
func Run(
	ctx context.Context,
	linkName,
	transportName,
	carrierName,
	roomURL,
	keyHex string,
	dnsServer,
	socksProxyAddr string,
	socksProxyPort int,
	socksProxyUser, socksProxyPass string,
	warpProxyAddr string,
	warpProxyPort int,
	videoWidth int,
	videoHeight int,
	videoFPS int,
	videoBitrate string,
	videoHW string,
	videoQRSize int,
	videoQRRecovery string,
	videoCodec string,
	videoTileModule int,
	videoTileRS int,
	vp8FPS int,
	vp8BatchSize int,
	peers int,
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cipher, err := setupCipher(keyHex)
	if err != nil {
		return fmt.Errorf("setupCipher failed: %w", err)
	}

	// Install the SOCKS5 config globally so all HTTP clients used by
	// providers (jazz/wb_stream/telemost API calls) and outbound dials
	// route through the same proxy.
	if socksProxyAddr != "" {
		protect.SetSocks5(protect.Socks5Config{
			Addr: net.JoinHostPort(socksProxyAddr, strconv.Itoa(socksProxyPort)),
			User: socksProxyUser,
			Pass: socksProxyPass,
		})
	} else {
		protect.SetSocks5(protect.Socks5Config{})
	}

	s := &Server{
		cipher:         cipher,
		dnsServer:      dnsServer,
		socksProxyAddr: socksProxyAddr,
		socksProxyPort: socksProxyPort,
		socksProxyUser: socksProxyUser,
		socksProxyPass: socksProxyPass,
		warpProxyAddr:  warpProxyAddr,
		warpProxyPort:  warpProxyPort,
	}
	s.setupResolver()

	if err := s.bringUpLink(
		runCtx, linkName, transportName, carrierName, roomURL, cancel,
		videoWidth, videoHeight, videoFPS, videoBitrate, videoHW,
		videoQRSize, videoQRRecovery, videoCodec, videoTileModule, videoTileRS,
		vp8FPS, vp8BatchSize, peers,
	); err != nil {
		return err
	}

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
	cfg.MaxFrameSize = 32768
	cfg.MaxReceiveBuffer = 16 * 1024 * 1024
	cfg.MaxStreamBuffer = 1024 * 1024
	cfg.KeepAliveInterval = 10 * time.Second
	cfg.KeepAliveTimeout = 60 * time.Second
	return cfg
}

// peerTagFor returns the LiveKit topic / wbstream peer tag for one peer
// of an N-way bond. Empty when peers <= 1 to preserve the historical
// "olcrtc" topic and skip the receive-side filter.
func peerTagFor(peers, idx int) string {
	if peers <= 1 {
		return ""
	}
	return fmt.Sprintf("olcrtc-%d", idx)
}

//nolint:funlen // bringUpLink coordinates N-way link creation, callbacks and watchers
func (s *Server) bringUpLink(
	ctx context.Context,
	linkName, transportName, carrierName, roomURL string,
	cancel context.CancelFunc,
	videoWidth, videoHeight, videoFPS int,
	videoBitrate, videoHW string,
	videoQRSize int,
	videoQRRecovery string,
	videoCodec string,
	videoTileModule, videoTileRS int,
	vp8FPS, vp8BatchSize int,
	peers int,
) error {
	if peers < 1 {
		peers = 1
	}
	links := make([]link.Link, 0, peers)
	for i := range peers {
		idx := i
		ln, err := link.New(ctx, linkName, link.Config{
			Transport:       transportName,
			Carrier:         carrierName,
			RoomURL:         roomURL,
			Name:            names.Generate(),
			OnData:          func(data []byte) { s.pushFromPeer(idx, data) },
			DNSServer:       s.dnsServer,
			ProxyAddr:       s.socksProxyAddr,
			ProxyPort:       s.socksProxyPort,
			VideoWidth:      videoWidth,
			VideoHeight:     videoHeight,
			VideoFPS:        videoFPS,
			VideoBitrate:    videoBitrate,
			VideoHW:         videoHW,
			VideoQRSize:     videoQRSize,
			VideoQRRecovery: videoQRRecovery,
			VideoCodec:      videoCodec,
			VideoTileModule: videoTileModule,
			VideoTileRS:     videoTileRS,
			VP8FPS:          vp8FPS,
			VP8BatchSize:    vp8BatchSize,
			PeerTag:         peerTagFor(peers, idx),
		})
		if err != nil {
			for _, prev := range links {
				_ = prev.Close()
			}
			return fmt.Errorf("failed to create link peer %d: %w", idx, err)
		}
		links = append(links, ln)
	}
	s.links = links

	for i, ln := range links {
		idx := i
		ln.SetEndedCallback(func(reason string) {
			logger.Infof("Server peer %d reported conference end: %s", idx, reason)
			cancel()
		})
		ln.SetReconnectCallback(func() { s.handleReconnect() })
	}

	logger.Infof("Connecting %d link peer(s) via %s/%s/%s...", peers, linkName, transportName, carrierName)
	for i, ln := range links {
		if err := ln.Connect(ctx); err != nil {
			return fmt.Errorf("failed to connect link peer %d: %w", i, err)
		}
	}
	logger.Infof("Link connected (%d peer(s))", peers)

	if err := s.installSession(); err != nil {
		return err
	}

	for _, ln := range links {
		watch := ln
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			watch.WatchConnection(ctx)
		}()
	}
	return nil
}

func (s *Server) installSession() error {
	conn, err := muxconn.NewBonder(s.links, s.cipher)
	if err != nil {
		return fmt.Errorf("create bonder: %w", err)
	}
	sess, err := smux.Server(conn, smuxConfig())
	if err != nil {
		logger.Warnf("smux server init failed: %v", err)
		return fmt.Errorf("smux server init: %w", err)
	}
	s.sessMu.Lock()
	s.conn = conn
	s.session = sess
	s.sessMu.Unlock()
	return nil
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

	s.sessMu.Lock()
	if s.session != dead {
		s.sessMu.Unlock()
		return
	}
	if s.session != nil {
		_ = s.session.Close()
		s.session = nil
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	s.sessMu.Unlock()
	if err := s.installSession(); err != nil {
		logger.Warnf("server reinstall session failed: %v", err)
	}
}

// pushFromPeer is the OnData hook installed on every link peer; it
// hands the encrypted wire payload to the bonder which decrypts and
// merges it into the smux read buffer.
func (s *Server) pushFromPeer(idx int, data []byte) {
	s.sessMu.RLock()
	conn := s.conn
	s.sessMu.RUnlock()
	if conn != nil {
		conn.Push(idx, data)
	}
}

// serve drives the smux Accept loop, spawning a tunnel per inbound stream.
// The loop tolerates session bounces (reconnects) by waiting until a fresh
// session is installed instead of terminating the server.
func (s *Server) serve(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
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

		stream, err := sess.AcceptStream()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			logger.Infof("AcceptStream returned %v - reinstalling session", err)
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

func (s *Server) shutdown() {
	s.sessMu.Lock()
	if s.session != nil {
		_ = s.session.Close()
	}
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.sessMu.Unlock()
	for _, ln := range s.links {
		_ = ln.Close()
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
	if req.Cmd != "connect" {
		return req, false
	}
	return req, true
}

func (s *Server) dispatch(stream *smux.Stream, req ConnectRequest) {
	addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))
	logger.Infof("sid=%d connect %s", stream.ID(), addr)

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

	go func() {
		_, _ = io.Copy(stream, conn)
		_ = stream.Close()
	}()
	_, _ = io.Copy(conn, stream)
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

	// Without WARP — direct connection.
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
	req = append(req, byte(targetPort>>8), byte(targetPort)) //nolint:gosec

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
