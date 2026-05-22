package videochannel

import (
	"bytes"
	"testing"
)

func TestVisualRoundTrip(t *testing.T) {
	payload := []byte("hello over visual videochannel")
	frame, err := renderVisualFrame(payload, 320, 240, "qrcode", "low", 4, 20)
	if err != nil {
		t.Fatalf("renderVisualFrame failed: %v", err)
	}

	got, err := extractVisualPayload(frame, 320, 240, "qrcode", 4, 20)
	if err != nil {
		t.Fatalf("extractVisualPayload failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got=%q want=%q", got, payload)
	}
}

func TestIdleFrameIgnored(t *testing.T) {
	frame, err := renderVisualFrame(nil, 320, 240, "qrcode", "low", 4, 20)
	if err != nil {
		t.Fatalf("renderVisualFrame failed: %v", err)
	}

	got, err := extractVisualPayload(frame, 320, 240, "qrcode", 4, 20)
	if err == nil && len(got) != 0 {
		t.Fatalf("expected idle frame to be ignored, got=%q", got)
	}
}

func TestTileVisualRoundTrip(t *testing.T) {
	payload := []byte("hello over tile videochannel")
	frame, err := renderVisualFrame(payload, 1080, 1080, "tile", "", 4, 20)
	if err != nil {
		t.Fatalf("renderVisualFrame tile failed: %v", err)
	}

	got, err := extractVisualPayload(frame, 1080, 1080, "tile", 4, 20)
	if err != nil {
		t.Fatalf("extractVisualPayload tile failed: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got=%q want=%q", got, payload)
	}
}

func TestTileIdleFrameIgnored(t *testing.T) {
	frame, err := renderVisualFrame(nil, 1080, 1080, "tile", "", 4, 20)
	if err != nil {
		t.Fatalf("renderVisualFrame tile failed: %v", err)
	}

	got, err := extractVisualPayload(frame, 1080, 1080, "tile", 4, 20)
	if err == nil && len(got) != 0 {
		t.Fatalf("expected tile idle frame to be ignored, got=%q", got)
	}
}

func TestTransportFrameRoundTrip(t *testing.T) {
	encoded := encodeDataFrameForBinding(frameRoleClient, 0x12345678, 42, 0xdeadbeef, 1024, 1, 3, []byte("chunk"))
	decoded, err := decodeTransportFrame(encoded)
	if err != nil {
		t.Fatalf("decodeTransportFrame failed: %v", err)
	}
	assertFrameHeader(t, decoded, frameTypeData, frameRoleClient, 0x12345678, 42, 0xdeadbeef)
	assertFrameFragmentation(t, decoded, 1024, 1, 3)
	if !bytes.Equal(decoded.payload, []byte("chunk")) {
		t.Fatalf("payload mismatch: got=%q", decoded.payload)
	}
}

func assertFrameHeader(t *testing.T, f transportFrame, typ, role byte, binding, seq, crc uint32) {
	t.Helper()
	if f.typ != typ || f.role != role || f.binding != binding || f.seq != seq || f.crc != crc {
		t.Fatalf("unexpected frame header: %+v", f)
	}
}

func assertFrameFragmentation(t *testing.T, f transportFrame, totalLen uint32, fragIdx, fragTotal uint16) {
	t.Helper()
	if f.totalLen != totalLen || f.fragIdx != fragIdx || f.fragTotal != fragTotal {
		t.Fatalf("unexpected fragmentation fields: %+v", f)
	}
}

func TestAcceptFrameRole(t *testing.T) {
	server := &streamTransport{remoteRole: frameRoleClient, bindingToken: 10}
	if !server.acceptFrame(transportFrame{role: frameRoleClient, binding: 10}) {
		t.Fatal("server rejected client frame")
	}
	if server.acceptFrame(transportFrame{role: frameRoleServer, binding: 10}) {
		t.Fatal("server accepted server frame")
	}
	if server.acceptFrame(transportFrame{role: frameRoleClient, binding: 11}) {
		t.Fatal("server accepted different binding")
	}

	client := &streamTransport{remoteRole: frameRoleServer, bindingToken: 20}
	if !client.acceptFrame(transportFrame{role: frameRoleServer, binding: 20}) {
		t.Fatal("client rejected server frame")
	}
	if client.acceptFrame(transportFrame{role: frameRoleClient, binding: 20}) {
		t.Fatal("client accepted client frame")
	}
}
