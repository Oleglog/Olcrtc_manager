package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	subserver "github.com/openlibrecommunity/olcrtc/internal/subscription/server"
)

func (s *Server) handleSubs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.proxySubRequest(w, r, "/api/subscriptions")
	case http.MethodPost:
		s.proxySubRequest(w, r, "/api/subscriptions")
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSubsSlug(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/subs")
	target := "/api/subscriptions" + path
	s.proxySubRequest(w, r, target)
}

func (s *Server) handlePublicSub(w http.ResponseWriter, r *http.Request) {
	if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/open") {
		s.handlePublicSubOpen(w, r)
		return
	}
	// Proxy /sub/{slug} to subscription server.
	target := r.URL.Path
	if r.URL.RawQuery != "" {
		target = target + "?" + r.URL.RawQuery
	}
	s.proxySubRequestInternal(w, r, target)
}

func (s *Server) handlePublicSubOpen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	normalizedPath := strings.TrimSuffix(r.URL.Path, "/")
	sourcePath := strings.TrimSuffix(normalizedPath, "/open")
	slug := strings.TrimPrefix(sourcePath, "/sub/")
	if sourcePath == normalizedPath || slug == "" || strings.Contains(slug, "/") || r.Host == "" {
		http.NotFound(w, r)
		return
	}
	source := url.URL{Scheme: "https", Host: r.Host, Path: sourcePath}
	query := url.Values{"url": {source.String()}}
	if name := strings.TrimSpace(r.URL.Query().Get("name")); name != "" && len(name) <= 120 {
		query.Set("name", name)
	}
	deepLink := url.URL{Scheme: "olcrtc", Host: "subscription", RawQuery: query.Encode()}
	subserver.OpenSubscriptionLink(w, deepLink.String())
}

func (s *Server) proxySubRequestInternal(w http.ResponseWriter, r *http.Request, target string) {
	body, _ := io.ReadAll(r.Body)
	defer r.Body.Close()

	urlStr, ok := s.subscriptions.endpointURL(target)
	if !ok {
		s.writeSubscriptionUnavailable(w)
		return
	}
	req, err := http.NewRequest(r.Method, urlStr, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	req.Header.Del("Authorization")
	s.doProxy(w, req)
}

func (s *Server) proxySubRequest(w http.ResponseWriter, r *http.Request, targetPath string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	urlStr, ok := s.subscriptions.endpointURL(targetPath)
	if !ok {
		s.writeSubscriptionUnavailable(w)
		return
	}
	req, err := http.NewRequest(r.Method, urlStr, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	req.Header = r.Header.Clone()
	req.Header.Del("Authorization")
	if token := s.subscriptions.token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	s.doProxy(w, req)
}

func (s *Server) writeSubscriptionUnavailable(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error":   "subscription_service_unavailable",
		"message": "Внутренний сервис подписок Admin UI не запущен.",
	})
}

func (s *Server) removeLinkedSubscriptionInstance(id int) ([]string, int64, error) {
	if !s.subscriptions.enabled() {
		return nil, 0, nil
	}
	body, err := json.Marshal(map[string]int{"source_instance_id": id})
	if err != nil {
		return nil, 0, err
	}
	urlStr, ok := s.subscriptions.endpointURL("/api/subscriptions/remove-linked")
	if !ok {
		return nil, 0, fmt.Errorf("subscription backend is unavailable")
	}
	req, err := http.NewRequest(http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := s.subscriptions.token(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, 0, fmt.Errorf("subscription cleanup status %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var result struct {
		Removed int64    `json:"removed_instances"`
		Updated []string `json:"updated_subscriptions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, 0, err
	}
	return result.Updated, result.Removed, nil
}

func (s *Server) doProxy(w http.ResponseWriter, req *http.Request) {
	timeout := 10 * time.Second
	if req.Method == http.MethodDelete || (req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/mirror")) {
		timeout = 90 * time.Second
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "connection refused") {
			s.writeSubscriptionUnavailable(w)
			return
		}
		http.Error(w, "Subscription API unreachable: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
