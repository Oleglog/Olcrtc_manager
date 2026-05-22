// Package session wires runtime configuration to application mode entrypoints.
package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync/atomic"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/carrier/builtin"
	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/control"
	"github.com/openlibrecommunity/olcrtc/internal/crypto"
	"github.com/openlibrecommunity/olcrtc/internal/link"
	"github.com/openlibrecommunity/olcrtc/internal/link/direct"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/names"
	"github.com/openlibrecommunity/olcrtc/internal/server"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/openlibrecommunity/olcrtc/internal/transport/datachannel"
	"github.com/openlibrecommunity/olcrtc/internal/transport/seichannel"
	"github.com/openlibrecommunity/olcrtc/internal/transport/videochannel"
	"github.com/openlibrecommunity/olcrtc/internal/transport/vp8channel"
)

const (
	modeSRV          = "srv"
	modeCNC          = "cnc"
	modeGen          = "gen"
	authJazz         = "jazz"
	authNone         = "none"
	transportVideo   = "videochannel"
	transportVP8     = "vp8channel"
	transportSEI     = "seichannel"
	videoCodecQRCode = "qrcode"
	videoCodecTile   = "tile"
)

const (
	defaultVideoWidth      = 1920
	defaultVideoHeight     = 1080
	defaultVideoFPS        = 30
	defaultVideoBitrate    = "2M"
	defaultVideoHW         = "none"
	defaultVideoQRRecovery = "low"
	defaultVP8FPS          = 25
	defaultVP8BatchSize    = 1
	defaultSEIFPS          = 60
	defaultSEIBatchSize    = 64
	defaultSEIFragmentSize = 900
	defaultSEIAckTimeoutMS = 2000
)

var sessionRestartDelay = 2 * time.Second //nolint:gochecknoglobals // tests shorten lifecycle rotation delay

