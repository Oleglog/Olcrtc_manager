package muxconn

// Bonder fans smux frames out across N parallel link.Link peers and
// reassembles incoming wire payloads from any peer into a single byte
// stream that smux can drain.
//
// Each smux frame carries an 8-byte header with a 4-byte stream id; the
// bonder hashes that id and uses the result to pick a target peer for
// the frame, so all bytes of any individual smux stream are pinned to
// one peer (no reordering hazards). Frames belonging to different
// streams are spread across peers, which lets smux saturate independent
// congestion windows on browsing-style workloads.
//
// Control frames (smux NOP/keepalives, sid==0) and headerless leftovers
// shorter than the smux header are routed via peer 0 so behaviour with
// peers==1 is byte-for-byte identical to the historical single-peer
// muxconn.Conn.
//
// Both sides must agree on the peer count: -peers 1 is the back-compat
// default and disables striping entirely. -peers N (N>1) requires the
// other endpoint to be running with the same N.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"sync"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/link"
)

// ErrNoLinks is returned when a Bonder is constructed with zero peers.
var ErrNoLinks = errors.New("muxconn: bonder requires at least one link")

const (
	smuxHeaderSize    = 8
	smuxSidOffset     = 4
	smuxFallbackPeer  = 0
	smuxBackoffPeriod = 10 * time.Millisecond
)

// Bonder multiplexes a single smux byte stream across N link.Link peers.
type Bonder struct {
	links  []link.Link
	cipher *crypto.Cipher

	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

// NewBonder wraps a slice of links with a shared AEAD cipher.
//
// Wire each link's OnData callback so that every received message is
// handed back via Push - the bonder decrypts, joins, and exposes the
// plaintext via Read.
func NewBonder(links []link.Link, cipher *crypto.Cipher) (*Bonder, error) {
	if len(links) == 0 {
		return nil, ErrNoLinks
	}
	b := &Bonder{
		links:  links,
		cipher: cipher,
	}
	b.cond = sync.NewCond(&b.mu)
	return b, nil
}

// Push hands an encrypted wire payload (one OnData event from any peer)
// to the bonder. The peer index is informational only - all peers feed
// into the same plaintext buffer because hash-based striping pins every
// smux stream to one peer, so no reordering can occur within a stream.
func (b *Bonder) Push(_ int, ciphertext []byte) {
	pt, err := b.cipher.Decrypt(ciphertext)
	if err != nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.buf = append(b.buf, pt...)
	b.cond.Broadcast()
}

// Read implements io.Reader. Blocks until at least one byte is available.
func (b *Bonder) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for !b.closed && len(b.buf) == 0 {
		b.cond.Wait()
	}
	if len(b.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, b.buf)
	b.buf = b.buf[n:]
	return n, nil
}

// Write encrypts p and ships it via the link selected by the smux
// stream id encoded in the first 8 bytes of p.
func (b *Bonder) Write(p []byte) (int, error) {
	idx := b.pickPeer(p)
	ln := b.links[idx]

	for {
		if b.isClosed() {
			return 0, ErrClosed
		}
		if ln.CanSend() {
			break
		}
		time.Sleep(smuxBackoffPeriod)
	}

	enc, err := b.cipher.Encrypt(p)
	if err != nil {
		return 0, fmt.Errorf("encrypt: %w", err)
	}
	if err := ln.Send(enc); err != nil {
		return 0, fmt.Errorf("send via peer %d: %w", idx, err)
	}
	return len(p), nil
}

// pickPeer returns the index of the link that should carry frame p.
//
// We hash the smux stream id rather than taking it modulo N directly
// because smux assigns ids in increments of 2 (odd from the client,
// even from the server); a raw modulo would leave half the peers idle
// for even N. fnv32 distributes uniformly and is cheap.
func (b *Bonder) pickPeer(p []byte) int {
	if len(b.links) == 1 {
		return 0
	}
	if len(p) < smuxHeaderSize {
		return smuxFallbackPeer
	}
	sid := binary.LittleEndian.Uint32(p[smuxSidOffset:smuxHeaderSize])
	if sid == 0 {
		return smuxFallbackPeer
	}
	h := fnv.New32a()
	var idBuf [4]byte
	binary.LittleEndian.PutUint32(idBuf[:], sid)
	_, _ = h.Write(idBuf[:])
	// len(b.links) is bounded by the integer flag (`-peers`, default 4)
	// and validated to be >= 1 in NewBonder, so the conversion to
	// uint32 is safe; gosec G115 cannot prove this from local context.
	n := uint32(len(b.links)) //nolint:gosec // bounded peer count
	return int(h.Sum32() % n)
}

// Close unblocks any pending Read with io.EOF. It does NOT close the
// underlying links: the caller owns their lifecycle, mirroring the
// original muxconn.Conn semantics so a smux session can be reinstalled
// on top of the same links after a transient failure.
func (b *Bonder) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	b.cond.Broadcast()
	return nil
}

// CloseLinks closes every peer link and the bonder itself. Returns the
// joined errors from each link.Close.
func (b *Bonder) CloseLinks() error {
	_ = b.Close()
	var errs []error
	for i, ln := range b.links {
		if err := ln.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close peer %d: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

func (b *Bonder) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}
