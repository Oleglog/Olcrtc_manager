package domain

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// ApplyOwnPort binds the given domain using the "own-port" strategy:
//   - Acquires a Let's Encrypt cert via certbot (standalone if :80 free,
//     webroot via an additive nginx conf.d snippet if :80 owned by nginx).
//   - Installs a separate caddy systemd unit (`olcrtc-caddy.service`) that
//     terminates TLS on a non-standard port and reverse-proxies to the local
//     subscription server.
//   - Adds a certbot renewal deploy-hook to reload our caddy on cert refresh.
//
// The existing host configuration (3x-ui's nginx, user's caddy, etc.) is NOT
// modified — we only add new files in our own paths.
func ApplyOwnPort(ctx context.Context, profile *HostProfile, params BindParams, r Reporter) (*BindResult, error) {
	if r == nil {
		r = NopReporter{}
	}
	if profile == nil {
		profile = Detect(params.PublicIP)
	}

	if err := dnsCheck(params.Domain, params.PublicIP, r); err != nil {
		return nil, err
	}

	port := params.Port
	if port == 0 {
		port = pickOwnPort(profile)
	}
	r.Emit(Event{Kind: EventStep, StepID: "port", Title: fmt.Sprintf("Использую порт %d", port)})

	if !binaryExists("certbot") {
		r.Emit(Event{Kind: EventStep, StepID: "install_certbot", Title: "Установка certbot"})
		if err := aptInstall("certbot"); err != nil {
			return nil, fmt.Errorf("install certbot: %w", err)
		}
	}

	r.Emit(Event{Kind: EventStep, StepID: "cert", Title: "Получение сертификата Let's Encrypt"})
	certPath, keyPath, err := acquireCert(profile, params.Domain, params.Email, r)
	if err != nil {
		return nil, err
	}

	if caddyBinaryPath() == "" {
		r.Emit(Event{Kind: EventStep, StepID: "install_caddy", Title: "Установка caddy"})
		if err := aptInstall("caddy"); err != nil {
			return nil, fmt.Errorf("install caddy: %w", err)
		}
		if caddyBinaryPath() == "" {
			return nil, errors.New("caddy не найден после apt install — установите вручную и повторите")
		}
	}

	r.Emit(Event{Kind: EventStep, StepID: "config", Title: "Запись конфига caddy"})
	if err := writeOwnPortConfigs(params.Domain, port, params.SubPort, certPath, keyPath); err != nil {
		return nil, err
	}
	if err := writeRenewalHook(); err != nil {
		r.Emit(Event{Kind: EventLog, Message: fmt.Sprintf("warn: renewal hook не записан: %v", err)})
	}

	r.Emit(Event{Kind: EventStep, StepID: "start", Title: "Запуск olcrtc-caddy.service"})
	if err := systemctl("daemon-reload"); err != nil {
		return nil, fmt.Errorf("daemon-reload: %w", err)
	}
	if err := systemctl("enable", "olcrtc-caddy.service"); err != nil {
		return nil, fmt.Errorf("enable olcrtc-caddy: %w", err)
	}
	if err := systemctl("restart", "olcrtc-caddy.service"); err != nil {
		return nil, fmt.Errorf("start olcrtc-caddy: %w", err)
	}

	r.Emit(Event{Kind: EventStep, StepID: "verify", Title: "Проверка TLS-соединения"})
	if err := verifyOwnPort(params.Domain, port, 15*time.Second); err != nil {
		r.Emit(Event{Kind: EventLog, Message: fmt.Sprintf("warn: проверка TLS не прошла: %v", err)})
		// Don't fail hard — the systemd unit is up; user can debug from `systemctl status`.
	} else {
		r.Emit(Event{Kind: EventLog, Message: "TLS handshake успешен, сертификат валиден"})
	}

	subURL := fmt.Sprintf("https://%s:%d", params.Domain, port)
	r.Emit(Event{Kind: EventDone, Message: subURL, Strategy: StrategyOwnPort})

	return &BindResult{
		Strategy: StrategyOwnPort,
		Domain:   params.Domain,
		Port:     port,
		SubURL:   subURL,
		CertPath: certPath,
		KeyPath:  keyPath,
	}, nil
}