var (
	// ErrRoomIDRequired indicates that a room id is required for the selected carrier.
	ErrRoomIDRequired = errors.New("room ID required (set room.id)")
	// ErrModeRequired indicates that mode is not one of the supported values.
	ErrModeRequired = errors.New("mode required (set mode to srv, cnc or gen)")
	// ErrAmountRequired indicates that gen.amount is required for gen mode.
	ErrAmountRequired = errors.New("amount required for gen mode (set gen.amount)")
	// ErrAuthRequired indicates that no auth provider was selected.
	ErrAuthRequired = errors.New(
		"auth provider required (set auth.provider to jitsi, telemost, jazz, wbstream or none)")
	// ErrURLRequired indicates that auth.url must be provided when the auth provider has no default URL.
	ErrURLRequired = errors.New("SFU URL required (set auth.url)")
	// ErrUnsupportedCarrier indicates that carrier is not registered.
	ErrUnsupportedCarrier = errors.New("unsupported carrier")
	// ErrUnsupportedLink indicates that link is not registered.
	ErrUnsupportedLink = errors.New("unsupported link")
	// ErrUnsupportedTransport indicates that transport is not registered.
	ErrUnsupportedTransport = errors.New("unsupported transport")

	// ErrLinkRequired indicates that link is not provided.
	ErrLinkRequired = errors.New("link required (set link to direct)")
	// ErrTransportRequired indicates that transport is not provided.
	ErrTransportRequired = errors.New(
		"transport required (set transport to datachannel, videochannel, seichannel or vp8channel)")
	// ErrKeyRequired indicates that encryption key is not provided.
	ErrKeyRequired = errors.New("key required (set crypto.key)")
	// ErrDNSServerRequired indicates that dns server is not provided.
	ErrDNSServerRequired = errors.New("dns server required (set net.dns)")

	// ErrVideoWidthRequired indicates that video width is required for videochannel.
	ErrVideoWidthRequired = errors.New("video width required for videochannel (set video.width)")
	// ErrVideoHeightRequired indicates that video height is required for videochannel.
	ErrVideoHeightRequired = errors.New("video height required for videochannel (set video.height)")
	// ErrVideoFPSRequired indicates that video fps is required for videochannel.
	ErrVideoFPSRequired = errors.New("video fps required for videochannel (set video.fps)")
	// ErrVideoBitrateRequired indicates that video bitrate is required for videochannel.
	ErrVideoBitrateRequired = errors.New(
		"video bitrate required for videochannel (set video.bitrate)")
	// ErrVideoHWRequired indicates that video hardware acceleration is required.
	ErrVideoHWRequired = errors.New(
		"video hardware acceleration required for videochannel (set video.hw to none or nvenc)")
	// ErrVideoCodecInvalid indicates that the video codec is not valid.
	ErrVideoCodecInvalid = errors.New(
		"invalid video codec for videochannel (set video.codec to qrcode or tile)")
	// ErrTileCodecDimensions indicates that tile codec requires 1080x1080 dimensions.
	ErrTileCodecDimensions = errors.New("tile codec requires video.width: 1080 and video.height: 1080")

	// ErrVP8FPSRequired indicates that vp8 fps is required for vp8channel.
	ErrVP8FPSRequired = errors.New("vp8 fps required for vp8channel (set vp8.fps)")
	// ErrVP8BatchSizeRequired indicates that vp8 batch size is required for vp8channel.
	ErrVP8BatchSizeRequired = errors.New("vp8 batch size required for vp8channel (set vp8.batch_size)")
	// ErrSEIFPSRequired indicates that seichannel fps is required.
	ErrSEIFPSRequired = errors.New("fps required for seichannel (set sei.fps)")
	// ErrSEIBatchSizeRequired indicates that seichannel batch size is required.
	ErrSEIBatchSizeRequired = errors.New("batch size required for seichannel (set sei.batch_size)")
	// ErrSEIFragmentSizeRequired indicates that seichannel fragment size is required.
	ErrSEIFragmentSizeRequired = errors.New("fragment size required for seichannel (set sei.fragment_size)")
	// ErrSEIAckTimeoutRequired indicates that seichannel ack timeout is required.
	ErrSEIAckTimeoutRequired = errors.New("ack timeout required for seichannel (set sei.ack_timeout_ms)")

	// ErrSOCKSHostRequired indicates that socks host is required for cnc mode.
	ErrSOCKSHostRequired = errors.New("socks host required for cnc mode (set socks.host)")
	// ErrSOCKSPortRequired indicates that socks port is required for cnc mode.
	ErrSOCKSPortRequired = errors.New("socks port required for cnc mode (set socks.port)")
	// ErrSOCKSAuthRequired indicates that a non-loopback SOCKS listener requires authentication.
	ErrSOCKSAuthRequired = errors.New(
		"socks auth required when binding outside loopback (set socks.user and socks.pass)")

	// ErrLivenessIntervalInvalid indicates that liveness.interval is not a positive duration.
	ErrLivenessIntervalInvalid = errors.New(
		"invalid liveness interval (set liveness.interval to a duration > 0)")
	// ErrLivenessTimeoutInvalid indicates that liveness.timeout is not a positive duration.
	ErrLivenessTimeoutInvalid = errors.New(
		"invalid liveness timeout (set liveness.timeout to a duration > 0)")
	// ErrLivenessFailuresInvalid indicates that liveness.failures is not positive.
	ErrLivenessFailuresInvalid = errors.New(
		"invalid liveness failures (set liveness.failures to a value > 0)")
	// ErrLifecycleMaxSessionDurationInvalid indicates that lifecycle.max_session_duration is not a positive duration.
	ErrLifecycleMaxSessionDurationInvalid = errors.New(
		"invalid max session duration (set lifecycle.max_session_duration to a duration > 0)")
	// ErrTrafficMaxPayloadSizeInvalid indicates that traffic.max_payload_size is not valid.
	ErrTrafficMaxPayloadSizeInvalid = errors.New(
		"invalid traffic max payload size (set traffic.max_payload_size to 0 or a value above crypto overhead)")
	// ErrTrafficMinDelayInvalid indicates that traffic.min_delay is not a non-negative duration.
	ErrTrafficMinDelayInvalid = errors.New(
		"invalid traffic min delay (set traffic.min_delay to a duration >= 0)")
	// ErrTrafficMaxDelayInvalid indicates that traffic.max_delay is not a non-negative duration.
	ErrTrafficMaxDelayInvalid = errors.New(
		"invalid traffic max delay (set traffic.max_delay to a duration >= 0 and >= traffic.min_delay)")
	errPositiveDuration    = errors.New("duration must be > 0")
	errNonNegativeDuration = errors.New("duration must be >= 0")
)

