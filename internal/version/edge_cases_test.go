package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCase represents a single versioning test scenario
type TestCase struct {
	Image           string            `json:"image"`
	CurrentTag      string            `json:"current_tag"`
	AvailableTags   []string          `json:"available_tags"`
	ExpectedLatest  string            `json:"expected_latest"`
	ExpectedVersion string            `json:"expected_version"`
	Notes           string            `json:"notes"`
	Labels          map[string]string `json:"labels,omitempty"` // Docker labels for testing label-based logic
}

// EdgeCase documents a known edge case
type EdgeCase struct {
	Issue            string `json:"issue"`
	Example          string `json:"example"`
	ExpectedBehavior string `json:"expected_behavior"`
	Pattern          string `json:"pattern,omitempty"`
	RequiresOverride bool   `json:"requires_override,omitempty"`
	Notes            string `json:"notes,omitempty"`
}

// ContainerTestSuite represents all tests for a specific container
type ContainerTestSuite struct {
	Name          string      `json:"name"`
	Description   string      `json:"description"`
	DockerHubURL  string      `json:"docker_hub_url,omitempty"`
	TestCases     []TestCase  `json:"test_cases"`
	EdgeCases     []EdgeCase  `json:"edge_cases,omitempty"`
	OverrideConfig interface{} `json:"override_config,omitempty"`
}

// TestVersionEdgeCases runs all JSON-based edge case tests
func TestVersionEdgeCases(t *testing.T) {
	testDataDir := filepath.Join("testdata")

	// Find all JSON files in testdata (including custom/)
	jsonFiles, err := findJSONFiles(testDataDir)
	if err != nil {
		t.Fatalf("Failed to find JSON test files: %v", err)
	}

	if len(jsonFiles) == 0 {
		t.Skip("No JSON test files found in testdata/")
	}

	t.Logf("Found %d container test suites", len(jsonFiles))

	// Create parser instance
	parser := NewParser()

	// Run tests for each container
	for _, jsonFile := range jsonFiles {
		suite, err := loadTestSuite(jsonFile)
		if err != nil {
			t.Errorf("Failed to load %s: %v", jsonFile, err)
			continue
		}

		// Create subtest for each container
		containerName := strings.TrimSuffix(filepath.Base(jsonFile), ".json")
		t.Run(containerName, func(t *testing.T) {
			runContainerTests(t, suite, parser)
		})
	}
}

