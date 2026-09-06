package admin

import "testing"

func TestCarrierTransportCompatibilityMatrix(t *testing.T) {
	compatible := [][2]string{
		{"telemost", "vp8channel"},
		{"wbstream", "vp8channel"},
		{"jitsi", "datachannel"},
	}
	for _, combo := range compatible {
		if !isCarrierTransportCompatible(combo[0], combo[1]) {
			t.Fatalf("expected %s+%s to be compatible", combo[0], combo[1])
		}
	}

	incompatible := [][2]string{
		{"telemost", "datachannel"},
		{"wbstream", "datachannel"},
		{"jitsi", "vp8channel"},
		{"telemost", "seichannel"},
		{"wbstream", "seichannel"},
		{"jitsi", "seichannel"},
		{"telemost", "videochannel"},
		{"wbstream", "videochannel"},
		{"jitsi", "videochannel"},
		{"unknown", "vp8channel"},
		{"telemost", "unknown"},
		{"", ""},
	}
	for _, combo := range incompatible {
		if isCarrierTransportCompatible(combo[0], combo[1]) {
			t.Fatalf("expected %s+%s to be incompatible", combo[0], combo[1])
		}
	}
}

func TestCompatibleTransports(t *testing.T) {
	if got := compatibleTransports("telemost"); len(got) != 1 || got[0] != "vp8channel" {
		t.Fatalf("telemost transports = %v", got)
	}
	if got := compatibleTransports("wbstream"); len(got) != 1 || got[0] != "vp8channel" {
		t.Fatalf("wbstream transports = %v", got)
	}
	if got := compatibleTransports("jitsi"); len(got) != 1 || got[0] != "datachannel" {
		t.Fatalf("jitsi transports = %v", got)
	}
}
