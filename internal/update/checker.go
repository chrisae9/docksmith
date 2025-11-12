package update

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/version"
)

// RegistryClient defines the interface for registry operations
type RegistryClient interface {
	ListTags(ctx context.Context, imageRef string) ([]string, error)
	GetTagDigest(ctx context.Context, imageRef, tag string) (string, error)
	GetLatestTag(ctx context.Context, imageRef string) (string, error)
	ListTagsWithDigests(ctx context.Context, imageRef string) (map[string][]string, error)
}

// Checker checks for available container updates.
type Checker struct {
	dockerClient    docker.Client
	registryManager RegistryClient
	storage         storage.Storage // Optional - can be nil for memory-only mode
	versionParser   *version.Parser
	versionComp     *version.Comparator
	extractor       *version.Extractor
}

// NewChecker creates a new update checker.
// The storage parameter is optional and can be nil for backwards compatibility (memory-only mode).
func NewChecker(dockerClient docker.Client, registryManager RegistryClient, storage storage.Storage) *Checker {
	return &Checker{
		dockerClient:    dockerClient,
		registryManager: registryManager,
		storage:         storage,
		versionParser:   version.NewParser(),
		versionComp:     version.NewComparator(),
		extractor:       version.NewExtractor(),
	}
}

// CheckForUpdates checks all containers for available updates.
// Returns partial results even on error - the result will never be nil.
func (c *Checker) CheckForUpdates(ctx context.Context) (*CheckResult, error) {
	// Initialize result first - we return this even if listing fails
	result := &CheckResult{
		Updates: make([]ContainerUpdate, 0),
	}

	containers, err := c.dockerClient.ListContainers(ctx)
	if err != nil {
		// Return empty result with error - don't fail completely
		return result, fmt.Errorf("failed to list containers: %w", err)
	}

	result.TotalChecked = len(containers)

	for _, container := range containers {
		// Check context for cancellation
		select {
		case <-ctx.Done():
			// Context cancelled - return what we have so far
			return result, ctx.Err()
		default:
		}

		// Check if container should be ignored
		if c.shouldIgnoreContainer(container) {
			update := ContainerUpdate{
				ContainerName: container.Name,
				Image:         container.Image,
				Status:        Ignored,
			}
			result.Updates = append(result.Updates, update)
			result.Ignored++
			continue
		}

		update := c.checkContainer(ctx, container)
		result.Updates = append(result.Updates, update)

		// Update counters
		switch update.Status {
		case UpdateAvailable:
			result.UpdatesFound++
		case UpToDate:
			result.UpToDate++
		case LocalImage:
			result.LocalImages++
		case CheckFailed, MetadataUnavailable:
			result.Failed++
		}
	}

	// Log check results to history if storage is available
	if c.storage != nil {
		if err := c.logCheckResults(ctx, result.Updates); err != nil {
			// Log error but don't fail the check operation
			log.Printf("Failed to log check results to history: %v", err)
		}
	}

	return result, nil
}

// logCheckResults logs the check results to storage as a batch operation
func (c *Checker) logCheckResults(ctx context.Context, updates []ContainerUpdate) error {
	if len(updates) == 0 {
		return nil
	}

	// Convert ContainerUpdate to CheckHistoryEntry
	entries := make([]storage.CheckHistoryEntry, 0, len(updates))
	for _, update := range updates {
		// Map UpdateStatus to string for storage
		status := c.mapStatusToString(update.Status)

		entry := storage.CheckHistoryEntry{
			ContainerName:  update.ContainerName,
			Image:          update.Image,
			CurrentVersion: update.CurrentVersion,
			LatestVersion:  update.LatestVersion,
			Status:         status,
			Error:          update.Error,
		}

		entries = append(entries, entry)
	}

	// Batch log all entries
	return c.storage.LogCheckBatch(ctx, entries)
}

