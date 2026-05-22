package seichannel

import (
	"bytes"
	"testing"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
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

func TestSEIRoundTripThroughRTPPacketizerAndSampleBuilder(t *testing.T) {
	payload := []byte("hello through rtp")
	accessUnit := buildVideoAccessUnit(payload)

	payloader := &codecs.H264Payloader{}
	packets := payloader.Payload(1200, accessUnit)
	if len(packets) == 0 {
		t.Fatal("H264 payloader returned no packets")
	}

	sb := samplebuilder.New(128, &codecs.H264Packet{}, 90000)
	for i, packetPayload := range packets {
		sb.Push(&rtp.Packet{
			Header: rtp.Header{
				SequenceNumber: uint16(i + 1),
				Timestamp:      1234,
				Marker:         i == len(packets)-1,
			},
			Payload: packetPayload,
		})
	}
	sb.Flush()

	sample := sb.Pop()
	if sample == nil {
		t.Fatal("samplebuilder returned nil sample")
	}

	got, err := extractVideoPayloads(sample.Data)
	if err != nil {
		t.Fatalf("extractVideoPayloads(sample) error = %v", err)
	}
	if len(got) != 1 || !bytes.Equal(got[0], payload) {
		t.Fatalf("RTP SEI payloads = %q, want %q", got, payload)
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

func TestHelloFrameRoundTrip(t *testing.T) {
	hello, err := decodeTransportFrame(encodeHelloFrame())
	if err != nil {
		t.Fatalf("decodeTransportFrame(hello) failed: %v", err)
	}
	if hello.typ != frameTypeHello {
		t.Fatalf("hello frame type = %d, want %d", hello.typ, frameTypeHello)
	}
}
