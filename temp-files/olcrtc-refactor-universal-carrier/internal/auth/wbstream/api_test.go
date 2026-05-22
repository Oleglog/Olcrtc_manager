package wbstream

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
)

const (
	testAccessToken = "access"
	testRoomID      = "room"
	testToken       = "token"
	testPeerName    = "peer"
)

func withWBAPIServer(t *testing.T, h http.Handler) {
	t.Helper()
	old := apiBase
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		apiBase = old
		srv.Close()
	})
	apiBase = srv.URL
}

func TestWBStreamAPIHappyPath(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/api/v1/auth/user/guest-register", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(guestRegisterResponse{AccessToken: testAccessToken}) //nolint:gosec
	})
	mux.HandleFunc("POST /api-room/api/v2/room", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testAccessToken {
			t.Fatalf("room auth = %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createRoomResponse{RoomID: testRoomID})
	})
	mux.HandleFunc("POST /api-room/api/v1/room/"+testRoomID+"/join", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api-room-manager/v2/room/"+testRoomID+"/connection-details",
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("displayName") != testPeerName {
				t.Fatalf("displayName query = %q", r.URL.Query().Get("displayName"))
			}
			_ = json.NewEncoder(w).Encode(tokenResponse{RoomToken: testToken})
		})

	withWBAPIServer(t, mux)

	access, err := registerGuest(context.Background(), testPeerName)
	if err != nil {
		t.Fatalf("registerGuest() error = %v", err)
	}
	if access != testAccessToken {
		t.Fatalf("registerGuest() = %q", access)
	}

	room, err := createRoom(context.Background(), access)
	if err != nil {
		t.Fatalf("createRoom() error = %v", err)
	}
	if room != testRoomID {
		t.Fatalf("createRoom() = %q", room)
	}

	if err := joinRoom(context.Background(), access, room); err != nil {
		t.Fatalf("joinRoom() error = %v", err)
	}
	tok, err := getToken(context.Background(), access, room, testPeerName)
	if err != nil {
		t.Fatalf("getToken() error = %v", err)
	}
	if tok.RoomToken != testToken {
		t.Fatalf("getToken() = %q", tok.RoomToken)
	}
}

func TestWBStreamAPIErrors(t *testing.T) {
	withWBAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusBadGateway)
	}))

	if _, err := registerGuest(context.Background(), testPeerName); !errors.Is(err, errGuestRegister) {
		t.Fatalf("registerGuest() error = %v, want %v", err, errGuestRegister)
	}
	if _, err := createRoom(context.Background(), testAccessToken); !errors.Is(err, errCreateRoom) {
		t.Fatalf("createRoom() error = %v, want %v", err, errCreateRoom)
	}
	if err := joinRoom(context.Background(), testAccessToken, testRoomID); !errors.Is(err, errJoinRoom) {
		t.Fatalf("joinRoom() error = %v, want %v", err, errJoinRoom)
	}
	if _, err := getToken(context.Background(), testAccessToken, testRoomID, testPeerName); !errors.Is(err, errGetToken) {
		t.Fatalf("getToken() error = %v, want %v", err, errGetToken)
	}
}

func TestWBStreamIssue(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /auth/api/v1/auth/user/guest-register", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(guestRegisterResponse{AccessToken: testAccessToken}) //nolint:gosec
	})
	mux.HandleFunc("POST /api-room/api/v2/room", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(createRoomResponse{RoomID: "created"})
	})
	mux.HandleFunc("POST /api-room/api/v1/room/{id}/join", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /api-room-manager/v2/room/{id}/connection-details", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(tokenResponse{RoomToken: testToken})
	})

	withWBAPIServer(t, mux)

	p := Provider{}
	creds, err := p.Issue(context.Background(), auth.Config{
		RoomURL: "any",
		Name:    testPeerName,
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if creds.Token != testToken {
		t.Fatalf("creds.Token = %q", creds.Token)
	}
	if creds.Extra["roomID"] != "created" {
		t.Fatalf("creds.Extra[roomID] = %q", creds.Extra["roomID"])
	}
}
