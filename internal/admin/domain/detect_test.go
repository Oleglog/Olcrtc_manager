package domain

import (
	"testing"
)

func TestParseSSOwner(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`users:(("x-ui",pid=1060,fd=10))`, "x-ui"},
		{`users:(("nginx",pid=200,fd=4),("nginx",pid=201,fd=4))`, "nginx"},
		{`users:(("nginx: master",pid=200,fd=4))`, "nginx"},
		{`users:(("caddy",pid=500,fd=8))`, "caddy"},
		{`users:(("xray",pid=600,fd=12))`, "xray"},
		{`users:(("olcrtc-admin",pid=700,fd=7))`, "olcrtc"},
		{`users:(("sshd",pid=1,fd=3))`, "sshd"},
		{`garbage`, "other"},
	}
	for _, c := range cases {
		got := parseSSOwner(c.in)
		if got != c.want {
			t.Errorf("parseSSOwner(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseSSOutput_BasicListenerSet(t *testing.T) {
	// Mimic real `ss -tlnp` output.
	input := `State                  Recv-Q                 Send-Q                                  Local Address:Port                                  Peer Address:Port                Process
LISTEN                 0                      4096                                          *:2096                                              *:*                            users:(("x-ui",pid=1060,fd=10))
LISTEN                 0                      4096                                          *:8443                                              *:*                            users:(("olcrtc-admin",pid=622032,fd=7))
LISTEN                 0                      511                                           *:80                                                *:*                            users:(("nginx",pid=519269,fd=6))
LISTEN                 0                      511                                           *:443                                               *:*                            users:(("caddy",pid=519269,fd=10))
LISTEN                 0                      128                                       [::]:22                                              [::]:*                            users:(("sshd",pid=1,fd=3))
`
	got := parseSSOutput(input)
	if len(got) != 5 {
		t.Fatalf("parseSSOutput: got %d listeners, want 5: %+v", len(got), got)
	}
	expect := map[int]string{
		2096: "x-ui",
		8443: "olcrtc",
		80:   "nginx",
		443:  "caddy",
		22:   "sshd",
	}
	for _, l := range got {
		want, ok := expect[l.Port]
		if !ok {
			t.Errorf("unexpected port: %+v", l)
			continue
		}
		if l.Owner != want {
			t.Errorf("port %d: owner=%q want %q", l.Port, l.Owner, want)
		}
	}
}

func TestListenerForPort(t *testing.T) {
	ls := []SocketListener{
		{Port: 80, Owner: "nginx"},
		{Port: 443, Owner: "caddy"},
		{Port: 2096, Owner: "x-ui"},
	}
	if got := listenerForPort(ls, 80); got != "nginx" {
		t.Errorf("port 80: got %q want nginx", got)
	}
	if got := listenerForPort(ls, 443); got != "caddy" {
		t.Errorf("port 443: got %q want caddy", got)
	}
	if got := listenerForPort(ls, 9999); got != "" {
		t.Errorf("port 9999: got %q want empty", got)
	}
}

func TestNginxOutputHasStreamMux(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name: "stream block with ssl_preread",
			input: `# configuration file /etc/nginx/nginx.conf:
stream {
    map $ssl_preread_server_name $upstream {
        default xray;
    }
    server {
        listen 443;
        ssl_preread on;
        proxy_pass $upstream;
    }
}
`,
			want: true,
		},
		{
			name: "stream block tight-spaced",
			input: `stream{
    server { listen 443; ssl_preread on; proxy_pass x; }
}`,
			want: true,
		},
		{
			name: "stream block but no ssl_preread (plain TCP proxy)",
			input: `stream {
    server { listen 5432; proxy_pass postgres; }
}`,
			want: false,
		},
		{
			name:  "no stream block at all",
			input: `http { server { listen 80; } }`,
			want:  false,
		},
		{
			name:  "empty",
			input: ``,
			want:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := nginxOutputHasStreamMux(c.input)
			if got != c.want {
				t.Errorf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestParseRealityDestFromJSON(t *testing.T) {
	cases := []struct {
		name string
		json string
		want int
	}{
		{
			name: "host:port",
			json: `{"inbounds":[{"streamSettings":{"realitySettings":{"dest":"127.0.0.1:9443"}}}]}`,
			want: 9443,
		},
		{
			name: "port-only",
			json: `{"inbounds":[{"streamSettings":{"realitySettings":{"dest":"9443"}}}]}`,
			want: 9443,
		},
		{
			name: "no-reality",
			json: `{"inbounds":[{"streamSettings":{}}]}`,
			want: 0,
		},
		{
			name: "multiple-inbounds-first-reality",
			json: `{"inbounds":[
				{"streamSettings":{}},
				{"streamSettings":{"realitySettings":{"dest":"127.0.0.1:11111"}}}
			]}`,
			want: 11111,
		},
		{
			name: "invalid-json",
			json: `not json`,
			want: 0,
		},
		{
			name: "empty",
			json: ``,
			want: 0,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseRealityDestFromJSON([]byte(c.json))
			if got != c.want {
				t.Errorf("got %d want %d", got, c.want)
			}
		})
	}
}

func TestAtoiSafe(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"123", 123},
		{"0", 0},
		{"", 0},
		{"abc", 0},
		{"  443 ", 443},
		{"1a2", 0},
	}
	for _, c := range cases {
		if got := atoiSafe(c.in); got != c.want {
			t.Errorf("atoiSafe(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestBuildStrategies_CleanHost(t *testing.T) {
	p := &HostProfile{
		PublicIP:     "1.2.3.4",
		Port443Owner: "", // free
		Port80Owner:  "",
	}
	got := buildStrategies(p)
	if len(got) != 3 {
		t.Fatalf("want 3 strategies, got %d", len(got))
	}
	byName := map[string]Strategy{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if !byName[StrategyClean].Recommended {
		t.Errorf("clean should be recommended on clean host")
	}
	if !byName[StrategyClean].Available {
		t.Errorf("clean should be available")
	}
	if !byName[StrategyOwnPort].Available {
		t.Errorf("own-port should always be available")
	}
	if byName[StrategyOwnPort].Recommended {
		t.Errorf("own-port should NOT be recommended on clean host")
	}
	if byName[StrategySNIMux].Available {
		t.Errorf("sni-mux should NOT be available on clean host (no nginx mux)")
	}
}

func TestBuildStrategies_3xUIHost(t *testing.T) {
	p := &HostProfile{
		HasXUI:         true,
		HasNginxStream: true,
		Port443Owner:   "nginx",
		Port80Owner:    "nginx",
	}
	got := buildStrategies(p)
	byName := map[string]Strategy{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if byName[StrategyClean].Available {
		t.Errorf("clean should NOT be available when :443 is taken by nginx mux")
	}
	if !byName[StrategyOwnPort].Recommended {
		t.Errorf("own-port should be recommended on 3x-ui host")
	}
	if !byName[StrategySNIMux].Available {
		t.Errorf("sni-mux should be available when nginx mux is detected")
	}
	if byName[StrategySNIMux].Recommended {
		t.Errorf("sni-mux must never be auto-recommended (opt-in only)")
	}
}

func TestBuildStrategies_Port443BusyButNoMux(t *testing.T) {
	// E.g. caddy on :443, no 3x-ui.
	p := &HostProfile{
		HasCaddy:     true,
		Port443Owner: "caddy",
		Port80Owner:  "caddy",
	}
	got := buildStrategies(p)
	byName := map[string]Strategy{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if byName[StrategyClean].Available {
		t.Errorf("clean should NOT be available when :443 is taken")
	}
	if !byName[StrategyOwnPort].Recommended {
		t.Errorf("own-port should be recommended when :443 is taken")
	}
}

func TestBuildStrategies_AtMostOneRecommended(t *testing.T) {
	profiles := []*HostProfile{
		{},
		{HasXUI: true, HasNginxStream: true, Port443Owner: "nginx"},
		{Port443Owner: ""},
		{Port443Owner: "caddy"},
	}
	for i, p := range profiles {
		got := buildStrategies(p)
		count := 0
		for _, s := range got {
			if s.Recommended {
				count++
			}
		}
		if count != 1 {
			t.Errorf("profile %d: expected exactly 1 recommended, got %d", i, count)
		}
	}
}
