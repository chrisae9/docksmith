package update

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/registry"
	"github.com/chis/docksmith/internal/version"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration Test 1: Container Discovery Integration Test
// Tests: Real container discovery → version extraction → label parsing
func TestProductLogicIntegration_ContainerDiscovery(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()

	// Use real Docker client
	dockerClient, err := docker.NewService()
	require.NoError(t, err, "Failed to create Docker client")
	defer dockerClient.Close()

	// Test 1: List all running containers
	containers, err := dockerClient.ListContainers(ctx)
	require.NoError(t, err, "Failed to list containers")
	t.Logf("Found %d running containers", len(containers))

	// Verify we can discover containers
	assert.NotEmpty(t, containers, "Should have at least some running containers")

	// Test 2: Extract version information from containers
	versionExtractor := version.NewExtractor()
	foundVersionedContainers := 0

	for _, container := range containers {
		t.Logf("Checking container: %s (image: %s)", container.Name, container.Image)

		// Extract image info
		imgInfo := versionExtractor.ExtractFromImage(container.Image)
		assert.NotEmpty(t, imgInfo.Registry, "Container %s should have registry", container.Name)
		assert.NotEmpty(t, imgInfo.Repository, "Container %s should have repository", container.Name)

		// Check if version can be extracted from tag
		if imgInfo.Tag != nil && imgInfo.Tag.IsVersioned {
			foundVersionedContainers++
			t.Logf("  → Found versioned tag: %s (type: %s)",
				imgInfo.Tag.Version.String(), imgInfo.Tag.VersionType)
		}

		// Test 3: Try to get current version from image labels
		currentVersion, err := dockerClient.GetImageVersion(ctx, container.Image)
		if err == nil && currentVersion != "" {
			t.Logf("  → Image label version: %s", currentVersion)
		}

		// Test 4: Get image digest
		digest, err := dockerClient.GetImageDigest(ctx, container.Image)
		if err == nil && digest != "" {
			// Verify digest format
			assert.True(t, strings.HasPrefix(digest, "sha256:"),
				"Digest should start with sha256:")
			t.Logf("  → Image digest: %s", digest[:19]+"...")
		}
	}

	t.Logf("Summary: Found %d containers with versioned tags", foundVersionedContainers)
	// We should have at least a few containers with semantic versions
	assert.Greater(t, foundVersionedContainers, 0,
		"Should find at least one container with a versioned tag")
}

// Integration Test 2: Registry Query Integration Test
// Tests: Real registry API calls → fetch available versions → semantic version parsing
func TestProductLogicIntegration_RegistryQuery(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()

	// Create real registry manager
	githubToken := os.Getenv("GITHUB_TOKEN") // Optional but helps with rate limits
	registryManager := registry.NewManager(githubToken)

	// Test with multiple real images
	testCases := []struct {
		name       string
		imageRef   string
		minTags    int
		expectSemver bool
	}{
		{
			name:       "Docker Hub - nginx",
			imageRef:   "docker.io/library/nginx",
			minTags:    10,
			expectSemver: true,
		},
		{
			name:       "GHCR - linuxserver/plex",
			imageRef:   "ghcr.io/linuxserver/plex",
			minTags:    5,
			expectSemver: true,
		},
		{
			name:       "GHCR - linuxserver/sonarr",
			imageRef:   "ghcr.io/linuxserver/sonarr",
			minTags:    5,
			expectSemver: true,
		},
	}

	parser := version.NewParser()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Test 1: List tags from registry
			tags, err := registryManager.ListTags(ctx, tc.imageRef)
			require.NoError(t, err, "Failed to list tags for %s", tc.imageRef)

			t.Logf("Found %d tags for %s", len(tags), tc.imageRef)
			assert.GreaterOrEqual(t, len(tags), tc.minTags,
				"Should have at least %d tags", tc.minTags)

			// Test 2: Parse semantic versions from tags
			var semanticVersions []*version.Version
			for _, tag := range tags {
				// Skip meta tags
				if tag == "latest" || tag == "stable" || tag == "develop" {
					continue
				}

				// Try to parse as versioned tag
				tagInfo := parser.ParseImageTag("dummy:" + tag)
				if tagInfo != nil && tagInfo.IsVersioned && tagInfo.Version != nil {
					semanticVersions = append(semanticVersions, tagInfo.Version)
					if len(semanticVersions) <= 5 {
						t.Logf("  → Parsed version: %s (from tag: %s)",
							tagInfo.Version.String(), tag)
					}
				}
			}

			if tc.expectSemver {
				assert.NotEmpty(t, semanticVersions,
					"Should find semantic versions for %s", tc.imageRef)
				t.Logf("  → Total semantic versions found: %d", len(semanticVersions))
			}

			// Test 3: Verify we can determine latest semantic version
			if len(semanticVersions) > 0 {
				comparator := version.NewComparator()
				latest := semanticVersions[0]

				for _, v := range semanticVersions[1:] {
					if comparator.IsNewer(latest, v) {
						latest = v
					}
				}

				t.Logf("  → Latest semantic version: %s", latest.String())
				assert.NotNil(t, latest, "Should determine latest version")
			}
		})
	}
}

