package admin

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
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
	out, err := SystemctlRun("-u", service, "--no-pager", "-n", fmt.Sprintf("%d", lines))
	return out, err
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
