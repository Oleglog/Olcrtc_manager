package e2e

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"runtime"
	"slices"
	"testing"
	"time"

	enginebuiltin "github.com/openlibrecommunity/olcrtc/internal/engine/builtin"
)

var (
	errStressNoRoundtrips   = errors.New("no successful roundtrips within duration")
	errStressPayloadMatch   = errors.New("payload mismatch")
	errStressNoBulkProgress = errors.New("bulk pump made zero progress")
)

var (
	realStress = flag.Bool( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.stress",
		false,
		"run real provider stress matrix (bulk transfer + sustained echo) — requires -olcrtc.real-e2e",
	)
	realStressBulkDuration = flag.Duration( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.stress-bulk-duration",
		60*time.Second,
		"per-case duration for the bulk pattern-pump phase (set 0 to skip). "+
			"Throughput differs by ~3 orders of magnitude across transports "+
			"(datachannel: MiB/s; videochannel: KB/s), so we measure how much "+
			"flows in a fixed time rather than fixing the byte budget.",
	)
	realStressDuration = flag.Duration( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.stress-duration",
		30*time.Second,
		"per-case duration for the sustained echo phase (set 0 to skip)",
	)
	realStressEchoSize = flag.Int( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.stress-echo-size",
		1024,
		"single-roundtrip payload size during the sustained echo phase",
	)
	realStressCaseTimeout = flag.Duration( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.stress-case-timeout",
		5*time.Minute,
		"hard timeout per stress carrier×transport case (covers connect + bulk + echo)",
	)
	realStressBulkChunkSize = flag.Int( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.stress-bulk-chunk",
		4096,
		"bulk request-response chunk size in bytes",
	)
)

// TestRealProviderTransportStress exercises every real carrier×transport
// combination under load. For each pair, two phases run sequentially over
// a single SOCKS connection:
//
//  1. Bulk phase: stream a deterministic byte pattern through the tunnel
//     for -olcrtc.stress-bulk-duration and verify it echoes back byte-for-
//     byte. Reports observed throughput. Different transports differ by
//     orders of magnitude (qr-encoded videochannel vs SCTP datachannel),
//     so we measure rather than assert a fixed budget.
//  2. Echo phase: send -olcrtc.stress-echo-size payloads as fast as the
//     loop will go for -olcrtc.stress-duration, recording per-RT latency
//     and computing p50/p95/p99.
//
// Around both phases we snapshot runtime.NumGoroutine to surface obvious
// goroutine leaks introduced by reconnect / bytestream / epoch regressions.
//
// Gated by -olcrtc.stress so it never runs on every push; intended for the
// nightly soak job in CI and for local stress profiling.
//
//nolint:cyclop // matrix of carrier×transport expectations is naturally branchy
func TestRealProviderTransportStress(t *testing.T) {
	if !*realE2E {
		t.Skip("real provider e2e disabled; pass -olcrtc.real-e2e to enable")
	}
	if !*realStress {
		t.Skip("stress disabled; pass -olcrtc.stress to enable")
	}

	carriers := splitTestList(*realE2ECarriers)
	transports := splitTestList(*realE2ETransports)
	if len(carriers) == 0 {
		t.Fatal("no real e2e carriers selected")
	}
	if len(transports) == 0 {
		t.Fatal("no real e2e transports selected")
	}

	echoAddr := startEchoServer(t)
	for _, carrierName := range carriers {
		t.Run(carrierName, func(t *testing.T) {
			roomCtx, cancelRoom := context.WithTimeout(context.Background(), *realStressCaseTimeout)
			defer cancelRoom()
			roomURL := requireRealRoom(roomCtx, t, carrierName)
			var authFailed bool
			for _, transportName := range transports {
				t.Run(transportName, func(t *testing.T) {
					if authFailed {
						t.Skip("skipping: carrier auth failed on previous transport")
					}
					expectation := realE2ECaseExpectation(carrierName, transportName)
					if expectation == realE2EExpectFail {
						t.Skip("skipping: combo not expected to pass even at baseline")
					}
					err := runRealE2EStressCase(t, carrierName, transportName, roomURL, echoAddr)
					if err != nil && errors.Is(err, enginebuiltin.ErrAuthFailed) {
						authFailed = true
						t.Skipf("skip %s stress: auth failed: %v", carrierName, err)
					}
					switch {
					case err == nil:
						t.Logf("STRESS OK %s/%s", carrierName, transportName)
					case expectation == realE2EExpectUnstable:
						logUnstableOutcome(t, "STRESS UNSTABLE", carrierName, transportName, err)
					default:
						t.Fatalf("STRESS FAIL %s/%s: %v", carrierName, transportName, err)
					}
				})
			}
		})
	}
}

