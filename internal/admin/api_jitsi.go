package admin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type jitsiCheckResponse struct {
	URL             string `json:"url"`
	Host            string `json:"host"`
	OK              bool   `json:"ok"`
	Status          int    `json:"status"`
	LatencyMS       int64  `json:"latency_ms"`
	HasBOSH         bool   `json:"has_bosh"`
	HasWebSocket    bool   `json:"has_websocket"`
	ColibriWSHint   bool   `json:"colibri_ws_hint"`
	AnonymousHint   bool   `json:"anonymous_hint"`
	RecommendedMode string `json:"recommended_mode"`
	Message         string `json:"message"`
}

func (s *Server) handleJitsiCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	raw := strings.TrimSpace(r.URL.Query().Get("url"))
	if raw == "" {
		http.Error(w, "Bad Request: url is required", http.StatusBadRequest)
		return
	}
	res, err := checkJitsiHost(r.Context(), raw)
	if err != nil {
		writeJSON(w, http.StatusOK, jitsiCheckResponse{URL: raw, OK: false, Message: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func checkJitsiHost(ctx context.Context, raw string) (jitsiCheckResponse, error) {
	u, err := normaliseJitsiCheckURL(raw)
	if err != nil {
		return jitsiCheckResponse{}, err
	}
	cfgURL := *u
	cfgURL.Path = "/config.js"
	cfgURL.RawQuery = ""
	cfgURL.Fragment = ""

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfgURL.String(), nil)
	if err != nil {
		return jitsiCheckResponse{}, err
	}
	req.Header.Set("User-Agent", "olcrtc-admin-jitsi-check/1")
	client := &http.Client{Timeout: 9 * time.Second}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return jitsiCheckResponse{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	text := strings.ToLower(string(body))
	res := jitsiCheckResponse{
		URL:       u.String(),
		Host:      u.Host,
		Status:    resp.StatusCode,
		LatencyMS: latency,
	}
	res.HasBOSH = strings.Contains(text, "bosh") || strings.Contains(text, "http-bind")
	res.HasWebSocket = strings.Contains(text, "websocket") || strings.Contains(text, "xmpp-websocket")
	res.ColibriWSHint = strings.Contains(text, "colibri") || strings.Contains(text, "websocket")
	res.AnonymousHint = strings.Contains(text, "anonymousdomain") || strings.Contains(text, "anonymous")
	res.OK = resp.StatusCode >= 200 && resp.StatusCode < 300 && len(body) > 200 && (res.HasBOSH || res.HasWebSocket)
	if res.ColibriWSHint {
		res.RecommendedMode = "auto"
		res.Message = "config.js доступен. Colibri WS можно подтвердить только при запуске комнаты; используйте bridge=auto или colibri-ws для диагностики."
	} else if res.OK {
		res.RecommendedMode = "sctp"
		res.Message = "config.js доступен, но явного websocket/colibri hint не найден. Для datachannel лучше bridge=auto или sctp."
	} else {
		res.RecommendedMode = "auto"
		res.Message = fmt.Sprintf("config.js вернул status=%d или не похож на Jitsi config", resp.StatusCode)
	}
	return res, nil
}

func normaliseJitsiCheckURL(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("only http/https Jitsi URLs are supported")
	}
	if u.Host == "" {
		return nil, errors.New("Jitsi host is empty")
	}
	return u, nil
}
