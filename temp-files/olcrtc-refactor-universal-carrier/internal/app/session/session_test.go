package session

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/control"
	"github.com/openlibrecommunity/olcrtc/internal/crypto"
)

const testBadDuration = "nope"

func TestApplyTransportDefaults(t *testing.T) {
	tests := []struct {
		name string
		in   Config
		want Config
	}{
		{
			name: "vp8",
			in:   Config{Transport: transportVP8},
			want: Config{Transport: transportVP8, VP8FPS: 25, VP8BatchSize: 1},
		},
		{
			name: "sei",
			in:   Config{Transport: transportSEI},
			want: Config{
				Transport:       transportSEI,
				SEIFPS:          60,
				SEIBatchSize:    64,
				SEIFragmentSize: 900,
				SEIAckTimeoutMS: 2000,
			},
		},
		{
			name: "video qrcode",
			in:   Config{Transport: transportVideo},
			want: Config{
				Transport:       transportVideo,
				VideoWidth:      1920,
				VideoHeight:     1080,
				VideoFPS:        30,
				VideoBitrate:    "2M",
				VideoHW:         defaultVideoHW,
				VideoQRRecovery: "low",
				VideoCodec:      videoCodecQRCode,
			},
		},
		{
			name: "video tile dimensions",
			in:   Config{Transport: transportVideo, VideoCodec: videoCodecTile},
			want: Config{
				Transport:       transportVideo,
				VideoWidth:      1080,
				VideoHeight:     1080,
				VideoFPS:        30,
				VideoBitrate:    "2M",
				VideoHW:         defaultVideoHW,
				VideoQRRecovery: "low",
				VideoCodec:      videoCodecTile,
			},
		},
		{
			name: "keeps explicit values",
			in: Config{
				Transport:       transportSEI,
				SEIFPS:          10,
				SEIBatchSize:    2,
				SEIFragmentSize: 300,
				SEIAckTimeoutMS: 1500,
			},
			want: Config{
				Transport:       transportSEI,
				SEIFPS:          10,
				SEIBatchSize:    2,
				SEIFragmentSize: 300,
				SEIAckTimeoutMS: 1500,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ApplyTransportDefaults(tt.in)
			if got != tt.want {
				t.Fatalf("ApplyTransportDefaults() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestApplyLivenessDefaults(t *testing.T) {
	got := ApplyLivenessDefaults(Config{})
	if got.LivenessInterval != control.DefaultInterval.String() {
		t.Fatalf("LivenessInterval = %q, want %q", got.LivenessInterval, control.DefaultInterval.String())
	}
	if got.LivenessTimeout != control.DefaultTimeout.String() {
		t.Fatalf("LivenessTimeout = %q, want %q", got.LivenessTimeout, control.DefaultTimeout.String())
	}
	if got.LivenessFailures != control.DefaultFailures {
		t.Fatalf("LivenessFailures = %d, want %d", got.LivenessFailures, control.DefaultFailures)
	}

	explicit := Config{LivenessInterval: "1s", LivenessTimeout: "500ms", LivenessFailures: 9}
	if got := ApplyLivenessDefaults(explicit); got != explicit {
		t.Fatalf("ApplyLivenessDefaults() = %+v, want %+v", got, explicit)
	}
}

func TestRunWithSessionRotationRestartsAfterMaxDuration(t *testing.T) {
	oldRestartDelay := sessionRestartDelay
	sessionRestartDelay = time.Millisecond
	t.Cleanup(func() { sessionRestartDelay = oldRestartDelay })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls atomic.Int32
	err := runWithSessionRotation(ctx, 5*time.Millisecond, func(ctx context.Context) error {
		if calls.Add(1) >= 2 {
			cancel()
			return nil
		}
		<-ctx.Done()
		return nil
	})
	if err != nil {
		t.Fatalf("runWithSessionRotation() error = %v", err)
	}
	if got := calls.Load(); got < 2 {
		t.Fatalf("run calls = %d, want at least 2", got)
	}
}

//nolint:maintidx // table-driven validation test naturally has many cases
func TestValidate(t *testing.T) {
	RegisterDefaults()

	base := Config{
		Mode:      modeSRV,
		Link:      "direct",
		Transport: "datachannel",
		Auth:      "telemost",
		RoomID:    "room-1",
		KeyHex:    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
		DNSServer: "1.1.1.1:53", //nolint:goconst // test literal, repetition is intentional
	}

	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{name: "valid baseline", cfg: base},
		{
			name: "jazz allows empty room id",
			cfg: func() Config {
				cfg := base
				cfg.Auth = "jazz"
				cfg.RoomID = ""
				return cfg
			}(),
		},
		{
			name: "cnc requires socks host and port",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSHost = "127.0.0.1"
				cfg.SOCKSPort = 1080
				return cfg
			}(),
		},
		{
			name: "missing mode",
			cfg: func() Config {
				cfg := base
				cfg.Mode = ""
				return cfg
			}(),
			want: ErrModeRequired,
		},
		{
			name: "unsupported carrier",
			cfg: func() Config {
				cfg := base
				cfg.Auth = "unknown" //nolint:goconst // test literal, repetition is intentional
				return cfg
			}(),
			want: ErrUnsupportedCarrier,
		},
		{
			name: "unsupported link",
			cfg: func() Config {
				cfg := base
				cfg.Link = "unknown"
				return cfg
			}(),
			want: ErrUnsupportedLink,
		},
		{
			name: "unsupported transport",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "unknown"
				return cfg
			}(),
			want: ErrUnsupportedTransport,
		},
		{
			name: "room id required for non jazz",
			cfg: func() Config {
				cfg := base
				cfg.RoomID = ""
				return cfg
			}(),
			want: ErrRoomIDRequired,
		},
		{
			name: "key required",
			cfg: func() Config {
				cfg := base
				cfg.KeyHex = ""
				return cfg
			}(),
			want: ErrKeyRequired,
		},
		{
			name: "dns server required",
			cfg: func() Config {
				cfg := base
				cfg.DNSServer = ""
				return cfg
			}(),
			want: ErrDNSServerRequired,
		},
		{
			name: "videochannel requires dimensions and bitrate settings",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel" //nolint:goconst // test literal, repetition is intentional
				return cfg
			}(),
			want: ErrVideoWidthRequired,
		},
		{
			name: "videochannel rejects invalid codec",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				cfg.VideoHW = defaultVideoHW
				cfg.VideoCodec = "bogus"
				return cfg
			}(),
			want: ErrVideoCodecInvalid,
		},
		{
			name: "videochannel requires height",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				return cfg
			}(),
			want: ErrVideoHeightRequired,
		},
		{
			name: "videochannel requires fps",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				return cfg
			}(),
			want: ErrVideoFPSRequired,
		},
		{
			name: "videochannel requires bitrate",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				return cfg
			}(),
			want: ErrVideoBitrateRequired,
		},
		{
			name: "videochannel requires hw",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				return cfg
			}(),
			want: ErrVideoHWRequired,
		},
		{
			name: "tile codec requires square 1080 dimensions",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 640
				cfg.VideoHeight = 480
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				cfg.VideoHW = defaultVideoHW
				cfg.VideoCodec = "tile"
				return cfg
			}(),
			want: ErrTileCodecDimensions,
		},
		{
			name: "videochannel valid",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "videochannel"
				cfg.VideoWidth = 1080
				cfg.VideoHeight = 1080
				cfg.VideoFPS = 30
				cfg.VideoBitrate = "1M"
				cfg.VideoHW = defaultVideoHW
				cfg.VideoCodec = "tile"
				return cfg
			}(),
		},
		{
			name: "vp8channel requires fps",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "vp8channel" //nolint:goconst // test literal, repetition is intentional
				return cfg
			}(),
			want: ErrVP8FPSRequired,
		},
		{
			name: "vp8channel requires batch size",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "vp8channel"
				cfg.VP8FPS = 25
				return cfg
			}(),
			want: ErrVP8BatchSizeRequired,
		},
		{
			name: "vp8channel valid",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "vp8channel"
				cfg.VP8FPS = 25
				cfg.VP8BatchSize = 16
				return cfg
			}(),
		},
		{
			name: "seichannel requires fps",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "seichannel" //nolint:goconst // test literal, repetition is intentional
				return cfg
			}(),
			want: ErrSEIFPSRequired,
		},
		{
			name: "seichannel requires batch size",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "seichannel"
				cfg.SEIFPS = 20
				return cfg
			}(),
			want: ErrSEIBatchSizeRequired,
		},
		{
			name: "seichannel requires fragment size",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "seichannel"
				cfg.SEIFPS = 20
				cfg.SEIBatchSize = 1
				return cfg
			}(),
			want: ErrSEIFragmentSizeRequired,
		},
		{
			name: "seichannel requires ack timeout",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "seichannel"
				cfg.SEIFPS = 20
				cfg.SEIBatchSize = 1
				cfg.SEIFragmentSize = 900
				return cfg
			}(),
			want: ErrSEIAckTimeoutRequired,
		},
		{
			name: "seichannel valid",
			cfg: func() Config {
				cfg := base
				cfg.Transport = "seichannel"
				cfg.SEIFPS = 20
				cfg.SEIBatchSize = 1
				cfg.SEIFragmentSize = 900
				cfg.SEIAckTimeoutMS = 3000
				return cfg
			}(),
		},
		{
			name: "cnc requires socks host",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSPort = 1080
				return cfg
			}(),
			want: ErrSOCKSHostRequired,
		},
		{
			name: "cnc requires socks port",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSHost = "127.0.0.1"
				return cfg
			}(),
			want: ErrSOCKSPortRequired,
		},
		{
			name: "cnc rejects unauthenticated wildcard socks bind",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSHost = "0.0.0.0"
				cfg.SOCKSPort = 1080
				return cfg
			}(),
			want: ErrSOCKSAuthRequired,
		},
		{
			name: "cnc allows authenticated wildcard socks bind",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSHost = "0.0.0.0"
				cfg.SOCKSPort = 1080
				cfg.SOCKSUser = "user"
				cfg.SOCKSPass = "pass"
				return cfg
			}(),
		},
		{
			name: "cnc allows localhost socks bind without auth",
			cfg: func() Config {
				cfg := base
				cfg.Mode = modeCNC
				cfg.SOCKSHost = "localhost"
				cfg.SOCKSPort = 1080
				return cfg
			}(),
		},
		{
			name: "liveness rejects bad interval",
			cfg: func() Config {
				cfg := base
				cfg.LivenessInterval = testBadDuration
				return cfg
			}(),
			want: ErrLivenessIntervalInvalid,
		},
		{
			name: "liveness rejects zero timeout",
			cfg: func() Config {
				cfg := base
				cfg.LivenessTimeout = "0s"
				return cfg
			}(),
			want: ErrLivenessTimeoutInvalid,
		},
		{
			name: "liveness rejects negative failures",
			cfg: func() Config {
				cfg := base
				cfg.LivenessFailures = -1
				return cfg
			}(),
			want: ErrLivenessFailuresInvalid,
		},
		{
			name: "lifecycle accepts max session duration",
			cfg: func() Config {
				cfg := base
				cfg.MaxSessionDuration = "1h"
				return cfg
			}(),
		},
		{
			name: "lifecycle rejects bad max session duration",
			cfg: func() Config {
				cfg := base
				cfg.MaxSessionDuration = testBadDuration
				return cfg
			}(),
			want: ErrLifecycleMaxSessionDurationInvalid,
		},
		{
			name: "lifecycle rejects zero max session duration",
			cfg: func() Config {
				cfg := base
				cfg.MaxSessionDuration = "0s"
				return cfg
			}(),
			want: ErrLifecycleMaxSessionDurationInvalid,
		},
		{
			name: "traffic accepts shaping",
			cfg: func() Config {
				cfg := base
				cfg.TrafficMaxPayloadSize = 4096
				cfg.TrafficMinDelay = "5ms"
				cfg.TrafficMaxDelay = "30ms"
				return cfg
			}(),
		},
		{
			name: "traffic rejects negative max payload",
			cfg: func() Config {
				cfg := base
				cfg.TrafficMaxPayloadSize = -1
				return cfg
			}(),
			want: ErrTrafficMaxPayloadSizeInvalid,
		},
		{
			name: "traffic rejects payload smaller than crypto overhead",
			cfg: func() Config {
				cfg := base
				cfg.TrafficMaxPayloadSize = crypto.WireOverhead
				return cfg
			}(),
			want: ErrTrafficMaxPayloadSizeInvalid,
		},
		{
			name: "traffic rejects bad min delay",
			cfg: func() Config {
				cfg := base
				cfg.TrafficMinDelay = testBadDuration
				return cfg
			}(),
			want: ErrTrafficMinDelayInvalid,
		},
		{
			name: "traffic rejects negative max delay",
			cfg: func() Config {
				cfg := base
				cfg.TrafficMaxDelay = "-1ms"
				return cfg
			}(),
			want: ErrTrafficMaxDelayInvalid,
		},
		{
			name: "traffic rejects max delay below min delay",
			cfg: func() Config {
				cfg := base
				cfg.TrafficMinDelay = "30ms"
				cfg.TrafficMaxDelay = "5ms"
				return cfg
			}(),
			want: ErrTrafficMaxDelayInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(tt.cfg)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("Validate() error = %v, want %v", err, tt.want)
			}
		})
	}
}

