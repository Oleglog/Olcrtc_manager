package admin

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseJWTExpiry(t *testing.T) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"exp":1893456000}`))
	token := "header." + payload + ".signature"

	got, err := parseJWTExpiry(token)
	if err != nil {
		t.Fatalf("parseJWTExpiry() error = %v", err)
	}
	if got != 1893456000 {
		t.Fatalf("parseJWTExpiry() = %d, want 1893456000", got)
	}

	for _, invalid := range []string{"", "not-a-jwt", "header.invalid.signature", "header.e30.signature"} {
		if _, err := parseJWTExpiry(invalid); err == nil {
			t.Errorf("parseJWTExpiry(%q) unexpectedly succeeded", invalid)
		}
	}
}

func TestValidateWBProxy(t *testing.T) {
	valid := []string{
		"",
		"http://127.0.0.1:8080",
		"https://proxy.example:8443",
		"socks5://proxy.example:1080",
	}
	for _, value := range valid {
		if err := validateWBProxy(value); err != nil {
			t.Errorf("validateWBProxy(%q) error = %v", value, err)
		}
	}

	invalid := []string{
		"proxy.example:8080",
		"ftp://proxy.example:21",
		"http://user:pass@proxy.example:8080",
		"http://proxy.example:8080?secret=value",
		"http://proxy.example:8080#fragment",
	}
	for _, value := range invalid {
		if err := validateWBProxy(value); err == nil {
			t.Errorf("validateWBProxy(%q) unexpectedly succeeded", value)
		}
	}
}

func TestWBSessionResponseOnlyReturnsCreateToken(t *testing.T) {
	finished := time.Now()
	base := wbAutomationSession{
		ID: "session", Secret: "viewer-secret", Phase: "success", Message: "done",
		StartedAt: finished.Add(-time.Minute), Deadline: finished.Add(time.Minute),
		Token: "bearer-secret", RoomID: "room", FinishedAt: finished,
	}

	create := base
	create.Action = "create"
	if got := wbSessionResponse(&create)["token"]; got != "bearer-secret" {
		t.Fatalf("create token = %v, want bearer-secret", got)
	}

	refresh := base
	refresh.Action = "refresh"
	response := wbSessionResponse(&refresh)
	if _, ok := response["token"]; ok {
		t.Fatal("refresh response exposed the WB bearer token")
	}
	if _, ok := response["viewer_url"]; ok {
		t.Fatal("finished response still contains viewer_url")
	}

	active := base
	active.Action = "create"
	active.Phase = "awaiting_login"
	active.FinishedAt = time.Time{}
	activeResponse := wbSessionResponse(&active)
	viewerURL := fmt.Sprint(activeResponse["viewer_url"])
	if !strings.Contains(viewerURL, "viewer-secret") || strings.Contains(viewerURL, "bearer-secret") {
		t.Fatalf("unexpected viewer URL %q", viewerURL)
	}
}

func TestWBSessionServiceHasWritablePrivateTmp(t *testing.T) {
	data, err := wbAutomationAssets.ReadFile("wbautomation/olcrtc-wb-session.service")
	if err != nil {
		t.Fatal(err)
	}
	unit := string(data)
	if !strings.Contains(unit, "PrivateTmp=true") {
		t.Fatal("WB session service does not provide a private writable /tmp")
	}
	if strings.Contains(unit, "PrivateTmp=false") {
		t.Fatal("WB session service still exposes the read-only host /tmp")
	}
}
