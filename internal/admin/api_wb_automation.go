package admin

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

const (
	wbAssetsDir             = "/usr/local/lib/olcrtc/wb-automation"
	wbInstallDir            = "/opt/olcrtc-wb-automation"
	wbRuntimeDir            = "/run/olcrtc-wb"
	wbProfileDir            = "/var/lib/olcrtc-wb/profile"
	wbProfileMarker         = "/var/lib/olcrtc-wb/profile-used-at"
	wbConfigFile            = "/etc/olcrtc/wb-automation.json"
	wbAccountFile           = "/etc/olcrtc/wb-account.json"
	wbComponentStateFile    = "/run/olcrtc-wb-components-state.json"
	wbSessionService        = "olcrtc-wb-session.service"
	wbVNCAddress            = "127.0.0.1:5907"
	wbSessionInitialTimeout = 15 * time.Minute
	wbSessionExtension      = 15 * time.Minute
	wbProfileTTL            = 7 * 24 * time.Hour
)

//go:embed wbautomation/*
var wbAutomationAssets embed.FS

type wbAutomationManager struct {
	mu      sync.Mutex
	session *wbAutomationSession
}

type wbAutomationSession struct {
	ID         string
	Secret     string
	Action     string
	Phase      string
	Message    string
	Percent    int
	StartedAt  time.Time
	Deadline   time.Time
	Extended   bool
	RoomID     string
	Token      string
	ExpiresAt  int64
	Apply      *wbTokenApplyResult
	FinishedAt time.Time
}

type wbWorkerState struct {
	Phase          string `json:"phase"`
	Message        string `json:"message"`
	Percent        int    `json:"percent"`
	Token          string `json:"token"`
	TokenExpiresAt int64  `json:"token_expires_at"`
	RoomID         string `json:"room_id"`
}