// Config holds runtime session settings.
type Config struct {
	Mode                  string
	Link                  string
	Transport             string
	Auth                  string
	Engine                string
	URL                   string
	Token                 string
	RoomID                string
	ChannelID             string
	KeyHex                string
	SOCKSHost             string
	SOCKSPort             int
	SOCKSUser             string
	SOCKSPass             string
	DNSServer             string
	SOCKSProxyAddr        string
	SOCKSProxyPort        int
	VideoWidth            int
	VideoHeight           int
	VideoFPS              int
	VideoBitrate          string
	VideoHW               string
	VideoQRSize           int
	VideoQRRecovery       string
	VideoCodec            string
	VideoTileModule       int
	VideoTileRS           int
	VP8FPS                int
	VP8BatchSize          int
	SEIFPS                int
	SEIBatchSize          int
	SEIFragmentSize       int
	SEIAckTimeoutMS       int
	LivenessInterval      string
	LivenessTimeout       string
	LivenessFailures      int
	MaxSessionDuration    string
	TrafficMaxPayloadSize int
	TrafficMinDelay       string
	TrafficMaxDelay       string
	Amount                int
}

// RegisterDefaults registers built-in carriers and transports.
func RegisterDefaults() {
	builtin.Register()
	link.Register("direct", direct.New)
	transport.Register("datachannel", datachannel.New)
	transport.Register("videochannel", videochannel.New)
	transport.Register("seichannel", seichannel.New)
	transport.Register("vp8channel", vp8channel.New)
}

// ApplyAuthDefaults fills in Engine and URL from the auth provider when they are not set explicitly.
// For -auth none the fields are left untouched (the caller supplies them directly).
//
// An empty cfg.URL is acceptable when the auth provider does not advertise a
// DefaultServiceURL — those providers (e.g. jitsi) extract the SFU host from
// the user-supplied RoomURL inside Issue(), so an externally fixed
// service URL would be meaningless. Providers that DO advertise a
// DefaultServiceURL (telemost, wbstream, jazz) still require URL to be set
// when their default cannot be applied.
func ApplyAuthDefaults(cfg Config) (Config, error) {
	if cfg.Auth == authNone || cfg.Auth == "" {
		return cfg, nil
	}
	p, _ := auth.Get(cfg.Auth) // unknown auth is caught later by validateAuth
	if p == nil {
		return cfg, nil
	}
	if cfg.Engine == "" {
		cfg.Engine = p.Engine()
	}
	if cfg.URL == "" {
		cfg.URL = p.DefaultServiceURL()
	}
	if cfg.URL == "" && p.DefaultServiceURL() != "" {
		return cfg, fmt.Errorf("%w: auth provider %q has no default URL", ErrURLRequired, cfg.Auth)
	}
	return cfg, nil
}

// ApplyTransportDefaults fills documented transport defaults without changing core routing fields.
func ApplyTransportDefaults(cfg Config) Config {
	switch cfg.Transport {
	case transportVideo:
		return applyVideoDefaults(cfg)
	case transportVP8:
		return applyVP8Defaults(cfg)
	case transportSEI:
		return applySEIDefaults(cfg)
	default:
		return cfg
	}
}

