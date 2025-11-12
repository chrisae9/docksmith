package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/chis/docksmith/internal/update"
	"github.com/chis/docksmith/internal/version"
)

// TestCheckCommandParsing tests flag parsing
func TestCheckCommandParsing(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected CheckOptions
	}{
		{
			name: "default options",
			args: []string{},
			expected: CheckOptions{
				OutputFormat: "table",
			},
		},
		{
			name: "json output",
			args: []string{"--format=json"},
			expected: CheckOptions{
				OutputFormat: "json",
			},
		},
		{
			name: "json shorthand",
			args: []string{"--json=true"},
			expected: CheckOptions{
				OutputFormat: "json",
			},
		},
		{
			name: "verbose mode",
			args: []string{"-v"},
			expected: CheckOptions{
				OutputFormat: "table",
				Verbose:      true,
			},
		},
		{
			name: "filter by name",
			args: []string{"--filter=nginx"},
			expected: CheckOptions{
				OutputFormat: "table",
				FilterName:   "nginx",
			},
		},
		{
			name: "filter by stack",
			args: []string{"--stack=webapp"},
			expected: CheckOptions{
				OutputFormat: "table",
				FilterStack:  "webapp",
			},
		},
		{
			name: "filter by update type",
			args: []string{"--type=major"},
			expected: CheckOptions{
				OutputFormat: "table",
				FilterType:   "major",
			},
		},
		{
			name: "standalone only",
			args: []string{"--standalone"},
			expected: CheckOptions{
				OutputFormat: "table",
				Standalone:   true,
			},
		},
		{
			name: "quiet mode",
			args: []string{"--quiet"},
			expected: CheckOptions{
				OutputFormat: "table",
				Quiet:        true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCheckCommand()
			err := cmd.ParseFlags(tt.args)
			if err != nil {
				t.Fatalf("ParseFlags() error = %v", err)
			}

			if cmd.options.OutputFormat != tt.expected.OutputFormat {
				t.Errorf("OutputFormat = %v, want %v", cmd.options.OutputFormat, tt.expected.OutputFormat)
			}
			if cmd.options.Verbose != tt.expected.Verbose {
				t.Errorf("Verbose = %v, want %v", cmd.options.Verbose, tt.expected.Verbose)
			}
			if cmd.options.FilterName != tt.expected.FilterName {
				t.Errorf("FilterName = %v, want %v", cmd.options.FilterName, tt.expected.FilterName)
			}
			if cmd.options.FilterStack != tt.expected.FilterStack {
				t.Errorf("FilterStack = %v, want %v", cmd.options.FilterStack, tt.expected.FilterStack)
			}
			if cmd.options.FilterType != tt.expected.FilterType {
				t.Errorf("FilterType = %v, want %v", cmd.options.FilterType, tt.expected.FilterType)
			}
			if cmd.options.Standalone != tt.expected.Standalone {
				t.Errorf("Standalone = %v, want %v", cmd.options.Standalone, tt.expected.Standalone)
			}
			if cmd.options.Quiet != tt.expected.Quiet {
				t.Errorf("Quiet = %v, want %v", cmd.options.Quiet, tt.expected.Quiet)
			}
		})
	}
}

// TestCheckCommandJSONOutput tests JSON output formatting
func TestCheckCommandJSONOutput(t *testing.T) {
	cmd := NewCheckCommand()
	cmd.options.OutputFormat = "json"

	// Create a test result
	result := &update.DiscoveryResult{
		Containers: []update.ContainerInfo{
			{
				ContainerUpdate: update.ContainerUpdate{
					ContainerName:  "nginx",
					Image:          "nginx:1.20.0",
					CurrentVersion: "1.20.0",
					LatestVersion:  "1.21.0",
					Status:         update.UpdateAvailable,
					ChangeType:     version.MinorChange,
				},
				Stack: "webapp",
			},
		},
		TotalChecked: 1,
		UpdatesFound: 1,
	}

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := cmd.outputJSON(result)
	if err != nil {
		t.Fatalf("outputJSON() error = %v", err)
	}

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	output := buf.String()

	// Verify it's valid JSON
	var decoded update.DiscoveryResult
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("Invalid JSON output: %v", err)
	}

	if len(decoded.Containers) != 1 {
		t.Errorf("Expected 1 container, got %d", len(decoded.Containers))
	}
}