const testAuthWBStream = "wbstream"

func TestValidateGen(t *testing.T) {
	RegisterDefaults()

	tests := []struct {
		name string
		cfg  Config
		want error
	}{
		{
			name: "valid wbstream",
			cfg:  Config{Auth: testAuthWBStream, DNSServer: "1.1.1.1:53", Amount: 3},
		},
		{
			name: "valid jazz",
			cfg:  Config{Auth: "jazz", DNSServer: "1.1.1.1:53", Amount: 1},
		},
		{
			name: "missing auth",
			cfg:  Config{DNSServer: "1.1.1.1:53", Amount: 1},
			want: ErrAuthRequired,
		},
		{
			name: "unsupported auth",
			cfg:  Config{Auth: "unknown", DNSServer: "1.1.1.1:53", Amount: 1},
			want: ErrUnsupportedCarrier,
		},
		{
			name: "missing dns",
			cfg:  Config{Auth: testAuthWBStream, Amount: 1},
			want: ErrDNSServerRequired,
		},
		{
			name: "amount zero",
			cfg:  Config{Auth: testAuthWBStream, DNSServer: "1.1.1.1:53", Amount: 0},
			want: ErrAmountRequired,
		},
		{
			name: "amount negative",
			cfg:  Config{Auth: testAuthWBStream, DNSServer: "1.1.1.1:53", Amount: -1},
			want: ErrAmountRequired,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGen(tt.cfg)
			if tt.want == nil {
				if err != nil {
					t.Fatalf("ValidateGen() error = %v", err)
				}
				return
			}
			if !errors.Is(err, tt.want) {
				t.Fatalf("ValidateGen() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestGenUnsupportedAuth(t *testing.T) {
	RegisterDefaults()
	cfg := Config{Auth: "telemost", DNSServer: "1.1.1.1:53", Amount: 1}
	err := Gen(context.Background(), cfg, func(string) {})
	if !errors.Is(err, ErrUnsupportedCarrier) {
		t.Fatalf("Gen(telemost) error = %v, want ErrUnsupportedCarrier", err)
	}
}
