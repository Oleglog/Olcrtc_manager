package admin

// Carrier/transport compatibility matrix (issue #52, 2026-09-06).
//
// Only these combinations are supported product-wide:
//   - telemost + vp8channel
//   - wbstream + vp8channel
//   - jitsi + datachannel
//
// datachannel is banned for telemost (Goolom SFU does not route standard
// datachannels: dataChannelSharing=TO_RTP) and for wbstream (guest tokens
// carry canPublishData=false). seichannel and videochannel are dead on this
// project and intentionally not offered. The Android client enforces the
// same matrix in OlcrtcProfile.supports(); this file is the server-side
// counterpart used by create/update validation.
func isCarrierTransportCompatible(carrier, transport string) bool {
	switch carrier {
	case "telemost", "wbstream":
		return transport == "vp8channel"
	case "jitsi":
		return transport == "datachannel"
	default:
		return false
	}
}

// compatibleTransports returns the selectable transports for a carrier,
// in preferred order. Used to drive the Admin UI selects.
func compatibleTransports(carrier string) []string {
	switch carrier {
	case "telemost", "wbstream":
		return []string{"vp8channel"}
	case "jitsi":
		return []string{"datachannel"}
	default:
		return []string{"vp8channel", "datachannel"}
	}
}
