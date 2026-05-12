package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/admin"
	"github.com/openlibrecommunity/olcrtc/internal/admin/domain"
)

func main() {
	// Subcommands.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "bind":
			os.Exit(runBind(os.Args[2:]))
		case "unbind":
			os.Exit(runUnbind(os.Args[2:]))
		case "detect":
			os.Exit(runDetect(os.Args[2:]))
		}
	}

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

	// Read optional domain-port / domain-strategy from admin.env.
	domPort := 0
	domStrategy := ""
	{
		envPath := *configDir + "/admin.env"
		vals := admin.ReadInstanceEnv(envPath)
		if v, ok := vals["OLCRTC_DOMAIN_PORT"]; ok {
			domPort, _ = strconv.Atoi(v)
		}
		domStrategy = vals["OLCRTC_DOMAIN_STRATEGY"]
	}

	srv := admin.NewServer(admin.Config{
		Port:           *port,
		Token:          *token,
		Domain:         *domain,
		SubPort:        *subPort,
		TLSDir:         *tlsDir,
		ACMEEmail:      *acmeEmail,
		ConfigDir:      *configDir,
		PublicIP:       publicIP,
		DomainPort:     domPort,
		DomainStrategy: domStrategy,
	})

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("olcrtc-admin starting on https://%s:%d", publicIP, *port)
	if err := srv.Start(ctx); err != nil {
		log.Fatalf("Server error: %v", err)
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

// ── CLI Subcommands ──────────────────────────────────────────────────────────

// runBind implements `olcrtc-admin bind --domain X --email Y [--strategy S] [--port P] [--sub-port N]`.
func runBind(args []string) int {
	fs := flag.NewFlagSet("bind", flag.ExitOnError)
	d := fs.String("domain", "", "Domain to bind (required)")
	email := fs.String("email", "", "ACME email (recommended)")
	strat := fs.String("strategy", "auto", "Strategy: auto, clean, own-port, sni-mux")
	port := fs.Int("port", 0, "Public port for own-port strategy (0=auto)")
	subPort := fs.Int("sub-port", 0, "Local subscription-server port (0=read from admin.env)")
	configDir := fs.String("config-dir", "/etc/olcrtc", "Config directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *d == "" {
		fmt.Fprintln(os.Stderr, "Usage: olcrtc-admin bind --domain DOMAIN [--email EMAIL] [--strategy auto|own-port|clean]")
		return 1
	}

	// Resolve sub-port from admin.env if not given.
	sp := *subPort
	if sp == 0 {
		envPath := *configDir + "/admin.env"
		vals := admin.ReadInstanceEnv(envPath)
		if v, ok := vals["OLCRTC_SUB_PORT"]; ok {
			sp, _ = strconv.Atoi(v)
		}
		if sp == 0 {
			sp = 2096
		}
	}

	reporter := domain.FuncReporter(func(ev domain.Event) {
		switch ev.Kind {
		case domain.EventStep:
			fmt.Printf("  [%s] %s\n", ev.StepID, ev.Title)
		case domain.EventLog:
			fmt.Printf("  → %s\n", ev.Message)
		case domain.EventError:
			fmt.Fprintf(os.Stderr, "  ✗ %s\n", ev.Message)
		case domain.EventDone:
			fmt.Printf("  ✓ %s\n", ev.Message)
		}
	})

	res, err := domain.Apply(context.Background(), domain.BindParams{
		Domain:   *d,
		Email:    *email,
		Strategy: *strat,
		SubPort:  sp,
		PublicIP: getPublicIP(),
		Port:     *port,
	}, reporter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bind failed: %v\n", err)
		return 1
	}

	// Persist to admin.env.
	_ = admin.SetAdminEnvKey(*configDir, "OLCRTC_ADMIN_DOMAIN", res.Domain)
	_ = admin.SetAdminEnvKey(*configDir, "OLCRTC_DOMAIN_PORT", strconv.Itoa(res.Port))
	_ = admin.SetAdminEnvKey(*configDir, "OLCRTC_DOMAIN_STRATEGY", res.Strategy)

	fmt.Printf("\nDomain bound: %s\n", res.SubURL)
	fmt.Println("Restart olcrtc-admin to pick up the new domain in subscription URLs:")
	fmt.Println("  sudo systemctl restart olcrtc-admin")
	return 0
}

// runUnbind implements `olcrtc-admin unbind`.
func runUnbind(args []string) int {
	fs := flag.NewFlagSet("unbind", flag.ExitOnError)
	configDir := fs.String("config-dir", "/etc/olcrtc", "Config directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	reporter := domain.FuncReporter(func(ev domain.Event) {
		if ev.Kind == domain.EventStep || ev.Kind == domain.EventDone {
			fmt.Printf("  [%s] %s\n", ev.StepID, ev.Title)
		}
	})

	envPath := *configDir + "/admin.env"
	vals := admin.ReadInstanceEnv(envPath)
	strategy := vals["OLCRTC_DOMAIN_STRATEGY"]

	if strategy == domain.StrategyOwnPort {
		if err := domain.UnbindOwnPort(context.Background(), reporter); err != nil {
			fmt.Fprintf(os.Stderr, "unbind failed: %v\n", err)
			return 1
		}
	}

	_ = admin.SetAdminEnvKey(*configDir, "OLCRTC_ADMIN_DOMAIN", "")
	_ = admin.DeleteAdminEnvKey(*configDir, "OLCRTC_DOMAIN_PORT")
	_ = admin.DeleteAdminEnvKey(*configDir, "OLCRTC_DOMAIN_STRATEGY")

	fmt.Println("Domain unbound. Restart olcrtc-admin:")
	fmt.Println("  sudo systemctl restart olcrtc-admin")
	return 0
}

// runDetect implements `olcrtc-admin detect` — prints the host profile as JSON.
func runDetect(_ []string) int {
	profile := domain.Detect(getPublicIP())
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(profile); err != nil {
		fmt.Fprintf(os.Stderr, "encode: %v\n", err)
		return 1
	}
	return 0
}
