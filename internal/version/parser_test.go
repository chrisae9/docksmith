package version

import "testing"

func TestParseImageTag(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name           string
		imageTag       string
		expectVersioned bool
		expectLatest   bool
		expectMajor    int
		expectMinor    int
		expectPatch    int
		expectSuffix   string
	}{
		{
			name:           "semver with colon",
			imageTag:       "nginx:1.21.3",
			expectVersioned: true,
			expectMajor:    1,
			expectMinor:    21,
			expectPatch:    3,
		},
		{
			name:           "semver with alpine suffix",
			imageTag:       "nginx:1.21.3-alpine",
			expectVersioned: true,
			expectMajor:    1,
			expectMinor:    21,
			expectPatch:    3,
			// "alpine" is now correctly extracted as a suffix
			expectSuffix:   "alpine",
		},
		{
			name:         "latest tag",
			imageTag:     "nginx:latest",
			expectLatest: true,
		},
		{
			name:           "linuxserver format",
			imageTag:       "ghcr.io/linuxserver/plex:5.28.0.10274-ls285",
			expectVersioned: true,
			expectMajor:    5,
			expectMinor:    28,
			expectPatch:    0,
			// 4th segment (10274) is now captured as Revision, ls285 normalized away
			expectSuffix:   "",
		},
		{
			name:           "linuxserver sonarr format with dot-separated build number",
			imageTag:       "ghcr.io/linuxserver/sonarr:4.0.16.2946-ls297",
			expectVersioned: true,
			expectMajor:    4,
			expectMinor:    0,
			expectPatch:    16,
			// Both .2946 and -ls297 should be normalized away, leaving empty suffix
			expectSuffix:   "",
		},
		{
			name:           "version with v prefix",
			imageTag:       "traefik:v2.9.6",
			expectVersioned: true,
			expectMajor:    2,
			expectMinor:    9,
			expectPatch:    6,
		},
		{
			name:           "version with prerelease",
			imageTag:       "app:1.0.0-beta.1",
			expectVersioned: true,
			expectMajor:    1,
			expectMinor:    0,
			expectPatch:    0,
		},
		{
			name:           "major.minor only",
			imageTag:       "postgres:14",
			expectVersioned: true,
			expectMajor:    14,
			expectMinor:    0,
			expectPatch:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag(tt.imageTag)

			if info.IsLatest != tt.expectLatest {
				t.Errorf("IsLatest: got %v, want %v", info.IsLatest, tt.expectLatest)
			}

			if info.IsVersioned != tt.expectVersioned {
				t.Errorf("IsVersioned: got %v, want %v", info.IsVersioned, tt.expectVersioned)
			}

			if tt.expectVersioned {
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
			}

			if info.Suffix != tt.expectSuffix {
				t.Errorf("Suffix: got %q, want %q", info.Suffix, tt.expectSuffix)
			}
		})
	}
}

func TestBuildNumberTiebreaking(t *testing.T) {
	parser := NewParser()
	comp := NewComparator()

	// Parse tags like Calibre uses
	tag1 := parser.ParseImageTag("linuxserver/calibre:v8.16.2-ls374")
	tag2 := parser.ParseImageTag("linuxserver/calibre:8.16.2")

	if tag1.Version == nil || tag2.Version == nil {
		t.Fatal("Expected versions to be parsed")
	}

	// Both have same semantic version
	if tag1.Version.Major != 8 || tag1.Version.Minor != 16 || tag1.Version.Patch != 2 {
		t.Errorf("tag1 version: got %d.%d.%d, want 8.16.2", tag1.Version.Major, tag1.Version.Minor, tag1.Version.Patch)
	}
	if tag2.Version.Major != 8 || tag2.Version.Minor != 16 || tag2.Version.Patch != 2 {
		t.Errorf("tag2 version: got %d.%d.%d, want 8.16.2", tag2.Version.Major, tag2.Version.Minor, tag2.Version.Patch)
	}

	// tag1 should have BuildNumber=374, tag2 should have BuildNumber=0
	if tag1.Version.BuildNumber != 374 {
		t.Errorf("tag1 BuildNumber: got %d, want 374", tag1.Version.BuildNumber)
	}
	if tag2.Version.BuildNumber != 0 {
		t.Errorf("tag2 BuildNumber: got %d, want 0", tag2.Version.BuildNumber)
	}

	// tag1 (with ls374) should be considered "newer" than tag2 (without ls suffix)
	// because same semantic version, higher build number
	cmp := comp.Compare(tag1.Version, tag2.Version)
	if cmp != 1 {
		t.Errorf("Compare(v8.16.2-ls374, 8.16.2): got %d, want 1 (tag1 newer)", cmp)
	}

	// IsNewer(v1, v2) returns true if v2 > v1
	if !comp.IsNewer(tag2.Version, tag1.Version) {
		t.Error("IsNewer(8.16.2, v8.16.2-ls374) should be true (ls374 is newer)")
	}
}

