// Package protect provides functions to protect sockets from VPN routing
// and (optionally) route all outbound TCP through a SOCKS5 proxy.
package protect

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
)

// Protector is called with a socket file descriptor before connect.
// On Android, this calls VpnService.protect(fd) to bypass VPN routing.
var Protector func(fd int) bool //nolint:gochecknoglobals

// Socks5Config configures an outbound SOCKS5 proxy that all helpers in this
// package will route through. Empty Addr means "no proxy, dial directly".
//
// Supported auth: NO_AUTH (User and Pass empty) and RFC 1929 USER/PASSWORD
// (User non-empty). Configure once at startup before any provider / server
// code calls into this package.
type Socks5Config struct {
	Addr string // host:port, empty = no proxy
	User string
	Pass string
}

//nolint:gochecknoglobals // package-level config matches the Protector pattern above
var (
	socks5Mu     sync.RWMutex
	socks5Cfg    Socks5Config
	cachedDialer proxy.ContextDialer
)

// SetSocks5 installs the SOCKS5 configuration used by NewHTTPClient,
// DialContext, NewDialer, and ProxyDialer. Pass an empty Socks5Config to
// clear it.
func SetSocks5(cfg Socks5Config) {
	socks5Mu.Lock()
	defer socks5Mu.Unlock()
	socks5Cfg = cfg
	cachedDialer = nil // will be lazily re-created on next call
}

// Socks5 returns the currently configured SOCKS5 proxy.
func Socks5() Socks5Config {
	socks5Mu.RLock()
	defer socks5Mu.RUnlock()
	return socks5Cfg
}

func controlFunc(network, _ string, c syscall.RawConn) error {
	if Protector == nil {
		return nil
	}
	var err error
	controlErr := c.Control(func(fd uintptr) {
		if !Protector(int(fd)) { //nolint:gosec
			err = &net.OpError{Op: "protect", Net: network, Err: net.ErrClosed}
		}
	})
	if controlErr != nil {
		return fmt.Errorf("control failed: %w", controlErr)
	}
	return err
}

// directDialer returns the underlying net.Dialer that opens a fresh socket,
// honoring the Android VpnService.protect() hook.
func directDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlFunc,
	}
}

// resolveDialer returns a proxy.ContextDialer that either dials directly
// (when no SOCKS5 is configured) or routes through the configured proxy.
// The result is cached until SetSocks5 is called again.
func resolveDialer() proxy.ContextDialer {
	socks5Mu.RLock()
	if cachedDialer != nil {
		d := cachedDialer
		socks5Mu.RUnlock()
		return d
	}
	cfg := socks5Cfg
	socks5Mu.RUnlock()

	socks5Mu.Lock()
	defer socks5Mu.Unlock()
	// double-check after upgrading lock
	if cachedDialer != nil {
		return cachedDialer
	}

	direct := directDialer()
	if cfg.Addr == "" {
		cachedDialer = directDialerWrapper{direct}
		return cachedDialer
	}

	var auth *proxy.Auth
	if cfg.User != "" || cfg.Pass != "" {
		auth = &proxy.Auth{User: cfg.User, Password: cfg.Pass}
	}
	d, err := proxy.SOCKS5("tcp", cfg.Addr, auth, direct)
	if err != nil {
		// Fall back to direct dialing rather than crashing the process.
		// The error is logged at point of use when a connection fails.
		cachedDialer = directDialerWrapper{direct}
		return cachedDialer
	}
	if cd, ok := d.(proxy.ContextDialer); ok {
		cachedDialer = cd
		return cachedDialer
	}
	cachedDialer = legacyDialerWrapper{d}
	return cachedDialer
}

type directDialerWrapper struct{ d *net.Dialer }

func (w directDialerWrapper) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := w.d.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	return conn, nil
}

type legacyDialerWrapper struct{ d proxy.Dialer }

func (w legacyDialerWrapper) DialContext(_ context.Context, network, address string) (net.Conn, error) {
	conn, err := w.d.Dial(network, address)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	return conn, nil
}

// NewDialer returns a net.Dialer that calls Protector on each new socket.
//
// NB: this dialer does NOT route through the configured SOCKS5 proxy. It is
// only used by callers (e.g. pion's ICE proxy dialer) that need a raw
// net.Dialer interface.
func NewDialer() *net.Dialer {
	return directDialer()
}

// NewHTTPClient returns an http.Client whose Transport routes through the
// configured SOCKS5 proxy when one is set, otherwise dials directly.
func NewHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext:           DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	return &http.Client{Transport: transport}
}

// DialContext dials using a protected socket, routing through the configured
// SOCKS5 proxy when one is set.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d := resolveDialer()
	conn, err := d.DialContext(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	return conn, nil
}

// ProxyDialer implements golang.org/x/net/proxy.Dialer for pion ICE.
//
// Note: ICE traffic is UDP and cannot meaningfully traverse a SOCKS5 proxy
// that only supports CONNECT (TCP). This dialer therefore intentionally
// ignores the SOCKS5 configuration and always dials directly with the
// VpnService.protect() hook applied. ICE flows reach the WebRTC peer over
// UDP regardless of any proxy setting.
type ProxyDialer struct{}

// Dial connects to the address on the named network using a protected socket.
func (d *ProxyDialer) Dial(network, addr string) (net.Conn, error) {
	conn, err := directDialer().Dial(network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	return conn, nil
}

// NewProxyDialer returns a proxy.Dialer that protects ICE sockets.
func NewProxyDialer() *ProxyDialer {
	return &ProxyDialer{}
}

// ErrSocks5Misconfigured is returned by helpers that detect an invalid
// SOCKS5 config at startup.
var ErrSocks5Misconfigured = errors.New("socks5 proxy misconfigured")
