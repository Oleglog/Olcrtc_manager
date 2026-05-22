package carrier

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type stubSession struct{}

func (s *stubSession) Capabilities() Capabilities {
	return Capabilities{ByteStream: true, VideoTrack: true}
}

func snapshotCarrierRegistry() map[string]Factory {
	out := make(map[string]Factory, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

func restoreCarrierRegistry(src map[string]Factory) {
	registry = make(map[string]Factory, len(src))
	for k, v := range src {
		registry[k] = v
	}
}

func TestRegisterAndAvailable(t *testing.T) {
	old := snapshotCarrierRegistry()
	t.Cleanup(func() { restoreCarrierRegistry(old) })

	Register("test-carrier", func(_ context.Context, cfg Config) (Session, error) {
		if cfg.Name != "peer" {
			t.Fatalf("carrier config name = %q, want peer", cfg.Name)
		}
		return &stubSession{}, nil
	})

	sess, err := New(context.Background(), "test-carrier", Config{Name: "peer"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	caps := sess.Capabilities()
	if !caps.ByteStream || !caps.VideoTrack {
		t.Fatalf("Capabilities() = %+v, want byte and video true", caps)
	}

	if !reflect.DeepEqual(Available(), []string{"test-carrier"}) {
		t.Fatalf("Available() = %#v, want %#v", Available(), []string{"test-carrier"})
	}
}

func TestNewReturnsErrCarrierNotFound(t *testing.T) {
	old := snapshotCarrierRegistry()
	t.Cleanup(func() { restoreCarrierRegistry(old) })
	registry = map[string]Factory{}

	_, err := New(context.Background(), "missing", Config{})
	if !errors.Is(err, ErrCarrierNotFound) {
		t.Fatalf("New() error = %v, want %v", err, ErrCarrierNotFound)
	}
}