// mapStatusToString converts UpdateStatus to storage-compatible string
func (c *Checker) mapStatusToString(status UpdateStatus) string {
	switch status {
	case UpdateAvailable:
		return "update_available"
	case UpToDate:
		return "up_to_date"
	case LocalImage:
		return "local_image"
	case Unknown:
		return "unknown"
	case CheckFailed:
		return "failed"
	case MetadataUnavailable:
		return "metadata_unavailable"
	default:
		return "unknown"
	}
}

// shouldIgnoreContainer checks if a container has the docksmith.ignore label set to true
func (c *Checker) shouldIgnoreContainer(container docker.Container) bool {
	// Check for docksmith.ignore label
	if ignoreValue, found := container.Labels["docksmith.ignore"]; found {
		log.Printf("Container %s has docksmith.ignore label: '%s'", container.Name, ignoreValue)
		// Accept "true", "1", "yes" as ignore values
		ignoreValue = strings.ToLower(strings.TrimSpace(ignoreValue))
		shouldIgnore := ignoreValue == "true" || ignoreValue == "1" || ignoreValue == "yes"
		log.Printf("Container %s shouldIgnore: %v", container.Name, shouldIgnore)
		return shouldIgnore
	}
	log.Printf("Container %s has no docksmith.ignore label (labels: %d)", container.Name, len(container.Labels))
	return false
}

