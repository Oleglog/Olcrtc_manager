package vp8channel

import (
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/transport"
)

const (
	defaultFPS       = 60
	defaultBatchSize = 64
	// defaultMaxBytesPerSec paces the wire byte-rate just under the Telemost
	// SFU's measured per-slot policer knee (~1.4 MiB/s). Above it the SFU
	// drops bursts wholesale, collapsing goodput and starving keepalives;
	// staying under keeps loss near zero. See TestRealRawVP8Throughput.
	defaultMaxBytesPerSec = 1_200_000
)

// Options tunes the vp8channel transport. Zero values fall back to documented defaults.
type Options struct {
	FPS       int
	BatchSize int
	// MaxBytesPerSec caps the wire byte-rate fed to the video track. Zero
	// falls back to defaultMaxBytesPerSec.
	MaxBytesPerSec int
}

// TransportOptions marks Options as belonging to the transport options family.
func (Options) TransportOptions() {}

func optionsFrom(cfg transport.Config) (Options, error) {
	if cfg.Options == nil {
		return Options{}, nil
	}
	opts, ok := cfg.Options.(Options)
	if !ok {
		return Options{}, fmt.Errorf("%w: vp8channel: got %T", transport.ErrOptionsTypeMismatch, cfg.Options)
	}
	return opts, nil
}
