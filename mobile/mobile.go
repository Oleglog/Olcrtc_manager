// Package mobile provides a gomobile-compatible API for olcRTC.
// Build with: gomobile bind -target=android ./mobile
package mobile

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/app/session"
	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/protect"

	_ "golang.org/x/mobile/bind"                       // ensure gomobile bind is available
	_ "google.golang.org/genproto/protobuf/field_mask" // keep gomobile on post-split genproto modules
)

// SocketProtector protects sockets from VPN routing on Android.
// Implement this interface in Kotlin/Java and pass to SetProtector.
type SocketProtector interface {
	Protect(fd int) bool
}

// LogWriter receives log messages from olcRTC.
type LogWriter interface {
	WriteLog(msg string)
}

var (
	errAlreadyRunning     = errors.New("olcRTC already running")
	errCarrierRequired    = errors.New("carrier is required")
	errRoomIDRequired     = errors.New("roomID is required")
	errKeyHexRequired     = errors.New("keyHex is required")
	errNotRunning         = errors.New("olcRTC is not running")
	errStoppedBeforeReady = errors.New("olcRTC stopped before becoming ready")
	errStartTimedOut      = errors.New("olcRTC start timed out")
)

const (
	defaultLink      = "direct"
	defaultTransport = "vp8channel"
	dataTransport    = "datachannel"
	defaultDNSServer = "1.1.1.1:53"
	carrierWBStream  = "wbstream"
)

//nolint:gochecknoglobals // Mobile bindings expose a singleton runtime controlled by the embedding app.
var (
	mu          sync.Mutex
	defaults    mobileConfig
	defaultsSet sync.Once
	cancel      context.CancelFunc
	done        chan struct{}
	ready       chan struct{}
	errRun      error
)

type mobileConfig struct {
	link         string
	transport    string
	dnsServer    string
	vp8FPS       int
	vp8BatchSize int
}

// SetProtector sets the Android VPN socket protector.
// Must be called before Start.
func SetProtector(p SocketProtector) {
	if p == nil {
		protect.Protector = nil
		return
	}
	protect.Protector = func(fd int) bool {
		return p.Protect(fd)
	}
}

// SetLogWriter sets a custom log writer for olcRTC output.
func SetLogWriter(w LogWriter) {
	if w != nil {
		log.SetOutput(&logBridge{w: w})
	}
}

// SetProviders registers built-in carriers, links, and transports.
func SetProviders() {
	registerDefaults()
}

// SetTransport selects the transport used by Start.
// Supported values: vp8channel and datachannel.
func SetTransport(transport string) {
	mu.Lock()
	defer mu.Unlock()
	ensureDefaultConfigLocked()
	defaults.transport = normalizeTransport(transport)
}

// SetLink selects the link used by Start.
// Supported value today: direct.
func SetLink(link string) {
	mu.Lock()
	defer mu.Unlock()
	ensureDefaultConfigLocked()
	defaults.link = link
}

// SetDNS selects the DNS server used by the tunnel.
func SetDNS(dnsServer string) {
	mu.Lock()
	defer mu.Unlock()
	ensureDefaultConfigLocked()
	defaults.dnsServer = dnsServer
}

// SetVP8Options configures vp8channel.
func SetVP8Options(fps, batchSize int) {
	mu.Lock()
	defer mu.Unlock()
	ensureDefaultConfigLocked()
	defaults.vp8FPS = clamp(fps, 1, 120)
	defaults.vp8BatchSize = clamp(batchSize, 1, 32)
}

// SetDebug enables or disables verbose logging.
func SetDebug(enabled bool) {
	logger.SetVerbose(enabled)
	if enabled {
		log.SetFlags(log.Ltime | log.Lshortfile)
		return
	}

	log.SetFlags(log.Ltime)
}

// Start launches the olcRTC client in background.
// carrierName: carrier/provider name ("telemost", "jazz", "wbstream", "wbstream")
// roomID: carrier-specific room ID
// keyHex: 64-char hex encryption key
// socksPort: local SOCKS5 proxy port (e.g. 10808)
// socksUser/socksPass: SOCKS5 credentials (empty = no auth).
func Start(carrierName, roomID, keyHex string, socksPort int, socksUser, socksPass string) error {
	mu.Lock()
	ensureDefaultConfigLocked()
	cfg := defaults
	mu.Unlock()

	return startWithConfig(carrierName, cfg.transport, roomID, keyHex, socksPort, socksUser, socksPass, cfg)
}

