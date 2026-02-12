package update

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/storage"
	"gopkg.in/yaml.v3"
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
	case Ignored:
		return "ignored"
	default:
		return "unknown"
	}
}

// shouldIgnoreContainer checks if a container should be ignored from update checks.
// Only checks container labels (source of truth from compose file).
func (c *Checker) shouldIgnoreContainer(container docker.Container) bool {
	if ignoreValue, ok := container.Labels[scripts.IgnoreLabel]; ok {
		ignore := ignoreValue == "true" || ignoreValue == "1" || ignoreValue == "yes"
		if ignore {
			log.Printf("Container %s: Ignore flag set via label", container.Name)
			return true
		}
	}

	return false
}

// checkComposeMismatch checks if the running container's image differs from the compose file specification.
// Detects two scenarios:
// 1. Container lost its tag reference and is running with bare SHA digest (and compose has image: spec)
// 2. Container is running different tag than what compose file specifies
// Returns (true, expectedImage) if there's a mismatch, (false, "") if no mismatch or unable to determine.
func (c *Checker) checkComposeMismatch(container docker.Container) (bool, string) {
	// Check if this is a compose-managed container
	composeFile, ok := container.Labels["com.docker.compose.project.config_files"]
	if !ok || composeFile == "" {
		// Not a compose-managed container
		return false, ""
	}

	// First, try to load the compose file to check what it specifies
	composeFiles := strings.Split(composeFile, ",")
	if len(composeFiles) == 0 {
		return false, ""
	}

	composePath := strings.TrimSpace(composeFiles[0])
	if composePath == "" {
		return false, ""
	}

	// Load and parse the compose file (handles include-based setups)
	composeData, err := compose.LoadComposeFileOrIncluded(composePath, container.Name)
	if err != nil {
		// If we can't read the compose file, we can't determine mismatch
		return false, ""
	}

	// Find the service definition for this container
	service, err := composeData.FindServiceByContainerName(container.Name)
	if err != nil {
		log.Printf("Container %s: Failed to find service in compose file: %v", container.Name, err)
		return false, ""
	}

	// Extract the image specification from the service definition
	if service.Node.Kind != yaml.MappingNode {
		return false, ""
	}

	var imageSpec string
	var hasBuild bool
	for i := 0; i < len(service.Node.Content); i += 2 {
		keyNode := service.Node.Content[i]
		if keyNode.Value == "image" && i+1 < len(service.Node.Content) {
			valueNode := service.Node.Content[i+1]
			imageSpec = valueNode.Value
		}
		if keyNode.Value == "build" {
			hasBuild = true
		}
	}

	// If compose uses build: without image:, this is a local build - not a mismatch
	if hasBuild && imageSpec == "" {
		return false, ""
	}

	if imageSpec == "" {
		// No image spec in compose file
		return false, ""
	}

	// Resolve environment variable syntax (e.g., ${OPENCLAW_IMAGE:-openclaw:latest})
	if compose.ContainsEnvVar(imageSpec) {
		imageSpec = compose.ResolveEnvVars(imageSpec)
		if compose.ContainsEnvVar(imageSpec) {
			// Still has unresolved env vars — can't determine mismatch
			log.Printf("Container %s: Skipping mismatch check - image spec has unresolvable env vars", container.Name)
			return false, ""
		}
	}

	// SCENARIO 1: Check if container lost its tag reference (running with bare SHA)
	// This happens when container.Image is "sha256:..." or just the digest
	if strings.HasPrefix(container.Image, "sha256:") || (!strings.Contains(container.Image, ":") && len(container.Image) == 64) {
		log.Printf("Container %s: Lost tag reference - running with bare SHA digest, compose specifies: %s", container.Name, imageSpec)
		return true, imageSpec
	}

	// Normalize both images for comparison (handle digest vs tag differences)
	// The running container may have a full SHA while compose has a tag
	runningImage := container.Image

	// If running image contains @sha256:, extract the base image name
	if strings.Contains(runningImage, "@sha256:") {
		runningImage = strings.Split(runningImage, "@")[0]
	}

	// If running image has no tag, it's using latest implicitly
	if !strings.Contains(runningImage, ":") {
		runningImage += ":latest"
	}

	// Normalize compose spec too
	normalizedSpec := imageSpec
	if !strings.Contains(normalizedSpec, ":") {
		normalizedSpec += ":latest"
	}

	// Compare the normalized images
	if runningImage != normalizedSpec {
		log.Printf("Container %s: Image mismatch - running: %s, compose: %s", container.Name, container.Image, imageSpec)
		return true, imageSpec
	}

	return false, ""
}

