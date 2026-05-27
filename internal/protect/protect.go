// Package protect provides functions to protect sockets from VPN routing.
package protect

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultDialTimeout       = 10 * time.Second
	defaultKeepAlive         = 30 * time.Second
	defaultIdleConnTimeout   = 30 * time.Second
	defaultTLSHandshake      = 10 * time.Second
	defaultResponseHeader    = 10 * time.Second
	defaultWebSocketTimeout  = 10 * time.Second
	defaultHTTPClientTimeout = 30 * time.Second
	defaultStatusBodyLimit   = 1024
	perServerDNSTimeout      = 3 * time.Second
)

// HTTPDNSServer is the IPv4 DNS resolver used for plain HTTP/HTTPS dials
// from auth providers. The Android client cannot rely on the system
// resolver because, while the VpnService is up, system DNS lookups go
// through the TUN interface and race with the very session the HTTP call
// is trying to set up. Empty string falls back to the system resolver.
var HTTPDNSServer = "77.88.8.8:53" //nolint:gochecknoglobals // package-level state intentional

// DNSFallbackServers is the static list of well-known public resolvers
// raced in parallel with the user-configured server. Carriers that
// blackhole one of them (a common situation on Russian mobile data)
// will still succeed via a sibling endpoint.
var DNSFallbackServers = []string{ //nolint:gochecknoglobals // package-level state intentional
	"1.1.1.1:53",
	"1.0.0.1:53",
	"8.8.8.8:53",
	"8.8.4.4:53",
	"77.88.8.8:53",
	"77.88.8.1:53",
	"9.9.9.9:53",
}

