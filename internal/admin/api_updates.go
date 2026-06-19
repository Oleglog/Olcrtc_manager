package admin

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
)

// Version is set via ldflags at build time.
var Version = "1.8.20"

// MinUpdatableVersion is the floor for the version dropdown — versions below
// this lack the auto-update endpoint, so installing them would brick the flow.
const MinUpdatableVersion = "1.8.27"

// Update check cache: stored after each successful GitHub fetch, served only
// as a stale fallback when both GitHub paths (redirect + API) fail.
type updateCache struct {
	timestamp       time.Time
	currentVersion  string
	latestVersion   string
	updateAvailable bool
	releaseURL      string
	tagName         string
}

var (
	updateCacheMu sync.RWMutex
	cachedUpdate  *updateCache
)

func (s *Server) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	currentVersion := "v" + Version
	tagName, releaseURL, source, err := fetchLatestTag()
	if err != nil {
		// Both GitHub paths failed — fall back to stale cache if any
		updateCacheMu.RLock()
		if cachedUpdate != nil {
			cached := cachedUpdate
			updateCacheMu.RUnlock()
			writeJSON(w, http.StatusOK, map[string]any{
				"current_version":  cached.currentVersion,
				"latest_version":   cached.latestVersion,
				"update_available": cached.updateAvailable,
				"release_url":      cached.releaseURL,
				"tag_name":         cached.tagName,
				"source":           "stale_cache",
				"stale":            true,
			})
			return
		}
		updateCacheMu.RUnlock()
		logger.Errorf("check updates: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   "github_unreachable",
			"message": "GitHub недоступен. Попробуйте позже.",
		})
		return
	}

	latestVersion := strings.TrimPrefix(tagName, "server-")
	updateAvailable := latestVersion != currentVersion && tagName != ""

	updateCacheMu.Lock()
	cachedUpdate = &updateCache{
		timestamp:       time.Now(),
		currentVersion:  currentVersion,
		latestVersion:   latestVersion,
		updateAvailable: updateAvailable,
		releaseURL:      releaseURL,
		tagName:         tagName,
	}
	updateCacheMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"current_version":  currentVersion,
		"latest_version":   latestVersion,
		"update_available": updateAvailable,
		"release_url":      releaseURL,
		"tag_name":         tagName,
		"source":           source,
	})
}

// fetchLatestTag tries the rate-limit-free redirect path first, then falls back
// to the rate-limited GitHub API. Returns source = "redirect" or "api".
func fetchLatestTag() (tag, url, source string, err error) {
	tag, url, err = fetchLatestTagViaRedirect()
	if err == nil {
		return tag, url, "redirect", nil
	}
	redirectErr := err
	tag, url, err = fetchLatestTagViaAPI()
	if err == nil {
		return tag, url, "api", nil
	}
	return "", "", "", fmt.Errorf("redirect: %w; api: %w", redirectErr, err)
}

// fetchLatestTagViaRedirect resolves the latest release by reading the Location
// header GitHub returns from the public /releases/latest URL. This is a normal
// HTML endpoint, not the API, so it is not subject to the 60 req/hour anonymous
// API limit.
func fetchLatestTagViaRedirect() (tagName, releaseURL string, err error) {
	const url = "https://github.com/Oleglog/Olcrtc_manager/releases/latest"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "olcrtc-admin/"+Version)

	client := &http.Client{
		Timeout: 8 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently && resp.StatusCode != http.StatusSeeOther && resp.StatusCode != http.StatusTemporaryRedirect && resp.StatusCode != http.StatusPermanentRedirect {
		return "", "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc == "" {
		return "", "", fmt.Errorf("missing Location header")
	}
	// Expected: https://github.com/Oleglog/Olcrtc_manager/releases/tag/server-vX.Y.Z
	idx := strings.LastIndex(loc, "/tag/")
	if idx < 0 {
		return "", "", fmt.Errorf("unexpected Location: %s", loc)
	}
	tag := loc[idx+len("/tag/"):]
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return "", "", fmt.Errorf("empty tag in Location: %s", loc)
	}
	if !strings.HasPrefix(loc, "http") {
		loc = "https://github.com" + loc
	}
	return tag, loc, nil
}

