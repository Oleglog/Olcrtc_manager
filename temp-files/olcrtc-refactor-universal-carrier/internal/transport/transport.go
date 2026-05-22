// Package transport defines transport abstractions and registry.
package transport

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrTransportNotFound is returned when a requested transport is not registered.
	ErrTransportNotFound = errors.New("transport not found")
)

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
}

// TrafficConfig controls optional reliability-oriented send shaping.
type TrafficConfig struct {
	MaxPayloadSize int
	MinDelay       time.Duration
	MaxDelay       time.Duration
}

// Config holds common transport configuration.
type Config struct {
	Carrier string
	RoomURL string
	// Engine, URL, Token are forwarded to carrier.Config for the "none" auth
	// carrier (direct engine access without a service-specific auth flow).
	Engine          string
	URL             string
	Token           string
	ChannelID       string
	DeviceID        string
	Name            string
	OnData          func([]byte)
	DNSServer       string
	ProxyAddr       string
	ProxyPort       int
	VideoWidth      int
	VideoHeight     int
	VideoFPS        int
	VideoBitrate    string
	VideoHW         string
	VideoQRSize     int
	VideoQRRecovery string
	VideoCodec      string
	VideoTileModule int
	VideoTileRS     int
	VP8FPS          int
	VP8BatchSize    int
	SEIFPS          int
	SEIBatchSize    int
	SEIFragmentSize int
	SEIAckTimeoutMS int
	Traffic         TrafficConfig
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
		return nil, ErrTransportNotFound
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
