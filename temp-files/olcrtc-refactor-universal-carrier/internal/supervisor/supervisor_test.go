package supervisor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openlibrecommunity/olcrtc/internal/app/session"
)

var errRunnerBoom = errors.New("boom")

const (
	testProfileFirst  = "first"
	testProfileSecond = "second"
	testProfileOne    = "one"
)

func TestRunRequiresProfiles(t *testing.T) {
	err := Run(context.Background(), Config{}, func(context.Context, session.Config) error { return nil })
	if !errors.Is(err, ErrNoProfiles) {
		t.Fatalf("Run() error = %v, want %v", err, ErrNoProfiles)
	}
}

func TestRunAdvancesProfilesAndStopsAtMaxCycles(t *testing.T) {
	profiles := []Profile{
		{Name: testProfileFirst, Config: session.Config{Auth: "wbstream"}},
		{Name: testProfileSecond, Config: session.Config{Auth: "jitsi"}},
	}
	var started []string
	var ended []string
	err := Run(context.Background(), Config{
		Profiles:   profiles,
		RetryDelay: -1,
		MaxCycles:  1,
		OnProfileStart: func(profile Profile, cycle int) {
			started = append(started, profile.Name)
			if cycle != 1 {
				t.Fatalf("cycle = %d, want 1", cycle)
			}
		},
		OnProfileEnd: func(profile Profile, _ int, err error) {
			ended = append(ended, profile.Name)
			if !errors.Is(err, errRunnerBoom) {
				t.Fatalf("profile %s err = %v, want %v", profile.Name, err, errRunnerBoom)
			}
		},
	}, func(_ context.Context, cfg session.Config) error {
		if cfg.Auth == "" {
			t.Fatal("runner received empty auth")
		}
		return errRunnerBoom
	})
	if !errors.Is(err, ErrMaxCyclesExceeded) {
		t.Fatalf("Run() error = %v, want %v", err, ErrMaxCyclesExceeded)
	}
	if got, want := started, []string{testProfileFirst, testProfileSecond}; !equalStrings(got, want) {
		t.Fatalf("started = %v, want %v", got, want)
	}
	if got, want := ended, []string{testProfileFirst, testProfileSecond}; !equalStrings(got, want) {
		t.Fatalf("ended = %v, want %v", got, want)
	}
}

//nolint:cyclop // status history test verifies one complete failover cycle
func TestRunEmitsStatusHistory(t *testing.T) {
	profiles := []Profile{
		{Name: testProfileFirst, Config: session.Config{Auth: "wbstream"}},
		{Name: testProfileSecond, Config: session.Config{Auth: "jitsi"}},
	}
	var snapshots []Status
	err := Run(context.Background(), Config{
		Profiles:     profiles,
		RetryDelay:   -1,
		MaxCycles:    1,
		HistoryLimit: 3,
		OnStatus: func(status Status) {
			snapshots = append(snapshots, status)
		},
	}, func(_ context.Context, cfg session.Config) error {
		if cfg.Auth == testProfileFirst {
			t.Fatal("runner received profile name instead of config")
		}
		return errRunnerBoom
	})
	if !errors.Is(err, ErrMaxCyclesExceeded) {
		t.Fatalf("Run() error = %v, want %v", err, ErrMaxCyclesExceeded)
	}
	if len(snapshots) != 4 {
		t.Fatalf("status snapshots = %d, want 4", len(snapshots))
	}
	first := snapshots[0]
	if first.ActiveProfile != testProfileFirst || first.ActiveProfileIndex != 0 || first.Cycle != 1 {
		t.Fatalf("first status = %+v", first)
	}
	if first.Profiles[0].Starts != 1 || first.Profiles[0].LastStarted.IsZero() {
		t.Fatalf("first profile start status = %+v", first.Profiles[0])
	}
	last := snapshots[len(snapshots)-1]
	if last.ActiveProfile != "" || last.ActiveProfileIndex != -1 {
		t.Fatalf("last active status = %+v", last)
	}
	if last.Profiles[0].Failures != 1 || last.Profiles[1].Failures != 1 {
		t.Fatalf("profile failures = %+v", last.Profiles)
	}
	if last.LastError == "" || last.Profiles[1].LastError == "" {
		t.Fatalf("last errors missing: %+v", last)
	}
	if len(last.History) != 3 {
		t.Fatalf("history length = %d, want 3", len(last.History))
	}
	if last.History[0].Type != EventProfileEnd || last.History[0].Profile != testProfileFirst {
		t.Fatalf("oldest bounded history event = %+v", last.History[0])
	}
	if last.History[2].Type != EventProfileEnd || last.History[2].Profile != testProfileSecond ||
		last.History[2].Error == "" {
		t.Fatalf("last history event = %+v", last.History[2])
	}
}

func TestRunStatusSnapshotIsImmutable(t *testing.T) {
	var first Status
	var second Status
	err := Run(context.Background(), Config{
		Profiles:   []Profile{{Name: testProfileOne}},
		RetryDelay: -1,
		MaxCycles:  1,
		OnStatus: func(status Status) {
			if first.Profiles == nil {
				first = status
				first.Profiles[0].Starts = 99
				first.History[0].Profile = "mutated"
				return
			}
			second = status
		},
	}, func(context.Context, session.Config) error {
		return errRunnerBoom
	})
	if !errors.Is(err, ErrMaxCyclesExceeded) {
		t.Fatalf("Run() error = %v, want %v", err, ErrMaxCyclesExceeded)
	}
	if first.Profiles[0].Starts != 99 || first.History[0].Profile != "mutated" {
		t.Fatalf("test mutation did not apply to snapshot: %+v", first)
	}
	if second.Profiles[0].Starts != 1 || second.History[0].Profile != testProfileOne {
		t.Fatalf("snapshot mutation leaked into later status: %+v", second)
	}
}

func TestRunReturnsNilOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	err := Run(ctx, Config{
		Profiles:   []Profile{{Name: testProfileOne}},
		RetryDelay: time.Hour,
	}, func(context.Context, session.Config) error {
		cancel()
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