func fetchLatestTagViaAPI() (tagName, releaseURL string, err error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/Oleglog/Olcrtc_manager/releases/latest", nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "olcrtc-admin/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github API returned %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", "", err
	}
	return release.TagName, release.HTMLURL, nil
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

STATE_FILE=/tmp/olcrtc-update-state.json

write_state() {
    local phase="$1"
    local message="$2"
    local percent="$3"
    local now
    now=$(date +%%s)
    cat > "$STATE_FILE" <<EOF
{"phase":"$phase","message":"$message","percent":$percent,"target_version":"%s","updated_at":$now}
EOF
}

fail_state() {
    local message="$1"
    local now
    now=$(date +%%s)
    cat > "$STATE_FILE" <<EOF
{"phase":"error","message":"$message","percent":0,"target_version":"%s","updated_at":$now}
EOF
}

echo "=== olcRTC Update Started at $(date) ==="
echo "Updating to version: %s"
echo "Architecture: %s"

write_state "starting" "Подготовка обновления..." 2

TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

write_state "downloading_server" "Скачивание сервера..." 10
echo "Downloading olcrtc binary..."
if ! curl -fsSL --max-time 60 "%s/olcrtc-linux-%s" -o "$TMPDIR/olcrtc"; then
    echo "ERROR: Failed to download olcrtc binary"
    fail_state "Не удалось скачать сервер"
    systemctl start olcrtc-admin.service || true
    exit 1
fi

write_state "downloading_admin" "Скачивание админки..." 25
echo "Downloading olcrtc-admin binary..."
if ! curl -fsSL --max-time 60 "%s/olcrtc-admin-linux-%s" -o "$TMPDIR/olcrtc-admin"; then
    echo "ERROR: Failed to download olcrtc-admin binary"
    fail_state "Не удалось скачать админку"
    systemctl start olcrtc-admin.service || true
    exit 1
fi

write_state "verifying" "Проверка бинарников..." 35
# Verify binaries are valid ELF files
if ! file "$TMPDIR/olcrtc" | grep -q "ELF"; then
    echo "ERROR: olcrtc binary is not a valid ELF file"
    fail_state "Повреждённый бинарник сервера"
    systemctl start olcrtc-admin.service || true
    exit 1
fi

if ! file "$TMPDIR/olcrtc-admin" | grep -q "ELF"; then
    echo "ERROR: olcrtc-admin binary is not a valid ELF file"
    fail_state "Повреждённый бинарник админки"
    systemctl start olcrtc-admin.service || true
    exit 1
fi

chmod +x "$TMPDIR/olcrtc" "$TMPDIR/olcrtc-admin"

write_state "stopping" "Остановка сервисов..." 45
echo "Stopping services..."
systemctl stop olcrtc-admin.service || true
systemctl stop olcrtc-server.service || true
%s

sleep 3

write_state "replacing" "Замена бинарников..." 60
echo "Replacing binaries..."
install -m 0755 "$TMPDIR/olcrtc" /usr/local/bin/olcrtc
install -m 0755 "$TMPDIR/olcrtc-admin" /usr/local/bin/olcrtc-admin

echo "Reloading systemd..."
systemctl daemon-reload

write_state "starting_server" "Запуск сервера..." 75
echo "Starting services..."
systemctl start olcrtc-server.service
sleep 2

write_state "starting_admin" "Запуск админки..." 88
systemctl start olcrtc-admin.service
sleep 1
%s

