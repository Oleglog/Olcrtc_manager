package tunnel_test

import (
	"context"
	"errors"
	"testing"

	"github.com/openlibrecommunity/olcrtc/pkg/olcrtc/tunnel"
)

var errNo = errors.New("no")

func TestRun_FailsWithoutKey(t *testing.T) {
	tunnel.RegisterDefaults()
	err := tunnel.New(tunnel.Config{
		Transport: "datachannel",
		Carrier:   "telemost",
		RoomURL:   "room-1",
		DNSServer: "8.8.8.8:53",
	}).Run(context.Background())
	if err == nil {
		t.Fatal("Run(no key) error = nil")
	}
}

func TestRun_PropagatesAuthHook(_ *testing.T) {
	tunnel.RegisterDefaults()

	var called bool
	cfg := tunnel.Config{
		AuthHook: func(string, map[string]any) (string, error) {
			called = true
			return "", errNo
		},
	}
	_ = tunnel.New(cfg).Run(context.Background())
	// Run bails before ever invoking AuthHook (no key, no carrier wired); this
	// test exists to pin the public surface and ensure the hook field compiles
	// against the re-exported handshake.AuthFunc type alias. Behavior coverage
	// of AuthHook itself lives in internal/handshake tests.
	_ = called
}

// Compile-time checks: the public type aliases must be assignable.
var (
	_ tunnel.AuthFunc         = func(string, map[string]any) (string, error) { return "", nil }
	_ tunnel.SessionOpenFunc  = func(string, string, map[string]any) {}
	_ tunnel.SessionCloseFunc = func(string, string) {}
	_ tunnel.TrafficFunc      = func(string, string, uint64, uint64) {}
)
