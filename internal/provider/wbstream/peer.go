// Package wbstream implements the WB Stream WebRTC provider.
package wbstream

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	lksdk "github.com/livekit/server-sdk-go/v2"
	"github.com/pion/webrtc/v4"
)

const (
	wsURL = "wss://wbstream01-el.wb.ru:7880"
)

var (
	// ErrPeerClosed is returned when an operation is attempted on a closed peer.
	ErrPeerClosed = errors.New("peer closed")
	// ErrSendQueueFull is returned when the transmission queue is full.
	ErrSendQueueFull = errors.New("send queue full")
	// ErrLiveKitNotConnected is returned when the LiveKit room is not connected.
	ErrLiveKitNotConnected = errors.New("livekit room not connected")
)

// Peer represents a WB Stream WebRTC connection using LiveKit.
type Peer struct {
	roomURL         string
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

// NewPeer creates a new WB Stream provider peer.
func NewPeer(ctx context.Context, roomURL, name string, onData func([]byte)) (*Peer, error) {
	_, cancel := context.WithCancel(ctx)
	return &Peer{
		roomURL:   roomURL,
		name:      name,
		onData:    onData,
		sendQueue: make(chan []byte, 5000),
		done:      make(chan struct{}),
		cancel:    cancel,
	}, nil
}

// Connect starts the WebRTC connection process.
func (p *Peer) Connect(ctx context.Context) error {
	token, err := p.getRoomToken(ctx)
	if err != nil {
		return fmt.Errorf("get room token: %w", err)
	}

	roomCB := &lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnDataReceived: func(data []byte, _ lksdk.DataReceiveParams) {
				if p.onData != nil {
					p.onData(data)
				}
			},
			OnTrackSubscribed: func(track *webrtc.TrackRemote, _ *lksdk.RemoteTrackPublication, _ *lksdk.RemoteParticipant) {
				if track.Kind() != webrtc.RTPCodecTypeVideo {
					return
				}

				p.videoTrackMu.RLock()
				cb := p.onVideoTrack
				p.videoTrackMu.RUnlock()
				if cb != nil {
					cb(track, nil)
				}
			},
		},
		OnDisconnected: func() {
			if p.onEnded != nil {
				p.onEnded("disconnected from livekit")
			}
		},
	}

	room, err := lksdk.ConnectToRoomWithToken(wsURL, token, roomCB, lksdk.WithAutoSubscribe(true))
	if err != nil {
		return fmt.Errorf("connect to room: %w", err)
	}

	p.room = room
	if err := p.publishPendingTracks(); err != nil {
		return err
	}
	p.wg.Add(1)
	go p.processSendQueue()

	return nil
}

func (p *Peer) publishPendingTracks() error {
	p.videoTrackMu.RLock()
	defer p.videoTrackMu.RUnlock()

	for _, track := range p.videoTracks {
		if _, err := p.room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
			Name: "videochannel",
		}); err != nil {
			return fmt.Errorf("failed to publish track: %w", err)
		}
	}

	return nil
}

func (p *Peer) getRoomToken(ctx context.Context) (string, error) {
	accessToken, err := registerGuest(ctx, p.name)
	if err != nil {
		return "", fmt.Errorf("register guest: %w", err)
	}

	roomID := p.roomURL
	if roomID == "" || roomID == "any" {
		roomID, err = createRoom(ctx, accessToken)
		if err != nil {
			return "", fmt.Errorf("create room: %w", err)
		}
		log.Printf("WB Stream room created: %s", roomID)
		log.Printf("To connect client use: -id %s", roomID)
	}

	if err := joinRoom(ctx, accessToken, roomID); err != nil {
		return "", fmt.Errorf("join room: %w", err)
	}

	token, err := getToken(ctx, accessToken, roomID, p.name)
	if err != nil {
		return "", fmt.Errorf("get token: %w", err)
	}

	return token, nil
}

func (p *Peer) processSendQueue() {
	defer p.wg.Done()
	for {
		select {
		case <-p.done:
			return
		case data, ok := <-p.sendQueue:
			if !ok {
				return
			}
			if err := p.room.LocalParticipant.PublishDataPacket(
				lksdk.UserData(data),
				lksdk.WithDataPublishTopic("olcrtc"),
				lksdk.WithDataPublishReliable(true),
			); err != nil {
				log.Printf("WB Stream publish data error: %v", err)
			}
		}
	}
}

// Send transmits data to the room.
func (p *Peer) Send(data []byte) error {
	if p.closed.Load() {
		return ErrPeerClosed
	}
	select {
	case p.sendQueue <- data:
		return nil
	default:
		return ErrSendQueueFull
	}
}

// Close terminates the provider connection.
func (p *Peer) Close() error {
	if p.closed.CompareAndSwap(false, true) {
		p.cancel()
		close(p.done)
		if p.room != nil {
			p.room.Disconnect()
		}
		close(p.sendQueue)
		p.wg.Wait()
	}
	return nil
}

// SetReconnectCallback is a stub for WB Stream.
func (p *Peer) SetReconnectCallback(cb func(*webrtc.DataChannel)) {
	p.onReconnect = cb
}

// SetShouldReconnect is a stub for WB Stream.
func (p *Peer) SetShouldReconnect(fn func() bool) {
	p.shouldReconnect = fn
}

// SetEndedCallback sets the function to call when the session ends.
func (p *Peer) SetEndedCallback(cb func(string)) {
	p.onEnded = cb
}

// WatchConnection is a stub for WB Stream.
func (p *Peer) WatchConnection(_ context.Context) {}

// CanSend checks if the provider is ready to transmit data.
func (p *Peer) CanSend() bool {
	return !p.closed.Load() && p.room != nil
}

// GetSendQueue returns the data transmission queue.
func (p *Peer) GetSendQueue() chan []byte {
	return p.sendQueue
}

// GetBufferedAmount is a stub for WB Stream.
func (p *Peer) GetBufferedAmount() uint64 {
	return 0
}

// AddVideoTrack adds a video track to the LiveKit room.
func (p *Peer) AddVideoTrack(track webrtc.TrackLocal) error {
	p.videoTrackMu.Lock()
	p.videoTracks = append(p.videoTracks, track)
	p.videoTrackMu.Unlock()

	if p.room == nil || p.room.LocalParticipant == nil {
		return nil
	}

	if _, err := p.room.LocalParticipant.PublishTrack(track, &lksdk.TrackPublicationOptions{
		Name: "videochannel",
	}); err != nil {
		return fmt.Errorf("failed to publish track: %w", err)
	}

	return nil
}

// SetVideoTrackHandler registers a callback for remote video tracks.
func (p *Peer) SetVideoTrackHandler(cb func(*webrtc.TrackRemote, *webrtc.RTPReceiver)) {
	p.videoTrackMu.Lock()
	defer p.videoTrackMu.Unlock()
	p.onVideoTrack = cb
}
