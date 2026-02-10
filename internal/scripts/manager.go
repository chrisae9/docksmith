package scripts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/config"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/storage"
)

const (
	// ScriptsDir is the default directory for pre-update check scripts
	ScriptsDir = "/scripts"

	// PreUpdateCheckLabel is the Docker label key for pre-update checks
	PreUpdateCheckLabel = "docksmith.pre-update-check"

	// IgnoreLabel is the Docker label key to ignore containers from update checks
	IgnoreLabel = "docksmith.ignore"

	// AllowLatestLabel is the Docker label key to allow :latest tags
	AllowLatestLabel = "docksmith.allow-latest"

	// RestartAfterLabel is the Docker label key for restart dependencies
	// Comma-separated list of container names. When any of those containers restart,
	// this container will also restart after them.
	// Example: torrent container has "docksmith.restart-after=gluetun"
	//          When gluetun restarts, torrent will restart too
	RestartAfterLabel = "docksmith.restart-after"

	// VersionPinMajorLabel is the Docker label key to pin updates within the current major version
	// When set to "true", the container will only update to newer versions within the same major version.
	// Example: Container on Node 20.10.0 will update to 20.11.x but not to 21.x
	//          Container on Redis 7.2 will update to 7.4 but not to 8.x
	// Default: false (allow major version upgrades)
	VersionPinMajorLabel = "docksmith.version-pin-major"

	// VersionPinMinorLabel is the Docker label key to pin updates within the current minor version
	// When set to "true", the container will only update to newer patch versions within the same minor version.
	// Example: Container on Node 20.10.0 will update to 20.10.5 but not to 20.11.0
	//          Container on Redis 7.2.1 will update to 7.2.4 but not to 7.3.0
	// Default: false (allow minor version upgrades)
	VersionPinMinorLabel = "docksmith.version-pin-minor"

	// VersionPinPatchLabel is the Docker label key to pin updates within the current patch version
	// When set to "true", only build metadata/suffix changes are allowed (major.minor.patch stays the same).
	// Example: Container on v1.2.3-ls100 will update to v1.2.3-ls101 but not to v1.2.4
	//          Container on 3.5.1-alpine will update to 3.5.1-alpine2 but not to 3.5.2-alpine
	// Default: false (allow patch version upgrades)
	VersionPinPatchLabel = "docksmith.version-pin-patch"

	// TagRegexLabel is the Docker label key for custom tag filtering via regular expressions
	// When set, only tags matching the regex pattern will be considered for updates.
	// Example: "^v?[0-9.]+-alpine$" to only allow Alpine-based images
	//          "^v?[0-9]+-lts" to only allow LTS tags
	// Default: "" (no filtering)
	TagRegexLabel = "docksmith.tag-regex"

	// VersionMinLabel is the Docker label key to set a minimum version threshold
	// When set, only versions >= this value will be considered for updates.
	// Example: "2.0.0" to never suggest versions below 2.0.0
	//          "7.2" to skip old 7.0 and 7.1 branches
	// Default: "" (no minimum)
	VersionMinLabel = "docksmith.version-min"

	// VersionMaxLabel is the Docker label key to set a maximum version cap
	// When set, only versions <= this value will be considered for updates.
	// Example: "3.9.99" to cap at 3.x and defer v4 migration
	//          "20.99" to stay on Node 20.x indefinitely
	// Default: "" (no maximum)
	VersionMaxLabel = "docksmith.version-max"

	// AllowPrereleaseLabel is the Docker label key to allow prerelease versions
	// When set to "true", prerelease versions (alpha, beta, rc, pre, etc.) will be considered for updates.
	// By default, prereleases are skipped unless you're already running a prerelease version.
	// Example: Set to "true" on a container where you want to track beta releases
	// Default: false (skip prerelease versions)
	AllowPrereleaseLabel = "docksmith.allow-prerelease"
)

// Manager handles script discovery, validation, and assignment operations.
type Manager struct {
	storage storage.Storage
	config  *config.Config
}

// NewManager creates a new script manager.
func NewManager(storage storage.Storage, config *config.Config) *Manager {
	return &Manager{
		storage: storage,
		config:  config,
	}
}

