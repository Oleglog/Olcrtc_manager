// Package session wires runtime configuration to application mode entrypoints.
package session

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/auth"
	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/carrier/builtin"
	"github.com/openlibrecommunity/olcrtc/internal/client"
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
	subserver "github.com/openlibrecommunity/olcrtc/internal/subscription/server"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/store"
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

var (
	// ErrRoomIDRequired indicates that a room id is required for the selected carrier.
	ErrRoomIDRequired = errors.New("room ID required (use -id <id>)")
	// ErrModeRequired indicates that mode is not one of the supported values.
	ErrModeRequired = errors.New("mode required (use -mode srv, -mode cnc or -mode gen)")
	// ErrAmountRequired indicates that -amount is required for gen mode.
	ErrAmountRequired = errors.New("amount required for gen mode (use -amount <n>)")
	// ErrAuthRequired indicates that no auth provider was selected.
	ErrAuthRequired = errors.New(
		"auth provider required (use -auth telemost, -auth jazz, -auth wbstream or -auth none)")
	// ErrURLRequired indicates that -url must be provided when the auth provider has no default URL.
	ErrURLRequired = errors.New("SFU URL required (use -url wss://...)")
	// ErrUnsupportedCarrier indicates that carrier is not registered.
	ErrUnsupportedCarrier = errors.New("unsupported carrier")
	// ErrUnsupportedLink indicates that link is not registered.
	ErrUnsupportedLink = errors.New("unsupported link")
	// ErrUnsupportedTransport indicates that transport is not registered.
	ErrUnsupportedTransport = errors.New("unsupported transport")

	// ErrLinkRequired indicates that link is not provided.
	ErrLinkRequired = errors.New("link required (use -link direct)")
	// ErrTransportRequired indicates that transport is not provided.
	ErrTransportRequired = errors.New(
		"transport required (use -transport datachannel, -transport videochannel, " +
			"-transport seichannel or -transport vp8channel)")
	// ErrKeyRequired indicates that encryption key is not provided.
	ErrKeyRequired = errors.New("key required (use -key <hex>)")
	// ErrDNSServerRequired indicates that dns server is not provided.
	ErrDNSServerRequired = errors.New("dns server required (use -dns 1.1.1.1:53)")

	// ErrVideoWidthRequired indicates that video width is required for videochannel.
	ErrVideoWidthRequired = errors.New("video width required for videochannel (use -video-w)")
	// ErrVideoHeightRequired indicates that video height is required for videochannel.
	ErrVideoHeightRequired = errors.New("video height required for videochannel (use -video-h)")
	// ErrVideoFPSRequired indicates that video fps is required for videochannel.
	ErrVideoFPSRequired = errors.New("video fps required for videochannel (use -video-fps)")
	// ErrVideoBitrateRequired indicates that video bitrate is required for videochannel.
	ErrVideoBitrateRequired = errors.New(
		"video bitrate required for videochannel (use -video-bitrate)")
	// ErrVideoHWRequired indicates that video hardware acceleration is required.
	ErrVideoHWRequired = errors.New(
		"video hardware acceleration required for videochannel (use -video-hw none/nvenc)")
	// ErrVideoCodecInvalid indicates that the video codec is not valid.
	ErrVideoCodecInvalid = errors.New(
		"invalid video codec for videochannel (use -video-codec qrcode or -video-codec tile)")
	// ErrTileCodecDimensions indicates that tile codec requires 1080x1080 dimensions.
	ErrTileCodecDimensions = errors.New("tile codec requires -video-w 1080 -video-h 1080")

	// ErrVP8FPSRequired indicates that vp8 fps is required for vp8channel.
	ErrVP8FPSRequired = errors.New("vp8 fps required for vp8channel (use -vp8-fps)")
	// ErrVP8BatchSizeRequired indicates that vp8 batch size is required for vp8channel.
	ErrVP8BatchSizeRequired = errors.New("vp8 batch size required for vp8channel (use -vp8-batch)")
	// ErrSEIFPSRequired indicates that seichannel fps is required.
	ErrSEIFPSRequired = errors.New("fps required for seichannel (use -fps)")
	// ErrSEIBatchSizeRequired indicates that seichannel batch size is required.
	ErrSEIBatchSizeRequired = errors.New("batch size required for seichannel (use -batch)")
	// ErrSEIFragmentSizeRequired indicates that seichannel fragment size is required.
	ErrSEIFragmentSizeRequired = errors.New("fragment size required for seichannel (use -frag)")
	// ErrSEIAckTimeoutRequired indicates that seichannel ack timeout is required.
	ErrSEIAckTimeoutRequired = errors.New("ack timeout required for seichannel (use -ack-ms)")

	// ErrSOCKSHostRequired indicates that socks host is required for cnc mode.
	ErrSOCKSHostRequired = errors.New("socks host required for cnc mode (use -socks-host)")
	// ErrSOCKSPortRequired indicates that socks port is required for cnc mode.
	ErrSOCKSPortRequired = errors.New("socks port required for cnc mode (use -socks-port)")
)

