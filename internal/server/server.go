// Package server implements the olcrtc tunnel server logic.
package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/mux"
	"github.com/openlibrecommunity/olcrtc/internal/names"
	"github.com/openlibrecommunity/olcrtc/internal/protect"
	"github.com/openlibrecommunity/olcrtc/internal/provider"
	"github.com/pion/webrtc/v4"
)

var (
	// ErrKeySize is returned when the encryption key is not 32 bytes.
	ErrKeySize = errors.New("key must be 32 bytes")
	// ErrKeyStringLength is returned when the encryption key string length is not 32.
	ErrKeyStringLength = errors.New("key string length must be 32")
	// ErrSocks5AuthFailed is returned when SOCKS5 authentication fails.
	ErrSocks5AuthFailed = errors.New("SOCKS5 auth failed")
	// ErrSocks5ConnectFailed is returned when SOCKS5 connection fails.
	ErrSocks5ConnectFailed = errors.New("SOCKS5 connect failed")
	// ErrNoPeers is returned when no peers are available.
	ErrNoPeers = errors.New("no peers available")
	// ErrDialProxy is returned when dialing the proxy fails.
	ErrDialProxy = errors.New("failed to dial proxy")
	// ErrEncryptFailed is returned when encryption fails.
	ErrEncryptFailed = errors.New("encrypt failed")
)

// Server handles incoming WebRTC connections and proxies their traffic.
type Server struct {
	peers          []provider.Provider
	cipher         *crypto.Cipher
	mux            *mux.Multiplexer
	connections    map[uint16]net.Conn
	connMu         sync.RWMutex
	streamPumps    map[uint16]net.Conn
	pumpMu         sync.Mutex
	peerIdx        atomic.Uint32
	activeClients  atomic.Int32
	wg             sync.WaitGroup
	dnsServer      string
	resolver       *net.Resolver
	socksProxyAddr string
	socksProxyPort int
	socksProxyUser string
	socksProxyPass string
}

// ConnectRequest is a message from the client to establish a new connection.
type ConnectRequest struct {
	Cmd  string `json:"cmd"`
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

// Run starts the server with the specified parameters.
func Run(
	ctx context.Context,
	providerName,
	roomURL,
	keyHex string,
	dnsServer,
	socksProxyAddr string,
	socksProxyPort int,
	socksProxyUser, socksProxyPass string,
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
		connections:    make(map[uint16]net.Conn),
		streamPumps:    make(map[uint16]net.Conn),
		peers:          make([]provider.Provider, 0),
		dnsServer:      dnsServer,
		socksProxyAddr: socksProxyAddr,
		socksProxyPort: socksProxyPort,
		socksProxyUser: socksProxyUser,
		socksProxyPass: socksProxyPass,
	}

	if s.dnsServer == "" {
		s.dnsServer = "1.1.1.1:53"
	}

	s.setupResolver()
	s.setupMux()

	const peerCount = 1
	for i := range peerCount {
		if err := s.addPeer(runCtx, providerName, roomURL, i, cancel); err != nil {
			return fmt.Errorf("addPeer failed: %w", err)
		}
	}

	err = s.runLoop(runCtx)

	s.wg.Wait()

	return err
}

func setupCipher(keyHex string) (*crypto.Cipher, error) {
	var key []byte
	var err error

	if keyHex == "" {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, fmt.Errorf("failed to generate key: %w", err)
		}
		log.Printf("Generated key: %x", key)
	} else {
		key, err = hex.DecodeString(keyHex)
		if err != nil {
			return nil, fmt.Errorf("failed to decode key: %w", err)
		}
		if len(key) != 32 {
			return nil, fmt.Errorf("%w, got %d", ErrKeySize, len(key))
		}
	}

	keyStr := string(key)
	if len(keyStr) != 32 {
		return nil, fmt.Errorf("%w, got %d", ErrKeyStringLength, len(keyStr))
	}

	cipher, err := crypto.NewCipher(keyStr)
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

func (s *Server) setupMux() {
	s.mux = mux.New(0, func(frame []byte) error {
		for {
			canSend := true
			for _, peer := range s.peers {
				if !peer.CanSend() {
					canSend = false
					break
				}
			}
			if canSend {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}

		encrypted, err := s.cipher.Encrypt(frame)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrEncryptFailed, err)
		}
		if len(s.peers) == 0 {
			return ErrNoPeers
		}
		idx := s.peerIdx.Add(1) % uint32(len(s.peers)) //nolint:gosec
		return s.peers[idx].Send(encrypted)
	})
}

