package jitsi

import (
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/pion/webrtc/v4"
)

// ICE URL schemes and TURN transports per RFC 7064 / RFC 7065.
const (
	schemeSTUN  = "stun"
	schemeSTUNS = "stuns"
	schemeTURN  = "turn"
	schemeTURNS = "turns"

	transportUDP = "udp"
	transportTCP = "tcp"

	// Default ports pion's stun.ParseURI substitutes when a URL has no
	// port at all (3478 for stun/turn, 5349 for the TLS variants).
	defaultPortSTUN = "3478"
	defaultPortTLS  = "5349"
)

// normaliseICEServers rewrites ICE server URLs into the canonical
// scheme:host:port?transport=proto form pion accepts, preserving as many
// entries as possible.
//
// Some Jitsi deployments advertise TURN/STUN services over XEP-0215 disco
// without a port or transport attribute. The j library renders those as URLs
// like "stun:host:" or "turn:host:?transport=", and pion validates every
// ICEServers URL inside NewPeerConnection, failing the whole configuration
// with "InvalidAccessError: invalid port" before a single candidate is
// gathered. Rather than dropping such entries (they usually point at a
// perfectly good relay listening on the default port), normalisation fills
// in the scheme's default port and strips unusable transport queries, and
// only discards URLs with no salvageable host/port. Servers left with no
// URLs are removed; credentials are kept untouched.
//
// pion also validates TURN credentials inside NewPeerConnection: any
// turn/turns URL on a server with an empty username or a credential that is
// nil or not a non-empty password string fails the whole configuration with
// "no turn server credentials"/"invalid turn server credentials". TURN/TURNS
// URLs on such servers are therefore dropped while stun/stuns URLs on the
// same server (which need no credentials) are retained.
func normaliseICEServers(in []webrtc.ICEServer) []webrtc.ICEServer {
	out := make([]webrtc.ICEServer, 0, len(in))
	for _, srv := range in {
		turnOK := hasPionTURNCredentials(srv)
		urls := make([]string, 0, len(srv.URLs))
		for _, raw := range srv.URLs {
			u, ok := normaliseICEServerURL(raw)
			if !ok {
				continue
			}
			if !turnOK && isTURNURL(u) {
				continue
			}
			urls = append(urls, u)
		}
		if len(urls) == 0 {
			continue
		}
		srv.URLs = urls
		out = append(out, srv)
	}
	return out
}

// hasPionTURNCredentials reports whether a server carries credentials that
// pion's ICEServer.urls() accepts for turn/turns URLs with the password
// credential type (the only type Jitsi XEP-0215 discovery produces): a
// non-empty Username and a Credential holding a non-empty string. OAuth
// credentials are deliberately not recognised here; nothing in this code
// path sets CredentialType, so pion would treat the credential as a
// password anyway.
func hasPionTURNCredentials(srv webrtc.ICEServer) bool {
	if srv.Username == "" || srv.Credential == nil {
		return false
	}
	password, ok := srv.Credential.(string)
	return ok && password != ""
}

// isTURNURL reports whether a normalised ICE URL uses the turn or turns
// scheme. It is only called on normaliseICEServerURL output, whose scheme is
// already lowercased.
func isTURNURL(u string) bool {
	return strings.HasPrefix(u, schemeTURN+":") || strings.HasPrefix(u, schemeTURNS+":")
}

// normaliseICEServerURL canonicalises one stun/stuns/turn/turns URL.
//
// It mirrors pion's stun.ParseURI semantics (opaque host:port form, default
// ports for missing ports, transport query only meaningful for turn/turns)
// with deliberate, lenient deviations that keep default-port relays usable
// instead of erroring the whole peer-connection config:
//   - an empty port ("stun:host:") gets the scheme default port, where pion
//     returns ErrPort;
//   - a missing, empty or unknown transport value is stripped so pion falls
//     back to the scheme default (udp for turn, tcp for turns), and known
//     values are lowercased; pion would return ErrProtoType/ErrInvalidQuery.
//     Likewise queries on stun/stuns URLs are stripped, where pion returns
//     ErrSTUNQuery.
//
// It is stricter than stun.ParseURI in one respect: ports outside 1-65535
// are rejected here (ParseURI accepts any integer and the ICE agent fails
// later), because such an entry can never produce a working candidate.
func normaliseICEServerURL(raw string) (string, bool) {
	parts, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parts.Opaque == "" {
		// Includes "scheme://host" authority forms: RFC 7064/7065 URLs
		// are opaque, and pion rejects authority-style variants too.
		return "", false
	}
	scheme := strings.ToLower(parts.Scheme)
	defaultPort, ok := iceDefaultPort(scheme)
	if !ok {
		return "", false
	}
	hostport, ok := joinICEHostPort(parts.Opaque, defaultPort)
	if !ok {
		return "", false
	}
	normalised := scheme + ":" + hostport
	if transport, keep := iceTransport(scheme, parts.RawQuery); keep {
		normalised += "?transport=" + transport
	}
	return normalised, true
}

// iceDefaultPort returns the default port for an ICE URL scheme and whether
// the scheme names an ICE server at all.
func iceDefaultPort(scheme string) (string, bool) {
	switch scheme {
	case schemeSTUN, schemeTURN:
		return defaultPortSTUN, true
	case schemeSTUNS, schemeTURNS:
		return defaultPortTLS, true
	default:
		return "", false
	}
}

// joinICEHostPort canonicalises the opaque host[:port] part of an ICE URL,
// substituting defaultPort when the port is missing ("host", "[::1]") or
// empty ("host:"). It rejects anything without a usable host or with a port
// outside 1-65535, and re-brackets IPv6 literals via net.JoinHostPort.
func joinICEHostPort(hostport, defaultPort string) (string, bool) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		var addrErr *net.AddrError
		if !errors.As(err, &addrErr) || addrErr.Err != "missing port in address" {
			return "", false
		}
		host = strings.TrimSuffix(strings.TrimPrefix(hostport, "["), "]")
	}
	if host == "" {
		return "", false
	}
	if port == "" {
		port = defaultPort
	}
	if n, atoiErr := strconv.Atoi(port); atoiErr != nil || n < 1 || n > 65535 {
		return "", false
	}
	return net.JoinHostPort(host, port), true
}

// iceTransport extracts a usable transport query value for turn/turns URLs.
// stun/stuns URLs never carry a transport (pion rejects any query on them),
// and unknown or malformed transports are dropped so pion applies the scheme
// default instead of rejecting the URL.
func iceTransport(scheme, rawQuery string) (string, bool) {
	if scheme != schemeTURN && scheme != schemeTURNS {
		return "", false
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", false
	}
	switch transport := strings.ToLower(values.Get("transport")); transport {
	case transportUDP, transportTCP:
		return transport, true
	default:
		return "", false
	}
}