// ApplyLivenessDefaults fills documented control-stream liveness defaults.
func ApplyLivenessDefaults(cfg Config) Config {
	if cfg.LivenessInterval == "" {
		cfg.LivenessInterval = control.DefaultInterval.String()
	}
	if cfg.LivenessTimeout == "" {
		cfg.LivenessTimeout = control.DefaultTimeout.String()
	}
	if cfg.LivenessFailures == 0 {
		cfg.LivenessFailures = control.DefaultFailures
	}
	return cfg
}

func applyVideoDefaults(cfg Config) Config {
	if cfg.VideoCodec == "" {
		cfg.VideoCodec = videoCodecQRCode
	}
	width := defaultVideoWidth
	if cfg.VideoCodec == videoCodecTile {
		width = defaultVideoHeight
	}
	if cfg.VideoWidth == 0 {
		cfg.VideoWidth = width
	}
	if cfg.VideoHeight == 0 {
		cfg.VideoHeight = defaultVideoHeight
	}
	if cfg.VideoFPS == 0 {
		cfg.VideoFPS = defaultVideoFPS
	}
	if cfg.VideoBitrate == "" {
		cfg.VideoBitrate = defaultVideoBitrate
	}
	if cfg.VideoHW == "" {
		cfg.VideoHW = defaultVideoHW
	}
	if cfg.VideoQRRecovery == "" {
		cfg.VideoQRRecovery = defaultVideoQRRecovery
	}
	return cfg
}

func applyVP8Defaults(cfg Config) Config {
	if cfg.VP8FPS == 0 {
		cfg.VP8FPS = defaultVP8FPS
	}
	if cfg.VP8BatchSize == 0 {
		cfg.VP8BatchSize = defaultVP8BatchSize
	}
	return cfg
}

func applySEIDefaults(cfg Config) Config {
	if cfg.SEIFPS == 0 {
		cfg.SEIFPS = defaultSEIFPS
	}
	if cfg.SEIBatchSize == 0 {
		cfg.SEIBatchSize = defaultSEIBatchSize
	}
	if cfg.SEIFragmentSize == 0 {
		cfg.SEIFragmentSize = defaultSEIFragmentSize
	}
	if cfg.SEIAckTimeoutMS == 0 {
		cfg.SEIAckTimeoutMS = defaultSEIAckTimeoutMS
	}
	return cfg
}

// Validate verifies that the runtime config refers to registered components and all required fields are present.
func Validate(cfg Config) error {
	if err := validateMode(cfg); err != nil {
		return err
	}
	if err := validateAuth(cfg); err != nil {
		return err
	}
	if err := validateLink(cfg); err != nil {
		return err
	}
	if err := validateTransportRegistration(cfg); err != nil {
		return err
	}
	if err := validateCommon(cfg); err != nil {
		return err
	}
	if err := validateTransportConfig(cfg); err != nil {
		return err
	}
	if err := validateLivenessConfig(cfg); err != nil {
		return err
	}
	if err := validateLifecycleConfig(cfg); err != nil {
		return err
	}
	if err := validateTrafficConfig(cfg); err != nil {
		return err
	}
	return validateModeConfig(cfg)
}

func validateMode(cfg Config) error {
	switch cfg.Mode {
	case modeSRV, modeCNC, modeGen:
		return nil
	default:
		return ErrModeRequired
	}
}

func validateAuth(cfg Config) error {
	if cfg.Auth == "" {
		return ErrAuthRequired
	}
	if !slices.Contains(carrier.Available(), cfg.Auth) {
		return fmt.Errorf("%w: %s (available: %v)", ErrUnsupportedCarrier, cfg.Auth, carrier.Available())
	}
	return nil
}

