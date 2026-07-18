// Package server implements the HTTP subscription server for olcRTC.
//
// Public endpoint:
//
//	GET /sub/{slug}        — plain-text list of olcrtc:// URIs
//	GET /sub/{slug}/open   — redirect into the Android client
//
// Management endpoints (localhost only):
//
//	GET    /api/subscriptions
//	POST   /api/subscriptions
//	DELETE /api/subscriptions/{slug}
//	GET    /api/subscriptions/{slug}/instances
//	POST   /api/subscriptions/{slug}/instances
//	DELETE /api/subscriptions/{slug}/instances/{id}
//	GET    /api/subscriptions/{slug}/mirror
//	POST   /api/subscriptions/{slug}/mirror
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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/mirror"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/model"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/store"
)

// Server is the subscription HTTP server.
type Server struct {
	store    *store.Store
	port     int
	apiToken string
	mirror   *mirror.Manager
	mirrorMu sync.RWMutex
	deleting map[string]struct{}
	srv      *http.Server
}

// New creates a new subscription server. apiToken may be empty to disable
// bearer-token authentication (localhost-only restriction still applies).
func New(st *store.Store, port int, apiToken string, mirrorManager ...*mirror.Manager) *Server {
	var mm *mirror.Manager
	if len(mirrorManager) > 0 {
		mm = mirrorManager[0]
	}
	return &Server{store: st, port: port, apiToken: apiToken, mirror: mm, deleting: make(map[string]struct{})}
}

// Start starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.port))
	if err != nil {
		return fmt.Errorf("subscription server: %w", err)
	}
	defer func() { _ = listener.Close() }()
	return s.Serve(ctx, listener)
}

// Serve starts the HTTP server on an already-bound listener. Admin UI uses
// this to keep its private subscription backend independent from instances.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("/sub/", s.handleSub)

	// Management API (localhost-gated).
	mux.HandleFunc("/api/subscriptions", s.localhostOnly(s.handleSubscriptions))
	mux.HandleFunc("/api/subscriptions/", s.localhostOnly(s.handleSubscriptionsSlug))
	mux.HandleFunc("/api/subscriptions/export", s.localhostOnly(s.handleExport))
	mux.HandleFunc("/api/subscriptions/import", s.localhostOnly(s.handleImport))
	mux.HandleFunc("/api/subscriptions/refresh-linked", s.localhostOnly(s.handleRefreshLinked))
	mux.HandleFunc("/api/subscriptions/remove-linked", s.localhostOnly(s.handleRemoveLinked))
	mux.HandleFunc("/api/export", s.localhostOnly(s.handleExport))
	mux.HandleFunc("/api/import", s.localhostOnly(s.handleImport))

	s.srv = &http.Server{
		Addr:         listener.Addr().String(),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 90 * time.Second,
	}

	// Graceful shutdown on context cancellation.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.srv.Shutdown(shutCtx)
		case <-done:
		}
	}()

	logger.Infof("subscription server listening on %s", listener.Addr())
	if err := s.srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("subscription server: %w", err)
	}
	return nil
}

// SetMirror replaces mirror settings without restarting the subscription HTTP
// server. Manager instances are immutable after construction.
func (s *Server) SetMirror(manager *mirror.Manager) {
	s.mirrorMu.Lock()
	s.mirror = manager
	s.mirrorMu.Unlock()
}

func (s *Server) currentMirror() *mirror.Manager {
	s.mirrorMu.RLock()
	defer s.mirrorMu.RUnlock()
	return s.mirror
}

func (s *Server) setMirrorDeleting(slug string, deleting bool) {
	s.mirrorMu.Lock()
	defer s.mirrorMu.Unlock()
	if deleting {
		if s.deleting == nil {
			s.deleting = make(map[string]struct{})
		}
		s.deleting[slug] = struct{}{}
	} else {
		delete(s.deleting, slug)
	}
}

func (s *Server) mirrorDeleting(slug string) bool {
	s.mirrorMu.RLock()
	defer s.mirrorMu.RUnlock()
	_, deleting := s.deleting[slug]
	return deleting
}

