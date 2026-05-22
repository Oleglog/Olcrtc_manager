// Package muxconn adapts a link.Link into an io.ReadWriteCloser suitable for
// driving a smux session. The wrapper applies AEAD on every wire-bound write
// and inverts it on every received message before exposing the bytes as a
// byte stream.
//
// Link semantics are message-oriented: each Send produces exactly one OnData
// on the peer. smux operates on a pure byte stream (header + payload may be
// glued or split across reads). We bridge by:
//
//   - Treating each Push as an opaque chunk handed off via a channel that
//     Read drains in arbitrary slices, retaining any tail bytes that did
//     not fit the caller's buffer for the next Read.
//   - Letting smux's sendLoop call Write once per frame; we encrypt and hand
//     the whole buffer to the link as a single message. Length boundaries
//     are preserved end-to-end by the transport (KCP length-prefix framing
//     in vp8channel, native message boundaries in datachannel).
package muxconn

import (
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
)

// ErrClosed is returned from Read/Write after the conn has been closed.
var ErrClosed = errors.New("muxconn: closed")

const (
	// inboundQueue is the buffered capacity of the Push -> Read pipeline.
	// It absorbs short Read stalls without applying back-pressure to the
	// transport callback. Frames are typically smux-sized (well under
	// 16 KiB), so 256 amounts to a few MiB of in-flight data, which is
	// enough for sustained throughput on every transport we have without
	// unbounded growth on a stuck reader.
	inboundQueue = 256

	// pooledFrameCap is the capacity each pooled plaintext buffer is born
	// with. It is sized to fit the largest smux frame any of our
	// transports will deliver after AEAD overhead is stripped (datachannel
	// caps at 12 KiB on the wire, vp8channel at 60 KiB; we round up to
	// give Open room to write in place without growing the slice).
	pooledFrameCap = 64 * 1024
)

// frameBufPool recycles plaintext buffers between Push (decrypts a wire
// frame into a buffer) and Read (consumes the buffer fully then returns
// it). It is global so all Conn instances share the same hot cache —
// most clients in the same process talk to a handful of peers, and
// per-Conn pools fragment the warm set unnecessarily.
var frameBufPool = sync.Pool{ //nolint:gochecknoglobals // intentional process-wide buffer pool
	New: func() any {
		b := make([]byte, 0, pooledFrameCap)
		return &b
	},
}

func acquireFrameBuf() *[]byte {
	bp := frameBufPool.Get().(*[]byte) //nolint:forcetypeassert // pool only ever holds *[]byte
	*bp = (*bp)[:0]
	return bp
}

func releaseFrameBuf(bp *[]byte) {
	if bp == nil {
		return
	}
	// Drop oversized buffers so a one-off huge frame can't poison the
	// pool's working set forever.
	if cap(*bp) > pooledFrameCap*2 {
		return
	}
	*bp = (*bp)[:0]
	frameBufPool.Put(bp)
}

// Conn is an io.ReadWriteCloser over a [transport.Transport] with optional AEAD wrapping.
//
// Push produces decrypted plaintext frames into an internal channel; Read
// drains the channel and slices each frame across as many caller buffers
// as needed. The hot path is lock-free: a single producer (the transport
// callback) and a single consumer (smux's read loop) communicate via a
// buffered channel without any cond/mutex ping-pong.
//
// Plaintext buffers are recycled through frameBufPool: Push borrows a
// buffer to decrypt into, ships it through the channel, and Read returns
// the buffer to the pool once its caller has consumed all the bytes.
type Conn struct {
	ln     transport.Transport
	send   func([]byte) error
	cipher *crypto.Cipher

	in        chan *[]byte
	closeOnce sync.Once
	closeCh   chan struct{}
	closed    atomic.Bool

	// leftoverBuf holds the pool buffer whose tail is still in
	// `leftover`. When `leftover` empties we return leftoverBuf to the
	// pool and clear both fields. Touched only by Read.
	leftoverBuf *[]byte
	leftover    []byte
}

// New wires a Conn over the given transport. Push must be set as the
// transport's OnData callback before this conn is used.
func New(ln transport.Transport, cipher *crypto.Cipher) *Conn {
	return &Conn{
		ln:      ln,
		send:    ln.Send,
		cipher:  cipher,
		in:      make(chan *[]byte, inboundQueue),
		closeCh: make(chan struct{}),
	}
}

