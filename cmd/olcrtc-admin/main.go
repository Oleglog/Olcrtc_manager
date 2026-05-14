package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/admin"
)

func main() {
	var (
		port      = flag.Int("port", 0, "HTTPS port (0 = auto)")
		token     = flag.String("token", "", "Bearer token for API auth")
		domain    = flag.String("domain", "", "Domain for Let's Encrypt (empty = self-signed)")
		subPort   = flag.Int("sub-port", 2096, "Subscription API port for proxying")
		tlsDir    = flag.String("tls-dir", "/var/lib/olcrtc/admin-tls", "TLS certificates directory")
		acmeEmail = flag.String("acme-email", "", "Email for Let's Encrypt account")
		configDir = flag.String("config-dir", "/etc/olcrtc", "Directory with instance env files")
		showToken = flag.Bool("show-token", false, "Show token from admin.env and exit")
	)
	flag.Parse()

	// Strip quotes from domain (systemd may pass quoted empty string).
	*domain = strings.Trim(*domain, `"' `)

	if *showToken {
		tok, err := admin.ReadAdminToken(*configDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Token not found:", err)
			os.Exit(1)
		}
		fmt.Println(tok)
		os.Exit(0)
	}

	// Ensure token is available.
	if *token == "" {
		tok, err := admin.ReadAdminToken(*configDir)
		if err != nil {
			// Generate a new token and write admin.env.
			tok = generateToken()
			if err := admin.WriteAdminEnv(*configDir, 0, tok, *domain, *subPort); err != nil {
				log.Fatalf("Failed to write admin.env: %v", err)
			}
		}
		*token = tok
	}

	// Auto-pick port if not specified.
	if *port == 0 {
		savedPort, _ := admin.ReadAdminPort(*configDir)
		if savedPort > 0 {
			*port = savedPort
		} else {
			p, err := admin.FindFreePort()
			if err != nil {
				log.Fatalf("Failed to find free port: %v", err)
			}
			*port = p
			if err := admin.WriteAdminEnv(*configDir, *port, *token, *domain, *subPort); err != nil {
				log.Fatalf("Failed to save admin.env: %v", err)
			}
		}
	}

	publicIP := getPublicIP()

	srv := admin.NewServer(admin.Config{
		Port:      *port,
		Token:     *token,
		Domain:    *domain,
		SubPort:   *subPort,
		TLSDir:    *tlsDir,
		ACMEEmail: *acmeEmail,
		ConfigDir: *configDir,
		PublicIP:  publicIP,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("olcrtc-admin starting on https://%s:%d", publicIP, *port)
	if err := srv.Start(ctx); err != nil {
		log.Printf("Server error: %v", err)
	}
}

func generateToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func getPublicIP() string {
	resp, err := admin.HTTPGetWithTimeout("https://api.ipify.org", 3*time.Second)
	if err == nil {
		ip := strings.TrimSpace(string(resp))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	// Fallback: try to get local non-loopback IP.
	addrs, err := net.InterfaceAddrs()
	if err == nil {
		for _, a := range addrs {
			if ipNet, ok := a.(*net.IPNet); ok && !ipNet.IP.IsLoopback() && ipNet.IP.To4() != nil {
				return ipNet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}
