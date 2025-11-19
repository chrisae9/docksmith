package scripts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/compose"
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

// findComposeFileForContainer finds the compose file that defines a container.
// Scans all configured compose file paths and returns the first match.
func (m *Manager) findComposeFileForContainer(containerName string) (string, error) {
	for _, composePath := range m.config.ComposeFilePaths {
		// Load compose file
		composeFile, err := compose.LoadComposeFile(composePath)
		if err != nil {
			// Skip files that can't be loaded
			continue
		}

		// Try to find service in this file
		_, err = composeFile.FindServiceByContainerName(containerName)
		if err == nil {
			// Found it!
			return composePath, nil
		}
	}

	return "", fmt.Errorf("no compose file found for container: %s", containerName)
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
