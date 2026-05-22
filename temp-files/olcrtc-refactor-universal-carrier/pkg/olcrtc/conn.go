package olcrtc

import (
	"errors"
	"fmt"
	"net"
	"time"
)

// conn wraps a Session as a net.Conn.
// Read is backed by an io.Pipe fed by the engine's OnData callback.
// Write calls Session.Send.
// Deadlines are not supported — callers should use context cancellation.
type conn struct {
	s *Session
}

func (c *conn) Read(b []byte) (int, error) {
	n, err := c.s.pr.Read(b)
	if err != nil {
		return n, fmt.Errorf("read: %w", err)
	}
	return n, nil
}

func (c *conn) Write(b []byte) (int, error) {
	if err := c.s.inner.Send(b); err != nil {
		return 0, fmt.Errorf("write: %w", err)
	}
	return len(b), nil
}

func (c *conn) Close() error {
	_ = c.s.pw.CloseWithError(net.ErrClosed)
	if err := c.s.inner.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

func (c *conn) LocalAddr() net.Addr  { return webrtcAddr("local") }
func (c *conn) RemoteAddr() net.Addr { return webrtcAddr("remote") }

func (c *conn) SetDeadline(_ time.Time) error      { return errors.ErrUnsupported }
func (c *conn) SetReadDeadline(_ time.Time) error  { return errors.ErrUnsupported }
func (c *conn) SetWriteDeadline(_ time.Time) error { return errors.ErrUnsupported }

type webrtcAddr string

func (a webrtcAddr) Network() string { return "webrtc" }
func (a webrtcAddr) String() string  { return string(a) }
