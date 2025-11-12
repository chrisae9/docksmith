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
			// Build numbers are normalized (ls285 removed), leaving just the build number
			expectSuffix:   "10274",
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

func TestParseTag(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name        string
		tag         string
		expectNil   bool
		expectMajor int
		expectMinor int
		expectPatch int
	}{
		{
			name:        "simple semver",
			tag:         "1.2.3",
			expectMajor: 1,
			expectMinor: 2,
			expectPatch: 3,
		},
		{
			name:        "with v prefix",
			tag:         "v10.5.2",
			expectMajor: 10,
			expectMinor: 5,
			expectPatch: 2,
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
		})
	}
}
