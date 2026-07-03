package goolom

import (
	"testing"
)

const (
	testTURNURL = "turn:turn.tel.yandex.net:3478?transport=udp"
	keyURLs     = "urls"
)

// TestParseICEServerKeepsTURN locks in the issue #95 fix: the SFU-advertised
// ICE server set must retain TURN relays, not just STUN. Stripping TURN left
// symmetric/CGNAT mobile clients with no durable candidate pair.
func TestParseICEServerKeepsTURN(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
		want []string
	}{
		{
			name: "turn and stun both kept",
			raw: map[string]any{
				keyURLs:      []any{testTURNURL, defaultSTUNURL},
				"username":   "user",
				"credential": "pass",
			},
			want: []string{testTURNURL, defaultSTUNURL},
		},
		{
			name: "turns kept",
			raw:  map[string]any{keyURLs: []any{"turns:turn.tel.yandex.net:5349"}},
			want: []string{"turns:turn.tel.yandex.net:5349"},
		},
		{
			name: "stun only kept",
			raw:  map[string]any{keyURLs: []any{defaultSTUNURL}},
			want: []string{defaultSTUNURL},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ice, ok := parseICEServer(tc.raw)
			if !ok {
				t.Fatalf("parseICEServer(%v) returned ok=false", tc.raw)
			}
			if len(ice.URLs) != len(tc.want) {
				t.Fatalf("URLs = %v, want %v", ice.URLs, tc.want)
			}
			for i, want := range tc.want {
				if ice.URLs[i] != want {
					t.Fatalf("URLs[%d] = %q, want %q", i, ice.URLs[i], want)
				}
			}
		})
	}
}

// TestParseICEServerCredentials confirms TURN credentials survive parsing,
// since a relay is useless without them.
func TestParseICEServerCredentials(t *testing.T) {
	ice, ok := parseICEServer(map[string]any{
		keyURLs:      []any{testTURNURL},
		"username":   "u",
		"credential": "c",
	})
	if !ok {
		t.Fatal("parseICEServer returned ok=false")
	}
	if ice.Username != "u" || ice.Credential != "c" {
		t.Fatalf("creds = %q/%v, want u/c", ice.Username, ice.Credential)
	}
}
