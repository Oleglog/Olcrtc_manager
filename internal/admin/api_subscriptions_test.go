package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// After 1.9.69 the admin public /sub/{slug}/open no longer renders the
// deep-link interstitial itself — it proxies the embedded subscription
// server, which owns the HTML interstitial (with mirror params) so there is
// one /open path server-wide. With no subscription backend started, the
// proxy returns 503 (writeSubscriptionUnavailable). This assert just guards
// that the /open path is proxied rather than handled inline, i.e. the
// deprecated admin-side render is gone.

func TestPublicSubOpenProxiedToSubscriptionServer(t *testing.T) {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"https://myolcrtc.mooo.com/sub/example/open?name=Example",
		nil,
	)

	(&Server{}).handlePublicSub(recorder, request)

	// No backend running -> proxy reports the subscription service unavailable,
	// not 200 with an inline-rendered page (the old behaviour we removed).
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, body = %s; want 503 (proxied, not inline-rendered)",
			recorder.Code, recorder.Body.String())
	}
}