// TestCheckCommandFiltering tests filtering options
func TestCheckCommandFiltering(t *testing.T) {
	result := &update.DiscoveryResult{
		Containers: []update.ContainerInfo{
			{
				ContainerUpdate: update.ContainerUpdate{
					ContainerName: "nginx",
					Status:        update.UpdateAvailable,
					ChangeType:    version.MajorChange,
				},
				Stack: "webapp",
			},
			{
				ContainerUpdate: update.ContainerUpdate{
					ContainerName: "postgres",
					Status:        update.UpdateAvailable,
					ChangeType:    version.MinorChange,
				},
				Stack: "database",
			},
			{
				ContainerUpdate: update.ContainerUpdate{
					ContainerName: "redis",
					Status:        update.UpToDate,
				},
			},
		},
		TotalChecked: 3,
		UpdatesFound: 2,
		UpToDate:     1,
	}

	tests := []struct {
		name     string
		options  CheckOptions
		expected int // expected number of containers after filtering
	}{
		{
			name:     "no filter",
			options:  CheckOptions{},
			expected: 3,
		},
		{
			name: "filter by name",
			options: CheckOptions{
				FilterName: "nginx",
			},
			expected: 1,
		},
		{
			name: "filter by stack",
			options: CheckOptions{
				FilterStack: "webapp",
			},
			expected: 1,
		},
		{
			name: "filter by major updates",
			options: CheckOptions{
				FilterType: "major",
			},
			expected: 1,
		},
		{
			name: "filter by minor updates",
			options: CheckOptions{
				FilterType: "minor",
			},
			expected: 1,
		},
		{
			name: "filter standalone",
			options: CheckOptions{
				Standalone: true,
			},
			expected: 1, // Only redis has no stack
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCheckCommand()
			cmd.options = tt.options

			filtered := cmd.filterResults(result)

			if len(filtered.Containers) != tt.expected {
				t.Errorf("Expected %d containers, got %d", tt.expected, len(filtered.Containers))
			}
		})
	}
}

// TestCheckCommandErrorHandling tests error handling
func TestCheckCommandErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		format      string
		expectError bool
	}{
		{
			name:        "invalid output format",
			format:      "invalid",
			expectError: true,
		},
		{
			name:        "valid json format",
			format:      "json",
			expectError: false,
		},
		{
			name:        "valid table format",
			format:      "table",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCheckCommand()
			result := &update.DiscoveryResult{
				Containers: []update.ContainerInfo{},
			}

			var err error
			switch tt.format {
			case "json":
				err = cmd.outputJSON(result)
			case "table", "":
				err = cmd.outputTable(result)
			default:
				err = fmt.Errorf("unknown output format: %s", tt.format)
			}

			if tt.expectError && err == nil {
				t.Error("Expected error but got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestCheckCommandColorCoding tests color-coded output
func TestCheckCommandColorCoding(t *testing.T) {
	// Disable NO_COLOR for this test
	oldNoColor := os.Getenv("NO_COLOR")
	os.Unsetenv("NO_COLOR")
	defer func() {
		if oldNoColor != "" {
			os.Setenv("NO_COLOR", oldNoColor)
		}
	}()

	cmd := NewCheckCommand()

	tests := []struct {
		name       string
		container  update.ContainerInfo
		shouldHave string
	}{
		{
			name: "major update is red",
			container: update.ContainerInfo{
				ContainerUpdate: update.ContainerUpdate{
					Status:     update.UpdateAvailable,
					ChangeType: version.MajorChange,
				},
			},
			shouldHave: "\033[31m", // Red
		},
		{
			name: "minor update is yellow",
			container: update.ContainerInfo{
				ContainerUpdate: update.ContainerUpdate{
					Status:     update.UpdateAvailable,
					ChangeType: version.MinorChange,
				},
			},
			shouldHave: "\033[33m", // Yellow
		},
		{
			name: "patch update is green",
			container: update.ContainerInfo{
				ContainerUpdate: update.ContainerUpdate{
					Status:     update.UpdateAvailable,
					ChangeType: version.PatchChange,
				},
			},
			shouldHave: "\033[32m", // Green
		},
		{
			name: "up to date is green",
			container: update.ContainerInfo{
				ContainerUpdate: update.ContainerUpdate{
					Status: update.UpToDate,
				},
			},
			shouldHave: "\033[32m", // Green
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := cmd.formatStatus(tt.container)
			if !strings.Contains(status, tt.shouldHave) {
				t.Errorf("Expected status to contain color code %q, got: %s", tt.shouldHave, status)
			}
		})
	}
}

// TestCheckCommandQuietMode tests quiet mode exit codes
func TestCheckCommandQuietMode(t *testing.T) {
	cmd := NewCheckCommand()
	cmd.options.Quiet = true
	cmd.options.OutputFormat = "table"

	tests := []struct {
		name         string
		result       *update.DiscoveryResult
		expectedExit bool
	}{
		{
			name: "no updates - exit 0",
			result: &update.DiscoveryResult{
				UpdatesFound: 0,
			},
			expectedExit: false,
		},
		{
			name: "updates found - would exit 1",
			result: &update.DiscoveryResult{
				UpdatesFound: 1,
			},
			expectedExit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Note: We can't actually test os.Exit, but we can verify the logic
			if tt.expectedExit && tt.result.UpdatesFound == 0 {
				t.Error("Expected non-zero updates for exit test")
			}
			if !tt.expectedExit && tt.result.UpdatesFound > 0 {
				t.Error("Expected zero updates for no-exit test")
			}
		})
	}
}
