package domain

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
)

// Known process owner names returned by parseSSOwner.
const (
	ownerOther  = "other"
	ownerNginx  = "nginx"
	ownerCaddy  = "caddy"
	ownerXUI    = "x-ui"
	ownerXray   = "xray"
	ownerOlcrtc = "olcrtc"
)

// Detect inspects the local host and returns a HostProfile describing
// installed services, port occupancy, and the list of strategies that
// can be used to bind a public domain.
//
// PublicIP is taken from the caller (admin already knows its own
// public IP); we pass it in to avoid duplicating the network call.
func Detect(publicIP string) *HostProfile {
	p := &HostProfile{PublicIP: publicIP}

	// Process / package presence.
	p.HasXUI = detectXUI()
	p.HasNginx = binaryExists("nginx")
	p.HasCaddy = binaryExists("caddy")
	p.HasCertbot = binaryExists("certbot")
	p.HasXray = detectXray()

	// Nginx stream + ssl_preread check (3x-ui-style SNI mux).
	if p.HasNginx {
		p.HasNginxStream = detectNginxStreamMux()
	}

	// Caddy systemd state.
	if p.HasCaddy {
		p.CaddyManagedBySystemd = isSystemdServiceActive("caddy.service") ||
			isSystemdServiceActive("caddy")
	}

	// Port occupancy.
	listeners := parseSocketListeners()
	p.Port80Owner = listenerForPort(listeners, 80)
	p.Port443Owner = listenerForPort(listeners, 443)

	// Reality dest port (best effort).
	p.RealityDestPort = detectRealityDestPort()

	// Build strategies.
	p.Strategies = buildStrategies(p)

	return p
}

// buildStrategies returns the ordered strategy list for the profile.
// All known strategies are included; Available/Recommended flags express
// what the UI should present.
func buildStrategies(p *HostProfile) []Strategy {
	port443Free := p.Port443Owner == ""
	hasNginxMux := p.HasXUI || p.HasNginxStream

	out := []Strategy{
		{
			Name:            StrategyClean,
			Title:           "Чистый :443 (nginx + certbot)",
			Description:     "Установит nginx + certbot, выпустит Let's Encrypt сертификат на :443. Клиентский URL — без порта.",
			Risk:            "low",
			Available:       port443Free && !hasNginxMux,
			Recommended:     port443Free && !hasNginxMux,
			SubscriptionURL: "https://{domain}/sub/{slug}",
		},
		{
			Name:  StrategyOwnPort,
			Title: "Отдельный порт (безопасно для 3x-ui)",
			Description: "Поднимет собственный caddy на нестандартном порту " +
				"(например :8444). Существующие сервисы не трогаются. " +
				"Клиентский URL содержит порт.",
			Risk:            "low",
			Available:       true,        // always available as a fallback
			Recommended:     hasNginxMux, // recommended on 3x-ui hosts
			SubscriptionURL: "https://{domain}:8444/sub/{slug}",
		},
		{
			Name:  StrategySNIMux,
			Title: "Инъекция в nginx stream (3x-ui)",
			Description: "Добавит SNI-правило в существующий nginx stream-блок. " +
				"Чистый URL, но риск повредить настройки 3x-ui/xray. " +
				"Требует backup и подтверждения.",
			Risk:        "high",
			Available:   hasNginxMux,
			Recommended: false, // never auto-recommended; user opt-in only
			UnavailableReason: func() string {
				if !hasNginxMux {
					return "nginx stream с ssl_preread не найден (нет 3x-ui SNI multiplexer)."
				}
				return ""
			}(),
			SubscriptionURL: "https://{domain}/sub/{slug}",
		},
	}

	// Make sure at most one strategy is marked Recommended.
	seen := false
	for i := range out {
		if out[i].Recommended {
			if seen {
				out[i].Recommended = false
			}
			seen = true
		}
	}
	// If nothing got recommended (edge case), default to own-port.
	if !seen {
		for i := range out {
			if out[i].Name == StrategyOwnPort {
				out[i].Recommended = true
				break
			}
		}
	}

	return out
}

