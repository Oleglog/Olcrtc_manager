package admin

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ReadAdminToken reads the admin token from admin.env.
func ReadAdminToken(configDir string) (string, error) {
	f := filepath.Join(configDir, "admin.env")
	return readEnvValue(f, "OLCRTC_ADMIN_TOKEN")
}

// ReadAdminPort reads the admin port from admin.env.
func ReadAdminPort(configDir string) (int, error) {
	f := filepath.Join(configDir, "admin.env")
	s, err := readEnvValue(f, "OLCRTC_ADMIN_PORT")
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(s)
}

// WriteAdminEnv writes the admin environment file, preserving any
// already-present extra keys (OLCRTC_DOMAIN_PORT, OLCRTC_DOMAIN_STRATEGY, …).
func WriteAdminEnv(configDir string, port int, token, domain string, subPort int) error {
	f := filepath.Join(configDir, "admin.env")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	existing := ReadInstanceEnv(f)
	existing["OLCRTC_ADMIN_PORT"] = fmt.Sprintf("%d", port)
	existing["OLCRTC_ADMIN_TOKEN"] = token
	existing["OLCRTC_ADMIN_DOMAIN"] = domain
	existing["OLCRTC_SUB_PORT"] = fmt.Sprintf("%d", subPort)

	var sb strings.Builder
	// Keep canonical ordering for the four well-known keys, then sort extras.
	for _, k := range []string{"OLCRTC_ADMIN_PORT", "OLCRTC_ADMIN_TOKEN", "OLCRTC_ADMIN_DOMAIN", "OLCRTC_SUB_PORT"} {
		fmt.Fprintf(&sb, "%s=%s\n", k, existing[k])
		delete(existing, k)
	}
	// Stable order for the rest.
	keys := make([]string, 0, len(existing))
	for k := range existing {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", k, existing[k])
	}
	return os.WriteFile(f, []byte(sb.String()), 0644)
}

// SetAdminEnvKey upserts a single key in admin.env, preserving all other keys.
// Used to write domain-related keys (OLCRTC_DOMAIN_PORT, OLCRTC_DOMAIN_STRATEGY)
// without touching token/port settings.
func SetAdminEnvKey(configDir, key, value string) error {
	f := filepath.Join(configDir, "admin.env")
	existing := ReadInstanceEnv(f)
	existing[key] = value
	return WriteInstanceEnv(f, existing)
}

// DeleteAdminEnvKey removes a key from admin.env (no-op if absent).
func DeleteAdminEnvKey(configDir, key string) error {
	f := filepath.Join(configDir, "admin.env")
	existing := ReadInstanceEnv(f)
	if _, ok := existing[key]; !ok {
		return nil
	}
	delete(existing, key)
	// Rewrite from scratch since WriteInstanceEnv only upserts.
	var sb strings.Builder
	keys := make([]string, 0, len(existing))
	for k := range existing {
		keys = append(keys, k)
	}
	sortStrings(keys)
	for _, k := range keys {
		fmt.Fprintf(&sb, "%s=%s\n", k, existing[k])
	}
	if err := os.MkdirAll(filepath.Dir(f), 0755); err != nil {
		return err
	}
	return os.WriteFile(f, []byte(sb.String()), 0644)
}

func sortStrings(s []string) {
	// Simple insertion sort; the slice is tiny.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// ReadInstanceEnv reads all values from an instance env file.
func ReadInstanceEnv(path string) map[string]string {
	vals := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return vals
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			vals[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return vals
}

// WriteInstanceEnv writes key=value pairs to an instance env file.
func WriteInstanceEnv(path string, vals map[string]string) error {
	existing := ReadInstanceEnv(path)
	for k, v := range vals {
		existing[k] = v
	}

	var sb strings.Builder
	for k, v := range existing {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

// SetEnvValue sets a single key in an env file.
func SetEnvValue(path, key, value string) error {
	return WriteInstanceEnv(path, map[string]string{key: value})
}

// GetEnvValue reads a single key from an env file.
func GetEnvValue(path, key string) string {
	vals := ReadInstanceEnv(path)
	return vals[key]
}

// ListInstances returns all instance IDs (0 for main, plus extras).
func ListInstances(configDir string) ([]int, error) {
	var ids []int
	mainEnv := filepath.Join(configDir, "env")
	if _, err := os.Stat(mainEnv); err == nil {
		ids = append(ids, 0)
	}

	entries, err := os.ReadDir(configDir)
	if err != nil {
		return ids, nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if n, err := strconv.Atoi(e.Name()); err == nil && n > 0 {
			envPath := filepath.Join(configDir, e.Name(), "env")
			if _, err := os.Stat(envPath); err == nil {
				ids = append(ids, n)
			}
		}
	}
	return ids, nil
}

// InstanceEnvPath returns the env file path for an instance.
func InstanceEnvPath(configDir string, id int) string {
	if id == 0 {
		return filepath.Join(configDir, "env")
	}
	return filepath.Join(configDir, fmt.Sprintf("%d", id), "env")
}

// InstanceKeyPath returns the key file path for an instance.
func InstanceKeyPath(configDir string, id int) string {
	if id == 0 {
		return filepath.Join(configDir, "key.hex")
	}
	return filepath.Join(configDir, fmt.Sprintf("%d", id), "key.hex")
}

// InstanceService returns the systemd service name for an instance.
func InstanceService(id int) string {
	if id == 0 {
		return "olcrtc-server.service"
	}
	return fmt.Sprintf("olcrtc-server@%d.service", id)
}

func readEnvValue(path, key string) (string, error) {
	vals := ReadInstanceEnv(path)
	v, ok := vals[key]
	if !ok {
		return "", fmt.Errorf("key %s not found in %s", key, path)
	}
	return v, nil
}
