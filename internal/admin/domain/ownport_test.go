package domain

import (
	"strings"
	"testing"
)

func TestRenderCaddyfile_ContainsExpectedKeys(t *testing.T) {
	cfg := renderCaddyfile("example.com", 8444, 2097, "/etc/letsencrypt/live/example.com/fullchain.pem", "/etc/letsencrypt/live/example.com/privkey.pem")
	for _, want := range []string{
		"admin off",
		"auto_https off",
		"storage file_system",
		"example.com:8444",
		"tls /etc/letsencrypt/live/example.com/fullchain.pem /etc/letsencrypt/live/example.com/privkey.pem",
		"reverse_proxy 127.0.0.1:2097",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("Caddyfile missing %q\nfull:\n%s", want, cfg)
		}
	}
}

func TestRenderCaddyUnit_ContainsExpectedFields(t *testing.T) {
	u := renderCaddyUnit("/usr/bin/caddy")
	for _, want := range []string{
		"Type=notify",
		"ExecStart=/usr/bin/caddy run --config /etc/olcrtc/caddy/Caddyfile",
		"ExecReload=/usr/bin/caddy reload --config /etc/olcrtc/caddy/Caddyfile",
		"AmbientCapabilities=CAP_NET_BIND_SERVICE",
		"Restart=on-failure",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("systemd unit missing %q\nfull:\n%s", want, u)
		}
	}
}

func TestRenderNginxACME_ContainsDomain(t *testing.T) {
	got := renderNginxACME("sub.example.com")
	for _, want := range []string{
		"server_name sub.example.com",
		"listen 80",
		"location ^~ /.well-known/acme-challenge/",
		"root " + acmeWebroot,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("nginx ACME snippet missing %q\nfull:\n%s", want, got)
		}
	}
}

func TestRenderRenewalHook(t *testing.T) {
	got := renderRenewalHook()
	if !strings.HasPrefix(got, "#!/bin/sh") {
		t.Errorf("renewal hook should start with shebang, got: %q", got)
	}
	if !strings.Contains(got, "systemctl reload olcrtc-caddy.service") {
		t.Errorf("renewal hook should reload olcrtc-caddy")
	}
}

func TestChooseStrategy_AutoPicksRecommended(t *testing.T) {
	// 3x-ui host: own-port should be recommended.
	profile := &HostProfile{
		HasXUI:       true,
		Port443Owner: "nginx",
		Strategies: []Strategy{
			{Name: StrategyClean, Available: false, Recommended: false},
			{Name: StrategyOwnPort, Available: true, Recommended: true},
			{Name: StrategySNIMux, Available: true, Recommended: false},
		},
	}
	for _, in := range []string{"", "auto"} {
		got, err := chooseStrategy(profile, in)
		if err != nil {
			t.Fatalf("chooseStrategy(%q) error: %v", in, err)
		}
		if got != StrategyOwnPort {
			t.Errorf("chooseStrategy(%q) = %q, want %q", in, got, StrategyOwnPort)
		}
	}
}

func TestChooseStrategy_ExplicitMatchesAvailability(t *testing.T) {
	profile := &HostProfile{
		Strategies: []Strategy{
			{Name: StrategyClean, Available: false, UnavailableReason: ":443 занят"},
			{Name: StrategyOwnPort, Available: true},
			{Name: StrategySNIMux, Available: true},
		},
	}
	if got, err := chooseStrategy(profile, "own-port"); err != nil || got != StrategyOwnPort {
		t.Errorf("expected own-port, got %q err=%v", got, err)
	}
	if _, err := chooseStrategy(profile, "clean"); err == nil {
		t.Errorf("expected error for unavailable clean strategy")
	}
	if _, err := chooseStrategy(profile, "bogus"); err == nil {
		t.Errorf("expected error for unknown strategy")
	}
}

func TestPickOwnPort_DefaultWhenFree(t *testing.T) {
	// Note: this test depends on whether the runner has OwnPortDefault free.
	// Skip if not.
	if !portIsFree(OwnPortDefault) {
		t.Skipf("port %d busy on this runner", OwnPortDefault)
	}
	got := pickOwnPort(&HostProfile{})
	if got != OwnPortDefault {
		t.Errorf("pickOwnPort() = %d, want %d", got, OwnPortDefault)
	}
}

func TestPickOwnPort_SkipsRealityDestPort(t *testing.T) {
	// If the would-be default is in use by Reality, we should skip it.
	profile := &HostProfile{RealityDestPort: OwnPortDefault}
	got := pickOwnPort(profile)
	if got == OwnPortDefault {
		t.Errorf("pickOwnPort should skip %d when Reality uses it", OwnPortDefault)
	}
}