func (s *Server) mirrorEnabled() bool {
	manager := s.currentMirror()
	return manager != nil && manager.Enabled()
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
	if strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/open") {
		s.handleSubOpen(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	slug := strings.Trim(strings.TrimPrefix(r.URL.Path, "/sub/"), "/ \t\r\n")
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
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	for _, u := range uris {
		_, _ = fmt.Fprintln(w, u)
	}
}

func (s *Server) handleSubOpen(w http.ResponseWriter, r *http.Request) {
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
	subscription, err := s.store.GetSubscriptionBySlug(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("handleSubOpen %s: %v", slug, err)
		return
	}
	source := url.URL{Scheme: "https", Host: r.Host, Path: sourcePath}
	deepLink := url.URL{
		Scheme:   "olcrtc",
		Host:     "subscription",
		RawQuery: url.Values{"url": {source.String()}, "name": {subscription.Name}}.Encode(),
	}
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	http.Redirect(w, r, deepLink.String(), http.StatusFound)
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

	case len(parts) == 2 && parts[1] == "mirror":
		switch r.Method {
		case http.MethodGet:
			s.getMirror(w, r, slug)
		case http.MethodPost:
			s.refreshMirror(w, r, slug)
		default:
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
				s.deleteInstance(w, id, slug)
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
	s.syncMirrorAsync(reqContext(r), sub.Slug)
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
		s.syncMirrorAsync(reqContext(r), slug)
		writeJSON(w, http.StatusOK, map[string]int64{"detached": n})
		return
	}

	s.setMirrorDeleting(slug, true)
	defer s.setMirrorDeleting(slug, false)
	if _, err := s.store.GetMirror(slug); err == nil {
		manager := s.currentMirror()
		if manager == nil {
			http.Error(w, "Mirror cleanup unavailable: Yandex settings are missing", http.StatusBadGateway)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		err = manager.Delete(ctx, slug)
		cancel()
		if err != nil {
			http.Error(w, "Mirror cleanup failed: "+err.Error(), http.StatusBadGateway)
			logger.Errorf("deleteMirror %s: %v", slug, err)
			return
		}
	} else if !errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("getMirror before delete %s: %v", slug, err)
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
		RawURI           string `json:"raw_uri"`
		SourceInstanceID *int   `json:"source_instance_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}

	inst, err := s.store.AddInstanceWithSource(slug, req.RawURI, req.SourceInstanceID)
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
	s.syncMirrorAsync(reqContext(r), slug)
	writeJSON(w, http.StatusCreated, inst)
}

func (s *Server) handleRefreshLinked(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		URIs map[int]string `json:"uris"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}
	slugs, err := s.store.RefreshLinkedInstances(req.URIs)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("refresh linked subscriptions: %v", err)
		return
	}
	mirrorErrors := make(map[string]string)
	for _, slug := range slugs {
		if !s.mirrorEnabled() {
			continue
		}
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		_, syncErr := s.syncMirror(ctx, slug)
		cancel()
		if syncErr != nil {
			mirrorErrors[slug] = syncErr.Error()
			logger.Warnf("refresh linked mirror %s: %v", slug, syncErr)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"updated_subscriptions": slugs,
		"mirror_errors":         mirrorErrors,
	})
}

func (s *Server) handleRemoveLinked(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SourceInstanceID *int `json:"source_instance_id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		http.Error(w, "Bad Request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if req.SourceInstanceID == nil || *req.SourceInstanceID < 0 {
		http.Error(w, "Bad Request: source_instance_id is required", http.StatusBadRequest)
		return
	}
	slugs, removed, err := s.store.DeleteInstancesBySource(*req.SourceInstanceID)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("remove linked instance %d: %v", *req.SourceInstanceID, err)
		return
	}
	for _, slug := range slugs {
		s.syncMirrorAsync(r.Context(), slug)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"removed_instances":     removed,
		"updated_subscriptions": slugs,
	})
}

func (s *Server) deleteInstance(w http.ResponseWriter, id int64, slug string) {
	if err := s.store.DeleteInstance(id); errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	} else if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("deleteInstance %d: %v", id, err)
		return
	}
	s.syncMirrorAsync(context.Background(), slug)
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

func (s *Server) getMirror(w http.ResponseWriter, _ *http.Request, slug string) {
	m, err := s.store.GetMirror(slug)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		logger.Errorf("getMirror %s: %v", slug, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) refreshMirror(w http.ResponseWriter, r *http.Request, slug string) {
	if !s.mirrorEnabled() {
		http.Error(w, "Mirror disabled", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	m, err := s.syncMirror(ctx, slug)
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "Mirror error: "+err.Error(), http.StatusBadGateway)
		logger.Errorf("syncMirror %s: %v", slug, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) syncMirrorAsync(ctx context.Context, slug string) {
	if !s.mirrorEnabled() {
		return
	}
	go func() {
		bg, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if _, err := s.syncMirror(bg, slug); err != nil {
			logger.Warnf("subscription mirror sync failed: slug=%s err=%v", slug, err)
		}
	}()
}

func (s *Server) syncMirror(ctx context.Context, slug string) (*model.Mirror, error) {
	if s.mirrorDeleting(slug) {
		return nil, errors.New("subscription deletion in progress")
	}
	manager := s.currentMirror()
	if manager == nil || !manager.Enabled() {
		return nil, errors.New("mirror manager is disabled")
	}
	key, err := s.store.GetOrCreateMirrorKey(slug, mirror.GenerateKey)
	if err != nil {
		return nil, err
	}
	uris, err := s.store.InstanceURIs(slug)
	if err != nil {
		return nil, err
	}
	publicURL, err := manager.Publish(ctx, slug, uris, key)
	if err != nil {
		return nil, err
	}
	cleanup := func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if cleanupErr := manager.Delete(cleanupCtx, slug); cleanupErr != nil {
			logger.Warnf("cleanup orphaned mirror %s: %v", slug, cleanupErr)
		}
	}
	if s.mirrorDeleting(slug) {
		cleanup()
		return nil, errors.New("subscription deletion in progress")
	}
	stored, err := s.store.UpsertMirror(slug, "yandex_disk", publicURL, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			cleanup()
		}
		return nil, err
	}
	return stored, nil
}

func reqContext(r *http.Request) context.Context {
	if r == nil || r.Context() == nil {
		return context.Background()
	}
	return r.Context()
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
