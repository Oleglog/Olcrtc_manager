package mobile

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/control"
	"github.com/openlibrecommunity/olcrtc/internal/logger"
	"github.com/openlibrecommunity/olcrtc/internal/protect"
)

type testProtector struct {
	called int
}

func (p *testProtector) Protect(fd int) bool {
	p.called = fd
	return true
}

type testLogWriter struct {
	got string
}

func (w *testLogWriter) WriteLog(msg string) {
	w.got += msg
}

func resetMobileGlobals(t *testing.T) {
	t.Helper()
	mu.Lock()
	if cancel != nil {
		cancel()
	}
	cancel = nil
	done = nil
	ready = nil
	errRun = nil
	runClientWithReady = clientRunWithReady
	defaults = mobileConfig{}
	defaultsSet = sync.Once{}
	mu.Unlock()
	protect.Protector = nil
	logger.SetVerbose(false)
}

var clientRunWithReady = runClientWithReady //nolint:gochecknoglobals // package-level state intentional

var (
	errMobileCheckFailed = errors.New("check failed")
	errMobileRunFailed   = errors.New("run failed")
)

func TestProtectorAndLogging(t *testing.T) {
	resetMobileGlobals(t)
	p := &testProtector{}
	SetProtector(p)
	if protect.Protector == nil || !protect.Protector(123) || p.called != 123 {
		t.Fatal("SetProtector() did not install adapter")
	}
	SetProtector(nil)
	if protect.Protector != nil {
		t.Fatal("SetProtector(nil) did not clear protector")
	}

	w := &testLogWriter{}
	SetLogWriter(w)
	log.Print("hello")
	if !strings.Contains(w.got, "hello") {
		t.Fatalf("log writer got %q, want hello", w.got)
	}
}

//nolint:cyclop // compact setter smoke test verifies several related defaults together
func TestDefaultsAndSetters(t *testing.T) {
	resetMobileGlobals(t)

	SetTransport("dc")
	SetLink("direct")
	SetDNS("9.9.9.9:53")
	SetVP8Options(-1, 999)
	SetLivenessOptions(2500, 750, -1)

	mu.Lock()
	got := defaults
	mu.Unlock()
	if got.transport != dataTransport || got.link != defaultLink || got.dnsServer != "9.9.9.9:53" ||
		got.vp8FPS != 1 || got.vp8BatchSize != 64 ||
		got.livenessInterval != 2500*time.Millisecond || got.livenessTimeout != 750*time.Millisecond ||
		got.livenessFailures != control.DefaultFailures {
		t.Fatalf("defaults = %+v", got)
	}

	SetDebug(true)
	if !logger.IsVerbose() {
		t.Fatal("SetDebug(true) did not enable verbose")
	}
	SetDebug(false)
	if logger.IsVerbose() {
		t.Fatal("SetDebug(false) did not disable verbose")
	}
}

//nolint:cyclop // table-driven test naturally has many branches
func TestNormalizeBuildRoomAndClamp(t *testing.T) {
	tests := map[string]string{
		"datachannel": dataTransport,
		"data":        dataTransport,
		"dc":          dataTransport,
		"vp8channel":  defaultTransport,
		"vp8":         defaultTransport,
		"bad":         defaultTransport,
	}
	for in, want := range tests {
		if got := normalizeTransport(in); got != want {
			t.Fatalf("normalizeTransport(%q) = %q, want %q", in, got, want)
		}
	}

	if normalizeCarrier(carrierWBStream) != carrierWBStream || normalizeCarrier("jazz") != "jazz" {
		t.Fatal("normalizeCarrier() returned unexpected value")
	}

	if got := buildRoomURL("telemost", "abc"); got != "https://telemost.yandex.ru/j/abc" {
		t.Fatalf("telemost room URL = %q", got)
	}
	if got := buildRoomURL("jazz", ""); got != "any" {
		t.Fatalf("jazz empty room URL = %q", got)
	}
	if got := buildRoomURL(carrierWBStream, "room"); got != "room" {
		t.Fatalf("wbstream room URL = %q", got)
	}

	if clampAtLeastOne(0, 10) != 1 || clampAtLeastOne(11, 10) != 10 || clampAtLeastOne(5, 10) != 5 {
		t.Fatal("clampAtLeastOne() returned unexpected value")
	}
}

