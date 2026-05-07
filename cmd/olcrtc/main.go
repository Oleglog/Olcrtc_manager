// Package main provides the olcrtc CLI entrypoint.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	lksdk "github.com/livekit/server-sdk-go/v2"
	protoLogger "github.com/livekit/protocol/logger"
	"github.com/openlibrecommunity/olcrtc/internal/app/session"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/names"
)

// ErrDataDirRequired is returned when no data directory is specified.
var ErrDataDirRequired = errors.New("data directory required (use -data data)")

type config struct {
	mode           string
	link           string
	transport      string
	carrier        string
	roomID         string
	provider       string
	socksPort      int
	socksHost      string
	keyHex         string
	debug          bool
	dataDir        string
	dnsServer      string
	socksProxyAddr string
	socksProxyPort int
	socksProxyUser string
	socksProxyPass string
	warpProxyAddr  string
	warpProxyPort  int
	videoWidth     int
	videoHeight    int
	videoFPS       int
	videoBitrate   string
	videoHW        string
	videoQRSize     int
	videoQRRecovery string
	videoCodec      string
	videoTileModule int
	videoTileRS     int
	vp8FPS          int
        vp8BatchSize    int
        subEnabled      bool
        subPort         int
        subDBPath       string
        subAPIToken     string
}


func main() {
	if err := run(); err != nil {
		logger.Error(err)
		os.Exit(1)
	}
}

func run() error {
	session.RegisterDefaults()

	cfg := parseFlags()
	configureLogging(cfg.debug)

	if err := session.Validate(toSessionConfig(cfg)); err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	if cfg.dataDir == "" {
		return ErrDataDirRequired
	}

	dataDir, err := resolveDataDir(cfg.dataDir)
	if err != nil {
		return err
	}

	if err := loadNames(dataDir); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- session.Run(ctx, toSessionConfig(cfg))
	}()

	select {
	case <-sigCh:
		logger.Info("Shutting down gracefully...")
		cancel()
		return waitForShutdown(errCh)
	case err := <-errCh:
		return err
	}
}

func parseFlags() config {
	cfg := config{}

	flag.StringVar(&cfg.mode, "mode", "", "Mode: srv or cnc")
	flag.StringVar(&cfg.link, "link", "", "Link: direct (p2p connection type)")
	flag.StringVar(&cfg.transport, "transport", "", "Transport: datachannel, videochannel, seichannel")
	flag.StringVar(&cfg.carrier, "carrier", "", "Carrier: telemost, jazz, wbstream")
	flag.StringVar(&cfg.roomID, "id", "", "Room ID")
	flag.StringVar(&cfg.provider, "provider", "", "Deprecated alias for -carrier")
	flag.IntVar(&cfg.socksPort, "socks-port", 0, "SOCKS5 port (client only)")
	flag.StringVar(&cfg.socksHost, "socks-host", "", "SOCKS5 listen host (client only)")
	flag.StringVar(&cfg.keyHex, "key", "", "Shared encryption key (hex)")
	flag.BoolVar(&cfg.debug, "debug", false, "Enable verbose logging")
	flag.StringVar(&cfg.dataDir, "data", "", "Path to data directory")
	flag.StringVar(&cfg.dnsServer, "dns", "", "DNS server (e.g. 1.1.1.1:53)")
	flag.StringVar(&cfg.socksProxyAddr, "socks-proxy", "", "SOCKS5 proxy address (server only)")
	flag.IntVar(&cfg.socksProxyPort, "socks-proxy-port", 0, "SOCKS5 proxy port (server only)")
	flag.StringVar(&cfg.socksProxyUser, "socks-proxy-user", "", "SOCKS5 proxy username (RFC 1929, server only)")
	flag.StringVar(&cfg.socksProxyPass, "socks-proxy-pass", "", "SOCKS5 proxy password (RFC 1929, server only)")
	flag.StringVar(&cfg.warpProxyAddr, "warp-proxy", "", "WARP SOCKS5 proxy address for client tunnel traffic (server only)")
	flag.IntVar(&cfg.warpProxyPort, "warp-proxy-port", 40000, "WARP SOCKS5 proxy port (server only)")
	flag.IntVar(&cfg.videoWidth, "video-w", 0, "Video logical width (videochannel only)")
	flag.IntVar(&cfg.videoHeight, "video-h", 0, "Video logical height (videochannel only)")
	flag.IntVar(&cfg.videoFPS, "video-fps", 0, "Video frames per second (videochannel only)")
	flag.StringVar(&cfg.videoBitrate, "video-bitrate", "", "Video bitrate (videochannel only)")
	flag.StringVar(&cfg.videoHW, "video-hw", "", "Hardware acceleration (none, nvenc)")
	flag.IntVar(&cfg.videoQRSize, "video-qr-size", 0, "Video QR code fragment size (videochannel only)")
	flag.StringVar(&cfg.videoQRRecovery, "video-qr-recovery", "low",
		"QR error correction: low (7%), medium (15%), high (25%), highest (30%)")
	flag.StringVar(&cfg.videoCodec, "video-codec", "qrcode", "Visual codec: qrcode or tile")
	flag.IntVar(&cfg.videoTileModule, "video-tile-module", 0,
		"Tile module size in pixels 1..270 (videochannel tile only, default 4)")
	flag.IntVar(&cfg.videoTileRS, "video-tile-rs", 0,
		"Tile Reed-Solomon parity percent 0..200 (videochannel tile only, default 20)")
	flag.IntVar(&cfg.vp8FPS, "vp8-fps", 0, "VP8 frames per second (vp8channel only, default 25)")
	flag.IntVar(&cfg.vp8BatchSize, "vp8-batch", 0, "VP8 frames per tick (vp8channel only, default 1)")
        flag.BoolVar(&cfg.subEnabled, "sub-enabled", false, "Enable subscription HTTP server")
        flag.IntVar(&cfg.subPort, "sub-port", 2096, "Subscription server listen port")
        flag.StringVar(&cfg.subDBPath, "sub-db", "", "Subscription database path")
        flag.StringVar(&cfg.subAPIToken, "sub-token", "", "Subscription API bearer token")
	flag.Parse()

	return cfg
}

