package logger

import (
	"bytes"
	"log"
	"os"
	"strings"
	"testing"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
		SetVerbose(false)
	})
	return &buf
}

func TestVerboseFlag(t *testing.T) {
	SetVerbose(true)
	if !IsVerbose() {
		t.Fatal("IsVerbose() = false, want true")
	}
	SetVerbose(false)
	if IsVerbose() {
		t.Fatal("IsVerbose() = true, want false")
	}
}

func TestLoggingFunctions(t *testing.T) {
	buf := captureLogs(t)

	Info("info")
	Infof("%s", "infof")
	Warn("warn")
	Warnf("%s", "warnf")
	Error("error")
	Errorf("%s", "errorf")

	got := buf.String()
	for _, want := range []string{"info", "infof", "warn", "warnf", "error", "errorf"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output %q does not contain %q", got, want)
		}
	}
}

func TestVerboseAndDebugLogging(t *testing.T) {
	buf := captureLogs(t)

	Verbosef("%s", "hidden")
	Debugf("%s", "hidden-debug")
	if got := buf.String(); got != "" {
		t.Fatalf("unexpected log output when verbose disabled: %q", got)
	}

	SetVerbose(true)
	Verbosef("%s", "visible")
	Debugf("%s", "visible-debug")
	got := buf.String()
	for _, want := range []string{"visible", "visible-debug"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log output %q does not contain %q", got, want)
		}
	}
}

func TestPionLoggerDropsTURNRefreshNoise(t *testing.T) {
	buf := captureLogs(t)

	turnc := NewPionLoggerFactory().NewLogger("turnc")
	turnc.Errorf("Fail to refresh permissions: %s", "CreatePermission error response")

	ice := NewPionLoggerFactory().NewLogger("ice")
	ice.Errorf("Fail to refresh permissions: %s", "CreatePermission error response")
	ice.Warn("normal warning")

	got := buf.String()
	if strings.Contains(got, "turnc") || strings.Contains(got, "refresh permissions") {
		t.Fatalf("unexpected TURN refresh noise in log output: %q", got)
	}
	if !strings.Contains(got, "normal warning") {
		t.Fatalf("expected normal warning to pass through, got %q", got)
	}
}

func TestDisableNoisyPionLogsMergesTurncScope(t *testing.T) {
	t.Setenv("PION_LOG_DISABLE", "ice")
	t.Setenv("PION_LOG_ERROR", "turnc,ice")

	DisableNoisyPionLogs()

	got := os.Getenv("PION_LOG_DISABLE")
	if !strings.Contains(got, "ice") || !strings.Contains(got, "turnc") {
		t.Fatalf("PION_LOG_DISABLE = %q, want ice and turnc", got)
	}
	if got := os.Getenv("PION_LOG_ERROR"); got != "ice" {
		t.Fatalf("PION_LOG_ERROR = %q, want ice", got)
	}
}
