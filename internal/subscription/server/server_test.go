package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/subscription/model"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/store"
)

func TestGetMirrorReturnsStoredMetadataWithoutPublishing(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	if _, err := st.CreateSubscription("example", "Example"); err != nil {
		t.Fatal(err)
	}
	want, err := st.UpsertMirror("example", "yandex_disk", "https://disk.yandex.example/public", "mirror-key")
	if err != nil {
		t.Fatal(err)
	}

	srv := New(st, 0, "") // No mirror manager: GET must only read SQLite.
	req := httptest.NewRequest(http.MethodGet, "/api/subscriptions/example/mirror", nil)
	recorder := httptest.NewRecorder()
	srv.handleSubscriptionsSlug(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET mirror status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var got model.Mirror
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.URL != want.URL || got.Key != want.Key || got.Type != want.Type {
		t.Fatalf("GET mirror = %+v, want URL=%q key=%q type=%q", got, want.URL, want.Key, want.Type)
	}
}
