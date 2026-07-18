package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/server"
)

func TestWritePeerStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime", "status.json")
	oldest := time.Date(2026, time.July, 19, 10, 0, 0, 0, time.UTC)
	updated := oldest.Add(time.Minute)

	if err := writePeerStatus(path, server.PeerStatus{
		ActivePeers:       2,
		OldestConnectedAt: oldest,
	}, updated); err != nil {
		t.Fatalf("writePeerStatus() error = %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document peerStatusDocument
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if document.ActivePeers != 2 || document.OldestConnectedAt == nil || !document.OldestConnectedAt.Equal(oldest) {
		t.Fatalf("document = %+v", document)
	}
	if !document.UpdatedAt.Equal(updated) {
		t.Fatalf("updated_at = %s, want %s", document.UpdatedAt, updated)
	}
}
