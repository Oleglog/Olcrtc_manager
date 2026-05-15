// Package livekit implements an engine.Session backed by the LiveKit SFU
// protocol via the upstream livekit/server-sdk-go client.
//
// This engine is service-agnostic: it accepts a wss:// signaling URL and an
// access token, and provides byte-stream + video-track primitives over a
// LiveKit room. Service-specific token acquisition (e.g. WB Stream, Jazz,
// or a self-hosted LiveKit deployment) lives in the auth package.
package livekit

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	protoLogger "github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/openlibrecommunity/olcrtc/internal/engine"
	"github.com/pion/webrtc/v4"
)

const (
	defaultSendQueueSize = 5000
	dataPublishTopic     = "olcrtc"
	videoTrackName       = "videochannel"
)

var (
	// ErrSessionClosed is returned when an operation is attempted on a closed session.
	ErrSessionClosed = errors.New("livekit session closed")
	// ErrSendQueueFull is returned when the outbound queue cannot accept more data.
	ErrSendQueueFull = errors.New("livekit send queue full")
	// ErrRoomNotConnected is returned when the underlying room is not connected yet.
	ErrRoomNotConnected = errors.New("livekit room not connected")
	// ErrURLRequired is returned when no signaling URL was supplied.
	ErrURLRequired = errors.New("livekit signaling URL required")
	// ErrTokenRequired is returned when no access token was supplied.
	ErrTokenRequired = errors.New("livekit access token required")
)

// Session is the LiveKit engine handle.
type Session struct {
	url             string
	token           string
	name            string
	room            *lksdk.Room
	onData          func([]byte)
	onReconnect     func(*webrtc.DataChannel)
	shouldReconnect func() bool
	onEnded         func(string)
	sendQueue       chan []byte
	closed          atomic.Bool
	done            chan struct{}
	cancel          context.CancelFunc
	videoTrackMu    sync.RWMutex
	videoTracks     []webrtc.TrackLocal
	onVideoTrack    func(*webrtc.TrackRemote, *webrtc.RTPReceiver)
	wg              sync.WaitGroup
}

// New creates a new LiveKit engine session.
func New(ctx context.Context, cfg engine.Config) (engine.Session, error) {
	if cfg.URL == "" {
		return nil, ErrURLRequired
	}
	if cfg.Token == "" {
		return nil, ErrTokenRequired
	}
	_, cancel := context.WithCancel(ctx)
	return &Session{
		url:       cfg.URL,
		token:     cfg.Token,
		name:      cfg.Name,
		onData:    cfg.OnData,
		sendQueue: make(chan []byte, defaultSendQueueSize),
		done:      make(chan struct{}),
		cancel:    cancel,
	}, nil
}

// Capabilities reports what this engine can do.
func (s *Session) Capabilities() engine.Capabilities {
	return engine.Capabilities{ByteStream: true, VideoTrack: true}
}

// Connect joins the LiveKit room.
func (s *Session) Connect(_ context.Context) error {
	roomCB := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnDataReceived: func(data []byte, _ lksdk.DataReceiveParams) {
				if s.onData != nil {
					s.onData(data)
				}
			},
			OnTrackSubscribed: func(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, _ *lksdk.RemoteParticipant) {
				if track.Kind() != webrtc.RTPCodecTypeVideo {
					return
				}
				s.videoTrackMu.RLock()
				cb := s.onVideoTrack
				s.videoTrackMu.RUnlock()
				if cb != nil {
					cb(track, nil)
				}
			},
		},
		OnDisconnected: func() {
			if !s.closed.Load() && s.onEnded != nil {
				s.onEnded("disconnected from livekit")
			}
		},
	}

	room, err := lksdk.ConnectToRoomWithToken(
		s.url,
		s.token,
		roomCB,
		lksdk.WithAutoSubscribe(true),
		lksdk.WithLogger(protoLogger.GetDiscardLogger()),
	)
	if err != nil {
		return fmt.Errorf("connect to room: %w", err)
	}

	s.room = room
	if err := s.publishPendingTracks(); err != nil {
		return err
	}
	s.wg.Add(1)
	go s.processSendQueue()
	return nil
}

