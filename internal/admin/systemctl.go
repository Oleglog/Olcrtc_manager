package admin

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var (
	systemdUnitDir     = "/etc/systemd/system"
	systemctlRemoveRun = SystemctlRun
)

// SystemctlStatus holds systemd unit status.
type SystemctlStatus struct {
	State  string `json:"state"`
	Active string `json:"active"`
	Uptime string `json:"uptime"`
}

// SystemctlRun runs a systemctl command and returns combined output.
func SystemctlRun(args ...string) (string, error) {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// SystemctlStart starts a service.
func SystemctlStart(service string) error {
	_, err := SystemctlRun("start", service)
	return err
}

// SystemctlStop stops a service.
func SystemctlStop(service string) error {
	_, err := SystemctlRun("stop", service)
	return err
}

// SystemctlRemoveInstance permanently removes one instantiated server unit.
// The shared olcrtc-server@.service template and other instance IDs are kept.
func SystemctlRemoveInstance(service string) error {
	const prefix = "olcrtc-server@"
	const suffix = ".service"
	if !strings.HasPrefix(service, prefix) || !strings.HasSuffix(service, suffix) {
		return fmt.Errorf("invalid instance service %q", service)
	}
	idText := strings.TrimSuffix(strings.TrimPrefix(service, prefix), suffix)
	id, err := strconv.Atoi(idText)
	if err != nil || id <= 0 || strconv.Itoa(id) != idText {
		return fmt.Errorf("invalid instance service %q", service)
	}

	// systemctl may report an error for an already-disabled or already-cleaned
	// unit. Exact filesystem cleanup below is authoritative for final deletion.
	_, _ = systemctlRemoveRun("disable", "--now", service)
	_, _ = systemctlRemoveRun("clean", "--what=state", service)
	_, _ = systemctlRemoveRun("reset-failed", service)

	var cleanupErr error
	for _, path := range []string{
		filepath.Join(systemdUnitDir, service),
		filepath.Join(systemdUnitDir, "multi-user.target.wants", service),
		filepath.Join(systemdUnitDir, service+".d"),
	} {
		if err := os.RemoveAll(path); err != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("remove %s: %w", path, err))
		}
	}
	if _, err := systemctlRemoveRun("daemon-reload"); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("systemctl daemon-reload: %w", err))
	}
	return cleanupErr
}

// SystemctlRestart restarts a service.
func SystemctlRestart(service string) error {
	_, err := SystemctlRun("restart", service)
	return err
}

// SystemctlStatusInfo returns status info for a service.
func SystemctlStatusInfo(service string) (*SystemctlStatus, error) {
	out, err := SystemctlRun("show", service,
		"--property=ActiveState",
		"--property=SubState",
		"--property=ActiveEnterTimestamp")
	if err != nil {
		return nil, err
	}

	st := &SystemctlStatus{}
	lines := strings.Split(out, "\n")
	var enterTime time.Time
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "ActiveState=") {
			st.Active = strings.TrimPrefix(line, "ActiveState=")
		}
		if strings.HasPrefix(line, "SubState=") {
			st.State = strings.TrimPrefix(line, "SubState=")
		}
		if strings.HasPrefix(line, "ActiveEnterTimestamp=") {
			ts := strings.TrimPrefix(line, "ActiveEnterTimestamp=")
			if ts != "" {
				enterTime, _ = time.Parse("Mon 2006-01-02 15:04:05 MST", ts)
			}
		}
	}

	if st.State == "" {
		st.State = st.Active
	}

	if !enterTime.IsZero() {
		st.Uptime = formatDuration(time.Since(enterTime))
	}
	return st, nil
}

// JournalctlLogs returns recent log lines for a service.
func JournalctlLogs(service string, lines int) (string, error) {
	if lines <= 0 {
		lines = 100
	}
	cmd := exec.Command("journalctl", "-u", service, "--no-pager", "-n", fmt.Sprintf("%d", lines))
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// formatDuration converts a duration to a human-readable string.
func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// GetSystemUptime returns system uptime.
func GetSystemUptime() (string, error) {
	out, err := exec.Command("cat", "/proc/uptime").Output()
	if err != nil {
		return "", err
	}
	parts := strings.Fields(string(out))
	if len(parts) == 0 {
		return "", fmt.Errorf("unable to read uptime")
	}
	sec, err := parseFloat(parts[0])
	if err != nil {
		return "", err
	}
	return formatDuration(time.Duration(sec) * time.Second), nil
}

func parseFloat(s string) (float64, error) {
	var sec float64
	_, err := fmt.Sscanf(s, "%f", &sec)
	return sec, err
}

// GetHostname returns the system hostname.
func GetHostname() string {
	out, _ := exec.Command("hostname").Output()
	return strings.TrimSpace(string(out))
}

// GetOSInfo returns OS description.
func GetOSInfo() string {
	if out, err := exec.Command("bash", "-c", "source /etc/os-release && echo \"$PRETTY_NAME\"").Output(); err == nil {
		return strings.TrimSpace(string(out))
	}
	return "Linux"
}

// ListUsedPorts returns a list of used ports (ss output).
func ListUsedPorts() (string, error) {
	out, err := exec.Command("ss", "-tlnp").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
