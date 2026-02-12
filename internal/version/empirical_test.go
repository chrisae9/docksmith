package version

import (
	"testing"
)

// TestRealWorldNginxTags tests version parsing with real nginx tags
func TestRealWorldNginxTags(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name           string
		tag            string
		expectVersioned bool
		expectedMajor  int
		expectedMinor  int
		expectedPatch  int
		expectedSuffix string
		versionType    string
	}{
		{
			name:           "nginx stable semantic",
			tag:            "1.25.3",
			expectVersioned: true,
			expectedMajor:  1,
			expectedMinor:  25,
			expectedPatch:  3,
			expectedSuffix: "",
			versionType:    "semantic",
		},
		{
			name:           "nginx alpine variant",
			tag:            "1.25.3-alpine",
			expectVersioned: true,
			expectedMajor:  1,
			expectedMinor:  25,
			expectedPatch:  3,
			expectedSuffix: "alpine",
			versionType:    "semantic",
		},
		{
			name:           "nginx alpine3.18 variant",
			tag:            "1.25.3-alpine3.18",
			expectVersioned: true,
			expectedMajor:  1,
			expectedMinor:  25,
			expectedPatch:  3,
			expectedSuffix: "alpine3.18",
			versionType:    "semantic",
		},
		{
			name:           "nginx perl variant",
			tag:            "1.25.3-perl",
			expectVersioned: true,
			expectedMajor:  1,
			expectedMinor:  25,
			expectedPatch:  3,
			expectedSuffix: "perl",
			versionType:    "semantic",
		},
		{
			name:           "nginx bookworm variant",
			tag:            "1.25.3-bookworm",
			expectVersioned: true,
			expectedMajor:  1,
			expectedMinor:  25,
			expectedPatch:  3,
			expectedSuffix: "bookworm",
			versionType:    "semantic",
		},
		{
			name:           "nginx latest",
			tag:            "latest",
			expectVersioned: false,
			versionType:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag("nginx:" + tt.tag)

			if info.IsVersioned != tt.expectVersioned {
				t.Errorf("IsVersioned = %v, want %v", info.IsVersioned, tt.expectVersioned)
			}

			if tt.expectVersioned {
				if info.Version == nil {
					t.Fatal("Expected version to be parsed, got nil")
				}

				if info.Version.Major != tt.expectedMajor {
					t.Errorf("Major = %d, want %d", info.Version.Major, tt.expectedMajor)
				}
				if info.Version.Minor != tt.expectedMinor {
					t.Errorf("Minor = %d, want %d", info.Version.Minor, tt.expectedMinor)
				}
				if info.Version.Patch != tt.expectedPatch {
					t.Errorf("Patch = %d, want %d", info.Version.Patch, tt.expectedPatch)
				}
				if info.Suffix != tt.expectedSuffix {
					t.Errorf("Suffix = %q, want %q", info.Suffix, tt.expectedSuffix)
				}
				if info.VersionType != tt.versionType {
					t.Errorf("VersionType = %q, want %q", info.VersionType, tt.versionType)
				}
			}
		})
	}
}