//nolint:cyclop // two phases plus tunnel/connection setup naturally branch
func runRealE2EStressCase(t *testing.T, carrierName, transportName, roomURL, echoAddr string) (err error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), *realStressCaseTimeout)
	defer cancel()

	goroutinesBefore := runtime.NumGoroutine()

	rt, err := startRealTunnel(ctx, t, carrierName, transportName, roomURL, testClientDeviceID, testClientDeviceID)
	if err != nil {
		return err
	}
	defer func() {
		if stopErr := rt.stopErr(); err == nil && stopErr != nil {
			err = stopErr
		}
	}()

	conn, err := connectViaSOCKSWithin(rt.socksAddr, echoAddr, *realStressCaseTimeout)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	if d := *realStressBulkDuration; d > 0 {
		written, dur, err := streamPatternForDuration(conn, d, *realStressBulkChunkSize)
		if err != nil {
			return fmt.Errorf("bulk pump: %w", err)
		}
		throughput := float64(written) / dur.Seconds() / (1 << 20)
		t.Logf("bulk %s/%s: %d bytes in %s (%.3f MiB/s)",
			carrierName, transportName, written, dur, throughput)
		if written == 0 {
			return errStressNoBulkProgress
		}
	}

	if d := *realStressDuration; d > 0 {
		stats, err := sustainedEcho(conn, *realStressEchoSize, d)
		if err != nil {
			return fmt.Errorf("sustained echo: %w", err)
		}
		t.Logf("echo  %s/%s: %d rt in %s, p50=%s p95=%s p99=%s max=%s lost=%d",
			carrierName, transportName, stats.count, d,
			stats.p50, stats.p95, stats.p99, stats.maxLatency, stats.lost)
		if stats.count == 0 {
			return fmt.Errorf("%w: %s", errStressNoRoundtrips, d)
		}
	}

	goroutinesAfter := runtime.NumGoroutine()
	// Allow some slack — pion/quic spawn helpers that take time to wind down
	// after Close, but a real leak shows up as tens of extra goroutines.
	const goroutineLeakSlack = 30
	if goroutinesAfter > goroutinesBefore+goroutineLeakSlack {
		t.Logf("WARNING: goroutines grew %d -> %d during %s/%s",
			goroutinesBefore, goroutinesAfter, carrierName, transportName)
	}

	return nil
}

