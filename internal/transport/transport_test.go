package transport

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type stubTransport struct{}

func (s *stubTransport) Connect(context.Context) error   { return nil }
func (s *stubTransport) Send([]byte) error               { return nil }
func (s *stubTransport) Close() error                    { return nil }
func (s *stubTransport) SetReconnectCallback(func())     {}
func (s *stubTransport) SetShouldReconnect(func() bool)  {}
func (s *stubTransport) SetEndedCallback(func(string))   {}
func (s *stubTransport) WatchConnection(context.Context) {}
func (s *stubTransport) CanSend() bool                   { return true }
func (s *stubTransport) Reconnect(string)                {}
func (s *stubTransport) Features() Features              { return Features{Reliable: true} }

func snapshotTransportRegistry() map[string]Factory {
	out := make(map[string]Factory, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

func restoreTransportRegistry(src map[string]Factory) {
	registry = make(map[string]Factory, len(src))
	for k, v := range src {
		registry[k] = v
	}
}

func TestNewAndAvailable(t *testing.T) {
	old := snapshotTransportRegistry()
	t.Cleanup(func() { restoreTransportRegistry(old) })

	called := false
	Register("test-transport", func(_ context.Context, cfg Config) (Transport, error) {
		called = cfg.DeviceID == "client-1"
		return &stubTransport{}, nil
	})

	got, err := New(context.Background(), "test-transport", Config{DeviceID: "client-1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !called {
		t.Fatal("factory did not receive config")
	}
	if _, ok := got.(*stubTransport); !ok {
		t.Fatalf("New() returned %T, want *stubTransport", got)
	}

	if !reflect.DeepEqual(Available(), []string{"test-transport"}) {
		t.Fatalf("Available() = %#v, want %#v", Available(), []string{"test-transport"})
	}
}

func TestNewReturnsErrTransportNotFound(t *testing.T) {
	old := snapshotTransportRegistry()
	t.Cleanup(func() { restoreTransportRegistry(old) })
	registry = map[string]Factory{}

	_, err := New(context.Background(), "missing", Config{})
	if !errors.Is(err, ErrTransportNotFound) {
		t.Fatalf("New() error = %v, want %v", err, ErrTransportNotFound)
	}
}