// UnbindOwnPort tears down the own-port setup: stops the systemd unit,
// removes our config files, and removes the nginx ACME snippet if present.
// The Let's Encrypt cert is left in place (next renewal will fail, that's OK).
func UnbindOwnPort(_ context.Context, r Reporter) error {
	if r == nil {
		r = NopReporter{}
	}
	r.Emit(Event{Kind: EventStep, StepID: "stop", Title: "Остановка olcrtc-caddy.service"})
	_ = run("systemctl", "disable", "--now", "olcrtc-caddy.service")
	_ = os.Remove(caddyUnitPath)
	_ = os.Remove(caddyfilePath)
	_ = systemctl("daemon-reload")

	// Remove nginx ACME snippet if present.
	if _, err := os.Stat(nginxACMEPath); err == nil {
		_ = os.Remove(nginxACMEPath)
		_ = run("nginx", "-t")
		_ = run("systemctl", "reload", "nginx")
	}

	_ = os.Remove(deployHookPath)

	r.Emit(Event{Kind: EventDone, Message: "Привязка домена удалена"})
	return nil
}

func writeOwnPortConfigs(domain string, port, subPort int, certPath, keyPath string) error {
	caddyBin := caddyBinaryPath()
	if caddyBin == "" {
		return errors.New("caddy binary not found")
	}
	if err := os.MkdirAll(caddyfileDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", caddyfileDir, err)
	}
	cfg := renderCaddyfile(domain, port, subPort, certPath, keyPath)
	if err := writeFileAtomic(caddyfilePath, []byte(cfg), 0o640); err != nil {
		return err
	}
	unit := renderCaddyUnit(caddyBin)
	if err := writeFileAtomic(caddyUnitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	return nil
}

func writeRenewalHook() error {
	if err := os.MkdirAll(deployHookDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", deployHookDir, err)
	}
	return writeFileAtomic(deployHookPath, []byte(renderRenewalHook()), 0o755)
}

func acquireCert(profile *HostProfile, domain, email string, r Reporter) (cert, key string, err error) {
	// Build common args.
	args := []string{"certonly", "--non-interactive", "--agree-tos"}
	if email != "" {
		args = append(args, "-m", email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	args = append(args, "-d", domain)

	cleanup := func() {}

	switch profile.Port80Owner {
	case "":
		args = append(args, "--standalone", "--preferred-challenges", "http")
	case ownerNginx:
		// Write additive acme-challenge snippet, reload nginx.
		if err := os.MkdirAll(acmeWebroot, 0o755); err != nil {
			return "", "", fmt.Errorf("mkdir %s: %w", acmeWebroot, err)
		}
		if err := writeFileAtomic(nginxACMEPath, []byte(renderNginxACME(domain)), 0o644); err != nil {
			return "", "", fmt.Errorf("write %s: %w", nginxACMEPath, err)
		}
		if err := run("nginx", "-t"); err != nil {
			_ = os.Remove(nginxACMEPath)
			return "", "", fmt.Errorf("nginx -t failed after writing %s: %w", nginxACMEPath, err)
		}
		if err := run("systemctl", "reload", "nginx"); err != nil {
			_ = os.Remove(nginxACMEPath)
			return "", "", fmt.Errorf("reload nginx: %w", err)
		}
		args = append(args, "--webroot", "-w", acmeWebroot)
		cleanup = func() { /* keep snippet — harmless and useful for renewals */ }
	case ownerCaddy:
		if !profile.CaddyManagedBySystemd {
			return "", "", errors.New(":80 занят caddy, и сервис не управляется systemd — " +
				"остановите caddy вручную и повторите.")
		}
		serviceName := resolveCaddyServiceName()
		if serviceName == "" {
			return "", "", errors.New(":80 занят caddy, но активный systemd unit не найден — " +
				"остановите caddy вручную и повторите.")
		}
		r.Emit(Event{Kind: EventStep, StepID: "stop_caddy",
			Title: fmt.Sprintf("Временно останавливаю %s для выдачи сертификата", serviceName)})
		if err := systemctl("stop", serviceName); err != nil {
			return "", "", fmt.Errorf("stop %s: %w", serviceName, err)
		}
		// Always try to restart caddy when this function returns, regardless of certbot result.
		defer func() {
			r.Emit(Event{Kind: EventStep, StepID: "start_caddy",
				Title: fmt.Sprintf("Запускаю %s обратно", serviceName)})
			if startErr := systemctl("start", serviceName); startErr != nil {
				r.Emit(Event{Kind: EventLog, Message: fmt.Sprintf(
					"warn: не удалось запустить %s: %v — запустите вручную", serviceName, startErr)})
			}
		}()
		if err := waitPortFree(80, 5*time.Second); err != nil {
			return "", "", fmt.Errorf("после остановки %s порт :80 не освободился: %w", serviceName, err)
		}
		args = append(args, "--standalone", "--preferred-challenges", "http")
	default:
		return "", "", fmt.Errorf(":80 занят процессом %q — автоматическая выдача сертификата не поддержана", profile.Port80Owner)
	}

	r.Emit(Event{Kind: EventLog, Message: "certbot " + strings.Join(args, " ")})
	if err := run("certbot", args...); err != nil {
		cleanup()
		return "", "", fmt.Errorf("certbot: %w", err)
	}

	cert = letsEncryptLive + "/" + domain + "/fullchain.pem"
	key = letsEncryptLive + "/" + domain + "/privkey.pem"
	if _, err := os.Stat(cert); err != nil {
		return "", "", fmt.Errorf("cert не найден после certbot: %s", cert)
	}
	return cert, key, nil
}

func dnsCheck(domain, publicIP string, r Reporter) error {
	r.Emit(Event{Kind: EventStep, StepID: "dns", Title: "Проверка DNS для " + domain})
	ips, err := net.LookupHost(domain)
	if err != nil {
		return fmt.Errorf("DNS lookup для %s не удался: %w", domain, err)
	}
	if publicIP == "" {
		return nil // skip mismatch check if we don't know our IP
	}
	for _, ip := range ips {
		if ip == publicIP {
			return nil
		}
	}
	return fmt.Errorf("DNS A-запись для %s указывает на %v, а ожидалось %s — обновите DNS и повторите",
		domain, ips, publicIP)
}

// pickOwnPort selects a free TCP port for our caddy, preferring 8444.
// Falls back to 8445, 8446, ..., 8460 then 9444, 9445, ..., 9460.
func pickOwnPort(profile *HostProfile) int {
	candidates := []int{OwnPortDefault}
	for p := OwnPortDefault + 1; p <= 8460; p++ {
		candidates = append(candidates, p)
	}
	for p := 9444; p <= 9460; p++ {
		candidates = append(candidates, p)
	}
	for _, p := range candidates {
		if portIsFree(p) && !portUsedByProfile(profile, p) {
			return p
		}
	}
	return OwnPortDefault
}

func portIsFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// waitPortFree polls until the given local TCP port becomes free, or until
// the timeout elapses. Used after stopping an external service that owns
// the port so that certbot --standalone can bind to it.
func waitPortFree(port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if portIsFree(port) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("port :%d still busy after %s", port, timeout)
}

// resolveCaddyServiceName returns the active caddy systemd unit name, or
// "" if no caddy service is currently active. Tries common unit names that
// distros use for the upstream caddy package.
func resolveCaddyServiceName() string {
	for _, name := range []string{"caddy.service", "caddy"} {
		if isSystemdServiceActive(name) {
			return name
		}
	}
	return ""
}

func portUsedByProfile(profile *HostProfile, port int) bool {
	if profile == nil {
		return false
	}
	if profile.RealityDestPort == port {
		return true
	}
	return false
}

// verifyOwnPort waits for the listener on domain:port to be reachable and
// then performs a real TLS handshake validated against the system trust
// store. A self-signed or caddy-internal certificate fails the check loudly
// so the bind result doesn't lie about the cert state.
func verifyOwnPort(domain string, port int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	addr := net.JoinHostPort(domain, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: 2 * time.Second}

	// Phase 1: wait for TCP listener to come up.
	var lastDialErr error
	for time.Now().Before(deadline) {
		conn, err := dialer.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			lastDialErr = nil
			break
		}
		lastDialErr = err
		time.Sleep(500 * time.Millisecond)
	}
	if lastDialErr != nil {
		return fmt.Errorf("не удалось подключиться к %s за %s: %w", addr, timeout, lastDialErr)
	}

	// Phase 2: full TLS handshake with verification against the system trust store.
	tlsCfg := &tls.Config{
		ServerName: domain,
		MinVersion: tls.VersionTLS12,
	}
	conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("TLS-хэндшейк с %s не прошёл: %w (сертификат невалиден/self-signed — проверьте /etc/letsencrypt/live/%s/ и лог certbot)", addr, err, domain)
	}
	state := conn.ConnectionState()
	_ = conn.Close()

	// Sanity check: reject obviously-wrong issuers (caddy internal CA).
	if len(state.PeerCertificates) == 0 {
		return fmt.Errorf("%s не прислал сертификат", addr)
	}
	issuer := strings.ToLower(state.PeerCertificates[0].Issuer.CommonName)
	if strings.Contains(issuer, "caddy") || strings.Contains(issuer, "local authority") {
		return fmt.Errorf("%s отдаёт internal-CA сертификат (issuer=%q) — Caddyfile не использует Let's Encrypt файлы",
			addr, state.PeerCertificates[0].Issuer.CommonName)
	}
	return nil
}
