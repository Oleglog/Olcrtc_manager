package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

// Instance represents an olcrtc server instance.
//
// Secret fields follow a mask-with-flag pattern: the actual value is not
// returned in list/detail responses; instead, a boolean flag (HasPassword)
// signals presence and a separate authenticated endpoint exposes the raw
// value when needed (e.g. /api/instances/{id}/room-password).
type Instance struct {
	ID                      int    `json:"id"`
	Label                   string `json:"label"`
	Carrier                 string `json:"carrier"`
	Transport               string `json:"transport"`
	RoomID                  string `json:"room_id"`
	ClientID                string `json:"client_id"`
	Name                    string `json:"name"`
	Status                  string `json:"status"`
	Uptime                  string `json:"uptime"`
	URI                     string `json:"uri"`
	SocksProxy              string `json:"socks_proxy"`
	WarpProxy               string `json:"warp_proxy"`
	DNS                     string `json:"dns"`
	Debug                   bool   `json:"debug"`
	JitsiBridgeMode         string `json:"jitsi_bridge_mode"`
	JitsiSCTPMaxMessageSize string `json:"jitsi_sctp_max_message_size"`
	TrafficMaxPayloadSize   string `json:"traffic_max_payload_size"`
	TrafficMinDelay         string `json:"traffic_min_delay"`
	TrafficMaxDelay         string `json:"traffic_max_delay"`
	VP8FPS                  string `json:"vp8_fps"`
	VP8Batch                string `json:"vp8_batch"`
}

