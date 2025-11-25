package update

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/version"
)

// TestVersionPinMinor tests that version-pin-minor constrains updates to patch versions only
func TestVersionPinMinor(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "pin minor allows patch updates",
			currentVersion: "1.25.0",
			availableTags:  []string{"1.25.0", "1.25.1", "1.25.2", "1.26.0", "2.0.0"},
			labels: map[string]string{
				scripts.VersionPinMinorLabel: "true",
			},
			expectedLatest: "1.25.2",
		},
		{
			name:           "pin minor blocks minor updates",
			currentVersion: "7.2.0",
			availableTags:  []string{"7.2.0", "7.2.1", "7.2.4", "7.3.0", "7.4.0", "8.0.0"},
			labels: map[string]string{
				scripts.VersionPinMinorLabel: "true",
			},
			expectedLatest: "7.2.4",
		},
		{
			name:           "pin minor with no newer patch returns current",
			currentVersion: "20.10.0",
			availableTags:  []string{"20.10.0", "20.11.0", "21.0.0"},
			labels: map[string]string{
				scripts.VersionPinMinorLabel: "true",
			},
			expectedLatest: "20.10.0",
		},
		{
			name:           "without pin minor allows all updates",
			currentVersion: "1.25.0",
			availableTags:  []string{"1.25.0", "1.25.1", "1.26.0", "2.0.0"},
			labels:         map[string]string{},
			expectedLatest: "2.0.0", // All updates allowed without any pinning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			// Use the findLatestVersion logic directly
			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, "", currentVer, tt.labels)

			if result != tt.expectedLatest {
				t.Errorf("Expected latest %s, got %s", tt.expectedLatest, result)
			}
		})
	}
}

// TestVersionPinMajor tests that version-pin-major constrains updates within major version
func TestVersionPinMajor(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "pin major allows minor and patch updates",
			currentVersion: "20.10.0",
			availableTags:  []string{"20.10.0", "20.11.0", "20.11.1", "21.0.0", "22.0.0"},
			labels: map[string]string{
				scripts.VersionPinMajorLabel: "true",
			},
			expectedLatest: "20.11.1",
		},
		{
			name:           "pin major blocks major updates",
			currentVersion: "7.2.0",
			availableTags:  []string{"7.2.0", "7.4.0", "7.4.1", "8.0.0", "9.0.0"},
			labels: map[string]string{
				scripts.VersionPinMajorLabel: "true",
			},
			expectedLatest: "7.4.1",
		},
		{
			name:           "without pin major allows major updates",
			currentVersion: "7.2.0",
			availableTags:  []string{"7.2.0", "7.4.0", "8.0.0"},
			labels:         map[string]string{},
			expectedLatest: "8.0.0", // Major update allowed without pin
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, "", currentVer, tt.labels)

			if result != tt.expectedLatest {
				t.Errorf("Expected latest %s, got %s", tt.expectedLatest, result)
			}
		})
	}
}

// TestVersionMin tests that version-min excludes versions below the minimum
func TestVersionMin(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "version-min filters out old versions",
			currentVersion: "14.0",
			availableTags:  []string{"13.0", "13.5", "14.0", "14.2", "15.0", "16.0"},
			labels: map[string]string{
				scripts.VersionMinLabel: "14.0",
			},
			expectedLatest: "16.0",
		},
		{
			name:           "version-min with exact boundary",
			currentVersion: "7.0",
			availableTags:  []string{"6.2", "7.0", "7.2", "7.4"},
			labels: map[string]string{
				scripts.VersionMinLabel: "7.0",
			},
			expectedLatest: "7.4",
		},
		{
			name:           "version-min excludes all available",
			currentVersion: "5.0",
			availableTags:  []string{"5.0", "5.5", "6.0"},
			labels: map[string]string{
				scripts.VersionMinLabel: "10.0",
			},
			expectedLatest: "", // No versions meet the minimum
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, "", currentVer, tt.labels)

			if result != tt.expectedLatest {
				t.Errorf("Expected latest '%s', got '%s'", tt.expectedLatest, result)
			}
		})
	}
}

