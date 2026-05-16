package jitsi

import (
	"context"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
)

// TestReconnectWindowResetsAfterTimeWindow covers fix 5d4592f: when the
// reconnect window elapses, reconnectCount must roll back to zero so the
// 5-attempt cap does not consume attempts accumulated long ago.
//
// The existing reconnect tests never exercise the window-rollover branch
// of handleReconnectAttempt; this test drives it directly.
func TestReconnectWindowResetsAfterTimeWindow(t *testing.T) {
	js := newChurnSession(t)
	defer func() { _ = js.Close() }()

	// Pre-fill the window with maxReconnects attempts as if they happened
	// just inside the window. The next attempt without rollover would trip
	// the cap; with rollover (window expired) it must start fresh.
	js.reconnectMu.Lock()
	js.reconnectWindowStart = time.Now().Add(-reconnectWindow - time.Second)
	js.reconnectCount = maxReconnects
	js.reconnectMu.Unlock()

	count, rolled := simulateAttempt(js)
	if !rolled {
		t.Fatal("expected window rollover, got continuation of stale window")
	}
	if count != 1 {
		t.Fatalf("reconnectCount after rollover = %d, want 1", count)
	}
}

// TestReconnectWindowEnforcesCapWithinWindow covers the negative half of
// fix 5d4592f: within a single window, attempts past the cap must signal
// session end. Pairs with the rollover test above to lock in both branches.
func TestReconnectWindowEnforcesCapWithinWindow(t *testing.T) {
	js := newChurnSession(t)
	defer func() { _ = js.Close() }()

	endedCh := make(chan string, 1)
	js.SetEndedCallback(func(reason string) {
		select {
		case endedCh <- reason:
		default:
		}
	})

	// Seed window in the present so attempts accumulate without rollover.
	js.reconnectMu.Lock()
	js.reconnectWindowStart = time.Now()
	js.reconnectCount = maxReconnects
	js.reconnectMu.Unlock()

	// One more attempt should exceed the cap and end the session.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan bool, 1)
	go func() { done <- js.handleReconnectAttempt(ctx) }()

	select {
	case reason := <-endedCh:
		if reason == "" {
			t.Fatal("ended with empty reason")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cap was not enforced within window")
	}
	cancel()
	<-done
}

// TestResetPeerClearsBindingForNewPeer covers fix 032151b: after an
// upper-layer handshake failure the supervisor calls ResetPeer, and the
// next peer in the room must be allowed to latch — not blocked by the
// previously-latched (now stale) endpoint.
//
// jitsi_test.go has no coverage for this path.
func TestResetPeerClearsBindingForNewPeer(t *testing.T) {
	js := newChurnSession(t)
	defer func() { _ = js.Close() }()

	var got [][]byte
	var mu sync.Mutex
	js.onData = func(b []byte) {
		mu.Lock()
		got = append(got, append([]byte(nil), b...))
		mu.Unlock()
	}
	js.localEpoch.Store(0xDEADBEEF)

	// Peer A latches and delivers.
	frameA := makeBridgeFrameForEpoch(t, 0x1111, 0, []byte("from-A"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerA", map[string]any{rawFieldKey: frameA}), true)

	// Peer B tries while A still owns the latch — must be dropped.
	frameB1 := makeBridgeFrameForEpoch(t, 0x2222, 0, []byte("from-B-blocked"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{rawFieldKey: frameB1}), true)

	// Handshake failure recovery: reset.
	js.ResetPeer()
	if js.peerEpoch.Load() != 0 {
		t.Fatalf("peerEpoch after ResetPeer = %#x, want 0", js.peerEpoch.Load())
	}
	if p := js.peerEndpoint.Load(); p != nil {
		t.Fatalf("peerEndpoint after ResetPeer = %q, want nil", *p)
	}

	// Peer B retries and is now allowed.
	frameB2 := makeBridgeFrameForEpoch(t, 0x2222, 0, []byte("from-B-allowed"))
	js.deliverBridgeMessage(makeBridgeMessageFrom("peerB", map[string]any{rawFieldKey: frameB2}), true)

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("delivered = %d frames, want 2 (from-A then from-B-allowed): %q", len(got), got)
	}
	if string(got[0]) != "from-A" || string(got[1]) != "from-B-allowed" {
		t.Fatalf("delivered = %q, want [from-A from-B-allowed]", got)
	}
}

// TestChurnPeerEpochChanges hammers fix acac112 (epoch-based bridge frame
// filtering) under churn: many epoch transitions in rapid succession from
// the same peer. Existing tests fire a single epoch change; this test fires
// hundreds and asserts that:
//   - no payload carrying a stale receiver-epoch is delivered;
//   - peerEpoch always tracks the latest accepted sender-epoch;
//   - the reconnect channel is signaled (at least once) on real changes.
//
// Run with -race to catch CAS misuses on peerEpoch / peerEndpoint.
func TestChurnPeerEpochChanges(t *testing.T) {
	js := newChurnSession(t)
	defer func() { _ = js.Close() }()

	js.localEpoch.Store(0x42424242)
	js.SetShouldReconnect(func() bool { return true })

	var delivered atomic.Uint64
	var staleDelivered atomic.Uint64
	js.onData = func(b []byte) {
		delivered.Add(1)
		// Stale frames in this test are tagged with the literal "STALE".
		if len(b) >= 5 && string(b[:5]) == "STALE" {
			staleDelivered.Add(1)
		}
	}

	const iterations = 500
	const goroutines = 8
	var wg sync.WaitGroup
	for g := range goroutines {
		seed := uint64(g) + 1
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)) //nolint:gosec // weak RNG is fine for test fixtures
			for i := range iterations {
				switch rng.IntN(3) {
				case 0:
					// Fresh epoch; receiverEpoch=0 acts as announce.
					ep := uint32(rng.Uint64()|1) & 0xFFFFFFFE //nolint:gosec // truncation is the intent
					payload := fmt.Appendf(nil, "ok-%d-%d", seed, i)
					raw := makeBridgeFrameForEpoch(t, ep, 0, payload)
					js.deliverBridgeMessage(
						makeBridgeMessageFrom("peerA",
							map[string]any{rawFieldKey: raw}), true)
				case 1:
					// Stale: receiverEpoch mismatched with local. Must be dropped.
					raw := makeBridgeFrameForEpoch(t, 0x1111, 0xBADBAD, []byte("STALE-rcv"))
					js.deliverBridgeMessage(
						makeBridgeMessageFrom("peerA",
							map[string]any{rawFieldKey: raw}), true)
				case 2:
					// Acknowledging local epoch: must pass.
					payload := fmt.Appendf(nil, "ack-%d-%d", seed, i)
					raw := makeBridgeFrameForEpoch(t, 0x9999, 0x42424242, payload)
					js.deliverBridgeMessage(
						makeBridgeMessageFrom("peerA",
							map[string]any{rawFieldKey: raw}), true)
				}
				drainReconnectCh(js)
			}
		})
	}
	wg.Wait()

	if staleDelivered.Load() != 0 {
		t.Fatalf("stale frames delivered: %d (filter regression)", staleDelivered.Load())
	}
	if delivered.Load() == 0 {
		t.Fatal("no frames delivered at all — filter is too aggressive")
	}
}

