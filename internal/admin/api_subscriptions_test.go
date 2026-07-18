package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestPublicSubscriptionOpenRedirectsToClient(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"https://myolcrtc.mooo.com/sub/example/open?name=Example",
		nil,
	)

	(&Server{}).handlePublicSub(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	location, err := url.Parse(recorder.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Scheme != "olcrtc" || location.Host != "subscription" {
		t.Fatalf("location = %q", location.String())
	}
	if got := location.Query().Get("url"); got != "https://myolcrtc.mooo.com/sub/example" {
		t.Fatalf("source URL = %q", got)
	}
	if got := location.Query().Get("name"); got != "Example" {
		t.Fatalf("name = %q", got)
	}
}

func TestPublicSubscriptionOpenRejectsNestedSlug(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://example.com/sub/a/b/open", nil)

	(&Server{}).handlePublicSub(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d", recorder.Code)
	}
}
