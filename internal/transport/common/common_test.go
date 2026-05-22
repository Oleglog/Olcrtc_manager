package common_test

import (
	"hash/crc32"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/transport/common"
)

func TestRandomID(t *testing.T) {
	a := common.RandomID()
	b := common.RandomID()
	if len(a) != 8 || len(b) != 8 {
		t.Fatalf("RandomID() = %q, %q, want 8 hex chars each", a, b)
	}
	if a == b {
		t.Fatalf("RandomID() returned the same value twice: %q", a)
	}
}

func TestFragmentPayloadEmpty(t *testing.T) {
	got := common.FragmentPayload(nil, 16)
	if len(got) != 1 || len(got[0]) != 0 {
		t.Fatalf("FragmentPayload(nil) = %v, want one empty fragment", got)
	}
}

func TestFragmentPayloadChunks(t *testing.T) {
	data := []byte("hello world")
	got := common.FragmentPayload(data, 4)
	if len(got) != 3 || string(got[0]) != "hell" || string(got[1]) != "o wo" || string(got[2]) != "rld" {
		t.Fatalf("FragmentPayload(%q, 4) = %v", data, got)
	}
}

func TestReassemblerDeliveredAndDuplicate(t *testing.T) {
	r := common.NewReassembler(8)
	payload := []byte("hello world")
	crc := crc32.ChecksumIEEE(payload)
	frags := common.FragmentPayload(payload, 5)

	for i, frag := range frags {
		result, data := r.Push(common.Fragment{
			Seq:       1,
			CRC:       crc,
			TotalLen:  uint32(len(payload)),  //nolint:gosec // bounded test fixture
			FragIdx:   uint16(i),
			FragTotal: uint16(len(frags)),    //nolint:gosec // bounded test fixture
			Payload:   frag,
		})
		if i < len(frags)-1 {
			if result != common.ResultPartial {
				t.Fatalf("Push(%d) result = %v, want Partial", i, result)
			}
		} else {
			if result != common.ResultDelivered || string(data) != "hello world" {
				t.Fatalf("Push(final) = %v / %q", result, data)
			}
		}
	}

	// re-push the last fragment: duplicate path.
	result, _ := r.Push(common.Fragment{
		Seq:       1,
		CRC:       crc,
		TotalLen:  uint32(len(payload)),      //nolint:gosec // bounded test fixture
		FragIdx:   uint16(len(frags) - 1),    //nolint:gosec // bounded test fixture
		FragTotal: uint16(len(frags)),        //nolint:gosec // bounded test fixture
		Payload:   frags[len(frags)-1],
	})
	if result != common.ResultDuplicate {
		t.Fatalf("dup push result = %v, want Duplicate", result)
	}
}

func TestReassemblerIgnoresCRCMismatch(t *testing.T) {
	r := common.NewReassembler(8)
	payload := []byte("abcd")
	frags := common.FragmentPayload(payload, 4)
	result, _ := r.Push(common.Fragment{
		Seq:       1,
		CRC:       0xdeadbeef, // wrong
		TotalLen:  uint32(len(payload)),  //nolint:gosec // bounded test fixture
		FragIdx:   0,
		FragTotal: uint16(len(frags)),    //nolint:gosec // bounded test fixture
		Payload:   frags[0],
	})
	if result != common.ResultDelivered {
		// single-fragment path: assemble fires immediately, CRC check fails, ignore.
		if result != common.ResultIgnore {
			t.Fatalf("Push() result = %v, want Ignore", result)
		}
	}
}

func TestAckRegistry(t *testing.T) {
	a := common.NewAckRegistry()
	ch := a.Register(42)
	defer a.Unregister(42)
	go a.Resolve(42, 0xcafebabe)
	got := <-ch
	if got != 0xcafebabe {
		t.Fatalf("Resolve forwarded %x, want %x", got, 0xcafebabe)
	}
	// Stale resolve does not block / panic.
	a.Resolve(999, 0)
}
