package mirror

import (
	"context"
	"net/http"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDeleteRemovesMirrorWhilePublishingDisabled(t *testing.T) {
	manager := New(Config{
		Enabled:    false,
		Provider:   "yandex_disk",
		OAuthToken: "secret",
		BasePath:   "/custom/subscriptions",
	})
	manager.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodDelete {
			t.Fatalf("method = %s, want DELETE", req.Method)
		}
		if got := req.URL.Query().Get("path"); got != "/custom/subscriptions/example.json" {
			t.Fatalf("path = %q", got)
		}
		if got := req.URL.Query().Get("permanently"); got != "true" {
			t.Fatalf("permanently = %q", got)
		}
		if got := req.Header.Get("Authorization"); got != "OAuth secret" {
			t.Fatalf("authorization = %q", got)
		}
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})}

	if err := manager.Delete(context.Background(), "example"); err != nil {
		t.Fatal(err)
	}
}
