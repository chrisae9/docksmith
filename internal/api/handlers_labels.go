package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/scripts"
)

// Docker Compose label names
const (
	composeConfigFilesLabel = "com.docker.compose.project.config_files"
	composeServiceLabel     = "com.docker.compose.service"
	composeProjectLabel     = "com.docker.compose.project"
)

// SetLabelsRequest represents a request to set labels
type SetLabelsRequest struct {
	Container        string  `json:"container"`
	Ignore           *bool   `json:"ignore,omitempty"`
	AllowLatest      *bool   `json:"allow_latest,omitempty"`
	Script           *string `json:"script,omitempty"`
	RestartDependsOn *string `json:"restart_depends_on,omitempty"`
	NoRestart        bool    `json:"no_restart,omitempty"`
	Force            bool    `json:"force,omitempty"`
}

// RemoveLabelsRequest represents a request to remove labels
type RemoveLabelsRequest struct {
	Container  string   `json:"container"`
	LabelNames []string `json:"label_names"`
	NoRestart  bool     `json:"no_restart,omitempty"`
	Force      bool     `json:"force,omitempty"`
}

// LabelOperationResult represents the result of a label operation
type LabelOperationResult struct {
	Success        bool              `json:"success"`
	Container      string            `json:"container"`
	Operation      string            `json:"operation"`
	LabelsModified map[string]string `json:"labels_modified,omitempty"`
	LabelsRemoved  []string          `json:"labels_removed,omitempty"`
	ComposeFile    string            `json:"compose_file"`
	Restarted      bool              `json:"restarted"`
	PreCheckRan    bool              `json:"pre_check_ran"`
	PreCheckPassed bool              `json:"pre_check_passed,omitempty"`
	Message        string            `json:"message,omitempty"`
}

// handleLabelsGet returns labels for a container
// GET /api/labels/:container
func (s *Server) handleLabelsGet(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("container")

	if containerName == "" {
		output.WriteJSONError(w, fmt.Errorf("missing container name"))
		return
	}

	ctx := r.Context()

	// Find the container
	container, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		output.WriteJSONError(w, err)
		return
	}

	// Extract docksmith labels
	docksmithLabels := make(map[string]string)
	if val, ok := container.Labels[scripts.IgnoreLabel]; ok {
		docksmithLabels[scripts.IgnoreLabel] = val
	}
	if val, ok := container.Labels[scripts.AllowLatestLabel]; ok {
		docksmithLabels[scripts.AllowLatestLabel] = val
	}
	if val, ok := container.Labels[scripts.PreUpdateCheckLabel]; ok {
		docksmithLabels[scripts.PreUpdateCheckLabel] = val
	}
	if val, ok := container.Labels[scripts.RestartDependsOnLabel]; ok {
		docksmithLabels[scripts.RestartDependsOnLabel] = val
	}

	RespondSuccess(w, map[string]any{
		"container": containerName,
		"labels":    docksmithLabels,
	})
}

