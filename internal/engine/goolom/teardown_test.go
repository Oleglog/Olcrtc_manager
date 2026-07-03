package goolom

import (
	"context"
	"testing"
	"time"
)

// TestSleepCtxCancel confirms sleepCtx returns promptly with ctx.Err() when
// the context is cancelled before the duration elapses, so the reconnect
// path bails out during shutdown instead of sleeping through the backoff.
func TestSleepCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := sleepCtx(ctx, 5*time.Second); err == nil {
		t.Fatal("sleepCtx returned nil err on cancelled ctx")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("sleepCtx took %s, want prompt return", elapsed)
	}
}

// TestSleepCtxElapsed confirms sleepCtx returns nil when the duration elapses
// before the context is cancelled.
func TestSleepCtxElapsed(t *testing.T) {
	if err := sleepCtx(context.Background(), 10*time.Millisecond); err != nil {
		t.Fatalf("sleepCtx returned %v, want nil", err)
	}
}

// TestClosePeerConnsNil confirms closePeerConns is a quick no-op when both
// peer connections are nil, never blocking the teardown path.
func TestClosePeerConnsNil(t *testing.T) {
	s := &Session{}
	done := make(chan struct{})
	go func() {
		s.closePeerConns()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closePeerConns blocked with nil peer connections")
	}
}
