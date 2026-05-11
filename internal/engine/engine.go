// Package engine defines the wire-level transport engine that connects to a
// remote SFU. An engine is independent of how the room credentials were
// obtained: it accepts a signaling URL and an access token, and exposes the
// byte/video primitives the rest of olcrtc consumes.
//
// Engines model the SFU protocol family (e.g. LiveKit, Goolom). Service-
// specific bits (e.g. WB / Jazz / Telemost API flows) live in the auth
// package, not here.
package engine

import (
	"context"
	"errors"

	"github.com/pion/webrtc/v4"
)

var (
	// ErrEngineNotFound is returned when a requested engine is not registered.
	ErrEngineNotFound = errors.New("engine not found")
	// ErrByteStreamUnsupported is returned when an engine cannot expose a byte stream.
	ErrByteStreamUnsupported = errors.New("engine does not support byte stream")
	// ErrVideoTrackUnsupported is returned when an engine cannot exchange video tracks.
	ErrVideoTrackUnsupported = errors.New("engine does not support video tracks")
)

// Capabilities describes the transport primitives an engine can expose.
type Capabilities struct {
	ByteStream bool
	VideoTrack bool
}

// Config is the runtime input to an engine factory. URL/Token are produced by
// an auth provider (or supplied directly by the caller for "none" auth).
type Config struct {
	URL       string
	Token     string
	Name      string
	OnData    func([]byte)
	DNSServer string
	ProxyAddr string
	ProxyPort int
}

// Session is the engine-level runtime handle. It is shaped to match what
// the upper transport layer expects: send/receive bytes, optional video
// tracks, and lifecycle callbacks.
//
//nolint:interfacebloat // mirrors the historical provider.Provider surface that the rest of olcrtc consumes
type Session interface {
	Connect(ctx context.Context) error
	Send(data []byte) error
	Close() error
	SetReconnectCallback(cb func(*webrtc.DataChannel))
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
	GetSendQueue() chan []byte
	GetBufferedAmount() uint64
	Capabilities() Capabilities
}

// VideoTrackCapable is implemented by engines that can exchange video tracks.
type VideoTrackCapable interface {
	AddVideoTrack(track webrtc.TrackLocal) error
	SetVideoTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver))
}

// Factory creates a new engine session.
type Factory func(ctx context.Context, cfg Config) (Session, error)

var registry = make(map[string]Factory) //nolint:gochecknoglobals // package-level state intentional

// Register adds an engine factory to the registry.
func Register(name string, factory Factory) {
	registry[name] = factory
}

// New creates an engine session by name.
func New(ctx context.Context, name string, cfg Config) (Session, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, ErrEngineNotFound
	}
	return factory(ctx, cfg)
}

// Available returns the list of registered engine names.
func Available() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}