// checkEnvControlled checks if a container's image is controlled by a .env variable.
// If the compose file uses ${VAR:-default} and the .env file defines VAR, sets EnvControlled=true.
func (c *Checker) checkEnvControlled(container docker.Container, update *ContainerUpdate) {
	composeFile, ok := container.Labels["com.docker.compose.project.config_files"]
	if !ok || composeFile == "" {
		return
	}

	composePath := strings.TrimSpace(strings.Split(composeFile, ",")[0])
	if composePath == "" {
		return
	}

	// Load compose file and find this service's image spec
	composeData, err := compose.LoadComposeFileOrIncluded(composePath, container.Name)
	if err != nil {
		return
	}
	// Try by container name first, then by compose service label
	service, err := composeData.FindServiceByContainerName(container.Name)
	if err != nil {
		if serviceName, ok := container.Labels["com.docker.compose.service"]; ok && serviceName != "" {
			service, err = composeData.FindServiceByContainerName(serviceName)
		}
		if err != nil {
			return
		}
	}
	if service.Node.Kind != yaml.MappingNode {
		return
	}

	var imageSpec string
	for i := 0; i < len(service.Node.Content); i += 2 {
		if service.Node.Content[i].Value == "image" && i+1 < len(service.Node.Content) {
			imageSpec = service.Node.Content[i+1].Value
			break
		}
	}

	if !compose.ContainsEnvVar(imageSpec) {
		return
	}

	varName := compose.ExtractEnvVarName(imageSpec)
	if varName == "" {
		return
	}

	// Check if the variable is defined in the .env file
	composeDir := filepath.Dir(composePath)
	dotEnv := compose.LoadDotEnv(composeDir)
	if _, exists := dotEnv[varName]; exists {
		update.EnvControlled = true
		update.EnvVarName = varName
		log.Printf("Container %s: Image controlled by .env variable %s", container.Name, varName)
	}
}