func (s *Server) handleInstancesList(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listInstances(w, r)
	case http.MethodPost:
		s.createInstance(w, r)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleInstances(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/instances/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 1 {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getInstance(w, id)
		case http.MethodDelete:
			s.deleteInstance(w, id)
		case http.MethodPut:
			s.updateInstanceConfig(w, r, id)
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
		return
	}

	action := parts[1]
	switch action {
	case "uri":
		if r.Method == http.MethodGet {
			s.getInstanceURI(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "qr":
		if r.Method == http.MethodGet {
			s.getInstanceQR(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "restart":
		if r.Method == http.MethodPost {
			s.restartInstance(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "stop":
		if r.Method == http.MethodPost {
			s.stopInstance(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "start":
		if r.Method == http.MethodPost {
			s.startInstance(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "config":
		if r.Method == http.MethodPut {
			s.updateInstanceConfig(w, r, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "rotate-key":
		if r.Method == http.MethodPost {
			s.rotateKey(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "rotate-room":
		if r.Method == http.MethodPost {
			s.rotateRoom(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "rotate-client-id":
		if r.Method == http.MethodPost {
			s.rotateClientID(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	case "ping":
		if r.Method == http.MethodPost {
			s.pingInstance(w, id)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	default:
		http.Error(w, "Not Found", http.StatusNotFound)
	}
}

func (s *Server) listInstances(w http.ResponseWriter, r *http.Request) {
	ids, err := ListInstances(s.cfg.ConfigDir)
	if err != nil {
		logger.Errorf("listInstances: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	var result []Instance
	for _, id := range ids {
		inst := s.buildInstance(id)
		result = append(result, inst)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getInstance(w http.ResponseWriter, id int) {
	inst := s.buildInstance(id)
	if inst.RoomID == "" && inst.Name == "" {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, inst)
}

func (s *Server) createInstance(w http.ResponseWriter, r *http.Request) {
	ids, _ := ListInstances(s.cfg.ConfigDir)
	maxID := 0
	for _, id := range ids {
		if id > maxID {
			maxID = id
		}
	}
	newID := maxID + 1

	envPath := InstanceEnvPath(s.cfg.ConfigDir, newID)
	keyPath := InstanceKeyPath(s.cfg.ConfigDir, newID)

	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Parse optional body for initial config.
	carrier := "jitsi"
	transport := "vp8channel"
	name := ""
	roomID := ""
	vp8FPS := "120"
	vp8Batch := "64"
	dns := ""
	socksProxy := ""
	warpProxy := ""
	jitsiBridgeMode := "auto"
	jitsiSCTPMaxMessageSize := ""
	trafficMaxPayloadSize := ""
	trafficMinDelay := ""
	trafficMaxDelay := ""
	if r.Body != nil {
		var req struct {
			Carrier                 string `json:"carrier"`
			Transport               string `json:"transport"`
			Name                    string `json:"name"`
			RoomID                  string `json:"room_id"`
			VP8FPS                  any    `json:"vp8_fps"`
			VP8Batch                any    `json:"vp8_batch"`
			DNS                     string `json:"dns"`
			SocksProxy              string `json:"socks_proxy"`
			WarpProxy               string `json:"warp_proxy"`
			JitsiBridgeMode         string `json:"jitsi_bridge_mode"`
			JitsiSCTPMaxMessageSize string `json:"jitsi_sctp_max_message_size"`
			TrafficMaxPayloadSize   string `json:"traffic_max_payload_size"`
			TrafficMinDelay         string `json:"traffic_min_delay"`
			TrafficMaxDelay         string `json:"traffic_max_delay"`
		}
		if err := readJSON(r, &req); err == nil {
			if req.Carrier != "" {
				carrier = req.Carrier
			}
			if req.Transport != "" {
				transport = req.Transport
			}
			if req.Name != "" {
				name = req.Name
			}
			roomID = req.RoomID
			if req.VP8FPS != nil {
				vp8FPS = sanitizeUnsignedAny(req.VP8FPS)
			}
			if req.VP8Batch != nil {
				vp8Batch = sanitizeUnsignedAny(req.VP8Batch)
			}
			dns = strings.TrimSpace(req.DNS)
			socksProxy = strings.TrimSpace(req.SocksProxy)
			warpProxy = strings.TrimSpace(req.WarpProxy)
			if req.JitsiBridgeMode != "" {
				jitsiBridgeMode = normalizeJitsiBridgeMode(req.JitsiBridgeMode)
			}
			jitsiSCTPMaxMessageSize = sanitizeUnsignedString(req.JitsiSCTPMaxMessageSize)
			trafficMaxPayloadSize = sanitizeUnsignedString(req.TrafficMaxPayloadSize)
			trafficMinDelay = strings.TrimSpace(req.TrafficMinDelay)
			trafficMaxDelay = strings.TrimSpace(req.TrafficMaxDelay)
		}
	}

	if name == "" {
		name = fmt.Sprintf("%s_olcrtc_%d", carrier, newID+1)
	}

	vals := make(map[string]string)
	vals["OLCRTC_CARRIER"] = carrier
	vals["OLCRTC_TRANSPORT"] = transport
	vals["OLCRTC_KEY"] = hex.EncodeToString(key)
	vals["OLCRTC_NAME"] = name
	vals["OLCRTC_ROOM_ID"] = strings.TrimSpace(roomID)
	vals["OLCRTC_CLIENT_ID"] = uuid.NewString()
	vals["OLCRTC_JITSI_BRIDGE_MODE"] = jitsiBridgeMode
	if jitsiSCTPMaxMessageSize != "" {
		vals["OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE"] = jitsiSCTPMaxMessageSize
	}
	vals["OLCRTC_VP8_FPS"] = vp8FPS
	vals["OLCRTC_VP8_BATCH"] = vp8Batch
	if dns != "" {
		vals["OLCRTC_DNS"] = dns
	}
	if socksProxy != "" {
		vals["OLCRTC_SOCKS_PROXY"] = socksProxy
	}
	if warpProxy != "" {
		vals["OLCRTC_WARP_PROXY"] = warpProxy
	}
	if trafficMaxPayloadSize != "" {
		vals["OLCRTC_TRAFFIC_MAX_PAYLOAD"] = trafficMaxPayloadSize
	}
	if trafficMinDelay != "" {
		vals["OLCRTC_TRAFFIC_MIN_DELAY"] = trafficMinDelay
	}
	if trafficMaxDelay != "" {
		vals["OLCRTC_TRAFFIC_MAX_DELAY"] = trafficMaxDelay
	}
	delete(vals, "OLCRTC_ROOM_PASSWORD")
	if err := WriteInstanceEnv(envPath, vals); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	svc := InstanceService(newID)
	_ = SystemctlStart(svc)
	writeJSON(w, http.StatusCreated, s.buildInstance(newID))
}

func (s *Server) deleteInstance(w http.ResponseWriter, id int) {
	if id == 0 {
		http.Error(w, "Cannot delete main instance", http.StatusBadRequest)
		return
	}
	svc := InstanceService(id)
	_ = SystemctlStop(svc)

	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	keyPath := InstanceKeyPath(s.cfg.ConfigDir, id)
	_ = os.Remove(envPath)
	_ = os.Remove(keyPath)

	// Remove directory if empty.
	dir := filepath.Dir(envPath)
	_ = os.Remove(dir)

	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) updateInstanceConfig(w http.ResponseWriter, r *http.Request, id int) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	updates := buildInstanceConfigUpdates(req)

	// Validate that wbstream has a non-empty Room ID. WB Stream no longer
	// auto-creates rooms; the server would otherwise refuse to start with
	// ErrRoomIDRequired.
	effective := ReadInstanceEnv(envPath)
	for k, v := range updates {
		effective[k] = v
	}
	carrier := effective["OLCRTC_CARRIER"]
	if carrier == "" {
		carrier = effective["OLCRTC_PROVIDER"]
	}
	room := effective["OLCRTC_ROOM_ID"]
	if carrier == "wbstream" && (room == "" || room == "any") {
		http.Error(w, "wbstream requires a Room ID — WB Stream no longer auto-creates rooms; create one at https://stream.wb.ru and paste it into Room ID", http.StatusBadRequest)
		return
	}

	if err := WriteInstanceEnv(envPath, updates); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Restart service.
	svc := InstanceService(id)
	_ = SystemctlRestart(svc)

	writeJSON(w, http.StatusOK, s.buildInstance(id))
}

// buildInstanceConfigUpdates maps a parsed config request body to the
// OLCRTC_* env keys WriteInstanceEnv expects. client_id is intentionally NOT
// accepted from the request body to prevent typos / accidental overrides; it
// is rotated only via the dedicated /rotate-client-id endpoint and seeded by
// createInstance / buildInstance lazy-migration.
func buildInstanceConfigUpdates(req map[string]any) map[string]string {
	updates := make(map[string]string)
	if v, ok := req["carrier"].(string); ok {
		updates["OLCRTC_CARRIER"] = v
	}
	if v, ok := req["transport"].(string); ok {
		updates["OLCRTC_TRANSPORT"] = v
	}
	if v, ok := req["name"].(string); ok {
		updates["OLCRTC_NAME"] = v
	}
	if v, ok := req["room_id"].(string); ok {
		updates["OLCRTC_ROOM_ID"] = strings.TrimSpace(v)
	}
	if v, ok := req["dns"].(string); ok {
		updates["OLCRTC_DNS"] = v
	}
	if v, ok := req["socks_proxy"].(string); ok {
		updates["OLCRTC_SOCKS_PROXY"] = v
	}
	if v, ok := req["warp_proxy"].(string); ok {
		updates["OLCRTC_WARP_PROXY"] = v
	}
	if v, ok := req["jitsi_bridge_mode"].(string); ok {
		updates["OLCRTC_JITSI_BRIDGE_MODE"] = normalizeJitsiBridgeMode(v)
	}
	if v, ok := req["jitsi_sctp_max_message_size"].(string); ok {
		updates["OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE"] = sanitizeUnsignedString(v)
	}
	if v, ok := req["jitsi_sctp_max_message_size"].(float64); ok {
		updates["OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE"] = sanitizeUnsignedFloat(v)
	}
	if v, ok := req["traffic_max_payload_size"].(string); ok {
		updates["OLCRTC_TRAFFIC_MAX_PAYLOAD"] = sanitizeUnsignedString(v)
	}
	if v, ok := req["traffic_max_payload_size"].(float64); ok {
		updates["OLCRTC_TRAFFIC_MAX_PAYLOAD"] = sanitizeUnsignedFloat(v)
	}
	if v, ok := req["traffic_min_delay"].(string); ok {
		updates["OLCRTC_TRAFFIC_MIN_DELAY"] = strings.TrimSpace(v)
	}
	if v, ok := req["traffic_max_delay"].(string); ok {
		updates["OLCRTC_TRAFFIC_MAX_DELAY"] = strings.TrimSpace(v)
	}
	if v, ok := req["debug"].(bool); ok {
		if v {
			updates["OLCRTC_DEBUG"] = "1"
		} else {
			updates["OLCRTC_DEBUG"] = ""
		}
	}
	addInstanceTuningUpdates(req, updates)
	return updates
}

// addInstanceTuningUpdates handles the numeric VP8/SEI tuning knobs, split out
// of buildInstanceConfigUpdates to keep each function's branch count low.
func addInstanceTuningUpdates(req map[string]any, updates map[string]string) {
	if v, ok := req["vp8_fps"].(string); ok {
		updates["OLCRTC_VP8_FPS"] = sanitizeUnsignedString(v)
	}
	if v, ok := req["vp8_fps"].(float64); ok {
		updates["OLCRTC_VP8_FPS"] = sanitizeUnsignedFloat(v)
	}
	if v, ok := req["vp8_batch"].(string); ok {
		updates["OLCRTC_VP8_BATCH"] = sanitizeUnsignedString(v)
	}
	if v, ok := req["vp8_batch"].(float64); ok {
		updates["OLCRTC_VP8_BATCH"] = sanitizeUnsignedFloat(v)
	}
	if v, ok := req["sei_fps"].(float64); ok {
		updates["OLCRTC_SEI_FPS"] = fmt.Sprintf("%.0f", v)
	}
	if v, ok := req["sei_batch"].(float64); ok {
		updates["OLCRTC_SEI_BATCH"] = fmt.Sprintf("%.0f", v)
	}
	if v, ok := req["sei_frag"].(float64); ok {
		updates["OLCRTC_SEI_FRAG"] = fmt.Sprintf("%.0f", v)
	}
	if v, ok := req["sei_ack_ms"].(float64); ok {
		updates["OLCRTC_SEI_ACK"] = fmt.Sprintf("%.0f", v)
	}
}

func (s *Server) getInstanceURI(w http.ResponseWriter, id int) {
	uri := s.buildURI(id)
	writeJSON(w, http.StatusOK, map[string]string{"uri": uri})
}

func (s *Server) getInstanceQR(w http.ResponseWriter, id int) {
	// Return the URI; client-side JS will generate QR.
	uri := s.buildURI(id)
	writeJSON(w, http.StatusOK, map[string]string{"uri": uri})
}

func (s *Server) restartInstance(w http.ResponseWriter, id int) {
	svc := InstanceService(id)
	if err := SystemctlRestart(svc); err != nil {
		http.Error(w, "Failed to restart", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) stopInstance(w http.ResponseWriter, id int) {
	svc := InstanceService(id)
	if err := SystemctlStop(svc); err != nil {
		http.Error(w, "Failed to stop", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) startInstance(w http.ResponseWriter, id int) {
	svc := InstanceService(id)
	if err := SystemctlStart(svc); err != nil {
		http.Error(w, "Failed to start", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) rotateKey(w http.ResponseWriter, id int) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	keyHex := hex.EncodeToString(key)
	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	if err := SetEnvValue(envPath, "OLCRTC_KEY", keyHex); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	svc := InstanceService(id)
	_ = SystemctlRestart(svc)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "key": keyHex})
}

func (s *Server) rotateRoom(w http.ResponseWriter, id int) {
	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)

	// For wbstream auto-rotation is no longer possible because WB Stream
	// disabled the room creation API. Refuse and tell the user to create
	// a room manually and update Room ID via the config form.
	vals := ReadInstanceEnv(envPath)
	carrier := vals["OLCRTC_CARRIER"]
	if carrier == "" {
		carrier = vals["OLCRTC_PROVIDER"]
	}
	if carrier == "wbstream" {
		http.Error(w, "wbstream Room ID can no longer be rotated automatically — WB Stream disabled the room creation API. Create a new room at https://stream.wb.ru and paste its ID into the Room ID field manually.", http.StatusBadRequest)
		return
	}

	if err := SetEnvValue(envPath, "OLCRTC_ROOM_ID", ""); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	svc := InstanceService(id)
	_ = SystemctlRestart(svc)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// rotateClientID generates a new client identifier for the instance and
// restarts the service. The client_id is included in the published URI;
// peers that imported the previous URI must re-import after rotation.
func (s *Server) rotateClientID(w http.ResponseWriter, id int) {
	newID := uuid.NewString()
	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	if err := SetEnvValue(envPath, "OLCRTC_CLIENT_ID", newID); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	svc := InstanceService(id)
	_ = SystemctlRestart(svc)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "client_id": newID})
}

// ensureClientID lazy-migrates legacy instances that pre-date the
// per-instance client identifier. If OLCRTC_CLIENT_ID is missing or
// empty it generates a fresh UUID, persists it via SetEnvValue and
// returns the new value. Errors are logged and an empty string is
// returned, in which case buildURI will simply omit the client_id query
// parameter.
func (s *Server) ensureClientID(envPath string, current string) string {
	if current != "" {
		return current
	}
	newID := uuid.NewString()
	if err := SetEnvValue(envPath, "OLCRTC_CLIENT_ID", newID); err != nil {
		logger.Errorf("ensureClientID: persist %s: %v", envPath, err)
		return ""
	}
	return newID
}

func normalizeJitsiBridgeMode(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "colibri-ws", "colibri", "ws", "websocket":
		return "colibri-ws"
	case "sctp", "datachannel", "dc":
		return "sctp"
	default:
		return "auto"
	}
}

func effectiveJitsiBridgeMode(v string) string {
	if strings.TrimSpace(v) == "" {
		return "auto"
	}
	return normalizeJitsiBridgeMode(v)
}

func sanitizeUnsignedString(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func sanitizeUnsignedFloat(v float64) string {
	if v < 0 {
		return ""
	}
	return fmt.Sprintf("%.0f", v)
}

func sanitizeUnsignedAny(v any) string {
	switch t := v.(type) {
	case string:
		return sanitizeUnsignedString(t)
	case float64:
		return sanitizeUnsignedFloat(t)
	default:
		return ""
	}
}

func (s *Server) buildInstance(id int) Instance {
	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	vals := ReadInstanceEnv(envPath)

	carrier := vals["OLCRTC_CARRIER"]
	if carrier == "" {
		carrier = vals["OLCRTC_PROVIDER"]
	}
	transport := vals["OLCRTC_TRANSPORT"]
	if transport == "" {
		transport = "datachannel"
	}
	name := vals["OLCRTC_NAME"]
	if name == "" {
		name = fmt.Sprintf("%s_olcrtc", carrier)
	}

	clientID := s.ensureClientID(envPath, vals["OLCRTC_CLIENT_ID"])

	label := "Доп. #" + strconv.Itoa(id)
	if id == 0 {
		label = "Основной"
	}

	st, _ := SystemctlStatusInfo(InstanceService(id))
	status := "unknown"
	uptime := ""
	if st != nil {
		status = st.State
		uptime = st.Uptime
	}

	return Instance{
		ID:                    id,
		Label:                 label,
		Carrier:               carrier,
		Transport:             transport,
		RoomID:                vals["OLCRTC_ROOM_ID"],
		ClientID:              clientID,
		Name:                  name,
		Status:                status,
		Uptime:                uptime,
		URI:                   s.buildURIWith(vals, clientID),
		SocksProxy:            vals["OLCRTC_SOCKS_PROXY"],
		WarpProxy:             vals["OLCRTC_WARP_PROXY"],
		DNS:                   vals["OLCRTC_DNS"],
		Debug:                 vals["OLCRTC_DEBUG"] == "1",
		JitsiBridgeMode:         effectiveJitsiBridgeMode(vals["OLCRTC_JITSI_BRIDGE_MODE"]),
		JitsiSCTPMaxMessageSize: vals["OLCRTC_JITSI_SCTP_MAX_MESSAGE_SIZE"],
		TrafficMaxPayloadSize:   vals["OLCRTC_TRAFFIC_MAX_PAYLOAD"],
		TrafficMinDelay:         vals["OLCRTC_TRAFFIC_MIN_DELAY"],
		TrafficMaxDelay:         vals["OLCRTC_TRAFFIC_MAX_DELAY"],
		VP8FPS:                  vals["OLCRTC_VP8_FPS"],
		VP8Batch:                vals["OLCRTC_VP8_BATCH"],
	}
}

func (s *Server) buildURI(id int) string {
	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	vals := ReadInstanceEnv(envPath)
	clientID := s.ensureClientID(envPath, vals["OLCRTC_CLIENT_ID"])
	return s.buildURIWith(vals, clientID)
}

// buildURIWith renders the deep-link URI from a pre-loaded env map. It is
// extracted so buildInstance does not have to read the env file twice.
func (s *Server) buildURIWith(vals map[string]string, clientID string) string {
	carrier := vals["OLCRTC_CARRIER"]
	if carrier == "" {
		carrier = vals["OLCRTC_PROVIDER"]
	}
	room := vals["OLCRTC_ROOM_ID"]
	key := vals["OLCRTC_KEY"]
	name := vals["OLCRTC_NAME"]
	if name == "" {
		name = fmt.Sprintf("%s_olcrtc", carrier)
	}
	transport := vals["OLCRTC_TRANSPORT"]
	vp8Fps := vals["OLCRTC_VP8_FPS"]
	vp8Batch := vals["OLCRTC_VP8_BATCH"]

	uri := fmt.Sprintf("olcrtc://%s@room/%s?key=%s", carrier, room, key)
	if transport != "" && transport != "datachannel" {
		uri += "&transport=" + url.QueryEscape(transport)
		if transport == "vp8channel" {
			if vp8Fps != "" {
				uri += "&vp8_fps=" + vp8Fps
			}
			if vp8Batch != "" {
				uri += "&vp8_batch=" + vp8Batch
			}
		}
	}
	if clientID != "" {
		uri += "&client_id=" + url.QueryEscape(clientID)
	}
	uri += "#" + name
	return uri
}

func (s *Server) pingInstance(w http.ResponseWriter, id int) {
	st, err := SystemctlStatusInfo(InstanceService(id))
	if err != nil {
		logger.Errorf("ping instance %d: %v", id, err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "failed_to_check_status",
			"message": err.Error(),
		})
		return
	}
	instanceStatus := "unknown"
	if st != nil {
		instanceStatus = st.State
	}

	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	vals := ReadInstanceEnv(envPath)

	target, kind := pickPingTarget(vals)

	rtt, loss, err := runPing(target)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":              false,
			"instance_status": instanceStatus,
			"target":          target,
			"target_kind":     kind,
			"message":         "Не удалось пинговать " + target + ": " + err.Error(),
		})
		return
	}

	ok := loss < 100 && instanceStatus == "running"
	msg := fmt.Sprintf("%s · %.1f ms · потери %d%%", target, rtt, loss)
	if instanceStatus != "running" {
		msg = "Инстанс не запущен (" + instanceStatus + "). Цель: " + msg
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              ok,
		"instance_status": instanceStatus,
		"target":          target,
		"target_kind":     kind,
		"rtt_ms":          rtt,
		"packet_loss":     loss,
		"message":         msg,
	})
}

// pickPingTarget chooses what to ping for an instance:
//  1. Host of OLCRTC_SOCKS_PROXY (the actual upstream the instance routes through)
//  2. Host of OLCRTC_WARP_PROXY
//  3. 1.1.1.1 as a generic internet-reachability probe
func pickPingTarget(vals map[string]string) (host, kind string) {
	if v := strings.TrimSpace(vals["OLCRTC_SOCKS_PROXY"]); v != "" {
		if h := extractHost(v); h != "" {
			return h, "socks_proxy"
		}
	}
	if v := strings.TrimSpace(vals["OLCRTC_WARP_PROXY"]); v != "" {
		if h := extractHost(v); h != "" {
			return h, "warp_proxy"
		}
	}
	return "1.1.1.1", "internet"
}

// extractHost pulls the host portion out of either a URL ("scheme://[user:pass@]host:port")
// or a plain "host:port" / "host" string.
func extractHost(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Host != "" {
			if h, _, err := net.SplitHostPort(u.Host); err == nil {
				return h
			}
			return u.Host
		}
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}

var pingAvgRTT = regexp.MustCompile(`(?:rtt|round-trip)\s+min/avg/max(?:/m?dev)?\s*=\s*[\d.]+/([\d.]+)/`)
var pingLoss = regexp.MustCompile(`(\d+)% packet loss`)

// runPing shells out to `ping -c 3 -W 2 <host>` and returns the average RTT
// (ms) and packet loss percentage.
func runPing(host string) (avgMs float64, lossPct int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ping", "-c", "3", "-W", "2", host)
	out, err := cmd.CombinedOutput()
	output := string(out)
	// ping returns non-zero on 100% loss but still prints stats — parse before
	// surfacing the error.
	lossPct = 100
	if m := pingLoss.FindStringSubmatch(output); len(m) == 2 {
		if n, perr := strconv.Atoi(m[1]); perr == nil {
			lossPct = n
		}
	}
	if m := pingAvgRTT.FindStringSubmatch(output); len(m) == 2 {
		if v, perr := strconv.ParseFloat(m[1], 64); perr == nil {
			avgMs = v
		}
	}
	if lossPct < 100 {
		return avgMs, lossPct, nil
	}
	if err == nil {
		err = fmt.Errorf("100%% packet loss")
	} else if ctx.Err() == context.DeadlineExceeded {
		err = fmt.Errorf("таймаут")
	}
	return avgMs, lossPct, err
}