// findJSONFiles recursively finds all .json files in a directory
func findJSONFiles(dir string) ([]string, error) {
	var files []string

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(path, ".json") {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

// loadTestSuite loads a container test suite from JSON
func loadTestSuite(path string) (*ContainerTestSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var suite ContainerTestSuite
	if err := json.Unmarshal(data, &suite); err != nil {
		return nil, err
	}

	return &suite, nil
}

// runContainerTests executes all test cases for a container
func runContainerTests(t *testing.T, suite *ContainerTestSuite, parser *Parser) {
	t.Logf("Testing: %s - %s", suite.Name, suite.Description)

	if len(suite.TestCases) == 0 {
		t.Skip("No test cases defined")
	}

	passedCount := 0
	failedCount := 0

	for _, tc := range suite.TestCases {
		testName := tc.Image
		if tc.CurrentTag != "" {
			testName += ":" + tc.CurrentTag
		}

		t.Run(testName, func(t *testing.T) {
			if runSingleTestCase(t, &tc, parser) {
				passedCount++
			} else {
				failedCount++
			}
		})
	}

	// Log edge cases for documentation
	if len(suite.EdgeCases) > 0 {
		t.Logf("Edge cases documented: %d", len(suite.EdgeCases))
		for _, ec := range suite.EdgeCases {
			if ec.RequiresOverride {
				t.Logf("  ⚠️  %s (requires override)", ec.Issue)
			} else {
				t.Logf("  ℹ️  %s", ec.Issue)
			}
		}
	}

	// Summary
	total := passedCount + failedCount
	if total > 0 {
		t.Logf("Results: %d/%d passed", passedCount, total)
	}
}

// runSingleTestCase executes a single test case
func runSingleTestCase(t *testing.T, tc *TestCase, parser *Parser) bool {
	if tc.Notes != "" {
		t.Logf("Test: %s", tc.Notes)
	}

	// Check if this is a digest-only container (requires override)
	if tc.ExpectedVersion == "digest" {
		t.Skipf("Container uses digest-only comparison (requires override)")
		return false
	}

	// Parse current tag
	currentTagInfo := parser.ParseImageTag(tc.Image + ":" + tc.CurrentTag)
	if currentTagInfo.Version == nil {
		t.Errorf("Failed to parse current tag %q: no version found", tc.CurrentTag)
		return false
	}

	// Parse all available tags
	type tagWithInfo struct {
		tag  string
		info *TagInfo
	}
	var parsedTags []tagWithInfo
	for _, tag := range tc.AvailableTags {
		info := parser.ParseImageTag(tc.Image + ":" + tag)
		if info.Version == nil {
			// Some tags might be meta tags or unparseable - that's ok
			t.Logf("  Skipping unparseable tag: %s", tag)
			continue
		}
		parsedTags = append(parsedTags, tagWithInfo{tag: tag, info: info})
	}

	if len(parsedTags) == 0 {
		t.Error("No parseable versions found in available tags")
		return false
	}

	// Find the latest version with matching suffix
	currentSuffix := currentTagInfo.Suffix
	var latestVersion *Version
	var latestTag string

	// Check if major version pinning is enabled
	pinMajor := false
	if tc.Labels != nil {
		if val, ok := tc.Labels["docksmith.version-pin-major"]; ok && val == "true" {
			pinMajor = true
			t.Logf("  Major version pinning enabled (current major: %d)", currentTagInfo.Version.Major)
		}
	}

	for _, parsed := range parsedTags {
		v := parsed.info.Version

		// Skip if current is stable and this is prerelease
		if currentTagInfo.Version.Prerelease == "" && v.Prerelease != "" {
			t.Logf("  Skipping prerelease: %s", parsed.tag)
			continue
		}

		// Apply major version pinning if enabled
		if pinMajor && v.Major != currentTagInfo.Version.Major {
			t.Logf("  Skipping different major version: %s (major: %d)", parsed.tag, v.Major)
			continue
		}

		// Check suffix compatibility
		// If current has a suffix, prefer tags with matching suffixes
		if currentSuffix != "" {
			if parsed.info.Suffix == "" {
				// Skip tags without suffix if current has one
				t.Logf("  Skipping no-suffix tag (prefer suffix): %s", parsed.tag)
				continue
			}
			if !suffixesMatch(currentSuffix, parsed.info.Suffix) {
				t.Logf("  Skipping different suffix: %s (suffix: %s)", parsed.tag, parsed.info.Suffix)
				continue
			}
		}

		// Track latest
		if latestVersion == nil || parser.CompareVersions(v, latestVersion) > 0 {
			latestVersion = v
			latestTag = parsed.tag
		}
	}

	if latestVersion == nil {
		t.Error("No compatible version found")
		return false
	}

	// Check if we found the expected latest tag
	if latestTag != tc.ExpectedLatest {
		t.Errorf("Latest tag mismatch:\n  Expected: %s\n  Got:      %s",
			tc.ExpectedLatest, latestTag)
		return false
	}

	// Check if the version was parsed correctly
	expectedTagInfo := parser.ParseImageTag(tc.Image + ":" + tc.ExpectedVersion)
	if expectedTagInfo.Version == nil {
		// Expected version might be "digest" or other special value
		if tc.ExpectedVersion != "digest" {
			t.Logf("  Note: Expected version %q could not be parsed", tc.ExpectedVersion)
		}
	} else {
		extractedVersion := latestVersion.String()
		expectedVersionStr := expectedTagInfo.Version.String()

		if extractedVersion != expectedVersionStr {
			t.Errorf("Version extraction mismatch:\n  Expected: %s\n  Got:      %s",
				expectedVersionStr, extractedVersion)
			return false
		}
	}

	t.Logf("  ✓ Correctly identified %s as latest", latestTag)
	return true
}

// suffixesMatch checks if two suffixes are compatible
// This mimics the suffix matching logic from the checker
func suffixesMatch(current, available string) bool {
	// Exact match
	if current == available {
		return true
	}

	// Generic suffix (e.g., "alpine") matches specific (e.g., "alpine3.19")
	if strings.HasPrefix(available, current) {
		return true
	}

	// Check if both belong to the same platform family
	// e.g., "alpine3.18" and "alpine3.19" are both alpine3.x
	// Extract base platform (letters + major version number)
	currentBase := extractPlatformBase(current)
	availableBase := extractPlatformBase(available)

	if currentBase != "" && currentBase == availableBase {
		return true
	}

	// Specific suffix doesn't match different specific suffix
	return false
}

// extractPlatformBase extracts the platform family from a suffix
// Examples: "alpine3.18" -> "alpine3", "alpine3.19" -> "alpine3"
//           "bookworm" -> "bookworm", "bullseye" -> "bullseye"
func extractPlatformBase(suffix string) string {
	if suffix == "" {
		return ""
	}

	// Find the first digit
	for i, ch := range suffix {
		if ch >= '0' && ch <= '9' {
			// Include the digit as part of the base (e.g., alpine3)
			// but don't include the dot or minor version
			base := suffix[:i+1]
			// Check if next char is a dot (e.g., "alpine3." in "alpine3.18")
			if i+1 < len(suffix) && suffix[i+1] == '.' {
				return base // Return "alpine3" from "alpine3.18"
			}
			// No dot after digit, return as-is (e.g., "alpine" from "alpine")
			return base
		}
	}

	// No digits found, return the whole suffix (e.g., "bookworm", "bullseye")
	return suffix
}

// TestEdgeCaseDocumentation generates a report of all edge cases
func TestEdgeCaseDocumentation(t *testing.T) {
	testDataDir := filepath.Join("testdata")
	jsonFiles, err := findJSONFiles(testDataDir)
	if err != nil {
		t.Fatalf("Failed to find JSON test files: %v", err)
	}

	t.Logf("\n=== Edge Case Documentation ===\n")

	totalEdgeCases := 0
	requiresOverride := 0

	for _, jsonFile := range jsonFiles {
		suite, err := loadTestSuite(jsonFile)
		if err != nil {
			continue
		}

		if len(suite.EdgeCases) == 0 {
			continue
		}

		containerName := strings.TrimSuffix(filepath.Base(jsonFile), ".json")
		t.Logf("\n## %s (%s)", suite.Name, containerName)
		t.Logf("Description: %s", suite.Description)

		for _, ec := range suite.EdgeCases {
			totalEdgeCases++

			status := "✓"
			if ec.RequiresOverride {
				status = "⚠️"
				requiresOverride++
			}

			t.Logf("\n  %s **%s**", status, ec.Issue)
			t.Logf("     Example: %s", ec.Example)
			t.Logf("     Expected: %s", ec.ExpectedBehavior)

			if ec.Pattern != "" {
				t.Logf("     Pattern: %s", ec.Pattern)
			}
			if ec.Notes != "" {
				t.Logf("     Notes: %s", ec.Notes)
			}
		}
	}

	t.Logf("\n=== Summary ===")
	t.Logf("Total edge cases documented: %d", totalEdgeCases)
	t.Logf("Require override/special handling: %d", requiresOverride)
	t.Logf("Handled by standard parser: %d", totalEdgeCases-requiresOverride)
}

// TestParserCoverage checks what percentage of documented edge cases pass
func TestParserCoverage(t *testing.T) {
	testDataDir := filepath.Join("testdata")
	jsonFiles, err := findJSONFiles(testDataDir)
	if err != nil {
		t.Fatalf("Failed to find JSON test files: %v", err)
	}

	parser := NewParser()
	totalTests := 0
	passedTests := 0
	failedContainers := []string{}

	for _, jsonFile := range jsonFiles {
		suite, err := loadTestSuite(jsonFile)
		if err != nil {
			continue
		}

		containerName := strings.TrimSuffix(filepath.Base(jsonFile), ".json")
		containerPassed := true

		for _, tc := range suite.TestCases {
			totalTests++

			// Quick validation without logging
			currentTagInfo := parser.ParseImageTag(tc.Image + ":" + tc.CurrentTag)
			if currentTagInfo.Version == nil {
				failedContainers = append(failedContainers, containerName)
				containerPassed = false
				continue
			}

			var latestVersion *Version
			var latestTag string

			// Check if major version pinning is enabled
			pinMajor := false
			if tc.Labels != nil {
				if val, ok := tc.Labels["docksmith.version-pin-major"]; ok && val == "true" {
					pinMajor = true
				}
			}

			for _, tag := range tc.AvailableTags {
				info := parser.ParseImageTag(tc.Image + ":" + tag)
				if info.Version == nil {
					continue
				}

				if currentTagInfo.Version.Prerelease == "" && info.Version.Prerelease != "" {
					continue
				}

				// Apply major version pinning if enabled
				if pinMajor && info.Version.Major != currentTagInfo.Version.Major {
					continue
				}

				// Check suffix compatibility
				if currentTagInfo.Suffix != "" {
					if info.Suffix == "" {
						continue // Skip tags without suffix if current has one
					}
					if !suffixesMatch(currentTagInfo.Suffix, info.Suffix) {
						continue
					}
				}

				if latestVersion == nil || parser.CompareVersions(info.Version, latestVersion) > 0 {
					latestVersion = info.Version
					latestTag = tag
				}
			}

			if latestTag == tc.ExpectedLatest {
				passedTests++
			} else {
				if containerPassed {
					failedContainers = append(failedContainers, containerName)
					containerPassed = false
				}
			}
		}
	}

	percentage := 0
	if totalTests > 0 {
		percentage = (passedTests * 100) / totalTests
	}

	t.Logf("\n=== Parser Coverage Report ===")
	t.Logf("Total test cases: %d", totalTests)
	t.Logf("Passed: %d (%d%%)", passedTests, percentage)
	t.Logf("Failed: %d", totalTests-passedTests)

	if len(failedContainers) > 0 {
		t.Logf("\nContainers with failures:")
		for _, name := range failedContainers {
			t.Logf("  - %s", name)
		}
	}

	// We don't fail the test, just report coverage
	if percentage < 80 {
		t.Logf("\n⚠️  Coverage is below 80%% - parser may need enhancements")
	} else {
		t.Logf("\n✓ Good parser coverage!")
	}
}
