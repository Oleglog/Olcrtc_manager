package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/server"
)

type peerStatusDocument struct {
	ActivePeers       int        `json:"active_peers"`
	OldestConnectedAt *time.Time `json:"oldest_connected_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func newPeerStatusPublisher(path string) server.PeerStatusFunc {
	if path == "" {
		return nil
	}
	return func(status server.PeerStatus) {
		if err := writePeerStatus(path, status, time.Now()); err != nil {
			logger.Errorf("write peer status: %v", err)
		}
	}
}

func writePeerStatus(path string, status server.PeerStatus, updatedAt time.Time) error {
	var oldest *time.Time
	if !status.OldestConnectedAt.IsZero() {
		value := status.OldestConnectedAt.UTC()
		oldest = &value
	}
	document := peerStatusDocument{
		ActivePeers:       status.ActivePeers,
		OldestConnectedAt: oldest,
		UpdatedAt:          updatedAt.UTC(),
	}
	data, err := json.Marshal(document)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, 0644)
}
