// Package transport defines transport abstractions and registry.
//
// A transport encodes byte payloads onto a carrier (engine) primitive — either
// a reliable byte stream (datachannel) or a video track (videochannel,
// seichannel, vp8channel). Transport-specific tuning lives in per-transport
// Options types; the common configuration shared by every transport lives in
// [Config].
package transport

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrTransportNotFound is returned when a requested transport is not registered.
var ErrTransportNotFound = errors.New("transport not found")

// ErrOptionsTypeMismatch is returned when a transport receives options of the wrong type.
var ErrOptionsTypeMismatch = errors.New("transport options type mismatch")

// Features describes the delivery semantics of a transport.
type Features struct {
	Reliable        bool
	Ordered         bool
	MessageOriented bool
	MaxPayloadSize  int
}

// Transport defines a byte transport independent of the underlying carrier.
type Transport interface {
	Connect(ctx context.Context) error
	Send(data []byte) error
	Close() error
	SetReconnectCallback(cb func())
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
	Features() Features
	// Reconnect asks the underlying carrier (engine) to tear down and
	// re-establish the SFU connection. Upper layers call this when a
	// liveness probe declares the link dead — useful when the engine has
	// not yet noticed silent packet loss.
	Reconnect(reason string)
}

// PeerTransport is implemented by transports whose carrier can identify and
// address individual remote endpoints.
type PeerTransport interface {
	Transport
	SendTo(peerID string, data []byte) error
	SupportsPeerRouting() bool
}

// Options is a marker for per-transport option structs. Each transport package
// defines its own Options type (e.g. videochannel.Options) and registers a
// factory that consumes it via type assertion. A nil Options is valid for
// transports that need no extra configuration (e.g. datachannel).
type Options interface {
	TransportOptions()
}

// TrafficConfig controls optional reliability-oriented send shaping.
type TrafficConfig struct {
	MaxPayloadSize int
	MinDelay       time.Duration
	MaxDelay       time.Duration
}

// Config holds common transport configuration applicable to every transport.
type Config struct {
	// Carrier is the auth-provider name; engine/URL/token are resolved through it.
	Carrier string
	RoomURL string
	// Engine, URL, Token are forwarded to carrier.Config for the "none" auth
	// carrier (direct engine access without a service-specific auth flow).
	Engine     string
	URL        string
	Token      string
	ChannelID  string
	DeviceID   string
	Name       string
	OnData     func([]byte)
	OnPeerData func(peerID string, data []byte)
	DNSServer  string
	ProxyAddr  string
	ProxyPort  int

	// Options carries transport-specific tuning. Type is per-transport-package.
	Options Options

	// Traffic controls payload-size and pacing shaping applied around the
	// underlying transport's Send.
	Traffic TrafficConfig
}

// Factory creates a transport instance.
type Factory func(ctx context.Context, cfg Config) (Transport, error)

var registry = make(map[string]Factory) //nolint:gochecknoglobals // package-level state intentional

// Register adds a transport factory to the registry.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// New creates a transport instance by name.
func New(ctx context.Context, name string, cfg Config) (Transport, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrTransportNotFound, name)
	}
	tr, err := factory(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return WithTraffic(tr, cfg.Traffic), nil
}

// Available returns a list of registered transport names.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
