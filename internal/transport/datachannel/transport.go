// Package datachannel provides a transport backed by the current WebRTC providers.
package datachannel

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
)

// defaultMaxPayloadSize is advertised by Features() to the upper layers.
// Pion SCTP negotiates a much larger ceiling (~1 GiB by default), and
// LiveKit's PublishDataPacket has no hard cap on user data, so 64 KiB is a
// safe high-throughput default for a single peer. Override via env if you
// need to fall back to the historical 12 KiB or push higher for testing.
const defaultMaxPayloadSize = 64 * 1024

// envMaxPayloadSize lets operators tune the advertised datachannel payload
// without rebuilding. Both peers should agree to avoid framing surprises.
const envMaxPayloadSize = "OLCRTC_DC_MAX_PAYLOAD"

func resolveMaxPayloadSize() int {
	raw, ok := os.LookupEnv(envMaxPayloadSize)
	if !ok || raw == "" {
		return defaultMaxPayloadSize
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		logger.Warnf("datachannel: invalid %s=%q, using default %d", envMaxPayloadSize, raw, defaultMaxPayloadSize)
		return defaultMaxPayloadSize
	}
	return v
}

type streamTransport struct {
	stream carrier.ByteStream
}

// New creates a datachannel transport backed by a carrier-specific provider.
func New(ctx context.Context, cfg transport.Config) (transport.Transport, error) {
	session, err := carrier.New(ctx, cfg.Carrier, carrier.Config{
		RoomURL:   cfg.RoomURL,
		Name:      cfg.Name,
		OnData:    cfg.OnData,
		DNSServer: cfg.DNSServer,
		ProxyAddr: cfg.ProxyAddr,
		ProxyPort: cfg.ProxyPort,
	})
	if err != nil {
		return nil, fmt.Errorf("create provider transport: %w", err)
	}

	streamCapable, ok := session.(carrier.ByteStreamCapable)
	if !ok {
		return nil, carrier.ErrByteStreamUnsupported
	}

	stream, err := streamCapable.OpenByteStream()
	if err != nil {
		return nil, fmt.Errorf("open byte stream: %w", err)
	}

	return &streamTransport{stream: stream}, nil
}

// Connect starts the transport connection.
func (p *streamTransport) Connect(ctx context.Context) error {
	if err := p.stream.Connect(ctx); err != nil {
		return fmt.Errorf("stream connect: %w", err)
	}
	return nil
}

// Send transmits data through the transport.
func (p *streamTransport) Send(data []byte) error {
	if err := p.stream.Send(data); err != nil {
		return fmt.Errorf("stream send: %w", err)
	}
	return nil
}

// Close terminates the transport.
func (p *streamTransport) Close() error {
	if err := p.stream.Close(); err != nil {
		return fmt.Errorf("stream close: %w", err)
	}
	return nil
}

// SetReconnectCallback registers reconnect handling.
func (p *streamTransport) SetReconnectCallback(cb func()) {
	p.stream.SetReconnectCallback(cb)
}

// SetShouldReconnect configures reconnect policy.
func (p *streamTransport) SetShouldReconnect(fn func() bool) {
	p.stream.SetShouldReconnect(fn)
}

// SetEndedCallback registers end-of-session handling.
func (p *streamTransport) SetEndedCallback(cb func(string)) {
	p.stream.SetEndedCallback(cb)
}

// WatchConnection monitors connection lifecycle.
func (p *streamTransport) WatchConnection(ctx context.Context) {
	p.stream.WatchConnection(ctx)
}

// CanSend reports whether transport is ready for sending.
func (p *streamTransport) CanSend() bool {
	return p.stream.CanSend()
}

// Features describes the current datachannel transport semantics.
func (p *streamTransport) Features() transport.Features {
	return transport.Features{
		Reliable:        true,
		Ordered:         true,
		MessageOriented: true,
		MaxPayloadSize:  resolveMaxPayloadSize(),
	}
}