// Integration Test 3: Version Comparison Integration Test
// Tests: Current version → available versions → determine update candidate
func TestProductLogicIntegration_VersionComparison(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	parser := version.NewParser()
	comparator := version.NewComparator()

	testCases := []struct {
		name           string
		currentVersion string
		availableTags  []string
		expectedUpdate string
		shouldUpdate   bool
		changeType     version.ChangeType
	}{
		{
			name:           "Minor update available",
			currentVersion: "1.20.0",
			availableTags:  []string{"1.20.0", "1.20.1", "1.21.0", "1.21.1", "2.0.0-beta"},
			expectedUpdate: "1.21.1",
			shouldUpdate:   true,
			changeType:     version.MinorChange,
		},
		{
			name:           "Patch update available",
			currentVersion: "1.21.0",
			availableTags:  []string{"1.21.0", "1.21.1", "1.21.2"},
			expectedUpdate: "1.21.2",
			shouldUpdate:   true,
			changeType:     version.PatchChange,
		},
		{
			name:           "Major update available",
			currentVersion: "1.21.0",
			availableTags:  []string{"1.21.0", "1.21.1", "2.0.0", "2.1.0"},
			expectedUpdate: "2.1.0",
			shouldUpdate:   true,
			changeType:     version.MajorChange,
		},
		{
			name:           "Already at latest",
			currentVersion: "1.21.2",
			availableTags:  []string{"1.20.0", "1.21.0", "1.21.2"},
			expectedUpdate: "",
			shouldUpdate:   false,
			changeType:     version.NoChange,
		},
		{
			name:           "Skip prerelease when on stable",
			currentVersion: "1.20.0",
			availableTags:  []string{"1.20.0", "1.21.0-beta", "1.21.0-rc1"},
			expectedUpdate: "",
			shouldUpdate:   false,
			changeType:     version.NoChange,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Parse current version
			currentVer := parser.ParseTag(tc.currentVersion)
			require.NotNil(t, currentVer, "Should parse current version")

			// Parse and find latest from available tags
			var latestVer *version.Version
			var latestTag string

			for _, tag := range tc.availableTags {
				tagInfo := parser.ParseImageTag("dummy:" + tag)
				if tagInfo == nil || !tagInfo.IsVersioned || tagInfo.Version == nil {
					continue
				}

				// Skip prerelease if current is stable
				if currentVer.Prerelease == "" && tagInfo.Version.Prerelease != "" {
					continue
				}

				if latestVer == nil || comparator.IsNewer(latestVer, tagInfo.Version) {
					latestVer = tagInfo.Version
					latestTag = tag
				}
			}

			// Test comparison logic
			if tc.shouldUpdate {
				require.NotNil(t, latestVer, "Should find a newer version")
				assert.True(t, comparator.IsNewer(currentVer, latestVer),
					"Latest (%s) should be newer than current (%s)",
					latestVer.String(), currentVer.String())

				assert.Equal(t, tc.expectedUpdate, latestTag,
					"Should identify correct update candidate")

				changeType := comparator.GetChangeType(currentVer, latestVer)
				assert.Equal(t, tc.changeType, changeType,
					"Should correctly identify change type")

				t.Logf("Update available: %s → %s (%s change)",
					tc.currentVersion, latestTag, changeType.String())
			} else {
				if latestVer != nil {
					assert.False(t, comparator.IsNewer(currentVer, latestVer),
						"Should not find newer version when already at latest")
				}
				t.Logf("Already at latest: %s", tc.currentVersion)
			}
		})
	}
}

