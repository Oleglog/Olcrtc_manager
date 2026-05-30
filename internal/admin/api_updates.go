package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

// Version is set via ldflags at build time.
var Version = "1.8.18"

func (s *Server) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	// Query GitHub API for latest release
	resp, err := http.Get("https://api.github.com/repos/Oleglog/Olcrtc_manager/releases/latest")
	if err != nil {
		logger.Errorf("check updates: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "failed_to_check_updates",
			"message": err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "github_api_error",
			"message": fmt.Sprintf("GitHub API returned %d", resp.StatusCode),
		})
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		logger.Errorf("decode release: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "failed_to_parse_response",
			"message": err.Error(),
		})
		return
	}

	// Compare versions
	currentVersion := "v" + Version
	latestVersion := strings.TrimPrefix(release.TagName, "server-")

	updateAvailable := latestVersion != currentVersion && release.TagName != ""

	writeJSON(w, http.StatusOK, map[string]any{
		"current_version":  currentVersion,
		"latest_version":   latestVersion,
		"update_available": updateAvailable,
		"release_url":      release.HTMLURL,
	})
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Version string `json:"version"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "invalid_request",
			"message": "Invalid JSON body",
		})
		return
	}

	if req.Version == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "version_required",
			"message": "Version field is required",
		})
		return
	}

	// TODO: Implement actual update logic
	// This would involve:
	// 1. Download new binary from GitHub release
	// 2. Verify checksum
	// 3. Stop services
	// 4. Replace binary
	// 5. Restart services

	writeJSON(w, http.StatusNotImplemented, map[string]string{
		"error":   "not_implemented",
		"message": "Автообновление пока не реализовано. Пожалуйста, обновите вручную через install.sh",
	})
}
