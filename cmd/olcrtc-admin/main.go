package main

import (
	"context"
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
		domain    = flag.String("domain", "", "Domain for Let's Encrypt (empty = self-signed)")
		subPort   = flag.Int("sub-port", 2096, "Subscription API port for proxying")
		tlsDir    = flag.String("tls-dir", "/var/lib/olcrtc/admin-tls", "TLS certificates directory")
		acmeEmail = flag.String("acme-email", "", "Email for Let's Encrypt account")
		configDir = flag.String("config-dir", "/etc/olcrtc", "Directory with instance env files")
		showCreds = flag.Bool("show-credentials", false, "Show admin login credentials and exit")
	)
	flag.Parse()

	*domain = strings.Trim(*domain, `"' `)

	if *showCreds {
		u, p, err := admin.ReadAdminCredentials(*configDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Credentials not found:", err)
			os.Exit(1)
		}
		fmt.Printf("Username: %s\nPassword: %s\n", u, p)
		os.Exit(0)
	}

	username, password, err := admin.ReadAdminCredentials(*configDir)
	if err != nil || username == "" || password == "" {
		username = "admin"
		password = "admin"
		if err := admin.WriteAdminEnv(*configDir, 0, username, password, *domain, *subPort); err != nil {
			log.Fatalf("Failed to write admin.env: %v", err)
		}
	}

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
			if err := admin.WriteAdminEnv(*configDir, *port, username, password, *domain, *subPort); err != nil {
				log.Fatalf("Failed to save admin.env: %v", err)
			}
		}
	}

	publicIP := getPublicIP()

	srv := admin.NewServer(admin.Config{
		Port:      *port,
		Username:  username,
		Password:  password,
		Domain:    *domain,
		SubPort:   *subPort,
		TLSDir:    *tlsDir,
		ACMEEmail: *acmeEmail,
		ConfigDir: *configDir,
		PublicIP:  publicIP,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("olcrtc-admin starting on https://%s:%d (user: %s)", publicIP, *port, username)
	if err := srv.Start(ctx); err != nil {
		log.Printf("Server error: %v", err)
	}
}

func getPublicIP() string {
	resp, err := admin.HTTPGetWithTimeout("https://api.ipify.org", 3*time.Second)
	if err == nil {
		ip := strings.TrimSpace(string(resp))
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
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