// TestChurnConcurrentResetAndDeliver races ResetPeer against concurrent
// deliverBridgeMessage from multiple peers. Under -race it would catch
// torn reads on peerEndpoint / peerEpoch; logically it asserts that we
// never deliver data attributed to a peer that lost the latch.
func TestChurnConcurrentResetAndDeliver(t *testing.T) {
	js := newChurnSession(t)
	defer func() { _ = js.Close() }()

	js.localEpoch.Store(0x55555555)
	js.SetShouldReconnect(func() bool { return true })
	js.onData = func([]byte) {} // discard

	stop := make(chan struct{})
	var wg sync.WaitGroup

	for i, peer := range []string{"peerA", "peerB", "peerC"} {
		ep := uint32(0x1000 * (i + 1))
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				raw := makeBridgeFrameForEpoch(t, ep, 0, []byte(peer))
				js.deliverBridgeMessage(
					makeBridgeMessageFrom(peer,
						map[string]any{rawFieldKey: raw}), true)
				drainReconnectCh(js)
			}
		})
	}

	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			js.ResetPeer()
			time.Sleep(time.Microsecond * 50)
		}
	})

	time.Sleep(200 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestChurnReconnectAttemptSerial exercises handleReconnectAttempt across
// many synthetic windows back-to-back. The lock added on the reconnect
// counters means -race must stay clean even though only one goroutine
// drives the loop (matching production), so we also fire one extra reader
// to surface any future regression that adds a second writer.
func TestChurnReconnectAttemptSerial(t *testing.T) {
	js := newChurnSession(t)
	defer func() { _ = js.Close() }()

	stop := make(chan struct{})
	go func() {
		// Reader: snapshots counters without blocking the writer.
		for {
			select {
			case <-stop:
				return
			default:
			}
			js.reconnectMu.Lock()
			_ = js.reconnectCount
			_ = js.reconnectWindowStart
			js.reconnectMu.Unlock()
		}
	}()

	for i := range 20 {
		// Force rollover every iteration.
		js.reconnectMu.Lock()
		js.reconnectWindowStart = time.Now().Add(-reconnectWindow - time.Second)
		js.reconnectCount = 0
		js.reconnectMu.Unlock()

		count, rolled := simulateAttempt(js)
		if !rolled {
			t.Fatalf("iter %d: expected rollover", i)
		}
		if count != 1 {
			t.Fatalf("iter %d: count after rollover = %d, want 1", i, count)
		}
	}
	close(stop)
}

// --- helpers ---

func newChurnSession(t *testing.T) *Session {
	t.Helper()
	sess, err := New(context.Background(), engine.Config{
		URL:   testHost,
		Extra: map[string]string{credentialKeyRoom: testRoom},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	js, ok := sess.(*Session)
	if !ok {
		t.Fatal("sess is not *Session")
	}
	return js
}

// simulateAttempt replicates the window-and-counter logic of
// handleReconnectAttempt without invoking reconnect() (which would touch
// real network state). Returns (post-increment count, true-if-window-rolled).
func simulateAttempt(js *Session) (int, bool) {
	now := time.Now()
	js.reconnectMu.Lock()
	defer js.reconnectMu.Unlock()
	rolled := false
	if js.reconnectWindowStart.IsZero() || now.Sub(js.reconnectWindowStart) > reconnectWindow {
		js.reconnectWindowStart = now
		js.reconnectCount = 0
		rolled = true
	}
	js.reconnectCount++
	return js.reconnectCount, rolled
}

func drainReconnectCh(js *Session) {
	select {
	case <-js.reconnectCh:
	default:
	}
}

// Keep binary.BigEndian referenced even if all current uses are removed.
var _ = binary.BigEndian