// Integration Test 4: End-to-End Update Check Workflow
// Tests: Real container → real registry → version discovery → update determination
func TestProductLogicIntegration_EndToEndUpdateCheck(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()

	// Setup real clients
	dockerClient, err := docker.NewService()
	require.NoError(t, err)
	defer dockerClient.Close()

	githubToken := os.Getenv("GITHUB_TOKEN")
	registryManager := registry.NewManager(githubToken)

	// Create checker with real clients
	checker := NewChecker(dockerClient, registryManager, nil)

	t.Log("Starting end-to-end update check...")
	startTime := time.Now()

	// Run the actual product logic
	result, err := checker.CheckForUpdates(ctx)
	require.NoError(t, err, "CheckForUpdates should not fail")

	duration := time.Since(startTime)
	t.Logf("Check completed in %v", duration)

	// Verify result structure
	assert.NotNil(t, result, "Result should not be nil")
	assert.Equal(t, len(result.Updates), result.TotalChecked,
		"TotalChecked should match number of updates")

	// Log summary
	t.Logf("Summary:")
	t.Logf("  Total containers: %d", result.TotalChecked)
	t.Logf("  Updates available: %d", result.UpdatesFound)
	t.Logf("  Up to date: %d", result.UpToDate)
	t.Logf("  Local images: %d", result.LocalImages)
	t.Logf("  Failed checks: %d", result.Failed)

	// Verify we checked at least some containers
	assert.Greater(t, result.TotalChecked, 0, "Should have checked at least one container")

	// Analyze individual update results
	for _, update := range result.Updates {
		t.Logf("Container: %s", update.ContainerName)
		t.Logf("  Image: %s", update.Image)
		t.Logf("  Status: %s", update.Status)

		switch update.Status {
		case UpdateAvailable:
			t.Logf("  Current: %s", update.CurrentVersion)
			t.Logf("  Latest: %s", update.LatestVersion)
			t.Logf("  Change: %s", update.ChangeType.String())

			// Verify update information is complete
			assert.NotEmpty(t, update.LatestVersion,
				"Update available should have latest version")
			assert.NotEqual(t, version.UnknownChange, update.ChangeType,
				"Should determine change type for versioned updates")

		case UpToDate:
			if update.CurrentVersion != "" {
				t.Logf("  Version: %s (up to date)", update.CurrentVersion)
			}

		case LocalImage:
			t.Logf("  (Local image, not checking registry)")

		case CheckFailed:
			t.Logf("  Error: %s", update.Error)

		case MetadataUnavailable:
			t.Logf("  Warning: %s", update.Error)
		}
	}

	// Verify at least one successful check
	successfulChecks := result.UpdatesFound + result.UpToDate + result.LocalImages
	assert.Greater(t, successfulChecks, 0,
		"Should have at least one successful check")
}