// DiscoverScripts scans the scripts directory and returns all found scripts.
// Returns an error if the scripts directory doesn't exist or can't be read.
func (m *Manager) DiscoverScripts() ([]Script, error) {
	// Check if scripts directory exists
	if _, err := os.Stat(ScriptsDir); os.IsNotExist(err) {
		return []Script{}, nil // Return empty list if directory doesn't exist
	}

	var scripts []Script

	// Walk the scripts directory
	err := filepath.Walk(ScriptsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Skip hidden files and non-script files
		if strings.HasPrefix(info.Name(), ".") {
			return nil
		}

		// Only include .sh files
		if !strings.HasSuffix(info.Name(), ".sh") {
			return nil
		}

		// Get relative path from scripts directory
		relPath, err := filepath.Rel(ScriptsDir, path)
		if err != nil {
			return fmt.Errorf("failed to get relative path: %w", err)
		}

		// Check if executable
		executable := false
		if info.Mode()&0111 != 0 {
			executable = true
		}

		script := Script{
			Name:         info.Name(),
			Path:         path,
			RelativePath: relPath,
			Executable:   executable,
			Size:         info.Size(),
			ModifiedTime: info.ModTime(),
			FileInfo:     info,
		}

		scripts = append(scripts, script)
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan scripts directory: %w", err)
	}

	return scripts, nil
}

// ValidateScript checks if a script exists and is executable.
func (m *Manager) ValidateScript(scriptPath string) error {
	// Construct full path
	fullPath := filepath.Join(ScriptsDir, scriptPath)

	// Check if file exists
	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("script not found: %s", scriptPath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat script: %w", err)
	}

	// Check if it's a file (not a directory)
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a script: %s", scriptPath)
	}

	// Check if executable
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("script is not executable: %s (run: chmod +x %s)", scriptPath, fullPath)
	}

	return nil
}

