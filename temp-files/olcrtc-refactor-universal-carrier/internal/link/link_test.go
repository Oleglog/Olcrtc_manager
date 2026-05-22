package link

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type stubLink struct{}

func (s *stubLink) Connect(context.Context) error   { return nil }
func (s *stubLink) Send([]byte) error               { return nil }
func (s *stubLink) Close() error                    { return nil }
func (s *stubLink) SetReconnectCallback(func())     {}
func (s *stubLink) SetShouldReconnect(func() bool)  {}
func (s *stubLink) SetEndedCallback(func(string))   {}
func (s *stubLink) WatchConnection(context.Context) {}
func (s *stubLink) CanSend() bool                   { return true }

func snapshotLinkRegistry() map[string]Factory {
	out := make(map[string]Factory, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

func restoreLinkRegistry(src map[string]Factory) {
	registry = make(map[string]Factory, len(src))
	for k, v := range src {
		registry[k] = v
	}
}

func TestNewAndAvailable(t *testing.T) {
	old := snapshotLinkRegistry()
	t.Cleanup(func() { restoreLinkRegistry(old) })

	called := false
	Register("test-link", func(_ context.Context, cfg Config) (Link, error) {
		called = cfg.DeviceID == "client-1"
		return &stubLink{}, nil
	})

	got, err := New(context.Background(), "test-link", Config{DeviceID: "client-1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !called {
		t.Fatal("factory did not receive config")
	}
	if _, ok := got.(*stubLink); !ok {
		t.Fatalf("New() returned %T, want *stubLink", got)
	}

	if !reflect.DeepEqual(Available(), []string{"test-link"}) {
		t.Fatalf("Available() = %#v, want %#v", Available(), []string{"test-link"})
	}
}

func TestNewReturnsErrLinkNotFound(t *testing.T) {
	old := snapshotLinkRegistry()
	t.Cleanup(func() { restoreLinkRegistry(old) })
	registry = map[string]Factory{}

	_, err := New(context.Background(), "missing", Config{})
	if !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("New() error = %v, want %v", err, ErrLinkNotFound)
	}
}