type wbProxyConfig struct {
	Server   string `json:"server"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type wbAccountState struct {
	Token      string `json:"token"`
	ExpiresAt  int64  `json:"expires_at"`
	CapturedAt int64  `json:"captured_at"`
}

type wbTokenApplyResult struct {
	UpdatedInstances     []int    `json:"updated_instances"`
	RestartedInstances   []int    `json:"restarted_instances"`
	UpdatedSubscriptions []string `json:"updated_subscriptions,omitempty"`
}

func newWBAutomationManager() *wbAutomationManager {
	return &wbAutomationManager{}
}

func (s *Server) setupWBAutomationRoutes() {
	s.mux.HandleFunc("/api/wb-automation/components", s.withAuth(s.withCORS(s.handleWBComponents)))
	s.mux.HandleFunc("/api/wb-automation/components/progress", s.withAuth(s.withCORS(s.handleWBComponentsProgress)))
	s.mux.HandleFunc("/api/wb-automation/config", s.withAuth(s.withCORS(s.handleWBConfig)))
	s.mux.HandleFunc("/api/wb-automation/session", s.withAuth(s.withCORS(s.handleWBSession)))
	s.mux.HandleFunc("/api/wb-automation/session/extend", s.withAuth(s.withCORS(s.handleWBSessionExtend)))
	s.mux.HandleFunc("/api/wb-automation/session/cancel", s.withAuth(s.withCORS(s.handleWBSessionCancel)))
	s.mux.HandleFunc("/api/wb-automation/vnc", s.handleWBVNCWebSocket)
	s.mux.HandleFunc("/wb-automation/session", s.handleWBSessionPage)
	s.mux.Handle("/wb-automation/assets/", http.StripPrefix(
		"/wb-automation/assets/", http.FileServer(http.Dir("/usr/share/novnc"))))
}

func (s *Server) handleWBComponents(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, s.wbComponentsStatus())
	case http.MethodPost:
		var req struct {
			Action string `json:"action"`
		}
		if err := readJSON(r, &req); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		if req.Action != "install" && req.Action != "remove" {
			http.Error(w, "action must be install or remove", http.StatusBadRequest)
			return
		}
		if err := s.startWBComponentsJob(req.Action); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"ok": true, "action": req.Action})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) wbComponentsStatus() map[string]any {
	supported := runtime.GOOS == "linux" && runtime.GOARCH == "amd64"
	installed := pathExists(filepath.Join(wbInstallDir, "node_modules", "playwright")) &&
		pathExists(filepath.Join(wbInstallDir, "node", "bin", "node")) &&
		pathExists(filepath.Join(wbInstallDir, "worker.mjs")) &&
		pathExists("/usr/share/novnc/core/rfb.js") && pathExists("/etc/systemd/system/"+wbSessionService)
	status := map[string]any{
		"installed": installed,
		"supported": supported,
		"platform":  "ubuntu/debian x86_64",
	}
	if usedAt, err := readUnixTimestamp(wbProfileMarker); err == nil {
		status["profile_last_used_at"] = usedAt
		status["profile_expires_at"] = usedAt + int64(wbProfileTTL/time.Second)
	}
	if account, err := readWBAccountState(); err == nil {
		if account.Token == "" {
			account = s.firstWBAccountState()
		}
		status["token_expires_at"] = account.ExpiresAt
		status["token_expired"] = account.ExpiresAt > 0 && time.Now().Unix() >= account.ExpiresAt
		status["has_token"] = account.Token != ""
	}
	if proxy, err := readWBProxyConfig(); err == nil {
		status["proxy_server"] = proxy.Server
		status["proxy_username"] = proxy.Username
		status["proxy_has_password"] = proxy.Password != ""
	}
	return status
}

func (s *Server) startWBComponentsJob(action string) error {
	if supported, _ := s.wbComponentsStatus()["supported"].(bool); action == "install" && !supported {
		return errors.New("WB automation requires Ubuntu/Debian x86_64")
	}
	if err := ensureWBAutomationAssets(); err != nil {
		return err
	}
	script := filepath.Join(wbAssetsDir, action+".sh")
	initial := map[string]any{
		"phase":      "queued",
		"message":    "Запуск операции...",
		"percent":    1,
		"updated_at": time.Now().Unix(),
	}
	if err := writeJSONFile(wbComponentStateFile, initial, 0644); err != nil {
		return err
	}
	_, _ = SystemctlRun("stop", "olcrtc-wb-components.service")
	_, _ = SystemctlRun("reset-failed", "olcrtc-wb-components.service")
	cmd := exec.Command("systemd-run", "--no-block", "--collect",
		"--unit=olcrtc-wb-components", "--description=olcRTC WB automation components", "bash", script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start components job: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if action == "remove" {
		s.wbAutomation.mu.Lock()
		s.wbAutomation.session = nil
		s.wbAutomation.mu.Unlock()
	}
	return nil
}

func (s *Server) handleWBComponentsProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	data, err := os.ReadFile(wbComponentStateFile)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"phase": "idle", "percent": 0})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}

func (s *Server) handleWBConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		cfg, _ := readWBProxyConfig()
		writeJSON(w, http.StatusOK, map[string]any{
			"server":       cfg.Server,
			"username":     cfg.Username,
			"has_password": cfg.Password != "",
		})
	case http.MethodPost:
		var req struct {
			Server        string `json:"server"`
			Username      string `json:"username"`
			Password      string `json:"password"`
			ClearPassword bool   `json:"clear_password"`
		}
		if err := readJSON(r, &req); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		cfg, _ := readWBProxyConfig()
		cfg.Server = strings.TrimSpace(req.Server)
		cfg.Username = strings.TrimSpace(req.Username)
		if req.Password != "" {
			cfg.Password = req.Password
		}
		if req.ClearPassword || cfg.Server == "" {
			cfg.Password = ""
		}
		if cfg.Server == "" {
			cfg.Username = ""
		}
		if err := validateWBProxy(cfg.Server); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := writeJSONFile(wbConfigFile, cfg, 0600); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWBSession(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req struct {
			Action string `json:"action"`
		}
		if err := readJSON(r, &req); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		session, err := s.startWBSession(req.Action)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errWBSessionActive) || errors.Is(err, errWBComponentsMissing) {
				status = http.StatusConflict
			}
			http.Error(w, err.Error(), status)
			return
		}
		writeJSON(w, http.StatusAccepted, wbSessionResponse(session))
	case http.MethodGet:
		s.wbAutomation.mu.Lock()
		defer s.wbAutomation.mu.Unlock()
		if s.wbAutomation.session == nil {
			writeJSON(w, http.StatusOK, map[string]any{"phase": "idle"})
			return
		}
		writeJSON(w, http.StatusOK, wbSessionResponse(s.wbAutomation.session))
	case http.MethodDelete:
		if err := s.cancelWBSession(); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

var (
	errWBSessionActive     = errors.New("WB browser session is already active")
	errWBSessionApplying   = errors.New("WB token is being applied")
	errWBComponentsMissing = errors.New("WB automation components are not installed")
)

func (s *Server) startWBSession(action string) (*wbAutomationSession, error) {
	if action != "create" && action != "refresh" {
		return nil, errors.New("action must be create or refresh")
	}
	if installed, _ := s.wbComponentsStatus()["installed"].(bool); !installed {
		return nil, errWBComponentsMissing
	}

	s.wbAutomation.mu.Lock()
	defer s.wbAutomation.mu.Unlock()
	if current := s.wbAutomation.session; current != nil && current.FinishedAt.IsZero() {
		return nil, errWBSessionActive
	}
	_, _ = SystemctlRun("stop", wbSessionService)
	_, _ = SystemctlRun("reset-failed", wbSessionService)
	cleanupWBWorkerFiles()
	if err := prepareWBProfile(); err != nil {
		return nil, err
	}
	if err := os.WriteFile(wbProfileMarker, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600); err != nil {
		return nil, fmt.Errorf("mark WB profile use: %w", err)
	}
	if err := ensureWBRuntimeDir(); err != nil {
		return nil, err
	}

	id, err := randomHex(16)
	if err != nil {
		return nil, err
	}
	secret, err := randomHex(24)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(wbSessionInitialTimeout)
	proxy, _ := readWBProxyConfig()
	job := map[string]any{
		"action":           action,
		"profile_dir":      wbProfileDir,
		"state_file":       filepath.Join(wbRuntimeDir, "state.json"),
		"control_file":     filepath.Join(wbRuntimeDir, "control.json"),
		"deadline_unix":    deadline.Unix(),
		"home_url":         "https://stream.wb.ru",
		"existing_room_id": s.firstWBRoomID(),
		"proxy":            proxy,
	}
	if err := writeWBWorkerJSON(filepath.Join(wbRuntimeDir, "job.json"), job); err != nil {
		return nil, err
	}
	if err := writeWBWorkerJSON(filepath.Join(wbRuntimeDir, "control.json"), map[string]any{
		"deadline_unix": deadline.Unix(),
	}); err != nil {
		return nil, err
	}
	_ = writeWBWorkerJSON(filepath.Join(wbRuntimeDir, "state.json"), map[string]any{
		"phase": "queued", "message": "Запуск Chrome...", "percent": 1,
	})

	if out, err := SystemctlRun("start", wbSessionService); err != nil {
		cleanupWBWorkerFiles()
		return nil, fmt.Errorf("start %s: %w: %s", wbSessionService, err, strings.TrimSpace(out))
	}
	session := &wbAutomationSession{
		ID: id, Secret: secret, Action: action, Phase: "queued", Message: "Запуск Chrome...",
		Percent: 1, StartedAt: time.Now(), Deadline: deadline,
	}
	s.wbAutomation.session = session
	go s.monitorWBSession(id)
	return session, nil
}

func (s *Server) monitorWBSession(id string) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.wbAutomation.mu.Lock()
		session := s.wbAutomation.session
		if session == nil || session.ID != id {
			s.wbAutomation.mu.Unlock()
			return
		}
		if time.Now().After(session.Deadline) {
			session.Phase = "error"
			session.Message = "Время авторизации истекло"
			session.FinishedAt = time.Now()
			s.wbAutomation.mu.Unlock()
			_, _ = SystemctlRun("stop", wbSessionService)
			cleanupWBWorkerFiles()
			return
		}
		var state wbWorkerState
		data, err := os.ReadFile(filepath.Join(wbRuntimeDir, "state.json"))
		if err == nil && json.Unmarshal(data, &state) == nil {
			if state.Phase == "success" {
				session.Phase = "applying"
				session.Message = "Применение данных WB Stream..."
				session.Percent = 95
			} else {
				session.Phase = state.Phase
				session.Message = state.Message
				session.Percent = state.Percent
			}
		}
		if state.Phase != "success" && state.Phase != "error" {
			s.wbAutomation.mu.Unlock()
			continue
		}
		if state.Phase == "error" {
			session.FinishedAt = time.Now()
			s.wbAutomation.mu.Unlock()
			_, _ = SystemctlRun("stop", wbSessionService)
			cleanupWBWorkerFiles()
			return
		}
		session.RoomID = state.RoomID
		session.Token = state.Token
		session.ExpiresAt = state.TokenExpiresAt
		action := session.Action
		s.wbAutomation.mu.Unlock()
		cleanupWBWorkerFiles()

		var apply *wbTokenApplyResult
		if action == "refresh" {
			result, applyErr := s.applyWBAccountToken(state.Token)
			if applyErr != nil {
				s.wbAutomation.mu.Lock()
				if current := s.wbAutomation.session; current != nil && current.ID == id {
					current.Phase = "error"
					current.Message = "Токен получен, но не применён: " + applyErr.Error()
					current.FinishedAt = time.Now()
				}
				s.wbAutomation.mu.Unlock()
				return
			}
			apply = result
		}
		_ = os.WriteFile(wbProfileMarker, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600)
		s.wbAutomation.mu.Lock()
		if current := s.wbAutomation.session; current != nil && current.ID == id {
			current.Apply = apply
			current.Phase = "success"
			current.Message = "Данные WB Stream получены"
			current.Percent = 100
			current.FinishedAt = time.Now()
		}
		s.wbAutomation.mu.Unlock()
		return
	}
}

func wbSessionResponse(session *wbAutomationSession) map[string]any {
	response := map[string]any{
		"id": session.ID, "phase": session.Phase, "message": session.Message,
		"percent": session.Percent, "action": session.Action,
		"started_at": session.StartedAt.Unix(), "deadline_at": session.Deadline.Unix(),
		"extended": session.Extended,
	}
	if session.FinishedAt.IsZero() {
		response["viewer_url"] = "/wb-automation/session?token=" + url.QueryEscape(session.Secret)
	}
	if session.Phase == "success" {
		response["room_id"] = session.RoomID
		if session.Action == "create" {
			response["token"] = session.Token
		}
		response["token_expires_at"] = session.ExpiresAt
		response["apply"] = session.Apply
	}
	return response
}

func (s *Server) handleWBSessionExtend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	s.wbAutomation.mu.Lock()
	defer s.wbAutomation.mu.Unlock()
	session := s.wbAutomation.session
	if session == nil || !session.FinishedAt.IsZero() {
		http.Error(w, "no active session", http.StatusConflict)
		return
	}
	if session.Extended {
		http.Error(w, "session was already extended", http.StatusConflict)
		return
	}
	session.Extended = true
	session.Deadline = session.Deadline.Add(wbSessionExtension)
	if err := writeWBWorkerJSON(filepath.Join(wbRuntimeDir, "control.json"), map[string]any{
		"deadline_unix": session.Deadline.Unix(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, wbSessionResponse(session))
}

func (s *Server) handleWBSessionCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.cancelWBSession(); err != nil {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) cancelWBSession() error {
	s.wbAutomation.mu.Lock()
	if session := s.wbAutomation.session; session != nil && session.Phase == "applying" && session.FinishedAt.IsZero() {
		s.wbAutomation.mu.Unlock()
		return errWBSessionApplying
	}
	s.wbAutomation.session = nil
	s.wbAutomation.mu.Unlock()
	_, _ = SystemctlRun("stop", wbSessionService)
	cleanupWBWorkerFiles()
	return nil
}

func (s *Server) handleWBSessionPage(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("token")
	if !s.validWBSessionSecret(secret) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	_, _ = fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>WB Stream login</title><style>html,body,#screen{margin:0;width:100%%;height:100%%;overflow:hidden;background:#111}canvas{width:100%%!important;height:100%%!important;object-fit:contain}</style></head><body><div id="screen"></div><script type="module">import RFB from '/wb-automation/assets/core/rfb.js';const target=document.getElementById('screen');const proto=location.protocol==='https:'?'wss':'ws';function connect(){const rfb=new RFB(target,proto+'://'+location.host+'/api/wb-automation/vnc?token=%s');rfb.scaleViewport=true;rfb.resizeSession=false;rfb.addEventListener('disconnect',()=>setTimeout(connect,1500),{once:true});}connect();</script></body></html>`, url.QueryEscape(secret))
}