// streamPatternForDuration pumps a deterministic byte pattern through conn
// for at most `duration` using a synchronous request-response loop: write a
// chunk, wait until the same chunk echoes back and verify, then write the
// next one. Returns total bytes successfully echoed and elapsed time.
//
// Why request-response rather than concurrent write+read streams:
// transport throughputs differ by ~3 orders of magnitude (datachannel does
// MiB/s; videochannel/seichannel ~25 KB/s through 256-byte qr-encoded
// frames at 25 FPS). An asynchronous writer outruns a slow transport,
// fills muxconn / SOCKS / RTP-track buffers, and the deadlocked pipe
// eventually trips a TCP-write deadline — which is not a real bug, just
// the natural consequence of pumping into a slow pipe with no flow
// control. Request-response naturally rate-limits to the transport's
// actual round-trip throughput, which is what we want to measure.
func streamPatternForDuration(conn net.Conn, duration time.Duration, chunkSize int) (int64, time.Duration, error) {
	if chunkSize <= 0 {
		chunkSize = 4096
	}
	// Per-chunk roundtrip deadline. Slow transports (videochannel) can
	// take seconds+ per chunk in practice; 15s gives ample margin
	// without making genuine stalls hang forever.
	const chunkTimeout = 15 * time.Second

	start := time.Now()
	deadline := start.Add(duration)

	buf := make([]byte, chunkSize)
	echoed := make([]byte, chunkSize)
	want := make([]byte, chunkSize)

	reader := bufio.NewReader(conn)
	var total int64

	for time.Now().Before(deadline) {
		fillPattern(buf, total)
		if err := conn.SetWriteDeadline(time.Now().Add(chunkTimeout)); err != nil {
			return total, time.Since(start), fmt.Errorf("set write deadline at %d: %w", total, err)
		}
		if _, err := conn.Write(buf); err != nil {
			return total, time.Since(start), fmt.Errorf("write at %d: %w", total, err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(chunkTimeout)); err != nil {
			return total, time.Since(start), fmt.Errorf("set read deadline at %d: %w", total, err)
		}
		if _, err := io.ReadFull(reader, echoed); err != nil {
			return total, time.Since(start), fmt.Errorf("read at %d: %w", total, err)
		}
		fillPattern(want, total)
		if !bytes.Equal(echoed, want) {
			return total, time.Since(start), fmt.Errorf("%w %d", errPayloadMismatchOffset, total)
		}
		total += int64(chunkSize)
	}
	return total, time.Since(start), nil
}

type echoStats struct {
	count                int
	lost                 int
	p50, p95, p99        time.Duration
	maxLatency           time.Duration
}

// sustainedEcho writes payloads of size `payloadSize` and waits for them to
// echo back, recording per-roundtrip latency. Runs until duration elapses
// or the underlying connection fails. Each write/read uses a deadline so a
// stuck transport surfaces as a finite-time test failure rather than a hang.
//
//nolint:cyclop // per-rt deadlines + error wrapping naturally branch many ways
func sustainedEcho(conn net.Conn, payloadSize int, duration time.Duration) (echoStats, error) {
	if payloadSize < 4 {
		payloadSize = 4
	}
	deadline := time.Now().Add(duration)
	payload := make([]byte, payloadSize)
	for i := range payload {
		payload[i] = byte('a' + (i % 26))
	}
	// Mark the payload terminator so we can ReadFull a fixed length back.
	payload[payloadSize-1] = '\n'

	reader := bufio.NewReader(conn)
	var stats echoStats
	latencies := make([]time.Duration, 0, 1024)

	buf := make([]byte, payloadSize)
	for time.Now().Before(deadline) {
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return stats, fmt.Errorf("set write deadline: %w", err)
		}
		start := time.Now()
		if _, err := conn.Write(payload); err != nil {
			stats.lost++
			return stats, fmt.Errorf("write at rt #%d: %w", stats.count, err)
		}
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return stats, fmt.Errorf("set read deadline: %w", err)
		}
		if _, err := io.ReadFull(reader, buf); err != nil {
			stats.lost++
			return stats, fmt.Errorf("read at rt #%d: %w", stats.count, err)
		}
		lat := time.Since(start)
		if !bytes.Equal(buf, payload) {
			return stats, fmt.Errorf("%w at rt #%d", errStressPayloadMatch, stats.count)
		}
		latencies = append(latencies, lat)
		if lat > stats.maxLatency {
			stats.maxLatency = lat
		}
		stats.count++
	}

	if len(latencies) > 0 {
		slices.Sort(latencies)
		stats.p50 = latencies[len(latencies)*50/100]
		stats.p95 = latencies[min(len(latencies)*95/100, len(latencies)-1)]
		stats.p99 = latencies[min(len(latencies)*99/100, len(latencies)-1)]
	}
	return stats, nil
}
