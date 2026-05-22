package muxconn

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	cryptopkg "github.com/openlibrecommunity/olcrtc/internal/crypto"
)

var errMuxBoom = errors.New("boom")

type stubLink struct {
	mu        sync.Mutex
	canSend   bool
	sendErr   error
	sent      [][]byte
	canSendFn func() bool
}

func (s *stubLink) Connect(context.Context) error   { return nil }
func (s *stubLink) Close() error                    { return nil }
func (s *stubLink) SetReconnectCallback(func())     {}
func (s *stubLink) SetShouldReconnect(func() bool)  {}
func (s *stubLink) SetEndedCallback(func(string))   {}
func (s *stubLink) WatchConnection(context.Context) {}
func (s *stubLink) Send(data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, append([]byte(nil), data...))
	return s.sendErr
}
func (s *stubLink) CanSend() bool {
	if s.canSendFn != nil {
		return s.canSendFn()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.canSend
}

func newTestCipher(t *testing.T) *cryptopkg.Cipher {
	t.Helper()
	c, err := cryptopkg.NewCipher("01234567890123456789012345678901")
	if err != nil {
		t.Fatalf("NewCipher() error = %v", err)
	}
	return c
}

func TestPushAndReadRoundTrip(t *testing.T) {
	cipher := newTestCipher(t)
	conn := New(&stubLink{canSend: true}, cipher)

	msg1, err := cipher.Encrypt([]byte("hello "))
	if err != nil {
		t.Fatalf("Encrypt(msg1) error = %v", err)
	}
	msg2, err := cipher.Encrypt([]byte("world"))
	if err != nil {
		t.Fatalf("Encrypt(msg2) error = %v", err)
	}

	conn.Push(msg1)
	conn.Push(msg2)

	buf := make([]byte, 11)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got := string(buf[:n]); got != "hello world" {
		t.Fatalf("Read() = %q, want %q", got, "hello world")
	}
}

func TestPushIgnoresInvalidCiphertext(t *testing.T) {
	cipher := newTestCipher(t)
	conn := New(&stubLink{canSend: true}, cipher)

	conn.Push([]byte("bad"))
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	buf := make([]byte, 8)
	n, err := conn.Read(buf)
	if !errors.Is(err, io.EOF) || n != 0 {
		t.Fatalf("Read() = (%d, %v), want (0, EOF)", n, err)
	}
}

func TestWriteEncryptsAndSends(t *testing.T) {
	cipher := newTestCipher(t)
	ln := &stubLink{canSend: true}
	conn := New(ln, cipher)

	n, err := conn.Write([]byte("payload"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if n != len("payload") {
		t.Fatalf("Write() n = %d, want %d", n, len("payload"))
	}
	if len(ln.sent) != 1 {
		t.Fatalf("sent packets = %d, want 1", len(ln.sent))
	}

	got, err := cipher.Decrypt(ln.sent[0])
	if err != nil {
		t.Fatalf("Decrypt(sent) error = %v", err)
	}
	if !bytes.Equal(got, []byte("payload")) {
		t.Fatalf("decrypted payload = %q, want %q", got, "payload")
	}
}

func TestWriteWaitsForCanSend(t *testing.T) {
	cipher := newTestCipher(t)
	start := time.Now()
	readyAt := start.Add(15 * time.Millisecond)
	ln := &stubLink{
		canSendFn: func() bool {
			return time.Now().After(readyAt)
		},
	}
	conn := New(ln, cipher)

	if _, err := conn.Write([]byte("payload")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if len(ln.sent) != 1 {
		t.Fatalf("sent packets = %d, want 1", len(ln.sent))
	}
}

func TestWriteReturnsErrClosedWhileWaiting(t *testing.T) {
	cipher := newTestCipher(t)
	conn := New(&stubLink{canSend: false}, cipher)

	done := make(chan error, 1)
	go func() {
		_, err := conn.Write([]byte("payload"))
		done <- err
	}()

	time.Sleep(10 * time.Millisecond)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("Write() error = %v, want %v", err, ErrClosed)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Write() did not unblock after Close")
	}
}

func TestWriteWrapsSendError(t *testing.T) {
	cipher := newTestCipher(t)
	conn := New(&stubLink{canSend: true, sendErr: errMuxBoom}, cipher)

	_, err := conn.Write([]byte("payload"))
	if err == nil || err.Error() != "send: boom" {
		t.Fatalf("Write() error = %v", err)
	}
}

func TestCloseMakesReadReturnEOF(t *testing.T) {
	cipher := newTestCipher(t)
	conn := New(&stubLink{canSend: true}, cipher)

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4)
		n, err := conn.Read(buf)
		if !errors.Is(err, io.EOF) || n != 0 {
			t.Errorf("Read() = (%d, %v), want (0, EOF)", n, err)
		}
	}()

	time.Sleep(10 * time.Millisecond)
	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("Read() did not unblock after Close")
	}
}
