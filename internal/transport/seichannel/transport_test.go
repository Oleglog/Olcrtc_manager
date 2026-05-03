package seichannel

import (
	"bytes"
	"testing"
)

func TestSEIRoundTrip(t *testing.T) {
	payload := []byte("hello over seichannel")
	accessUnit := buildVideoAccessUnit(payload)

	got, err := extractVideoPayloads(accessUnit)
	if err != nil {
		t.Fatalf("extractVideoPayloads failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 payload, got %d", len(got))
	}
	if !bytes.Equal(got[0], payload) {
		t.Fatalf("payload mismatch: got=%q want=%q", got[0], payload)
	}
}

func TestTransportFrameRoundTrip(t *testing.T) {
	encoded := encodeDataFrame(42, 0xdeadbeef, 1024, 1, 3, []byte("chunk"))
	decoded, err := decodeTransportFrame(encoded)
	if err != nil {
		t.Fatalf("decodeTransportFrame failed: %v", err)
	}
	if decoded.typ != frameTypeData || decoded.seq != 42 || decoded.crc != 0xdeadbeef {
		t.Fatalf("unexpected frame header: %+v", decoded)
	}
	if decoded.totalLen != 1024 || decoded.fragIdx != 1 || decoded.fragTotal != 3 {
		t.Fatalf("unexpected fragmentation fields: %+v", decoded)
	}
	if !bytes.Equal(decoded.payload, []byte("chunk")) {
		t.Fatalf("payload mismatch: got=%q", decoded.payload)
	}
}
