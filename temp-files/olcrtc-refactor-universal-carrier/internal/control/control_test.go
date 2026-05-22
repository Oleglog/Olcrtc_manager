package control

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func controlPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() {
		_ = a.Close()
		_ = b.Close()
	})
	return a, b
}

func TestRunPingPongReportsRTT(t *testing.T) {
	a, b := controlPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := make(chan Health, 1)
	cfg := Config{
		Interval: 10 * time.Millisecond,
		Timeout:  100 * time.Millisecond,
		Failures: 2,
		OnPong: func(h Health) {
			select {
			case got <- h:
			default:
			}
		},
	}
	errCh := make(chan error, 2)
	go func() { errCh <- Run(ctx, a, cfg) }()
	go func() { errCh <- Run(ctx, b, cfg) }()

	select {
	case h := <-got:
		if h.Seq == 0 {
			t.Fatal("Health.Seq = 0")
		}
		if h.RTT < 0 {
			t.Fatalf("Health.RTT = %v", h.RTT)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pong health")
	}

	cancel()
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("Run() after cancel = %v", err)
		}
	}
}

func TestRunMarksUnhealthyAfterMissedPongs(t *testing.T) {
	a, b := controlPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_, _ = io.Copy(io.Discard, b)
	}()

	missedCh := make(chan int, 1)
	missedCallbackCh := make(chan int, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(ctx, a, Config{
			Interval: 10 * time.Millisecond,
			Timeout:  5 * time.Millisecond,
			Failures: 2,
			OnMissedPong: func(missed int) {
				select {
				case missedCallbackCh <- missed:
				default:
				}
			},
			OnUnhealthy: func(missed int) { missedCh <- missed },
		})
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrUnhealthy) {
			t.Fatalf("Run() error = %v, want ErrUnhealthy", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unhealthy result")
	}
	if missed := <-missedCh; missed < 2 {
		t.Fatalf("missed = %d, want >= 2", missed)
	}
	if missed := <-missedCallbackCh; missed < 1 {
		t.Fatalf("missed callback = %d, want >= 1", missed)
	}
}

func TestRunRejectsBadProtocolVersion(t *testing.T) {
	a, b := controlPair(t)
	errCh := make(chan error, 1)
	go func() {
		errCh <- Run(context.Background(), a, Config{Interval: time.Hour})
	}()
	if err := writeFrame(b, Message{Version: 999, Type: TypePing, Seq: 1}); err != nil {
		t.Fatalf("writeFrame() error = %v", err)
	}

	select {
	case err := <-errCh:
		if !errors.Is(err, ErrProtocolVersion) {
			t.Fatalf("Run() error = %v, want ErrProtocolVersion", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for protocol error")
	}
}

func TestReadFrameRejectsTooLarge(t *testing.T) {
	a, b := controlPair(t)
	go func() {
		var hdr [4]byte
		binary.BigEndian.PutUint32(hdr[:], MaxMessageSize+1)
		_, _ = b.Write(hdr[:])
	}()
	_, err := readFrame(a)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("readFrame() error = %v, want ErrFrameTooLarge", err)
	}
}
