package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/storage"
	"github.com/google/uuid"
)

// Docker Compose label constants are defined in constants.go

// SetLabelsRequest represents a request to set labels
type SetLabelsRequest struct {
	Container        string  `json:"container"`
	Ignore           *bool   `json:"ignore,omitempty"`
	AllowLatest      *bool   `json:"allow_latest,omitempty"`
	AllowPrerelease  *bool   `json:"allow_prerelease,omitempty"`
	VersionPinMajor  *bool   `json:"version_pin_major,omitempty"`
	VersionPinMinor  *bool   `json:"version_pin_minor,omitempty"`
	TagRegex         *string `json:"tag_regex,omitempty"`
	VersionMin       *string `json:"version_min,omitempty"`
	VersionMax       *string `json:"version_max,omitempty"`
	Script           *string `json:"script,omitempty"`
	RestartAfter *string `json:"restart_after,omitempty"`
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
	OperationID    string            `json:"operation_id,omitempty"`
	LabelsModified map[string]string `json:"labels_modified,omitempty"`
	LabelsRemoved  []string          `json:"labels_removed,omitempty"`
	ComposeFile    string            `json:"compose_file"`
	Restarted      bool              `json:"restarted"`
	PreCheckRan    bool              `json:"pre_check_ran"`
	PreCheckPassed bool              `json:"pre_check_passed,omitempty"`
	Message        string            `json:"message,omitempty"`
}

// docksmithLabels are all the label keys that docksmith manages
var docksmithLabels = []string{
	scripts.IgnoreLabel,
	scripts.AllowLatestLabel,
	scripts.AllowPrereleaseLabel,
	scripts.VersionPinMajorLabel,
	scripts.VersionPinMinorLabel,
	scripts.TagRegexLabel,
	scripts.VersionMinLabel,
	scripts.VersionMaxLabel,
	scripts.PreUpdateCheckLabel,
	scripts.RestartAfterLabel,
}

// getDocksmithLabels extracts all docksmith labels from a container's labels
func getDocksmithLabels(containerLabels map[string]string) map[string]string {
	result := make(map[string]string)
	for _, key := range docksmithLabels {
		if val, ok := containerLabels[key]; ok {
			result[key] = val
		}
	}
	return result
}

// handleLabelsGet returns ALL labels for a container
// GET /api/labels/:container
// Returns all container labels from Docker, useful for identifying the container.
func (s *Server) handleLabelsGet(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("container")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	ctx := r.Context()

	// Find the container
	container, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, err)
		return
	}

	// Return ALL container labels (not just docksmith labels)
	// This is useful for identifying what the container is
	RespondSuccess(w, map[string]any{
		"container": containerName,
		"labels":    container.Labels,
		"source":    "container_labels",
	})
}

