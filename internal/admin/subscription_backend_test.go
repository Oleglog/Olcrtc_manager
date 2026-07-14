package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdminSubscriptionBackendDoesNotNeedMainInstance(t *testing.T) {
	configDir := t.TempDir()
	if err := WriteInstanceEnv(InstanceEnvPath(configDir, 0), map[string]string{
		"OLCRTC_SUB_ENABLED":   "1",
		"OLCRTC_SUB_DB":        filepath.Join(configDir, "subscriptions.db"),
		"OLCRTC_SUB_API_TOKEN": "internal-secret",
	}); err != nil {
		t.Fatal(err)
	}

	s := NewServer(Config{ConfigDir: configDir, SubPort: 2096})
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.subscriptions.start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/subs", strings.NewReader(`{"name":"Independent","slug":"independent"}`))
	create.Header.Set("Authorization", "Basic admin-credentials-must-not-be-forwarded")
	createRecorder := httptest.NewRecorder()
	s.handleSubs(createRecorder, create)
	if createRecorder.Code != http.StatusCreated {
		cancel()
		t.Fatalf("create subscription status = %d, body = %s", createRecorder.Code, createRecorder.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/subs", nil)
	listRecorder := httptest.NewRecorder()
	s.handleSubs(listRecorder, list)
	var subscriptions []struct {
		Slug string `json:"slug"`
	}
	decodeErr := json.Unmarshal(listRecorder.Body.Bytes(), &subscriptions)
	if listRecorder.Code != http.StatusOK || decodeErr != nil || len(subscriptions) != 1 || subscriptions[0].Slug != "independent" {
		cancel()
		t.Fatalf("list subscriptions status = %d, decode error = %v, body = %s", listRecorder.Code, decodeErr, listRecorder.Body.String())
	}

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for s.subscriptions.running() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s.subscriptions.running() {
		t.Fatal("subscription backend did not stop with the Admin context")
	}
}
