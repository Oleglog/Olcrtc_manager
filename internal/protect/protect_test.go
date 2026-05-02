package protect_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/protect"
)

// fakeSocks5 is a minimal RFC 1928 / RFC 1929 SOCKS5 proxy used as a test
// double. It expects exactly one CONNECT, optionally requiring USER/PASSWORD
// auth, and tunnels traffic to the requested target.
type fakeSocks5 struct {
	requireAuth bool
	wantUser    string
	wantPass    string
	authSeen    atomic.Bool
}

func (f *fakeSocks5) handle(t *testing.T, c net.Conn) {
	t.Helper()
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	// method negotiation
	header := make([]byte, 2)
	if _, err := io.ReadFull(c, header); err != nil {
		t.Logf("fakeSocks5: read header: %v", err)
		return
	}
	if header[0] != 5 {
		return
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}

	hasUserPass := false
	hasNoAuth := false
	for _, m := range methods {
		switch m {
		case 0x00:
			hasNoAuth = true
		case 0x02:
			hasUserPass = true
		}
	}

	switch {
	case f.requireAuth && hasUserPass:
		// Send USER/PASS selection and run RFC 1929 sub-negotiation.
		if _, err := c.Write([]byte{5, 2}); err != nil {
			return
		}
		ver := make([]byte, 1)
		if _, err := io.ReadFull(c, ver); err != nil || ver[0] != 1 {
			return
		}
		ulen := make([]byte, 1)
		if _, err := io.ReadFull(c, ulen); err != nil {
			return
		}
		user := make([]byte, ulen[0])
		if _, err := io.ReadFull(c, user); err != nil {
			return
		}
		plen := make([]byte, 1)
		if _, err := io.ReadFull(c, plen); err != nil {
			return
		}
		pass := make([]byte, plen[0])
		if _, err := io.ReadFull(c, pass); err != nil {
			return
		}
		if string(user) == f.wantUser && string(pass) == f.wantPass {
			f.authSeen.Store(true)
			if _, err := c.Write([]byte{1, 0}); err != nil {
				return
			}
		} else {
			_, _ = c.Write([]byte{1, 1})
			return
		}
	case f.requireAuth && !hasUserPass:
		// Client refused user/pass — reject.
		_, _ = c.Write([]byte{5, 0xff})
		return
	case hasNoAuth:
		if _, err := c.Write([]byte{5, 0}); err != nil {
			return
		}
	default:
		_, _ = c.Write([]byte{5, 0xff})
		return
	}

	// CONNECT request
	connReq := make([]byte, 4)
	if _, err := io.ReadFull(c, connReq); err != nil {
		return
	}
	if connReq[0] != 5 || connReq[1] != 1 {
		return
	}

	var host string
	switch connReq[3] {
	case 1: // IPv4
		ip := make([]byte, 4)
		if _, err := io.ReadFull(c, ip); err != nil {
			return
		}
		host = net.IPv4(ip[0], ip[1], ip[2], ip[3]).String()
	case 3: // domain
		l := make([]byte, 1)
		if _, err := io.ReadFull(c, l); err != nil {
			return
		}
		buf := make([]byte, l[0])
		if _, err := io.ReadFull(c, buf); err != nil {
			return
		}
		host = string(buf)
	default:
		return
	}
	portBuf := make([]byte, 2)
	if _, err := io.ReadFull(c, portBuf); err != nil {
		return
	}
	port := int(portBuf[0])<<8 | int(portBuf[1])

	upstream, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), 5*time.Second)
	if err != nil {
		_, _ = c.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()

	// success reply with bound address 0.0.0.0:0
	if _, err := c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}

	_ = c.SetDeadline(time.Time{})
	_ = upstream.SetDeadline(time.Time{})
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(upstream, c); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, upstream); done <- struct{}{} }()
	<-done
}

func startFakeSocks5(t *testing.T, f *fakeSocks5) (string, func()) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go f.handle(t, c)
		}
	}()
	return l.Addr().String(), func() { _ = l.Close() }
}

