// Package builtin registers the built-in carrier implementations.
package builtin

import (
	authJitsi "github.com/openlibrecommunity/olcrtc/internal/auth/jitsi"
	authSaluteJazz "github.com/openlibrecommunity/olcrtc/internal/auth/salutejazz"
	authTelemost "github.com/openlibrecommunity/olcrtc/internal/auth/telemost"
	authWBStream "github.com/openlibrecommunity/olcrtc/internal/auth/wbstream"
	_ "github.com/openlibrecommunity/olcrtc/internal/engine/goolom"     // engine registration via init
	_ "github.com/openlibrecommunity/olcrtc/internal/engine/jitsi"      // engine registration via init
	_ "github.com/openlibrecommunity/olcrtc/internal/engine/livekit"    // engine registration via init
	_ "github.com/openlibrecommunity/olcrtc/internal/engine/salutejazz" // engine registration via init
)

// Register wires the built-in carriers into the carrier registry.
func Register() {
	registerEngineAuth("wbstream", authWBStream.Provider{})
	registerEngineAuth("jazz", authSaluteJazz.Provider{})
	registerEngineAuth("telemost", authTelemost.Provider{})
	registerEngineAuth("jitsi", authJitsi.Provider{})
	registerDirect("none")
}
