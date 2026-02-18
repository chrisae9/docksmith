package version

import "testing"

// TestAuditRealContainerParsing tests version parsing with real container tags
// from the production environment, covering 4-segment versions, git hashes,
// architecture prefixes, and various suffix patterns.
func TestAuditRealContainerParsing(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name            string
		imageTag        string
		expectVersioned bool
		expectMajor     int
		expectMinor     int
		expectPatch     int
		expectRevision  int
		expectHasRev    bool
		expectBuildNum  int
		expectSuffix    string
		expectType      string // "semantic", "date", "hash", "meta", ""
	}{
		// 4-segment LinuxServer tags
		{
			name:            "plex 4-segment with git hash and ls suffix",
			imageTag:        "ghcr.io/linuxserver/plex:1.42.2.10156-f737b826c-ls292",
			expectVersioned: true,
			expectMajor:     1, expectMinor: 42, expectPatch: 2,
			expectRevision: 10156, expectHasRev: true,
			expectBuildNum: 292, expectSuffix: "",
			expectType: "semantic",
		},
		{
			name:            "plex latest 4-segment",
			imageTag:        "ghcr.io/linuxserver/plex:1.43.0.10492-121068a07-ls293",
			expectVersioned: true,
			expectMajor:     1, expectMinor: 43, expectPatch: 0,
			expectRevision: 10492, expectHasRev: true,
			expectBuildNum: 293, expectSuffix: "",
			expectType: "semantic",
		},
		{
			name:            "kavita 4-segment with ls suffix",
			imageTag:        "ghcr.io/linuxserver/kavita:v0.8.9.1-ls99",
			expectVersioned: true,
			expectMajor:     0, expectMinor: 8, expectPatch: 9,
			expectRevision: 1, expectHasRev: true,
			expectBuildNum: 99, expectSuffix: "",
			expectType: "semantic",
		},
		{
			name:            "sonarr 4-segment with ls suffix",
			imageTag:        "ghcr.io/linuxserver/sonarr:4.0.16.2946-ls297",
			expectVersioned: true,
			expectMajor:     4, expectMinor: 0, expectPatch: 16,
			expectRevision: 2946, expectHasRev: true,
			expectBuildNum: 297, expectSuffix: "",
			expectType: "semantic",
		},
		{
			name:            "prowlarr 4-segment",
			imageTag:        "ghcr.io/linuxserver/prowlarr:1.36.2.5059-ls135",
			expectVersioned: true,
			expectMajor:     1, expectMinor: 36, expectPatch: 2,
			expectRevision: 5059, expectHasRev: true,
			expectBuildNum: 135, expectSuffix: "",
			expectType: "semantic",
		},
		{
			name:            "radarr 4-segment",
			imageTag:        "ghcr.io/linuxserver/radarr:5.23.3.9987-ls271",
			expectVersioned: true,
			expectMajor:     5, expectMinor: 23, expectPatch: 3,
			expectRevision: 9987, expectHasRev: true,
			expectBuildNum: 271, expectSuffix: "",
			expectType: "semantic",
		},
		// 3-segment standard versions
		{
			name:            "glances current (3-segment)",
			imageTag:        "nicolargo/glances:4.5.0",
			expectVersioned: true,
			expectMajor:     4, expectMinor: 5, expectPatch: 0,
			expectRevision: 0, expectHasRev: false,
			expectSuffix: "", expectType: "semantic",
		},
		{
			name:            "glances latest (4-segment)",
			imageTag:        "nicolargo/glances:4.5.0.5",
			expectVersioned: true,
			expectMajor:     4, expectMinor: 5, expectPatch: 0,
			expectRevision: 5, expectHasRev: true,
			expectSuffix: "", expectType: "semantic",
		},
		{
			name:            "caddy major.minor only",
			imageTag:        "caddy:2.11",
			expectVersioned: true,
			expectMajor:     2, expectMinor: 11, expectPatch: 0,
			expectSuffix: "", expectType: "semantic",
		},
		{
			name:            "mosquitto with openssl suffix",
			imageTag:        "eclipse-mosquitto:2.0.21-openssl",
			expectVersioned: true,
			expectMajor:     2, expectMinor: 0, expectPatch: 21,
			expectSuffix: "openssl", expectType: "semantic",
		},
		{
			name:            "gluetun v-prefixed",
			imageTag:        "qmcgaw/gluetun:v3.40.3",
			expectVersioned: true,
			expectMajor:     3, expectMinor: 40, expectPatch: 3,
			expectSuffix: "", expectType: "semantic",
		},
		{
			name:            "traefik v-prefixed",
			imageTag:        "traefik:v3.3.6",
			expectVersioned: true,
			expectMajor:     3, expectMinor: 3, expectPatch: 6,
			expectSuffix: "", expectType: "semantic",
		},
		// Git hash tags that must NOT parse as versions
		{
			name:            "bazarr hash tag (digit-leading hex)",
			imageTag:        "ghcr.io/linuxserver/bazarr:8adcd0b4-ls46",
			expectVersioned: false,
			expectType:      "hash",
		},
		{
			name:            "tautulli hash tag (digit-leading hex)",
			imageTag:        "ghcr.io/linuxserver/tautulli:3e0b2401-ls2",
			expectVersioned: false,
			expectType:      "hash",
		},
		{
			name:            "pure short git hash",
			imageTag:        "myapp:abc123d",
			expectVersioned: false,
			expectType:      "hash",
		},
		// Architecture prefix tags that must NOT parse as versioned
		{
			name:            "amd64 architecture prefix",
			imageTag:        "ghcr.io/linuxserver/plex:amd64-1.43.0.10492-121068a07-ls293",
			expectVersioned: false,
		},
		{
			name:            "arm64v8 architecture prefix",
			imageTag:        "ghcr.io/linuxserver/plex:arm64v8-1.43.0.10492-121068a07-ls293",
			expectVersioned: false,
		},
		{
			name:            "version- prefix tag",
			imageTag:        "ghcr.io/linuxserver/plex:version-1.43.0.10492-121068a07",
			expectVersioned: false,
		},
		// CI build numbers that must NOT parse as versions
		{
			name:            "gluetun CI build number 1243",
			imageTag:        "qmcgaw/gluetun:1243",
			expectVersioned: false,
		},
		{
			name:            "gluetun CI build number 1086",
			imageTag:        "qmcgaw/gluetun:1086",
			expectVersioned: false,
		},
		// Date-based versions
		{
			name:            "seedboxapi date+time tag",
			imageTag:        "myapp:20250413-0519",
			expectVersioned: true,
			expectMajor:     2025, expectMinor: 4, expectPatch: 13,
			// Known: time component "-0519" becomes suffix (not normalized)
			expectSuffix: "0519",
			expectType:   "date",
		},
		{
			name:            "homeassistant date version",
			imageTag:        "homeassistant/home-assistant:2026.2.2",
			expectVersioned: true,
			expectMajor:     2026, expectMinor: 2, expectPatch: 2,
			expectType: "date",
		},
		// Meta tags
		{
			name:            "server-cuda non-versioned tag",
			imageTag:        "myapp:server-cuda",
			expectVersioned: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag(tt.imageTag)

			if info.IsVersioned != tt.expectVersioned {
				t.Errorf("IsVersioned: got %v, want %v", info.IsVersioned, tt.expectVersioned)
			}

			if tt.expectType != "" && info.VersionType != tt.expectType {
				t.Errorf("VersionType: got %q, want %q", info.VersionType, tt.expectType)
			}

			if !tt.expectVersioned {
				return
			}

			if info.Version == nil {
				t.Fatal("Expected version but got nil")
			}

			if info.Version.Major != tt.expectMajor {
				t.Errorf("Major: got %d, want %d", info.Version.Major, tt.expectMajor)
			}
			if info.Version.Minor != tt.expectMinor {
				t.Errorf("Minor: got %d, want %d", info.Version.Minor, tt.expectMinor)
			}
			if info.Version.Patch != tt.expectPatch {
				t.Errorf("Patch: got %d, want %d", info.Version.Patch, tt.expectPatch)
			}
			if info.Version.Revision != tt.expectRevision {
				t.Errorf("Revision: got %d, want %d", info.Version.Revision, tt.expectRevision)
			}
			if info.Version.HasRevision != tt.expectHasRev {
				t.Errorf("HasRevision: got %v, want %v", info.Version.HasRevision, tt.expectHasRev)
			}
			if tt.expectBuildNum != 0 && info.Version.BuildNumber != tt.expectBuildNum {
				t.Errorf("BuildNumber: got %d, want %d", info.Version.BuildNumber, tt.expectBuildNum)
			}
			if info.Suffix != tt.expectSuffix {
				t.Errorf("Suffix: got %q, want %q", info.Suffix, tt.expectSuffix)
			}
		})
	}
}

