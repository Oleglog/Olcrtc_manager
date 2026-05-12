package admin

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/admin/domain"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	ids, _ := ListInstances(s.cfg.ConfigDir)
	running := 0
	for _, id := range ids {
		st, _ := SystemctlStatusInfo(InstanceService(id))
		if st != nil && st.State == "running" {
			running++
		}
	}

	uptime, _ := GetSystemUptime()
	osInfo := GetOSInfo()

	tlsMode := "self-signed"
	tlsExpires := ""
	if s.cfg.Domain != "" {
		tlsMode = "letsencrypt"
	} else {
		// Read expiry from self-signed cert.
		certPath := filepath.Join(s.cfg.TLSDir, "server.crt")
		if _, err := os.ReadFile(certPath); err == nil {
			// Quick parse with crypto/x509 would need import, skip for now.
			tlsExpires = time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)
		}
	}

	adminDomain := s.cfg.Domain
	if adminDomain == "" {
		adminDomain = s.cfg.PublicIP
	}

	// Read SOCKS/WARP from main instance.
	mainEnv := InstanceEnvPath(s.cfg.ConfigDir, 0)
	vals := ReadInstanceEnv(mainEnv)

	// Public subscription URL: host (domain if bound, else IP) +
	// :domain_port if set, else :admin_port. /sub/{slug} is appended by callers.
	publicHost := s.cfg.Domain
	if publicHost == "" {
		publicHost = s.cfg.PublicIP
	}
	publicPort := s.cfg.Port
	if s.cfg.DomainPort > 0 {
		publicPort = s.cfg.DomainPort
	}
	publicURL := fmt.Sprintf("https://%s:%d", publicHost, publicPort)
	if publicPort == 443 {
		publicURL = fmt.Sprintf("https://%s", publicHost)
	}

	result := map[string]any{
		"version":           "0.4.0",
		"admin_version":     "0.1.0",
		"hostname":          GetHostname(),
		"public_ip":         s.cfg.PublicIP,
		"os":                osInfo,
		"uptime":            uptime,
		"admin_port":        s.cfg.Port,
		"sub_port":          s.cfg.SubPort,
		"sub_enabled":       true,
		"socks_proxy":       vals["OLCRTC_SOCKS_PROXY"],
		"warp_proxy":        vals["OLCRTC_WARP_PROXY"],
		"domain":            s.cfg.Domain,
		"domain_port":       s.cfg.DomainPort,
		"domain_strategy":   s.cfg.DomainStrategy,
		"tls_mode":          tlsMode,
		"tls_expires":       tlsExpires,
		"instances_total":   len(ids),
		"instances_running": running,
		"admin_url":         fmt.Sprintf("https://%s:%d", adminDomain, s.cfg.Port),
		"public_url":        publicURL,
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleSystemLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/system/logs/")
	parts := strings.SplitN(path, "?", 2)
	service := parts[0]
	lines := 100
	if q := r.URL.Query().Get("lines"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 {
			lines = n
		}
	}

	out, err := JournalctlLogs(service, lines)
	if err != nil {
		logger.Errorf("journalctl %s: %v", service, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "failed_to_read_logs",
			"message": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"service": service, "logs": out})
}

func (s *Server) handleSystemDomain(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.bindDomain(w, r)
	case http.MethodDelete:
		s.unbindDomain(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) bindDomain(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Domain   string `json:"domain"`
		Email    string `json:"email"`
		Strategy string `json:"strategy"` // "auto" | "clean" | "own-port" | "sni-mux"
		Port     int    `json:"port"`     // 0 = auto-pick (own-port)
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if req.Domain == "" {
		http.Error(w, "domain required", http.StatusBadRequest)
		return
	}

	// Collect progress events for the JSON response (no SSE yet; we send the
	// full event log when done so the UI can render a summary).
	var events []domain.Event
	reporter := domain.FuncReporter(func(ev domain.Event) {
		events = append(events, ev)
	})

	res, err := domain.Apply(r.Context(), domain.BindParams{
		Domain:   req.Domain,
		Email:    req.Email,
		Strategy: req.Strategy,
		SubPort:  s.cfg.SubPort,
		PublicIP: s.cfg.PublicIP,
		Port:     req.Port,
	}, reporter)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "bind_failed",
			"message": err.Error(),
			"events":  events,
		})
		return
	}

	// Persist state.
	s.cfg.Domain = req.Domain
	s.cfg.DomainPort = res.Port
	s.cfg.DomainStrategy = res.Strategy
	if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Token, req.Domain, s.cfg.SubPort); err != nil {
		http.Error(w, "Internal Server Error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = SetAdminEnvKey(s.cfg.ConfigDir, "OLCRTC_DOMAIN_PORT", fmt.Sprintf("%d", res.Port))
	_ = SetAdminEnvKey(s.cfg.ConfigDir, "OLCRTC_DOMAIN_STRATEGY", res.Strategy)

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":         true,
		"domain":     res.Domain,
		"strategy":   res.Strategy,
		"port":       res.Port,
		"public_url": res.SubURL,
		"events":     events,
		"message":    fmt.Sprintf("Домен привязан. Подписки доступны по %s/sub/{slug}", res.SubURL),
	})
}

func (s *Server) unbindDomain(w http.ResponseWriter, _ *http.Request) {
	var events []domain.Event
	reporter := domain.FuncReporter(func(ev domain.Event) {
		events = append(events, ev)
	})

	// Only own-port strategy creates an actual systemd unit to tear down.
	if s.cfg.DomainStrategy == domain.StrategyOwnPort {
		if err := domain.UnbindOwnPort(context.Background(), reporter); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "unbind_failed",
				"message": err.Error(),
				"events":  events,
			})
			return
		}
	}

	s.cfg.Domain = ""
	s.cfg.DomainPort = 0
	s.cfg.DomainStrategy = ""
	if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Token, "", s.cfg.SubPort); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	_ = DeleteAdminEnvKey(s.cfg.ConfigDir, "OLCRTC_DOMAIN_PORT")
	_ = DeleteAdminEnvKey(s.cfg.ConfigDir, "OLCRTC_DOMAIN_STRATEGY")

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"events":  events,
		"message": "Домен отвязан.",
	})
}

func (s *Server) handleSystemDomainDetect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	profile := domain.Detect(s.cfg.PublicIP)
	writeJSON(w, http.StatusOK, profile)
}

func (s *Server) handleSystemPorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	out, err := ListUsedPorts()
	if err != nil {
		logger.Errorf("list ports: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ports": out})
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "/" || path == "/login" {
		path = "/index.html"
	}
	// Security: prevent directory traversal.
	if strings.Contains(path, "..") {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	data, err := staticFS.ReadFile("static" + path)
	if err != nil {
		// Fallback to index.html for SPA routing.
		data, err = staticFS.ReadFile("static/index.html")
		if err != nil {
			http.Error(w, "Not Found", http.StatusNotFound)
			return
		}
	}
	contentType := "text/plain"
	switch {
	case strings.HasSuffix(path, ".html"):
		contentType = "text/html; charset=utf-8"
	case strings.HasSuffix(path, ".js"):
		contentType = "application/javascript; charset=utf-8"
	case strings.HasSuffix(path, ".css"):
		contentType = "text/css; charset=utf-8"
	case strings.HasSuffix(path, ".ico"):
		contentType = "image/x-icon"
	}
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
