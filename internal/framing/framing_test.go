package framing_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/framing"
)

func TestRoundTripJSON(t *testing.T) {
	var buf bytes.Buffer
	type msg struct {
		Type string `json:"type"`
		N    int    `json:"n"`
	}
	in := msg{Type: "ping", N: 7}
	if err := framing.WriteJSON(&buf, in, 1024); err != nil {
		t.Fatalf("write: %v", err)
	}
	body, err := framing.ReadBytes(&buf, 1024)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := `{"type":"ping","n":7}`
	if string(body) != want {
		t.Fatalf("body=%q want=%q", body, want)
	}
}

func TestWriteTooLarge(t *testing.T) {
	var buf bytes.Buffer
	err := framing.WriteBytes(&buf, []byte(strings.Repeat("x", 10)), 5)
	if !errors.Is(err, framing.ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestReadTooLarge(t *testing.T) {
	var buf bytes.Buffer
	// Manually craft an oversized header.
	buf.Write([]byte{0x00, 0x00, 0x10, 0x00}) // 4096
	_, err := framing.ReadBytes(&buf, 1024)
	if !errors.Is(err, framing.ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestReadTruncated(t *testing.T) {
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00, 0x00, 0x04})
	buf.WriteByte(0x41) // only 1 of 4 body bytes
	_, err := framing.ReadBytes(&buf, 1024)
	if err == nil || errors.Is(err, framing.ErrFrameTooLarge) {
		t.Fatalf("want EOF/unexpected, got %v", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("want UnexpectedEOF, got %v", err)
	}
}

func TestZeroMaxAllowsAnything(t *testing.T) {
	var buf bytes.Buffer
	big := bytes.Repeat([]byte{0xAA}, 100_000)
	if err := framing.WriteBytes(&buf, big, 0); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := framing.ReadBytes(&buf, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, big) {
		t.Fatalf("roundtrip mismatch")
	}
}
