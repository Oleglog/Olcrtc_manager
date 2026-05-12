// Package domain provides detection and binding of public domains for
// the olcRTC subscription server. It supports multiple strategies depending
// on the host profile (clean host, 3x-ui co-existence, existing caddy).
package domain

// HostProfile describes what is currently installed and running on the host
// in terms of services relevant to TLS / reverse-proxying.
type HostProfile struct {
	// Identifies.
	PublicIP string `json:"public_ip"`

	// Process / package presence.
	HasXUI         bool `json:"has_xui"`          // x-ui (3x-ui) panel process or systemd unit
	HasNginx       bool `json:"has_nginx"`        // nginx binary installed
	HasNginxStream bool `json:"has_nginx_stream"` // nginx -T shows stream block with ssl_preread
	HasCaddy       bool `json:"has_caddy"`        // caddy binary installed
	HasCertbot     bool `json:"has_certbot"`      // certbot binary installed
	HasXray        bool `json:"has_xray"`         // standalone xray binary/service

	// Caddy state.
	CaddyManagedBySystemd bool `json:"caddy_managed_by_systemd"`

	// Port occupancy. Empty string means free; "nginx", "caddy", "x-ui",
	// "xray", or "other" otherwise.
	Port80Owner  string `json:"port_80_owner"`
	Port443Owner string `json:"port_443_owner"`

	// Reality detection.
	// RealityDestPort is the local TCP port xray's Reality protocol points
	// to as its decoy `dest`. 0 if not detected. Used by Tier-3 SNI mux
	// strategy to avoid collisions.
	RealityDestPort int `json:"reality_dest_port,omitempty"`

	// Strategies available on this host, ordered by preference.
	Strategies []Strategy `json:"strategies"`
}

// Strategy describes one way to bind a domain on the current host.
type Strategy struct {
	// Name is a stable identifier: "clean", "own-port", "sni-mux".
	Name string `json:"name"`

	// Title is a short human-readable label (shown as button text in UI).
	Title string `json:"title"`

	// Description explains what this strategy does.
	Description string `json:"description"`

	// Risk is "low", "medium", or "high".
	Risk string `json:"risk"`

	// Recommended marks the preferred strategy for the detected profile.
	Recommended bool `json:"recommended"`

	// Available indicates whether the strategy can be applied right now.
	// Strategies are always listed but may be unavailable (e.g. clean :443
	// requires :443 to be free).
	Available bool `json:"available"`

	// UnavailableReason explains why Available is false.
	UnavailableReason string `json:"unavailable_reason,omitempty"`

	// SubscriptionURL is the public URL pattern this strategy will produce,
	// with {domain} as a placeholder. E.g. "https://{domain}/sub/{slug}" or
	// "https://{domain}:8444/sub/{slug}".
	SubscriptionURL string `json:"subscription_url"`
}

// Known strategy names.
const (
	StrategyClean   = "clean"    // Tier 1: install nginx + certbot on free :443
	StrategyOwnPort = "own-port" // Tier 2: own caddy on a non-standard port (safe for 3x-ui)
	StrategySNIMux  = "sni-mux"  // Tier 3: inject SNI entry into user's nginx stream (3x-ui-aware)
)