// checkContainer checks a single container for updates.
func (c *Checker) checkContainer(ctx context.Context, container docker.Container) ContainerUpdate {
	log.Printf("checkContainer: Starting check for %s (image: %s)", container.Name, container.Image)
	update := ContainerUpdate{
		ContainerName: container.Name,
		Image:         container.Image,
	}

	// Check if container explicitly allows :latest tag
	allowLatest := false
	if allowValue, found := container.Labels["docksmith.allow-latest"]; found {
		allowValue = strings.ToLower(strings.TrimSpace(allowValue))
		allowLatest = allowValue == "true" || allowValue == "1" || allowValue == "yes"
		if allowLatest {
			log.Printf("Container %s has docksmith.allow-latest=true, will not suggest migration", container.Name)
		}
	}

	// Check if local image
	isLocal, err := c.dockerClient.IsLocalImage(ctx, container.Image)
	log.Printf("checkContainer %s: isLocal=%v, err=%v", container.Name, isLocal, err)
	if err == nil && isLocal {
		update.IsLocal = true
		update.Status = LocalImage
		return update
	}

	// Get current version from image labels
	currentVersion := c.getCurrentVersion(ctx, container.Image)
	// Validate that the version is actually a semantic version, not just "latest" or similar
	if currentVersion != "" {
		parsed := c.versionParser.ParseTag(currentVersion)
		if parsed == nil {
			// Label contains non-version text like "latest", ignore it
			log.Printf("checkContainer %s: Ignoring non-semantic version label: '%s'", container.Name, currentVersion)
			currentVersion = ""
		}
	}
	update.CurrentVersion = currentVersion

	// Extract registry info and parse tag for suffix
	imgInfo := c.extractor.ExtractFromImage(container.Image)

	// Store the current tag being used (extract from image string)
	// Format: registry/repository:tag or repository:tag
	if strings.Contains(container.Image, ":") {
		parts := strings.Split(container.Image, ":")
		if len(parts) >= 2 {
			update.CurrentTag = parts[len(parts)-1] // Get the last part after ":"
		}
	}

	// If no version found in labels, try to extract from image tag
	if currentVersion == "" && imgInfo.Tag != nil && imgInfo.Tag.IsVersioned && imgInfo.Tag.Version != nil {
		// Use the version from the tag (e.g., nginx:1.29.3 -> 1.29.3)
		currentVersion = imgInfo.Tag.Version.String()
		update.CurrentVersion = currentVersion
	}

	// Get current image digest for SHA-based fallback
	currentDigest, digestErr := c.dockerClient.GetImageDigest(ctx, container.Image)
	if digestErr != nil {
		log.Printf("checkContainer %s: Failed to get digest: %v", container.Name, digestErr)
	} else {
		log.Printf("checkContainer %s: Got digest: %s", container.Name, currentDigest[:min(12, len(currentDigest))])
	}
	update.CurrentDigest = currentDigest

	// If no current version found (e.g., using :latest tag), try to resolve from digest
	if currentVersion == "" && currentDigest != "" {
		log.Printf("checkContainer %s: No current version, attempting digest resolution", container.Name)
		imageRef := imgInfo.Registry + "/" + imgInfo.Repository

		resolvedVersion := c.resolveVersionFromDigest(ctx, imageRef, currentDigest)
		if resolvedVersion != "" {
			log.Printf("checkContainer %s: Resolved version from digest: %s", container.Name, resolvedVersion)
			currentVersion = resolvedVersion
			update.CurrentVersion = currentVersion
		} else {
			log.Printf("checkContainer %s: Could not resolve version from digest", container.Name)
		}
	}

	imageRef := imgInfo.Registry + "/" + imgInfo.Repository

	// Extract suffix and track which tag to check (for SHA fallback)
	currentSuffix := ""
	checkTag := "latest" // Default to latest for SHA comparison
	if imgInfo.Tag != nil {
		currentSuffix = imgInfo.Tag.Suffix
		update.UsingLatestTag = imgInfo.Tag.IsLatest // Track if using :latest
		if !imgInfo.Tag.IsLatest {
			// If not using :latest, extract the tag name
			checkTag = strings.TrimPrefix(imgInfo.Tag.Full, imgInfo.Registry+"/"+imgInfo.Repository+":")
		}
	}
	update.CurrentSuffix = currentSuffix

	// Query registry for available tags
	log.Printf("checkContainer %s: Querying registry for tags at %s", container.Name, imageRef)
	tags, err := c.registryManager.ListTags(ctx, imageRef)
	if err != nil {
		log.Printf("checkContainer %s: ListTags error: %v", container.Name, err)
		// Check if this is a registry metadata error (not a critical failure)
		if c.isRegistryMetadataError(err) {
			update.Status = MetadataUnavailable
			update.Error = "Version information unavailable (registry lookup failed)"
			return update
		}
		// Real failure - couldn't contact registry or image doesn't exist
		update.Status = CheckFailed
		update.Error = err.Error()
		return update
	}

	update.AvailableTags = tags
	log.Printf("Container %s: Got %d available tags from registry", container.Name, len(tags))

	// Parse current version to check if it's stable (for prerelease filtering)
	currentVer := c.versionParser.ParseTag(currentVersion)

	// Track if we've determined status via digest comparison (to skip version comparison)
	digestCheckComplete := false

	// Special case: If tracking a non-semantic tag (like :latest) and we have a digest,
	// use digest comparison as the primary check, not fallback
	if checkTag == "latest" || checkTag == "stable" || checkTag == "main" {
		if currentDigest != "" {
			log.Printf("Container %s: Using :latest tag, checking digest first", container.Name)
			// Query registry for the digest of the tag we're tracking
			latestDigest, err := c.registryManager.GetTagDigest(ctx, imageRef, checkTag)
			if err == nil {
				update.LatestDigest = latestDigest

				// Compare digests (normalize format)
				currentSHA := strings.TrimPrefix(currentDigest, "sha256:")
				latestSHA := strings.TrimPrefix(latestDigest, "sha256:")

				if currentSHA != latestSHA {
					update.Status = UpdateAvailable
					update.ChangeType = version.UnknownChange

					// Try to resolve the semantic version tag for the latest digest
					log.Printf("Resolving semver for %s latest digest: %s", container.Name, latestDigest)
					semverTag := c.resolveVersionFromDigest(ctx, imageRef, latestDigest)
					if semverTag != "" {
						log.Printf("Found semver tag for %s: %s", container.Name, semverTag)
						// Found a semantic version tag for the latest digest
						update.LatestVersion = semverTag

						// Compare versions to determine change type
						if currentVersion != "" {
							currentVer := c.versionParser.ParseTag(currentVersion)
							latestVer := c.versionParser.ParseTag(semverTag)
							log.Printf("Container %s: Parsed current='%s' -> %v, latest='%s' -> %v", container.Name, currentVersion, currentVer, semverTag, latestVer)
							if currentVer != nil && latestVer != nil {
								changeType := c.versionComp.GetChangeType(currentVer, latestVer)
								log.Printf("Container %s: ChangeType from %s to %s = %s", container.Name, currentVersion, semverTag, changeType)
								update.ChangeType = changeType
							} else {
								log.Printf("Container %s: Failed to parse versions for change type", container.Name)
							}
						}
					} else {
						log.Printf("Could not resolve semver for %s, falling back to tag: %s", container.Name, checkTag)
						// Couldn't find semantic version, fall back to tag name
						update.LatestVersion = fmt.Sprintf("(newer image available, tag: %s)", checkTag)
					}
					// Mark digest check complete
					digestCheckComplete = true
				} else {
					// Digests match - we're up to date with :latest
					update.Status = UpToDate
					log.Printf("Container %s: Digests match, marking as UpToDate", container.Name)
					// Find semantic version tag that points to the SAME digest
					// This ensures we only recommend tag migration, not an actual update
					log.Printf("Container %s: Finding semver tag for same digest", container.Name)
					semverTag := c.resolveVersionFromDigest(ctx, imageRef, currentDigest)
					if semverTag != "" {
						update.LatestVersion = semverTag
						log.Printf("Container %s: Found semver tag %s for current digest", container.Name, semverTag)
					} else if currentVersion != "" {
						update.LatestVersion = currentVersion
						log.Printf("Container %s: No semver tag found for digest, using current version", container.Name)
					}
				}
				// If using :latest and up to date, mark as pinnable (unless explicitly allowed)
				// But only if we found an actual semantic version to recommend
				if update.Status == UpToDate && checkTag == "latest" && !allowLatest {
					// Check if we have a real semantic version TAG to recommend (not just "latest")
					// We check LatestVersion exists and isn't "latest" - we don't care if the VERSION
					// number is the same, we want to migrate from :latest TAG to a specific semver TAG
					if update.LatestVersion != "" && update.LatestVersion != "latest" {
						update.Status = UpToDatePinnable
						update.RecommendedTag = update.LatestVersion
						log.Printf("Container %s: Marked as pinnable with recommendation: %s", container.Name, update.RecommendedTag)
					} else {
						log.Printf("Container %s: Using :latest but no semver tags available for migration", container.Name)
						// Keep as UpToDate - nothing to migrate to
					}
				}
				// Mark that we've completed digest check
				// This will skip the semantic version comparison below
				digestCheckComplete = true
				log.Printf("Container %s: Digest comparison complete, will skip semantic version comparison", container.Name)
			} else {
				// Digest lookup failed
				log.Printf("Container %s: Digest lookup failed: %v", container.Name, err)
				if c.isRegistryMetadataError(err) {
					update.Status = MetadataUnavailable
					update.Error = "Version information unavailable (digest lookup failed)"
					return update
				}
				// Fall through to semantic version comparison
			}
		}
	}

	// Find the latest version from tags (filtered by suffix)
	// We always do this to populate LatestVersion for display/recommendation purposes
	log.Printf("Container %s: Calling findLatestVersion with suffix='%s', currentVer=%v", container.Name, currentSuffix, currentVer)
	latestVersion := c.findLatestVersion(tags, currentSuffix, currentVer)
	log.Printf("Container %s: findLatestVersion returned: '%s'", container.Name, latestVersion)

	// Update LatestVersion if we don't already have one from digest resolution
	if update.LatestVersion == "" {
		update.LatestVersion = latestVersion
	}

	// Only do semantic version comparison if we haven't already determined status via digest check
	if !digestCheckComplete {
		// Compare versions if we have both
		if currentVersion != "" && latestVersion != "" {
		// currentVer already parsed above
		latestVer := c.versionParser.ParseTag(latestVersion)

		if currentVer != nil && latestVer != nil {
			changeType := c.versionComp.GetChangeType(currentVer, latestVer)
			update.ChangeType = changeType

			// IsNewer(v1, v2) returns true if v2 is newer than v1
			// So we check IsNewer(current, latest) to see if latest is newer
			if c.versionComp.IsNewer(currentVer, latestVer) {
				update.Status = UpdateAvailable
			} else {
				update.Status = UpToDate
			}
		} else {
			// Can't parse versions, but have version strings to compare
			if currentVersion != latestVersion {
				update.Status = UpdateAvailable
				update.ChangeType = version.UnknownChange
			} else {
				update.Status = UpToDate
				update.ChangeType = version.NoChange
			}
		}
	} else if latestVersion != "" {
		// We have latest but not current - show as available
		update.Status = UpdateAvailable
		update.ChangeType = version.UnknownChange
	} else {
		update.Status = Unknown
	}

	// SHA-based fallback: If no versioned update found (or status is UNKNOWN),
	// compare digests with the tag being tracked (usually :latest)
	if update.Status == Unknown || (update.Status == UpToDate && latestVersion == "") {
		if currentDigest != "" {
			// Query registry for the digest of the tag we're tracking
			latestDigest, err := c.registryManager.GetTagDigest(ctx, imageRef, checkTag)
			if err == nil {
				update.LatestDigest = latestDigest

				// Compare digests (normalize format)
				currentSHA := strings.TrimPrefix(currentDigest, "sha256:")
				latestSHA := strings.TrimPrefix(latestDigest, "sha256:")

				if currentSHA != latestSHA {
					update.Status = UpdateAvailable
					update.ChangeType = version.UnknownChange

					// Try to resolve the semantic version tag for the latest digest
					log.Printf("Resolving semver for %s latest digest: %s", container.Name, latestDigest)
					semverTag := c.resolveVersionFromDigest(ctx, imageRef, latestDigest)
					if semverTag != "" {
						log.Printf("Found semver tag for %s: %s", container.Name, semverTag)
						// Found a semantic version tag for the latest digest
						update.LatestVersion = semverTag

						// Compare versions to determine change type
						if currentVersion != "" {
							currentVer := c.versionParser.ParseTag(currentVersion)
							latestVer := c.versionParser.ParseTag(semverTag)
							log.Printf("Container %s: Parsed current='%s' -> %v, latest='%s' -> %v", container.Name, currentVersion, currentVer, semverTag, latestVer)
							if currentVer != nil && latestVer != nil {
								changeType := c.versionComp.GetChangeType(currentVer, latestVer)
								log.Printf("Container %s: ChangeType from %s to %s = %s", container.Name, currentVersion, semverTag, changeType)
								update.ChangeType = changeType
							} else {
								log.Printf("Container %s: Failed to parse versions for change type", container.Name)
							}
						}
					} else {
						log.Printf("Could not resolve semver for %s, falling back to tag: %s", container.Name, checkTag)
						// Couldn't find semantic version, fall back to tag name
						update.LatestVersion = fmt.Sprintf("(newer image available, tag: %s)", checkTag)
					}
				} else {
					update.Status = UpToDate
				}
			} else {
				// SHA fallback failed - check if it's a metadata error
				if c.isRegistryMetadataError(err) {
					// Registry metadata unavailable, but container might be healthy
					// If we got this far with no version comparison, assume up-to-date with warning
					if update.Status == Unknown {
						update.Status = MetadataUnavailable
						update.Error = "Version information unavailable (digest lookup failed)"
					}
					// If status is UpToDate from earlier checks, keep it and just add a note
					// Don't change status to error
				} else {
					// Real error - provide diagnostic info
					if update.Status == Unknown {
						update.Status = CheckFailed
						update.Error = fmt.Sprintf("cannot determine update status: no semantic versions found and digest check failed (%v)", err)
					}
				}
			}
		} else {
			// No digest available for comparison
			if update.Status == Unknown {
				update.Status = MetadataUnavailable
				update.Error = "Version information unavailable (no digest available)"
			}
		}
	}
	} // End of !digestCheckComplete block

	// If using :latest tag and we resolved a semantic version, mark as pinnable (unless explicitly allowed)
	// But only if we have an actual semantic version to recommend
	if update.UsingLatestTag && update.CurrentVersion != "" && update.Status == UpToDate && !allowLatest {
		// Check if we have a real semantic version TAG to recommend (not just "latest")
		// We check LatestVersion exists and isn't "latest" - we don't care if the VERSION
		// number is the same, we want to migrate from :latest TAG to a specific semver TAG
		if update.LatestVersion != "" && update.LatestVersion != "latest" {
			// Change status to indicate it should be pinned to semver
			update.Status = UpToDatePinnable
			update.RecommendedTag = update.LatestVersion
			log.Printf("Container %s: Marked as pinnable with recommendation: %s", container.Name, update.RecommendedTag)
		} else {
			log.Printf("Container %s: Using :latest but no semver tags available for migration", container.Name)
			// Keep as UpToDate - nothing to migrate to
		}
	}

	// Run pre-update check if configured and update is available or migration to semver is recommended
	// (because changing from :latest to :v1.2.3 will trigger an update operation)
	log.Printf("Container %s: Checking pre-update conditions - status=%s", container.Name, update.Status)
	if update.Status == UpdateAvailable || update.Status == UpdateAvailableBlocked || update.Status == UpToDatePinnable {
		if checkScript, found := container.Labels["docksmith.pre-update-check"]; found && checkScript != "" {
			update.PreUpdateCheck = checkScript
			log.Printf("Container %s: Running pre-update check: %s", container.Name, checkScript)

			success, reason := c.runPreUpdateCheck(ctx, checkScript, container.Name)
			if !success {
				log.Printf("Container %s: Pre-update check failed: %s", container.Name, reason)
				// Mark as blocked since any operation (update or semver migration) would trigger a container update
				update.Status = UpdateAvailableBlocked
				update.PreUpdateCheckFail = reason
			} else {
				log.Printf("Container %s: Pre-update check passed", container.Name)
			}
		}
	}

	return update
}