// handleLabelsSet sets labels on a container
// POST /api/labels/set
func (s *Server) handleLabelsSet(w http.ResponseWriter, r *http.Request) {
	var req SetLabelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		output.WriteJSONError(w, fmt.Errorf("invalid request: %w", err))
		return
	}

	if req.Container == "" {
		output.WriteJSONError(w, fmt.Errorf("missing container name"))
		return
	}

	if req.Ignore == nil && req.AllowLatest == nil && req.Script == nil && req.RestartDependsOn == nil {
		output.WriteJSONError(w, fmt.Errorf("no labels specified"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	result, err := s.setLabels(ctx, &req)
	if err != nil {
		output.WriteJSONError(w, err)
		return
	}

	// Trigger background check to update container status after label change
	if s.backgroundChecker != nil {
		s.backgroundChecker.TriggerCheck()
	}

	RespondSuccess(w, result)
}

// handleLabelsRemove removes labels from a container
// POST /api/labels/remove
func (s *Server) handleLabelsRemove(w http.ResponseWriter, r *http.Request) {
	var req RemoveLabelsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		output.WriteJSONError(w, fmt.Errorf("invalid request: %w", err))
		return
	}

	if req.Container == "" {
		output.WriteJSONError(w, fmt.Errorf("missing container name"))
		return
	}

	if len(req.LabelNames) == 0 {
		output.WriteJSONError(w, fmt.Errorf("no labels specified"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	result, err := s.removeLabels(ctx, &req)
	if err != nil {
		output.WriteJSONError(w, err)
		return
	}

	// Trigger background check to update container status after label change
	if s.backgroundChecker != nil {
		s.backgroundChecker.TriggerCheck()
	}

	RespondSuccess(w, result)
}

// withComposeBackup wraps a compose file operation with automatic backup and restore
// It creates a backup before the operation, restores on error, and cleans up on success
func withComposeBackup(composeFilePath string, operation func(backupPath string) error) error {
	backupPath, err := compose.BackupComposeFile(composeFilePath)
	if err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}

	success := false
	defer func() {
		if success {
			// Clean up backup on success
			_ = os.Remove(backupPath)
		}
	}()

	err = operation(backupPath)
	if err != nil {
		// Restore from backup on error
		compose.RestoreFromBackup(composeFilePath, backupPath)
		return err
	}

	success = true
	return nil
}

// setLabels implements the label setting logic (atomic: compose update + restart)
func (s *Server) setLabels(ctx context.Context, req *SetLabelsRequest) (*LabelOperationResult, error) {
	result := &LabelOperationResult{
		Success:        false,
		Container:      req.Container,
		Operation:      "set",
		LabelsModified: make(map[string]string),
		Restarted:      false,
		PreCheckRan:    false,
	}

	// Find container
	container, err := s.findContainerByName(ctx, req.Container)
	if err != nil {
		return nil, err
	}

	// Get compose file path
	composeFilePath, ok := container.Labels[composeConfigFilesLabel]
	if !ok || composeFilePath == "" {
		return nil, fmt.Errorf("container %s is not managed by docker compose", container.Name)
	}

	// Translate host path to container path
	composeFilePath = s.pathTranslator.TranslateToContainer(composeFilePath)

	result.ComposeFile = composeFilePath
	serviceName := container.Labels[composeServiceLabel]

	// Run pre-update check if configured and not forced/no-restart
	skipCheck := req.NoRestart || req.Force
	ran, passed, err := s.executeContainerPreUpdateCheck(ctx, container, skipCheck)
	result.PreCheckRan = ran
	result.PreCheckPassed = passed
	if err != nil {
		return nil, fmt.Errorf("pre-update check failed: %w (use force to skip)", err)
	}

	// Create backup
	backupPath, err := compose.BackupComposeFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}
	defer func() {
		// Clean up backup on success
		if result.Success {
			_ = os.Remove(backupPath)
		}
	}()

	// Load compose file
	composeFile, err := compose.LoadComposeFile(composeFilePath)
	if err != nil {
		compose.RestoreFromBackup(composeFilePath, backupPath)
		return nil, fmt.Errorf("failed to load compose file: %w", err)
	}

	// Find service
	service, err := composeFile.FindServiceByContainerName(serviceName)
	if err != nil {
		compose.RestoreFromBackup(composeFilePath, backupPath)
		return nil, fmt.Errorf("failed to find service: %w", err)
	}

	// Apply label updates
	if req.Ignore != nil {
		if *req.Ignore {
			// Set to true (non-default)
			if err := service.SetLabel(scripts.IgnoreLabel, "true"); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to set ignore label: %w", err)
			}
			result.LabelsModified[scripts.IgnoreLabel] = "true"
		} else {
			// Remove label when false (default value)
			if err := service.RemoveLabel(scripts.IgnoreLabel); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to remove ignore label: %w", err)
			}
			result.LabelsModified[scripts.IgnoreLabel] = ""
		}
	}

	if req.AllowLatest != nil {
		if *req.AllowLatest {
			// Set to true (non-default)
			if err := service.SetLabel(scripts.AllowLatestLabel, "true"); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to set allow-latest label: %w", err)
			}
			result.LabelsModified[scripts.AllowLatestLabel] = "true"
		} else {
			// Remove label when false (default value)
			if err := service.RemoveLabel(scripts.AllowLatestLabel); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to remove allow-latest label: %w", err)
			}
			result.LabelsModified[scripts.AllowLatestLabel] = ""
		}
	}

	if req.Script != nil {
		// If script is empty string, remove the label; otherwise set it
		if *req.Script == "" {
			if err := service.RemoveLabel(scripts.PreUpdateCheckLabel); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to remove script label: %w", err)
			}
			result.LabelsModified[scripts.PreUpdateCheckLabel] = ""
		} else {
			if err := service.SetLabel(scripts.PreUpdateCheckLabel, *req.Script); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to set script label: %w", err)
			}
			result.LabelsModified[scripts.PreUpdateCheckLabel] = *req.Script
		}
	}

	if req.RestartDependsOn != nil {
		// If restart-depends-on is empty string, remove the label; otherwise set it
		if *req.RestartDependsOn == "" {
			if err := service.RemoveLabel(scripts.RestartDependsOnLabel); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to remove restart-depends-on label: %w", err)
			}
			result.LabelsModified[scripts.RestartDependsOnLabel] = ""
		} else {
			if err := service.SetLabel(scripts.RestartDependsOnLabel, *req.RestartDependsOn); err != nil {
				compose.RestoreFromBackup(composeFilePath, backupPath)
				return nil, fmt.Errorf("failed to set restart-depends-on label: %w", err)
			}
			result.LabelsModified[scripts.RestartDependsOnLabel] = *req.RestartDependsOn
		}
	}

	// Save compose file
	if err := composeFile.Save(); err != nil {
		compose.RestoreFromBackup(composeFilePath, backupPath)
		return nil, fmt.Errorf("failed to save compose file: %w", err)
	}

	// Restart container to apply labels (unless --no-restart)
	if !req.NoRestart {
		if err := s.restartContainerByService(ctx, composeFilePath, serviceName); err != nil {
			compose.RestoreFromBackup(composeFilePath, backupPath)
			return nil, fmt.Errorf("failed to restart container: %w", err)
		}
		result.Restarted = true
	}

	result.Success = true
	result.Message = fmt.Sprintf("%d label(s) set successfully", len(result.LabelsModified))

	return result, nil
}