write_state "completed" "Обновление завершено" 100
echo "=== Update Completed at $(date) ==="
`,
		version,
		version,
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

	// Seed initial state so frontend sees progress immediately, before script runs.
	initial := fmt.Sprintf(`{"phase":"queued","message":"Запуск процесса обновления...","percent":1,"target_version":%q,"updated_at":%d}`, version, time.Now().Unix())
	_ = os.WriteFile(updateStateFile, []byte(initial), 0644)

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

const updateStateFile = "/tmp/olcrtc-update-state.json"

func (s *Server) handleUpdateProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	data, err := os.ReadFile(updateStateFile)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"phase":   "idle",
			"message": "Обновление не запущено",
			"percent": 0,
		})
		return
	}
	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"phase":   "unknown",
			"message": "Не удалось прочитать состояние",
			"percent": 0,
		})
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// releaseInfo is one entry in the version dropdown.
type releaseInfo struct {
	Tag         string `json:"tag"`
	Version     string `json:"version"`
	URL         string `json:"url"`
	PublishedAt string `json:"published_at"`
}

func (s *Server) handleListReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	releases, source, err := fetchReleases()
	if err != nil {
		logger.Errorf("list releases: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   "github_unreachable",
			"message": "Не удалось получить список версий с GitHub.",
		})
		return
	}
	floor := MinUpdatableVersion
	filtered := make([]releaseInfo, 0, len(releases))
	for _, rel := range releases {
		if compareSemver(strings.TrimPrefix(rel.Version, "v"), floor) >= 0 {
			filtered = append(filtered, rel)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"releases":    filtered,
		"min_version": "v" + floor,
		"source":      source,
	})
}

// fetchReleases tries the rate-limit-free atom feed first, then GitHub API.
func fetchReleases() ([]releaseInfo, string, error) {
	rels, err := fetchReleasesViaAtom()
	if err == nil {
		return rels, "atom", nil
	}
	atomErr := err
	rels, err = fetchReleasesViaAPI()
	if err == nil {
		return rels, "api", nil
	}
	return nil, "", fmt.Errorf("atom: %w; api: %w", atomErr, err)
}

func fetchReleasesViaAtom() ([]releaseInfo, error) {
	const url = "https://github.com/Oleglog/Olcrtc_manager/releases.atom"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "olcrtc-admin/"+Version)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("atom returned %d", resp.StatusCode)
	}
	var feed struct {
		Entries []struct {
			ID      string `xml:"id"`
			Title   string `xml:"title"`
			Updated string `xml:"updated"`
			Link    []struct {
				Rel  string `xml:"rel,attr"`
				Href string `xml:"href,attr"`
			} `xml:"link"`
		} `xml:"entry"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, err
	}
	out := make([]releaseInfo, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		// id format: tag:github.com,2008:Repository/<id>/<tag>
		idx := strings.LastIndex(e.ID, "/")
		if idx < 0 {
			continue
		}
		tag := e.ID[idx+1:]
		if !strings.HasPrefix(tag, "server-v") {
			continue
		}
		version := strings.TrimPrefix(tag, "server-")
		htmlURL := ""
		for _, l := range e.Link {
			if l.Rel == "alternate" || l.Rel == "" {
				htmlURL = l.Href
				break
			}
		}
		out = append(out, releaseInfo{
			Tag:         tag,
			Version:     version,
			URL:         htmlURL,
			PublishedAt: e.Updated,
		})
	}
	return out, nil
}

func fetchReleasesViaAPI() ([]releaseInfo, error) {
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/Oleglog/Olcrtc_manager/releases?per_page=30", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "olcrtc-admin/"+Version)
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned %d", resp.StatusCode)
	}
	var raw []struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
		Draft       bool   `json:"draft"`
		Prerelease  bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]releaseInfo, 0, len(raw))
	for _, r := range raw {
		if r.Draft {
			continue
		}
		if !strings.HasPrefix(r.TagName, "server-v") {
			continue
		}
		out = append(out, releaseInfo{
			Tag:         r.TagName,
			Version:     strings.TrimPrefix(r.TagName, "server-"),
			URL:         r.HTMLURL,
			PublishedAt: r.PublishedAt,
		})
	}
	return out, nil
}

// compareSemver compares two "X.Y.Z" strings (ignoring leading "v"). Returns
// negative if a<b, 0 if equal, positive if a>b. Non-numeric segments compare
// as 0.
func compareSemver(a, b string) int {
	a = strings.TrimPrefix(a, "v")
	b = strings.TrimPrefix(b, "v")
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	for i := range 3 {
		var ai, bi int
		if i < len(aParts) {
			ai, _ = strconv.Atoi(aParts[i])
		}
		if i < len(bParts) {
			bi, _ = strconv.Atoi(bParts[i])
		}
		if ai != bi {
			return ai - bi
		}
	}
	return 0
}
