package wbstream

import (
	"context"
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
)

// Provider produces LiveKit credentials for the WB Stream service.
type Provider struct{}

// Engine reports which engine consumes credentials from this auth provider.
func (Provider) Engine() string { return "livekit" }

// DefaultServiceURL returns the WB Stream service URL.
func (Provider) DefaultServiceURL() string { return "https://stream.wb.ru" }

// Issue runs the WB Stream auth flow and returns LiveKit credentials.
//
// If cfg.RoomURL is empty or "any", a fresh room is created on the fly —
// keeping the behaviour the legacy wbstream provider had.
func (Provider) Issue(ctx context.Context, cfg auth.Config) (auth.Credentials, error) {
	accessToken, err := registerGuest(ctx, cfg.Name)
	if err != nil {
		return auth.Credentials{}, fmt.Errorf("register guest: %w", err)
	}

	roomID := cfg.RoomURL
	if roomID == "" || roomID == "any" {
		roomID, err = createRoom(ctx, accessToken)
		if err != nil {
			return auth.Credentials{}, fmt.Errorf("create room: %w", err)
		}
	}

	if err := joinRoom(ctx, accessToken, roomID); err != nil {
		return auth.Credentials{}, fmt.Errorf("join room: %w", err)
	}

	tok, err := getToken(ctx, accessToken, roomID, cfg.Name)
	if err != nil {
		return auth.Credentials{}, fmt.Errorf("get token: %w", err)
	}

	url := tok.ServerURL
	if url == "" {
		url = defaultWSURL
	}

	return auth.Credentials{
		URL:   url,
		Token: tok.RoomToken,
		Extra: map[string]string{"roomID": roomID},
	}, nil
}

// CreateRoom registers a temporary guest and creates a WB Stream room.
// Used by gen mode.
func (Provider) CreateRoom(ctx context.Context, cfg auth.Config) (string, error) {
	accessToken, err := registerGuest(ctx, cfg.Name)
	if err != nil {
		return "", fmt.Errorf("register guest: %w", err)
	}
	roomID, err := createRoom(ctx, accessToken)
	if err != nil {
		return "", fmt.Errorf("create room: %w", err)
	}
	return roomID, nil
}

func init() { //nolint:gochecknoinits // auth registration is the canonical Go pattern for plugins
	auth.Register("wbstream", Provider{})
}
