package domain

import "context"

// BindParams are the inputs to Apply / Unbind.
type BindParams struct {
	// Domain to bind, e.g. "olcrtc.example.com". Required.
	Domain string

	// Email for the ACME account. Optional; if empty, certbot uses
	// --register-unsafely-without-email.
	Email string

	// Strategy is one of "auto", "clean", "own-port", "sni-mux".
	// "auto" lets Apply pick the recommended strategy from Detect().
	Strategy string

	// SubPort is the local subscription-server port to proxy to (e.g. 2096).
	SubPort int

	// PublicIP is this host's public IP, used for the DNS check.
	PublicIP string

	// Port is the public TCP port for the strategy.
	// Used only by "own-port" (default 8444). 0 = auto-pick.
	Port int
}

// EventKind is the type of progress event emitted by Apply.
type EventKind string

// Event kinds emitted during Apply.
const (
	EventStep     EventKind = "step"
	EventLog      EventKind = "log"
	EventOK       EventKind = "ok"
	EventError    EventKind = "error"
	EventDone     EventKind = "done"
	EventRollback EventKind = "rollback"
)

// Event is one progress message.
type Event struct {
	Kind     EventKind `json:"kind"`
	StepID   string    `json:"step_id,omitempty"`
	Title    string    `json:"title,omitempty"`
	Message  string    `json:"message,omitempty"`
	Strategy string    `json:"strategy,omitempty"`
}

// BindResult is the final outcome of a successful Apply.
type BindResult struct {
	Strategy string `json:"strategy"`
	Domain   string `json:"domain"`
	// Port is the public TCP port behind the domain (443 for clean,
	// e.g. 8444 for own-port).
	Port     int    `json:"port"`
	SubURL   string `json:"sub_url"` // e.g. https://example.com:8444 (no /sub/{slug} part)
	CertPath string `json:"cert_path,omitempty"`
	KeyPath  string `json:"key_path,omitempty"`
}

// Reporter receives progress events.
//
// Implementations include a noop logger (for CLI quiet mode), a stdout/stderr
// printer (for installer), and an SSE forwarder (for the admin UI).
type Reporter interface {
	Emit(ev Event)
}

// NopReporter discards all events.
type NopReporter struct{}

// Emit implements Reporter.
func (NopReporter) Emit(Event) {}

// FuncReporter wraps a function as a Reporter.
type FuncReporter func(Event)

// Emit implements Reporter.
func (f FuncReporter) Emit(ev Event) { f(ev) }

// Ctx is a tiny helper alias so callers can use a context without importing.
type Ctx = context.Context
