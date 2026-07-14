package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
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
	// Proxy /sub/{slug} to subscription server.
	target := r.URL.Path
	if r.URL.RawQuery != "" {
		target = target + "?" + r.URL.RawQuery
	}
	s.proxySubRequestInternal(w, r, target)
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