var (
	sensitiveFieldRE = regexp.MustCompile(
		`(?i)((?:access[_-]?token|room[_-]?token|token|credentials)"?\s*[:=]\s*"?)` +
			`[^",\s}]+`,
	)
	sensitiveBearerRE = regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`)
)

// Protector is called with a socket file descriptor before connect.
// On Android, this calls VpnService.protect(fd) to bypass VPN routing.
var Protector func(fd int) bool //nolint:gochecknoglobals // package-level state intentional

func controlFunc(network, address string, c syscall.RawConn) error {
	if Protector == nil {
		return nil
	}
	var err error
	controlErr := c.Control(func(fd uintptr) {
		if !Protector(int(fd)) {
			log.Printf("[protect] Protector(fd=%d, %s, %s) returned false", int(fd), network, address)
			err = &net.OpError{Op: "protect", Net: network, Err: net.ErrClosed}
		}
	})
	if controlErr != nil {
		return fmt.Errorf("control failed: %w", controlErr)
	}
	return err
}

// NewDialer returns a net.Dialer that calls Protector on each new socket.
//
// The dialer is IPv4-preferring: most carrier networks the Android client
// runs on only carry IPv4 reliably, and Go's default Happy Eyeballs
// otherwise tries AAAA first and fails with ENETUNREACH — even after
// VpnService.protect() reroutes the fd. FallbackDelay: -1 disables
// the IPv4/IPv6 race.
func NewDialer() *net.Dialer {
	return &net.Dialer{
		Timeout:       defaultDialTimeout,
		KeepAlive:     defaultKeepAlive,
		Control:       controlFunc,
		FallbackDelay: -1,
	}
}

// NewTLSConfig returns the shared TLS policy for provider HTTP/WebSocket clients.
func NewTLSConfig() *tls.Config {
	return &tls.Config{MinVersion: tls.VersionTLS12}
}

// NewHTTPTransport returns an HTTP transport using protected sockets.
func NewHTTPTransport() *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           protectedRaceDial,
		TLSClientConfig:       NewTLSConfig(),
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          10,
		IdleConnTimeout:       defaultIdleConnTimeout,
		TLSHandshakeTimeout:   defaultTLSHandshake,
		ResponseHeaderTimeout: defaultResponseHeader,
	}
}

// NewHTTPClient returns an http.Client using protected sockets and a
// protected resolver. Hostname resolution is performed via raceResolve,
// which queries all configured upstreams over both UDP/53 and TCP/53 in
// parallel and takes the first success. The connect step then dials each
// returned IPv4 in order until one succeeds, again through a protected socket.
func NewHTTPClient() *http.Client {
	return &http.Client{
		Transport: NewHTTPTransport(),
		Timeout:   defaultHTTPClientTimeout,
	}
}

// NewWebSocketDialer returns a WebSocket dialer using protected sockets and shared TLS policy.
func NewWebSocketDialer(handshakeTimeout time.Duration) websocket.Dialer {
	if handshakeTimeout <= 0 {
		handshakeTimeout = defaultWebSocketTimeout
	}
	return websocket.Dialer{
		NetDialContext:  DialContext,
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: NewTLSConfig(),
		HandshakeTimeout: handshakeTimeout,
	}
}

// StatusError formats an upstream HTTP error while bounding and redacting the body.
func StatusError(base error, resp *http.Response, limit int64) error {
	if limit <= 0 {
		limit = defaultStatusBodyLimit
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit))
	bodyText := RedactSensitive(strings.TrimSpace(string(body)))
	if bodyText == "" {
		return fmt.Errorf("%w: status %d", base, resp.StatusCode)
	}
	return fmt.Errorf("%w: status %d: %s", base, resp.StatusCode, bodyText)
}

// RedactSensitive removes common token-like values from provider error text.
func RedactSensitive(text string) string {
	text = sensitiveBearerRE.ReplaceAllString(text, "${1}<redacted>")
	return sensitiveFieldRE.ReplaceAllString(text, "${1}<redacted>")
}

// DialContext dials using a protected socket with race-based DNS
// resolution. Forces tcp4 for plain "tcp" networks to avoid IPv6
// routing issues on mobile carriers.
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !strings.HasSuffix(network, "4") && !strings.HasSuffix(network, "6") {
		network = network + "4"
	}
	conn, err := protectedRaceDial(ctx, network, address)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	return conn, nil
}

// ProxyDialer implements golang.org/x/net/proxy.Dialer for pion ICE.
type ProxyDialer struct{}

// Dial connects to the address on the named network using a protected
// socket and race-based DNS resolution. Forces tcp4 for plain "tcp".
func (d *ProxyDialer) Dial(network, addr string) (net.Conn, error) {
	if !strings.HasSuffix(network, "4") && !strings.HasSuffix(network, "6") {
		network = network + "4"
	}
	conn, err := protectedRaceDial(context.Background(), network, addr)
	if err != nil {
		return nil, fmt.Errorf("dial failed: %w", err)
	}
	return conn, nil
}

// NewProxyDialer returns a proxy.Dialer that protects ICE sockets.
func NewProxyDialer() *ProxyDialer {
	return &ProxyDialer{}
}

// raceResolve resolves host to a list of IPv4 addresses by querying the
// configured HTTPDNSServer and every entry of DNSFallbackServers over
// both UDP/53 and TCP/53 in parallel. The first goroutine to return a
// non-empty list of A records wins; everyone else is cancelled. All
// sockets go through controlFunc so they bypass the Android VPN tunnel.
//
//nolint:cyclop // the parallel race naturally has several terminal branches
func raceResolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			return []net.IP{v4}, nil
		}
	}

	servers := DNSServerList()
	if len(servers) == 0 {
		return nil, errors.New("no DNS servers configured")
	}

	raceCtx, cancel := context.WithTimeout(ctx, defaultDialTimeout)
	defer cancel()

	type raceResult struct {
		ips []net.IP
		err error
		src string
	}

	networks := []string{"udp4", "tcp4"}
	total := len(servers) * len(networks)
	resCh := make(chan raceResult, total)
	var wg sync.WaitGroup

	for _, srv := range servers {
		for _, network := range networks {
			wg.Add(1)
			go func(server, network string) {
				defer wg.Done()
				ips, err := lookupVia(raceCtx, host, server, network)
				select {
				case resCh <- raceResult{ips: ips, err: err, src: network + "://" + server}:
				case <-raceCtx.Done():
				}
			}(srv, network)
		}
	}

	go func() {
		wg.Wait()
		close(resCh)
	}()

	var lastErr error
	for res := range resCh {
		if res.err == nil && len(res.ips) > 0 {
			log.Printf("[protect] DNS race won: %s for %s (%d IPv4)", res.src, host, len(res.ips))
			return res.ips, nil
		}
		if res.err != nil {
			lastErr = res.err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("all %d DNS race attempts returned no IPv4 for %s", total, host)
	}
	return nil, fmt.Errorf("DNS race failed for %s: %w", host, lastErr)
}

// DNSServerList returns the configured upstream first (if any), followed
// by the static fallbacks with the configured one de-duplicated.
func DNSServerList() []string {
	seen := map[string]bool{}
	out := make([]string, 0, 1+len(DNSFallbackServers))
	if HTTPDNSServer != "" {
		out = append(out, HTTPDNSServer)
		seen[HTTPDNSServer] = true
	}
	for _, s := range DNSFallbackServers {
		if !seen[s] {
			out = append(out, s)
			seen[s] = true
		}
	}
	return out
}

// lookupVia performs a single A-record lookup for host against the given
// server using the given protected transport (udp4 or tcp4).
func lookupVia(ctx context.Context, host, server, network string) ([]net.IP, error) {
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(c context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{
				Timeout: perServerDNSTimeout,
				Control: controlFunc,
			}
			return d.DialContext(c, network, server)
		},
	}

	queryCtx, cancel := context.WithTimeout(ctx, perServerDNSTimeout)
	defer cancel()

	addrs, err := r.LookupIPAddr(queryCtx, host)
	if err != nil {
		return nil, fmt.Errorf("%s://%s: %w", network, server, err)
	}

	out := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if v4 := a.IP.To4(); v4 != nil {
			out = append(out, v4)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s://%s: no IPv4 for %s", network, server, host)
	}
	return out, nil
}

// protectedRaceDial resolves the host part of addr via raceResolve and
// dials each returned IPv4 in order using a protected tcp4 socket. If
// addr is already a literal IP, it is dialed directly (still protected).
func protectedRaceDial(ctx context.Context, network, addr string) (net.Conn, error) {
	if !strings.HasSuffix(network, "4") && !strings.HasSuffix(network, "6") {
		network = network + "4"
	}

	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("split host/port %q: %w", addr, err)
	}

	dialer := NewDialer()

	if ip := net.ParseIP(host); ip != nil {
		return dialer.DialContext(ctx, network, addr)
	}

	ips, err := raceResolve(ctx, host)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, ip := range ips {
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		log.Printf("[protect] dial %s://%s failed: %v", network, net.JoinHostPort(ip.String(), port), dialErr)
		lastErr = dialErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no IPv4 returned for %s", host)
	}
	return nil, fmt.Errorf("dial %s: %w", host, lastErr)
}
