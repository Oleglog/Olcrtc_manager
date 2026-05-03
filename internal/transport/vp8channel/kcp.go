// Package vp8channel provides byte transport over VP8 video frames using KCP.
package vp8channel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	kcp "github.com/xtaci/kcp-go/v5"
)

// Both peers establish a KCP session with the same convid. KCP does not
// require a handshake - packets are matched by conv field, so a static
// constant gives us a symmetrical P2P setup.
const kcpConvID = 0xC0FFEE01

// KCP tuning targets a lossy, bursty carrier (VP8 over an SFU). The defaults
// are TCP-like and recover slowly after burst losses.
const (
	// kcp-go hardcodes mtuLimit=1500, so SetMtu() above this is silently
	// clamped. Stay below that with headroom for KCP overhead (24 bytes).
	kcpMTU = 1400

	// Receive/send window in segments. Large window allows in-flight bursts
	// without stalling - important when one VP8 frame may carry many KCP
	// segments and ACKs trickle back at frame cadence.
	kcpSndWnd = 4096
	kcpRcvWnd = 4096

	// Length prefix for our message framing on top of KCP stream mode.
	// We use stream mode because UDPSession.Write fragments messages > MSS
	// outside of kcp.Send, which destroys the frg field that message mode
	// relies on for boundary preservation. Adding our own length-prefix
	// framing sidesteps that bug entirely.
	kcpLenPrefix = 4

	// Hard cap on a single message. Anything larger would require an
	// unbounded reassembly buffer on the receiver and is almost certainly
	// a protocol error upstream.
	kcpMaxMessage = 8 * 1024 * 1024
)

// ErrKCPMessageTooLarge is returned by send when the message exceeds
// kcpMaxMessage.
var ErrKCPMessageTooLarge = errors.New("vp8channel: kcp message exceeds maximum size")

// kcpRuntime owns the KCP session and the goroutine that pumps reassembled
// messages from KCP up to cfg.OnData.
type kcpRuntime struct {
	conn      *kcpConn
	sess      *kcp.UDPSession
	readDone  chan struct{}
	writeMu   sync.Mutex // serializes length-prefix + payload writes
	closeOnce sync.Once
}

func startKCP(out chan<- []byte, onData func([]byte)) (*kcpRuntime, error) {
	c := newKCPConn(out, inboundQueueSize)

	sess, err := kcp.NewConn3(kcpConvID, fakeUDPAddr(), nil, 0, 0, c)
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("kcp new conn: %w", err)
	}

	// Aggressive ARQ tuning: nodelay=1, interval=10ms, fast resend=2, no
	// congestion control. This is the standard "turbo" preset used by KCP
	// tunnels on lossy networks (shadowsocks, kcptun) and is the whole point
	// of choosing KCP over SCTP.
	sess.SetNoDelay(1, 10, 2, 1)
	sess.SetWindowSize(kcpSndWnd, kcpRcvWnd)
	sess.SetMtu(kcpMTU)
	sess.SetACKNoDelay(true)
	sess.SetWriteDelay(false)

	rt := &kcpRuntime{
		conn:     c,
		sess:     sess,
		readDone: make(chan struct{}),
	}

	go rt.readLoop(onData)

	return rt, nil
}

func (r *kcpRuntime) readLoop(onData func([]byte)) {
	defer close(r.readDone)

	var hdr [kcpLenPrefix]byte
	for {
		if _, err := io.ReadFull(r.sess, hdr[:]); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(hdr[:])
		if size == 0 {
			continue
		}
		if size > kcpMaxMessage {
			// Stream framing is now corrupted - there is no safe way to
			// resync without a session reset. Bail and let the upper layer
			// reconnect.
			return
		}
		payload := make([]byte, size)
		if _, err := io.ReadFull(r.sess, payload); err != nil {
			return
		}
		if onData != nil {
			onData(payload)
		}
	}
}

// deliver hands a wire payload (already reassembled out of VP8 RTP) to KCP.
func (r *kcpRuntime) deliver(payload []byte) {
	r.conn.deliver(payload)
}

// send queues an application message for reliable delivery. The length
// prefix + payload pair is written under a mutex so that interleaved
// concurrent senders cannot tear the framing.
func (r *kcpRuntime) send(msg []byte) error {
	if len(msg) > kcpMaxMessage {
		return ErrKCPMessageTooLarge
	}
	var hdr [kcpLenPrefix]byte
	//nolint:gosec
	binary.BigEndian.PutUint32(hdr[:], uint32(len(msg)))

	r.writeMu.Lock()
	defer r.writeMu.Unlock()

	if _, err := r.sess.Write(hdr[:]); err != nil {
		return fmt.Errorf("kcp write header: %w", err)
	}
	if _, err := r.sess.Write(msg); err != nil {
		return fmt.Errorf("kcp write payload: %w", err)
	}
	return nil
}

func (r *kcpRuntime) close() {
	r.closeOnce.Do(func() {
		_ = r.sess.Close()
		_ = r.conn.Close()
	})
}