// NewPeer wires a Conn whose writes are addressed to a specific transport peer.
func NewPeer(ln transport.PeerTransport, cipher *crypto.Cipher, peerID string) *Conn {
	return &Conn{
		ln: ln,
		send: func(data []byte) error {
			return ln.SendTo(peerID, data)
		},
		cipher:  cipher,
		in:      make(chan *[]byte, inboundQueue),
		closeCh: make(chan struct{}),
	}
}

// Push hands an encrypted wire payload (one OnData event) to the conn.
//
// On the producer side: borrow a pooled plaintext buffer, decrypt into
// it, then either deliver via the inbound channel or, if the caller has
// Close'd, return the buffer to the pool. Blocking forever on a wedged
// reader would wedge the transport callback and trip its watchdog, so we
// also bail on closeCh.
func (c *Conn) Push(ciphertext []byte) {
	bufPtr := acquireFrameBuf()
	pt, err := c.cipher.DecryptInto(*bufPtr, ciphertext)
	if err != nil {
		releaseFrameBuf(bufPtr)
		logger.Debugf("muxconn: decrypt failed, dropping frame: %v", err)
		return
	}
	*bufPtr = pt
	if c.closed.Load() {
		releaseFrameBuf(bufPtr)
		return
	}
	select {
	case c.in <- bufPtr:
	case <-c.closeCh:
		releaseFrameBuf(bufPtr)
	}
}

// Read implements io.Reader. Blocks until at least one byte is available;
// after that, drains additional ready frames non-blockingly to fill p, so
// a single Read can absorb several queued frames in one go. This matches
// the prior cond/append-based implementation's concatenation behaviour
// and lets smux's bufio reader pull large chunks at a time.
func (c *Conn) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(c.leftover) == 0 {
		bufPtr, ok := c.takeFrame()
		if !ok {
			return 0, io.EOF
		}
		c.leftoverBuf = bufPtr
		c.leftover = *bufPtr
	}
	n := copy(p, c.leftover)
	c.leftover = c.leftover[n:]
	c.recycleIfDrained()

	// Greedily pull additional frames already sitting in the queue,
	// without blocking. This keeps the channel from accumulating a
	// backlog when the consumer asks for a large buffer.
	for n < len(p) && len(c.leftover) == 0 {
		select {
		case bufPtr, ok := <-c.in:
			if !ok {
				return n, nil
			}
			data := *bufPtr
			m := copy(p[n:], data)
			n += m
			if m < len(data) {
				c.leftoverBuf = bufPtr
				c.leftover = data[m:]
			} else {
				releaseFrameBuf(bufPtr)
			}
		default:
			return n, nil
		}
	}
	return n, nil
}

// takeFrame blocks until a frame is available or the conn is closed.
// On a clean close it still drains any frame that landed before the
// close signal won the race, so a peer that shuts us down right after a
// final write doesn't lose data.
func (c *Conn) takeFrame() (*[]byte, bool) {
	select {
	case bufPtr, ok := <-c.in:
		if !ok {
			return nil, false
		}
		return bufPtr, true
	case <-c.closeCh:
		select {
		case bufPtr, ok := <-c.in:
			if !ok {
				return nil, false
			}
			return bufPtr, true
		default:
			return nil, false
		}
	}
}

func (c *Conn) recycleIfDrained() {
	if len(c.leftover) == 0 && c.leftoverBuf != nil {
		releaseFrameBuf(c.leftoverBuf)
		c.leftoverBuf = nil
	}
}

// Write encrypts p and ships it to the link as a single message. Blocks while
// the link signals back-pressure.
func (c *Conn) Write(p []byte) (int, error) {
	// Spin briefly first - on a healthy link CanSend usually clears within
	// well under a millisecond, so a 10ms sleep adds visible per-frame
	// latency to interactive request/response traffic. Fall back to a
	// modest sleep only if the link is truly congested.
	const (
		fastSpinAttempts = 200
		slowPollDelay    = 2 * time.Millisecond
	)
	for attempt := 0; ; attempt++ {
		if c.closed.Load() {
			return 0, ErrClosed
		}
		if c.ln.CanSend() {
			break
		}
		if attempt < fastSpinAttempts {
			runtime.Gosched()
			continue
		}
		time.Sleep(slowPollDelay)
	}

	enc, err := c.cipher.Encrypt(p)
	if err != nil {
		return 0, fmt.Errorf("encrypt: %w", err)
	}
	if err := c.send(enc); err != nil {
		return 0, fmt.Errorf("send: %w", err)
	}
	return len(p), nil
}

// Close unblocks any pending Read with io.EOF.
func (c *Conn) Close() error {
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.closeCh)
	})
	return nil
}