// Integration Test 5: Orchestrator Discovery and Check
// Tests: Orchestrator coordination → concurrent checks → stack grouping → dependency analysis
func TestProductLogicIntegration_OrchestratorDiscoveryAndCheck(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()

	// Setup real clients
	dockerClient, err := docker.NewService()
	require.NoError(t, err)
	defer dockerClient.Close()

	githubToken := os.Getenv("GITHUB_TOKEN")
	registryManager := registry.NewManager(githubToken)

	// Create orchestrator
	orchestrator := NewOrchestrator(dockerClient, registryManager)
	orchestrator.SetMaxConcurrency(3) // Test concurrent checking

	t.Log("Starting orchestrator discovery and check...")
	startTime := time.Now()

	// Run full discovery and check
	result, err := orchestrator.DiscoverAndCheck(ctx)
	require.NoError(t, err, "DiscoverAndCheck should not fail")

	duration := time.Since(startTime)
	t.Logf("Discovery and check completed in %v", duration)

	// Verify results
	assert.NotNil(t, result, "Result should not be nil")
	assert.Equal(t, len(result.Containers), result.TotalChecked,
		"Container count should match total checked")

	// Log summary
	t.Logf("Discovery Summary:")
	t.Logf("  Total containers: %d", result.TotalChecked)
	t.Logf("  Updates found: %d", result.UpdatesFound)
	t.Logf("  Up to date: %d", result.UpToDate)
	t.Logf("  Local images: %d", result.LocalImages)
	t.Logf("  Failed: %d", result.Failed)
	t.Logf("  Stacks found: %d", len(result.Stacks))
	t.Logf("  Standalone containers: %d", len(result.StandaloneContainers))

	// Analyze stacks
	if len(result.Stacks) > 0 {
		t.Logf("Stack Analysis:")
		for stackName, stack := range result.Stacks {
			t.Logf("  Stack: %s", stackName)
			t.Logf("    Containers: %d", len(stack.Containers))
			t.Logf("    Has updates: %v", stack.HasUpdates)
			if stack.HasUpdates {
				t.Logf("    Update priority: %s", stack.UpdatePriority)
			}

			// Log containers in stack
			for _, container := range stack.Containers {
				t.Logf("      - %s: %s", container.ContainerName, container.Status)
				if container.Status == UpdateAvailable {
					t.Logf("        %s → %s (%s)",
						container.CurrentVersion,
						container.LatestVersion,
						container.ChangeType.String())
				}
			}
		}
	}

	// Verify update order was computed
	if len(result.UpdateOrder) > 0 {
		t.Logf("Update order (dependency-aware):")
		for i, containerName := range result.UpdateOrder {
			t.Logf("  %d. %s", i+1, containerName)
		}
	}

	// Verify we found at least some containers
	assert.Greater(t, result.TotalChecked, 0,
		"Should discover at least one container")
}

// Integration Test 6: Real Registry API Performance
// Tests: Registry response times → caching effectiveness → concurrent query handling
func TestProductLogicIntegration_RegistryPerformance(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()

	githubToken := os.Getenv("GITHUB_TOKEN")
	registryManager := registry.NewManager(githubToken)

	testImage := "ghcr.io/linuxserver/plex"

	// Test 1: First call (no cache)
	t.Log("Test 1: First call (cold cache)")
	start := time.Now()
	tags1, err := registryManager.ListTags(ctx, testImage)
	duration1 := time.Since(start)
	require.NoError(t, err)
	require.NotEmpty(t, tags1)
	t.Logf("  First call took: %v (found %d tags)", duration1, len(tags1))

	// Test 2: Second call (with cache)
	t.Log("Test 2: Second call (warm cache)")
	start = time.Now()
	tags2, err := registryManager.ListTags(ctx, testImage)
	duration2 := time.Since(start)
	require.NoError(t, err)
	require.NotEmpty(t, tags2)
	t.Logf("  Second call took: %v (found %d tags)", duration2, len(tags2))

	// Cache should make it significantly faster
	assert.Less(t, duration2, duration1,
		"Cached call should be faster than first call")
	t.Logf("  Speedup: %.2fx", float64(duration1)/float64(duration2))

	// Verify results are identical
	assert.Equal(t, len(tags1), len(tags2),
		"Cache should return same number of tags")

	// Test 3: Tags with digests
	t.Log("Test 3: Tags with digests")
	start = time.Now()
	tagDigests, err := registryManager.ListTagsWithDigests(ctx, testImage)
	duration3 := time.Since(start)
	require.NoError(t, err)
	require.NotEmpty(t, tagDigests)
	t.Logf("  ListTagsWithDigests took: %v (found %d tags)",
		duration3, len(tagDigests))

	// Verify digest format
	for tag, digests := range tagDigests {
		if len(digests) > 0 {
			digest := digests[0]
			assert.True(t, strings.HasPrefix(digest, "sha256:"),
				"Digest for tag %s should start with sha256:", tag)
		}
	}

	t.Logf("Performance Summary:")
	t.Logf("  Cold cache: %v", duration1)
	t.Logf("  Warm cache: %v", duration2)
	t.Logf("  With digests: %v", duration3)
}

