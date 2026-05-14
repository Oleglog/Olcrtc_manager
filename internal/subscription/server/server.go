// Package server implements the HTTP subscription server for olcRTC.
//
// Public endpoint:
//
//	GET /sub/{slug}        — plain-text list of olcrtc:// URIs
//
// Management endpoints (localhost only):
//
//	GET    /api/subscriptions
//	POST   /api/subscriptions
//	DELETE /api/subscriptions/{slug}
//	GET    /api/subscriptions/{slug}/instances
//	POST   /api/subscriptions/{slug}/instances
//	DELETE /api/subscriptions/{slug}/instances/{id}
//	GET    /api/export
//	POST   /api/import
package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/model"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/store"
)

// Server is the subscription HTTP server.
type Server struct {
	store    *store.Store
	port     int
	apiToken string
	srv      *http.Server
}

// New creates a new subscription server. apiToken may be empty to disable
// bearer-token authentication (localhost-only restriction still applies).
func New(st *store.Store, port int, apiToken string) *Server {
	return &Server{store: st, port: port, apiToken: apiToken}
}

// Start starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("/sub/", s.handleSub)

	// Management API (localhost-gated).
	mux.HandleFunc("/api/subscriptions", s.localhostOnly(s.handleSubscriptions))
	mux.HandleFunc("/api/subscriptions/", s.localhostOnly(s.handleSubscriptionsSlug))
	mux.HandleFunc("/api/subscriptions/export", s.localhostOnly(s.handleExport))
	mux.HandleFunc("/api/subscriptions/import", s.localhostOnly(s.handleImport))
	mux.HandleFunc("/api/export", s.localhostOnly(s.handleExport))
	mux.HandleFunc("/api/import", s.localhostOnly(s.handleImport))

	addr := fmt.Sprintf(":%d", s.port)
	s.srv = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Graceful shutdown on context cancellation.
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutCtx)
	}()

	logger.Infof("subscription server listening on %s", addr)
	if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("subscription server: %w", err)
	}
	return nil
}

// ── Middleware ───────────────────────────────────────────────────────────────

func (s *Server) localhostOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		// Optional bearer token check.
		if s.apiToken != "" {
			auth := r.Header.Get("Authorization")
			if auth != "Bearer "+s.apiToken {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}

// ── Public handler ──────────────────────────────────────────────────────────

func (s *Server) handleSub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := strings.TrimPrefix(r.URL.Path, "/sub/")
	if slug == "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	uris, err := s.store.InstanceURIs(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("handleSub %s: %v", slug, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	for _, u := range uris {
		fmt.Fprintln(w, u)
	}
}

// ── Management handlers ─────────────────────────────────────────────────────

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listSubscriptions(w, r)
	case http.MethodPost:
		s.createSubscription(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleSubscriptionsSlug(w http.ResponseWriter, r *http.Request) {
	// Route: /api/subscriptions/{slug}[/instances[/{id}]]
	path := strings.TrimPrefix(r.URL.Path, "/api/subscriptions/")
	parts := strings.SplitN(path, "/", 3)

	slug := parts[0]
	if slug == "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	switch {
	case len(parts) == 1:
		// /api/subscriptions/{slug}
		if r.Method == http.MethodDelete {
			s.deleteSubscription(w, r, slug)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}

	case len(parts) >= 2 && parts[1] == "instances":
		if len(parts) == 2 || parts[2] == "" {
			// /api/subscriptions/{slug}/instances
			switch r.Method {
			case http.MethodGet:
				s.listInstances(w, slug)
			case http.MethodPost:
				s.addInstance(w, r, slug)
			default:
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			}
		} else {
			// /api/subscriptions/{slug}/instances/{id}
			if r.Method == http.MethodDelete {
				id, err := strconv.ParseInt(parts[2], 10, 64)
				if err != nil {
					http.Error(w, "Bad Request: invalid instance id", http.StatusBadRequest)
					return
				}
				s.deleteInstance(w, id)
			} else {
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			}
		}

	default:
		http.Error(w, "Not Found", http.StatusNotFound)
	}
}

func (s *Server) listSubscriptions(w http.ResponseWriter, _ *http.Request) {
	subs, err := s.store.ListSubscriptions()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("listSubscriptions: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, subs)
}

func (s *Server) createSubscription(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "Bad Request: name is required", http.StatusBadRequest)
		return
	}
	if req.Slug == "" {
		req.Slug = generateSlug(req.Name)
	}

	sub, err := s.store.CreateSubscription(req.Slug, req.Name)
	if errors.Is(err, store.ErrSlugExists) {
		http.Error(w, "Conflict: slug already exists", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("createSubscription: %v", err)
		return
	}
	writeJSON(w, http.StatusCreated, sub)
}

func (s *Server) deleteSubscription(w http.ResponseWriter, r *http.Request, slug string) {
	// ?detach=true  — remove all instances but keep the subscription.
	if r.URL.Query().Get("detach") == "true" {
		n, err := s.store.DetachInstances(slug)
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		} else if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			logger.Errorf("detachInstances %s: %v", slug, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]int64{"detached": n})
		return
	}

	if err := s.store.DeleteSubscription(slug); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("deleteSubscription %s: %v", slug, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) listInstances(w http.ResponseWriter, slug string) {
	insts, err := s.store.ListInstances(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("listInstances %s: %v", slug, err)
		return
	}
	writeJSON(w, http.StatusOK, insts)
}

func (s *Server) addInstance(w http.ResponseWriter, r *http.Request, slug string) {
	var req struct {
		RawURI string `json:"raw_uri"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	inst, err := s.store.AddInstance(slug, req.RawURI)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found: subscription not found", http.StatusNotFound)
		return
	}
	if errors.Is(err, store.ErrInvalidURI) {
		http.Error(w, "Bad Request: URI must start with olcrtc://", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("addInstance %s: %v", slug, err)
		return
	}
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) deleteInstance(w http.ResponseWriter, id int64) {
	if err := s.store.DeleteInstance(id); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("deleteInstance %d: %v", id, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := s.store.Export()
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("export: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var data model.ExportFormat
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&data); err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	overwrite := r.URL.Query().Get("overwrite") == "true"
	created, skipped, err := s.store.Import(&data, overwrite)
	if err != nil {
		http.Error(w, "Internal Server Error: "+err.Error(), http.StatusInternalServerError)
		logger.Errorf("import: %v", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"created": created, "skipped": skipped})
}

func generateSlug(name string) string {
	return randomSlug()
}

func randomSlug() string {
	return randomString(5 + int(randInt(6)))
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		b[i] = letters[idx.Int64()]
	}
	return string(b)
}

func randInt(max int64) int64 {
	n, _ := rand.Int(rand.Reader, big.NewInt(max))
	return n.Int64()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
