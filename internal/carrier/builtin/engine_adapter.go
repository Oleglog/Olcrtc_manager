package builtin

import (
	"context"
	"errors"
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/engine"
	"github.com/pion/webrtc/v4"
)

// registerDirect registers a carrier that skips auth entirely — the caller
// supplies the engine name, SFU URL, and access token directly via
// carrier.Config.Engine / carrier.Config.URL / carrier.Config.Token.
func registerDirect(carrierName string) {
	carrier.Register(carrierName, func(ctx context.Context, cfg carrier.Config) (carrier.Session, error) {
		engineName := cfg.Engine
		if engineName == "" {
			engineName = "livekit"
		}
		sess, err := engine.New(ctx, engineName, engine.Config{
			URL:       cfg.URL,
			Token:     cfg.Token,
			Name:      cfg.Name,
			OnData:    cfg.OnData,
			DNSServer: cfg.DNSServer,
			ProxyAddr: cfg.ProxyAddr,
			ProxyPort: cfg.ProxyPort,
		})
		if err != nil {
			return nil, fmt.Errorf("engine new: %w", err)
		}
		return &engineSession{session: sess}, nil
	})
}

// registerEngineAuth registers a carrier name that resolves credentials
// through an auth provider and connects via the engine the auth provider
// reports.
func registerEngineAuth(carrierName string, authProvider auth.Provider) {
	carrier.Register(carrierName, func(ctx context.Context, cfg carrier.Config) (carrier.Session, error) {
		authCfg := auth.Config{
			RoomURL:   cfg.RoomURL,
			Name:      cfg.Name,
			DNSServer: cfg.DNSServer,
			ProxyAddr: cfg.ProxyAddr,
			ProxyPort: cfg.ProxyPort,
		}
		creds, err := authProvider.Issue(ctx, authCfg)
		if err != nil {
			return nil, fmt.Errorf("auth issue: %w", errors.Join(carrier.ErrAuthFailed, err))
		}

		sess, err := engine.New(ctx, authProvider.Engine(), engine.Config{
			URL:       creds.URL,
			Token:     creds.Token,
			Name:      cfg.Name,
			Extra:     creds.Extra,
			OnData:    cfg.OnData,
			DNSServer: cfg.DNSServer,
			ProxyAddr: cfg.ProxyAddr,
			ProxyPort: cfg.ProxyPort,
			Refresh: func(ctx context.Context) (engine.Credentials, error) {
				fresh, err := authProvider.Issue(ctx, authCfg)
				if err != nil {
					return engine.Credentials{}, fmt.Errorf("auth refresh: %w", err)
				}
				return engine.Credentials{URL: fresh.URL, Token: fresh.Token, Extra: fresh.Extra}, nil
			},
		})
		if err != nil {
			return nil, fmt.Errorf("engine new: %w", err)
		}
		return &engineSession{session: sess}, nil
	})
}

type engineSession struct {
	session engine.Session
}

func (s *engineSession) Capabilities() carrier.Capabilities {
	caps := s.session.Capabilities()
	return carrier.Capabilities{ByteStream: caps.ByteStream, VideoTrack: caps.VideoTrack}
}

func (s *engineSession) OpenByteStream() (carrier.ByteStream, error) {
	if !s.session.Capabilities().ByteStream {
		return nil, carrier.ErrByteStreamUnsupported
	}
	return &engineByteStream{session: s.session}, nil
}

func (s *engineSession) OpenVideoTrack() (carrier.VideoTrack, error) {
	vt, ok := s.session.(engine.VideoTrackCapable)
	if !ok {
		return nil, carrier.ErrVideoTrackUnsupported
	}
	return &engineVideoTrack{session: s.session, vt: vt}, nil
}

type engineByteStream struct {
	session engine.Session
}

func (b *engineByteStream) Connect(ctx context.Context) error {
	if err := b.session.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}

func (b *engineByteStream) Send(data []byte) error {
	if err := b.session.Send(data); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}

func (b *engineByteStream) Close() error {
	if err := b.session.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

func (b *engineByteStream) SetReconnectCallback(cb func()) {
	b.session.SetReconnectCallback(func(_ *webrtc.DataChannel) {
		if cb != nil {
			cb()
		}
	})
}

func (b *engineByteStream) SetShouldReconnect(fn func() bool) { b.session.SetShouldReconnect(fn) }
func (b *engineByteStream) SetEndedCallback(cb func(string))  { b.session.SetEndedCallback(cb) }
func (b *engineByteStream) WatchConnection(ctx context.Context) {
	b.session.WatchConnection(ctx)
}
func (b *engineByteStream) CanSend() bool { return b.session.CanSend() }

type engineVideoTrack struct {
	session engine.Session
	vt      engine.VideoTrackCapable
}

func (v *engineVideoTrack) Connect(ctx context.Context) error {
	if err := v.session.Connect(ctx); err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	return nil
}

func (v *engineVideoTrack) Close() error {
	if err := v.session.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return nil
}

func (v *engineVideoTrack) SetReconnectCallback(cb func()) {
	v.session.SetReconnectCallback(func(_ *webrtc.DataChannel) {
		if cb != nil {
			cb()
		}
	})
}

func (v *engineVideoTrack) SetShouldReconnect(fn func() bool) { v.session.SetShouldReconnect(fn) }
func (v *engineVideoTrack) SetEndedCallback(cb func(string))  { v.session.SetEndedCallback(cb) }
func (v *engineVideoTrack) WatchConnection(ctx context.Context) {
	v.session.WatchConnection(ctx)
}
func (v *engineVideoTrack) CanSend() bool { return v.session.CanSend() }

func (v *engineVideoTrack) AddTrack(track webrtc.TrackLocal) error {
	if err := v.vt.AddVideoTrack(track); err != nil {
		return fmt.Errorf("add track: %w", err)
	}
	return nil
}

func (v *engineVideoTrack) SetTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	v.vt.SetVideoTrackHandler(cb)
}