// checkContainer checks a single container for updates.
func (c *Checker) checkContainer(ctx context.Context, container docker.Container) ContainerUpdate {
	log.Printf("checkContainer: Starting check for %s (image: %s)", container.Name, container.Image)
	update := ContainerUpdate{
		ContainerName: container.Name,
		Image:         container.Image,
		HealthStatus:  container.HealthStatus,
	}

	// Check for compose mismatch (running image != compose file specification)
	if mismatch, expectedImage := c.checkComposeMismatch(container); mismatch {
		log.Printf("Container %s: COMPOSE MISMATCH - running %s, compose specifies %s", container.Name, container.Image, expectedImage)
		update.Status = ComposeMismatch
		update.ComposeImage = expectedImage
		update.Error = fmt.Sprintf("Running image (%s) differs from compose specification (%s)", container.Image, expectedImage)
		return update
	}

	// Check if image is controlled by a .env variable
	c.checkEnvControlled(container, &update)

	// Check if container explicitly allows :latest tag (only from labels)
	allowLatest := false
	if allowLatestValue, ok := container.Labels[scripts.AllowLatestLabel]; ok {
		allowLatest = allowLatestValue == "true" || allowLatestValue == "1" || allowLatestValue == "yes"
		if allowLatest {
			log.Printf("Container %s: allow-latest flag set via label", container.Name)
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

	// Get current version - prefer tag version over label version when they disagree
	// This handles cases like caddy:2.11 where the tag is "2.11" but the label says "v2.11.0-beta.1"
	currentVersion := ""

	// First, get version from image labels
	labelVersion := c.getCurrentVersion(ctx, container.Image)
	var labelParsed *version.Version
	if labelVersion != "" {
		labelParsed = c.versionParser.ParseTag(labelVersion)
		if labelParsed == nil {
			// Label contains non-version text like "latest", ignore it
			log.Printf("checkContainer %s: Ignoring non-semantic version label: '%s'", container.Name, labelVersion)
			labelVersion = ""
		}
	}

	// Get version from tag
	var tagVersion string
	var tagParsed *version.Version
	if imgInfo.Tag != nil && imgInfo.Tag.IsVersioned && imgInfo.Tag.Version != nil {
		tagParsed = imgInfo.Tag.Version
		tagVersion = imgInfo.Tag.Version.String()
	}

	// Decide which version to use
	// Prefer tag version when:
	// 1. Label has a prerelease but tag doesn't (e.g., label=v2.11.0-beta.1, tag=2.11)
	// 2. No label version available
	if labelVersion != "" && tagVersion != "" {
		// Both available - check if label is prerelease but tag is not
		if labelParsed != nil && tagParsed != nil {
			if labelParsed.Prerelease != "" && tagParsed.Prerelease == "" {
				// Label has prerelease but tag doesn't - prefer tag
				// This handles cases where the image label is outdated/incorrect (e.g., caddy:2.11)
				log.Printf("checkContainer %s: Label has prerelease (%s) but tag doesn't (%s) - using tag version",
					container.Name, labelVersion, tagVersion)
				currentVersion = tagVersion
			} else {
				// Use label version (typically more accurate for full semver)
				currentVersion = labelVersion
			}
		} else {
			currentVersion = labelVersion
		}
	} else if labelVersion != "" {
		currentVersion = labelVersion
	} else if tagVersion != "" {
		currentVersion = tagVersion
	}

	update.CurrentVersion = currentVersion

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
		if resolvedVersion != "" && resolvedVersion != "latest" {
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

	// For non-versioned, non-meta tags (e.g., "server-cuda"), the label-derived version
	// may come from a base image (e.g., Ubuntu "22.04") rather than the application.
	// Validate by checking if the version appears in any of the registry's actual tags.
	// Meta tags (latest, stable, etc.) are excluded because publishers typically set
	// org.opencontainers.image.version correctly for these.
	if tagVersion == "" && !isMetaTag(checkTag) && currentVersion != "" {
		versionInTags := false
		for _, t := range tags {
			if strings.Contains(t, currentVersion) {
				versionInTags = true
				break
			}
		}
		if !versionInTags {
			log.Printf("checkContainer %s: Label version '%s' not found in registry tags for non-meta tag '%s', likely from base image — clearing", container.Name, currentVersion, checkTag)
			currentVersion = ""
			update.CurrentVersion = ""
		}
	}

	// Parse current version to check if it's stable (for prerelease filtering)
	currentVer := c.versionParser.ParseTag(currentVersion)

	// Track if we've determined status via digest comparison (to skip version comparison)
	digestCheckComplete := false

	// Special case: If tracking a non-semantic tag (like :latest, :stable, :stable-tensorrt) and we have a digest,
	// use digest comparison as the primary check, not fallback
	if isMetaTag(checkTag) {
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
					// Set the tag we're tracking as the "latest version" (what to update to)
					update.LatestVersion = checkTag

					// Try to resolve the semantic version tag for the latest digest
					log.Printf("Resolving semver for %s latest digest: %s (suffix: '%s')", container.Name, latestDigest, currentSuffix)
					semverTag := c.resolveVersionFromDigest(ctx, imageRef, latestDigest, currentSuffix)
					if semverTag != "" && semverTag != "latest" {
						log.Printf("Found semver tag for %s: %s", container.Name, semverTag)
						// Found a semantic version tag for the latest digest - store as resolved version
						update.LatestResolvedVersion = semverTag

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
						log.Printf("Could not resolve semver for %s (tracking %s), no resolved version to display", container.Name, checkTag)
						// Couldn't find semantic version - LatestVersion stays as checkTag, no resolved version
					}
					// Mark digest check complete
					digestCheckComplete = true
				} else {
					// Digests match - we're up to date with :latest
					update.Status = UpToDate
					update.LatestVersion = checkTag // Keep the tag we're tracking
					log.Printf("Container %s: Digests match, marking as UpToDate", container.Name)
					// Find semantic version tag that points to the SAME digest
					// This ensures we only recommend tag migration, not an actual update
					log.Printf("Container %s: Finding semver tag for same digest (suffix: '%s')", container.Name, currentSuffix)
					semverTag := c.resolveVersionFromDigest(ctx, imageRef, currentDigest, currentSuffix)
					if semverTag != "" && semverTag != "latest" {
						update.LatestResolvedVersion = semverTag
						log.Printf("Container %s: Found semver tag %s for current digest", container.Name, semverTag)
					} else {
						log.Printf("Container %s: No semver tag found for digest", container.Name)
					}
				}
				// If using :latest and up to date, mark as pinnable (unless explicitly allowed)
				// But only if we found an actual semantic version to recommend
				if update.Status == UpToDate && checkTag == "latest" && !allowLatest {
					// Check if we have a real semantic version TAG to recommend (not just "latest")
					// We check LatestResolvedVersion exists - this is the actual semver tag to migrate to
					if update.LatestResolvedVersion != "" {
						update.Status = UpToDatePinnable
						update.RecommendedTag = update.LatestResolvedVersion
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
	// Used for semver comparison when not using meta tags
	log.Printf("Container %s: Calling findLatestVersion with suffix='%s', currentVer=%v", container.Name, currentSuffix, currentVer)
	latestVersion := c.findLatestVersion(tags, currentSuffix, currentVer, container.Labels)
	log.Printf("Container %s: findLatestVersion returned: '%s'", container.Name, latestVersion)

	// For non-meta tags, set LatestVersion to the latest semver tag from registry
	// For meta tags, LatestVersion was already set to the meta tag above
	if update.LatestVersion == "" {
		update.LatestVersion = latestVersion
	}

	// Re-check pinnable status using findLatestVersion result
	// This handles the case where resolveVersionFromDigest didn't find a semver tag,
	// but findLatestVersion found one (they could point to the same image)
	if update.Status == UpToDate && checkTag == "latest" && !allowLatest && latestVersion != "" && latestVersion != "latest" {
		if update.LatestResolvedVersion == "" {
			// No resolved version from digest lookup, use findLatestVersion result
			update.LatestResolvedVersion = latestVersion
		}
		update.Status = UpToDatePinnable
		update.RecommendedTag = update.LatestResolvedVersion
		log.Printf("Container %s: Marked as pinnable with recommendation: %s", container.Name, update.RecommendedTag)
	}

	// For meta tag containers with updates, fall back to findLatestVersion for resolved version
	// when digest-to-tag resolution failed (ListTagsWithDigests can return incomplete results)
	if isMetaTag(checkTag) && update.LatestResolvedVersion == "" && latestVersion != "" && latestVersion != "latest" {
		update.LatestResolvedVersion = latestVersion
		log.Printf("Container %s: Using findLatestVersion as resolved version: %s", container.Name, latestVersion)
	}

	// For meta tag containers, prefer findLatestVersion over digest-based resolution
	// when both resolve to the same version — findLatestVersion respects type filtering
	// and avoids picking wrong tag variants (e.g., "v2026.2.9" vs "2026.2.9")
	if isMetaTag(checkTag) && update.LatestResolvedVersion != "" && latestVersion != "" && latestVersion != "latest" && update.LatestResolvedVersion != latestVersion {
		resolvedVer := c.versionParser.ParseTag(update.LatestResolvedVersion)
		findLatestVer := c.versionParser.ParseTag(latestVersion)
		if resolvedVer != nil && findLatestVer != nil &&
			resolvedVer.Major == findLatestVer.Major &&
			resolvedVer.Minor == findLatestVer.Minor &&
			resolvedVer.Patch == findLatestVer.Patch {
			log.Printf("Container %s: Correcting resolved version '%s' -> '%s' (same version, better tag match)",
				container.Name, update.LatestResolvedVersion, latestVersion)
			update.LatestResolvedVersion = latestVersion
			if update.RecommendedTag != "" {
				update.RecommendedTag = latestVersion
			}
		}
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
					// Set the tag we're tracking as the "latest version"
					update.LatestVersion = checkTag

					// Try to resolve the semantic version tag for the latest digest
					log.Printf("Resolving semver for %s latest digest: %s (suffix: '%s')", container.Name, latestDigest, currentSuffix)
					semverTag := c.resolveVersionFromDigest(ctx, imageRef, latestDigest, currentSuffix)
					if semverTag != "" && semverTag != "latest" {
						log.Printf("Found semver tag for %s: %s", container.Name, semverTag)
						// Found a semantic version tag for the latest digest - store as resolved version
						update.LatestResolvedVersion = semverTag

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
						log.Printf("Could not resolve semver for %s (tracking %s), no resolved version to display", container.Name, checkTag)
						// Couldn't find semantic version - no resolved version
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
		// Check if we have a resolved semantic version TAG to recommend
		if update.LatestResolvedVersion != "" {
			// Change status to indicate it should be pinned to semver
			update.Status = UpToDatePinnable
			update.RecommendedTag = update.LatestResolvedVersion
			log.Printf("Container %s: Marked as pinnable with recommendation: %s", container.Name, update.RecommendedTag)
		} else {
			log.Printf("Container %s: Using :latest but no semver tags available for migration", container.Name)
			// Keep as UpToDate - nothing to migrate to
		}
	}

	// Run pre-update check if configured (only from labels)
	log.Printf("Container %s: Checking pre-update conditions - status=%s", container.Name, update.Status)
	checkScript := ""

	if scriptPath, ok := container.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
		log.Printf("Container %s: Pre-update script found in label: %s", container.Name, scriptPath)
		checkScript = scriptPath
	}

	// Run the check if we found a script
	if checkScript != "" {
		update.PreUpdateCheck = checkScript
		log.Printf("Container %s: Running pre-update check: %s", container.Name, checkScript)

		success, reason := c.runPreUpdateCheck(ctx, checkScript, container.Name)
		if !success {
			log.Printf("Container %s: Pre-update check failed: %s", container.Name, reason)
			update.PreUpdateCheckFail = reason
			// Only block if there's actually an update or semver migration available
			if update.Status == UpdateAvailable || update.Status == UpToDatePinnable {
				update.Status = UpdateAvailableBlocked
			}
		} else {
			log.Printf("Container %s: Pre-update check passed", container.Name)
			update.PreUpdateCheckPass = true
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
	// Construct full path if not already absolute
	fullPath := scriptPath
	if !filepath.IsAbs(scriptPath) {
		fullPath = filepath.Join(scripts.ScriptsDir, scriptPath)
	}

	cmd := exec.CommandContext(ctx, fullPath)

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
// Applies custom filters from container labels (regex, min/max versions, minor pinning).
func (c *Checker) findLatestVersion(tags []string, requiredSuffix string, currentVersion *version.Version, labels map[string]string) string {
	// Apply regex filter first (if specified)
	if regexPattern := labels[scripts.TagRegexLabel]; regexPattern != "" {
		tags = filterTagsByRegex(tags, regexPattern)
		log.Printf("findLatestVersion: Applied regex filter '%s', %d tags remain", regexPattern, len(tags))
	}

	var versions []*version.Version
	versionToTag := make(map[string]string)

	// Check if container explicitly allows prerelease versions
	allowPrerelease := labels[scripts.AllowPrereleaseLabel] == "true" ||
		labels[scripts.AllowPrereleaseLabel] == "1" ||
		labels[scripts.AllowPrereleaseLabel] == "yes"

	// Determine if we should skip prerelease versions
	// Skip prereleases unless:
	// 1. The allow-prerelease label is set, OR
	// 2. The current version is itself a prerelease (user is already tracking prereleases)
	// When current version is unknown (nil), default to stable behavior (skip prereleases)
	skipPrereleases := !allowPrerelease && (currentVersion == nil || currentVersion.Prerelease == "")

	// Parse min/max version constraints
	var minVersion, maxVersion *version.Version
	if minVerStr := labels[scripts.VersionMinLabel]; minVerStr != "" {
		minVersion = c.versionParser.ParseTag(minVerStr)
		if minVersion != nil {
			log.Printf("findLatestVersion: Min version constraint: %s", minVersion.String())
		}
	}
	if maxVerStr := labels[scripts.VersionMaxLabel]; maxVerStr != "" {
		maxVersion = c.versionParser.ParseTag(maxVerStr)
		if maxVersion != nil {
			log.Printf("findLatestVersion: Max version constraint: %s", maxVersion.String())
		}
	}

	// Check if major version pinning is enabled
	pinMajor := labels[scripts.VersionPinMajorLabel] == "true"
	if pinMajor && currentVersion != nil {
		log.Printf("findLatestVersion: Major version pinning enabled (current: %d.x)", currentVersion.Major)
	}

	// Check if minor version pinning is enabled
	pinMinor := labels[scripts.VersionPinMinorLabel] == "true"
	if pinMinor && currentVersion != nil {
		log.Printf("findLatestVersion: Minor version pinning enabled (current: %d.%d.x)", currentVersion.Major, currentVersion.Minor)
	}

	// Check if patch version pinning is enabled
	pinPatch := labels[scripts.VersionPinPatchLabel] == "true"
	if pinPatch && currentVersion != nil {
		log.Printf("findLatestVersion: Patch version pinning enabled (current: %d.%d.%d)", currentVersion.Major, currentVersion.Minor, currentVersion.Patch)
	}

	log.Printf("findLatestVersion: Looking for tags with suffix='%s', currentVersion=%v, skipPrereleases=%v, allowPrerelease=%v", requiredSuffix, currentVersion, skipPrereleases, allowPrerelease)

	for _, tag := range tags {
		if tag == "latest" || tag == "stable" || tag == "main" || tag == "develop" {
			continue // Skip non-versioned tags
		}

		// Parse the tag to extract version and suffix
		tagInfo := c.versionParser.ParseImageTag("dummy:" + tag)
		if tagInfo == nil || !tagInfo.IsVersioned || tagInfo.Version == nil {
			continue // Skip tags without semantic versions
		}

		// Skip tags whose version type doesn't match the current version's type
		// This prevents cross-comparison of semantic versions (3.23.3) with date tags (20260127)
		if currentVersion != nil && currentVersion.Type != "" && tagInfo.Version.Type != currentVersion.Type {
			continue
		}

		// Filter by suffix - must match exactly
		if tagInfo.Suffix != requiredSuffix {
			log.Printf("  Skipping tag %s: suffix '%s' != required '%s'", tag, tagInfo.Suffix, requiredSuffix)
			continue // Different variant, skip it
		}

		// Skip prerelease versions unless explicitly allowed or already on a prerelease
		if skipPrereleases && tagInfo.Version.Prerelease != "" {
			log.Printf("  Skipping tag %s: prerelease '%s' (prereleases not allowed)", tag, tagInfo.Version.Prerelease)
			continue
		}

		// Apply major version pinning filter
		if pinMajor && currentVersion != nil {
			if tagInfo.Version.Major != currentVersion.Major {
				log.Printf("  Skipping tag %s: different major version (pinned to %d.x)", tag, currentVersion.Major)
				continue
			}
		}

		// Apply minor version pinning filter
		if pinMinor && currentVersion != nil {
			if tagInfo.Version.Major != currentVersion.Major || tagInfo.Version.Minor != currentVersion.Minor {
				log.Printf("  Skipping tag %s: different minor version (pinned to %d.%d.x)", tag, currentVersion.Major, currentVersion.Minor)
				continue
			}
		}

		// Apply patch version pinning filter
		if pinPatch && currentVersion != nil {
			if tagInfo.Version.Major != currentVersion.Major || tagInfo.Version.Minor != currentVersion.Minor || tagInfo.Version.Patch != currentVersion.Patch {
				log.Printf("  Skipping tag %s: different patch version (pinned to %d.%d.%d)", tag, currentVersion.Major, currentVersion.Minor, currentVersion.Patch)
				continue
			}
		}

		// Apply minimum version filter
		if minVersion != nil && c.versionComp.Compare(tagInfo.Version, minVersion) < 0 {
			log.Printf("  Skipping tag %s: below minimum version %s", tag, minVersion.String())
			continue
		}

		// Apply maximum version filter
		if maxVersion != nil && c.versionComp.Compare(tagInfo.Version, maxVersion) > 0 {
			log.Printf("  Skipping tag %s: above maximum version %s", tag, maxVersion.String())
			continue
		}

		log.Printf("  Accepted tag %s: version=%s, suffix='%s', buildNum=%d", tag, tagInfo.Version.String(), tagInfo.Suffix, tagInfo.Version.BuildNumber)
		versions = append(versions, tagInfo.Version)
		// Use Original (the full tag) as key since String() doesn't include build number
		versionToTag[tagInfo.Version.Original] = tag
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
	return versionToTag[latest.Original]
}

// resolveVersionFromDigest attempts to find which semantic version tag corresponds
// to the given digest by querying the registry for tag-to-digest mappings.
// Uses cache if storage is available to reduce registry API calls.
// If requiredSuffix is provided, only tags with that suffix will be considered.
func (c *Checker) resolveVersionFromDigest(ctx context.Context, imageRef, currentDigest string, requiredSuffix ...string) string {
	// Extract optional suffix parameter
	suffix := ""
	if len(requiredSuffix) > 0 {
		suffix = requiredSuffix[0]
	}
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

	truncatedDigest := currentDigest
	if len(truncatedDigest) > 12 {
		truncatedDigest = truncatedDigest[:12]
	}
	log.Printf("Got %d tags with digests for %s, looking for digest %s", len(tagDigests), imageRef, truncatedDigest)

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
					// Filter by suffix if one is required
					if suffix != "" && tagInfo.Suffix != suffix {
						log.Printf("  Skipping tag %s: suffix '%s' != required '%s'", tag, tagInfo.Suffix, suffix)
						break
					}
					matchingVersions = append(matchingVersions, tag)
				}
				break
			}
		}
	}

	log.Printf("Found %d matching semver tags for digest %s (latest=%v)", len(matchingVersions), truncatedDigest, matchingLatest)

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

// filterTagsByRegex filters a list of tags by a regular expression pattern.
// Returns only tags that match the pattern. If the pattern is invalid, returns all tags (fail-open).
func filterTagsByRegex(tags []string, pattern string) []string {
	if pattern == "" {
		return tags
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		// Invalid regex - log error but don't block updates (fail-open for safety)
		log.Printf("Warning: Invalid regex pattern '%s': %v - ignoring filter", pattern, err)
		return tags
	}

	var filtered []string
	for _, tag := range tags {
		if re.MatchString(tag) {
			filtered = append(filtered, tag)
		}
	}

	return filtered
}

// isMetaTag checks if a tag is a meta tag (like "latest", "stable", "main")
// or a meta tag with a suffix (like "stable-tensorrt", "latest-alpine").
// These tags don't contain semantic versions and should use digest comparison.
func isMetaTag(tag string) bool {
	metaTags := map[string]bool{
		"latest": true, "stable": true, "main": true, "master": true,
		"develop": true, "dev": true, "edge": true,
		"nightly": true, "beta": true, "alpha": true, "rc": true,
	}

	// Check exact match
	if metaTags[strings.ToLower(tag)] {
		return true
	}

	// Check if tag starts with a meta tag followed by hyphen (e.g., "stable-tensorrt")
	if idx := strings.Index(tag, "-"); idx > 0 {
		prefix := strings.ToLower(tag[:idx])
		return metaTags[prefix]
	}

	return false
}
