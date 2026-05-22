package common_test

import (
	"bytes"
	"hash/crc32"
	"math/rand/v2"
	"sync"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/transport/common"
)

// TestReassemblerStressShuffledFragments hammers the reassembler with many
// concurrent messages whose fragments arrive in fully randomized order,
// with duplicates and interleaving across seqs. This mirrors what real
// transports (seichannel, videochannel) see under high RTT + reorder.
//
// Invariant: every payload, once Push returns ResultDelivered, must match
// the original bytes exactly.
//
//nolint:cyclop // stress fixture intentionally exercises many branches in one test
func TestReassemblerStressShuffledFragments(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in -short mode")
	}
	const messages = 200
	const fragSize = 64
	r := common.NewReassembler(messages * 2)
	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xDEADBEEF)) //nolint:gosec // weak RNG is fine for test fixtures

	type plan struct {
		seq     uint32
		payload []byte
		crc     uint32
		frags   []common.Fragment
	}

	plans := make([]*plan, messages)
	var allDrops []common.Fragment
	for i := range plans {
		size := 50 + rng.IntN(2000)
		p := make([]byte, size)
		for j := range p {
			p[j] = byte(rng.Uint32()) //nolint:gosec // truncation is the intent
		}
		raw := common.FragmentPayload(p, fragSize)
		seq := uint32(i + 1)
		crc := crc32.ChecksumIEEE(p)
		pl := &plan{seq: seq, payload: p, crc: crc, frags: make([]common.Fragment, 0, len(raw))}
		for idx, frag := range raw {
			pl.frags = append(pl.frags, common.Fragment{
				Seq:       seq,
				CRC:       crc,
				TotalLen:  uint32(len(p)), //nolint:gosec // test fixture, bounded
				FragIdx:   uint16(idx),
				FragTotal: uint16(len(raw)), //nolint:gosec // bounded
				Payload:   frag,
			})
			// 20% duplicate injection
			if rng.Float64() < 0.20 {
				allDrops = append(allDrops, pl.frags[len(pl.frags)-1])
			}
		}
		plans[i] = pl
	}

	// Build the global delivery sequence: every fragment from every message,
	// plus the duplicate batch, then shuffle.
	var all []common.Fragment
	for _, p := range plans {
		all = append(all, p.frags...)
	}
	all = append(all, allDrops...)
	rng.Shuffle(len(all), func(i, j int) { all[i], all[j] = all[j], all[i] })

	delivered := make(map[uint32][]byte, messages)
	dupCount := 0
	for _, f := range all {
		res, data := r.Push(f)
		switch res {
		case common.ResultDelivered:
			if existing, ok := delivered[f.Seq]; ok {
				// Re-delivery would be a logic error.
				t.Fatalf("seq %d delivered twice (was %d bytes, now %d)", f.Seq, len(existing), len(data))
			}
			delivered[f.Seq] = append([]byte(nil), data...)
		case common.ResultDuplicate:
			dupCount++
		case common.ResultPartial, common.ResultIgnore:
			// expected
		}
	}

	for _, p := range plans {
		got, ok := delivered[p.seq]
		if !ok {
			t.Fatalf("seq %d never delivered (had %d fragments)", p.seq, len(p.frags))
		}
		if !bytes.Equal(got, p.payload) {
			t.Fatalf("seq %d payload mismatch: got %d bytes, want %d", p.seq, len(got), len(p.payload))
		}
	}
	if dupCount == 0 {
		t.Fatal("test injected duplicates but reassembler reported none — duplicate path not exercised")
	}
	t.Logf("delivered %d/%d messages, observed %d duplicates", len(delivered), messages, dupCount)
}

// TestReassemblerConcurrentPushIsSafe drives many goroutines pushing
// fragments for distinct seqs into the same reassembler. The reassembler
// must serialize via its mutex without deadlocking or torn-state.
// Run with -race.
func TestReassemblerConcurrentPushIsSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent stress test in -short mode")
	}
	const writers = 16
	const perWriter = 50
	r := common.NewReassembler(writers * perWriter * 2)

	var wg sync.WaitGroup
	for w := range writers {
		base := uint32(w * perWriter)
		wg.Go(func() {
			rng := rand.New(rand.NewPCG(uint64(w)+1, 0xC0DE)) //nolint:gosec // test seed
			for i := range perWriter {
				size := 30 + rng.IntN(500)
				p := make([]byte, size)
				for j := range p {
					p[j] = byte(rng.Uint32()) //nolint:gosec // truncation is the intent
				}
				seq := base + uint32(i) + 1
				crc := crc32.ChecksumIEEE(p)
				raw := common.FragmentPayload(p, 32)
				idxs := rng.Perm(len(raw))
				for _, idx := range idxs {
					r.Push(common.Fragment{
						Seq:       seq,
						CRC:       crc,
						TotalLen:  uint32(len(p)), //nolint:gosec // bounded
						FragIdx:   uint16(idx),    //nolint:gosec // bounded
						FragTotal: uint16(len(raw)), //nolint:gosec // bounded
						Payload:   raw[idx],
					})
				}
			}
		})
	}
	wg.Wait()
}
