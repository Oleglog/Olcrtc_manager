package vp8channel

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

func pumpPackets(stop <-chan struct{}, from <-chan []byte, to *kcpRuntime) {
	for {
		select {
		case <-stop:
			return
		case pkt := <-from:
			// Strip the on-wire epoch header that kcpConn prepends;
			// the real receive path does this before calling deliver().
			if len(pkt) > epochHdrLen {
				to.deliver(pkt[epochHdrLen:])
			}
		}
	}
}

func checkMessages(t *testing.T, got, want [][]byte) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d messages, want %d", len(got), len(want))
	}
	for i, m := range want {
		if !bytes.Equal(got[i], m) {
			t.Errorf("msg %d mismatch: got %d bytes, want %d", i, len(got[i]), len(m))
		}
	}
}

func buildReceiver(n int) (func([]byte), <-chan struct{}, func() [][]byte) {
	var mu sync.Mutex
	var recv [][]byte
	done := make(chan struct{})
	cb := func(msg []byte) {
		mu.Lock()
		recv = append(recv, append([]byte(nil), msg...))
		count := len(recv)
		mu.Unlock()
		if count == n {
			close(done)
		}
	}
	get := func() [][]byte {
		mu.Lock()
		defer mu.Unlock()
		return recv
	}
	return cb, done, get
}

// TestKCPLoopback runs two KCP runtimes back-to-back through an in-memory
// pipe simulating a perfect carrier. Verifies that messages survive the
// KCP layer with their boundaries intact.
func TestKCPLoopback(t *testing.T) {
	msgs := [][]byte{
		[]byte("hello"),
		bytes.Repeat([]byte("x"), 1000),
		bytes.Repeat([]byte("y"), 20000),
	}

	a2b := make(chan []byte, 256)
	b2a := make(chan []byte, 256)

	cb, doneB, getRecv := buildReceiver(len(msgs))

	rtA, err := startKCP(a2b, nil, testEpochHdr(1))
	if err != nil {
		t.Fatalf("startKCP A: %v", err)
	}
	defer rtA.close()

	rtB, err := startKCP(b2a, cb, testEpochHdr(2))
	if err != nil {
		t.Fatalf("startKCP B: %v", err)
	}
	defer rtB.close()

	stop := make(chan struct{})
	defer close(stop)

	go pumpPackets(stop, a2b, rtB)
	go pumpPackets(stop, b2a, rtA)

	for _, m := range msgs {
		if err := rtA.send(m); err != nil {
			t.Fatalf("send: %v", err)
		}
	}

	select {
	case <-doneB:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for messages")
	}

	checkMessages(t, getRecv(), msgs)
}

func TestVP8KeepaliveDoesNotLookLikeKCP(t *testing.T) {
	if len(vp8Keepalive) >= 1 && vp8Keepalive[0] == kcpFrameMagic {
		t.Errorf("keepalive collides with kcp magic byte 0x%02x", kcpFrameMagic)
	}
}

func testEpochHdr(epoch uint32) [epochHdrLen]byte {
	var hdr [epochHdrLen]byte
	hdr[0] = kcpFrameMagic
	hdr[1] = byte(epoch >> 24)
	hdr[2] = byte(epoch >> 16) //nolint:gosec
	hdr[3] = byte(epoch >> 8)  //nolint:gosec
	hdr[4] = byte(epoch)       //nolint:gosec
	return hdr
}
