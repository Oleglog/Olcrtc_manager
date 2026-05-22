package seichannel

import (
	"bytes"
	"errors"
	"testing"
)

func TestFragmentPayload(t *testing.T) {
	frags := fragmentPayload([]byte("abcdef"), 2)
	want := [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}
	if len(frags) != len(want) {
		t.Fatalf("fragment count = %d, want %d", len(frags), len(want))
	}
	for i := range frags {
		if !bytes.Equal(frags[i], want[i]) {
			t.Fatalf("frag %d = %q, want %q", i, frags[i], want[i])
		}
	}

	empty := fragmentPayload(nil, 10)
	if len(empty) != 1 || len(empty[0]) != 0 {
		t.Fatalf("fragmentPayload(nil) = %#v, want one empty frag", empty)
	}
}

func TestDecodeTransportFrameErrorsAndAck(t *testing.T) {
	tests := []struct {
		data []byte
		want error
	}{
		{data: []byte{1, 2, 3}, want: ErrFrameTooShort},
		{data: []byte{0, 0, 0, 0, protocolVersion, frameTypeAck}, want: ErrUnexpectedMagic},
		{data: []byte{0x4f, 0x56, 0x43, 0x31, 9, frameTypeAck}, want: ErrUnexpectedVersion},
		{data: []byte{0x4f, 0x56, 0x43, 0x31, protocolVersion, frameTypeAck}, want: ErrAckTooShort},
		{data: []byte{0x4f, 0x56, 0x43, 0x31, protocolVersion, frameTypeData}, want: ErrDataTooShort},
		{data: []byte{0x4f, 0x56, 0x43, 0x31, protocolVersion, 99}, want: ErrUnexpectedFrameType},
	}
	for _, tt := range tests {
		if _, err := decodeTransportFrame(tt.data); !errors.Is(err, tt.want) {
			t.Fatalf("decodeTransportFrame(%v) error = %v, want %v", tt.data, err, tt.want)
		}
	}

	ack, err := decodeTransportFrame(encodeAckFrame(7, 0x1234))
	if err != nil {
		t.Fatalf("decode ack error = %v", err)
	}
	if ack.typ != frameTypeAck || ack.seq != 7 || ack.crc != 0x1234 {
		t.Fatalf("ack = %+v", ack)
	}
}

func TestSEIHelpersAndErrors(t *testing.T) {
	escaped := escapeRBSP([]byte{0, 0, 1, 0, 0, 2, 3})
	if !bytes.Equal(unescapeRBSP(escaped), []byte{0, 0, 1, 0, 0, 2, 3}) {
		t.Fatalf("unescapeRBSP(escapeRBSP()) = %v", unescapeRBSP(escaped))
	}

	value := appendSEIValue(nil, 300)
	got, next, err := consumeSEIValue(value, 0)
	if err != nil || got != 300 || next != len(value) {
		t.Fatalf("consumeSEIValue() = (%d, %d, %v), want 300", got, next, err)
	}
	if _, _, err := consumeSEIValue([]byte{0xff}, 0); !errors.Is(err, ErrSEIValueTruncated) {
		t.Fatalf("consumeSEIValue() error = %v, want %v", err, ErrSEIValueTruncated)
	}

	rbsp := appendSEIValue(nil, 5)
	rbsp = append(rbsp, appendSEIValue(nil, len(videoSEIUUID)+5)...)
	rbsp = append(rbsp, videoSEIUUID[:]...)
	rbsp = append(rbsp, []byte{1, 2}...)
	if _, err := extractTransportSEI(rbsp); !errors.Is(err, ErrSEIPayloadTruncated) {
		t.Fatalf("extractTransportSEI() error = %v, want %v", err, ErrSEIPayloadTruncated)
	}

	payloads, err := extractTransportSEI([]byte{4, 1, 0, 0x80})
	if err != nil {
		t.Fatalf("extractTransportSEI(non-transport) error = %v", err)
	}
	if len(payloads) != 0 {
		t.Fatalf("extractTransportSEI(non-transport) = %v, want none", payloads)
	}
}