func (s *Server) addPeer(
	ctx context.Context,
	providerName,
	roomURL string,
	peerID int,
	cancel context.CancelFunc,
) error {
	peer, err := provider.New(ctx, providerName, provider.Config{
		RoomURL:   roomURL,
		Name:      names.Generate(),
		OnData:    s.onData,
		DNSServer: s.dnsServer,
		ProxyAddr: s.socksProxyAddr,
		ProxyPort: s.socksProxyPort,
	})
	if err != nil {
		return fmt.Errorf("failed to create peer: %w", err)
	}

	peer.SetEndedCallback(func(reason string) {
		logger.Infof("Server peer %d reported conference end: %s", peerID, reason)
		cancel()
	})
	s.peers = append(s.peers, peer)

	peer.SetReconnectCallback(func(dc *webrtc.DataChannel) {
		s.handlePeerReconnect(peerID, dc)
	})

	logger.Infof("Connecting peer %d to %s...", peerID, providerName)
	if err := peer.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect peer: %w", err)
	}
	logger.Infof("Peer %d connected", peerID)

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		peer.WatchConnection(ctx)
	}()
	return nil
}

func (s *Server) handlePeerReconnect(peerID int, dc *webrtc.DataChannel) {
	logger.Infof("peer %d reconnect event: dc=%v", peerID, dc != nil)

	s.connMu.Lock()
	for sid, conn := range s.connections {
		if conn != nil {
			_ = conn.Close()
		}
		delete(s.connections, sid)
	}
	s.connMu.Unlock()

	if dc != nil {
		s.mux.UpdateSendFunc(func(frame []byte) error {
			encrypted, err := s.cipher.Encrypt(frame)
			if err != nil {
				return fmt.Errorf("%w: %w", ErrEncryptFailed, err)
			}
			if len(s.peers) == 0 {
				return ErrNoPeers
			}
			idx := s.peerIdx.Add(1) % uint32(len(s.peers)) //nolint:gosec
			return s.peers[idx].Send(encrypted)
		})
		s.mux.Reset()
	}
}

func (s *Server) onData(data []byte) {
	plaintext, err := s.cipher.Decrypt(data)
	if err != nil {
		logger.Debugf("Decrypt error: %v", err)
		return
	}

	if control, ok := mux.ParseControlFrame(plaintext); ok && control.Type == mux.ControlResetClient {
		logger.Infof("Received reset signal from client (clientID=%d)", control.ClientID)
		s.closeClientConnections(control.ClientID)
	}

	s.mux.HandleFrame(plaintext)
}

func (s *Server) closeClientConnections(clientID uint32) {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	for streamSid, conn := range s.connections {
		stream := s.mux.GetStream(streamSid)
		if stream != nil && stream.ClientID == clientID {
			if conn != nil {
				_ = conn.Close()
			}
			delete(s.connections, streamSid)
		}
	}
}

func (s *Server) runLoop(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.shutdown()
			return nil
		case <-ticker.C:
			s.processMuxStreams(ctx)
		}
	}
}

func (s *Server) shutdown() {
	s.connMu.Lock()
	for _, conn := range s.connections {
		if conn != nil {
			_ = conn.Close()
		}
	}
	s.connMu.Unlock()

	for i, peer := range s.peers {
		logger.Infof("closing peer %d", i)
		_ = peer.Close()
	}
}

func (s *Server) processMuxStreams(ctx context.Context) {
	sids := s.mux.GetStreams()
	for _, sid := range sids {
		if s.mux.StreamClosed(sid) {
			s.closeStreamConnection(sid)
			continue
		}

		if s.hasConnection(sid) {
			continue
		}

		data := s.mux.ReadStream(sid)
		if len(data) == 0 {
			continue
		}

		var req ConnectRequest
		if err := json.Unmarshal(data, &req); err == nil && req.Cmd == "connect" {
			logger.Infof("sid=%d connect %s:%d", sid, req.Addr, req.Port)
			s.closeStreamConnection(sid)
			go s.handleConnect(ctx, sid, req)
		}
	}
}

func (s *Server) hasConnection(sid uint16) bool {
	s.connMu.RLock()
	defer s.connMu.RUnlock()
	return s.connections[sid] != nil
}

