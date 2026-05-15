package salutejazz

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
)

// TestIssueRoomWithoutPassword ensures that passing a bare roomID without
// the ":<password>" suffix is rejected with auth.ErrRoomIDRequired and
// includes a hint about the expected format. This is the contract the
// admin UI / Android client rely on to surface a clear error rather than
// hanging the handshake when the user forgot the password (clauses
// bugfix.md 1.4 / 2.4 / 1.9 / 2.9).
func TestIssueRoomWithoutPassword(t *testing.T) {
	p := Provider{}
	_, err := p.Issue(context.Background(), auth.Config{RoomURL: "abc-no-colon"})
	if err == nil {
		t.Fatal("Issue() returned nil error for room without password")
	}
	if !errors.Is(err, auth.ErrRoomIDRequired) {
		t.Fatalf("Issue() error = %v, want wrapped auth.ErrRoomIDRequired", err)
	}
	if !strings.Contains(err.Error(), "expected <roomID>:<password>") {
		t.Fatalf("Issue() error = %q, want hint about <roomID>:<password> format", err)
	}
}
