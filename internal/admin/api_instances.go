package admin

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

// Instance represents an olcrtc server instance.
type Instance struct {
	ID         int    `json:"id"`
	Label      string `json:"label"`
	Carrier    string `json:"carrier"`
	Transport  string `json:"transport"`
	RoomID     string `json:"room_id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Uptime     string `json:"uptime"`
	URI        string `json:"uri"`
	SocksProxy string `json:"socks_proxy"`
	WarpProxy  string `json:"warp_proxy"`
	DNS        string `json:"dns"`
	Debug      bool   `json:"debug"`
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

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(keyPath), 0755); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Generate key.
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Copy defaults from main instance.
	mainEnv := InstanceEnvPath(s.cfg.ConfigDir, 0)
	vals := ReadInstanceEnv(mainEnv)
	vals["OLCRTC_KEY"] = hex.EncodeToString(key)
	vals["OLCRTC_ROOM_ID"] = ""
	vals["OLCRTC_NAME"] = fmt.Sprintf("%s_olcrtc_%d", vals["OLCRTC_CARRIER"], newID+1)
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
	os.Remove(dir)

	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func (s *Server) updateInstanceConfig(w http.ResponseWriter, r *http.Request, id int) {
	var req map[string]any
	if err := readJSON(r, &req); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
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
	if v, ok := req["dns"].(string); ok {
		updates["OLCRTC_DNS"] = v
	}
	if v, ok := req["socks_proxy"].(string); ok {
		updates["OLCRTC_SOCKS_PROXY"] = v
	}
	if v, ok := req["warp_proxy"].(string); ok {
		updates["OLCRTC_WARP_PROXY"] = v
	}
	if v, ok := req["debug"].(bool); ok {
		if v {
			updates["OLCRTC_DEBUG"] = "1"
		} else {
			updates["OLCRTC_DEBUG"] = ""
		}
	}
	if v, ok := req["vp8_fps"].(float64); ok {
		updates["OLCRTC_VP8_FPS"] = fmt.Sprintf("%.0f", v)
	}
	if v, ok := req["vp8_batch"].(float64); ok {
		updates["OLCRTC_VP8_BATCH"] = fmt.Sprintf("%.0f", v)
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

	if err := WriteInstanceEnv(envPath, updates); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Restart service.
	svc := InstanceService(id)
	_ = SystemctlRestart(svc)

	writeJSON(w, http.StatusOK, s.buildInstance(id))
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
	if err := SetEnvValue(envPath, "OLCRTC_ROOM_ID", ""); err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	svc := InstanceService(id)
	_ = SystemctlRestart(svc)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
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
		ID:         id,
		Label:      label,
		Carrier:    carrier,
		Transport:  transport,
		RoomID:     vals["OLCRTC_ROOM_ID"],
		Name:       name,
		Status:     status,
		Uptime:     uptime,
		URI:        s.buildURI(id),
		SocksProxy: vals["OLCRTC_SOCKS_PROXY"],
		WarpProxy:  vals["OLCRTC_WARP_PROXY"],
		DNS:        vals["OLCRTC_DNS"],
		Debug:      vals["OLCRTC_DEBUG"] == "1",
	}
}

func (s *Server) buildURI(id int) string {
	envPath := InstanceEnvPath(s.cfg.ConfigDir, id)
	vals := ReadInstanceEnv(envPath)

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
	uri += "#" + name
	return uri
}