func validateLink(cfg Config) error {
	if cfg.Link == "" {
		return ErrLinkRequired
	}
	if !slices.Contains(link.Available(), cfg.Link) {
		return fmt.Errorf("%w: %s (available: %v)", ErrUnsupportedLink, cfg.Link, link.Available())
	}
	return nil
}

func validateTransportRegistration(cfg Config) error {
	if cfg.Transport == "" {
		return ErrTransportRequired
	}
	if !slices.Contains(transport.Available(), cfg.Transport) {
		return fmt.Errorf("%w: %s (available: %v)", ErrUnsupportedTransport, cfg.Transport, transport.Available())
	}
	return nil
}

func validateCommon(cfg Config) error {
	if cfg.RoomID == "" && cfg.Auth != authJazz && cfg.Auth != authNone {
		return ErrRoomIDRequired
	}
	if cfg.KeyHex == "" {
		return ErrKeyRequired
	}
	if cfg.DNSServer == "" {
		return ErrDNSServerRequired
	}
	return nil
}

func validateTransportConfig(cfg Config) error {
	switch cfg.Transport {
	case transportVideo:
		return validateVideoChannel(cfg)
	case transportVP8:
		return validateVP8Channel(cfg)
	case transportSEI:
		return validateSEIChannel(cfg)
	default:
		return nil
	}
}

func validateVideoCodec(cfg Config) error {
	if cfg.VideoCodec != "" && cfg.VideoCodec != videoCodecQRCode && cfg.VideoCodec != videoCodecTile {
		return ErrVideoCodecInvalid
	}
	if cfg.VideoCodec == videoCodecTile && (cfg.VideoWidth != 1080 || cfg.VideoHeight != 1080) {
		return ErrTileCodecDimensions
	}
	return nil
}

func validateVideoChannel(cfg Config) error {
	if cfg.VideoWidth == 0 {
		return ErrVideoWidthRequired
	}
	if cfg.VideoHeight == 0 {
		return ErrVideoHeightRequired
	}
	if cfg.VideoFPS == 0 {
		return ErrVideoFPSRequired
	}
	if cfg.VideoBitrate == "" {
		return ErrVideoBitrateRequired
	}
	if cfg.VideoHW == "" {
		return ErrVideoHWRequired
	}
	return validateVideoCodec(cfg)
}

func validateVP8Channel(cfg Config) error {
	if cfg.VP8FPS == 0 {
		return ErrVP8FPSRequired
	}
	if cfg.VP8BatchSize == 0 {
		return ErrVP8BatchSizeRequired
	}
	return nil
}

func validateSEIChannel(cfg Config) error {
	if cfg.SEIFPS == 0 {
		return ErrSEIFPSRequired
	}
	if cfg.SEIBatchSize == 0 {
		return ErrSEIBatchSizeRequired
	}
	if cfg.SEIFragmentSize == 0 {
		return ErrSEIFragmentSizeRequired
	}
	if cfg.SEIAckTimeoutMS == 0 {
		return ErrSEIAckTimeoutRequired
	}
	return nil
}

func validateModeConfig(cfg Config) error {
	if cfg.Mode != modeCNC {
		return nil
	}
	if cfg.SOCKSHost == "" {
		return ErrSOCKSHostRequired
	}
	if cfg.SOCKSPort == 0 {
		return ErrSOCKSPortRequired
	}
	if !isLoopbackListenHost(cfg.SOCKSHost) && (cfg.SOCKSUser == "" || cfg.SOCKSPass == "") {
		return ErrSOCKSAuthRequired
	}
	return nil
}

func validateLivenessConfig(cfg Config) error {
	if _, err := parseLivenessDuration(cfg.LivenessInterval, control.DefaultInterval); err != nil {
		return fmt.Errorf("%w: %w", ErrLivenessIntervalInvalid, err)
	}
	if _, err := parseLivenessDuration(cfg.LivenessTimeout, control.DefaultTimeout); err != nil {
		return fmt.Errorf("%w: %w", ErrLivenessTimeoutInvalid, err)
	}
	if cfg.LivenessFailures < 0 {
		return ErrLivenessFailuresInvalid
	}
	return nil
}

