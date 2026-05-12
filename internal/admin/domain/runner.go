package domain

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// run executes a command and returns combined stdout+stderr on error.
// Used for "fire and forget" steps where we only care about success/failure.
func run(name string, args ...string) error {
	return runCtx(context.Background(), name, args...)
}

// runCtx is the context-aware variant of run.
func runCtx(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // installer/admin tools invoke fixed binary names
	out, err := cmd.CombinedOutput()
	if err != nil {
		s := strings.TrimSpace(string(out))
		if s == "" {
			return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
		}
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, s)
	}
	return nil
}

// runOut executes a command and returns its stdout (trimmed) plus any error.
func runOut(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // installer/admin tools invoke fixed binary names
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// systemctl is a thin wrapper that retries once after daemon-reload on the
// "Unit ... not found" race that systemd sometimes shows when a new unit was
// just written.
func systemctl(args ...string) error {
	if err := run("systemctl", args...); err == nil {
		return nil
	}
	// Retry once after daemon-reload.
	_ = run("systemctl", "daemon-reload")
	time.Sleep(200 * time.Millisecond)
	return run("systemctl", args...)
}

// aptInstall runs apt-get install -y in non-interactive mode.
func aptInstall(packages ...string) error {
	if len(packages) == 0 {
		return nil
	}
	args := []string{"install", "-y", "--no-install-recommends"}
	args = append(args, packages...)
	// Best-effort non-interactive flags.
	cmd := exec.Command("apt-get", args...) //nolint:gosec // fixed binary, package list is from our code
	cmd.Env = append(cmd.Env,
		"DEBIAN_FRONTEND=noninteractive",
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get install %v: %w: %s", packages, err, strings.TrimSpace(string(out)))
	}
	return nil
}
