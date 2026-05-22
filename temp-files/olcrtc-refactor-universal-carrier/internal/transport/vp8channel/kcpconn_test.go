package vp8channel

import (
	"bytes"
	"errors"
	"net"
	"testing"
	"time"
)

//nolint:cyclop // table-driven test naturally has many branches
func TestKCPConnReadWriteDeadlinesAndClose(t *testing.T) {
	out := make(chan []byte, 1)
	hdr := testEpochHdr(9)
	conn := newKCPConn(out, 1, hdr)

	if err := conn.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetDeadline() error = %v", err)
	}
	if conn.LocalAddr().String() != "127.0.0.1:1" {
		t.Fatalf("LocalAddr() = %v", conn.LocalAddr())
	}

	n, err := conn.WriteTo([]byte("payload"), nil)
	if err != nil || n != len("payload") {
		t.Fatalf("WriteTo() = (%d, %v), want payload length", n, err)
	}
	wire := <-out
	if !bytes.Equal(wire[:epochHdrLen], hdr[:]) || string(wire[epochHdrLen:]) != "payload" {
		t.Fatalf("wire packet = %v", wire)
	}

	conn.deliver([]byte("incoming"))
	buf := make([]byte, 64)
	n, addr, err := conn.ReadFrom(buf)
	if err != nil || addr == nil || string(buf[:n]) != "incoming" {
		t.Fatalf("ReadFrom() = (%d, %v, %v), payload %q", n, addr, err, buf[:n])
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, _, err := conn.ReadFrom(buf); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("ReadFrom() error = %v, want net.ErrClosed", err)
	}

	closedWrite := newKCPConn(make(chan []byte), 1, hdr)
	_ = closedWrite.Close()
	if _, err := closedWrite.WriteTo([]byte("x"), nil); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("WriteTo() error = %v, want net.ErrClosed", err)
	}
}

func TestKCPConnTimeouts(t *testing.T) {
	conn := newKCPConn(make(chan []byte), 1, testEpochHdr(1))
	if err := conn.SetReadDeadline(time.Now().Add(-time.Millisecond)); err != nil {
		t.Fatalf("SetReadDeadline() error = %v", err)
	}
	buf := make([]byte, 4)
	if _, _, err := conn.ReadFrom(buf); err == nil {
		t.Fatal("ReadFrom() unexpectedly succeeded")
	} else {
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("ReadFrom() error = %T %v, want timeout net.Error", err, err)
		}
	}

	if err := conn.SetWriteDeadline(time.Now().Add(-time.Millisecond)); err != nil {
		t.Fatalf("SetWriteDeadline() error = %v", err)
	}
	if _, err := conn.WriteTo([]byte("x"), nil); err == nil {
		t.Fatal("WriteTo() unexpectedly succeeded")
	}
}