func validateLifecycleConfig(cfg Config) error {
	if _, err := maxSessionDuration(cfg); err != nil {
		return err
	}
	return nil
}

func parseLivenessDuration(value string, def time.Duration) (time.Duration, error) {
	if value == "" {
		return def, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}
	if d <= 0 {
		return 0, errPositiveDuration
	}
	return d, nil
}

func livenessConfig(cfg Config) (control.Config, error) {
	interval, err := parseLivenessDuration(cfg.LivenessInterval, control.DefaultInterval)
	if err != nil {
		return control.Config{}, fmt.Errorf("%w: %w", ErrLivenessIntervalInvalid, err)
	}
	timeout, err := parseLivenessDuration(cfg.LivenessTimeout, control.DefaultTimeout)
	if err != nil {
		return control.Config{}, fmt.Errorf("%w: %w", ErrLivenessTimeoutInvalid, err)
	}
	failures := cfg.LivenessFailures
	if failures == 0 {
		failures = control.DefaultFailures
	}
	if failures < 0 {
		return control.Config{}, ErrLivenessFailuresInvalid
	}
	return control.Config{Interval: interval, Timeout: timeout, Failures: failures}, nil
}

func maxSessionDuration(cfg Config) (time.Duration, error) {
	if cfg.MaxSessionDuration == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(cfg.MaxSessionDuration)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrLifecycleMaxSessionDurationInvalid, err)
	}
	if d <= 0 {
		return 0, ErrLifecycleMaxSessionDurationInvalid
	}
	return d, nil
}

func validateTrafficConfig(cfg Config) error {
	_, err := trafficConfig(cfg)
	return err
}

func trafficConfig(cfg Config) (transport.TrafficConfig, error) {
	if cfg.TrafficMaxPayloadSize < 0 || (cfg.TrafficMaxPayloadSize > 0 &&
		cfg.TrafficMaxPayloadSize <= crypto.WireOverhead) {
		return transport.TrafficConfig{}, ErrTrafficMaxPayloadSizeInvalid
	}
	minDelay, err := parseOptionalNonNegativeDuration(cfg.TrafficMinDelay)
	if err != nil {
		return transport.TrafficConfig{}, fmt.Errorf("%w: %w", ErrTrafficMinDelayInvalid, err)
	}
	maxDelay, err := parseOptionalNonNegativeDuration(cfg.TrafficMaxDelay)
	if err != nil {
		return transport.TrafficConfig{}, fmt.Errorf("%w: %w", ErrTrafficMaxDelayInvalid, err)
	}
	if maxDelay > 0 && maxDelay < minDelay {
		return transport.TrafficConfig{}, ErrTrafficMaxDelayInvalid
	}
	return transport.TrafficConfig{
		MaxPayloadSize: cfg.TrafficMaxPayloadSize,
		MinDelay:       minDelay,
		MaxDelay:       maxDelay,
	}, nil
}

func parseOptionalNonNegativeDuration(value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("parse duration: %w", err)
	}
	if d < 0 {
		return 0, errNonNegativeDuration
	}
	return d, nil
}

func isLoopbackListenHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Run starts the configured mode.
func Run(ctx context.Context, cfg Config) error {
	cfg = ApplyTransportDefaults(cfg)
	cfg = ApplyLivenessDefaults(cfg)
	roomURL := cfg.RoomID
	liveness, err := livenessConfig(cfg)
	if err != nil {
		return err
	}
	maxDuration, err := maxSessionDuration(cfg)
	if err != nil {
		return err
	}
	traffic, err := trafficConfig(cfg)
	if err != nil {
		return err
	}

	run := func(ctx context.Context) error {
		return runOnce(ctx, cfg, roomURL, liveness, traffic)
	}
	if maxDuration > 0 {
		return runWithSessionRotation(ctx, maxDuration, run)
	}
	return run(ctx)
}