func TestLinuxServerBuildNumber(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name              string
		imageTag          string
		expectBuildNumber int
		expectSuffix      string
	}{
		{
			name:              "calibre style v-prefixed with ls suffix",
			imageTag:          "linuxserver/calibre:v8.16.0-ls374",
			expectBuildNumber: 374,
			expectSuffix:      "",
		},
		{
			name:              "calibre bare version (no ls suffix)",
			imageTag:          "linuxserver/calibre:8.16.2",
			expectBuildNumber: 0,
			expectSuffix:      "",
		},
		{
			name:              "plex with 5-digit build number and ls suffix",
			imageTag:          "ghcr.io/linuxserver/plex:5.28.0.10274-ls285",
			expectBuildNumber: 285,
			expectSuffix:      "",
		},
		{
			name:              "sonarr with 4-digit build number and ls suffix",
			imageTag:          "ghcr.io/linuxserver/sonarr:4.0.16.2946-ls297",
			expectBuildNumber: 297,
			expectSuffix:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag(tt.imageTag)

			if info.Version == nil {
				t.Fatal("Expected version but got nil")
			}

			if info.Version.BuildNumber != tt.expectBuildNumber {
				t.Errorf("BuildNumber: got %d, want %d", info.Version.BuildNumber, tt.expectBuildNumber)
			}

			if info.Suffix != tt.expectSuffix {
				t.Errorf("Suffix: got %q, want %q", info.Suffix, tt.expectSuffix)
			}
		})
	}
}

func TestParseTag(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name        string
		tag         string
		expectNil   bool
		expectMajor int
		expectMinor int
		expectPatch int
		expectType  string // expected Version.Type ("semantic", "date", or "")
	}{
		{
			name:        "simple semver",
			tag:         "1.2.3",
			expectMajor: 1,
			expectMinor: 2,
			expectPatch: 3,
			expectType:  "semantic",
		},
		{
			name:        "with v prefix",
			tag:         "v10.5.2",
			expectMajor: 10,
			expectMinor: 5,
			expectPatch: 2,
			expectType:  "semantic",
		},
		{
			name:      "non-version tag",
			tag:       "latest",
			expectNil: true,
		},
		{
			name:      "random string",
			tag:       "stable-alpine",
			expectNil: true,
		},
		{
			name:        "date-format version (dot-separated)",
			tag:         "2026.2.9",
			expectMajor: 2026,
			expectMinor: 2,
			expectPatch: 9,
			expectType:  "date",
		},
		{
			name:        "date-format version with v prefix is semantic",
			tag:         "v2026.2.9",
			expectMajor: 2026,
			expectMinor: 2,
			expectPatch: 9,
			expectType:  "semantic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version := parser.ParseTag(tt.tag)

			if tt.expectNil {
				if version != nil {
					t.Errorf("Expected nil version, got %v", version)
				}
				return
			}

			if version == nil {
				t.Fatal("Expected version but got nil")
			}

			if version.Major != tt.expectMajor {
				t.Errorf("Major: got %d, want %d", version.Major, tt.expectMajor)
			}
			if version.Minor != tt.expectMinor {
				t.Errorf("Minor: got %d, want %d", version.Minor, tt.expectMinor)
			}
			if version.Patch != tt.expectPatch {
				t.Errorf("Patch: got %d, want %d", version.Patch, tt.expectPatch)
			}
			if tt.expectType != "" && version.Type != tt.expectType {
				t.Errorf("Type: got %q, want %q", version.Type, tt.expectType)
			}
		})
	}
}
