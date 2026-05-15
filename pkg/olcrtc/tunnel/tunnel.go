// Package tunnel exposes olcrtc's server-side tunnel as an embeddable Go library.
//
// A [Server] accepts encrypted tunnel connections over a WebRTC SFU carrier
// and proxies their traffic to arbitrary TCP targets. Consumers plug in
// authorization and observability via the [Config] hooks:
//
//	srv := tunnel.New(tunnel.Config{
//	    Link:      "direct",
//	    Transport: "datachannel",
//	    Carrier:   "jitsi",
//	    RoomURL:   "https://meet.cryptopro.ru/myroom",
//	    KeyHex:    "<64-char hex>",
//	    DNSServer: "1.1.1.1:53",
//	    AuthHook: func(deviceID string, claims map[string]any) (string, error) {
//	        // reject unknown devices, enrich session with a DB-issued ID
//	        return db.IssueSession(deviceID, claims)
//	    },
//	    OnSessionOpen: func(sid, dev string, claims map[string]any) {
//	        log.Printf("session %s opened (device=%s)", sid, dev)
//	    },
//	    OnSessionClose: func(sid, reason string) {
//	        log.Printf("session %s closed (%s)", sid, reason)
//	    },
//	    OnTraffic: func(sid, addr string, in, out uint64) {
//	        metrics.Record(sid, addr, in, out)
//	    },
//	})
//	if err := srv.Run(ctx); err != nil {
//	    log.Fatal(err)
//	}
//
// Call [RegisterDefaults] once at program start to register the built-in
// carriers (jitsi, telemost, jazz, wbstream) and transports (datachannel,
// videochannel, seichannel, vp8channel).
package tunnel

import (
	"context"
	"fmt"

	"github.com/openlibrecommunity/olcrtc/internal/app/session"
	"github.com/openlibrecommunity/olcrtc/internal/handshake"
	"github.com/openlibrecommunity/olcrtc/internal/server"
)

// AuthFunc is invoked after CLIENT_HELLO to authorize the client and issue a
// session ID. Returning a non-nil error rejects the handshake; the error's
// message is forwarded to the client as the reject reason, so it should not
// leak sensitive details.
type AuthFunc = handshake.AuthFunc

// SessionOpenFunc fires right after a successful handshake, before the server
// starts accepting tunnel streams on that session.
type SessionOpenFunc = server.SessionOpenFunc

// SessionCloseFunc fires when a session ends. Reasons include "reconnect"
// (carrier dropped and was reestablished) and "closed" (graceful shutdown or
// ctx cancel).
type SessionCloseFunc = server.SessionCloseFunc

// TrafficFunc fires once per tunnel stream after both copy loops finish.
// bytesIn counts client→target bytes; bytesOut counts target→client bytes.
type TrafficFunc = server.TrafficFunc

// Config holds runtime server configuration.
type Config struct {
	// --- carrier selection ---
	Link      string // currently only "direct"
	Transport string // datachannel, videochannel, seichannel, vp8channel
	Carrier   string // jitsi, telemost, jazz, wbstream, none
	RoomURL   string // conference room identifier for the carrier

	// --- direct engine mode (Carrier == "none") ---
	Engine string // livekit, goolom, salutejazz, jitsi
	URL    string
	Token  string

	// --- crypto & networking ---
	KeyHex         string // 64-char hex (32 bytes) shared with the client
	DNSServer      string // resolver used for target dials, e.g. "1.1.1.1:53"
	SOCKSProxyAddr string // optional outbound SOCKS5 proxy host
	SOCKSProxyPort int    // optional outbound SOCKS5 proxy port

	// --- transport tuning ---
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

	// --- hooks ---
	// AuthHook authorizes the client. If nil, every client is admitted with a
	// random UUID as session ID.
	AuthHook AuthFunc
	// OnSessionOpen fires after a successful handshake. Nil is a no-op.
	OnSessionOpen SessionOpenFunc
	// OnSessionClose fires when the session is torn down. Nil is a no-op.
	OnSessionClose SessionCloseFunc
	// OnTraffic fires once per tunnel stream after both copy loops finish.
	// Nil is a no-op.
	OnTraffic TrafficFunc
}

// Server is an embeddable tunnel server.
type Server struct {
	cfg Config
}

// New returns a Server configured by cfg. Call [Server.Run] to start it.
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// Run starts the server and blocks until ctx is cancelled or the carrier ends.
func (s *Server) Run(ctx context.Context) error {
	if err := server.Run(ctx, server.Config{
		Link:            s.cfg.Link,
		Transport:       s.cfg.Transport,
		Carrier:         s.cfg.Carrier,
		RoomURL:         s.cfg.RoomURL,
		Engine:          s.cfg.Engine,
		URL:             s.cfg.URL,
		Token:           s.cfg.Token,
		KeyHex:          s.cfg.KeyHex,
		DNSServer:       s.cfg.DNSServer,
		SOCKSProxyAddr:  s.cfg.SOCKSProxyAddr,
		SOCKSProxyPort:  s.cfg.SOCKSProxyPort,
		VideoWidth:      s.cfg.VideoWidth,
		VideoHeight:     s.cfg.VideoHeight,
		VideoFPS:        s.cfg.VideoFPS,
		VideoBitrate:    s.cfg.VideoBitrate,
		VideoHW:         s.cfg.VideoHW,
		VideoQRSize:     s.cfg.VideoQRSize,
		VideoQRRecovery: s.cfg.VideoQRRecovery,
		VideoCodec:      s.cfg.VideoCodec,
		VideoTileModule: s.cfg.VideoTileModule,
		VideoTileRS:     s.cfg.VideoTileRS,
		VP8FPS:          s.cfg.VP8FPS,
		VP8BatchSize:    s.cfg.VP8BatchSize,
		SEIFPS:          s.cfg.SEIFPS,
		SEIBatchSize:    s.cfg.SEIBatchSize,
		SEIFragmentSize: s.cfg.SEIFragmentSize,
		SEIAckTimeoutMS: s.cfg.SEIAckTimeoutMS,
		AuthHook:        s.cfg.AuthHook,
		OnSessionOpen:   s.cfg.OnSessionOpen,
		OnSessionClose:  s.cfg.OnSessionClose,
		OnTraffic:       s.cfg.OnTraffic,
	}); err != nil {
		return fmt.Errorf("tunnel: %w", err)
	}
	return nil
}

// RegisterDefaults registers the built-in carriers, links and transports.
// Safe to call multiple times.
func RegisterDefaults() {
	session.RegisterDefaults()
}
