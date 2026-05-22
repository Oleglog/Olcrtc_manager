package salutejazz

import (
	"context"
	"fmt"
	"strings"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
)

// Provider produces SaluteJazz credentials.
type Provider struct{}

// Engine reports which engine consumes credentials from this auth provider.
func (Provider) Engine() string { return "salutejazz" }

// DefaultServiceURL returns the SaluteJazz service URL.
func (Provider) DefaultServiceURL() string { return "https://bk.salutejazz.ru" }

// Issue runs the SaluteJazz API flow and returns engine credentials.
//
// cfg.RoomURL accepts either an empty value (a new room is created on the
// fly, mirroring the legacy jazz provider) or "<roomID>:<password>".
func (Provider) Issue(ctx context.Context, cfg auth.Config) (auth.Credentials, error) {
	roomRef := strings.TrimSpace(cfg.RoomURL)
	var info *roomInfo
	var err error

	switch roomRef {
	case "", "any", "dummy":
		info, err = createRoom(ctx)
		if err != nil {
			return auth.Credentials{}, fmt.Errorf("create room: %w", err)
		}
	default:
		roomID, password, hasPassword := strings.Cut(roomRef, ":")
		if !hasPassword {
			return auth.Credentials{}, fmt.Errorf("%w: expected <roomID>:<password>", auth.ErrRoomIDRequired)
		}
		info, err = joinRoom(ctx, roomID, password)
		if err != nil {
			return auth.Credentials{}, fmt.Errorf("join room: %w", err)
		}
	}

	return auth.Credentials{
		URL:   info.ConnectorURL,
		Token: info.RoomID,
		Extra: map[string]string{
			"password": info.Password,
			"roomID":   info.RoomID,
		},
	}, nil
}

// CreateRoom creates a new SaluteJazz room and returns "<roomID>:<password>".
//
// Returned format mirrors the legacy gen-mode output so existing
// subscriptions and tooling keep working.
func (Provider) CreateRoom(ctx context.Context, _ auth.Config) (string, error) {
	info, err := createRoom(ctx)
	if err != nil {
		return "", fmt.Errorf("create room: %w", err)
	}
	return info.RoomID + ":" + info.Password, nil
}

func init() { //nolint:gochecknoinits // auth registration is the canonical Go pattern for plugins
	auth.Register("salutejazz", Provider{})
}