func TestStartValidation(t *testing.T) {
	resetMobileGlobals(t)

	if err := startWithConfig("", dataTransport, "room", "client", "key", 1080, "", "", mobileConfig{}); !errors.Is(err, errCarrierRequired) { //nolint:lll // long test description
		t.Fatalf("startWithConfig(missing carrier) = %v", err)
	}
	if err := startWithConfig("telemost", dataTransport, "", "client", "key", 1080, "", "", mobileConfig{}); !errors.Is(err, errRoomIDRequired) { //nolint:lll // long test description
		t.Fatalf("startWithConfig(missing room) = %v", err)
	}
	if err := startWithConfig("jazz", dataTransport, "", "", "key", 1080, "", "", mobileConfig{}); !errors.Is(err, errClientIDRequired) { //nolint:lll // long test description
		t.Fatalf("startWithConfig(missing client) = %v", err)
	}
	if err := startWithConfig("jazz", dataTransport, "", "client", "", 1080, "", "", mobileConfig{}); !errors.Is(err, errKeyHexRequired) { //nolint:lll // long test description
		t.Fatalf("startWithConfig(missing key) = %v", err)
	}

	mu.Lock()
	cancel = func() {}
	mu.Unlock()
	if err := startWithConfig("jazz", dataTransport, "", "client", "key", 1080, "", "", mobileConfig{}); !errors.Is(err, errAlreadyRunning) { //nolint:lll // long test description
		t.Fatalf("startWithConfig(running) = %v", err)
	}
	resetMobileGlobals(t)
}

//nolint:cyclop // table-driven test naturally has many branches
func TestStartWithInjectedRunnerLifecycle(t *testing.T) {
	resetMobileGlobals(t)
	t.Cleanup(func() {
		resetMobileGlobals(t)
	})
	SetLivenessOptions(2500, 750, 4)

	runClientWithReady = func(ctx context.Context, cfg client.Config, onReady func()) error {
		if cfg.Link != defaultLink || cfg.Transport != dataTransport || cfg.Carrier != carrierJazz ||
			cfg.RoomURL != "any" || cfg.DeviceID != "client" || cfg.LocalAddr != "127.0.0.1:1080" ||
			cfg.DNSServer != defaultDNSServer || cfg.VP8FPS != 60 || cfg.VP8BatchSize != 8 ||
			cfg.Liveness.Interval != 2500*time.Millisecond ||
			cfg.Liveness.Timeout != 750*time.Millisecond ||
			cfg.Liveness.Failures != 4 {
			t.Fatalf(
				"RunWithReady args mismatch: link=%q transport=%q carrier=%q room=%q client=%q "+
					"local=%q dns=%q vp8=%d/%d liveness=%+v",
				cfg.Link, cfg.Transport, cfg.Carrier, cfg.RoomURL, cfg.DeviceID,
				cfg.LocalAddr, cfg.DNSServer, cfg.VP8FPS, cfg.VP8BatchSize, cfg.Liveness,
			)
		}
		onReady()
		<-ctx.Done()
		return ctx.Err()
	}

	if err := StartWithTransport(carrierJazz, "dc", "", "client", "key", 1080, "", ""); err != nil {
		t.Fatalf("StartWithTransport() error = %v", err)
	}
	if !IsRunning() {
		t.Fatal("IsRunning() = false, want true")
	}
	if err := WaitReady(100); err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	Stop()
	if IsRunning() {
		t.Fatal("IsRunning() = true after Stop")
	}
}

