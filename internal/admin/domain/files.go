package domain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Filesystem paths for the own-port strategy.
const (
	OwnPortDefault = 8444

	caddyfileDir  = "/etc/olcrtc/caddy"
	caddyfilePath = "/etc/olcrtc/caddy/Caddyfile"
	caddyUnitPath = "/etc/systemd/system/olcrtc-caddy.service"

	nginxACMEPath = "/etc/nginx/conf.d/olcrtc-acme.conf"
	acmeWebroot   = "/var/www/olcrtc-acme"

	deployHookDir  = "/etc/letsencrypt/renewal-hooks/deploy"
	deployHookPath = "/etc/letsencrypt/renewal-hooks/deploy/olcrtc-caddy-reload"

	letsEncryptLive = "/etc/letsencrypt/live"
)

// renderCaddyfile returns the contents of /etc/olcrtc/caddy/Caddyfile for
// the own-port strategy. We disable auto_https and pin certbot-managed certs
// because Caddy doesn't manage ACME for non-:443 listeners reliably.
func renderCaddyfile(domain string, port, subPort int, certPath, keyPath string) string {
	var sb strings.Builder
	sb.WriteString("{\n")
	sb.WriteString("\tadmin off\n")
	sb.WriteString("\tauto_https off\n")
	sb.WriteString("\tstorage file_system {\n")
	sb.WriteString("\t\troot /var/lib/olcrtc/caddy\n")
	sb.WriteString("\t}\n")
	sb.WriteString("}\n\n")
	fmt.Fprintf(&sb, "%s:%d {\n", domain, port)
	fmt.Fprintf(&sb, "\ttls %s %s\n", certPath, keyPath)
	fmt.Fprintf(&sb, "\treverse_proxy 127.0.0.1:%d\n", subPort)
	sb.WriteString("}\n")
	return sb.String()
}

// renderCaddyUnit returns the contents of /etc/systemd/system/olcrtc-caddy.service.
// We use the system `caddy` binary (must already be present at /usr/bin/caddy or
// /usr/local/bin/caddy) — caddyBinaryPath() resolves it.
func renderCaddyUnit(caddyBin string) string {
	return `[Unit]
Description=olcRTC caddy reverse proxy (own port)
Documentation=https://github.com/openlibrecommunity/olcrtc
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=notify
ExecStart=` + caddyBin + ` run --config ` + caddyfilePath + ` --adapter caddyfile
ExecReload=` + caddyBin + ` reload --config ` + caddyfilePath + ` --adapter caddyfile --force
TimeoutStopSec=5s
LimitNOFILE=1048576
PrivateTmp=true
ProtectSystem=full
AmbientCapabilities=CAP_NET_BIND_SERVICE
Restart=on-failure
RestartSec=5s
StateDirectory=olcrtc/caddy

[Install]
WantedBy=multi-user.target
`
}

// renderNginxACME returns the contents of /etc/nginx/conf.d/olcrtc-acme.conf.
// This block is **additive** — it does NOT touch any existing nginx config.
// It only catches requests for our domain's ACME challenge path.
func renderNginxACME(domain string) string {
	return `# Managed by olcrtc — do not edit.
# Serves Let's Encrypt HTTP-01 challenges for ` + domain + `.
server {
    listen 80;
    listen [::]:80;
    server_name ` + domain + `;

    location ^~ /.well-known/acme-challenge/ {
        default_type "text/plain";
        root ` + acmeWebroot + `;
    }

    location = /.well-known/acme-challenge/ {
        return 404;
    }
}
`
}

// renderRenewalHook returns the contents of the certbot renewal deploy hook.
// Reloads our caddy unit after a successful renewal so it picks up the new cert.
func renderRenewalHook() string {
	return `#!/bin/sh
# Managed by olcrtc — do not edit.
# Reloads olcrtc-caddy.service after Let's Encrypt renews the certificate.
systemctl reload olcrtc-caddy.service 2>/dev/null || true
`
}

// caddyBinaryPath returns the absolute path to the system caddy binary, or "" if
// not found.
func caddyBinaryPath() string {
	for _, p := range []string{"/usr/bin/caddy", "/usr/local/bin/caddy", "/usr/sbin/caddy"} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// writeFileAtomic writes data to path via a tempfile + rename. Returns the
// final path on success.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp.*")
	if err != nil {
		return fmt.Errorf("tempfile: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("rename %s -> %s: %w", tmpName, path, err)
	}
	return nil
}
