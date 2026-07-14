package admin

import "testing"

func TestParseReleaseTag(t *testing.T) {
	tests := []struct {
		tag     string
		branch  string
		version string
		ok      bool
	}{
		{tag: "server-v1.9.53", branch: "master", version: "1.9.53", ok: true},
		{tag: "server-latency-tuning-v1.9.53", branch: "latency-tuning", version: "1.9.53", ok: true},
		{tag: "server-master-v1.9.53"},
		{tag: "server-feature/foo-v1.9.53"},
		{tag: "server-latency-tuning-v1.9"},
		{tag: "server-latency-tuning-v1.9.53;reboot"},
	}
	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			branch, version, ok := parseReleaseTag(tt.tag)
			if ok != tt.ok || branch != tt.branch || version != tt.version {
				t.Fatalf("parseReleaseTag(%q) = (%q, %q, %v), want (%q, %q, %v)", tt.tag, branch, version, ok, tt.branch, tt.version, tt.ok)
			}
		})
	}
}

func TestResolveReleaseTarget(t *testing.T) {
	target, err := resolveReleaseTarget("server-latency-tuning-v1.9.53", "latency-tuning", "v1.9.53")
	if err != nil {
		t.Fatal(err)
	}
	if target.DownloadURL != "https://github.com/Oleglog/Olcrtc_manager/releases/download/server-latency-tuning-v1.9.53" {
		t.Fatalf("DownloadURL = %q", target.DownloadURL)
	}

	legacy, err := resolveReleaseTarget("", "", "v1.9.53")
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Tag != "server-v1.9.53" || legacy.Branch != "master" {
		t.Fatalf("legacy target = %+v", legacy)
	}

	for _, input := range []struct {
		tag, branch, version string
	}{
		{tag: "server-latency-tuning-v1.9.53", branch: "master", version: "v1.9.53"},
		{tag: "server-latency-tuning-v1.9.53", branch: "latency-tuning", version: "v1.9.54"},
		{tag: "server-v1.9.53$(reboot)", branch: "master", version: "v1.9.53"},
	} {
		if _, err := resolveReleaseTarget(input.tag, input.branch, input.version); err == nil {
			t.Fatalf("resolveReleaseTarget(%q, %q, %q) succeeded", input.tag, input.branch, input.version)
		}
	}
}

func TestReleaseInfoFromTag(t *testing.T) {
	release, ok := releaseInfoFromTag("server-latency-tuning-v1.9.53", "url", "date", false)
	if !ok {
		t.Fatal("branch release was rejected")
	}
	if release.Branch != "latency-tuning" || release.Version != "v1.9.53" || !release.Prerelease {
		t.Fatalf("release = %+v", release)
	}
}
