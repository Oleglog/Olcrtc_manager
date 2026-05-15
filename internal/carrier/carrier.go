// Package carrier exposes carrier-oriented registration and construction APIs.
package carrier

import (
	"context"
	"errors"
)

var (
	// ErrCarrierNotFound is returned when a requested carrier is not registered.
	ErrCarrierNotFound = errors.New("carrier not found")
	// ErrByteStreamUnsupported is returned when a carrier cannot provide a byte stream.
	ErrByteStreamUnsupported = errors.New("carrier does not support byte stream")
	// ErrVideoTrackUnsupported is returned when a carrier cannot exchange video tracks.
	ErrVideoTrackUnsupported = errors.New("carrier does not support video tracks")
	// ErrAuthFailed is returned when a carrier's auth provider rejects the request.
	ErrAuthFailed = errors.New("carrier auth failed")
)

// Capabilities describes the transport primitives a carrier can expose.
type Capabilities struct {
	ByteStream bool
	VideoTrack bool
}

// Session is the carrier-level runtime handle.
type Session interface {
	Capabilities() Capabilities
}

// ByteStreamCapable is implemented by carriers that can expose a byte stream.
type ByteStreamCapable interface {
	OpenByteStream() (ByteStream, error)
}

// VideoTrackCapable is implemented by carriers that can exchange video tracks.
type VideoTrackCapable interface {
	OpenVideoTrack() (VideoTrack, error)
}

// Config holds carrier configuration.
type Config struct {
	RoomURL   string
	Name      string
	OnData    func([]byte)
	DNSServer string
	ProxyAddr string
	ProxyPort int
	// URL, Token, and Engine are used by the "none" auth carrier (direct engine access).
	URL    string
	Token  string
	Engine string
}

// Factory creates a new carrier session.
type Factory func(ctx context.Context, cfg Config) (Session, error)

var registry = make(map[string]Factory) //nolint:gochecknoglobals // package-level state intentional

// Register adds a carrier factory to the registry.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// New creates a carrier session by name.
func New(ctx context.Context, name string, cfg Config) (Session, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, ErrCarrierNotFound
	}
	return factory(ctx, cfg)
}

// Available returns a list of registered carriers.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