// TestAuditVersionComparisons tests real-world version comparison scenarios.
func TestAuditVersionComparisons(t *testing.T) {
	parser := NewParser()
	comp := NewComparator()

	tests := []struct {
		name         string
		currentTag   string
		latestTag    string
		expectNewer  bool
		expectSuffix bool // true if both tags should have matching (empty) suffixes
	}{
		{
			name:         "plex 4-segment minor upgrade",
			currentTag:   "1.42.2.10156-f737b826c-ls292",
			latestTag:    "1.43.0.10492-121068a07-ls293",
			expectNewer:  true,
			expectSuffix: true,
		},
		{
			name:         "glances 3-segment to 4-segment",
			currentTag:   "4.5.0",
			latestTag:    "4.5.0.5",
			expectNewer:  true,
			expectSuffix: true,
		},
		{
			name:         "sonarr 4-segment patch upgrade",
			currentTag:   "4.0.14.2938-ls295",
			latestTag:    "4.0.16.2946-ls297",
			expectNewer:  true,
			expectSuffix: true,
		},
		{
			name:         "plex same version different ls build",
			currentTag:   "1.42.2.10156-f737b826c-ls291",
			latestTag:    "1.42.2.10156-f737b826c-ls292",
			expectNewer:  true,
			expectSuffix: true,
		},
		{
			name:         "kavita revision-only upgrade",
			currentTag:   "v0.8.9.1-ls98",
			latestTag:    "v0.8.9.2-ls99",
			expectNewer:  true,
			expectSuffix: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentInfo := parser.ParseImageTag("test:" + tt.currentTag)
			latestInfo := parser.ParseImageTag("test:" + tt.latestTag)

			if currentInfo.Version == nil {
				t.Fatalf("Failed to parse current tag: %s", tt.currentTag)
			}
			if latestInfo.Version == nil {
				t.Fatalf("Failed to parse latest tag: %s", tt.latestTag)
			}

			isNewer := comp.IsNewer(currentInfo.Version, latestInfo.Version)
			if isNewer != tt.expectNewer {
				t.Errorf("IsNewer(%s, %s) = %v, want %v",
					tt.currentTag, tt.latestTag, isNewer, tt.expectNewer)
			}

			if tt.expectSuffix {
				if currentInfo.Suffix != latestInfo.Suffix {
					t.Errorf("Suffix mismatch: current=%q, latest=%q (should match for variant comparison)",
						currentInfo.Suffix, latestInfo.Suffix)
				}
			}
		})
	}
}

