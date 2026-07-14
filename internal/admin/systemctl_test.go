package admin

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSystemctlRemoveInstanceRemovesOnlyExactUnit(t *testing.T) {
	oldDir := systemdUnitDir
	oldRun := systemctlRemoveRun
	defer func() {
		systemdUnitDir = oldDir
		systemctlRemoveRun = oldRun
	}()

	systemdUnitDir = t.TempDir()
	var calls [][]string
	systemctlRemoveRun = func(args ...string) (string, error) {
		calls = append(calls, append([]string(nil), args...))
		return "", nil
	}

	service := "olcrtc-server@7.service"
	targets := []string{
		filepath.Join(systemdUnitDir, service),
		filepath.Join(systemdUnitDir, "multi-user.target.wants", service),
		filepath.Join(systemdUnitDir, service+".d", "override.conf"),
	}
	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("test"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	template := filepath.Join(systemdUnitDir, "olcrtc-server@.service")
	other := filepath.Join(systemdUnitDir, "olcrtc-server@8.service")
	for _, keep := range []string{template, other} {
		if err := os.WriteFile(keep, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}

	if err := SystemctlRemoveInstance(service); err != nil {
		t.Fatal(err)
	}
	for _, target := range targets {
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("target still exists: %s", target)
		}
	}
	for _, keep := range []string{template, other} {
		if _, err := os.Stat(keep); err != nil {
			t.Fatalf("unrelated unit removed: %s: %v", keep, err)
		}
	}
	wantCalls := [][]string{
		{"disable", "--now", service},
		{"clean", "--what=state", service},
		{"reset-failed", service},
		{"daemon-reload"},
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("systemctl calls = %v, want %v", calls, wantCalls)
	}
}

func TestSystemctlRemoveInstanceRejectsSharedOrInvalidUnits(t *testing.T) {
	for _, service := range []string{
		"olcrtc-server.service",
		"olcrtc-server@.service",
		"olcrtc-server@0.service",
		"olcrtc-server@7.service/../../other",
	} {
		if err := SystemctlRemoveInstance(service); err == nil {
			t.Fatalf("SystemctlRemoveInstance(%q) succeeded", service)
		}
	}
}
