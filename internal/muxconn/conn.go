// Package muxconn adapts a link.Link into an io.ReadWriteCloser suitable for
// driving a smux session. The wrapper applies AEAD on every wire-bound write
// and inverts it on every received message before exposing the bytes as a
// byte stream.
//
// Link semantics are message-oriented: each Send produces exactly one OnData
// on the peer. smux operates on a pure byte stream (header + payload may be
// glued or split across reads). We bridge by:
//
//   - Treating each Push as an opaque chunk appended to an internal byte
//     buffer that Read drains in arbitrary slices.
//   - Letting smux's sendLoop call Write once per frame; we encrypt and hand
//     the whole buffer to the link as a single message. Length boundaries
//     are preserved end-to-end by the transport (KCP length-prefix framing
//     in vp8channel, native message boundaries in datachannel).
package muxconn

import (
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/link"
)

// ErrClosed is returned from Read/Write after the conn has been closed.
var ErrClosed = errors.New("muxconn: closed")

// Conn is an io.ReadWriteCloser over a link.Link with optional AEAD wrapping.
type Conn struct {
	ln     link.Link
	cipher *crypto.Cipher

	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

// New wires a Conn over the given link. Push must be set as the link's OnData
// callback before this conn is used.
func New(ln link.Link, cipher *crypto.Cipher) *Conn {
	c := &Conn{ln: ln, cipher: cipher}
	c.cond = sync.NewCond(&c.mu)
	return c
}

// Push hands an encrypted wire payload (one OnData event) to the conn.
func (c *Conn) Push(ciphertext []byte) {
	pt, err := c.cipher.Decrypt(ciphertext)
	if err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.buf = append(c.buf, pt...)
	c.cond.Broadcast()
}

// Read implements io.Reader. Blocks until at least one byte is available.
func (c *Conn) Read(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for !c.closed && len(c.buf) == 0 {
		c.cond.Wait()
	}
	if len(c.buf) == 0 {
		return 0, io.EOF
	}
	n := copy(p, c.buf)
	c.buf = c.buf[n:]
	return n, nil
}

// Write encrypts p and ships it to the link as a single message. Blocks while
// the link signals back-pressure.
func (c *Conn) Write(p []byte) (int, error) {
	for {
		if c.isClosed() {
			return 0, ErrClosed
		}
		if c.ln.CanSend() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	enc, err := c.cipher.Encrypt(p)
	if err != nil {
		return 0, fmt.Errorf("encrypt: %w", err)
	}
	if err := c.ln.Send(enc); err != nil {
		return 0, fmt.Errorf("send: %w", err)
	}
	return len(p), nil
}

// Close unblocks any pending Read with io.EOF.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	c.cond.Broadcast()
	return nil
}

func (c *Conn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}
