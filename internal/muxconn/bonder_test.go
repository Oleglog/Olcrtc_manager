package muxconn_test

import (
	"context"
	"encoding/binary"
	"io"
	"sync"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/link"
	"github.com/openlibrecommunity/olcrtc/internal/muxconn"
)

const testKey = "0123456789abcdef0123456789abcdef"

// fakeLink is a link.Link stub that records every Send and routes it back
// to a paired remote bonder via a Push hook.
type fakeLink struct {
	mu     sync.Mutex
	sent   [][]byte
	push   func([]byte)
	closed bool
}

func (f *fakeLink) Connect(_ context.Context) error  { return nil }
func (f *fakeLink) Close() error                     { f.mu.Lock(); f.closed = true; f.mu.Unlock(); return nil }
func (f *fakeLink) SetReconnectCallback(_ func())    {}
func (f *fakeLink) SetShouldReconnect(_ func() bool) {}
func (f *fakeLink) SetEndedCallback(_ func(string))  {}
func (f *fakeLink) WatchConnection(_ context.Context) {}
func (f *fakeLink) CanSend() bool                    { return !f.isClosed() }

func (f *fakeLink) Send(data []byte) error {
	f.mu.Lock()
	cp := make([]byte, len(data))
	copy(cp, data)
	f.sent = append(f.sent, cp)
	push := f.push
	f.mu.Unlock()
	if push != nil {
		push(cp)
	}
	return nil
}

func (f *fakeLink) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func smuxFrame(sid uint32, payload string) []byte {
	buf := make([]byte, 8+len(payload))
	buf[0] = 2 // ver
	buf[1] = 2 // cmdPSH
	binary.LittleEndian.PutUint16(buf[2:], uint16(len(payload))) //nolint:gosec // test payloads are small
	binary.LittleEndian.PutUint32(buf[4:], sid)
	copy(buf[8:], payload)
	return buf
}

func newCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	c, err := crypto.NewCipher(testKey)
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	return c
}

func TestBonderRequiresLinks(t *testing.T) {
	t.Parallel()
	if _, err := muxconn.NewBonder(nil, newCipher(t)); err == nil {
		t.Fatal("expected error for empty link slice")
	}
}

func TestBonderRoutesSameStreamToSamePeer(t *testing.T) {
	t.Parallel()

	const peers = 4
	links := make([]link.Link, peers)
	fakes := make([]*fakeLink, peers)
	for i := range peers {
		fakes[i] = &fakeLink{}
		links[i] = fakes[i]
	}

	b, err := muxconn.NewBonder(links, newCipher(t))
	if err != nil {
		t.Fatalf("NewBonder: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	// Pick a stream id and write 100 frames; all should hit the same peer.
	const sid = uint32(7)
	for range 100 {
		if _, werr := b.Write(smuxFrame(sid, "x")); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
	}

	count := 0
	dest := -1
	for i, fk := range fakes {
		fk.mu.Lock()
		if len(fk.sent) > 0 {
			count++
			dest = i
		}
		fk.mu.Unlock()
	}
	if count != 1 {
		t.Fatalf("expected exactly one peer to receive frames, got %d", count)
	}
	if dest < 0 || dest >= peers {
		t.Fatalf("invalid destination peer %d", dest)
	}
}

func TestBonderSpreadsDistinctStreamsAcrossPeers(t *testing.T) {
	t.Parallel()

	const peers = 4
	links := make([]link.Link, peers)
	fakes := make([]*fakeLink, peers)
	for i := range peers {
		fakes[i] = &fakeLink{}
		links[i] = fakes[i]
	}

	b, err := muxconn.NewBonder(links, newCipher(t))
	if err != nil {
		t.Fatalf("NewBonder: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	// Smux assigns ids in steps of 2 (1, 3, 5, ...); ensure those land on
	// more than one peer.
	for sid := uint32(1); sid < 200; sid += 2 {
		if _, werr := b.Write(smuxFrame(sid, "x")); werr != nil {
			t.Fatalf("Write: %v", werr)
		}
	}

	used := 0
	for _, fk := range fakes {
		fk.mu.Lock()
		if len(fk.sent) > 0 {
			used++
		}
		fk.mu.Unlock()
	}
	if used < 2 {
		t.Fatalf("expected ids to spread across multiple peers, got %d", used)
	}
}

func TestBonderControlFramesGoToPeerZero(t *testing.T) {
	t.Parallel()

	const peers = 3
	links := make([]link.Link, peers)
	fakes := make([]*fakeLink, peers)
	for i := range peers {
		fakes[i] = &fakeLink{}
		links[i] = fakes[i]
	}

	b, err := muxconn.NewBonder(links, newCipher(t))
	if err != nil {
		t.Fatalf("NewBonder: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })

	if _, werr := b.Write(smuxFrame(0, "")); werr != nil {
		t.Fatalf("Write: %v", werr)
	}

	fakes[0].mu.Lock()
	got := len(fakes[0].sent)
	fakes[0].mu.Unlock()
	if got != 1 {
		t.Fatalf("expected control frame on peer 0, got %d", got)
	}
}

// TestBonderRoundTrip plumbs two bonders together through paired fake
// links and verifies that what one writes the other can read.
func TestBonderRoundTrip(t *testing.T) {
	t.Parallel()

	const peers = 3

	cipherA := newCipher(t)
	cipherB := newCipher(t)

	linksA := make([]link.Link, peers)
	linksB := make([]link.Link, peers)
	fakesA := make([]*fakeLink, peers)
	fakesB := make([]*fakeLink, peers)

	var bA, bB *muxconn.Bonder

	for i := range peers {
		fakesA[i] = &fakeLink{}
		fakesB[i] = &fakeLink{}
		linksA[i] = fakesA[i]
		linksB[i] = fakesB[i]
	}

	bA, err := muxconn.NewBonder(linksA, cipherA)
	if err != nil {
		t.Fatalf("NewBonder A: %v", err)
	}
	bB, err = muxconn.NewBonder(linksB, cipherB)
	if err != nil {
		t.Fatalf("NewBonder B: %v", err)
	}
	t.Cleanup(func() { _ = bA.Close(); _ = bB.Close() })

	for i := range peers {
		idx := i
		fakesA[i].push = func(data []byte) { bB.Push(idx, data) }
		fakesB[i].push = func(data []byte) { bA.Push(idx, data) }
	}

	frame := smuxFrame(11, "hello")
	if _, werr := bA.Write(frame); werr != nil {
		t.Fatalf("Write: %v", werr)
	}

	got := make([]byte, len(frame))
	if _, rerr := io.ReadFull(bB, got); rerr != nil {
		t.Fatalf("Read: %v", rerr)
	}
	if string(got) != string(frame) {
		t.Fatalf("round-trip mismatch: got %x want %x", got, frame)
	}
}