func (s *Server) handleWBVNCWebSocket(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("token")
	if !s.validWBSessionSecret(secret) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	upgrader := websocket.Upgrader{CheckOrigin: func(req *http.Request) bool {
		origin := req.Header.Get("Origin")
		if origin == "" {
			return true
		}
		parsed, err := url.Parse(origin)
		return err == nil && parsed.Host == req.Host
	}}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer func() { _ = ws.Close() }()
	tcpConn, err := net.DialTimeout("tcp", wbVNCAddress, 5*time.Second)
	if err != nil {
		_ = ws.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "VNC is starting"))
		return
	}
	defer func() { _ = tcpConn.Close() }()

	errCh := make(chan error, 2)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := tcpConn.Read(buf)
			if n > 0 {
				if writeErr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
					errCh <- writeErr
					return
				}
			}
			if readErr != nil {
				errCh <- readErr
				return
			}
		}
	}()
	go func() {
		for {
			messageType, reader, readErr := ws.NextReader()
			if readErr != nil {
				errCh <- readErr
				return
			}
			if messageType != websocket.BinaryMessage {
				continue
			}
			if _, copyErr := io.Copy(tcpConn, reader); copyErr != nil {
				errCh <- copyErr
				return
			}
		}
	}()
	<-errCh
}