// detectXUI returns true if the 3x-ui panel is present, by checking
// for the systemd unit OR the binary path.
func detectXUI() bool {
	if isSystemdServiceActive("x-ui") || isSystemdServiceActive("x-ui.service") {
		return true
	}
	// Common install paths.
	paths := []string{
		"/usr/local/x-ui/x-ui",
		"/usr/local/x-ui/bin/x-ui",
		"/etc/x-ui/x-ui.db",
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// detectXray returns true if a standalone xray service is present
// (separate from 3x-ui-bundled xray).
func detectXray() bool {
	if isSystemdServiceActive("xray") || isSystemdServiceActive("xray.service") {
		return true
	}
	if _, err := os.Stat("/usr/local/bin/xray"); err == nil {
		return true
	}
	if _, err := os.Stat("/etc/xray/config.json"); err == nil {
		return true
	}
	return false
}

// detectNginxStreamMux runs `nginx -T` and returns true if a stream {}
// block with `ssl_preread on` is found anywhere in the rendered config.
func detectNginxStreamMux() bool {
	out, err := runCmd("nginx", "-T")
	if err != nil {
		return false
	}
	return nginxOutputHasStreamMux(out)
}

// nginxOutputHasStreamMux is the pure-function half of detectNginxStreamMux,
// extracted for unit testing.
func nginxOutputHasStreamMux(nginxDump string) bool {
	// Must contain BOTH a stream {} context AND ssl_preread.
	// nginx -T output marks each loaded file with `# configuration file ...`
	// and inlines content. A simple containment check is sufficient.
	hasStream := strings.Contains(nginxDump, "stream {") ||
		strings.Contains(nginxDump, "stream{")
	hasPreread := strings.Contains(nginxDump, "ssl_preread on") ||
		strings.Contains(nginxDump, "ssl_preread  on") // tolerate extra space
	return hasStream && hasPreread
}

// SocketListener describes a single listener as reported by `ss -tlnp`.
type SocketListener struct {
	Port int
	// Owner is the simplified process name: "nginx", "caddy", "x-ui",
	// "xray", "olcrtc", or "other".
	Owner string
}

// parseSocketListeners reads `ss -tlnp` and returns one entry per listening
// TCP socket. Best-effort; returns empty slice on failure.
func parseSocketListeners() []SocketListener {
	out, err := runCmd("ss", "-tlnp")
	if err != nil {
		return nil
	}
	return parseSSOutput(out)
}

// parseSSOutput is the pure-function half of parseSocketListeners.
func parseSSOutput(ssOutput string) []SocketListener {
	var listeners []SocketListener
	lines := strings.Split(ssOutput, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "State") {
			continue
		}
		// Format: State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// Local address:port is field 3 (0-indexed) when State present, or 4 in some kernels.
		// Look for the first field containing ':'.
		var localAddr string
		for _, f := range fields {
			if strings.Contains(f, ":") && !strings.HasPrefix(f, "users:") {
				localAddr = f
				break
			}
		}
		if localAddr == "" {
			continue
		}
		// Split host:port from the right (handles IPv6 [::]:443).
		idx := strings.LastIndex(localAddr, ":")
		if idx < 0 {
			continue
		}
		portStr := localAddr[idx+1:]
		port := atoiSafe(portStr)
		if port == 0 {
			continue
		}
		// Owner from users:(("name",pid=...,...)).
		owner := ownerOther
		for _, f := range fields {
			if strings.HasPrefix(f, "users:") {
				owner = parseSSOwner(f)
				break
			}
		}
		// Also consider full line as a fallback.
		if owner == ownerOther {
			owner = parseSSOwner(line)
		}
		listeners = append(listeners, SocketListener{Port: port, Owner: owner})
	}
	return listeners
}

// parseSSOwner extracts a simplified owner name from `users:(("x-ui",pid=1060,fd=10))`.
func parseSSOwner(s string) string {
	// Find first occurrence of `(("name"`.
	start := strings.Index(s, `(("`)
	if start < 0 {
		return ownerOther
	}
	rest := s[start+3:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ownerOther
	}
	name := rest[:end]
	// Normalize.
	switch {
	case strings.HasPrefix(name, ownerNginx):
		return ownerNginx
	case name == ownerCaddy:
		return ownerCaddy
	case name == ownerXUI:
		return ownerXUI
	case name == ownerXray:
		return ownerXray
	case strings.HasPrefix(name, ownerOlcrtc):
		return ownerOlcrtc
	default:
		return name
	}
}

// listenerForPort returns the owner of the given port, or "" if not listening.
func listenerForPort(ls []SocketListener, port int) string {
	for _, l := range ls {
		if l.Port == port {
			return l.Owner
		}
	}
	return ""
}

// detectRealityDestPort reads common xray-config locations and extracts the
// Reality `dest` setting. Returns 0 if not found.
//
// 3x-ui (current) stores inbounds in SQLite — that case is NOT handled here
// (will be added when Tier-3 SNI mux is implemented). For now we look at
// flat JSON configs, which is enough to flag "Reality is in use" for the
// detect endpoint.
func detectRealityDestPort() int {
	paths := []string{
		"/usr/local/x-ui/bin/config.json",
		"/etc/xray/config.json",
		"/usr/local/etc/xray/config.json",
	}
	for _, p := range paths {
		data, err := os.ReadFile(p) //nolint:gosec // hardcoded path list
		if err != nil {
			continue
		}
		if port := parseRealityDestFromJSON(data); port != 0 {
			return port
		}
	}
	return 0
}

// parseRealityDestFromJSON walks inbounds[].streamSettings.realitySettings.dest
// and returns the port (or 0 if not found). dest can be "host:port" or just
// "port".
func parseRealityDestFromJSON(data []byte) int {
	var cfg struct {
		Inbounds []struct {
			StreamSettings struct {
				RealitySettings struct {
					Dest string `json:"dest"`
				} `json:"realitySettings"`
			} `json:"streamSettings"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return 0
	}
	for _, in := range cfg.Inbounds {
		dest := in.StreamSettings.RealitySettings.Dest
		if dest == "" {
			continue
		}
		// dest may be "host:port", "127.0.0.1:9443", or just "9443".
		if idx := strings.LastIndex(dest, ":"); idx >= 0 {
			return atoiSafe(dest[idx+1:])
		}
		return atoiSafe(dest)
	}
	return 0
}

// --- helpers ---

func binaryExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func isSystemdServiceActive(name string) bool {
	out, err := runCmd("systemctl", "is-active", "--quiet", name)
	if err == nil {
		return true
	}
	// is-active with --quiet returns exit code 0 if active; non-zero otherwise.
	// We treat any non-error as active; some systems may have stale output.
	_ = out
	// Fallback: parse non-quiet output.
	out, _ = runCmd("systemctl", "is-active", name)
	return strings.TrimSpace(out) == "active"
}

// runCmd executes a command and returns stdout. Stderr is discarded.
// Timeout is intentionally short; detect should be fast.
func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...) //nolint:gosec // helpers invoke only fixed binary names
	out, err := cmd.Output()
	return string(out), err //nolint:wrapcheck // pass-through to caller
}

func atoiSafe(s string) int {
	s = strings.TrimSpace(s)
	n := 0
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}
