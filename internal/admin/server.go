package admin

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

//go:embed static/*
var staticFS embed.FS

// Config holds server configuration.
type Config struct {
	Port      int
	Token     string
	Domain    string
	SubPort   int
	TLSDir    string
	ACMEEmail string
	ConfigDir string
	PublicIP  string
}

// Server is the admin HTTP server.
type Server struct {
	cfg        Config
	mux        *http.ServeMux
	srv        *http.Server
	subProxy   *httputil.ReverseProxy
	mu         sync.RWMutex
	lastBadIPs map[string]time.Time // simple rate-limit memory
}

// NewServer creates a new admin server.
func NewServer(cfg Config) *Server {
	s := &Server{
		cfg:        cfg,
		mux:        http.NewServeMux(),
		lastBadIPs: make(map[string]time.Time),
	}

	// Subscription reverse proxy.
	target := fmt.Sprintf("http://127.0.0.1:%d", cfg.SubPort)
	u, _ := url.Parse(target)
	s.subProxy = httputil.NewSingleHostReverseProxy(u)

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// Static SPA.
	s.mux.HandleFunc("/", s.handleStatic)
	s.mux.HandleFunc("/login", s.handleStatic)

	// API routes.
	s.mux.HandleFunc("/api/auth/login", s.withCORS(s.handleLogin))
	s.mux.HandleFunc("/api/auth/change-token", s.withAuth(s.withCORS(s.handleChangeToken)))

	s.mux.HandleFunc("/api/instances", s.withAuth(s.withCORS(s.handleInstancesList)))
	s.mux.HandleFunc("/api/instances/", s.withAuth(s.withCORS(s.handleInstances)))

	s.mux.HandleFunc("/api/subs", s.withAuth(s.withCORS(s.handleSubs)))
	s.mux.HandleFunc("/api/subs/", s.withAuth(s.withCORS(s.handleSubsSlug)))

	s.mux.HandleFunc("/api/system/status", s.withAuth(s.withCORS(s.handleSystemStatus)))
	s.mux.HandleFunc("/api/system/logs/", s.withAuth(s.withCORS(s.handleSystemLogs)))
	s.mux.HandleFunc("/api/system/domain", s.withAuth(s.withCORS(s.handleSystemDomain)))
	s.mux.HandleFunc("/api/system/sub-domain", s.withAuth(s.withCORS(s.handleSubDomain)))
	s.mux.HandleFunc("/api/system/ports", s.withAuth(s.withCORS(s.handleSystemPorts)))

	// Public subscription endpoint via admin UI.
	s.mux.HandleFunc("/sub/", s.handlePublicSub)
}

// Start starts the HTTPS server and blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	tlsConfig, err := s.buildTLS(ctx)
	if err != nil {
		return fmt.Errorf("tls setup: %w", err)
	}

	addr := fmt.Sprintf(":%d", s.cfg.Port)
	s.srv = &http.Server{
		Addr:         addr,
		Handler:      s.recovery(s.logging(s.mux)),
		TLSConfig:    tlsConfig,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.srv.Shutdown(shutCtx)
	}()

	log.Printf("Admin UI listening on https://%s%s", s.listenAddr(), addr)
	if err := s.srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *Server) listenAddr() string {
	if s.cfg.Domain != "" {
		return s.cfg.Domain
	}
	return s.cfg.PublicIP
}

func (s *Server) recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("panic: %v", rec)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func (s *Server) withCORS(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.RemoteAddr)

		auth := r.Header.Get("Authorization")
		expected := "Bearer " + s.cfg.Token
		if auth != expected {
			if s.isRateLimited(host) {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			s.recordBadAttempt(host)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		// Successful auth: clear any previous bad attempts for this IP.
		s.clearBadAttempt(host)
		next(w, r)
	}
}

func (s *Server) isRateLimited(ip string) bool {
	s.mu.RLock()
	last, ok := s.lastBadIPs[ip]
	s.mu.RUnlock()
	if !ok {
		return false
	}
	return time.Since(last) < 5*time.Minute
}

func (s *Server) recordBadAttempt(ip string) {
	s.mu.Lock()
	s.lastBadIPs[ip] = time.Now()
	s.mu.Unlock()
}

func (s *Server) clearBadAttempt(ip string) {
	s.mu.Lock()
	delete(s.lastBadIPs, ip)
	s.mu.Unlock()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func readJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