func (s *Server) validWBSessionSecret(secret string) bool {
	if secret == "" {
		return false
	}
	s.wbAutomation.mu.Lock()
	defer s.wbAutomation.mu.Unlock()
	session := s.wbAutomation.session
	return session != nil && session.Secret == secret && session.FinishedAt.IsZero() && time.Now().Before(session.Deadline)
}

func (s *Server) firstWBRoomID() string {
	ids, _ := ListInstances(s.cfg.ConfigDir)
	for _, id := range ids {
		vals := ReadInstanceEnv(InstanceEnvPath(s.cfg.ConfigDir, id))
		carrier := vals["OLCRTC_CARRIER"]
		if carrier == "" {
			carrier = vals["OLCRTC_PROVIDER"]
		}
		if carrier == "wbstream" && vals["OLCRTC_ROOM_ID"] != "" {
			return vals["OLCRTC_ROOM_ID"]
		}
	}
	return ""
}

func (s *Server) firstWBAccountState() wbAccountState {
	ids, _ := ListInstances(s.cfg.ConfigDir)
	for _, id := range ids {
		vals := ReadInstanceEnv(InstanceEnvPath(s.cfg.ConfigDir, id))
		carrier := vals["OLCRTC_CARRIER"]
		if carrier == "" {
			carrier = vals["OLCRTC_PROVIDER"]
		}
		token := strings.TrimSpace(vals["OLCRTC_AUTH_TOKEN"])
		if carrier == "wbstream" && token != "" {
			expiresAt, _ := parseJWTExpiry(token)
			return wbAccountState{Token: token, ExpiresAt: expiresAt}
		}
	}
	return wbAccountState{}
}