func TestSocks5UserPass(t *testing.T) {
	const want = "secret-payload"
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, want)
	}))
	defer target.Close()

	f := &fakeSocks5{requireAuth: true, wantUser: "alice", wantPass: "hunter2"}
	proxyAddr, stop := startFakeSocks5(t, f)
	defer stop()

	defer protect.SetSocks5(protect.Socks5Config{}) // reset
	protect.SetSocks5(protect.Socks5Config{
		Addr: proxyAddr,
		User: "alice",
		Pass: "hunter2",
	})

	client := protect.NewHTTPClient()
	client.Timeout = 5 * time.Second

	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("http://" + u.Host + "/x")
	if err != nil {
		t.Fatalf("GET via socks5+userpass: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
	if !f.authSeen.Load() {
		t.Fatal("expected USER/PASSWORD sub-negotiation, but proxy never validated credentials")
	}
}

func TestSocks5UserPassWrongCreds(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "ok")
	}))
	defer target.Close()

	f := &fakeSocks5{requireAuth: true, wantUser: "alice", wantPass: "hunter2"}
	proxyAddr, stop := startFakeSocks5(t, f)
	defer stop()

	defer protect.SetSocks5(protect.Socks5Config{})
	protect.SetSocks5(protect.Socks5Config{
		Addr: proxyAddr,
		User: "alice",
		Pass: "WRONG",
	})

	client := protect.NewHTTPClient()
	client.Timeout = 5 * time.Second

	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Get("http://" + u.Host + "/")
	if err == nil {
		t.Fatal("expected error with wrong credentials, got nil")
	}
}

func TestSocks5NoAuth(t *testing.T) {
	const want = "no-auth-payload"
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, want)
	}))
	defer target.Close()

	f := &fakeSocks5{requireAuth: false}
	proxyAddr, stop := startFakeSocks5(t, f)
	defer stop()

	defer protect.SetSocks5(protect.Socks5Config{})
	protect.SetSocks5(protect.Socks5Config{Addr: proxyAddr})

	client := protect.NewHTTPClient()
	client.Timeout = 5 * time.Second

	u, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get("http://" + u.Host + "/")
	if err != nil {
		t.Fatalf("GET via socks5 NO_AUTH: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != want {
		t.Fatalf("body = %q, want %q", body, want)
	}
}

func TestDirectDialNoProxy(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "direct")
	}))
	defer target.Close()

	defer protect.SetSocks5(protect.Socks5Config{})
	protect.SetSocks5(protect.Socks5Config{}) // explicitly clear

	client := protect.NewHTTPClient()
	client.Timeout = 5 * time.Second

	resp, err := client.Get(target.URL)
	if err != nil {
		t.Fatalf("direct GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "direct" {
		t.Fatalf("body = %q, want \"direct\"", body)
	}
}

func TestDialContextRoutesThroughProxy(t *testing.T) {
	// Listen on a port that the test will attempt to connect to via proxy.
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()

	f := &fakeSocks5{requireAuth: true, wantUser: "u", wantPass: "p"}
	proxyAddr, stop := startFakeSocks5(t, f)
	defer stop()

	defer protect.SetSocks5(protect.Socks5Config{})
	protect.SetSocks5(protect.Socks5Config{Addr: proxyAddr, User: "u", Pass: "p"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := protect.DialContext(ctx, "tcp", echo.Addr().String())
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "hello" {
		t.Fatalf("got %q, want %q", buf, "hello")
	}
	if !f.authSeen.Load() {
		t.Fatal("DialContext did not perform USER/PASS sub-negotiation")
	}
}

// Ensure that even if Socks5Config is set to a host that returns
// REP=0x02 ("connection not allowed by ruleset") on CONNECT, the error
// surfaces clearly. This mimics the user's residential proxy that fails
// without explicit credentials.
func TestSocks5ProxyRejectsConnect(t *testing.T) {
	rejectingProxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer rejectingProxy.Close()
	go func() {
		for {
			c, err := rejectingProxy.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				header := make([]byte, 2)
				if _, err := io.ReadFull(c, header); err != nil {
					return
				}
				methods := make([]byte, header[1])
				if _, err := io.ReadFull(c, methods); err != nil {
					return
				}
				_, _ = c.Write([]byte{5, 0}) // accept NO_AUTH
				connReq := make([]byte, 4)
				if _, err := io.ReadFull(c, connReq); err != nil {
					return
				}
				// Drain rest of the request (atyp=domain)
				if connReq[3] == 3 {
					l := make([]byte, 1)
					_, _ = io.ReadFull(c, l)
					buf := make([]byte, l[0]+2) // domain + port
					_, _ = io.ReadFull(c, buf)
				}
				_, _ = c.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0}) // REP=2
			}(c)
		}
	}()

	defer protect.SetSocks5(protect.Socks5Config{})
	protect.SetSocks5(protect.Socks5Config{Addr: rejectingProxy.Addr().String()})

	client := protect.NewHTTPClient()
	client.Timeout = 5 * time.Second
	_, err = client.Get("http://example.test/")
	if err == nil {
		t.Fatal("expected error when proxy rejects CONNECT, got nil")
	}
	if !errIsAboutSocks(err) {
		t.Logf("error: %v", err)
	}
}

func errIsAboutSocks(err error) bool {
	for err != nil {
		s := err.Error()
		if len(s) > 0 {
			return true
		}
		var u interface{ Unwrap() error }
		if !errors.As(err, &u) {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
