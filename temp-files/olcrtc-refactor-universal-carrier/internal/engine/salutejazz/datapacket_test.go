package salutejazz

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestDataPacketRoundTrip(t *testing.T) {
	payload := []byte("hello jazz")
	raw := EncodeDataPacket(payload)

	got, ok := DecodeDataPacket(raw)
	if !ok {
		t.Fatal("DecodeDataPacket() ok = false")
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("DecodeDataPacket() = %q, want %q", got, payload)
	}
}

func TestDecodeDataPacketRejectsMalformedPackets(t *testing.T) {
	tests := [][]byte{
		nil,
		{0xff},
		encodeField(1, 0, encodeVarint(0)),
		{byte(2<<3 | 2), 10, 1},
		{byte(3<<3 | 7), 0},
	}

	for _, raw := range tests {
		if payload, ok := DecodeDataPacket(raw); ok {
			t.Fatalf("DecodeDataPacket(%v) = (%q, true), want false", raw, payload)
		}
	}
}

func TestParseFieldsSkipsSupportedNonTargetWireTypes(t *testing.T) {
	data := encodeField(1, 0, encodeVarint(150))
	data = append(data, encodeField(3, 1, []byte("12345678"))...)
	data = append(data, encodeField(4, 5, []byte("1234"))...)
	data = append(data, encodeField(2, 2, []byte("target"))...)

	got, ok := parseFields(data, 2)
	if !ok || string(got) != "target" {
		t.Fatalf("parseFields() = (%q, %v), want target", got, ok)
	}
}

func TestByteReader(t *testing.T) {
	r := &byteReader{data: []byte{1, 2, 3}}
	b, err := r.ReadByte()
	if err != nil || b != 1 {
		t.Fatalf("ReadByte() = (%d, %v), want (1, nil)", b, err)
	}

	buf := make([]byte, 4)
	n, err := r.Read(buf)
	if err != nil || n != 2 || !bytes.Equal(buf[:n], []byte{2, 3}) {
		t.Fatalf("Read() = (%d, %v, %v), want two bytes", n, err, buf[:n])
	}

	if _, err := r.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadByte() error = %v, want EOF", err)
	}
	if n, err := r.Read(buf); !errors.Is(err, io.EOF) || n != 0 {
		t.Fatalf("Read() = (%d, %v), want (0, EOF)", n, err)
	}
}