// TestAuditGitHashNotVersion ensures git commit hashes starting with digits
// are classified as hashes, not misinterpreted as major version numbers.
func TestAuditGitHashNotVersion(t *testing.T) {
	parser := NewParser()

	// These are real-world tags where hex hashes start with 0-9
	hashTags := []struct {
		tag  string
		desc string
	}{
		{"8adcd0b4-ls46", "bazarr: hex hash starting with 8"},
		{"3e0b2401-ls2", "tautulli: hex hash starting with 3"},
		{"9f1a2b3c", "hypothetical: hex hash starting with 9"},
		{"0abcdef1", "hypothetical: hex hash starting with 0"},
		{"1a2b3c4d5e6f", "hypothetical: long hex hash starting with 1"},
	}

	for _, tt := range hashTags {
		t.Run(tt.desc, func(t *testing.T) {
			info := parser.ParseImageTag("test:" + tt.tag)

			if info.IsVersioned {
				t.Errorf("Tag %q should NOT be versioned (it's a git hash), but got version %v",
					tt.tag, info.Version)
			}

			if info.VersionType != "hash" {
				t.Errorf("Tag %q: VersionType = %q, want %q", tt.tag, info.VersionType, "hash")
			}
		})
	}

	// These should still parse as versions (not hashes)
	versionTags := []struct {
		tag  string
		desc string
	}{
		{"3.0.0-ls2", "real version 3.0.0 with ls suffix"},
		{"8.16.2", "real version 8.16.2"},
		{"v1.5.5-ls338", "v-prefixed version with ls suffix"},
		{"1.42.2.10156-f737b826c-ls292", "4-segment version with embedded hash"},
	}

	for _, tt := range versionTags {
		t.Run(tt.desc, func(t *testing.T) {
			info := parser.ParseImageTag("test:" + tt.tag)

			if !info.IsVersioned {
				t.Errorf("Tag %q SHOULD be versioned but was classified as %q",
					tt.tag, info.VersionType)
			}
		})
	}
}
