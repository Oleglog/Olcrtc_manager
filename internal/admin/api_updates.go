package admin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

// Version is set via ldflags at build time.
var Version = "1.8.19"

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
		"tag_name":         release.TagName,
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

	// Start update in background
	go func() {
		if err := performUpdate(req.Version); err != nil {
			logger.Errorf("update failed: %v", err)
		}
	}()

	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Обновление запущено. Сервер перезапустится через 1-2 минуты.",
	})
}

func performUpdate(version string) error {
	logger.Infof("Starting update to version %s", version)

	// Determine architecture
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "amd64"
	} else if arch == "arm64" {
		arch = "arm64"
	} else {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	// Construct download URLs
	tag := "server-v" + strings.TrimPrefix(version, "v")
	baseURL := fmt.Sprintf("https://github.com/Oleglog/Olcrtc_manager/releases/download/%s", tag)

	serverBinary := fmt.Sprintf("olcrtc-linux-%s", arch)
	adminBinary := fmt.Sprintf("olcrtc-admin-linux-%s", arch)

	serverURL := fmt.Sprintf("%s/%s", baseURL, serverBinary)
	adminURL := fmt.Sprintf("%s/%s", baseURL, adminBinary)

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "olcrtc-update-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	logger.Infof("Downloading binaries to %s", tmpDir)

	// Download server binary
	serverPath := filepath.Join(tmpDir, "olcrtc")
	if err := downloadFile(serverURL, serverPath); err != nil {
		return fmt.Errorf("download server binary: %w", err)
	}

	// Download admin binary
	adminPath := filepath.Join(tmpDir, "olcrtc-admin")
	if err := downloadFile(adminURL, adminPath); err != nil {
		return fmt.Errorf("download admin binary: %w", err)
	}

	// Make binaries executable
	if err := os.Chmod(serverPath, 0755); err != nil {
		return fmt.Errorf("chmod server: %w", err)
	}
	if err := os.Chmod(adminPath, 0755); err != nil {
		return fmt.Errorf("chmod admin: %w", err)
	}

	logger.Info("Stopping services...")

	// Stop all olcrtc services
	if err := exec.Command("systemctl", "stop", "olcrtc-admin").Run(); err != nil {
		logger.Warnf("stop olcrtc-admin: %v", err)
	}

	// Stop all instance services
	if err := exec.Command("bash", "-c", "systemctl stop 'olcrtc@*'").Run(); err != nil {
		logger.Warnf("stop olcrtc instances: %v", err)
	}

	// Wait a bit for services to stop
	time.Sleep(2 * time.Second)

	logger.Info("Replacing binaries...")

	// Replace binaries directly (no backup needed)
	if err := copyFile(serverPath, "/usr/local/bin/olcrtc"); err != nil {
		return fmt.Errorf("install server binary: %w", err)
	}
	if err := copyFile(adminPath, "/usr/local/bin/olcrtc-admin"); err != nil {
		return fmt.Errorf("install admin binary: %w", err)
	}

	// Set permissions
	if err := os.Chmod("/usr/local/bin/olcrtc", 0755); err != nil {
		return fmt.Errorf("chmod /usr/local/bin/olcrtc: %w", err)
	}
	if err := os.Chmod("/usr/local/bin/olcrtc-admin", 0755); err != nil {
		return fmt.Errorf("chmod /usr/local/bin/olcrtc-admin: %w", err)
	}

	logger.Info("Restarting services...")

	// Restart services
	if err := exec.Command("systemctl", "daemon-reload").Run(); err != nil {
		logger.Warnf("daemon-reload: %v", err)
	}

	// Start admin service
	if err := exec.Command("systemctl", "start", "olcrtc-admin").Run(); err != nil {
		logger.Errorf("start olcrtc-admin: %v", err)
	}

	// Start all instance services that were enabled
	if err := exec.Command("bash", "-c", "systemctl start 'olcrtc@*'").Run(); err != nil {
		logger.Warnf("start olcrtc instances: %v", err)
	}

	logger.Infof("Update to version %s completed successfully", version)
	return nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func copyFile(src, dst string) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()

	_, err = io.Copy(destination, source)
	return err
}

func verifyChecksum(filePath, expectedChecksum string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}

	actualChecksum := hex.EncodeToString(h.Sum(nil))
	if actualChecksum != expectedChecksum {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedChecksum, actualChecksum)
	}

	return nil
}
