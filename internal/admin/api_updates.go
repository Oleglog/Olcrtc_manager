package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

// Version is set via ldflags at build time.
var Version = "1.8.20"

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

	// Determine architecture
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "unsupported_arch",
			"message": fmt.Sprintf("Unsupported architecture: %s", arch),
		})
		return
	}

	// Get list of currently running instances BEFORE we start anything
	runningInstances := []int{}
	ids, _ := ListInstances("/etc/olcrtc")
	for _, id := range ids {
		st, _ := SystemctlStatusInfo(InstanceService(id))
		if st != nil && st.State == "running" {
			runningInstances = append(runningInstances, id)
		}
	}

	// Build list of additional instance services to restart (skip 0, it's olcrtc-server.service)
	var additionalServices []string
	for _, id := range runningInstances {
		if id != 0 {
			additionalServices = append(additionalServices, InstanceService(id))
		}
	}

	// Generate update script
	version := strings.TrimPrefix(req.Version, "v")
	tag := "server-v" + version
	repoURL := fmt.Sprintf("https://github.com/Oleglog/Olcrtc_manager/releases/download/%s", tag)

	additionalStartCmds := ""
	for _, svc := range additionalServices {
		additionalStartCmds += fmt.Sprintf("systemctl start %s || true\n", svc)
	}

	script := fmt.Sprintf(`#!/bin/bash
# olcRTC auto-update script - runs independently of admin service
exec > /tmp/olcrtc-update.log 2>&1
set -x

echo "=== olcRTC Update Started at $(date) ==="
echo "Updating to version: %s"
echo "Architecture: %s"

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

echo "Downloading olcrtc binary..."
if ! curl -fsSL --max-time 60 "%s/olcrtc-linux-%s" -o "$TMPDIR/olcrtc"; then
    echo "ERROR: Failed to download olcrtc binary"
    # Restart admin so user knows update failed
    systemctl start olcrtc-admin.service || true
    exit 1
fi

echo "Downloading olcrtc-admin binary..."
if ! curl -fsSL --max-time 60 "%s/olcrtc-admin-linux-%s" -o "$TMPDIR/olcrtc-admin"; then
    echo "ERROR: Failed to download olcrtc-admin binary"
    systemctl start olcrtc-admin.service || true
    exit 1
fi

# Verify binaries are valid ELF files
if ! file "$TMPDIR/olcrtc" | grep -q "ELF"; then
    echo "ERROR: olcrtc binary is not a valid ELF file"
    systemctl start olcrtc-admin.service || true
    exit 1
fi

if ! file "$TMPDIR/olcrtc-admin" | grep -q "ELF"; then
    echo "ERROR: olcrtc-admin binary is not a valid ELF file"
    systemctl start olcrtc-admin.service || true
    exit 1
fi

chmod +x "$TMPDIR/olcrtc" "$TMPDIR/olcrtc-admin"

echo "Stopping services..."
systemctl stop olcrtc-admin.service || true
systemctl stop olcrtc-server.service || true
%s

sleep 3

echo "Replacing binaries..."
install -m 0755 "$TMPDIR/olcrtc" /usr/local/bin/olcrtc
install -m 0755 "$TMPDIR/olcrtc-admin" /usr/local/bin/olcrtc-admin

echo "Reloading systemd..."
systemctl daemon-reload

echo "Starting services..."
systemctl start olcrtc-server.service
sleep 2
systemctl start olcrtc-admin.service
sleep 1
%s

echo "=== Update Completed at $(date) ==="
`,
		version,
		arch,
		repoURL, arch,
		repoURL, arch,
		buildStopCommands(additionalServices),
		additionalStartCmds,
	)

	scriptPath := "/tmp/olcrtc-update.sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		logger.Errorf("failed to create update script: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{
			"error":   "failed_to_create_script",
			"message": err.Error(),
		})
		return
	}

	// Send response BEFORE starting update
	writeJSON(w, http.StatusOK, map[string]string{
		"message": "Обновление запущено. Сервер перезапустится через 1-2 минуты.",
	})

	// Start update script in a separate transient systemd unit
	// This detaches it from admin service cgroup, so admin can stop without killing the script
	go func() {
		// Wait a bit to ensure response is sent
		time.Sleep(2 * time.Second)

		// Use systemd-run to create a transient unit independent of admin service
		cmd := exec.Command("systemd-run",
			"--no-block",
			"--collect",
			"--unit=olcrtc-update",
			"--description=olcRTC auto-update",
			"bash", scriptPath,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			logger.Errorf("failed to start systemd-run update: %v, output: %s", err, string(output))
			// Fallback: try to run directly with setsid
			fallback := exec.Command("setsid", "bash", scriptPath)
			fallback.SysProcAttr = &syscall.SysProcAttr{
				Setsid: true,
			}
			devNull, _ := os.Open(os.DevNull)
			if devNull != nil {
				fallback.Stdout = devNull
				fallback.Stderr = devNull
				fallback.Stdin = devNull
			}
			if err := fallback.Start(); err != nil {
				logger.Errorf("fallback update also failed: %v", err)
				return
			}
			_ = fallback.Process.Release()
			logger.Info("Fallback update started")
			return
		}

		logger.Info("Update script started via systemd-run, will continue after admin stops")
	}()
}

func buildStopCommands(services []string) string {
	cmds := ""
	for _, svc := range services {
		cmds += fmt.Sprintf("systemctl stop %s || true\n", svc)
	}
	return cmds
}
