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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/client"
	"github.com/openlibrecommunity/olcrtc/internal/server"
)

// Local throughput soak: pump as much traffic as the selected transport
// can sustain, locally, for an arbitrary duration.
//
// The tunnel is built on the in-memory carrier (no real provider, no
// network), so this measures the upper bound of what the
// SOCKS+muxconn+transport stack can do on this machine. Useful to:
//
//   - leave running for hours and watch for goroutine / memory growth
//   - reproduce slow-leak corruption with the byte-pattern verifier
//   - get a feel for raw transport throughput before touching real WebRTC
//
// Quick start:
//
//	go test -count=1 -v ./internal/e2e \
//	    -run '^TestLocalThroughputSoak$' \
//	    -olcrtc.local-soak \
//	    -olcrtc.local-soak-duration=12h \
//	    -timeout=13h
//
// The test is gated by -olcrtc.local-soak so it never runs in regular CI.

var (
	localSoakEnabled = flag.Bool( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.local-soak",
		false,
		"run TestLocalThroughputSoak (long-running local throughput pump)",
	)
	localSoakDuration = flag.Duration( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.local-soak-duration",
		30*time.Second,
		"how long to keep pumping traffic (e.g. 12h, 30m, 90s)",
	)
	localSoakTransport = flag.String( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.local-soak-transport",
		transportData,
		"transport to pump through: datachannel|videochannel|seichannel|vp8channel",
	)
	localSoakChunk = flag.Int( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.local-soak-chunk",
		64*1024,
		"write/read chunk size in bytes",
	)
	localSoakProgress = flag.Duration( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.local-soak-progress",
		30*time.Second,
		"how often to log throughput progress lines",
	)
	localSoakVerify = flag.Bool( //nolint:gochecknoglobals // package-level state intentional
		"olcrtc.local-soak-verify",
		true,
		"verify echoed bytes match the sent pattern (slower, but catches corruption)",
	)
)

var errLocalSoakPayloadMismatch = errors.New("local soak payload mismatch")

// TestLocalThroughputSoak pumps a deterministic byte pattern through a
// locally-built tunnel for -olcrtc.local-soak-duration and reports
// throughput periodically. Both writer and reader run concurrently on the
// same SOCKS connection; with the loopback echo server on the far end
// each byte gets written, tunneled across, echoed back, and verified.
func TestLocalThroughputSoak(t *testing.T) {
	if !*localSoakEnabled {
		t.Skip("local soak disabled; pass -olcrtc.local-soak to enable")
	}
	if *localSoakDuration <= 0 {
		t.Skip("local soak duration is zero")
	}
	if *localSoakChunk <= 0 {
		t.Fatalf("invalid -olcrtc.local-soak-chunk=%d", *localSoakChunk)
	}

	// Connection setup itself can be slow (first WebRTC handshake on
	// some transports), so don't fold it into the duration budget.
	const setupBudget = 30 * time.Second

	t.Logf("[soak] transport=%s duration=%s chunk=%d verify=%t progress=%s",
		*localSoakTransport, *localSoakDuration, *localSoakChunk,
		*localSoakVerify, *localSoakProgress)

	rt := startLocalSoakTunnel(t, *localSoakTransport)
	echoAddr := startEchoServer(t)

	conn, err := connectViaSOCKSWithin(rt.socksAddr, echoAddr, setupBudget)
	if err != nil {
		t.Fatalf("connect via SOCKS: %v", err)
	}
	defer func() { _ = conn.Close() }()

	pumpCtx, cancelPump := context.WithTimeout(context.Background(), *localSoakDuration)
	defer cancelPump()

	stats := runLocalSoakPump(pumpCtx, t, conn, *localSoakChunk, *localSoakVerify, *localSoakProgress)

	if stats.sent == 0 || stats.recv == 0 {
		t.Fatalf("no traffic moved: sent=%d recv=%d", stats.sent, stats.recv)
	}
	if stats.err != nil && !isExpectedShutdownErr(stats.err) {
		t.Fatalf("pump error: %v", stats.err)
	}

	t.Logf("[soak] DONE transport=%s elapsed=%s sent=%s recv=%s send=%s/s recv=%s/s",
		*localSoakTransport,
		stats.elapsed.Round(time.Second),
		humanBytes(stats.sent),
		humanBytes(stats.recv),
		humanBytes(int64(float64(stats.sent)/stats.elapsed.Seconds())),
		humanBytes(int64(float64(stats.recv)/stats.elapsed.Seconds())),
	)
}

// startLocalSoakTunnel mirrors startTunnel but lets the caller pick the
// transport (the original is hard-coded to datachannel).
func startLocalSoakTunnel(t *testing.T, transportName string) *tunnelRuntime {
	t.Helper()

	carrierName, room := registerMemoryCarrier(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	socksAddr := freeLocalAddr(ctx, t)
	options := e2eTransportOptions(transportName)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Run(ctx, server.Config{
			Transport:        transportName,
			TransportOptions: options,
			Carrier:          carrierName,
			RoomURL:          testRoom,
			KeyHex:           testKeyHex,
			DNSServer:        localDNSServer,
		})
	}()
	room.waitConnected(t, 1)

	ready := make(chan struct{})
	clientErr := make(chan error, 1)
	go func() {
		clientErr <- client.RunWithReady(ctx, client.Config{
			Transport:        transportName,
			TransportOptions: options,
			Carrier:          carrierName,
			RoomURL:          testRoom,
			KeyHex:           testKeyHex,
			DeviceID:         testClientDeviceID,
			LocalAddr:        socksAddr,
			DNSServer:        localDNSServer,
		}, func() { close(ready) })
	}()
	waitForReady(t, ready)

	return &tunnelRuntime{
		socksAddr: socksAddr,
		room:      room,
		cancel:    cancel,
		serverErr: serverErr,
		clientErr: clientErr,
		stopWait:  3 * time.Second,
	}
}

