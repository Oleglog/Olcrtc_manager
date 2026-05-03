package carrier

import (
	"context"
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/provider"
	"github.com/pion/webrtc/v4"
)

// ByteStream is a carrier capability for bidirectional byte transport.
type ByteStream interface {
	Connect(ctx context.Context) error
	Send(data []byte) error
	Close() error
	SetReconnectCallback(cb func())
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
}

// VideoTrack is a carrier capability for bidirectional video transport.
type VideoTrack interface {
	Connect(ctx context.Context) error
	Close() error
	SetReconnectCallback(cb func())
	SetShouldReconnect(fn func() bool)
	SetEndedCallback(cb func(string))
	WatchConnection(ctx context.Context)
	CanSend() bool
	AddTrack(track webrtc.TrackLocal) error
	SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver))
}

type videoTrackProvider interface {
	provider.Provider
	provider.VideoTrackCapable
}

type legacySession struct {
	provider provider.Provider
}

// Capabilities reports the transport primitives supported by the legacy carrier.
func (s *legacySession) Capabilities() Capabilities {
	caps := Capabilities{ByteStream: true}
	_, caps.VideoTrack = s.provider.(videoTrackProvider)
	return caps
}

// OpenByteStream adapts the legacy provider to a generic byte stream capability.
func (s *legacySession) OpenByteStream() (ByteStream, error) {
	return &legacyByteStream{provider: s.provider}, nil
}

// OpenVideoTrack adapts a legacy provider to the generic video track capability.
func (s *legacySession) OpenVideoTrack() (VideoTrack, error) {
	vtp, ok := s.provider.(videoTrackProvider)
	if !ok {
		return nil, ErrVideoTrackUnsupported
	}
	return &legacyVideoTrack{provider: vtp}, nil
}

type legacyByteStream struct {
	provider provider.Provider
}

func (p *legacyByteStream) Connect(ctx context.Context) error {
	if err := p.provider.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}
func (p *legacyByteStream) Send(data []byte) error {
	if err := p.provider.Send(data); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}
func (p *legacyByteStream) Close() error {
	if err := p.provider.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

func (p *legacyByteStream) SetReconnectCallback(cb func()) {
	p.provider.SetReconnectCallback(func(_ *webrtc.DataChannel) {
		if cb != nil {
			cb()
		}
	})
}

func (p *legacyByteStream) SetShouldReconnect(fn func() bool) { p.provider.SetShouldReconnect(fn) }
func (p *legacyByteStream) SetEndedCallback(cb func(string))  { p.provider.SetEndedCallback(cb) }
func (p *legacyByteStream) WatchConnection(ctx context.Context) {
	p.provider.WatchConnection(ctx)
}
func (p *legacyByteStream) CanSend() bool { return p.provider.CanSend() }

type legacyVideoTrack struct {
	provider videoTrackProvider
}

func (v *legacyVideoTrack) Connect(ctx context.Context) error {
	if err := v.provider.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}
func (v *legacyVideoTrack) Close() error {
	if err := v.provider.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}
func (v *legacyVideoTrack) SetShouldReconnect(fn func() bool) { v.provider.SetShouldReconnect(fn) }
func (v *legacyVideoTrack) SetEndedCallback(cb func(string))  { v.provider.SetEndedCallback(cb) }
func (v *legacyVideoTrack) WatchConnection(ctx context.Context) {
	v.provider.WatchConnection(ctx)
}
func (v *legacyVideoTrack) CanSend() bool { return v.provider.CanSend() }
func (v *legacyVideoTrack) AddTrack(track webrtc.TrackLocal) error {
	if err := v.provider.AddVideoTrack(track); err != nil {
		return fmt.Errorf("add track: %w", err)
	}
	return nil
}
func (v *legacyVideoTrack) SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	v.provider.SetVideoTrackHandler(cb)
}
func (v *legacyVideoTrack) SetReconnectCallback(cb func()) {
	v.provider.SetReconnectCallback(func(_ *webrtc.DataChannel) {
		if cb != nil {
			cb()
		}
	})
}
