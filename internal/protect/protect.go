// Package protect provides functions to protect sockets from VPN routing
// and (optionally) route all outbound TCP through a SOCKS5 proxy.
package protect

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/proxy"
)

// Protector is called with a socket file descriptor before connect.
// On Android, this calls VpnService.protect(fd) to bypass VPN routing.
var Protector func(fd int) bool //nolint:gochecknoglobals

// HTTPDNSServer is the IPv4 DNS resolver used for plain HTTP/HTTPS dials
// from auth providers. The Android client cannot rely on the system
// resolver because, while the VpnService is up, system DNS lookups go
// through the VPN tunnel — which deadlocks when the auth-path
// HTTP call is trying to set up. On Linux servers it also helps when
// the host resolver is misconfigured or blocked.
//
// Use TCP to dial this address: many mobile carriers in our target
// markets intercept or drop outbound UDP/53 to public resolvers but
// reliably pass TCP/53 to 1.1.1.1 / 8.8.8.8 / 77.88.8.8.
//
// Empty string falls back to net.DefaultResolver (used in tests and
// non-mobile setups where the system resolver works).
var HTTPDNSServer string //nolint:gochecknoglobals // package-level state intentional

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
		if !Protector(int(fd)) {
			err = &net.OpError{Op: "protect", Net: network, Err: net.ErrClosed}
		}
	})
	if controlErr != nil {
		return fmt.Errorf("control failed: %w", controlErr)
	}
	return err
}

// newProtectedResolver returns a net.Resolver whose own socket to the
// configured HTTPDNSServer is protected from VPN routing and pinned to
// TCP/53.
//
// We force TCP because UDP/53 to external resolvers is unreliable on
// some carriers and middleboxes (it gets MITM'd or silently dropped),
// while TCP/53 to public resolvers like 1.1.1.1 / 8.8.8.8 / 77.88.8.8
// is consistently reachable.
//
// If HTTPDNSServer is empty, callers get net.DefaultResolver — the
// system resolver behaves correctly outside of mobile/VPN contexts.
func newProtectedResolver() *net.Resolver {
	if HTTPDNSServer == "" {
		return net.DefaultResolver
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: 5 * time.Second,
				Control: controlFunc,
			}
			return d.DialContext(ctx, "tcp4", HTTPDNSServer)
		},
	}
}

// directDialer returns the underlying net.Dialer that opens a fresh socket,
// honoring the Android VpnService.protect() hook and routing DNS lookups
// through HTTPDNSServer (when set) instead of the system resolver.
func directDialer() *net.Dialer {
	d := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlFunc,
		// Disable Happy Eyeballs IPv4/IPv6 racing. On mobile carriers
		// the AAAA path often resolves but is unreachable, and the
		// race-and-fail behavior surfaces as ENETUNREACH after a long
		// wait. We can re-enable it once we have IPv6 telemetry.
		FallbackDelay: -1,
	}
	d.Resolver = newProtectedResolver()
	return d
}

// forceIPv4 returns the network argument with an explicit "4" suffix when
// the caller passed a generic "tcp" / "udp" and HTTPDNSServer is configured.
// This keeps the outbound socket on IPv4 — see directDialer for rationale.
func forceIPv4(network string) string {
	if HTTPDNSServer == "" {
		return network
	}
	if strings.HasSuffix(network, "4") || strings.HasSuffix(network, "6") {
		return network
	}
	switch network {
	case "tcp", "udp":
		return network + "4"
	}
	return network
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
// configured SOCKS5 proxy when one is set, otherwise dials directly. DNS
// lookups go through HTTPDNSServer when configured.
//
// HTTP/2 is disabled on purpose: pion's preconnect path used by the auth
// providers expects HTTP/1.1 keep-alive semantics.
func NewHTTPClient() *http.Client {
	transport := &http.Transport{
		DialContext:           DialContext,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          10,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   25 * time.Second,
	}
}

// DialContext dials using a protected socket, routing through the configured
// SOCKS5 proxy when one is set. When HTTPDNSServer is configured, generic
// "tcp"/"udp" networks are forced to IPv4 to dodge IPv6 routing surprises
// on mobile carriers.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d := resolveDialer()
	conn, err := d.DialContext(ctx, forceIPv4(network), address)
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
// Forces IPv4 for generic networks when HTTPDNSServer is set.
func (d *ProxyDialer) Dial(network, addr string) (net.Conn, error) {
	conn, err := directDialer().Dial(forceIPv4(network), addr)
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