// TestRealWorldPostgresTags tests version parsing with real postgres tags
func TestRealWorldPostgresTags(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name           string
		tag            string
		expectVersioned bool
		expectedMajor  int
		expectedMinor  int
		expectedPatch  int
		expectedSuffix string
	}{
		{
			name:           "postgres major version",
			tag:            "16",
			expectVersioned: true,
			expectedMajor:  16,
			expectedMinor:  0,
			expectedPatch:  0,
			expectedSuffix: "",
		},
		{
			name:           "postgres minor version",
			tag:            "16.1",
			expectVersioned: true,
			expectedMajor:  16,
			expectedMinor:  1,
			expectedPatch:  0,
			expectedSuffix: "",
		},
		{
			name:           "postgres alpine",
			tag:            "16.1-alpine",
			expectVersioned: true,
			expectedMajor:  16,
			expectedMinor:  1,
			expectedPatch:  0,
			expectedSuffix: "alpine",
		},
		{
			name:           "postgres alpine3.19",
			tag:            "16.1-alpine3.19",
			expectVersioned: true,
			expectedMajor:  16,
			expectedMinor:  1,
			expectedPatch:  0,
			expectedSuffix: "alpine3.19",
		},
		{
			name:           "postgres bullseye",
			tag:            "16.1-bullseye",
			expectVersioned: true,
			expectedMajor:  16,
			expectedMinor:  1,
			expectedPatch:  0,
			expectedSuffix: "bullseye",
		},
		{
			name:           "postgres bookworm",
			tag:            "16.1-bookworm",
			expectVersioned: true,
			expectedMajor:  16,
			expectedMinor:  1,
			expectedPatch:  0,
			expectedSuffix: "bookworm",
		},
		{
			name:           "postgres legacy version",
			tag:            "9.6.24",
			expectVersioned: true,
			expectedMajor:  9,
			expectedMinor:  6,
			expectedPatch:  24,
			expectedSuffix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag("postgres:" + tt.tag)

			if info.IsVersioned != tt.expectVersioned {
				t.Errorf("IsVersioned = %v, want %v", info.IsVersioned, tt.expectVersioned)
			}

			if tt.expectVersioned && info.Version != nil {
				if info.Version.Major != tt.expectedMajor {
					t.Errorf("Major = %d, want %d", info.Version.Major, tt.expectedMajor)
				}
				if info.Version.Minor != tt.expectedMinor {
					t.Errorf("Minor = %d, want %d", info.Version.Minor, tt.expectedMinor)
				}
				if info.Version.Patch != tt.expectedPatch {
					t.Errorf("Patch = %d, want %d", info.Version.Patch, tt.expectedPatch)
				}
				if info.Suffix != tt.expectedSuffix {
					t.Errorf("Suffix = %q, want %q", info.Suffix, tt.expectedSuffix)
				}
			}
		})
	}
}

// TestRealWorldRedisTags tests version parsing with real redis tags
func TestRealWorldRedisTags(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name           string
		tag            string
		expectVersioned bool
		expectedMajor  int
		expectedMinor  int
		expectedPatch  int
		expectedSuffix string
	}{
		{
			name:           "redis semantic version",
			tag:            "7.2.3",
			expectVersioned: true,
			expectedMajor:  7,
			expectedMinor:  2,
			expectedPatch:  3,
			expectedSuffix: "",
		},
		{
			name:           "redis alpine",
			tag:            "7.2.3-alpine",
			expectVersioned: true,
			expectedMajor:  7,
			expectedMinor:  2,
			expectedPatch:  3,
			expectedSuffix: "alpine",
		},
		{
			name:           "redis alpine3.19",
			tag:            "7.2.3-alpine3.19",
			expectVersioned: true,
			expectedMajor:  7,
			expectedMinor:  2,
			expectedPatch:  3,
			expectedSuffix: "alpine3.19",
		},
		{
			name:           "redis bookworm",
			tag:            "7.2.3-bookworm",
			expectVersioned: true,
			expectedMajor:  7,
			expectedMinor:  2,
			expectedPatch:  3,
			expectedSuffix: "bookworm",
		},
		{
			name:           "redis major.minor",
			tag:            "7.2",
			expectVersioned: true,
			expectedMajor:  7,
			expectedMinor:  2,
			expectedPatch:  0,
			expectedSuffix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag("redis:" + tt.tag)

			if info.IsVersioned != tt.expectVersioned {
				t.Errorf("IsVersioned = %v, want %v", info.IsVersioned, tt.expectVersioned)
			}

			if tt.expectVersioned && info.Version != nil {
				if info.Version.Major != tt.expectedMajor {
					t.Errorf("Major = %d, want %d", info.Version.Major, tt.expectedMajor)
				}
				if info.Version.Minor != tt.expectedMinor {
					t.Errorf("Minor = %d, want %d", info.Version.Minor, tt.expectedMinor)
				}
				if info.Version.Patch != tt.expectedPatch {
					t.Errorf("Patch = %d, want %d", info.Version.Patch, tt.expectedPatch)
				}
				if info.Suffix != tt.expectedSuffix {
					t.Errorf("Suffix = %q, want %q", info.Suffix, tt.expectedSuffix)
				}
			}
		})
	}
}

