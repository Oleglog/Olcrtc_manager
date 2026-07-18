package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	instanceRuntimeRoot = "/run"
	instanceStateRoot   = "/var/lib" // compatibility with binary-only updates
)

type instanceUsage struct {
	ID                int    `json:"id"`
	ActivePeers       int    `json:"active_peers"`
	UsageKnown        bool   `json:"usage_known"`
	OldestConnectedAt string `json:"oldest_connected_at,omitempty"`
	UpdatedAt          string `json:"updated_at,omitempty"`
}

type peerStatusFile struct {
	ActivePeers       int        `json:"active_peers"`
	OldestConnectedAt *time.Time `json:"oldest_connected_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (s *Server) handleInstanceUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	ids, err := ListInstances(s.cfg.ConfigDir)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	result := make([]instanceUsage, 0, len(ids))
	total := 0
	for _, id := range ids {
		status, _ := SystemctlStatusInfo(InstanceService(id))
		running := status != nil && (status.State == "running" || status.State == "active")
		usage := readInstanceUsage(id, running)
		result = append(result, usage)
		if usage.UsageKnown {
			total += usage.ActivePeers
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"instances":    result,
		"active_peers": total,
	})
}

func readInstanceUsage(id int, running bool) instanceUsage {
	usage := instanceUsage{ID: id}
	if !running {
		return usage
	}
	name := "olcrtc"
	if id > 0 {
		name = fmt.Sprintf("olcrtc-%d", id)
	}
	paths := []string{
		filepath.Join(instanceRuntimeRoot, name, "status.json"),
		filepath.Join(instanceStateRoot, name, "status.json"),
	}
	for _, path := range paths {
		if parsed, ok := readInstanceUsageFile(path, id); ok {
			return parsed
		}
	}
	return usage
}

func readInstanceUsageFile(path string, id int) (instanceUsage, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > 64*1024 {
		return instanceUsage{ID: id}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 64*1024 {
		return instanceUsage{ID: id}, false
	}
	var status peerStatusFile
	if err := json.Unmarshal(data, &status); err != nil || status.ActivePeers < 0 || status.UpdatedAt.IsZero() {
		return instanceUsage{ID: id}, false
	}
	if (status.ActivePeers > 0) != (status.OldestConnectedAt != nil) {
		return instanceUsage{ID: id}, false
	}
	usage := instanceUsage{
		ID:          id,
		ActivePeers: status.ActivePeers,
		UsageKnown:  true,
		UpdatedAt:   status.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if status.OldestConnectedAt != nil {
		usage.OldestConnectedAt = status.OldestConnectedAt.UTC().Format(time.RFC3339)
	}
	return usage, true
}