// isRegistryMetadataError checks if an error is a registry metadata lookup failure
// (like 404 on old SHAs) rather than a critical failure
func (c *Checker) isRegistryMetadataError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Check for 404 errors (object not found in registry)
	if strings.Contains(errStr, "404") || strings.Contains(errStr, "not found") {
		return true
	}

	// Check for manifest not found errors
	if strings.Contains(errStr, "manifest unknown") || strings.Contains(errStr, "manifest invalid") {
		return true
	}

	// Check for "no digest found" errors (from header responses)
	if strings.Contains(errStr, "no digest found") {
		return true
	}

	return false
}

// runPreUpdateCheck executes a pre-update check script and returns success status and reason.
// The script should exit 0 for success (safe to update) and non-zero for failure (blocked).
// Output from the script (stdout/stderr) is captured and returned as the reason.
func (c *Checker) runPreUpdateCheck(ctx context.Context, scriptPath, containerName string) (bool, string) {
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", scriptPath)

	// Set environment variables for the script
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("CONTAINER_NAME=%s", containerName),
	)

	// Capture both stdout and stderr
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	// Run the script
	err := cmd.Run()

	// Combine stdout and stderr for the reason message
	output := strings.TrimSpace(outBuf.String() + "\n" + errBuf.String())
	if output == "" {
		if err != nil {
			output = err.Error()
		} else {
			output = "Check passed"
		}
	}

	// Exit code 0 = success, anything else = blocked
	if err != nil {
		// Check if it's an exit error
		if _, ok := err.(*exec.ExitError); ok {
			// Script ran but returned non-zero - use its output directly
			return false, output
		}
		// Other error (script not found, permission denied, etc.)
		return false, fmt.Sprintf("Script error: %s", err.Error())
	}

	return true, output
}

