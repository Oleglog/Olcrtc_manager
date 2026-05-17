package protect

import (
	"context"
	"errors"
	"net"
	"net/http"
	"syscall"
	"testing"
	"time"
)

var errProtectBoom = errors.New("boom")

type rawConnStub struct {
	controlFn func(func(uintptr)) error
}

func (r rawConnStub) Control(fn func(uintptr)) error {
	if r.controlFn != nil {
		return r.controlFn(fn)
	}
	fn(42)
	return nil
}
func (r rawConnStub) Read(func(uintptr) bool) error  { return nil }
func (r rawConnStub) Write(func(uintptr) bool) error { return nil }

func TestControlFuncWithoutProtector(t *testing.T) {
	old := Protector
	Protector = nil
	t.Cleanup(func() { Protector = old })

	if err := controlFunc("tcp4", "", rawConnStub{}); err != nil {
		t.Fatalf("controlFunc() error = %v", err)
	}
}

func TestControlFuncWithProtector(t *testing.T) {
	old := Protector
	t.Cleanup(func() { Protector = old })

	called := 0
	Protector = func(fd int) bool {
		called++
		if fd != 42 {
			t.Fatalf("Protector fd = %d, want 42", fd)
		}
		return true
	}
	if err := controlFunc("tcp4", "", rawConnStub{}); err != nil {
		t.Fatalf("controlFunc() error = %v", err)
	}
	if called != 1 {
		t.Fatalf("Protector calls = %d, want 1", called)
	}

	Protector = func(int) bool { return false }
	err := controlFunc("tcp4", "", rawConnStub{})
	var opErr *net.OpError
	if !errors.As(err, &opErr) || opErr.Op != "protect" {
		t.Fatalf("controlFunc() error = %v, want protect op error", err)
	}
}

func TestControlFuncWrapsControlError(t *testing.T) {
	old := Protector
	Protector = func(int) bool { return true }
	t.Cleanup(func() { Protector = old })

	err := controlFunc("tcp4", "", rawConnStub{
		controlFn: func(func(uintptr)) error { return errProtectBoom },
	})
	if err == nil || err.Error() != "control failed: boom" {
		t.Fatalf("controlFunc() error = %v", err)
	}
}

//nolint:cyclop // table-driven test naturally has many branches
func TestNewDialerAndHTTPClient(t *testing.T) {
	dialer := NewDialer()
	if dialer.Timeout != 10*time.Second || dialer.KeepAlive != 30*time.Second || dialer.Control == nil {
		t.Fatalf("NewDialer() = %+v", dialer)
	}

	client := NewHTTPClient()
	tr, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport type = %T, want *http.Transport", client.Transport)
	}
	if tr.DialContext == nil || tr.ForceAttemptHTTP2 || tr.MaxIdleConns != 10 ||
		tr.IdleConnTimeout != 30*time.Second || tr.TLSHandshakeTimeout != 10*time.Second ||
		tr.ResponseHeaderTimeout != 15*time.Second {
		t.Fatalf("transport = %+v", tr)
	}
	if client.Timeout != 25*time.Second {
		t.Fatalf("client.Timeout = %v, want 25s", client.Timeout)
	}
}

func TestDialContextAndProxyDialer(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() { _ = ln.Close() }()

	accepted := make(chan struct{}, 2)
	go func() {
		for range 2 {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
			accepted <- struct{}{}
		}
	}()

	conn, err := DialContext(context.Background(), "tcp4", ln.Addr().String())
	if err != nil {
		t.Fatalf("DialContext() error = %v", err)
	}
	_ = conn.Close()

	proxyConn, err := NewProxyDialer().Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatalf("ProxyDialer.Dial() error = %v", err)
	}
	_ = proxyConn.Close()

	<-accepted
	<-accepted
}

func TestDialFailuresAreWrapped(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	if _, err := DialContext(ctx, "tcp4", "127.0.0.1:1"); err == nil {
		t.Fatal("DialContext() unexpectedly succeeded")
	}
	if _, err := NewProxyDialer().Dial("tcp4", "127.0.0.1:1"); err == nil {
		t.Fatal("ProxyDialer.Dial() unexpectedly succeeded")
	}
}

var _ syscall.RawConn = rawConnStub{}
