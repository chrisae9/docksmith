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
	ComposeMismatch        UpdateStatus = "COMPOSE_MISMATCH"     // Running image doesn't match compose file specification
	Ignored                UpdateStatus = "IGNORED"              // Container is ignored via docksmith.ignore label
)

// ContainerUpdate represents an update candidate for a container.
type ContainerUpdate struct {
	ContainerName      string              `json:"container_name"`
	Image              string              `json:"image"`
	CurrentTag         string              `json:"current_tag,omitempty"` // The tag portion of the image (e.g., "latest", "v1.2.3")
	CurrentVersion     string              `json:"current_version,omitempty"`
	CurrentSuffix      string              `json:"current_suffix,omitempty"` // Variant suffix (e.g., "tensorrt", "alpine") - used for strict filtering
	LatestVersion         string              `json:"latest_version,omitempty"`
	LatestResolvedVersion string              `json:"latest_resolved_version,omitempty"` // Resolved semantic version from latest digest lookup
	CurrentDigest         string              `json:"current_digest,omitempty"`          // SHA256 digest of current image
	LatestDigest          string              `json:"latest_digest,omitempty"`           // SHA256 digest of latest (for non-versioned fallback)
	AvailableTags      []string            `json:"available_tags,omitempty"`
	ChangeType         version.ChangeType  `json:"change_type"`
	Status             UpdateStatus        `json:"status"`
	Error              string              `json:"error,omitempty"`
	IsLocal            bool                `json:"is_local"`
	RecommendedTag     string              `json:"recommended_tag,omitempty"` // Recommended semver tag to switch to (if using :latest)
	UsingLatestTag     bool                `json:"using_latest_tag"`
	PreUpdateCheck     string              `json:"pre_update_check,omitempty"`      // Path to pre-update check script
	PreUpdateCheckFail string              `json:"pre_update_check_fail,omitempty"` // Reason why pre-update check failed (if blocked)
	PreUpdateCheckPass bool                `json:"pre_update_check_pass"`           // True if pre-update check passed
	HealthStatus       string              `json:"health_status,omitempty"`         // Current health status: "healthy", "unhealthy", "starting", "none"
	ComposeImage       string              `json:"compose_image,omitempty"`         // Image specified in compose file (for COMPOSE_MISMATCH)
	EnvControlled      bool                `json:"env_controlled,omitempty"`        // True if image is controlled by .env variable
	EnvVarName         string              `json:"env_var_name,omitempty"`          // Name of the controlling env var (e.g., "OPENCLAW_IMAGE")
	Note               string              `json:"note,omitempty"`                  // Informational note (e.g., ghost tag warning)
}

// CheckResult contains the results of checking for updates.
type CheckResult struct {
	Updates      []ContainerUpdate `json:"updates"`
	TotalChecked int               `json:"total_checked"`
	UpdatesFound int               `json:"updates_found"`
	UpToDate     int               `json:"up_to_date"`
	LocalImages  int               `json:"local_images"`
	Failed       int               `json:"failed"`
	Ignored      int               `json:"ignored"`
}