// getCurrentVersion extracts the current version from image labels.
func (c *Checker) getCurrentVersion(ctx context.Context, imageName string) string {
	version, err := c.dockerClient.GetImageVersion(ctx, imageName)
	if err != nil {
		return ""
	}
	return version
}

// findLatestVersion finds the newest semantic version from a list of tags.
// Only considers tags that match the given suffix (variant filter).
// If currentVersion is stable (no prerelease), skips prerelease versions.
func (c *Checker) findLatestVersion(tags []string, requiredSuffix string, currentVersion *version.Version) string {
	var versions []*version.Version
	versionToTag := make(map[string]string)

	// Check if current version is stable (no prerelease marker)
	isCurrentStable := currentVersion != nil && currentVersion.Prerelease == ""

	log.Printf("findLatestVersion: Looking for tags with suffix='%s', currentVersion=%v, isStable=%v", requiredSuffix, currentVersion, isCurrentStable)

	for _, tag := range tags {
		if tag == "latest" || tag == "stable" || tag == "main" || tag == "develop" {
			continue // Skip non-versioned tags
		}

		// Parse the tag to extract version and suffix
		tagInfo := c.versionParser.ParseImageTag("dummy:" + tag)
		if tagInfo == nil || !tagInfo.IsVersioned || tagInfo.Version == nil {
			continue // Skip tags without semantic versions
		}

		// Filter by suffix - must match exactly
		if tagInfo.Suffix != requiredSuffix {
			log.Printf("  Skipping tag %s: suffix '%s' != required '%s'", tag, tagInfo.Suffix, requiredSuffix)
			continue // Different variant, skip it
		}

		// Skip prerelease versions if current is stable
		if isCurrentStable && tagInfo.Version.Prerelease != "" {
			log.Printf("  Skipping tag %s: prerelease '%s' (current is stable)", tag, tagInfo.Version.Prerelease)
			continue // Don't suggest upgrading from stable to prerelease
		}

		log.Printf("  Accepted tag %s: version=%s, suffix='%s'", tag, tagInfo.Version.String(), tagInfo.Suffix)
		versions = append(versions, tagInfo.Version)
		versionToTag[tagInfo.Version.String()] = tag
	}

	if len(versions) == 0 {
		return ""
	}

	// Sort versions in descending order (newest first)
	sort.Slice(versions, func(i, j int) bool {
		// Compare returns 1 if i > j, so we want that for descending sort
		return c.versionComp.Compare(versions[i], versions[j]) > 0
	})

	latest := versions[0]
	return versionToTag[latest.String()]
}