func configureLogging(debug bool) {
	if debug {
		logger.SetVerbose(true)
		return
	}
	// Suppress noisy LiveKit/pion logs unless debug is enabled.
	_ = os.Setenv("PION_LOG_DISABLE", "all")
	lksdk.SetLogger(protoLogger.GetDiscardLogger())
}

func resolveDataDir(dataDir string) (string, error) {
	if filepath.IsAbs(dataDir) {
		return dataDir, nil
	}

	exePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}

	return filepath.Join(filepath.Dir(exePath), dataDir), nil
}

func loadNames(dataDir string) error {
	namesPath := filepath.Join(dataDir, "names")
	surnamesPath := filepath.Join(dataDir, "surnames")
	if err := names.LoadNameFiles(namesPath, surnamesPath); err != nil {
		return fmt.Errorf("load embedded names override: %w", err)
	}

	return nil
}

func toSessionConfig(cfg config) session.Config {
	return session.Config{
		Mode:            cfg.mode,
		Link:            cfg.link,
		Transport:       cfg.transport,
		Carrier:         firstNonEmpty(cfg.carrier, cfg.provider),
		RoomID:          cfg.roomID,
		KeyHex:          cfg.keyHex,
		SOCKSHost:       cfg.socksHost,
		SOCKSPort:       cfg.socksPort,
		DNSServer:       cfg.dnsServer,
		SOCKSProxyAddr:  cfg.socksProxyAddr,
		SOCKSProxyPort:  cfg.socksProxyPort,
		SOCKSProxyUser:  cfg.socksProxyUser,
		SOCKSProxyPass:  cfg.socksProxyPass,
		WarpProxyAddr:  cfg.warpProxyAddr,
		WarpProxyPort:  cfg.warpProxyPort,
		VideoWidth:      cfg.videoWidth,
		VideoHeight:     cfg.videoHeight,
		VideoFPS:        cfg.videoFPS,
		VideoBitrate:    cfg.videoBitrate,
		VideoHW:         cfg.videoHW,
		VideoQRSize:     cfg.videoQRSize,
		VideoQRRecovery: cfg.videoQRRecovery,
		VideoCodec:      cfg.videoCodec,
		VideoTileModule: cfg.videoTileModule,
		VideoTileRS:     cfg.videoTileRS,
		VP8FPS:          cfg.vp8FPS,
		VP8BatchSize:    cfg.vp8BatchSize,
                SubEnabled:      cfg.subEnabled,
                SubPort:         cfg.subPort,
                SubDBPath:       cfg.subDBPath,
                SubAPIToken:     cfg.subAPIToken,
        }
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func waitForShutdown(errCh <-chan error) error {
	done := make(chan error, 1)
	go func() {
		if err := <-errCh; err != nil {
			done <- err
		} else {
			done <- nil
		}
	}()

	select {
	case err := <-done:
		if err == nil {
			logger.Info("Shutdown complete")
		}
		return err
	case <-time.After(5 * time.Second):
		logger.Warn("Shutdown timeout, forcing exit")
		return nil
	}
}