// Integration Test 7: Semantic Version Discovery Accuracy
// Tests: Real images → version extraction → semantic version validation
func TestProductLogicIntegration_SemanticVersionAccuracy(t *testing.T) {
	if os.Getenv("INTEGRATION_TESTS") != "true" {
		t.Skip("Skipping integration test. Set INTEGRATION_TESTS=true to run.")
	}

	ctx := context.Background()

	dockerClient, err := docker.NewService()
	require.NoError(t, err)
	defer dockerClient.Close()

	githubToken := os.Getenv("GITHUB_TOKEN")
	registryManager := registry.NewManager(githubToken)

	checker := NewChecker(dockerClient, registryManager, nil)

	// Get real container list
	containers, err := dockerClient.ListContainers(ctx)
	require.NoError(t, err)

	// Find containers with semantic versions
	semanticVersionedContainers := 0
	versionedUpdatesFound := 0

	for _, container := range containers {
		update := checker.checkContainer(ctx, container)

		// Check if current version is semantic
		if update.CurrentVersion != "" {
			parser := version.NewParser()
			ver := parser.ParseTag(update.CurrentVersion)

			if ver != nil && ver.Type == "semantic" {
				semanticVersionedContainers++
				t.Logf("Container: %s", container.Name)
				t.Logf("  Current: %s (Major: %d, Minor: %d, Patch: %d)",
					update.CurrentVersion, ver.Major, ver.Minor, ver.Patch)

				if update.Status == UpdateAvailable && update.LatestVersion != "" {
					latestVer := parser.ParseTag(update.LatestVersion)
					if latestVer != nil && latestVer.Type == "semantic" {
						versionedUpdatesFound++
						t.Logf("  Latest: %s (Major: %d, Minor: %d, Patch: %d)",
							update.LatestVersion, latestVer.Major, latestVer.Minor, latestVer.Patch)
						t.Logf("  Change: %s", update.ChangeType.String())

						// Verify change type matches version difference
						if ver.Major != latestVer.Major {
							assert.Equal(t, version.MajorChange, update.ChangeType,
								"Major version change should be detected")
						} else if ver.Minor != latestVer.Minor {
							assert.Equal(t, version.MinorChange, update.ChangeType,
								"Minor version change should be detected")
						} else if ver.Patch != latestVer.Patch {
							assert.Equal(t, version.PatchChange, update.ChangeType,
								"Patch version change should be detected")
						}
					}
				}
			}
		}
	}

	t.Logf("Semantic Version Summary:")
	t.Logf("  Containers with semantic versions: %d", semanticVersionedContainers)
	t.Logf("  Versioned updates found: %d", versionedUpdatesFound)

	// We should find at least some containers with semantic versions
	assert.Greater(t, semanticVersionedContainers, 0,
		"Should find at least one container with semantic version")
}