func (s *Server) closeStreamConnection(sid uint16) {
	s.connMu.Lock()
	conn := s.connections[sid]
	if conn != nil {
		_ = conn.Close()
		delete(s.connections, sid)
	}
	s.connMu.Unlock()
}

func (s *Server) closeStreamConnectionIfCurrent(sid uint16, expected net.Conn) {
	s.connMu.Lock()
	conn := s.connections[sid]
	if conn == expected {
		_ = conn.Close()
		delete(s.connections, sid)
	}
	s.connMu.Unlock()
}

func (s *Server) markStreamPump(sid uint16, conn net.Conn) bool {
	s.pumpMu.Lock()
	defer s.pumpMu.Unlock()
	if current := s.streamPumps[sid]; current == conn {
		return false
	} else if current != nil {
		_ = current.Close()
	}
	s.streamPumps[sid] = conn
	return true
}

func (s *Server) unmarkStreamPump(sid uint16, conn net.Conn) {
	s.pumpMu.Lock()
	if s.streamPumps[sid] == conn {
		delete(s.streamPumps, sid)
	}
	s.pumpMu.Unlock()
}

func (s *Server) handleConnect(ctx context.Context, sid uint16, req ConnectRequest) {
	addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))

	s.closeStreamConnection(sid)

	dialStart := time.Now()
	conn, err := s.dial(req)
	dialElapsed := time.Since(dialStart)

	if err != nil {
		logger.Infof("sid=%d dial %s failed (%v): %v", sid, addr, dialElapsed, err)
		_ = s.mux.CloseStream(sid)
		return
	}

	s.connMu.Lock()
	s.connections[sid] = conn
	s.connMu.Unlock()

	logger.Infof("sid=%d connected %s in %v", sid, addr, dialElapsed)

	s.activeClients.Add(1)
	_ = s.mux.SendData(sid, []byte{0x00})
	s.startStreamPump(ctx, sid, conn)

	go s.pumpToMux(sid, conn)
}

// dial opens a TCP connection to req.Addr:req.Port for client tunnel
// traffic. It always dials directly from the VPS (never via the
// configured SOCKS5 proxy). The proxy exists to make provider HTTP/WS
// signalling appear to come from a residential / RU IP; routing client
// traffic through it would force every endpoint (Telegram, etc.) to see
// the proxy's geolocation, breaking services that are geo-restricted
// against that region.
func (s *Server) dial(req ConnectRequest) (net.Conn, error) {
	addr := net.JoinHostPort(req.Addr, strconv.Itoa(req.Port))

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

func (s *Server) pumpToMux(sid uint16, conn net.Conn) {
	defer func() {
		s.activeClients.Add(-1)
		_ = s.mux.CloseStream(sid)
		s.connMu.Lock()
		delete(s.connections, sid)
		s.connMu.Unlock()
	}()

	buf := make([]byte, 16384)
	totalSent := uint64(0)
	lastLog := time.Now()

	for {
		n, err := conn.Read(buf)
		if err != nil {
			if totalSent > 1024*1024 {
				logger.Infof("sid=%d done total=%dMB", sid, totalSent/(1024*1024))
			}
			return
		}

		for !s.canSendData() {
			time.Sleep(20 * time.Millisecond)
		}

		if err := s.mux.SendData(sid, buf[:n]); err != nil {
			return
		}

		totalSent += uint64(n) //nolint:gosec
		if time.Since(lastLog) > 5*time.Second {
			logger.Infof("sid=%d sent=%dMB", sid, totalSent/(1024*1024))
			lastLog = time.Now()
		}
	}
}

func (s *Server) startStreamPump(ctx context.Context, sid uint16, conn net.Conn) {
	if !s.markStreamPump(sid, conn) {
		return
	}

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.unmarkStreamPump(sid, conn)

		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				data := s.mux.ReadStream(sid)
				if len(data) > 0 {
					if _, err := conn.Write(data); err != nil {
						_ = s.mux.CloseStream(sid)
						s.closeStreamConnectionIfCurrent(sid, conn)
						return
					}
				}
				if s.mux.StreamClosed(sid) {
					s.closeStreamConnectionIfCurrent(sid, conn)
					return
				}
			}
		}
	}()
}

func (s *Server) canSendData() bool {
	for _, peer := range s.peers {
		if !peer.CanSend() {
			return false
		}
	}
	return true
}
