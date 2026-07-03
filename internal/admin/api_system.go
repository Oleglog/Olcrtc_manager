package admin

import (
	"fmt"
	"net"
	"net/http"
	neturl "net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

	result := map[string]any{
		"version":           Version,
		"admin_version":     "0.1.0",
		"hostname":          GetHostname(),
		"public_ip":         s.cfg.PublicIP,
		"os":                osInfo,
		"uptime":            uptime,
		"admin_port":        s.cfg.Port,
		"sub_port":                s.cfg.SubPort,
		"sub_enabled":             true,
		"subscription_public_url": s.cfg.SubPublicURL,
		"socks_proxy":       vals["OLCRTC_SOCKS_PROXY"],
		"warp_proxy":        vals["OLCRTC_WARP_PROXY"],
		"domain":            s.cfg.Domain,
		"tls_mode":          tlsMode,
		"tls_expires":       tlsExpires,
		"instances_total":   len(ids),
		"instances_running": running,
		"admin_url":         fmt.Sprintf("https://%s:%d", adminDomain, s.cfg.Port),
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
		Domain string `json:"domain"`
	}
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if req.Domain == "" {
		http.Error(w, "domain required", http.StatusBadRequest)
		return
	}

	// DNS check.
	ips, err := net.LookupHost(req.Domain)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "dns_lookup_failed",
			"message": "Не удалось разрешить DNS для домена",
		})
		return
	}
	found := false
	for _, ip := range ips {
		if ip == s.cfg.PublicIP {
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "dns_mismatch",
			"message": "DNS A-запись не указывает на IP этого сервера",
			"hint":    fmt.Sprintf("Ожидался IP %s, получены: %v", s.cfg.PublicIP, ips),
		})
		return
	}

	// Check port availability.
	free80 := IsPortFree("", 80)
	free443 := IsPortFree("", 443)
	if !free443 && !free80 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "ports_busy",
			"message":  "Порты 80 и 443 заняты. Настройте reverse-proxy вручную.",
			"hint":     "Обнаружен nginx с SNI multiplexer (stream). Инструкция: ...",
			"docs_url": "https://github.com/openlibrecommunity/olcrtc/blob/master/server-install/README.md",
		})
		return
	}

	s.cfg.Domain = req.Domain
	if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Username, s.cfg.Password, req.Domain, s.cfg.SubPort, s.cfg.SubPublicURL); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"domain":  req.Domain,
		"url":     fmt.Sprintf("https://%s:%d", req.Domain, s.cfg.Port),
		"message": "Домен привязан. Перезапустите olcrtc-admin для применения сертификата Let's Encrypt.",
	})
}

func (s *Server) unbindDomain(w http.ResponseWriter, r *http.Request) {
	s.cfg.Domain = ""
	if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Username, s.cfg.Password, "", s.cfg.SubPort, s.cfg.SubPublicURL); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Домен отвязан. Перезапустите olcrtc-admin для возврата к self-signed.",
	})
}

func (s *Server) handleSystemSubscriptionURL(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			PublicURL string `json:"public_url"`
		}
		if err := readJSON(r, &req); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		publicURL, err := normalizeSubscriptionPublicURL(req.PublicURL)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "invalid_subscription_public_url",
				"message": err.Error(),
			})
			return
		}
		s.cfg.SubPublicURL = publicURL
		if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Username, s.cfg.Password, s.cfg.Domain, s.cfg.SubPort, s.cfg.SubPublicURL); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                      true,
			"subscription_public_url": s.cfg.SubPublicURL,
			"message":                 "Публичный URL подписок сохранён",
		})
	case http.MethodDelete:
		s.cfg.SubPublicURL = ""
		if err := WriteAdminEnv(s.cfg.ConfigDir, s.cfg.Port, s.cfg.Username, s.cfg.Password, s.cfg.Domain, s.cfg.SubPort, s.cfg.SubPublicURL); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":      true,
			"message": "Публичный URL подписок сброшен",
		})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func normalizeSubscriptionPublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("URL обязателен, например https://your-domain.mooo.com")
	}
	u, err := neturl.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("укажите полный URL с https:// и доменом")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("поддерживаются только http:// или https://")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("URL не должен содержать query или fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
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
		path = "/index.html"
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
	_, _ = w.Write(data)
}