// StartWithTransport launches the client with an explicit transport for this start.
func StartWithTransport(
	carrierName, transportName, roomID, keyHex string,
	socksPort int,
	socksUser, socksPass string,
) error {
	mu.Lock()
	ensureDefaultConfigLocked()
	cfg := defaults
	cfg.transport = transportName
	mu.Unlock()

	return startWithConfig(carrierName, transportName, roomID, keyHex, socksPort, socksUser, socksPass, cfg)
}

func startWithConfig(
	carrierName, transportName, roomID, keyHex string,
	socksPort int,
	socksUser, socksPass string,
	cfg mobileConfig,
) error {
	mu.Lock()
	defer mu.Unlock()

	registerDefaults()
	carrierName = normalizeCarrier(carrierName)
	if transportName != "" {
		cfg.transport = normalizeTransport(transportName)
	}

	switch {
	case cancel != nil:
		return errAlreadyRunning
	case carrierName == "":
		return errCarrierRequired
	case roomID == "" && carrierName != "jazz":
		return errRoomIDRequired
	case keyHex == "":
		return errKeyHexRequired
	}

	roomURL := buildRoomURL(carrierName, roomID)

	ctx, cancelFunc := context.WithCancel(context.Background())
	cancel = cancelFunc
	done = make(chan struct{})
	ready = make(chan struct{})
	localReady := ready
	errRun = nil

	var readyOnce sync.Once
	go func() {
		defer cancelFunc()

		err := client.RunWithReady(
			ctx,
			cfg.link,
			cfg.transport,
			carrierName,
			roomURL,
			keyHex,
			fmt.Sprintf("127.0.0.1:%d", socksPort),
			cfg.dnsServer,
			socksUser,
			socksPass,
			func() {
				readyOnce.Do(func() {
					close(localReady)
				})
			},
			0,
			0,
			0,
			"",
			"",
			0,
			"",
			"",
			0,
			0,
			cfg.vp8FPS,
			cfg.vp8BatchSize,
		)

		mu.Lock()
		cancel = nil
		errRun = err
		mu.Unlock()
		close(done)
	}()

	return nil
}

// WaitReady blocks until the selected transport is connected and the local SOCKS5 listener is ready.
//
//nolint:cyclop // The control flow is intentionally linear so mobile callers can observe each startup state clearly.
func WaitReady(timeoutMillis int) error {
	mu.Lock()
	r := ready
	d := done
	runErr := errRun
	running := cancel != nil
	mu.Unlock()

	if r == nil {
		if runErr != nil {
			return runErr
		}

		return errNotRunning
	}

	select {
	case <-r:
		return nil
	default:
	}

	if !running {
		if runErr != nil {
			return runErr
		}

		return errStoppedBeforeReady
	}

	timer := time.NewTimer(time.Duration(timeoutMillis) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-r:
		return nil
	case <-d:
		mu.Lock()
		runErr = errRun
		mu.Unlock()
		if runErr != nil {
			return runErr
		}

		return errStoppedBeforeReady
	case <-timer.C:
		return errStartTimedOut
	}
}

// Stop gracefully stops the olcRTC client.
func Stop() {
	mu.Lock()
	cancelFunc := cancel
	doneCh := done
	mu.Unlock()

	if cancelFunc == nil {
		return
	}

	cancelFunc()

	if doneCh != nil {
		<-doneCh
	}
}

// IsRunning returns true if the olcRTC client is active.
func IsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return cancel != nil
}

func registerDefaults() {
	session.RegisterDefaults()
}

func ensureDefaultConfigLocked() {
	defaultsSet.Do(func() {
		defaults = mobileConfig{
			link:         defaultLink,
			transport:    defaultTransport,
			dnsServer:    defaultDNSServer,
			vp8FPS:       60,
			vp8BatchSize: 8,
		}
	})
}

func normalizeTransport(value string) string {
	switch value {
	case dataTransport, "data", "dc":
		return dataTransport
	case defaultTransport, "vp8":
		return defaultTransport
	default:
		return defaultTransport
	}
}

func normalizeCarrier(carrierName string) string {
	if carrierName == carrierWBStream {
		return carrierWBStream
	}
	return carrierName
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
	case carrierWBStream:
		return roomID
	default:
		return roomID
	}
}

func clamp(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// logBridge adapts LogWriter to io.Writer.
type logBridge struct {
	w LogWriter
}

func (b *logBridge) Write(p []byte) (int, error) {
	b.w.WriteLog(string(p))
	return len(p), nil
}
