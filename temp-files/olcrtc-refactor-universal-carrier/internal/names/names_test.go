package names

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseEmbedded(t *testing.T) {
	got := parseEmbedded(" Alice \n\n Bob\n")
	want := []string{"Alice", "Bob"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseEmbedded() = %#v, want %#v", got, want)
	}
}

func TestLoadNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "names.txt")
	if err := os.WriteFile(path, []byte(" Alice \n\nBob\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := loadNames(path)
	if err != nil {
		t.Fatalf("loadNames() error = %v", err)
	}
	want := []string{"Alice", "Bob"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("loadNames() = %#v, want %#v", got, want)
	}
}

func TestLoadNameFilesOverridesGlobals(t *testing.T) {
	oldFirst, oldLast := append([]string(nil), firstNames...), append([]string(nil), lastNames...)
	t.Cleanup(func() {
		firstNames = oldFirst
		lastNames = oldLast
	})

	dir := t.TempDir()
	first := filepath.Join(dir, "first.txt")
	last := filepath.Join(dir, "last.txt")
	if err := os.WriteFile(first, []byte("Neo\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(first) error = %v", err)
	}
	if err := os.WriteFile(last, []byte("Anderson\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(last) error = %v", err)
	}

	if err := LoadNameFiles(first, last); err != nil {
		t.Fatalf("LoadNameFiles() error = %v", err)
	}

	if got := Generate(); got != "Neo Anderson" {
		t.Fatalf("Generate() = %q, want %q", got, "Neo Anderson")
	}
}

func TestGenerateFallsBackWhenNamesEmpty(t *testing.T) {
	oldFirst, oldLast := append([]string(nil), firstNames...), append([]string(nil), lastNames...)
	t.Cleanup(func() {
		firstNames = oldFirst
		lastNames = oldLast
	})

	firstNames = nil
	lastNames = nil

	if got := Generate(); got != "anonymous user" {
		t.Fatalf("Generate() = %q, want anonymous user", got)
	}
}

func TestRandomIndexBounds(t *testing.T) {
	for range 20 {
		got := randomIndex(2)
		if got < 0 || got > 1 {
			t.Fatalf("randomIndex(2) = %d, out of range", got)
		}
	}

	if got := randomIndex(0); got != 0 {
		t.Fatalf("randomIndex(0) = %d, want 0", got)
	}
}

func TestLoadNameFilesIgnoresMissingFiles(t *testing.T) {
	oldFirst, oldLast := append([]string(nil), firstNames...), append([]string(nil), lastNames...)
	t.Cleanup(func() {
		firstNames = oldFirst
		lastNames = oldLast
	})

	firstNames = []string{"Kept"}
	lastNames = []string{"Value"}
	if err := LoadNameFiles("missing-first", "missing-last"); err != nil {
		t.Fatalf("LoadNameFiles() error = %v", err)
	}

	got := Generate()
	if !strings.Contains(got, "Kept") || !strings.Contains(got, "Value") {
		t.Fatalf("Generate() = %q, want preserved names", got)
	}
}