// removeLabels implements the label removal logic (atomic: compose update + restart)
func (s *Server) removeLabels(ctx context.Context, req *RemoveLabelsRequest) (*LabelOperationResult, error) {
	result := &LabelOperationResult{
		Success:       false,
		Container:     req.Container,
		Operation:     "remove",
		LabelsRemoved: req.LabelNames,
		Restarted:     false,
		PreCheckRan:   false,
	}

	// Find container
	container, err := s.findContainerByName(ctx, req.Container)
	if err != nil {
		return nil, err
	}

	// Get compose file path
	composeFilePath, ok := container.Labels[composeConfigFilesLabel]
	if !ok || composeFilePath == "" {
		return nil, fmt.Errorf("container %s is not managed by docker compose", container.Name)
	}

	// Translate host path to container path
	composeFilePath = s.pathTranslator.TranslateToContainer(composeFilePath)

	result.ComposeFile = composeFilePath
	serviceName := container.Labels[composeServiceLabel]

	// Run pre-update check if configured and not forced/no-restart
	skipCheck := req.NoRestart || req.Force
	ran, passed, err := s.executeContainerPreUpdateCheck(ctx, container, skipCheck)
	result.PreCheckRan = ran
	result.PreCheckPassed = passed
	if err != nil {
		return nil, fmt.Errorf("pre-update check failed: %w (use force to skip)", err)
	}

	// Create backup
	backupPath, err := compose.BackupComposeFile(composeFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to create backup: %w", err)
	}
	defer func() {
		// Clean up backup on success
		if result.Success {
			_ = os.Remove(backupPath)
		}
	}()

	// Load compose file
	composeFile, err := compose.LoadComposeFile(composeFilePath)
	if err != nil {
		compose.RestoreFromBackup(composeFilePath, backupPath)
		return nil, fmt.Errorf("failed to load compose file: %w", err)
	}

	// Find service
	service, err := composeFile.FindServiceByContainerName(serviceName)
	if err != nil {
		compose.RestoreFromBackup(composeFilePath, backupPath)
		return nil, fmt.Errorf("failed to find service: %w", err)
	}

	// Remove all specified labels
	for _, labelName := range req.LabelNames {
		if err := service.RemoveLabel(labelName); err != nil {
			compose.RestoreFromBackup(composeFilePath, backupPath)
			return nil, fmt.Errorf("failed to remove label %s: %w", labelName, err)
		}
	}

	// Save compose file
	if err := composeFile.Save(); err != nil {
		compose.RestoreFromBackup(composeFilePath, backupPath)
		return nil, fmt.Errorf("failed to save compose file: %w", err)
	}

	// Restart container to apply changes (unless --no-restart)
	if !req.NoRestart {
		if err := s.restartContainerByService(ctx, composeFilePath, serviceName); err != nil {
			compose.RestoreFromBackup(composeFilePath, backupPath)
			return nil, fmt.Errorf("failed to restart container: %w", err)
		}
		result.Restarted = true
	}

	result.Success = true
	result.Message = fmt.Sprintf("%d label(s) removed successfully", len(req.LabelNames))

	return result, nil
}