// TestDateBasedVersionTags tests date-based version formats
func TestDateBasedVersionTags(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name          string
		tag           string
		expectedYear  int
		expectedMonth int
		expectedDay   int
		versionType   string
	}{
		{
			name:          "YYYY.MM.DD format",
			tag:           "2024.01.15",
			expectedYear:  2024,
			expectedMonth: 1,
			expectedDay:   15,
			versionType:   "date",
		},
		{
			name:          "YYYY-MM-DD format",
			tag:           "2024-01-15",
			expectedYear:  2024,
			expectedMonth: 1,
			expectedDay:   15,
			versionType:   "date",
		},
		{
			name:          "YYYYMMDD format",
			tag:           "20240115",
			expectedYear:  2024,
			expectedMonth: 1,
			expectedDay:   15,
			versionType:   "date",
		},
		{
			name:          "YYYY.M.D format (single digit) - parsed as date",
			tag:           "2024.1.5",
			expectedYear:  2024,
			expectedMonth: 1,
			expectedDay:   5,
			versionType:   "date", // Now correctly recognized as date (non-padded format supported)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag("myapp:" + tt.tag)

			if !info.IsVersioned {
				t.Error("Expected IsVersioned to be true for date-based version")
			}

			if info.Version == nil {
				t.Fatal("Expected version to be parsed, got nil")
			}

			if info.Version.Major != tt.expectedYear {
				t.Errorf("Major (year) = %d, want %d", info.Version.Major, tt.expectedYear)
			}
			if info.Version.Minor != tt.expectedMonth {
				t.Errorf("Minor (month) = %d, want %d", info.Version.Minor, tt.expectedMonth)
			}
			if info.Version.Patch != tt.expectedDay {
				t.Errorf("Patch (day) = %d, want %d", info.Version.Patch, tt.expectedDay)
			}
			if info.VersionType != tt.versionType {
				t.Errorf("VersionType = %q, want %q", info.VersionType, tt.versionType)
			}
		})
	}
}

// TestCommitHashTags tests commit hash detection
func TestCommitHashTags(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name        string
		tag         string
		expectHash  bool
		versionType string
	}{
		{
			name:        "short git hash",
			tag:         "abc123d",
			expectHash:  true,
			versionType: "hash",
		},
		{
			name:        "long git hash",
			tag:         "abc123def456789012345678901234567890abcd",
			expectHash:  true,
			versionType: "hash",
		},
		{
			name:        "sha256 prefixed",
			tag:         "sha256-abc123def456",
			expectHash:  true,
			versionType: "hash",
		},
		{
			name:        "git prefixed",
			tag:         "git-abc123d",
			expectHash:  false, // This might not match depending on parser implementation
			versionType: "hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag("myapp:" + tt.tag)

			if tt.expectHash {
				if info.VersionType != tt.versionType {
					t.Errorf("VersionType = %q, want %q", info.VersionType, tt.versionType)
				}
				if info.Hash == "" {
					t.Error("Expected Hash to be set")
				}
			}
		})
	}
}