// TestVersionMax tests that version-max excludes versions above the maximum
func TestVersionMax(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "version-max caps at specified version",
			currentVersion: "7.0",
			availableTags:  []string{"7.0", "7.2", "7.4", "8.0", "9.0"},
			labels: map[string]string{
				scripts.VersionMaxLabel: "7.99",
			},
			expectedLatest: "7.4",
		},
		{
			name:           "version-max with exact boundary",
			currentVersion: "3.0",
			availableTags:  []string{"3.0", "3.5", "3.9", "4.0", "4.1"},
			labels: map[string]string{
				scripts.VersionMaxLabel: "3.9.99",
			},
			expectedLatest: "3.9",
		},
		{
			name:           "version-max excludes all newer versions",
			currentVersion: "20.0",
			availableTags:  []string{"20.0", "20.10", "20.11", "21.0"},
			labels: map[string]string{
				scripts.VersionMaxLabel: "20.99",
			},
			expectedLatest: "20.11",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, "", currentVer, tt.labels)

			if result != tt.expectedLatest {
				t.Errorf("Expected latest '%s', got '%s'", tt.expectedLatest, result)
			}
		})
	}
}

// TestTagRegex tests that tag-regex filters tags correctly
func TestTagRegex(t *testing.T) {
	tests := []struct {
		name          string
		tags          []string
		regexPattern  string
		expectedTags  []string
		expectError   bool
	}{
		{
			name:         "filter alpine tags",
			tags:         []string{"20.0", "20.0-alpine", "20.1", "20.1-alpine", "20.1-slim"},
			regexPattern: "^[0-9.]+-alpine$",
			expectedTags: []string{"20.0-alpine", "20.1-alpine"},
		},
		{
			name:         "filter LTS tags",
			tags:         []string{"20.0", "20-lts", "20.1-lts", "21.0", "21-lts"},
			regexPattern: "-lts$",
			expectedTags: []string{"20-lts", "20.1-lts", "21-lts"},
		},
		{
			name:         "filter version pattern",
			tags:         []string{"v1.0.0", "1.0.0", "v1.1.0", "latest", "main"},
			regexPattern: "^v?[0-9]+\\.[0-9]+\\.[0-9]+$",
			expectedTags: []string{"v1.0.0", "1.0.0", "v1.1.0"},
		},
		{
			name:         "no matches",
			tags:         []string{"latest", "main", "develop"},
			regexPattern: "^v[0-9]",
			expectedTags: []string{},
		},
		{
			name:         "empty pattern returns all",
			tags:         []string{"1.0", "2.0", "latest"},
			regexPattern: "",
			expectedTags: []string{"1.0", "2.0", "latest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterTagsByRegex(tt.tags, tt.regexPattern)

			if len(result) != len(tt.expectedTags) {
				t.Errorf("Expected %d tags, got %d: %v", len(tt.expectedTags), len(result), result)
				return
			}

			for i, expected := range tt.expectedTags {
				if result[i] != expected {
					t.Errorf("Tag %d: expected '%s', got '%s'", i, expected, result[i])
				}
			}
		})
	}
}

// TestCombinedConstraints tests multiple constraints working together
func TestCombinedConstraints(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "pin-minor + version-max",
			currentVersion: "20.10.0",
			availableTags:  []string{"20.10.0", "20.10.1", "20.10.2", "20.11.0", "21.0.0"},
			labels: map[string]string{
				scripts.VersionPinMinorLabel: "true",
				scripts.VersionMaxLabel:      "20.99",
			},
			expectedLatest: "20.10.2", // Pin-minor restricts to 20.10.x
		},
		{
			name:           "pin-major + version-min",
			currentVersion: "7.0.0",
			availableTags:  []string{"6.5", "7.0", "7.2", "7.4", "8.0"},
			labels: map[string]string{
				scripts.VersionPinMajorLabel: "true",
				scripts.VersionMinLabel:      "7.0",
			},
			expectedLatest: "7.4",
		},
		{
			name:           "version-min + version-max window",
			currentVersion: "14.0",
			availableTags:  []string{"13.0", "14.0", "15.0", "16.0", "17.0"},
			labels: map[string]string{
				scripts.VersionMinLabel: "14.0",
				scripts.VersionMaxLabel: "16.99",
			},
			expectedLatest: "16.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, "", currentVer, tt.labels)

			if result != tt.expectedLatest {
				t.Errorf("Expected latest '%s', got '%s'", tt.expectedLatest, result)
			}
		})
	}
}

