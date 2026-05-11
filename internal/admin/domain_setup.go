package admin

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SNIInfo holds information about detected SNI multiplexer.
type SNIInfo struct {
	Detected      bool   `json:"detected"`
	StreamConf    string `json:"stream_conf"`
	ProxyProtocol bool   `json:"proxy_protocol"`
}

// SubDomainStatus holds the current subscription domain state.
type SubDomainStatus struct {
	Domain       string `json:"domain"`
	SubURL       string `json:"sub_url"`
	SNIMode      bool   `json:"sni_mode"`
	CertExpires  string `json:"cert_expires,omitempty"`
	NginxOK      bool   `json:"nginx_ok"`
	Active       bool   `json:"active"`
}

// DetectSNI checks whether nginx has an SNI multiplexer (stream block with ssl_preread).
func DetectSNI() *SNIInfo {
	info := &SNIInfo{}

	out, err := exec.Command("grep", "-rl", "ssl_preread", "/etc/nginx/").CombinedOutput()
	if err != nil || len(bytes.TrimSpace(out)) == 0 {
		return info
	}

	files := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(files) == 0 {
		return info
	}

	info.Detected = true
	info.StreamConf = files[0]

	data, err := os.ReadFile(info.StreamConf)
	if err != nil {
		return info
	}
	if bytes.Contains(data, []byte("proxy_protocol on")) {
		info.ProxyProtocol = true
	}

	return info
}

// ReadSubDomain reads the subscription domain from the main env file.
func ReadSubDomain(configDir string) string {
	envPath := filepath.Join(configDir, "env")
	return GetEnvValue(envPath, "OLCRTC_SUB_DOMAIN")
}

// GetSubDomainStatus returns the current subscription domain status.
func GetSubDomainStatus(configDir string, publicIP string, subPort int) *SubDomainStatus {
	domain := ReadSubDomain(configDir)
	st := &SubDomainStatus{
		Domain: domain,
	}

	if domain == "" {
		st.SubURL = fmt.Sprintf("http://%s:%d/sub/{slug}", publicIP, subPort)
		return st
	}

	st.SubURL = fmt.Sprintf("https://%s/sub/{slug}", domain)
	st.Active = true

	// Check certificate expiry.
	certPath := fmt.Sprintf("/etc/letsencrypt/live/%s/fullchain.pem", domain)
	if _, err := os.Stat(certPath); err == nil {
		out, err := exec.Command("openssl", "x509", "-enddate", "-noout", "-in", certPath).Output()
		if err == nil {
			line := strings.TrimPrefix(strings.TrimSpace(string(out)), "notAfter=")
			t, err := time.Parse("Jan  2 15:04:05 2006 GMT", line)
			if err != nil {
				t, err = time.Parse("Jan 2 15:04:05 2006 GMT", line)
			}
			if err == nil {
				st.CertExpires = t.Format(time.RFC3339)
			}
		}
	}

	// Check nginx config exists and is valid.
	if _, err := os.Stat("/etc/nginx/sites-enabled/olcrtc-sub"); err == nil {
		out, err := exec.Command("nginx", "-t").CombinedOutput()
		st.NginxOK = err == nil && !bytes.Contains(out, []byte("failed"))
	}

	sni := DetectSNI()
	st.SNIMode = sni.Detected

	return st
}

// SetupSubDomain runs olcrtc-setup.sh --setup-domain to configure the
// subscription domain. It delegates all work (nginx, certbot, SNI) to
// the bash script so that CLI and UI behave identically.
func SetupSubDomain(domain string) ([]string, error) {
	var steps []string

	// Validate domain format.
	if domain == "" {
		return nil, fmt.Errorf("домен не указан")
	}
	if !isValidDomain(domain) {
		return nil, fmt.Errorf("некорректный домен: %s", domain)
	}

	// DNS pre-check.
	steps = append(steps, "Проверка DNS...")
	ips, err := net.LookupHost(domain)
	if err != nil {
		return steps, fmt.Errorf("не удалось разрешить DNS для домена: %w", err)
	}
	steps = append(steps, fmt.Sprintf("DNS разрешён: %v", ips))

	// Find setup script.
	scriptPath := findSetupScript()
	if scriptPath == "" {
		return steps, fmt.Errorf("olcrtc-setup.sh не найден")
	}
	steps = append(steps, "Скрипт найден: "+scriptPath)

	// Run setup script.
	steps = append(steps, "Запуск привязки домена...")
	cmd := exec.Command("sudo", "bash", scriptPath, "--setup-domain", domain)
	out, err := cmd.CombinedOutput()
	output := string(out)

	// Parse output into steps.
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			steps = append(steps, line)
		}
	}

	if err != nil {
		return steps, fmt.Errorf("ошибка привязки домена: %w\n%s", err, output)
	}

	return steps, nil
}

// RemoveSubDomain runs olcrtc-setup.sh --remove-domain.
func RemoveSubDomain() ([]string, error) {
	var steps []string

	scriptPath := findSetupScript()
	if scriptPath == "" {
		return steps, fmt.Errorf("olcrtc-setup.sh не найден")
	}

	steps = append(steps, "Запуск отвязки домена...")
	cmd := exec.Command("sudo", "bash", scriptPath, "--remove-domain")
	out, err := cmd.CombinedOutput()
	output := string(out)

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			steps = append(steps, line)
		}
	}

	if err != nil {
		return steps, fmt.Errorf("ошибка отвязки домена: %w\n%s", err, output)
	}

	return steps, nil
}

func findSetupScript() string {
	candidates := []string{
		"/usr/local/bin/olcrtc-setup.sh",
		"/opt/olcrtc/olcrtc-setup.sh",
	}
	// Also check next to the running binary.
	if ex, err := os.Executable(); err == nil {
		dir := filepath.Dir(ex)
		candidates = append(candidates, filepath.Join(dir, "olcrtc-setup.sh"))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func isValidDomain(domain string) bool {
	if strings.HasPrefix(domain, "*") {
		return false
	}
	// Reject IP addresses.
	if net.ParseIP(domain) != nil {
		return false
	}
	// Must have at least one dot.
	if !strings.Contains(domain, ".") {
		return false
	}
	// Only allow DNS-safe characters.
	for _, c := range domain {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
