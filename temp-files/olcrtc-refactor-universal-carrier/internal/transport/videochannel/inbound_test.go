package videochannel

import (
	"bytes"
	"hash/crc32"
	"testing"
)

func TestInboundAssemblyAndAck(t *testing.T) {
	var got []byte
	tr := &streamTransport{
		onData:      func(data []byte) { got = append([]byte(nil), data...) },
		outboundAck: make(chan []byte, 4),
		inbound:     make(map[uint32]*inboundMessage),
		delivered:   make(map[uint32]uint32),
	}

	payload := []byte("hello video")
	crc := crc32.ChecksumIEEE(payload)
	tr.handleInboundFrame(transportFrame{
		typ:       frameTypeData,
		seq:       1,
		crc:       crc,
		totalLen:  uint32(len(payload)), //nolint:gosec // G115: bounded conversion verified by surrounding logic
		fragIdx:   1,
		fragTotal: 2,
		payload:   []byte(" video"),
	})
	if len(got) != 0 {
		t.Fatalf("onData called before message complete: %q", got)
	}

	tr.handleInboundFrame(transportFrame{
		typ:       frameTypeData,
		seq:       1,
		crc:       crc,
		totalLen:  uint32(len(payload)), //nolint:gosec // G115: bounded conversion verified by surrounding logic
		fragIdx:   0,
		fragTotal: 2,
		payload:   []byte("hello"),
	})
	if !bytes.Equal(got, payload) {
		t.Fatalf("assembled payload = %q, want %q", got, payload)
	}
	select {
	case ack := <-tr.outboundAck:
		frame, err := decodeTransportFrame(ack)
		if err != nil || frame.typ != frameTypeAck || frame.seq != 1 || frame.crc != crc {
			t.Fatalf("ack frame = %+v err=%v", frame, err)
		}
	default:
		t.Fatal("handleInboundFrame() did not enqueue ack")
	}
}

func TestInboundRejectsBadFragmentsAndCRC(t *testing.T) {
	tr := &streamTransport{
		outboundAck: make(chan []byte, 2),
		inbound:     make(map[uint32]*inboundMessage),
		delivered:   make(map[uint32]uint32),
	}

	msg, complete := tr.upsertInbound(transportFrame{
		seq:       1,
		crc:       1,
		totalLen:  3,
		fragIdx:   3,
		fragTotal: 1,
		payload:   []byte("bad"),
	})
	if msg != nil || complete {
		t.Fatalf("upsertInbound(out of range) = (%v, %v), want nil false", msg, complete)
	}

	called := false
	tr.onData = func([]byte) { called = true }
	tr.handleInboundFrame(transportFrame{
		seq:       2,
		crc:       123,
		totalLen:  3,
		fragIdx:   0,
		fragTotal: 1,
		payload:   []byte("abc"),
	})
	if called {
		t.Fatal("handleInboundFrame() delivered payload with bad crc")
	}

	msg = &inboundMessage{
		totalLen: 3,
		crc:      crc32.ChecksumIEEE([]byte("abcdef")),
		frags:    [][]byte{[]byte("abc"), []byte("def")},
	}
	if got := tr.assembleMessage(msg); string(got) != "abc" {
		t.Fatalf("assembleMessage() = %q, want abc", got)
	}
}