// TestRegexValidation tests regex pattern validation
func TestRegexValidation(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		shouldError bool
	}{
		{
			name:        "valid simple pattern",
			pattern:     "^v[0-9]+",
			shouldError: false,
		},
		{
			name:        "valid complex pattern",
			pattern:     "^v?[0-9]+\\.[0-9]+\\.[0-9]+(-alpine)?$",
			shouldError: false,
		},
		{
			name:        "invalid unclosed bracket",
			pattern:     "(invalid[regex",
			shouldError: true,
		},
		{
			name:        "invalid unclosed paren",
			pattern:     "test(pattern",
			shouldError: true,
		},
		{
			name:        "empty pattern is valid",
			pattern:     "",
			shouldError: false,
		},
		{
			name:        "pattern with special chars",
			pattern:     ".*-alpine3\\.19$",
			shouldError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := regexp.Compile(tt.pattern)

			if tt.shouldError && err == nil {
				t.Errorf("Expected error for pattern '%s', but got none", tt.pattern)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for pattern '%s', but got: %v", tt.pattern, err)
			}
		})
	}
}

// TestRegexPatternLength tests that overly long patterns are rejected
func TestRegexPatternLength(t *testing.T) {
	maxLength := 500

	tests := []struct {
		name        string
		patternLen  int
		shouldError bool
	}{
		{
			name:        "pattern at max length",
			patternLen:  maxLength,
			shouldError: false,
		},
		{
			name:        "pattern over max length",
			patternLen:  maxLength + 1,
			shouldError: true,
		},
		{
			name:        "very long pattern",
			patternLen:  1000,
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create pattern of specified length
			pattern := make([]byte, tt.patternLen)
			for i := range pattern {
				pattern[i] = 'a'
			}

			err := validateRegexPattern(string(pattern), maxLength)

			if tt.shouldError && err == nil {
				t.Errorf("Expected error for pattern length %d, but got none", tt.patternLen)
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Expected no error for pattern length %d, but got: %v", tt.patternLen, err)
			}
		})
	}
}

// validateRegexPattern validates a regex pattern for length and syntax
func validateRegexPattern(pattern string, maxLength int) error {
	if len(pattern) > maxLength {
		return fmt.Errorf("pattern too long: %d > %d", len(pattern), maxLength)
	}
	_, err := regexp.Compile(pattern)
	return err
}

// TestSuffixMatchingWithConstraints tests that suffix matching works with version constraints
func TestSuffixMatchingWithConstraints(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		suffix         string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "alpine suffix with pin-major",
			currentVersion: "20.10.0",
			suffix:         "alpine",
			availableTags:  []string{"20.10.0-alpine", "20.11.0-alpine", "21.0.0-alpine", "20.11.0"},
			labels: map[string]string{
				scripts.VersionPinMajorLabel: "true",
			},
			expectedLatest: "20.11.0-alpine",
		},
		{
			name:           "bookworm suffix with version-max",
			currentVersion: "15.0",
			suffix:         "bookworm",
			availableTags:  []string{"15.0-bookworm", "16.0-bookworm", "17.0-bookworm"},
			labels: map[string]string{
				scripts.VersionMaxLabel: "16.99",
			},
			expectedLatest: "16.0-bookworm",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, tt.suffix, currentVer, tt.labels)

			if result != tt.expectedLatest {
				t.Errorf("Expected latest '%s', got '%s'", tt.expectedLatest, result)
			}
		})
	}
}
