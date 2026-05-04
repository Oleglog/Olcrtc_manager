package muxconn

import (
	"testing"

	"github.com/xtaci/smux"
)

// TestSmuxConfigPassesVerification guards against the smux library's wire-
// level constraints (e.g. MaxFrameSize must be in (0, 65535]). Without this
// test it is easy to bump a default to a value that compiles but fails at
// session.New() time, with the symptom only visible on a live tunnel.
func TestSmuxConfigPassesVerification(t *testing.T) {
	cfg := SmuxConfig()
	if err := smux.VerifyConfig(cfg); err != nil {
		t.Fatalf("default SmuxConfig() rejected by smux: %v", err)
	}
}

func TestSmuxConfigRespectsEnvOverrides(t *testing.T) {
	t.Setenv(envMaxFrameSize, "16384")
	t.Setenv(envMaxReceiveBuffer, "8388608")  // 8 MiB
	t.Setenv(envMaxStreamBuffer, "1048576")   // 1 MiB

	cfg := SmuxConfig()
	if cfg.MaxFrameSize != 16384 {
		t.Errorf("MaxFrameSize: want 16384, got %d", cfg.MaxFrameSize)
	}
	if cfg.MaxReceiveBuffer != 8388608 {
		t.Errorf("MaxReceiveBuffer: want 8388608, got %d", cfg.MaxReceiveBuffer)
	}
	if cfg.MaxStreamBuffer != 1048576 {
		t.Errorf("MaxStreamBuffer: want 1048576, got %d", cfg.MaxStreamBuffer)
	}
	if err := smux.VerifyConfig(cfg); err != nil {
		t.Fatalf("env-tuned SmuxConfig() rejected by smux: %v", err)
	}
}