func (s *Server) applyWBAccountToken(token string) (*wbTokenApplyResult, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("empty WB token")
	}
	expiresAt, _ := parseJWTExpiry(token)
	account := wbAccountState{Token: token, ExpiresAt: expiresAt, CapturedAt: time.Now().Unix()}
	if err := writeJSONFile(wbAccountFile, account, 0600); err != nil {
		return nil, err
	}

	ids, err := ListInstances(s.cfg.ConfigDir)
	if err != nil {
		return nil, err
	}
	result := &wbTokenApplyResult{}
	linkedURIs := make(map[int]string)
	var running []int
	for _, id := range ids {
		envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
		vals := ReadInstanceEnv(envPath)
		carrier := vals["OLCRTC_CARRIER"]
		if carrier == "" {
			carrier = vals["OLCRTC_PROVIDER"]
		}
		if carrier != "wbstream" {
			continue
		}
		if err := SetEnvValue(envPath, "OLCRTC_AUTH_TOKEN", token); err != nil {
			return nil, fmt.Errorf("update instance %d: %w", id, err)
		}
		vals["OLCRTC_AUTH_TOKEN"] = token
		clientID := s.ensureClientID(envPath, vals["OLCRTC_CLIENT_ID"])
		linkedURIs[id] = s.buildURIWith(vals, clientID, true)
		result.UpdatedInstances = append(result.UpdatedInstances, id)
		if status, _ := SystemctlStatusInfo(InstanceService(id)); status != nil && status.State == "running" {
			running = append(running, id)
		}
	}

	updatedSubs, err := s.refreshLinkedSubscriptionURIs(linkedURIs)
	if err != nil {
		logger.Warnf("WB token applied but subscription refresh failed: %v", err)
	} else {
		result.UpdatedSubscriptions = updatedSubs
	}
	for _, id := range running {
		if err := SystemctlRestart(InstanceService(id)); err != nil {
			return nil, fmt.Errorf("restart instance %d: %w", id, err)
		}
		result.RestartedInstances = append(result.RestartedInstances, id)
		time.Sleep(time.Second)
	}
	return result, nil
}

