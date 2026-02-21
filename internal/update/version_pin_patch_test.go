package update

import (
	"testing"

	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/version"
)

// TestVersionPinPatch tests that version-pin-patch constrains updates to build/suffix changes only
func TestVersionPinPatch(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "pin patch allows build suffix changes",
			currentVersion: "1.2.3",
			availableTags:  []string{"1.2.3", "1.2.4", "1.3.0", "2.0.0"},
			labels: map[string]string{
				scripts.VersionPinPatchLabel: "true",
			},
			expectedLatest: "1.2.3",
		},
		{
			name:           "pin patch blocks patch bumps",
			currentVersion: "7.2.1",
			availableTags:  []string{"7.2.1", "7.2.2", "7.3.0"},
			labels: map[string]string{
				scripts.VersionPinPatchLabel: "true",
			},
			expectedLatest: "7.2.1",
		},
		{
			name:           "pin patch with no newer versions returns current",
			currentVersion: "20.10.5",
			availableTags:  []string{"20.10.5", "20.10.6", "20.11.0"},
			labels: map[string]string{
				scripts.VersionPinPatchLabel: "true",
			},
			expectedLatest: "20.10.5",
		},
		{
			name:           "without pin patch allows all updates",
			currentVersion: "1.25.0",
			availableTags:  []string{"1.25.0", "1.25.1", "2.0.0"},
			labels:         map[string]string{},
			expectedLatest: "2.0.0",
		},
		{
			name:           "pin patch with linuxserver suffix allows build number changes",
			currentVersion: "1.2.3-ls100",
			availableTags:  []string{"1.2.3-ls100", "1.2.3-ls101", "1.2.3-ls102", "1.2.4-ls1"},
			labels: map[string]string{
				scripts.VersionPinPatchLabel: "true",
			},
			expectedLatest: "1.2.3-ls102",
		},
		{
			name:           "pin patch blocks minor and major upgrades",
			currentVersion: "3.5.1",
			availableTags:  []string{"3.5.1", "3.5.2", "3.6.0", "4.0.0"},
			labels: map[string]string{
				scripts.VersionPinPatchLabel: "true",
			},
			expectedLatest: "3.5.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, "", currentVer, tt.labels, "")

			if result != tt.expectedLatest {
				t.Errorf("Expected latest %s, got %s", tt.expectedLatest, result)
			}
		})
	}
}

// TestVersionPinPatchCombined tests pin-patch combined with other constraints
func TestVersionPinPatchCombined(t *testing.T) {
	parser := version.NewParser()

	tests := []struct {
		name           string
		currentVersion string
		availableTags  []string
		labels         map[string]string
		expectedLatest string
	}{
		{
			name:           "pin-patch + version-min",
			currentVersion: "7.2.1",
			availableTags:  []string{"7.2.0", "7.2.1", "7.2.2", "7.3.0"},
			labels: map[string]string{
				scripts.VersionPinPatchLabel: "true",
				scripts.VersionMinLabel:      "7.2.0",
			},
			expectedLatest: "7.2.1",
		},
		{
			name:           "pin-patch + version-max",
			currentVersion: "1.5.3",
			availableTags:  []string{"1.5.3", "1.5.4", "1.6.0", "2.0.0"},
			labels: map[string]string{
				scripts.VersionPinPatchLabel: "true",
				scripts.VersionMaxLabel:      "1.99",
			},
			expectedLatest: "1.5.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currentVer := parser.ParseTag(tt.currentVersion)
			if currentVer == nil {
				t.Fatalf("Failed to parse current version: %s", tt.currentVersion)
			}

			checker := &Checker{versionParser: parser}
			result := checker.findLatestVersion(tt.availableTags, "", currentVer, tt.labels, "")

			if result != tt.expectedLatest {
				t.Errorf("Expected latest '%s', got '%s'", tt.expectedLatest, result)
			}
		})
	}
}
