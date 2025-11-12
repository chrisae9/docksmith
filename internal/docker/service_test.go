package docker

import (
	"context"
	"testing"

	"github.com/docker/docker/api/types"
)

func TestConvertContainer(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name     string
		input    types.Container
		expected Container
	}{
		{
			name: "basic container conversion",
			input: types.Container{
				ID:      "abc123",
				Names:   []string{"/my-container"},
				Image:   "nginx:latest",
				State:   "running",
				Labels:  map[string]string{"app": "web"},
				Created: 1234567890,
			},
			expected: Container{
				ID:      "abc123",
				Name:    "my-container",
				Image:   "nginx:latest",
				State:   "running",
				Labels:  map[string]string{"app": "web"},
				Created: 1234567890,
			},
		},
		{
			name: "container with no leading slash",
			input: types.Container{
				ID:      "def456",
				Names:   []string{"another-container"},
				Image:   "postgres:14",
				State:   "exited",
				Labels:  map[string]string{},
				Created: 9876543210,
			},
			expected: Container{
				ID:      "def456",
				Name:    "another-container",
				Image:   "postgres:14",
				State:   "exited",
				Labels:  map[string]string{},
				Created: 9876543210,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.convertContainer(tt.input)

			if result.ID != tt.expected.ID {
				t.Errorf("ID: got %v, want %v", result.ID, tt.expected.ID)
			}
			if result.Name != tt.expected.Name {
				t.Errorf("Name: got %v, want %v", result.Name, tt.expected.Name)
			}
			if result.Image != tt.expected.Image {
				t.Errorf("Image: got %v, want %v", result.Image, tt.expected.Image)
			}
			if result.State != tt.expected.State {
				t.Errorf("State: got %v, want %v", result.State, tt.expected.State)
			}
		})
	}
}

// TestPreUpdateCheckLabelParsing tests parsing of docksmith.pre-update-check label
func TestPreUpdateCheckLabelParsing(t *testing.T) {
	tests := []struct {
		name           string
		labels         map[string]string
		expectedScript string
		expectedFound  bool
	}{
		{
			name: "has pre-update check script",
			labels: map[string]string{
				"docksmith.pre-update-check": "/scripts/check-plex.sh",
				"com.docker.compose.project": "media",
			},
			expectedScript: "/scripts/check-plex.sh",
			expectedFound:  true,
		},
		{
			name: "no pre-update check label",
			labels: map[string]string{
				"com.docker.compose.project": "media",
			},
			expectedScript: "",
			expectedFound:  false,
		},
		{
			name: "empty pre-update check label",
			labels: map[string]string{
				"docksmith.pre-update-check": "",
			},
			expectedScript: "",
			expectedFound:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := Container{
				Name:   "test-container",
				Labels: tt.labels,
			}

			script, found := ExtractPreUpdateCheck(container)
			if found != tt.expectedFound {
				t.Errorf("ExtractPreUpdateCheck() found = %v, want %v", found, tt.expectedFound)
			}
			if script != tt.expectedScript {
				t.Errorf("ExtractPreUpdateCheck() script = %v, want %v", script, tt.expectedScript)
			}
		})
	}
}

// TestManualStackDefinition tests manual stack definition functionality
func TestManualStackDefinition(t *testing.T) {
	ctx := context.Background()

	// Create test stack definitions
	defs := []StackDefinition{
		{
			Name: "monitoring",
			Containers: []string{
				"prometheus",
				"grafana",
				"alertmanager",
			},
		},
		{
			Name: "databases",
			Containers: []string{
				"postgres",
				"redis",
			},
		},
	}

	stackManager := NewStackManager()
	for _, def := range defs {
		stackManager.AddManualStack(def)
	}

	// Test container assignment to stacks
	tests := []struct {
		containerName string
		expectedStack string
		expectFound   bool
	}{
		{"prometheus", "monitoring", true},
		{"grafana", "monitoring", true},
		{"postgres", "databases", true},
		{"nginx", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.containerName, func(t *testing.T) {
			stack, found := stackManager.GetContainerStack(ctx, tt.containerName)
			if found != tt.expectFound {
				t.Errorf("GetContainerStack(%s) found = %v, want %v",
					tt.containerName, found, tt.expectFound)
			}
			if stack != tt.expectedStack {
				t.Errorf("GetContainerStack(%s) = %v, want %v",
					tt.containerName, stack, tt.expectedStack)
			}
		})
	}
}

