package admin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadInstanceUsageFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	data := []byte(`{"active_peers":2,"oldest_connected_at":"2026-07-19T10:00:00Z","updated_at":"2026-07-19T10:01:00Z"}`)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	usage, ok := readInstanceUsageFile(path, 7)
	if !ok || usage.ID != 7 || usage.ActivePeers != 2 || !usage.UsageKnown {
		t.Fatalf("usage = %+v, ok = %v", usage, ok)
	}
}

func TestReadInstanceUsageFileRejectsInconsistentStatus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "status.json")
	if err := os.WriteFile(path, []byte(`{"active_peers":1,"oldest_connected_at":null,"updated_at":"2026-07-19T10:01:00Z"}`), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if usage, ok := readInstanceUsageFile(path, 0); ok {
		t.Fatalf("usage = %+v, want invalid", usage)
	}
}