// resolveVersionFromDigest attempts to find which semantic version tag corresponds
// to the given digest by querying the registry for tag-to-digest mappings.
// Uses cache if storage is available to reduce registry API calls.
func (c *Checker) resolveVersionFromDigest(ctx context.Context, imageRef, currentDigest string) string {
	// Normalize digest format
	currentDigest = strings.TrimPrefix(currentDigest, "sha256:")

	// Check cache first if storage is available
	if c.storage != nil {
		cachedVersion, found, err := c.storage.GetVersionCache(ctx, currentDigest, imageRef, "amd64")
		if err != nil {
			// Log error but continue with registry lookup
			log.Printf("Cache lookup error for %s (%s): %v", imageRef, currentDigest, err)
		} else if found {
			digestDisplay := currentDigest
			if len(digestDisplay) > 12 {
				digestDisplay = digestDisplay[:12]
			}
			log.Printf("Cache hit: %s -> %s", digestDisplay, cachedVersion)
			return cachedVersion
		}
	}

	// Get tag→digest mappings from registry
	tagDigests, err := c.registryManager.ListTagsWithDigests(ctx, imageRef)
	if err != nil {
		// Failed to get mappings, can't resolve
		// But this is not critical - just means we can't resolve version
		log.Printf("Failed to get tag digests for %s: %v", imageRef, err)
		return ""
	}

	log.Printf("Got %d tags with digests for %s, looking for digest %s", len(tagDigests), imageRef, currentDigest[:12])

	// Debug: print first few tag→digest mappings
	if len(tagDigests) > 0 {
		log.Printf("Debug: Printing first 5 tags and their digests...")
		count := 0
		for tag, digests := range tagDigests {
			if count < 5 {
				log.Printf("  Tag '%s' has %d digest(s)", tag, len(digests))
				for _, d := range digests {
					truncated := d
					if len(truncated) > 12 {
						truncated = truncated[:12]
					}
					log.Printf("    - %s", truncated)
				}
				count++
			}
		}
	} else {
		log.Printf("Warning: tagDigests map is empty!")
	}

	// Find semantic version tags that match our current digest
	var matchingVersions []string
	var matchingLatest bool
	for tag, digests := range tagDigests {
		// Check if any of the digests for this tag match our current one
		for _, digest := range digests {
			digest = strings.TrimPrefix(digest, "sha256:")
			if digest == currentDigest {
				// This tag points to our current digest
				// Track if we found :latest
				if tag == "latest" {
					matchingLatest = true
				}
				// Parse it to see if it's a semantic version
				tagInfo := c.versionParser.ParseImageTag("dummy:" + tag)
				if tagInfo != nil && tagInfo.IsVersioned && tagInfo.Version != nil {
					matchingVersions = append(matchingVersions, tag)
				}
				break
			}
		}
	}

	log.Printf("Found %d matching semver tags for digest %s (latest=%v)", len(matchingVersions), currentDigest[:12], matchingLatest)

	// If we found semantic version tags, return the "best" one
	// Prefer the most specific version (e.g., "2.10.2" over "2.10" or "2")
	if len(matchingVersions) > 0 {
		// Parse all matching versions and pick the most specific
		var bestTag string
		var bestVersion *version.Version

		for _, tag := range matchingVersions {
			tagInfo := c.versionParser.ParseImageTag("dummy:" + tag)
			if tagInfo != nil && tagInfo.Version != nil {
				// Prefer versions with higher specificity (patch > minor-only > major-only)
				if bestVersion == nil {
					bestTag = tag
					bestVersion = tagInfo.Version
				} else {
					// Compare specificity: prefer tags with patch numbers
					currentSpecificity := 0
					if tagInfo.Version.Patch > 0 || strings.Contains(tag, ".") {
						currentSpecificity = strings.Count(tag, ".") // Count dots for specificity
					}
					bestSpecificity := 0
					if bestVersion.Patch > 0 || strings.Contains(bestTag, ".") {
						bestSpecificity = strings.Count(bestTag, ".")
					}

					if currentSpecificity > bestSpecificity {
						bestTag = tag
						bestVersion = tagInfo.Version
					}
				}
			}
		}

		resolvedVersion := bestTag
		log.Printf("Selected most specific tag: %s (from %v)", resolvedVersion, matchingVersions)

		// Save to cache if storage is available
		if c.storage != nil {
			err := c.storage.SaveVersionCache(ctx, currentDigest, imageRef, resolvedVersion, "amd64")
			if err != nil {
				// Log error but don't fail the resolution
				log.Printf("Failed to save to cache: %v", err)
			}
		}

		return resolvedVersion
	}

	// If no semantic version found but we matched :latest, return "latest"
	// This allows us to show "latest → vX.Y.Z" even when :latest doesn't have a semantic version tag
	if matchingLatest {
		log.Printf("Digest matches :latest tag (no semantic version tag found)")
		return "latest"
	}

	return ""
}