// TestImprovedVersionGrouping tests enhanced version tag pattern detection
func TestImprovedVersionGrouping(t *testing.T) {
	tests := []struct {
		name     string
		tags     []string
		expected map[string][]string // group name -> tags
	}{
		{
			name: "date-based tags",
			tags: []string{
				"2024.01.15",
				"2024.02.01",
				"2023.12.31",
				"latest",
			},
			expected: map[string][]string{
				"date": {"2024.02.01", "2024.01.15", "2023.12.31"},
				"meta": {"latest"},
			},
		},
		{
			name: "commit hash tags",
			tags: []string{
				"abc123def",
				"sha256-1234567890abcdef",
				"v1.2.3",
				"latest",
			},
			expected: map[string][]string{
				"hash":     {"sha256-1234567890abcdef", "abc123def"},
				"semantic": {"v1.2.3"},
				"meta":     {"latest"},
			},
		},
		{
			name: "mixed version formats",
			tags: []string{
				"1.2.3",
				"1.2.3-alpine",
				"2024.01.15",
				"develop",
				"main",
			},
			expected: map[string][]string{
				"semantic": {"1.2.3", "1.2.3-alpine"},
				"date":     {"2024.01.15"},
				"meta":     {"develop", "main"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grouped := GroupTagsByPattern(tt.tags)

			for groupName, expectedTags := range tt.expected {
				group, exists := grouped[groupName]
				if !exists {
					t.Errorf("Expected group %s not found", groupName)
					continue
				}

				if len(group) != len(expectedTags) {
					t.Errorf("Group %s has %d tags, expected %d",
						groupName, len(group), len(expectedTags))
				}
			}
		})
	}
}

// TestGHCRAuthenticationWorkaround tests GHCR authentication fallback mechanisms
func TestGHCRAuthenticationWorkaround(t *testing.T) {
	tests := []struct {
		name          string
		hasToken      bool
		isPublicImage bool
		expectAuth    bool
		expectError   bool
	}{
		{
			name:          "public image with token",
			hasToken:      true,
			isPublicImage: true,
			expectAuth:    true,
			expectError:   false,
		},
		{
			name:          "public image without token",
			hasToken:      false,
			isPublicImage: true,
			expectAuth:    false, // Should fallback to anonymous
			expectError:   false,
		},
		{
			name:          "private image without token",
			hasToken:      false,
			isPublicImage: false,
			expectAuth:    false,
			expectError:   true, // Should fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This would be tested with actual GHCR client
			// For now, we're testing the workaround logic
			shouldAuth, err := DetermineGHCRAuthStrategy(tt.hasToken, tt.isPublicImage)

			if (err != nil) != tt.expectError {
				t.Errorf("DetermineGHCRAuthStrategy() error = %v, expectError %v",
					err, tt.expectError)
			}

			if shouldAuth != tt.expectAuth {
				t.Errorf("DetermineGHCRAuthStrategy() auth = %v, want %v",
					shouldAuth, tt.expectAuth)
			}
		})
	}
}

// TestValidatePreUpdateScript tests validation of pre-update check scripts
func TestValidatePreUpdateScript(t *testing.T) {
	tests := []struct {
		name        string
		scriptPath  string
		expectValid bool
	}{
		{
			name:        "valid absolute path",
			scriptPath:  "/scripts/check.sh",
			expectValid: true,
		},
		{
			name:        "relative path rejected",
			scriptPath:  "./check.sh",
			expectValid: false,
		},
		{
			name:        "empty path",
			scriptPath:  "",
			expectValid: false,
		},
		{
			name:        "command injection attempt",
			scriptPath:  "/scripts/check.sh; rm -rf /",
			expectValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := ValidatePreUpdateScript(tt.scriptPath)
			if valid != tt.expectValid {
				t.Errorf("ValidatePreUpdateScript(%s) = %v, want %v",
					tt.scriptPath, valid, tt.expectValid)
			}
		})
	}
}

// TestCombinedStackGrouping tests combining compose labels with manual definitions
func TestCombinedStackGrouping(t *testing.T) {
	ctx := context.Background()
	stackManager := NewStackManager()

	// Add manual stack definition
	stackManager.AddManualStack(StackDefinition{
		Name:       "monitoring",
		Containers: []string{"prometheus", "grafana"},
	})

	containers := []Container{
		{
			Name: "web-app",
			Labels: map[string]string{
				"com.docker.compose.project": "webapp",
				"com.docker.compose.service": "frontend",
			},
		},
		{
			Name: "prometheus",
			Labels: map[string]string{}, // No compose labels
		},
		{
			Name: "standalone-db",
			Labels: map[string]string{},
		},
	}

	expected := map[string]string{
		"web-app":       "webapp",     // From compose label
		"prometheus":    "monitoring",  // From manual definition
		"standalone-db": "",           // No stack
	}

	for _, container := range containers {
		stack := stackManager.DetermineStack(ctx, container)
		expectedStack := expected[container.Name]

		if stack != expectedStack {
			t.Errorf("DetermineStack(%s) = %v, want %v",
				container.Name, stack, expectedStack)
		}
	}
}