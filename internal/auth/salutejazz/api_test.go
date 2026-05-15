package salutejazz

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
)

func withJazzAPIServer(t *testing.T, h http.Handler) {
	t.Helper()
	old := apiBase
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		apiBase = old
		srv.Close()
	})
	apiBase = srv.URL
}

func TestCreateMeetingAndPreconnect(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /room/create-meeting", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Jazz-Authtype") != authTypeAnonymous {
			t.Fatalf("missing auth header: %v", r.Header)
		}
		_ = json.NewEncoder(w).Encode(createResponse{RoomID: "room-1", Password: "pass"}) //nolint:gosec
	})
	mux.HandleFunc("POST /room/room-1/preconnect", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{connectorURLKey: testConnector})
	})

	withJazzAPIServer(t, mux)

	headers := map[string]string{
		headerAuthType: authTypeAnonymous,
		"Content-Type": "application/json",
	}
	created, err := createMeeting(context.Background(), headers)
	if err != nil {
		t.Fatalf("createMeeting() error = %v", err)
	}
	if created.RoomID != "room-1" || created.Password != "pass" {
		t.Fatalf("createMeeting() = %+v", created)
	}

	connector, err := preconnect(context.Background(), "room-1", "pass", headers)
	if err != nil {
		t.Fatalf("preconnect() error = %v", err)
	}
	if connector != testConnector {
		t.Fatalf("preconnect() = %q", connector)
	}
}

const (
	testRoomID      = "new-room"
	testPassword    = "new-pass"
	testConnector   = "wss://connector"
	connectorURLKey = "connectorUrl"
)

func TestCreateRoomAndJoinRoom(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /room/create-meeting", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(createResponse{RoomID: testRoomID, Password: testPassword}) //nolint:gosec
	})
	mux.HandleFunc("POST /room/{id}/preconnect", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{connectorURLKey: testConnector})
	})

	withJazzAPIServer(t, mux)

	room, err := createRoom(context.Background())
	if err != nil {
		t.Fatalf("createRoom() error = %v", err)
	}
	if room.RoomID != testRoomID || room.Password != testPassword ||
		room.ConnectorURL != testConnector {
		t.Fatalf("createRoom() = %+v", room)
	}

	room, err = joinRoom(context.Background(), "existing", "secret")
	if err != nil {
		t.Fatalf("joinRoom() error = %v", err)
	}
	if room.RoomID != "existing" || room.Password != "secret" || room.ConnectorURL != testConnector {
		t.Fatalf("joinRoom() = %+v", room)
	}
}

func TestJazzAPIErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/room/create-meeting", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusTeapot)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusInternalServerError)
	})

	withJazzAPIServer(t, mux)

	if _, err := createMeeting(context.Background(), nil); !errors.Is(err, errCreateRoomFailed) {
		t.Fatalf("createMeeting() error = %v, want %v", err, errCreateRoomFailed)
	}
	if _, err := preconnect(context.Background(), "room", "pass", nil); !errors.Is(err, errPreconnectFailed) {
		t.Fatalf("preconnect() error = %v, want %v", err, errPreconnectFailed)
	}
}

func TestJazzIssue(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /room/create-meeting", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(createResponse{RoomID: testRoomID, Password: testPassword}) //nolint:gosec
	})
	mux.HandleFunc("POST /room/{id}/preconnect", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{connectorURLKey: testConnector})
	})

	withJazzAPIServer(t, mux)

	p := Provider{}
	creds, err := p.Issue(context.Background(), auth.Config{
		RoomURL: "any",
		Name:    "peer",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if creds.URL != testConnector {
		t.Fatalf("creds.URL = %q", creds.URL)
	}
	if creds.Token != testRoomID {
		t.Fatalf("creds.Token = %q", creds.Token)
	}
	if creds.Extra["password"] != testPassword {
		t.Fatalf("creds.Extra[password] = %q", creds.Extra["password"])
	}
}
