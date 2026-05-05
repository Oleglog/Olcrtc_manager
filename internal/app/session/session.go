// Package session wires runtime configuration to application mode entrypoints.
package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
	"github.com/openlibrecommunity/olcrtc/internal/carrier/builtin"
	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/link"
	"github.com/openlibrecommunity/olcrtc/internal/link/direct"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/server"
	subserver "github.com/openlibrecommunity/olcrtc/internal/subscription/server"
	"github.com/openlibrecommunity/olcrtc/internal/subscription/store"
	"github.com/openlibrecommunity/olcrtc/internal/transport"
	"github.com/openlibrecommunity/olcrtc/internal/transport/datachannel"
	"github.com/openlibrecommunity/olcrtc/internal/transport/seichannel"
	"github.com/openlibrecommunity/olcrtc/internal/transport/videochannel"
	"github.com/openlibrecommunity/olcrtc/internal/transport/vp8channel"
)

const (
	modeSRV = "srv"
	modeCNC = "cnc"
)

var (
	// ErrRoomIDRequired indicates that a room id is required for the selected carrier.
	ErrRoomIDRequired = errors.New("room ID required (use -id <id>)")
	// ErrModeRequired indicates that mode is not one of the supported values.
	ErrModeRequired = errors.New("mode required (use -mode srv or -mode cnc)")
	// ErrCarrierRequired indicates that no carrier was selected.
	ErrCarrierRequired = errors.New(
		"carrier required (use -carrier telemost, -carrier jazz or -carrier wbstream)")
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
	Carrier         string
	RoomID          string
	KeyHex          string
	SOCKSHost       string
	SOCKSPort       int
	DNSServer       string
	SOCKSProxyAddr  string
	SOCKSProxyPort  int
	SOCKSProxyUser  string
	SOCKSProxyPass  string
	WarpProxyAddr   string
	WarpProxyPort   int
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
	SubEnabled      bool
	SubPort         int
	SubDBPath       string
	SubAPIToken     string
}

// RegisterDefaults registers built-in providers and transports.
func RegisterDefaults() {
	builtin.Register()
	link.Register("direct", direct.New)
	transport.Register("datachannel", datachannel.New)
	transport.Register("videochannel", videochannel.New)
	transport.Register("seichannel", seichannel.New)
	transport.Register("vp8channel", vp8channel.New)
}

// Validate verifies that the runtime config refers to registered components and all required fields are present.
func Validate(cfg Config) error {
	if err := validateMode(cfg); err != nil {
		return err
	}
	if err := validateCarrier(cfg); err != nil {
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
	if cfg.Mode == "" || (cfg.Mode != modeSRV && cfg.Mode != modeCNC) {
		return ErrModeRequired
	}
	return nil
}

func validateCarrier(cfg Config) error {
	if cfg.Carrier == "" {
		return ErrCarrierRequired
	}
	for _, c := range carrier.Available() {
		if cfg.Carrier == c {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (available: %v)", ErrUnsupportedCarrier, cfg.Carrier, carrier.Available())
}

func validateLink(cfg Config) error {
	if cfg.Link == "" {
		return ErrLinkRequired
	}
	for _, l := range link.Available() {
		if cfg.Link == l {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (available: %v)", ErrUnsupportedLink, cfg.Link, link.Available())
}

func validateTransportRegistration(cfg Config) error {
	if cfg.Transport == "" {
		return ErrTransportRequired
	}
	for _, t := range transport.Available() {
		if cfg.Transport == t {
			return nil
		}
	}
	return fmt.Errorf("%w: %s (available: %v)", ErrUnsupportedTransport, cfg.Transport, transport.Available())
}

func validateCommon(cfg Config) error {
	if cfg.RoomID == "" && cfg.Carrier != "jazz" {
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
	case "videochannel":
		return validateVideoChannel(cfg)
	case "vp8channel":
		return validateVP8Channel(cfg)
	default:
		return nil
	}
}

func validateVideoCodec(cfg Config) error {
	if cfg.VideoCodec != "" && cfg.VideoCodec != "qrcode" && cfg.VideoCodec != "tile" {
		return ErrVideoCodecInvalid
	}
	if cfg.VideoCodec == "tile" && (cfg.VideoWidth != 1080 || cfg.VideoHeight != 1080) {
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
	roomURL := buildRoomURL(cfg.Carrier, cfg.RoomID)

	switch cfg.Mode {
	case modeSRV:
		if cfg.SubEnabled {
			if err := startSubscriptionServer(ctx, cfg); err != nil {
				logger.Warnf("subscription server failed to start (continuing without it): %v", err)
			}
		}
		if err := server.Run(
			ctx,
			cfg.Link,
			cfg.Transport,
			cfg.Carrier,
			roomURL,
			cfg.KeyHex,
			cfg.DNSServer,
			cfg.SOCKSProxyAddr,
			cfg.SOCKSProxyPort,
			cfg.SOCKSProxyUser,
			cfg.SOCKSProxyPass,
			cfg.WarpProxyAddr,
			cfg.WarpProxyPort,
			cfg.VideoWidth,
			cfg.VideoHeight,
			cfg.VideoFPS,
			cfg.VideoBitrate,
			cfg.VideoHW,
			cfg.VideoQRSize,
			cfg.VideoQRRecovery,
			cfg.VideoCodec,
			cfg.VideoTileModule,
			cfg.VideoTileRS,
			cfg.VP8FPS,
			cfg.VP8BatchSize,
		); err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	case modeCNC:
		if err := client.Run(
			ctx,
			cfg.Link,
			cfg.Transport,
			cfg.Carrier,
			roomURL,
			cfg.KeyHex,
			fmt.Sprintf("%s:%d", cfg.SOCKSHost, cfg.SOCKSPort),
			cfg.DNSServer,
			"",
			"",
			cfg.VideoWidth,
			cfg.VideoHeight,
			cfg.VideoFPS,
			cfg.VideoBitrate,
			cfg.VideoHW,
			cfg.VideoQRSize,
			cfg.VideoQRRecovery,
			cfg.VideoCodec,
			cfg.VideoTileModule,
			cfg.VideoTileRS,
			cfg.VP8FPS,
			cfg.VP8BatchSize,
		); err != nil {
			return fmt.Errorf("client: %w", err)
		}
		return nil
	default:
		return ErrModeRequired
	}
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

func buildRoomURL(carrierName, roomID string) string {
	switch carrierName {
	case "telemost":
		return "https://telemost.yandex.ru/j/" + roomID
	case "jazz":
		if roomID == "" {
			return "any"
		}
		return roomID
	case "wbstream":
		return roomID
	default:
		return roomID
	}
}