func (s *Session) publishPendingTracks() error {
	s.videoTrackMu.RLock()
	defer s.videoTrackMu.RUnlock()
	for _, track := range s.videoTracks {
		if _, err := s.room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
			Name: videoTrackName,
		}); err != nil {
			return fmt.Errorf("failed to publish track: %w", err)
		}
	}
	return nil
}

func (s *Session) processSendQueue() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case data, ok := <-s.sendQueue:
			if !ok {
				return
			}
			if err := s.room.LocalParticipant.PublishDataPacket(
				lksdk.UserData(data),
				lksdk.WithDataPublishTopic(dataPublishTopic),
				lksdk.WithDataPublishReliable(true),
			); err != nil {
				log.Printf("livekit publish data error: %v", err)
			}
		}
	}
}

// Send queues data for transmission.
func (s *Session) Send(data []byte) error {
	if s.closed.Load() {
		return ErrSessionClosed
	}
	select {
	case s.sendQueue <- data:
		return nil
	default:
		return ErrSendQueueFull
	}
}

// Close terminates the session.
func (s *Session) Close() error {
	if s.closed.CompareAndSwap(false, true) {
		s.cancel()
		close(s.done)
		if s.room != nil {
			s.unpublishLocalTracks()
			s.room.Disconnect()
			// LiveKit's Disconnect() returns once the local SDK state
			// is torn down, not when the server has actually evicted
			// the participant. Without giving the signalling channel
			// time to flush the LEAVE_REQUEST and the server to act on
			// it, a back-to-back reconnect from the same identity in
			// the same room sees a still-alive ghost participant on
			// the SFU and inherits stale publication state.
			time.Sleep(2 * time.Second)
		}
		close(s.sendQueue)
		s.wg.Wait()
	}
	return nil
}

func (s *Session) unpublishLocalTracks() {
	if s.room == nil || s.room.LocalParticipant == nil {
		return
	}
	for _, publication := range s.room.LocalParticipant.TrackPublications() {
		if publication.SID() == "" {
			continue
		}
		if err := s.room.LocalParticipant.UnpublishTrack(publication.SID()); err != nil {
			log.Printf("livekit unpublish track error: %v", err)
		}
	}
}

// SetReconnectCallback stores the reconnect callback (LiveKit reconnects internally; this is kept for API parity).
func (s *Session) SetReconnectCallback(cb func(*webrtc.DataChannel)) { s.onReconnect = cb }

// SetShouldReconnect stores the reconnect predicate (kept for API parity).
func (s *Session) SetShouldReconnect(fn func() bool) { s.shouldReconnect = fn }

// SetEndedCallback registers a function to call when the session ends.
func (s *Session) SetEndedCallback(cb func(string)) { s.onEnded = cb }

// WatchConnection is a no-op; LiveKit handles connection supervision itself.
func (s *Session) WatchConnection(_ context.Context) {}

// CanSend reports whether the session is ready to accept data.
func (s *Session) CanSend() bool { return !s.closed.Load() && s.room != nil }

// GetSendQueue exposes the outbound queue.
func (s *Session) GetSendQueue() chan []byte { return s.sendQueue }

// GetBufferedAmount is a stub for LiveKit (the SDK handles its own buffering).
func (s *Session) GetBufferedAmount() uint64 { return 0 }

// AddVideoTrack publishes a video track to the room.
func (s *Session) AddVideoTrack(track webrtc.TrackLocal) error {
	s.videoTrackMu.Lock()
	s.videoTracks = append(s.videoTracks, track)
	s.videoTrackMu.Unlock()

	if s.room == nil || s.room.LocalParticipant == nil {
		return nil
	}
	if _, err := s.room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
		Name: videoTrackName,
	}); err != nil {
		return fmt.Errorf("failed to publish track: %w", err)
	}
	return nil
}

// SetVideoTrackHandler registers a callback for remote video tracks.
func (s *Session) SetVideoTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	s.videoTrackMu.Lock()
	defer s.videoTrackMu.Unlock()
	s.onVideoTrack = cb
}

func init() { //nolint:gochecknoinits // engine registration is the canonical Go pattern for plugins
	engine.Register("livekit", New)
}
