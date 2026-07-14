package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/subscription/model"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/store"
)

func TestSyncMirrorStopsWhileSubscriptionDeleting(t *testing.T) {
	srv := New(nil, 0, "")
	srv.setMirrorDeleting("example", true)
	if _, err := srv.syncMirror(context.Background(), "example"); err == nil {
		t.Fatal("syncMirror succeeded while subscription deletion was in progress")
	}
}

func TestRemoveLinkedInstancesEndpoint(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := st.CreateSubscription("example", "Example"); err != nil {
		t.Fatal(err)
	}
	sourceID := 7
	if _, err := st.AddInstanceWithSource("example", "olcrtc://wbstream@room/example?key=one#linked", &sourceID); err != nil {
		t.Fatal(err)
	}

	srv := New(st, 0, "")
	req := httptest.NewRequest(http.MethodPost, "/api/subscriptions/remove-linked", strings.NewReader(`{"source_instance_id":7}`))
	recorder := httptest.NewRecorder()
	srv.handleRemoveLinked(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("remove linked status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	instances, err := st.ListInstances("example")
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("linked instances remain: %+v", instances)
	}
}

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