// executeContainerPreUpdateCheck runs pre-update check if configured and not skipped.
// Returns (ran bool, passed bool, err error).
// The skipCheck parameter should be true when force=true or noRestart=true.
func (s *Server) executeContainerPreUpdateCheck(ctx context.Context, container *docker.Container, skipCheck bool) (bool, bool, error) {
	if skipCheck {
		return false, false, nil
	}

	scriptPath, ok := container.Labels[scripts.PreUpdateCheckLabel]
	if !ok || scriptPath == "" {
		return false, false, nil
	}

	if err := s.runPreUpdateCheck(ctx, container, scriptPath); err != nil {
		return true, false, err
	}

	return true, true, nil
}

// runPreUpdateCheck runs a pre-update check script
func (s *Server) runPreUpdateCheck(ctx context.Context, container *docker.Container, scriptPath string) error {
	// Use shared implementation with path translation disabled (API runs in container)
	return scripts.ExecutePreUpdateCheck(ctx, container, scriptPath, false)
}

// restartContainerByService recreates a container using docker compose
// Used for applying label changes (both additions and removals)
func (s *Server) restartContainerByService(ctx context.Context, composeFilePath, serviceName string) error {
	// Find the container by service name
	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *docker.Container
	for _, c := range containers {
		if svc, ok := c.Labels[composeServiceLabel]; ok && svc == serviceName {
			// Also verify it's from the same compose file
			if cf, ok := c.Labels[composeConfigFilesLabel]; ok {
				// Translate and compare
				translatedCF := s.pathTranslator.TranslateToContainer(cf)
				if translatedCF == composeFilePath {
					targetContainer = &c
					break
				}
			}
		}
	}

	if targetContainer == nil {
		return fmt.Errorf("container not found for service: %s", serviceName)
	}

	// Get host and container paths for compose
	hostComposePath := s.pathTranslator.TranslateToHost(composeFilePath)

	// Use compose-based recreation (preferred method)
	// This handles all dependencies, network modes, hostname conflicts, etc. automatically
	recreator := compose.NewRecreator(s.dockerService)
	if err := recreator.RecreateWithCompose(ctx, targetContainer, hostComposePath, composeFilePath); err != nil {
		return fmt.Errorf("failed to recreate container with compose: %w", err)
	}

	return nil
}

// findContainerByName searches for a container by name in the list of running containers
func (s *Server) findContainerByName(ctx context.Context, containerName string) (*docker.Container, error) {
	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, c := range containers {
		if c.Name == containerName {
			return &c, nil
		}
	}

	return nil, fmt.Errorf("container not found: %s", containerName)
}
