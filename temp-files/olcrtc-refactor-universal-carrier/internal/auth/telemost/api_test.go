package telemost

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withTelemostAPIServer(t *testing.T, h http.Handler) {
	t.Helper()
	old := apiBase
	srv := httptest.NewServer(h)
	t.Cleanup(func() {
		apiBase = old
		srv.Close()
	})
	apiBase = srv.URL
}

func TestGetConnectionInfo(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /conferences/{id...}", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/conferences/room/id/connection") {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if r.URL.Query().Get("display_name") != "peer" {
			t.Fatalf("display_name query = %q", r.URL.Query().Get("display_name"))
		}
		_ = json.NewEncoder(w).Encode(ConnectionInfo{
			RoomID:      "room",
			PeerID:      "peer-id",
			Credentials: "creds",
		})
	})

	withTelemostAPIServer(t, mux)

	info, err := GetConnectionInfo(context.Background(), "room/id", "peer")
	if err != nil {
		t.Fatalf("GetConnectionInfo() error = %v", err)
	}
	if info.RoomID != "room" || info.PeerID != "peer-id" || info.Credentials != "creds" {
		t.Fatalf("GetConnectionInfo() = %+v", info)
	}
}

func TestGetConnectionInfoErrors(t *testing.T) {
	withTelemostAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusForbidden)
	}))
	if _, err := GetConnectionInfo(context.Background(), "room", "peer"); !errors.Is(err, ErrAPI) {
		t.Fatalf("GetConnectionInfo() error = %v, want %v", err, ErrAPI)
	}

	withTelemostAPIServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{"))
	}))
	if _, err := GetConnectionInfo(context.Background(), "room", "peer"); err == nil {
		t.Fatal("GetConnectionInfo() unexpectedly accepted bad json")
	}
}