func runOnce(
	ctx context.Context,
	cfg Config,
	roomURL string,
	liveness control.Config,
	traffic transport.TrafficConfig,
) error {
	switch cfg.Mode {
	case modeSRV:
		if err := server.Run(ctx, server.Config{
			Link:            cfg.Link,
			Transport:       cfg.Transport,
			Carrier:         cfg.Auth,
			RoomURL:         roomURL,
			ChannelID:       cfg.ChannelID,
			KeyHex:          cfg.KeyHex,
			DNSServer:       cfg.DNSServer,
			SOCKSProxyAddr:  cfg.SOCKSProxyAddr,
			SOCKSProxyPort:  cfg.SOCKSProxyPort,
			VideoWidth:      cfg.VideoWidth,
			VideoHeight:     cfg.VideoHeight,
			VideoFPS:        cfg.VideoFPS,
			VideoBitrate:    cfg.VideoBitrate,
			VideoHW:         cfg.VideoHW,
			VideoQRSize:     cfg.VideoQRSize,
			VideoQRRecovery: cfg.VideoQRRecovery,
			VideoCodec:      cfg.VideoCodec,
			VideoTileModule: cfg.VideoTileModule,
			VideoTileRS:     cfg.VideoTileRS,
			VP8FPS:          cfg.VP8FPS,
			VP8BatchSize:    cfg.VP8BatchSize,
			SEIFPS:          cfg.SEIFPS,
			SEIBatchSize:    cfg.SEIBatchSize,
			SEIFragmentSize: cfg.SEIFragmentSize,
			SEIAckTimeoutMS: cfg.SEIAckTimeoutMS,
			Engine:          cfg.Engine,
			URL:             cfg.URL,
			Token:           cfg.Token,
			Liveness:        liveness,
			Traffic:         traffic,
			OnSessionOpen: func(sessionID, deviceID string, claims map[string]any) {
				logger.Infof("session opened: id=%s device=%s claims=%v", sessionID, deviceID, claims)
			},
			OnSessionClose: func(sessionID, reason string) {
				logger.Infof("session closed: id=%s reason=%s", sessionID, reason)
			},
			OnTraffic: func(sessionID, addr string, bytesIn, bytesOut uint64) {
				logger.Infof("traffic: session=%s addr=%s in=%d out=%d", sessionID, addr, bytesIn, bytesOut)
			},
		}); err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	case modeCNC:
		if err := client.Run(ctx, client.Config{
			Link:            cfg.Link,
			Transport:       cfg.Transport,
			Carrier:         cfg.Auth,
			RoomURL:         roomURL,
			ChannelID:       cfg.ChannelID,
			KeyHex:          cfg.KeyHex,
			LocalAddr:       fmt.Sprintf("%s:%d", cfg.SOCKSHost, cfg.SOCKSPort),
			DNSServer:       cfg.DNSServer,
			SOCKSUser:       cfg.SOCKSUser,
			SOCKSPass:       cfg.SOCKSPass,
			VideoWidth:      cfg.VideoWidth,
			VideoHeight:     cfg.VideoHeight,
			VideoFPS:        cfg.VideoFPS,
			VideoBitrate:    cfg.VideoBitrate,
			VideoHW:         cfg.VideoHW,
			VideoQRSize:     cfg.VideoQRSize,
			VideoQRRecovery: cfg.VideoQRRecovery,
			VideoCodec:      cfg.VideoCodec,
			VideoTileModule: cfg.VideoTileModule,
			VideoTileRS:     cfg.VideoTileRS,
			VP8FPS:          cfg.VP8FPS,
			VP8BatchSize:    cfg.VP8BatchSize,
			SEIFPS:          cfg.SEIFPS,
			SEIBatchSize:    cfg.SEIBatchSize,
			SEIFragmentSize: cfg.SEIFragmentSize,
			SEIAckTimeoutMS: cfg.SEIAckTimeoutMS,
			Engine:          cfg.Engine,
			URL:             cfg.URL,
			Token:           cfg.Token,
			Liveness:        liveness,
			Traffic:         traffic,
		}); err != nil {
			return fmt.Errorf("client: %w", err)
		}
		return nil
	default:
		return ErrModeRequired
	}
}

