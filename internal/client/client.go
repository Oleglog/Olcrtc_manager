// Package client implements the local SOCKS5 client side of the olcrtc tunnel.
package client

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/link"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/muxconn"
	"github.com/openlibrecommunity/olcrtc/internal/names"
	"github.com/xtaci/smux"
)

var (
	// ErrConnectFailed is returned when a tunnel connection fails.
	ErrConnectFailed = errors.New("tunnel connection failed")
	// ErrProxyAuth is returned when SOCKS proxy authentication fails.
	ErrProxyAuth = errors.New("SOCKS proxy auth failed")
	// ErrKeySize is returned when the encryption key is not 32 bytes.
	ErrKeySize = errors.New("key must be 32 bytes")
	// ErrInvalidSOCKSVersion is returned when the SOCKS version is not 5.
	ErrInvalidSOCKSVersion = errors.New("invalid socks version")
	// ErrUnsupportedSOCKSCommand is returned for unsupported SOCKS commands.
	ErrUnsupportedSOCKSCommand = errors.New("unsupported socks command")
	// ErrUnsupportedAddressType is returned for unsupported SOCKS address types.
	ErrUnsupportedAddressType = errors.New("unsupported address type")
	// ErrRemoteNotReady is returned when the server-side stream fails to signal readiness.
	ErrRemoteNotReady = errors.New("remote not ready")
)

// Client handles local SOCKS5 connections and tunnels them to the server.
type Client struct {
	ln        link.Link
	cipher    *crypto.Cipher
	conn      *muxconn.Conn
	session   *smux.Session
	sessMu    sync.RWMutex
	dnsServer string
}

// Run starts the client with the specified parameters.
func Run(
	ctx context.Context,
	linkName,
	transportName,
	carrierName,
	roomURL,
	keyHex string,
	localAddr string,
	dnsServer,
	socksUser string,
	socksPass string,
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
) error {
	return RunWithReady(
		ctx, linkName, transportName, carrierName, roomURL, keyHex, localAddr,
		dnsServer, socksUser, socksPass, nil,
		videoWidth, videoHeight, videoFPS, videoBitrate, videoHW,
		videoQRSize, videoQRRecovery, videoCodec, videoTileModule, videoTileRS,
		vp8FPS, vp8BatchSize,
	)
}

// RunWithReady is like Run but accepts a callback that is called when the client is ready.
func RunWithReady(
	ctx context.Context,
	linkName,
	transportName,
	carrierName,
	roomURL,
	keyHex string,
	localAddr string,
	dnsServer,
	_ string,
	_ string,
	onReady func(),
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
) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cipher, err := setupCipher(keyHex)
	if err != nil {
		return fmt.Errorf("setupCipher failed: %w", err)
	}

	c := &Client{cipher: cipher, dnsServer: dnsServer}

	if err := c.bringUpLink(
		runCtx, linkName, transportName, carrierName, roomURL, cancel,
		dnsServer, "", 0,
		videoWidth, videoHeight, videoFPS, videoBitrate, videoHW,
		videoQRSize, videoQRRecovery, videoCodec, videoTileModule, videoTileRS,
		vp8FPS, vp8BatchSize,
	); err != nil {
		return err
	}
	defer c.shutdown()

	lc := net.ListenConfig{}
	listener, err := lc.Listen(runCtx, "tcp4", localAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", localAddr, err)
	}
	defer func() { _ = listener.Close() }()

	logger.Infof("SOCKS5 server listening on %s", localAddr)

	if onReady != nil {
		onReady()
	}

	go c.acceptLoop(runCtx, listener)

	<-runCtx.Done()
	return nil
}

func (c *Client) bringUpLink(
	ctx context.Context,
	linkName, transportName, carrierName, roomURL string,
	cancel context.CancelFunc,
	dnsServer, socksProxyAddr string,
	socksProxyPort int,
	videoWidth, videoHeight, videoFPS int,
	videoBitrate, videoHW string,
	videoQRSize int,
	videoQRRecovery string,
	videoCodec string,
	videoTileModule, videoTileRS int,
	vp8FPS, vp8BatchSize int,
) error {
	ln, err := link.New(ctx, linkName, link.Config{
		Transport:       transportName,
		Carrier:         carrierName,
		RoomURL:         roomURL,
		Name:            names.Generate(),
		OnData:          c.onData,
		DNSServer:       dnsServer,
		ProxyAddr:       socksProxyAddr,
		ProxyPort:       socksProxyPort,
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
	})
	if err != nil {
		return fmt.Errorf("failed to create link: %w", err)
	}
	c.ln = ln

	ln.SetEndedCallback(func(reason string) {
		logger.Infof("Client link reported conference end: %s", reason)
		cancel()
	})
	ln.SetReconnectCallback(func() { c.handleReconnect() })

	if err := ln.Connect(ctx); err != nil {
		return fmt.Errorf("failed to connect link: %w", err)
	}

	c.conn = muxconn.New(ln, c.cipher)
	sess, err := smux.Client(c.conn, smuxConfig())
	if err != nil {
		return fmt.Errorf("smux client: %w", err)
	}
	c.sessMu.Lock()
	c.session = sess
	c.sessMu.Unlock()

	go ln.WatchConnection(ctx)
	return nil
}

// smuxConfig returns the tuned smux config used on both ends.
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

func (c *Client) handleReconnect() {
	logger.Infof("client link reconnect - tearing down smux session")
	c.sessMu.Lock()
	if c.session != nil {
		_ = c.session.Close()
		c.session = nil
	}
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
	c.sessMu.Unlock()
	c.conn = muxconn.New(c.ln, c.cipher)
	sess, err := smux.Client(c.conn, smuxConfig())
	if err != nil {
		logger.Warnf("smux re-init failed: %v", err)
		return
	}
	c.sessMu.Lock()
	c.session = sess
	c.sessMu.Unlock()
}