// handleLabelsSet sets labels on a container
// POST /api/labels/set
func (s *Server) handleLabelsSet(w http.ResponseWriter, r *http.Request) {
	var req SetLabelsRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if !validateRequired(w, "container", req.Container) {
		return
	}

	if req.Ignore == nil && req.AllowLatest == nil && req.VersionPinMajor == nil && req.VersionPinMinor == nil &&
		req.TagRegex == nil && req.VersionMin == nil && req.VersionMax == nil &&
		req.Script == nil && req.RestartAfter == nil {
		RespondBadRequest(w, fmt.Errorf("no labels specified"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), LabelOperationTimeout)
	defer cancel()

	result, err := s.setLabels(ctx, &req)
	if err != nil {
		RespondInternalError(w, err)
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
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if !validateRequired(w, "container", req.Container) {
		return
	}

	if len(req.LabelNames) == 0 {
		RespondBadRequest(w, fmt.Errorf("no labels specified"))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), LabelOperationTimeout)
	defer cancel()

	result, err := s.removeLabels(ctx, &req)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	// Trigger background check to update container status after label change
	if s.backgroundChecker != nil {
		s.backgroundChecker.TriggerCheck()
	}

	RespondSuccess(w, result)
}

// labelOperationConfig holds common parameters for label operations
type labelOperationConfig struct {
	containerName string
	operationType string // "set" or "remove"
	noRestart     bool
	force         bool
}

// labelModifier is a function that applies label changes to a service and returns the modified/removed labels
type labelModifier func(service *compose.Service) (modified map[string]string, removed []string, err error)

// executeLabelOperation is the common workflow for both set and remove operations
func (s *Server) executeLabelOperation(ctx context.Context, cfg labelOperationConfig, modify labelModifier) (*LabelOperationResult, error) {
	operationID := uuid.New().String()
	result := &LabelOperationResult{
		Success:        false,
		Container:      cfg.containerName,
		Operation:      cfg.operationType,
		OperationID:    operationID,
		LabelsModified: make(map[string]string),
		Restarted:      false,
		PreCheckRan:    false,
	}

	// Find container
	container, err := s.findContainerByName(ctx, cfg.containerName)
	if err != nil {
		return nil, err
	}

	// Capture old labels before making any changes
	oldLabels := getDocksmithLabels(container.Labels)
	oldLabelsJSON, err := json.Marshal(oldLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal old labels: %w", err)
	}

	// Get compose file path
	composeFilePath, ok := container.Labels[ComposeConfigFilesLabel]
	if !ok || composeFilePath == "" {
		return nil, fmt.Errorf("container %s is not managed by docker compose", container.Name)
	}

	// Translate host path to container path
	composeFilePath = s.pathTranslator.TranslateToContainer(composeFilePath)

	result.ComposeFile = composeFilePath
	serviceName := container.Labels[ComposeServiceLabel]
	stackName := container.Labels[ComposeProjectLabel]

	// Create operation record
	now := time.Now()
	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   container.ID,
		ContainerName: container.Name,
		StackName:     stackName,
		OperationType: "label_change",
		Status:        "in_progress",
		OldVersion:    string(oldLabelsJSON),
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.storageService != nil {
		s.storageService.SaveUpdateOperation(ctx, op)
	}

	// Run pre-update check if configured and not forced/no-restart
	skipCheck := cfg.noRestart || cfg.force
	ran, passed, err := s.executeContainerPreUpdateCheck(ctx, container, skipCheck)
	result.PreCheckRan = ran
	result.PreCheckPassed = passed
	if err != nil {
		s.failLabelOperation(ctx, operationID, fmt.Sprintf("pre-update check failed: %v", err))
		return nil, fmt.Errorf("pre-update check failed: %w (use force to skip)", err)
	}

	// Load compose file (handles include-based setups)
	composeFile, err := compose.LoadComposeFileOrIncluded(composeFilePath, cfg.containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to load compose file: %w", err)
	}

	// Find service
	service, err := composeFile.FindServiceByContainerName(serviceName)
	if err != nil {
		return nil, fmt.Errorf("failed to find service: %w", err)
	}

	// Apply the label modifications
	modified, removed, err := modify(service)
	if err != nil {
		return nil, err
	}
	result.LabelsModified = modified
	result.LabelsRemoved = removed

	// Save compose file
	if err := composeFile.Save(); err != nil {
		return nil, fmt.Errorf("failed to save compose file: %w", err)
	}

	// Restart container to apply changes (unless --no-restart)
	if !cfg.noRestart {
		if err := s.restartContainerByService(ctx, composeFilePath, serviceName); err != nil {
			s.failLabelOperation(ctx, operationID, fmt.Sprintf("failed to restart container: %v", err))
			return nil, fmt.Errorf("failed to restart container: %w", err)
		}
		result.Restarted = true
	}

	result.Success = true

	// Complete the operation
	var newLabelsJSON []byte
	if len(modified) > 0 {
		newLabelsJSON, err = json.Marshal(modified)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal modified labels: %w", err)
		}
		result.Message = fmt.Sprintf("%d label(s) set successfully", len(modified))
	} else if len(removed) > 0 {
		// For remove, create map with empty values to indicate removed
		removedMap := make(map[string]string)
		for _, labelName := range removed {
			removedMap[labelName] = ""
		}
		newLabelsJSON, err = json.Marshal(removedMap)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal removed labels: %w", err)
		}
		result.Message = fmt.Sprintf("%d label(s) removed successfully", len(removed))
	}
	s.completeLabelOperation(ctx, operationID, string(newLabelsJSON))

	return result, nil
}