// Config holds runtime session settings.
type Config struct {
	Mode            string
	Link            string
	Transport       string
	Auth            string
	Engine          string
	URL             string
	Token           string
	RoomID          string
	KeyHex          string
	SOCKSHost       string
	SOCKSPort       int
	SOCKSUser       string
	SOCKSPass       string
	DNSServer       string
	SOCKSProxyAddr  string
	SOCKSProxyPort  int
	VideoWidth      int
	VideoHeight     int
	VideoFPS        int
	VideoBitrate    string
	VideoHW         string
	VideoQRSize     int
	VideoQRRecovery string
	VideoCodec      string
	VideoTileModule int
	VideoTileRS     int
	VP8FPS          int
	VP8BatchSize    int
	SEIFPS          int
	SEIBatchSize    int
	SEIFragmentSize int
	SEIAckTimeoutMS int
	Amount          int

	// Наши фичи, которых нет в refactor:
	SOCKSProxyUser string
	SOCKSProxyPass string
	WarpProxyAddr  string
	WarpProxyPort  int
	SubEnabled     bool
	SubPort        int
	SubDBPath      string
	SubAPIToken    string
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
// Returns an error if the auth provider has no default URL and -url was not given.
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
	if cfg.URL == "" {
		return cfg, fmt.Errorf("%w: auth provider %q has no default URL", ErrURLRequired, cfg.Auth)
	}
	return cfg, nil
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
	return nil
}

// Run starts the configured mode.
func Run(ctx context.Context, cfg Config) error {
	roomURL := cfg.RoomID

	switch cfg.Mode {
	case modeSRV:
		if cfg.SubEnabled {
			if err := startSubscriptionServer(ctx, cfg); err != nil {
				logger.Warnf("subscription server failed to start (continuing without it): %v", err)
			}
		}
		if err := server.Run(ctx, server.Config{
			Link:            cfg.Link,
			Transport:       cfg.Transport,
			Carrier:         cfg.Auth,
			RoomURL:         roomURL,
			KeyHex:          cfg.KeyHex,
			DNSServer:       cfg.DNSServer,
			SOCKSProxyAddr:  cfg.SOCKSProxyAddr,
			SOCKSProxyPort:  cfg.SOCKSProxyPort,
			SOCKSProxyUser:  cfg.SOCKSProxyUser,
			SOCKSProxyPass:  cfg.SOCKSProxyPass,
			WarpProxyAddr:   cfg.WarpProxyAddr,
			WarpProxyPort:   cfg.WarpProxyPort,
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
			KeyHex:          cfg.KeyHex,
			LocalAddr:       fmt.Sprintf("%s:%d", cfg.SOCKSHost, cfg.SOCKSPort),
			DNSServer:       cfg.DNSServer,
			SOCKSUser:       cfg.SOCKSUser,
			SOCKSPass:       cfg.SOCKSPass,
			WarpProxyAddr:   cfg.WarpProxyAddr,
			WarpProxyPort:   cfg.WarpProxyPort,
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
		}); err != nil {
			return fmt.Errorf("client: %w", err)
		}
		return nil
	default:
		return ErrModeRequired
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

func startSubscriptionServer(ctx context.Context, cfg Config) error {
	dbPath := cfg.SubDBPath
	if dbPath == "" {
		dbPath = "data/subscriptions.db"
	}
	port := cfg.SubPort
	if port == 0 {
		port = 2096
	}

	st, err := store.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open subscription db: %w", err)
	}

	srv := subserver.New(st, port, cfg.SubAPIToken)
	go func() {
		if err := srv.Start(ctx); err != nil {
			logger.Errorf("subscription server stopped: %v", err)
		}
		_ = st.Close()
	}()

	return nil
}
