package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicSubscriptionOpenServesClientDeepLink(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"https://myolcrtc.mooo.com/sub/example/open?name=Example",
		nil,
	)

	(&Server{}).handlePublicSub(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "olcrtc://subscription?") {
		t.Fatalf("body does not embed olcrtc deep link: %q", body)
	}
	if !strings.Contains(body, "https://myolcrtc.mooo.com/sub/example") {
		t.Fatalf("body does not embed source URL")
	}
	if !strings.Contains(body, "name=Example") {
		t.Fatalf("body does not embed name")
	}
	if !strings.Contains(body, "Открыть в приложении") {
		t.Fatalf("body does not contain the open button")
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
