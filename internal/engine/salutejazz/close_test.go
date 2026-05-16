package salutejazz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestCloseUnblocksHandleSignaling pins down the shutdown ordering: when a
// peer goroutine is parked in handleSignaling -> ws.ReadJSON, calling Close
// must close the WebSocket up front so ReadJSON returns immediately and the
// signaling loop exits within the closeWaitTimeout. The historical bug had
// Close call wg.Wait() BEFORE closing the WS, so handleSignaling stayed
// parked for the full timeout (and on flaky networks longer once pion's
// PeerConnection.Close kicked in too) — which on CI showed up as
// "tunnel goroutine did not stop: client" in the real e2e jazz matrix.
//
//nolint:cyclop // setup + handler + assertions naturally produces several branches in one test
func TestCloseUnblocksHandleSignaling(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

	// Server side parks on a read so it never closes the connection
	// from its end, forcing the client-side ReadJSON to depend on
	// shutdownWebSocket flipping the read deadline / closing the conn.
	serverDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer func() {
			_ = conn.Close()
			close(serverDone)
		}()
		_, _, _ = conn.ReadMessage()
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	conn, resp, err := dialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}

	s := &Session{
		ws:              conn,
		reconnectCh:     make(chan struct{}, 1),
		closeCh:         make(chan struct{}),
		sessionCloseCh:  make(chan struct{}),
		sendQueue:       make(chan []byte, 1),
		subscriberConn:  make(chan struct{}),
		publisherConn:   make(chan struct{}),
		videoNegotiated: make(chan struct{}),
	}

	// Mirror Connect's bookkeeping for the signaling goroutine so
	// wg.Wait blocks on it during Close.
	signalingDone := make(chan struct{})
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(signalingDone)
		s.handleSignaling(context.Background())
	}()

	start := time.Now()
	if err := s.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	elapsed := time.Since(start)

	// closeWaitTimeout is 2s; with the fix Close should return well under that
	// because shutdownWebSocket trips ReadJSON's deadline up front. Allow some
	// slack so this remains stable on slow CI runners but still fail loudly
	// if the historical 2s wait creeps back in.
	if elapsed > closeWaitTimeout-500*time.Millisecond {
		t.Fatalf("Close() took %s, expected < %s; handleSignaling likely parked", elapsed, closeWaitTimeout)
	}

	select {
	case <-signalingDone:
	case <-time.After(time.Second):
		t.Fatal("handleSignaling did not exit after Close")
	}

	// Drain the server side too so the test doesn't leak goroutines.
	select {
	case <-serverDone:
	case <-time.After(time.Second):
	}
}

// TestShutdownWebSocketIsIdempotent guards the contract that Close can be
// called more than once (e.g. by both the carrier teardown path and a
// defer in tests) without panicking. gorilla/websocket's Close returns
// ErrCloseSent on the second call which we tolerate.
func TestShutdownWebSocketIsIdempotent(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _, _ = conn.ReadMessage()
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}

	s := &Session{ws: conn}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); s.shutdownWebSocket() }()
	go func() { defer wg.Done(); s.shutdownWebSocket() }()
	wg.Wait()
}

// TestCloseWithDeadlineDoesNotBlockOnStraggler pins down that a wedged
// PeerConnection.Close (modeled here as a never-returning closer) does not
// hold up Session.Close past its budget. The historical failure mode showed
// up in the real e2e matrix as "tunnel goroutine did not stop: client" when
// pion's TURN refresh storm kept the ICE agent alive long after the test
// asked it to shut down.
func TestCloseWithDeadlineDoesNotBlockOnStraggler(t *testing.T) {
	deadline := 50 * time.Millisecond
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	closers := []func() error{
		func() error { return nil },
		func() error { <-block; return nil },
	}

	start := time.Now()
	closeWithDeadline(closers, deadline)
	elapsed := time.Since(start)

	if elapsed > deadline*4 {
		t.Fatalf("closeWithDeadline blocked for %s, expected ~%s", elapsed, deadline)
	}
	if elapsed < deadline {
		t.Fatalf("closeWithDeadline returned in %s before deadline %s; straggler ignored",
			elapsed, deadline)
	}
}