// AssignScript assigns a script to a container.
// Database-only, no compose file modifications. Changes apply on next check.
func (m *Manager) AssignScript(ctx context.Context, containerName, scriptPath, assignedBy string) error {
	// Validate script if provided
	if scriptPath != "" {
		if err := m.ValidateScript(scriptPath); err != nil {
			return fmt.Errorf("script validation failed: %w", err)
		}
	}

	// Get existing settings or create new
	existing, found, err := m.storage.GetScriptAssignment(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to get existing settings: %w", err)
	}

	assignment := storage.ScriptAssignment{
		ContainerName: containerName,
		ScriptPath:    scriptPath,
		Enabled:       true,
		AssignedAt:    time.Now(),
		AssignedBy:    assignedBy,
		UpdatedAt:     time.Now(),
	}

	// Preserve existing ignore and allow_latest settings
	if found {
		assignment.Ignore = existing.Ignore
		assignment.AllowLatest = existing.AllowLatest
		assignment.AssignedAt = existing.AssignedAt // Keep original assigned time
	}

	if err := m.storage.SaveScriptAssignment(ctx, assignment); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// UnassignScript removes a script assignment from a container.
// Database-only. Changes apply on next check.
func (m *Manager) UnassignScript(ctx context.Context, containerName string) error {
	if err := m.storage.DeleteScriptAssignment(ctx, containerName); err != nil {
		return fmt.Errorf("failed to delete settings: %w", err)
	}
	return nil
}

// SetIgnore sets the ignore flag for a container.
// Database-only. Changes apply on next check.
func (m *Manager) SetIgnore(ctx context.Context, containerName string, ignore bool, assignedBy string) error {
	// Get existing settings or create new
	existing, found, err := m.storage.GetScriptAssignment(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to get existing settings: %w", err)
	}

	assignment := storage.ScriptAssignment{
		ContainerName: containerName,
		Enabled:       true,
		Ignore:        ignore,
		AssignedAt:    time.Now(),
		AssignedBy:    assignedBy,
		UpdatedAt:     time.Now(),
	}

	// Preserve existing settings
	if found {
		assignment.ScriptPath = existing.ScriptPath
		assignment.AllowLatest = existing.AllowLatest
		assignment.AssignedAt = existing.AssignedAt
	}

	if err := m.storage.SaveScriptAssignment(ctx, assignment); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// SetAllowLatest sets the allow-latest flag for a container.
// Database-only. Changes apply on next check.
func (m *Manager) SetAllowLatest(ctx context.Context, containerName string, allowLatest bool, assignedBy string) error {
	// Get existing settings or create new
	existing, found, err := m.storage.GetScriptAssignment(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to get existing settings: %w", err)
	}

	assignment := storage.ScriptAssignment{
		ContainerName: containerName,
		Enabled:       true,
		AllowLatest:   allowLatest,
		AssignedAt:    time.Now(),
		AssignedBy:    assignedBy,
		UpdatedAt:     time.Now(),
	}

	// Preserve existing settings
	if found {
		assignment.ScriptPath = existing.ScriptPath
		assignment.Ignore = existing.Ignore
		assignment.AssignedAt = existing.AssignedAt
	}

	if err := m.storage.SaveScriptAssignment(ctx, assignment); err != nil {
		return fmt.Errorf("failed to save settings: %w", err)
	}

	return nil
}

// ListAssignments retrieves all script assignments from the database.
func (m *Manager) ListAssignments(ctx context.Context, enabledOnly bool) ([]Assignment, error) {
	dbAssignments, err := m.storage.ListScriptAssignments(ctx, enabledOnly)
	if err != nil {
		return nil, fmt.Errorf("failed to list assignments: %w", err)
	}

	assignments := make([]Assignment, len(dbAssignments))
	for i, dbAssignment := range dbAssignments {
		assignments[i] = Assignment{
			ContainerName: dbAssignment.ContainerName,
			ScriptPath:    dbAssignment.ScriptPath,
			Enabled:       dbAssignment.Enabled,
			Ignore:        dbAssignment.Ignore,
			AllowLatest:   dbAssignment.AllowLatest,
			AssignedAt:    dbAssignment.AssignedAt,
			AssignedBy:    dbAssignment.AssignedBy,
			UpdatedAt:     dbAssignment.UpdatedAt,
		}
	}

	return assignments, nil
}

// MigrateLabels scans all running containers and migrates docksmith labels to database.
// Returns the number of containers migrated.
func (m *Manager) MigrateLabels(ctx context.Context, containers []docker.Container) (int, error) {
	migrated := 0
	for _, container := range containers {
		// Check for any docksmith labels
		scriptPath := container.Labels[PreUpdateCheckLabel]
		ignoreValue := container.Labels[IgnoreLabel]
		allowLatestValue := container.Labels[AllowLatestLabel]

		// Parse boolean values
		ignore := ignoreValue == "true" || ignoreValue == "1" || ignoreValue == "yes"
		allowLatest := allowLatestValue == "true" || allowLatestValue == "1" || allowLatestValue == "yes"

		// Only create database entry if at least one label exists
		if scriptPath != "" || ignore || allowLatest {
			// Check if entry already exists
			_, found, err := m.storage.GetScriptAssignment(ctx, container.Name)
			if err != nil {
				return migrated, fmt.Errorf("failed to check existing assignment for %s: %w", container.Name, err)
			}

			if found {
				// Skip if already exists
				continue
			}

			assignment := storage.ScriptAssignment{
				ContainerName: container.Name,
				ScriptPath:    scriptPath,
				Enabled:       true,
				Ignore:        ignore,
				AllowLatest:   allowLatest,
				AssignedAt:    time.Now(),
				AssignedBy:    "label-migration",
				UpdatedAt:     time.Now(),
			}

			if err := m.storage.SaveScriptAssignment(ctx, assignment); err != nil {
				return migrated, fmt.Errorf("failed to save assignment for %s: %w", container.Name, err)
			}

			migrated++
		}
	}

	return migrated, nil
}

// GetAssignment retrieves the assignment for a specific container.
func (m *Manager) GetAssignment(ctx context.Context, containerName string) (Assignment, bool, error) {
	dbAssignment, found, err := m.storage.GetScriptAssignment(ctx, containerName)
	if err != nil {
		return Assignment{}, false, fmt.Errorf("failed to get assignment: %w", err)
	}

	if !found {
		return Assignment{}, false, nil
	}

	assignment := Assignment{
		ContainerName: dbAssignment.ContainerName,
		ScriptPath:    dbAssignment.ScriptPath,
		Enabled:       dbAssignment.Enabled,
		Ignore:        dbAssignment.Ignore,
		AllowLatest:   dbAssignment.AllowLatest,
		AssignedAt:    dbAssignment.AssignedAt,
		AssignedBy:    dbAssignment.AssignedBy,
		UpdatedAt:     dbAssignment.UpdatedAt,
	}

	return assignment, true, nil
}
