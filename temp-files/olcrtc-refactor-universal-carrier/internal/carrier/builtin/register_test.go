package builtin

import (
	"slices"
	"testing"

	"github.com/openlibrecommunity/olcrtc/internal/carrier"
)

func TestRegister(t *testing.T) {
	Register()
	available := carrier.Available()
	for _, want := range []string{"jazz", "telemost", "wbstream"} {
		if !slices.Contains(available, want) {
			t.Fatalf("Available() = %v, missing %q", available, want)
		}
	}
}