func runWithSessionRotation(ctx context.Context, maxDuration time.Duration, run func(context.Context) error) error {
	for cycle := 1; ; cycle++ {
		currentCycle := cycle
		runCtx, cancel := context.WithCancel(ctx)
		var rotated atomic.Bool
		timer := time.AfterFunc(maxDuration, func() {
			rotated.Store(true)
			logger.Infof("session max duration reached: duration=%s cycle=%d", maxDuration, currentCycle)
			cancel()
		})

		err := run(runCtx)
		cancel()
		timer.Stop()
		if ctx.Err() != nil {
			return nil //nolint:nilerr // parent cancellation is normal shutdown for rotation
		}
		if rotated.Load() {
			if err != nil {
				logger.Warnf("session rotation ended with error: cycle=%d err=%v", currentCycle, err)
			}
			logger.Infof("session rotation restarting: next_cycle=%d", currentCycle+1)
			if err := waitSessionRestart(ctx); err != nil {
				return nil //nolint:nilerr // canceled restart delay means normal shutdown
			}
			continue
		}
		if err != nil {
			return err
		}
		logger.Infof("session ended cleanly with lifecycle rotation enabled: next_cycle=%d", currentCycle+1)
		if err := waitSessionRestart(ctx); err != nil {
			return nil //nolint:nilerr // canceled restart delay means normal shutdown
		}
	}
}

func waitSessionRestart(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("restart delay canceled: %w", ctx.Err())
	case <-time.After(sessionRestartDelay):
		return nil
	}
}

// ValidateGen validates that the config contains enough fields to run gen mode.
func ValidateGen(cfg Config) error {
	if cfg.Auth == "" {
		return ErrAuthRequired
	}
	if !slices.Contains(carrier.Available(), cfg.Auth) {
		return fmt.Errorf("%w: %s (available: %v)", ErrUnsupportedCarrier, cfg.Auth, carrier.Available())
	}
	if cfg.DNSServer == "" {
		return ErrDNSServerRequired
	}
	if cfg.Amount < 1 {
		return ErrAmountRequired
	}
	return nil
}

const (
	genMaxAttempts = 5
	genRetryDelay  = 2 * time.Second
)

func genRetry(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	for attempt := range genMaxAttempts {
		lastErr = fn(ctx)
		if lastErr == nil {
			return nil
		}
		if attempt < genMaxAttempts-1 {
			select {
			case <-ctx.Done():
				return fmt.Errorf("context canceled: %w", ctx.Err())
			case <-time.After(genRetryDelay):
			}
		}
	}
	return lastErr
}

// Gen creates cfg.Amount rooms for the configured auth provider and writes each room ID to out.
func Gen(ctx context.Context, cfg Config, out func(string)) error {
	p, err := auth.Get(cfg.Auth)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnsupportedCarrier, cfg.Auth)
	}
	creator, ok := p.(auth.RoomCreator)
	if !ok {
		return fmt.Errorf("%w: %s does not support room generation", ErrUnsupportedCarrier, cfg.Auth)
	}
	for i := range cfg.Amount {
		var roomID string
		err := genRetry(ctx, func(ctx context.Context) error {
			var genErr error
			roomID, genErr = creator.CreateRoom(ctx, auth.Config{Name: names.Generate()})
			if genErr != nil {
				return fmt.Errorf("CreateRoom: %w", genErr)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("gen room %d: %w", i+1, err)
		}
		out(roomID)
	}
	return nil
}