func (s *Server) refreshLinkedSubscriptionURIs(uris map[int]string) ([]string, error) {
	if len(uris) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{"uris": uris})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/api/subscriptions/refresh-linked", s.cfg.SubPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiToken := GetEnvValue(InstanceEnvPath(s.cfg.ConfigDir, 0), "OLCRTC_SUB_API_TOKEN"); apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+apiToken)
	}
	resp, err := (&http.Client{Timeout: 90 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("subscription refresh status %d: %s", resp.StatusCode, strings.TrimSpace(string(message)))
	}
	var result struct {
		Updated []string `json:"updated_subscriptions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return result.Updated, nil
}

func (s *Server) saveWBAccountToken(token string) {
	token = strings.TrimSpace(token)
	if token == "" {
		return
	}
	expiresAt, _ := parseJWTExpiry(token)
	if err := writeJSONFile(wbAccountFile, wbAccountState{
		Token: token, ExpiresAt: expiresAt, CapturedAt: time.Now().Unix(),
	}, 0600); err != nil {
		logger.Warnf("save WB account metadata: %v", err)
	}
}

func ensureWBAutomationAssets() error {
	if err := os.MkdirAll(wbAssetsDir, 0755); err != nil {
		return err
	}
	entries, err := wbAutomationAssets.ReadDir("wbautomation")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := wbAutomationAssets.ReadFile("wbautomation/" + entry.Name())
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		if strings.HasSuffix(entry.Name(), ".sh") {
			mode = 0755
		}
		if err := os.WriteFile(filepath.Join(wbAssetsDir, entry.Name()), data, mode); err != nil {
			return err
		}
	}
	return nil
}

func prepareWBProfile() error {
	if usedAt, err := readUnixTimestamp(wbProfileMarker); err == nil && time.Since(time.Unix(usedAt, 0)) > wbProfileTTL {
		if err := os.RemoveAll(wbProfileDir); err != nil {
			return err
		}
	}
	cmd := exec.Command("install", "-d", "-m", "0700", "-o", "olcrtc-wb", "-g", "olcrtc-wb", wbProfileDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("prepare WB profile: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureWBRuntimeDir() error {
	cmd := exec.Command("install", "-d", "-m", "0750", "-o", "olcrtc-wb", "-g", "olcrtc-wb", wbRuntimeDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("prepare WB runtime: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func readWBProxyConfig() (wbProxyConfig, error) {
	var cfg wbProxyConfig
	err := readJSONFile(wbConfigFile, &cfg)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	return cfg, err
}

func readWBAccountState() (wbAccountState, error) {
	var state wbAccountState
	err := readJSONFile(wbAccountFile, &state)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	return state, err
}

func validateWBProxy(server string) error {
	if server == "" {
		return nil
	}
	u, err := url.Parse(server)
	if err != nil || u.Host == "" {
		return errors.New("invalid proxy URL")
	}
	if u.User != nil {
		return errors.New("proxy credentials must use the separate username and password fields")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("proxy URL must not contain a query or fragment")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5":
		return nil
	default:
		return errors.New("proxy scheme must be http, https or socks5")
	}
}

func cleanupWBWorkerFiles() {
	_ = os.Remove(filepath.Join(wbRuntimeDir, "state.json"))
	_ = os.Remove(filepath.Join(wbRuntimeDir, "job.json"))
	_ = os.Remove(filepath.Join(wbRuntimeDir, "control.json"))
}

func parseJWTExpiry(token string) (int64, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return 0, errors.New("token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, err
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, err
	}
	if claims.Exp <= 0 {
		return 0, errors.New("JWT has no exp claim")
	}
	return claims.Exp, nil
}

func randomHex(bytesCount int) (string, error) {
	buf := make([]byte, bytesCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func writeJSONFile(path string, value any, mode os.FileMode) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, mode)
}

func writeWBWorkerJSON(path string, value any) error {
	if err := writeJSONFile(path, value, 0600); err != nil {
		return err
	}
	account, err := user.Lookup("olcrtc-wb")
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(account.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(account.Gid)
	if err != nil {
		return err
	}
	return os.Chown(path, uid, gid)
}

func readJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, value)
}

func readUnixTimestamp(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
