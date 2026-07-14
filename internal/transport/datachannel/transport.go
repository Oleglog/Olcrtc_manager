// Package datachannel provides a transport backed by a carrier's data channel.
package datachannel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/engine"
	enginebuiltin "github.com/openlibrecommunity/olcrtc/internal/engine/builtin"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/pion/webrtc/v4"
)

const (
	defaultMaxPayloadSize       = 12*1024 - 12
	serverQueueHighWatermark    = 128
	serverQueueLowWatermark     = 64
	serverBufferedHighWatermark = 512 * 1024
	serverBufferedLowWatermark  = 256 * 1024
)

// ErrByteStreamUnsupported is returned when a carrier engine cannot expose a byte stream.
var ErrByteStreamUnsupported = errors.New("engine does not support byte stream")

type streamTransport struct {
	session               engine.Session
	// ponytail: OnPeerData is the existing server-role signal; add an explicit
	// role only if clients later need peer-routed receive callbacks.
	serverMode            bool
	backpressureMu        sync.Mutex
	backpressured         atomic.Bool
	backpressureSince     atomic.Int64
	backpressureEvents    atomic.Uint64
	backpressureWaitNanos atomic.Int64
}

type byteStreamPayloadSizer interface {
	ByteStreamMaxPayloadSize() int
}

type sendQueueDepthProvider interface {
	SendQueueDepth() int
}

// New creates a datachannel transport backed by a carrier engine.
func New(ctx context.Context, cfg transport.Config) (transport.Transport, error) {
	sess, err := enginebuiltin.Open(ctx, cfg.Carrier, enginebuiltin.Config{
		RoomURL:    cfg.RoomURL,
		Name:       cfg.Name,
		OnData:     cfg.OnData,
		OnPeerData: cfg.OnPeerData,
		DNSServer:  cfg.DNSServer,
		ProxyAddr:  cfg.ProxyAddr,
		ProxyPort:  cfg.ProxyPort,
		Insecure:   cfg.Insecure,
		Engine:     cfg.Engine,
		URL:        cfg.URL,
		Token:      cfg.Token,
		AuthToken:  cfg.AuthToken,
	})
	if err != nil {
		return nil, fmt.Errorf("open engine session: %w", err)
	}

	if !sess.Capabilities().ByteStream {
		_ = sess.Close()
		return nil, ErrByteStreamUnsupported
	}

	return &streamTransport{session: sess, serverMode: cfg.OnPeerData != nil}, nil
}

// Connect starts the transport connection.
func (p *streamTransport) Connect(ctx context.Context) error {
	if err := p.session.Connect(ctx); err != nil {
		return fmt.Errorf("session connect: %w", err)
	}
	return nil
}

// Send transmits data through the transport.
func (p *streamTransport) Send(data []byte) error {
	if err := p.session.Send(data); err != nil {
		return fmt.Errorf("session send: %w", err)
	}
	return nil
}

// SendTo transmits data to a specific remote endpoint when the engine supports it.
func (p *streamTransport) SendTo(peerID string, data []byte) error {
	peer, ok := p.session.(engine.PeerSession)
	if !ok {
		return p.Send(data)
	}
	if err := peer.SendTo(peerID, data); err != nil {
		return fmt.Errorf("session send to peer: %w", err)
	}
	return nil
}

// SupportsPeerRouting reports whether this transport can address individual peers.
func (p *streamTransport) SupportsPeerRouting() bool {
	_, ok := p.session.(engine.PeerSession)
	return ok
}

// Close terminates the transport.
func (p *streamTransport) Close() error {
	if err := p.session.Close(); err != nil {
		return fmt.Errorf("session close: %w", err)
	}
	return nil
}

// ResetPeer clears peer binding on engines that expose it.
func (p *streamTransport) ResetPeer() {
	if resetter, ok := p.session.(interface{ ResetPeer() }); ok {
		resetter.ResetPeer()
	}
}

// Reconnect forwards to the underlying engine session.
func (p *streamTransport) Reconnect(reason string) { p.session.Reconnect(reason) }

// SetReconnectCallback registers reconnect handling.
func (p *streamTransport) SetReconnectCallback(cb func()) {
	p.session.SetReconnectCallback(func(*webrtc.DataChannel) {
		if cb != nil {
			cb()
		}
	})
}

// SetShouldReconnect configures reconnect policy.
func (p *streamTransport) SetShouldReconnect(fn func() bool) {
	p.session.SetShouldReconnect(fn)
}

// SetEndedCallback registers end-of-session handling.
func (p *streamTransport) SetEndedCallback(cb func(string)) {
	p.session.SetEndedCallback(cb)
}

// WatchConnection monitors connection lifecycle.
func (p *streamTransport) WatchConnection(ctx context.Context) {
	p.session.WatchConnection(ctx)
}

// CanSend reports whether transport is ready for sending.
func (p *streamTransport) CanSend() bool {
	if !p.session.CanSend() {
		return false
	}
	if !p.serverMode {
		return true
	}

	depth := p.sendQueueDepth()
	buffered := p.session.GetBufferedAmount()
	if p.backpressured.Load() {
		if depth > serverQueueLowWatermark || buffered > serverBufferedLowWatermark {
			return false
		}
		p.clearBackpressure()
	}
	if depth >= serverQueueHighWatermark || buffered >= serverBufferedHighWatermark {
		p.startBackpressure()
		return false
	}
	return true
}

func (p *streamTransport) sendQueueDepth() int {
	if provider, ok := p.session.(sendQueueDepthProvider); ok {
		return provider.SendQueueDepth()
	}
	queue := p.session.GetSendQueue()
	if queue == nil {
		return 0
	}
	return len(queue)
}

func (p *streamTransport) startBackpressure() {
	p.backpressureMu.Lock()
	defer p.backpressureMu.Unlock()
	if p.backpressured.Load() {
		return
	}
	p.backpressureSince.Store(time.Now().UnixNano())
	p.backpressureEvents.Add(1)
	p.backpressured.Store(true)
}

func (p *streamTransport) clearBackpressure() {
	p.backpressureMu.Lock()
	defer p.backpressureMu.Unlock()
	if !p.backpressured.Load() {
		return
	}
	since := p.backpressureSince.Swap(0)
	if since > 0 {
		p.backpressureWaitNanos.Add(time.Now().UnixNano() - since)
	}
	p.backpressured.Store(false)
}

func (p *streamTransport) backpressureWait() time.Duration {
	nanos := p.backpressureWaitNanos.Load()
	if since := p.backpressureSince.Load(); since > 0 {
		nanos += time.Now().UnixNano() - since
	}
	return time.Duration(nanos)
}

// Metrics reports server-side engine queue pressure.
func (p *streamTransport) Metrics() transport.RuntimeMetrics {
	return transport.RuntimeMetrics{
		SendQueueDepth:     p.sendQueueDepth(),
		BufferedAmount:     p.session.GetBufferedAmount(),
		Backpressured:      p.backpressured.Load(),
		BackpressureEvents: p.backpressureEvents.Load(),
		BackpressureWait:   p.backpressureWait(),
	}
}

// Features describes the current datachannel transport semantics.
func (p *streamTransport) Features() transport.Features {
	maxPayloadSize := defaultMaxPayloadSize
	if sizer, ok := p.session.(byteStreamPayloadSizer); ok {
		if v := sizer.ByteStreamMaxPayloadSize(); v > 0 {
			maxPayloadSize = v
		}
	}
	return transport.Features{
		Reliable:        true,
		Ordered:         true,
		MessageOriented: true,
		MaxPayloadSize:  maxPayloadSize,
	}
}
