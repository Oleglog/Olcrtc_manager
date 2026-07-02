// Package vp8channel provides byte transport over VP8 video frames using KCP.
/*
ЯНДЕКС ПИДОРАС СОСИ МОЙ ЖИРНЫЙ ХУЙ БЛЯТЬ
*/
package vp8channel

import (
	"encoding/binary"
	"hash/crc32"
	"net"
	"sync"
	"time"
)

// wireCRCLen is the size of the CRC32 trailer appended to every KCP packet
// on the wire. KCP is used without FEC/checksum here, while the fake-VP8
// carrier can still perturb payload bytes. Dropping packets with a bad CRC
// restores UDP-like semantics below KCP: corrupted packets are lost and KCP
// retransmits them instead of feeding corrupted bytes into muxconn.
const wireCRCLen = 4

var wireCRCTable = crc32.MakeTable(crc32.Castagnoli) //nolint:gochecknoglobals // read-only hot-path table

func fakeUDPAddr() *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
}

// kcpConn is a net.PacketConn implementation that bridges kcp-go on top of
// the vp8channel byte-message carrier.
//
//	kcp.UDPSession  ──Write──▶  WriteTo  ──▶ outbound chan  ──▶ VP8 wire
//	kcp.UDPSession  ◀──Read──   ReadFrom  ◀── inbound (deliver) ◀── VP8 wire
//
// All packet boundaries are preserved by the underlying transport, which is
// exactly what KCP expects from a UDP-like conn.
type kcpConn struct {
	out       chan<- []byte
	in        chan []byte
	closed    chan struct{}
	closeOnce sync.Once

	// epochHdr is prepended to every outgoing KCP packet so that the peer
	// can detect a session restart on our side (see transport.go for the
	// layout). Stable for the lifetime of this kcpConn.
	epochHdr [epochHdrLen]byte

	mu        sync.Mutex
	rDeadline time.Time
	wDeadline time.Time
}

func newKCPConn(out chan<- []byte, inboundCap int, epochHdr [epochHdrLen]byte) *kcpConn {
	if inboundCap <= 0 {
		inboundCap = 1024
	}
	return &kcpConn{
		out:      out,
		in:       make(chan []byte, inboundCap),
		closed:   make(chan struct{}),
		epochHdr: epochHdr,
	}
}

// deliver hands an incoming wire payload to the KCP read loop. The trailing
// CRC32 is verified and stripped first; a mismatch means the carrier corrupted
// the packet, so we drop it and let KCP retransmit. Drops on overflow are
// intentional - KCP will detect the loss via SACK and retransmit.
func (c *kcpConn) deliver(payload []byte) {
	if len(payload) < wireCRCLen {
		return
	}
	body := payload[:len(payload)-wireCRCLen]
	want := binary.BigEndian.Uint32(payload[len(payload)-wireCRCLen:])
	if crc32.Checksum(body, wireCRCTable) != want {
		return
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	select {
	case c.in <- cp:
	case <-c.closed:
	default:
	}
}

func (c *kcpConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.mu.Lock()
	deadline := c.rDeadline
	c.mu.Unlock()

	var timerC <-chan time.Time
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d <= 0 {
			return 0, nil, TimeoutError{}
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timerC = t.C
	}

	select {
	case msg := <-c.in:
		n := copy(p, msg)
		return n, fakeUDPAddr(), nil
	case <-c.closed:
		return 0, nil, net.ErrClosed
	case <-timerC:
		return 0, nil, TimeoutError{}
	}
}

func (c *kcpConn) WriteTo(p []byte, _ net.Addr) (int, error) {
	// Layout: [epoch header][KCP packet][CRC32(KCP packet)]. The receiver
	// strips the epoch header before deliver(), which verifies and strips CRC.
	buf := make([]byte, epochHdrLen+len(p)+wireCRCLen)
	copy(buf, c.epochHdr[:])
	copy(buf[epochHdrLen:], p)
	binary.BigEndian.PutUint32(buf[epochHdrLen+len(p):], crc32.Checksum(p, wireCRCTable))

	c.mu.Lock()
	deadline := c.wDeadline
	c.mu.Unlock()

	var timerC <-chan time.Time
	if !deadline.IsZero() {
		d := time.Until(deadline)
		if d <= 0 {
			return 0, TimeoutError{}
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timerC = t.C
	}

	select {
	case c.out <- buf:
		return len(p), nil
	case <-c.closed:
		return 0, net.ErrClosed
	case <-timerC:
		return 0, TimeoutError{}
	}
}

func (c *kcpConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *kcpConn) LocalAddr() net.Addr { return fakeUDPAddr() }

func (c *kcpConn) SetDeadline(t time.Time) error {
	_ = c.SetReadDeadline(t)
	_ = c.SetWriteDeadline(t)
	return nil
}

func (c *kcpConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.rDeadline = t
	c.mu.Unlock()
	return nil
}

func (c *kcpConn) SetWriteDeadline(t time.Time) error {
	c.mu.Lock()
	c.wDeadline = t
	c.mu.Unlock()
	return nil
}

// TimeoutError is a net.Error indicating a deadline exceeded.
type TimeoutError struct{}

func (TimeoutError) Error() string { return "i/o timeout" }

// Timeout reports that this error is a timeout.
func (TimeoutError) Timeout() bool { return true }

// Temporary reports that this error is temporary.
func (TimeoutError) Temporary() bool { return true }