//nolint:cyclop // table-driven test naturally has many branches
func TestStartUsesDefaultsAndCheckWithInjectedRunner(t *testing.T) {
	resetMobileGlobals(t)
	t.Cleanup(func() {
		resetMobileGlobals(t)
	})

	runClientWithReady = func(ctx context.Context, cfg client.Config, onReady func()) error {
		if cfg.Transport != defaultTransport || cfg.RoomURL != "https://telemost.yandex.ru/j/room" ||
			cfg.LocalAddr != "127.0.0.1:1081" || cfg.SOCKSUser != "u" || cfg.SOCKSPass != "p" ||
			cfg.Liveness.Interval != control.DefaultInterval ||
			cfg.Liveness.Timeout != control.DefaultTimeout ||
			cfg.Liveness.Failures != control.DefaultFailures {
			t.Fatalf("Start args mismatch: transport=%q room=%q local=%q user/pass=%q/%q liveness=%+v",
				cfg.Transport, cfg.RoomURL, cfg.LocalAddr, cfg.SOCKSUser, cfg.SOCKSPass, cfg.Liveness)
		}
		onReady()
		<-ctx.Done()
		return ctx.Err()
	}

	if err := Start("telemost", "room", "client", "key", 1081, "u", "p"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := WaitReady(100); err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	Stop()

	SetLivenessOptions(3000, 1000, 5)
	runClientWithReady = func(ctx context.Context, cfg client.Config, onReady func()) error {
		if cfg.Transport != dataTransport || cfg.VP8FPS != 1 || cfg.VP8BatchSize != 64 ||
			cfg.Liveness.Interval != 3000*time.Millisecond ||
			cfg.Liveness.Timeout != time.Second ||
			cfg.Liveness.Failures != 5 {
			t.Fatalf("Check args mismatch: transport=%q vp8=%d/%d liveness=%+v",
				cfg.Transport, cfg.VP8FPS, cfg.VP8BatchSize, cfg.Liveness)
		}
		onReady()
		<-ctx.Done()
		return nil
	}
	elapsed, err := Check("jazz", "dc", "", "client", "key", 1082, 100, -1, 999)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if elapsed < 0 {
		t.Fatalf("Check() elapsed = %d", elapsed)
	}
}

func TestPingPassesLiveness(t *testing.T) {
	resetMobileGlobals(t)
	t.Cleanup(func() {
		resetMobileGlobals(t)
	})
	SetLivenessOptions(4000, 1500, 6)

	seen := make(chan control.Config, 1)
	runClientWithReady = func(ctx context.Context, cfg client.Config, onReady func()) error {
		seen <- cfg.Liveness
		onReady()
		<-ctx.Done()
		return nil
	}

	_, _ = Ping("jazz", "dc", "", "client", "key", 1085, 100, "http://127.0.0.1/", 30, 1)
	select {
	case got := <-seen:
		if got.Interval != 4000*time.Millisecond || got.Timeout != 1500*time.Millisecond || got.Failures != 6 {
			t.Fatalf("Ping liveness = %+v", got)
		}
	default:
		t.Fatal("Ping did not start client")
	}
}

func TestCheckTimeoutAndRunError(t *testing.T) {
	resetMobileGlobals(t)
	t.Cleanup(func() {
		resetMobileGlobals(t)
	})

	runClientWithReady = func(ctx context.Context, _ client.Config, _ func()) error {
		<-ctx.Done()
		return nil
	}
	if _, err := Check("telemost", defaultTransport, "room", "client", "key", 1083, 1, 30, 1); !errors.Is(err, errStartTimedOut) { //nolint:lll // long test description
		t.Fatalf("Check(timeout) error = %v, want %v", err, errStartTimedOut)
	}

	want := errMobileCheckFailed
	runClientWithReady = func(context.Context, client.Config, func()) error {
		return want
	}
	if _, err := Check("telemost", defaultTransport, "room", "client", "key", 1084, 100, 30, 1); !errors.Is(err, want) {
		t.Fatalf("Check(run error) = %v, want %v", err, want)
	}
}

func TestWaitReadyStatesAndStop(t *testing.T) {
	resetMobileGlobals(t)

	if err := WaitReady(1); !errors.Is(err, errNotRunning) {
		t.Fatalf("WaitReady(not running) = %v", err)
	}

	mu.Lock()
	errRun = errMobileRunFailed
	mu.Unlock()
	if err := WaitReady(1); err == nil || err.Error() != "run failed" {
		t.Fatalf("WaitReady(run err) = %v", err)
	}

	mu.Lock()
	errRun = nil
	ready = make(chan struct{})
	done = make(chan struct{})
	cancel = func() {}
	mu.Unlock()
	if err := WaitReady(1); !errors.Is(err, errStartTimedOut) {
		t.Fatalf("WaitReady(timeout) = %v", err)
	}

	mu.Lock()
	close(ready)
	mu.Unlock()
	if err := WaitReady(1); err != nil {
		t.Fatalf("WaitReady(ready) error = %v", err)
	}

	mu.Lock()
	cancel = func() {}
	done = make(chan struct{})
	doneCh := done
	mu.Unlock()
	go func() {
		time.Sleep(time.Millisecond)
		close(doneCh)
	}()
	Stop()
	mu.Lock()
	cancel = nil
	mu.Unlock()
}

func TestLogBridge(t *testing.T) {
	w := &testLogWriter{}
	n, err := (&logBridge{w: w}).Write([]byte("abc"))
	if err != nil || n != 3 || w.got != "abc" {
		t.Fatalf("logBridge.Write() = (%d, %v), got %q", n, err, w.got)
	}
}
