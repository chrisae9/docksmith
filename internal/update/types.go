package update

import "github.com/chis/docksmith/internal/version"

// UpdateStatus represents the update status of a container.
type UpdateStatus string

const (
	UpdateAvailable        UpdateStatus = "UPDATE_AVAILABLE"
	UpdateAvailableBlocked UpdateStatus = "UPDATE_AVAILABLE_BLOCKED" // Update available but blocked by pre-update check
	UpToDate               UpdateStatus = "UP_TO_DATE"
	UpToDatePinnable       UpdateStatus = "UP_TO_DATE_PINNABLE" // Up to date but using :latest, should migrate to semver
	LocalImage             UpdateStatus = "LOCAL_IMAGE"
	Unknown                UpdateStatus = "UNKNOWN"
	CheckFailed            UpdateStatus = "CHECK_FAILED"
	MetadataUnavailable    UpdateStatus = "METADATA_UNAVAILABLE" // Registry lookup failed, but container may be healthy
	Ignored                UpdateStatus = "IGNORED"              // Container is ignored via docksmith.ignore label
)

// ContainerUpdate represents an update candidate for a container.
type ContainerUpdate struct {
	ContainerName      string
	Image              string
	CurrentTag         string // The tag portion of the image (e.g., "latest", "v1.2.3")
	CurrentVersion     string
	CurrentSuffix      string // Variant suffix (e.g., "tensorrt", "alpine") - used for strict filtering
	LatestVersion      string
	CurrentDigest      string // SHA256 digest of current image
	LatestDigest       string // SHA256 digest of latest (for non-versioned fallback)
	AvailableTags      []string
	ChangeType         version.ChangeType
	Status             UpdateStatus
	Error              string
	IsLocal            bool
	RecommendedTag     string // Recommended semver tag to switch to (if using :latest)
	UsingLatestTag     bool   // True if container is using :latest tag
	PreUpdateCheck     string // Path to pre-update check script
	PreUpdateCheckFail string // Reason why pre-update check failed (if blocked)
}

// CheckResult contains the results of checking for updates.
type CheckResult struct {
	Updates        []ContainerUpdate
	TotalChecked   int
	UpdatesFound   int
	UpToDate       int
	LocalImages    int
	Failed         int
	Ignored        int
}
