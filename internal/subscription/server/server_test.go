package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestPublicSubscriptionDisablesCachesAndReturnsCurrentProfiles(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	if _, err := st.CreateSubscription("example", "Example"); err != nil {
		t.Fatal(err)
	}
	profiles := []string{
		"olcrtc://wbstream@r/room-one?k=one&c=client-one",
		"olcrtc://wbstream@r/room-two?k=two&c=client-two#old-name",
		"olcrtc://telemost@r/room-two?k=two&c=client-two",
	}
	for _, profile := range profiles {
		if _, err := st.AddInstance("example", profile); err != nil {
			t.Fatal(err)
		}
	}

	recorder := httptest.NewRecorder()
	New(st, 0, "").handleSub(recorder, httptest.NewRequest(http.MethodGet, "/sub/example", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET subscription status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Cache-Control"); got != "no-store, max-age=0" {
		t.Fatalf("Cache-Control = %q", got)
	}
	want := []string{
		"olcrtc://wbstream@r/room-one?k=one&c=client-one&core=legacy#wbstream_Example_1",
		"olcrtc://wbstream@r/room-two?k=two&c=client-two&core=legacy#wbstream_Example_2",
		"olcrtc://telemost@r/room-two?k=two&c=client-two&core=legacy#telemost_Example",
	}
	if got := recorder.Body.String(); got != strings.Join(want, "\n")+"\n" {
		t.Fatalf("subscription body = %q", got)
	}
}

func TestNameSubscriptionURIsSanitizesNameAndKeepsInvalidEntries(t *testing.T) {
	profiles := []string{
		"olcrtc://wbstream@r/room?k=one#old",
		"not-a-uri",
	}
	want := []string{
		"olcrtc://wbstream@r/room?k=one#wbstream_%D0%9C%D0%BE%D1%8F_%D1%81%D0%B5%D0%BC%D1%8C%D1%8F",
		"not-a-uri",
	}
	got := nameSubscriptionURIs(" Моя семья! ", profiles)
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("named URI %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestStripAuthTokenParams(t *testing.T) {
	tests := []struct{ in, want string }{
		{"olcrtc://wbstream@r/room?k=one&a=secret#main", "olcrtc://wbstream@r/room?k=one#main"},
		{"olcrtc://wbstream@room/r?key=x&auth_token=y#frag", "olcrtc://wbstream@room/r?key=x#frag"},
		{"olcrtc://wbstream@r/r?auth.token=z&k=one", "olcrtc://wbstream@r/r?k=one"},
		{"olcrtc://wbstream@r/r?a=only", "olcrtc://wbstream@r/r"},
		{"olcrtc://wbstream@r/r?k=one", "olcrtc://wbstream@r/r?k=one"},
		{"olcrtc://wbstream@r/r#frag", "olcrtc://wbstream@r/r#frag"},
		{"olcrtc://wbstream@r/r?k=one&auth_token=t&a=x&c=cli", "olcrtc://wbstream@r/r?k=one&c=cli"},
	}
	for _, tt := range tests {
		if got := stripAuthTokenParams(tt.in); got != tt.want {
			t.Errorf("stripAuthTokenParams(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPublicSubscriptionOpenServesClientDeepLink(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := st.CreateSubscription("example", "Example subscription"); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "https://myolcrtc.mooo.com/sub/example/open", nil)
	recorder := httptest.NewRecorder()
	New(st, 0, "").handleSub(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET subscription open status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if ct := recorder.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	body := recorder.Body.String()
	// The subscription source URL is carried URL-encoded inside the deep link
	// query (url.Values.Encode), so assert the encoded form plus the deep-link
	// scheme rather than the plain URL, which never appears verbatim.
	encodedSource := url.QueryEscape("https://myolcrtc.mooo.com/sub/example")
	if !strings.Contains(body, "olcrtc://subscription?") {
		t.Fatalf("body does not embed olcrtc deep link: %q", body)
	}
	if !strings.Contains(body, "url="+encodedSource) {
		t.Fatalf("body does not embed encoded subscription source URL")
	}
	if !strings.Contains(body, "name="+url.QueryEscape("Example subscription")) {
		t.Fatalf("body does not embed encoded subscription name")
	}
	if !strings.Contains(body, "Открыть в приложении") {
		t.Fatalf("body does not contain the open button")
	}
	// Both __LINK__ placeholders must be filled (href + JS); a leftover
	// placeholder means the auto-open script would set link to the literal.
	if strings.Contains(body, "__LINK__") {
		t.Fatalf("body still contains unfilled __LINK__ placeholder: %q", body)
	}
}

func TestPublicSubscriptionOpenEmbedsYandexMirror(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "subscriptions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()
	if _, err := st.CreateSubscription("example", "Example subscription"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpsertMirror("example", "yandex_disk",
		"https://disk.yandex.ru/d/example", "mirror-key-43chars__________________"); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "https://myolcrtc.mooo.com/sub/example/open", nil)
	recorder := httptest.NewRecorder()
	New(st, 0, "").handleSub(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	// The /open deep link must carry the Yandex mirror so the client can reach
	// the subscription even when the primary server is down or allow-listed.
	if !strings.Contains(body, "mirror_type=yandex_disk") {
		t.Fatalf("body does not embed mirror_type: %q", body)
	}
	if !strings.Contains(body, "mirror_url="+url.QueryEscape("https://disk.yandex.ru/d/example")) {
		t.Fatalf("body does not embed encoded mirror_url: %q", body)
	}
	if !strings.Contains(body, "mirror_key=mirror-key-43chars__________________") {
		t.Fatalf("body does not embed mirror_key: %q", body)
	}
}