type localSoakStats struct {
	sent, recv int64
	elapsed    time.Duration
	err        error
}

// runLocalSoakPump runs a writer goroutine and a reader goroutine over the
// same conn until ctx expires, periodically logging progress. Bytes are
// counted atomically so the progress logger sees a coherent snapshot.
func runLocalSoakPump(
	ctx context.Context,
	t *testing.T,
	conn net.Conn,
	chunkSize int,
	verify bool,
	progressEvery time.Duration,
) localSoakStats {
	t.Helper()

	var sent, recv atomic.Int64
	start := time.Now()

	progressDone := make(chan struct{})
	go runLocalSoakProgress(ctx, t, &sent, &recv, start, progressEvery, progressDone)

	var (
		wg      sync.WaitGroup
		errOnce sync.Once
		pumpErr error
	)
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errOnce.Do(func() { pumpErr = err })
	}

	wg.Add(2)
	go pumpWriter(ctx, conn, chunkSize, &sent, &wg, recordErr)
	go pumpReader(ctx, conn, chunkSize, verify, &recv, &wg, recordErr)

	<-ctx.Done()
	// Force-close the conn so both pumps unblock from any in-flight I/O.
	// SetDeadline-in-the-past is the canonical kick.
	_ = conn.SetDeadline(time.Unix(1, 0))
	wg.Wait()
	<-progressDone

	return localSoakStats{
		sent:    sent.Load(),
		recv:    recv.Load(),
		elapsed: time.Since(start),
		err:     pumpErr,
	}
}

// runLocalSoakProgress emits periodic throughput lines until ctx fires.
func runLocalSoakProgress(
	ctx context.Context,
	t *testing.T,
	sent, recv *atomic.Int64,
	start time.Time,
	progressEvery time.Duration,
	done chan<- struct{},
) {
	t.Helper()
	defer close(done)
	if progressEvery <= 0 {
		return
	}
	ticker := time.NewTicker(progressEvery)
	defer ticker.Stop()
	var lastSent, lastRecv int64
	lastTime := start
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			s, r := sent.Load(), recv.Load()
			dt := now.Sub(lastTime).Seconds()
			instSendRate := int64(float64(s-lastSent) / dt)
			instRecvRate := int64(float64(r-lastRecv) / dt)
			t.Logf("[soak] elapsed=%s sent=%s recv=%s tx=%s/s rx=%s/s",
				now.Sub(start).Round(time.Second),
				humanBytes(s), humanBytes(r),
				humanBytes(instSendRate), humanBytes(instRecvRate),
			)
			lastSent, lastRecv = s, r
			lastTime = now
		}
	}
}

// pumpWriter pushes a deterministic byte pattern through conn until ctx
// expires or the connection errors out.
func pumpWriter(
	ctx context.Context,
	conn net.Conn,
	chunkSize int,
	sent *atomic.Int64,
	wg *sync.WaitGroup,
	recordErr func(error),
) {
	defer wg.Done()
	buf := make([]byte, chunkSize)
	var off int64
	for ctx.Err() == nil {
		fillPattern(buf, off)
		if _, err := conn.Write(buf); err != nil {
			if ctx.Err() == nil {
				recordErr(fmt.Errorf("write at %d: %w", off, err))
			}
			return
		}
		off += int64(chunkSize)
		sent.Add(int64(chunkSize))
	}
}

// pumpReader reads echoed bytes back, optionally verifying them against
// the deterministic pattern that pumpWriter produced at the same offset.
func pumpReader(
	ctx context.Context,
	conn net.Conn,
	chunkSize int,
	verify bool,
	recv *atomic.Int64,
	wg *sync.WaitGroup,
	recordErr func(error),
) {
	defer wg.Done()
	rdr := bufio.NewReader(conn)
	echoed := make([]byte, chunkSize)
	want := make([]byte, chunkSize)
	var off int64
	for ctx.Err() == nil {
		if _, err := io.ReadFull(rdr, echoed); err != nil {
			if ctx.Err() == nil {
				recordErr(fmt.Errorf("read at %d: %w", off, err))
			}
			return
		}
		if verify {
			fillPattern(want, off)
			if !bytes.Equal(echoed, want) {
				recordErr(fmt.Errorf("%w at offset %d", errLocalSoakPayloadMismatch, off))
				return
			}
		}
		off += int64(chunkSize)
		recv.Add(int64(chunkSize))
	}
}

// isExpectedShutdownErr filters errors that just mean "we asked the conn
// to stop" — deadline expirations from our SetDeadline kick, EOF from the
// peer half-closing, etc.
func isExpectedShutdownErr(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var nerr net.Error
	if errors.As(err, &nerr) && nerr.Timeout() {
		return true
	}
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	return false
}

// humanBytes formats a byte count with a binary-unit suffix.
func humanBytes(n int64) string {
	const (
		kib = 1 << 10
		mib = 1 << 20
		gib = 1 << 30
		tib = 1 << 40
	)
	switch {
	case n >= tib:
		return fmt.Sprintf("%.2f TiB", float64(n)/float64(tib))
	case n >= gib:
		return fmt.Sprintf("%.2f GiB", float64(n)/float64(gib))
	case n >= mib:
		return fmt.Sprintf("%.2f MiB", float64(n)/float64(mib))
	case n >= kib:
		return fmt.Sprintf("%.2f KiB", float64(n)/float64(kib))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