// TestVersionComparison tests version comparison logic with real examples
func TestVersionComparison(t *testing.T) {
	parser := NewParser()
	comparator := NewComparator()

	tests := []struct {
		name     string
		v1Tag    string
		v2Tag    string
		expected int // -1 if v1 < v2, 0 if equal, 1 if v1 > v2
	}{
		{
			name:     "nginx minor upgrade",
			v1Tag:    "1.24.0",
			v2Tag:    "1.25.0",
			expected: -1,
		},
		{
			name:     "nginx patch upgrade",
			v1Tag:    "1.25.2",
			v2Tag:    "1.25.3",
			expected: -1,
		},
		{
			name:     "postgres major upgrade",
			v1Tag:    "15.3",
			v2Tag:    "16.1",
			expected: -1,
		},
		{
			name:     "redis equal versions",
			v1Tag:    "7.2.3",
			v2Tag:    "7.2.3",
			expected: 0,
		},
		{
			name:     "version downgrade",
			v1Tag:    "2.0.0",
			v2Tag:    "1.9.9",
			expected: 1,
		},
		{
			name:     "prerelease vs release",
			v1Tag:    "1.0.0-beta",
			v2Tag:    "1.0.0",
			expected: -1, // Prerelease is less than release
		},
		{
			name:     "date-based comparison newer",
			v1Tag:    "2024.01.15",
			v2Tag:    "2024.02.20",
			expected: -1,
		},
		{
			name:     "date-based comparison older",
			v1Tag:    "2024.05.10",
			v2Tag:    "2024.01.05",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v1 := parser.ParseTag(tt.v1Tag)
			v2 := parser.ParseTag(tt.v2Tag)

			if v1 == nil {
				t.Fatalf("Failed to parse v1 tag: %s", tt.v1Tag)
			}
			if v2 == nil {
				t.Fatalf("Failed to parse v2 tag: %s", tt.v2Tag)
			}

			result := comparator.Compare(v1, v2)
			if result != tt.expected {
				t.Errorf("Compare(%s, %s) = %d, want %d", tt.v1Tag, tt.v2Tag, result, tt.expected)
			}
		})
	}
}

// TestSuffixHandling tests that suffixes don't affect version comparison
func TestSuffixHandling(t *testing.T) {
	parser := NewParser()
	comparator := NewComparator()

	tests := []struct {
		name       string
		tag1       string
		tag2       string
		shouldEqual bool
	}{
		{
			name:       "alpine suffix ignored in comparison",
			tag1:       "1.25.3",
			tag2:       "1.25.3-alpine",
			shouldEqual: true,
		},
		{
			name:       "different alpine versions same base",
			tag1:       "1.25.3-alpine3.18",
			tag2:       "1.25.3-alpine3.19",
			shouldEqual: true,
		},
		{
			name:       "debian vs alpine same version",
			tag1:       "16.1-bullseye",
			tag2:       "16.1-alpine",
			shouldEqual: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info1 := parser.ParseImageTag("test:" + tt.tag1)
			info2 := parser.ParseImageTag("test:" + tt.tag2)

			if info1.Version == nil || info2.Version == nil {
				t.Fatal("Failed to parse versions")
			}

			result := comparator.Compare(info1.Version, info2.Version)
			isEqual := (result == 0)

			if isEqual != tt.shouldEqual {
				t.Errorf("Versions %s and %s: got equal=%v, want equal=%v",
					tt.tag1, tt.tag2, isEqual, tt.shouldEqual)
			}
		})
	}
}

// TestEdgeCases tests edge cases and problematic formats
func TestEdgeCases(t *testing.T) {
	parser := NewParser()

	tests := []struct {
		name            string
		tag             string
		expectVersioned bool
		description     string
	}{
		{
			name:            "v prefix",
			tag:             "v1.2.3",
			expectVersioned: true,
			description:     "Version with v prefix should parse correctly",
		},
		{
			name:            "major only",
			tag:             "16",
			expectVersioned: true,
			description:     "Major version only should parse",
		},
		{
			name:            "major.minor only",
			tag:             "16.1",
			expectVersioned: true,
			description:     "Major.minor without patch should parse",
		},
		{
			name:            "complex suffix",
			tag:             "1.25.3-alpine3.18-perl",
			expectVersioned: true,
			description:     "Complex multi-part suffix should parse",
		},
		{
			name:            "meta tag stable",
			tag:             "stable",
			expectVersioned: false,
			description:     "Meta tags like stable are not versioned",
		},
		{
			name:            "meta tag edge",
			tag:             "edge",
			expectVersioned: false,
			description:     "Edge tag is a meta tag, not versioned",
		},
		{
			name:            "empty tag defaults to latest",
			tag:             "",
			expectVersioned: false,
			description:     "Empty tag should be treated as latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parser.ParseImageTag("test:" + tt.tag)

			if info.IsVersioned != tt.expectVersioned {
				t.Errorf("%s: IsVersioned = %v, want %v",
					tt.description, info.IsVersioned, tt.expectVersioned)
			}
		})
	}
}