// setLabels implements the label setting logic (atomic: compose update + restart)
func (s *Server) setLabels(ctx context.Context, req *SetLabelsRequest) (*LabelOperationResult, error) {
	return s.executeLabelOperation(ctx, labelOperationConfig{
		containerName: req.Container,
		operationType: "set",
		noRestart:     req.NoRestart,
		force:         req.Force,
	}, func(service *compose.Service) (map[string]string, []string, error) {
		modified := make(map[string]string)

		// Apply boolean label updates
		boolLabels := []struct {
			value    *bool
			labelKey string
		}{
			{req.Ignore, scripts.IgnoreLabel},
			{req.AllowLatest, scripts.AllowLatestLabel},
			{req.AllowPrerelease, scripts.AllowPrereleaseLabel},
			{req.VersionPinMajor, scripts.VersionPinMajorLabel},
			{req.VersionPinMinor, scripts.VersionPinMinorLabel},
		}

		for _, bl := range boolLabels {
			if bl.value != nil {
				val, err := applyBoolLabel(service, bl.labelKey, bl.value)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to apply %s label: %w", bl.labelKey, err)
				}
				modified[bl.labelKey] = val
			}
		}

		// Validate tag regex before applying
		if req.TagRegex != nil && *req.TagRegex != "" {
			if err := validateRegexPattern(*req.TagRegex); err != nil {
				return nil, nil, fmt.Errorf("invalid tag regex: %w", err)
			}
		}

		// Apply string label updates
		stringLabels := []struct {
			value    *string
			labelKey string
		}{
			{req.TagRegex, scripts.TagRegexLabel},
			{req.VersionMin, scripts.VersionMinLabel},
			{req.VersionMax, scripts.VersionMaxLabel},
			{req.Script, scripts.PreUpdateCheckLabel},
			{req.RestartAfter, scripts.RestartAfterLabel},
		}

		for _, sl := range stringLabels {
			if sl.value != nil {
				val, err := applyStringLabel(service, sl.labelKey, sl.value)
				if err != nil {
					return nil, nil, fmt.Errorf("failed to apply %s label: %w", sl.labelKey, err)
				}
				modified[sl.labelKey] = val
			}
		}

		return modified, nil, nil
	})
}

// removeLabels implements the label removal logic (atomic: compose update + restart)
func (s *Server) removeLabels(ctx context.Context, req *RemoveLabelsRequest) (*LabelOperationResult, error) {
	return s.executeLabelOperation(ctx, labelOperationConfig{
		containerName: req.Container,
		operationType: "remove",
		noRestart:     req.NoRestart,
		force:         req.Force,
	}, func(service *compose.Service) (map[string]string, []string, error) {
		// Remove all specified labels
		for _, labelName := range req.LabelNames {
			if err := service.RemoveLabel(labelName); err != nil {
				return nil, nil, fmt.Errorf("failed to remove label %s: %w", labelName, err)
			}
		}
		return nil, req.LabelNames, nil
	})
}

// failLabelOperation marks a label change operation as failed
func (s *Server) failLabelOperation(ctx context.Context, operationID, errorMsg string) {
	if s.storageService == nil {
		return
	}
	s.storageService.UpdateOperationStatus(ctx, operationID, "failed", errorMsg)
}

// completeLabelOperation marks a label change operation as complete
func (s *Server) completeLabelOperation(ctx context.Context, operationID, newLabelsJSON string) {
	if s.storageService == nil {
		return
	}
	op, found, _ := s.storageService.GetUpdateOperation(ctx, operationID)
	if !found {
		return
	}
	now := time.Now()
	op.Status = "complete"
	op.NewVersion = newLabelsJSON
	op.CompletedAt = &now
	s.storageService.SaveUpdateOperation(ctx, op)
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
		if svc, ok := c.Labels[ComposeServiceLabel]; ok && svc == serviceName {
			// Also verify it's from the same compose file
			if cf, ok := c.Labels[ComposeConfigFilesLabel]; ok {
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

// applyBoolLabel applies a boolean label change to a service.
// If value is true, sets the label to "true". If false, removes the label.
// Returns the value to store in LabelsModified ("true" or "").
func applyBoolLabel(service *compose.Service, labelKey string, value *bool) (string, error) {
	if value == nil {
		return "", nil // No change requested
	}

	if *value {
		if err := service.SetLabel(labelKey, "true"); err != nil {
			return "", err
		}
		return "true", nil
	}
	// Remove label when false (default value)
	if err := service.RemoveLabel(labelKey); err != nil {
		return "", err
	}
	return "", nil
}

// applyStringLabel applies a string label change to a service.
// If value is empty, removes the label. Otherwise sets it.
// Returns the value to store in LabelsModified.
func applyStringLabel(service *compose.Service, labelKey string, value *string) (string, error) {
	if value == nil {
		return "", nil // No change requested
	}

	if *value == "" {
		if err := service.RemoveLabel(labelKey); err != nil {
			return "", err
		}
		return "", nil
	}
	if err := service.SetLabel(labelKey, *value); err != nil {
		return "", err
	}
	return *value, nil
}
