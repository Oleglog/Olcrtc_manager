package muxconn

import (
	"os"
	"strconv"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/xtaci/smux"
)

// Smux configuration is shared between server and client so both ends agree
// on Version, MaxFrameSize and buffer sizes. A mismatch on the wire can
// silently stall the session, so this is the single source of truth.

// Tunable defaults sized for a fast, single-peer datachannel link. Each value
// is an upper bound, not a fixed allocation: smux only buffers what it needs.
//
// MaxFrameSize is the largest single smux PDU. It must stay below the carrier
// data-message limit on every supported provider; 64 KiB is well under what
// Pion SCTP and LiveKit forward in practice (Pion negotiates ~1 GiB by
// default; LiveKit imposes no hard cap on user data packets).
//
// MaxReceiveBuffer caps the per-session read window. With a fast link we want
// a generous BDP buffer; 64 MiB covers ~50 ms RTT at 10 Gbit/s with headroom.
//
// MaxStreamBuffer caps a single tunneled connection's in-flight bytes. 4 MiB
// keeps interactive streams snappy without starving the global window.
const (
	defaultMaxFrameSize     = 64 * 1024
	defaultMaxReceiveBuffer = 64 * 1024 * 1024
	defaultMaxStreamBuffer  = 4 * 1024 * 1024
	defaultKeepAlive        = 10 * time.Second
	defaultKeepAliveTimeout = 60 * time.Second
)

// Environment variable names recognised by SmuxConfig. Set any of them to a
// positive integer (bytes) to override the default. Both server and client
// must agree on MaxFrameSize, so set the same value on both sides.
const (
	envMaxFrameSize     = "OLCRTC_SMUX_MAX_FRAME_SIZE"
	envMaxReceiveBuffer = "OLCRTC_SMUX_MAX_RECEIVE_BUFFER"
	envMaxStreamBuffer  = "OLCRTC_SMUX_MAX_STREAM_BUFFER"
)

// SmuxConfig returns the smux configuration used on both ends of the tunnel.
// It applies env-based overrides where present and falls back to defaults.
func SmuxConfig() *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.Version = 2
	cfg.MaxFrameSize = envOrInt(envMaxFrameSize, defaultMaxFrameSize)
	cfg.MaxReceiveBuffer = envOrInt(envMaxReceiveBuffer, defaultMaxReceiveBuffer)
	cfg.MaxStreamBuffer = envOrInt(envMaxStreamBuffer, defaultMaxStreamBuffer)
	cfg.KeepAliveInterval = defaultKeepAlive
	cfg.KeepAliveTimeout = defaultKeepAliveTimeout
	return cfg
}

func envOrInt(name string, fallback int) int {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v <= 0 {
		logger.Warnf("muxconn: invalid %s=%q, using default %d", name, raw, fallback)
		return fallback
	}
	return v
}