func (c *Client) shutdown() {
	c.sessMu.Lock()
	if c.session != nil {
		_ = c.session.Close()
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.sessMu.Unlock()
	if c.ln != nil {
		_ = c.ln.Close()
	}
}

func setupCipher(keyHex string) (*crypto.Cipher, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to decode key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%w: got %d", ErrKeySize, len(key))
	}

	cipher, err := crypto.NewCipher(string(key))
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	return cipher, nil
}

func (c *Client) onData(data []byte) {
	c.sessMu.RLock()
	conn := c.conn
	c.sessMu.RUnlock()
	if conn != nil {
		conn.Push(data)
	}
}

func (c *Client) acceptLoop(ctx context.Context, ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				logger.Warnf("Accept error: %v", err)
				continue
			}
		}
		go c.handleSocks5(ctx, conn)
	}
}

func (c *Client) handleSocks5(_ context.Context, conn net.Conn) {
	defer func() { _ = conn.Close() }()

	if err := c.socks5Handshake(conn); err != nil {
		return
	}

	targetAddr, targetPort, err := c.socks5Request(conn)
	if err != nil {
		return
	}

	c.sessMu.RLock()
	sess := c.session
	c.sessMu.RUnlock()
	if sess == nil || sess.IsClosed() {
		_, _ = conn.Write(replyHostUnreachable())
		return
	}

	c.tunnel(conn, sess, targetAddr, targetPort)
}

func (c *Client) tunnel(conn net.Conn, sess *smux.Session, targetAddr string, targetPort int) {
	stream, err := sess.OpenStream()
	if err != nil {
		logger.Warnf("OpenStream failed: %v", err)
		_, _ = conn.Write(replyHostUnreachable())
		return
	}
	defer func() { _ = stream.Close() }()

	logger.Infof("sid=%d tunnel to %s:%d", stream.ID(), targetAddr, targetPort)

	if err := c.sendConnectRequest(stream, targetAddr, targetPort); err != nil {
		logger.Warnf("sid=%d connect failed: %v", stream.ID(), err)
		_, _ = conn.Write(replyHostUnreachable())
		return
	}

	if _, err := conn.Write(replySuccess()); err != nil {
		return
	}

	go func() {
		_, _ = io.Copy(stream, conn)
		_ = stream.Close()
	}()
	_, _ = io.Copy(conn, stream)
}

func (c *Client) sendConnectRequest(stream *smux.Stream, targetAddr string, targetPort int) error {
	connectReq, err := json.Marshal(map[string]any{
		"cmd":  "connect",
		"addr": targetAddr,
		"port": targetPort,
	})
	if err != nil {
		return fmt.Errorf("sid=%d marshal connect req: %w", stream.ID(), err)
	}

	_ = stream.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := stream.Write(connectReq); err != nil {
		return fmt.Errorf("sid=%d write connect req: %w", stream.ID(), err)
	}
	_ = stream.SetWriteDeadline(time.Time{})

	ack := make([]byte, 1)
	_ = stream.SetReadDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.ReadFull(stream, ack); err != nil || ack[0] != 0x00 {
		return fmt.Errorf("sid=%d: %w (read_err=%w ack=%v)", stream.ID(), ErrRemoteNotReady, err, ack)
	}
	_ = stream.SetReadDeadline(time.Time{})
	return nil
}

func (c *Client) socks5Handshake(conn net.Conn) error {
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("read socks5 header: %w", err)
	}
	if buf[0] != 5 {
		return fmt.Errorf("%w: %d", ErrInvalidSOCKSVersion, buf[0])
	}
	methods := make([]byte, buf[1])
	if _, err := io.ReadFull(conn, methods); err != nil {
		return fmt.Errorf("read socks5 methods: %w", err)
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return fmt.Errorf("write socks5 auth: %w", err)
	}
	return nil
}

func (c *Client) socks5Request(conn net.Conn) (string, int, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", 0, fmt.Errorf("read socks5 request: %w", err)
	}
	if header[1] != 1 {
		return "", 0, fmt.Errorf("%w: %d", ErrUnsupportedSOCKSCommand, header[1])
	}

	addr, err := c.readSocks5Addr(conn, header[3])
	if err != nil {
		return "", 0, err
	}

	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(conn, portBuf); err != nil {
		return "", 0, fmt.Errorf("read socks5 port: %w", err)
	}
	port := int(binary.BigEndian.Uint16(portBuf))

	return addr, port, nil
}

func (c *Client) readSocks5Addr(conn net.Conn, addrType byte) (string, error) {
	switch addrType {
	case 1: // IPv4
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", fmt.Errorf("read socks5 ipv4: %w", err)
		}
		return net.IP(buf).String(), nil
	case 3: // Domain
		lenBuf := make([]byte, 1)
		if _, err := io.ReadFull(conn, lenBuf); err != nil {
			return "", fmt.Errorf("read socks5 domain len: %w", err)
		}
		buf := make([]byte, lenBuf[0])
		if _, err := io.ReadFull(conn, buf); err != nil {
			return "", fmt.Errorf("read socks5 domain: %w", err)
		}
		return string(buf), nil
	default:
		return "", fmt.Errorf("%w: %d", ErrUnsupportedAddressType, addrType)
	}
}

func replySuccess() []byte {
	return []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
}

func replyHostUnreachable() []byte {
	return []byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0}
}
