package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/selfupdate"
	"github.com/chis/docksmith/internal/storage"
	"github.com/google/uuid"
)

// Docker Compose label constants are defined in constants.go

// SetLabelsRequest represents a request to set labels
type SetLabelsRequest struct {
	Container       string   `json:"container"`
	Ignore          *bool    `json:"ignore,omitempty"`
	AllowLatest     *bool    `json:"allow_latest,omitempty"`
	AllowPrerelease *bool    `json:"allow_prerelease,omitempty"`
	VersionPinMajor *bool    `json:"version_pin_major,omitempty"`
	VersionPinMinor *bool    `json:"version_pin_minor,omitempty"`
	VersionPinPatch *bool    `json:"version_pin_patch,omitempty"`
	TagRegex        *string  `json:"tag_regex,omitempty"`
	VersionMin      *string  `json:"version_min,omitempty"`
	VersionMax      *string  `json:"version_max,omitempty"`
	Script          *string  `json:"script,omitempty"`
	RestartAfter    *string  `json:"restart_after,omitempty"`
	Notify          *bool    `json:"notify,omitempty"`
	NotifyDigest    *bool    `json:"notify_digest,omitempty"`
	NotifyLevel     *string  `json:"notify_level,omitempty"`
	RemoveLabels    []string `json:"remove_labels,omitempty"`
	NoRestart       bool     `json:"no_restart,omitempty"`
	Force           bool     `json:"force,omitempty"`
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
	scripts.VersionPinPatchLabel,
	scripts.TagRegexLabel,
	scripts.VersionMinLabel,
	scripts.VersionMaxLabel,
	scripts.PreUpdateCheckLabel,
	scripts.RestartAfterLabel,
	scripts.NotifyLabel,
	scripts.NotifyLevelLabel,
	scripts.NotifyDigestLabel,
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

	if req.Ignore == nil && req.AllowLatest == nil && req.AllowPrerelease == nil && req.VersionPinMajor == nil && req.VersionPinMinor == nil && req.VersionPinPatch == nil &&
		req.TagRegex == nil && req.VersionMin == nil && req.VersionMax == nil &&
		req.Script == nil && req.RestartAfter == nil &&
		req.Notify == nil && req.NotifyDigest == nil && req.NotifyLevel == nil && len(req.RemoveLabels) == 0 {
		RespondBadRequest(w, fmt.Errorf("no labels specified"))
		return
	}

	if err := validateRemoveLabels(req.RemoveLabels); err != nil {
		RespondBadRequest(w, err)
		return
	}
	if err := validateNoSetRemoveOverlap(&req); err != nil {
		RespondBadRequest(w, err)
		return
	}

	if req.NotifyLevel != nil {
		if err := validateNotifyLevel(*req.NotifyLevel); err != nil {
			RespondBadRequest(w, err)
			return
		}
	}

	// Validate tag regex synchronously before launching async operation
	if req.TagRegex != nil && *req.TagRegex != "" {
		if err := validateRegexPattern(*req.TagRegex); err != nil {
			RespondBadRequest(w, fmt.Errorf("invalid tag regex: %w", err))
			return
		}
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
	batchGroupID  string // Links operations from a single batch action
}

// labelModifier is a function that applies label changes to a service and returns the modified/removed labels
type labelModifier func(service *compose.Service) (modified map[string]string, removed []string, err error)

// executeLabelOperation is the common workflow for both set and remove operations.
// It validates inputs, creates an operation record, and starts the work in a background
// goroutine (like UpdateSingleContainer does). This ensures the operation survives
// client disconnection.
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

	// Find container (validation - must complete before returning)
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

	// Get compose file path (validation - must complete before returning)
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
		BatchGroupID:  cfg.batchGroupID,
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.storageService != nil {
		s.storageService.SaveUpdateOperation(ctx, op)
	}

	// Run the actual label operation in a background goroutine
	// Use context.Background() so it survives client disconnection (like UpdateSingleContainer)
	go s.executeLabelOperationAsync(context.Background(), operationID, container, cfg, composeFilePath, serviceName, modify)

	// Return immediately with operation ID - frontend tracks progress via SSE
	result.Success = true
	result.Message = "Operation started"
	return result, nil
}

// selfRecreate starts a helper container that recreates docksmith from outside
// itself. Overridden in tests.
var selfRecreate = selfupdate.RecreateSelf

// executeLabelOperationAsync runs the label modification and restart in the background.
// This is analogous to executeSingleUpdate in the update orchestrator.
func (s *Server) executeLabelOperationAsync(ctx context.Context, operationID string, container *docker.Container, cfg labelOperationConfig, composeFilePath, serviceName string, modify labelModifier) {
	stackName := container.Labels[ComposeProjectLabel]

	log.Printf("LABEL_OP: Starting async label operation %s for container %s", operationID, cfg.containerName)

	// Stage 1: Pre-update check (0-10%)
	s.publishLabelProgress(operationID, cfg.containerName, stackName, "validating", 0, "Running pre-update check")
	skipCheck := cfg.noRestart || cfg.force
	_, _, err := s.executeContainerPreUpdateCheck(ctx, container, skipCheck)
	if err != nil {
		s.failLabelOperationWithEvent(ctx, operationID, cfg.containerName, stackName, fmt.Sprintf("pre-update check failed: %v", err))
		return
	}

	// Stage 2: Load and modify compose file (10-30%)
	s.publishLabelProgress(operationID, cfg.containerName, stackName, "updating_compose", 10, "Saving settings to compose file")

	// Serialize the read-modify-write per compose file: batch operations run one
	// goroutine per container, and services in the same file would otherwise
	// race (last save wins). The container restart below stays outside the lock.
	unlock := s.lockComposeFile(composeFilePath)
	modified, removed, err := func() (map[string]string, []string, error) {
		defer unlock()

		composeFile, err := compose.LoadComposeFileOrIncluded(composeFilePath, cfg.containerName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to load compose file: %w", err)
		}

		service, err := composeFile.FindServiceByContainerName(serviceName)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to find service: %w", err)
		}

		modified, removed, err := modify(service)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to apply labels: %w", err)
		}

		if err := composeFile.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save compose file: %w", err)
		}

		return modified, removed, nil
	}()
	if err != nil {
		s.failLabelOperationWithEvent(ctx, operationID, cfg.containerName, stackName, err.Error())
		return
	}

	s.publishLabelProgress(operationID, cfg.containerName, stackName, "updating_compose", 30, "Settings saved")

	// Stage 3: Restart container (30-90%)
	if !cfg.noRestart {
		if selfupdate.IsSelfContainer(container.ID, container.Image, container.Name) {
			s.executeSelfLabelRestart(ctx, operationID, container, composeFilePath, serviceName, modified, removed)
			return
		}

		s.publishLabelProgress(operationID, cfg.containerName, stackName, "stopping", 40, "Stopping container")

		if err := s.restartContainerByService(ctx, composeFilePath, serviceName); err != nil {
			s.failLabelOperationWithEvent(ctx, operationID, cfg.containerName, stackName, fmt.Sprintf("failed to restart container: %v", err))
			return
		}

		s.publishLabelProgress(operationID, cfg.containerName, stackName, "starting", 70, "Starting container")
		s.publishLabelProgress(operationID, cfg.containerName, stackName, "health_check", 90, "Health check passed")
	}

	// Stage 4: Complete (100%)
	s.publishLabelProgress(operationID, cfg.containerName, stackName, "complete", 100, "Settings saved and container restarted")

	newLabelsJSON := labelHistoryJSON(modified, removed)
	s.completeLabelOperation(ctx, operationID, newLabelsJSON)

	// Emit container updated event for dashboard refresh
	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"container_id":   container.ID,
				"container_name": cfg.containerName,
				"operation_id":   operationID,
				"status":         "complete",
			},
		})
	}

	log.Printf("LABEL_OP: Completed label operation %s for container %s", operationID, cfg.containerName)
}

// lockComposeFile takes the per-file lock that makes compose read-modify-write
// cycles atomic and returns the matching unlock. Servers built directly in tests
// may not have a mutex; those degrade to an unguarded section.
func (s *Server) lockComposeFile(composeFilePath string) func() {
	if s.composeMu == nil {
		return func() {}
	}
	return s.composeMu.Lock(composeFilePath)
}

// executeSelfLabelRestart applies a label change to docksmith itself.
// Docksmith cannot recreate itself with docker compose from inside the container
// it is about to stop: the compose process is killed together with the container's
// PID 1, leaving the container stopped and the operation unfinished. Instead the
// operation is parked as pending_restart and a helper container performs the
// recreate; resumePendingSelfUpdates() completes the operation on the next startup.
func (s *Server) executeSelfLabelRestart(ctx context.Context, operationID string, container *docker.Container, composeFilePath, serviceName string, modified map[string]string, removed []string) {
	stackName := container.Labels[ComposeProjectLabel]

	log.Printf("LABEL_OP: %s is docksmith itself, delegating restart to helper container", container.Name)

	s.publishLabelProgress(operationID, container.Name, stackName, "pending_restart", 90, "Docksmith is restarting to apply the label change...")

	// Persist the prepared state before this process exits so history and
	// rollback see the new labels after the restart.
	if s.storageService != nil {
		op, found, err := s.storageService.GetUpdateOperation(ctx, operationID)
		if err != nil {
			log.Printf("LABEL_OP: Warning - failed to load operation %s: %v", operationID, err)
		} else if found {
			op.Status = "pending_restart"
			op.NewVersion = labelHistoryJSON(modified, removed)
			op.ErrorMessage = "Label change prepared. Restarting docksmith to apply it..."
			if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
				log.Printf("LABEL_OP: Warning - failed to save pending_restart status: %v", err)
			}
		}
	}

	hostComposePath := s.pathTranslator.TranslateToHost(composeFilePath)

	go func() {
		if err := selfRecreate(context.Background(), container.Image, hostComposePath, serviceName); err != nil {
			log.Printf("LABEL_OP: Failed to start self-recreate helper for %s: %v", container.Name, err)
			s.failLabelOperationWithEvent(context.Background(), operationID, container.Name, stackName,
				fmt.Sprintf("failed to start self-recreate helper: %v", err))
			return
		}
		log.Printf("LABEL_OP: Self-recreate helper started for operation=%s", operationID)
	}()
}

// setLabels implements the label setting logic (atomic: compose update + restart)
func (s *Server) setLabels(ctx context.Context, req *SetLabelsRequest) (*LabelOperationResult, error) {
	return s.setLabelsWithConfig(ctx, req, "")
}

// setLabelsInGroup implements label setting with a batch group ID for linking related operations
func (s *Server) setLabelsInGroup(ctx context.Context, req *SetLabelsRequest, batchGroupID string) (*LabelOperationResult, error) {
	return s.setLabelsWithConfig(ctx, req, batchGroupID)
}

// setLabelsWithConfig is the common implementation for setLabels and setLabelsInGroup
func (s *Server) setLabelsWithConfig(ctx context.Context, req *SetLabelsRequest, batchGroupID string) (*LabelOperationResult, error) {
	return s.executeLabelOperation(ctx, labelOperationConfig{
		containerName: req.Container,
		operationType: "set",
		noRestart:     req.NoRestart,
		force:         req.Force,
		batchGroupID:  batchGroupID,
	}, func(service *compose.Service) (map[string]string, []string, error) {
		modified, err := applyLabelChanges(service, req)
		return modified, req.RemoveLabels, err
	})
}

// validateNotifyLevel checks that a notify-level value is one of major, minor, patch, build.
// An empty value is allowed (it removes the label).
func validateNotifyLevel(level string) error {
	switch level {
	case "", "major", "minor", "patch", "build":
		return nil
	}
	return fmt.Errorf("invalid notify_level %q: must be one of major, minor, patch, build", level)
}

// applyLabelChanges applies the requested label changes to a compose service
// and returns the labels that were modified.
func applyLabelChanges(service *compose.Service, req *SetLabelsRequest) (map[string]string, error) {
	// Validate before mutating any label
	if err := validateRemoveLabels(req.RemoveLabels); err != nil {
		return nil, err
	}
	if err := validateNoSetRemoveOverlap(req); err != nil {
		return nil, err
	}

	if req.NotifyLevel != nil {
		if err := validateNotifyLevel(*req.NotifyLevel); err != nil {
			return nil, err
		}
	}

	modified := make(map[string]string)
	// Removals share the same load/modify/save as the sets below
	for _, name := range req.RemoveLabels {
		if err := service.RemoveLabel(name); err != nil {
			return nil, fmt.Errorf("failed to remove label %s: %w", name, err)
		}
	}

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
		{req.VersionPinPatch, scripts.VersionPinPatchLabel},
	}

	for _, bl := range boolLabels {
		if bl.value != nil {
			val, err := applyBoolLabel(service, bl.labelKey, bl.value)
			if err != nil {
				return nil, fmt.Errorf("failed to apply %s label: %w", bl.labelKey, err)
			}
			modified[bl.labelKey] = val
		}
	}

	// Notify labels keep an explicit "false" (it is meaningful: mute / disable digest)
	for _, nl := range []struct {
		value    *bool
		labelKey string
	}{
		{req.Notify, scripts.NotifyLabel},
		{req.NotifyDigest, scripts.NotifyDigestLabel},
	} {
		if nl.value != nil {
			val := strconv.FormatBool(*nl.value)
			if err := service.SetLabel(nl.labelKey, val); err != nil {
				return nil, fmt.Errorf("failed to apply %s label: %w", nl.labelKey, err)
			}
			modified[nl.labelKey] = val
		}
	}

	// Validate tag regex before applying
	if req.TagRegex != nil && *req.TagRegex != "" {
		if err := validateRegexPattern(*req.TagRegex); err != nil {
			return nil, fmt.Errorf("invalid tag regex: %w", err)
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
		{req.NotifyLevel, scripts.NotifyLevelLabel},
	}

	for _, sl := range stringLabels {
		if sl.value != nil {
			val, err := applyStringLabel(service, sl.labelKey, sl.value)
			if err != nil {
				return nil, fmt.Errorf("failed to apply %s label: %w", sl.labelKey, err)
			}
			modified[sl.labelKey] = val
		}
	}

	return modified, nil
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

// publishLabelProgress publishes SSE progress events for label operations
func (s *Server) publishLabelProgress(operationID, containerName, stackName, stage string, percent int, message string) {
	if s.eventBus == nil {
		return
	}

	log.Printf("LABEL_OP: Publishing %s (%d%%) for operation=%s", stage, percent, operationID)

	s.eventBus.Publish(events.Event{
		Type: events.EventUpdateProgress,
		Payload: map[string]interface{}{
			"operation_id":   operationID,
			"container_name": containerName,
			"stack_name":     stackName,
			"stage":          stage,
			"progress":       percent,
			"message":        message,
			"timestamp":      time.Now().Unix(),
		},
	})
}

// failLabelOperationWithEvent marks operation as failed and emits SSE events
func (s *Server) failLabelOperationWithEvent(ctx context.Context, operationID, containerName, stackName, errorMsg string) {
	s.failLabelOperation(ctx, operationID, errorMsg)
	s.publishLabelProgress(operationID, containerName, stackName, "failed", 0, errorMsg)

	// Emit container updated event for dashboard refresh
	if s.eventBus != nil {
		s.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"container_name": containerName,
				"operation_id":   operationID,
				"status":         "failed",
			},
		})
	}

	log.Printf("LABEL_OP: Failed operation %s for container %s: %s", operationID, containerName, errorMsg)
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

// LabelRollbackRequest represents a request to rollback label changes
type LabelRollbackRequest struct {
	BatchGroupID   string   `json:"batch_group_id,omitempty"`  // Rollback all operations in this batch
	OperationIDs   []string `json:"operation_ids,omitempty"`   // Rollback specific operations
	ContainerNames []string `json:"container_names,omitempty"` // Rollback specific containers within a batch
	Force          bool     `json:"force,omitempty"`
}

// handleLabelRollback reverses label changes from a previous operation or batch
// POST /api/labels/rollback
func (s *Server) handleLabelRollback(w http.ResponseWriter, r *http.Request) {
	var req LabelRollbackRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if req.BatchGroupID == "" && len(req.OperationIDs) == 0 {
		RespondBadRequest(w, fmt.Errorf("batch_group_id or operation_ids required"))
		return
	}

	// Collect operations to rollback
	var operations []storage.UpdateOperation
	ctx := r.Context()

	if req.BatchGroupID != "" {
		ops, err := s.storageService.GetUpdateOperationsByBatchGroup(ctx, req.BatchGroupID)
		if err != nil {
			RespondInternalError(w, fmt.Errorf("failed to fetch batch operations: %w", err))
			return
		}
		operations = ops
	} else {
		for _, opID := range req.OperationIDs {
			op, found, err := s.storageService.GetUpdateOperation(ctx, opID)
			if err != nil {
				RespondInternalError(w, fmt.Errorf("failed to fetch operation %s: %w", opID, err))
				return
			}
			if found {
				operations = append(operations, op)
			}
		}
	}

	// Filter to only label_change operations that completed successfully
	var labelOps []storage.UpdateOperation
	for _, op := range operations {
		if op.OperationType != "label_change" || op.Status != "complete" {
			continue
		}
		// If specific containers requested, filter by name
		if len(req.ContainerNames) > 0 {
			found := false
			for _, name := range req.ContainerNames {
				if op.ContainerName == name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		labelOps = append(labelOps, op)
	}

	if len(labelOps) == 0 {
		RespondBadRequest(w, fmt.Errorf("no rollbackable label operations found"))
		return
	}

	// Generate a new batch group ID for the rollback operations
	rollbackBatchGroupID := uuid.New().String()
	results := make([]BatchLabelResult, 0, len(labelOps))

	for _, op := range labelOps {
		// Parse old and new labels
		var oldLabels map[string]string
		var newLabels map[string]string

		if op.OldVersion != "" {
			if err := json.Unmarshal([]byte(op.OldVersion), &oldLabels); err != nil {
				results = append(results, BatchLabelResult{
					Container: op.ContainerName,
					Success:   false,
					Error:     fmt.Sprintf("failed to parse old labels: %v", err),
				})
				continue
			}
		} else {
			oldLabels = make(map[string]string)
		}

		if op.NewVersion != "" {
			if err := json.Unmarshal([]byte(op.NewVersion), &newLabels); err != nil {
				results = append(results, BatchLabelResult{
					Container: op.ContainerName,
					Success:   false,
					Error:     fmt.Sprintf("failed to parse new labels: %v", err),
				})
				continue
			}
		} else {
			newLabels = make(map[string]string)
		}

		rollbackReq, hasChanges := s.buildLabelRollbackRequest(op.ContainerName, req.Force, oldLabels, newLabels)

		if !hasChanges {
			results = append(results, BatchLabelResult{
				Container: op.ContainerName,
				Success:   true,
			})
			continue
		}

		opCtx, cancel := context.WithTimeout(ctx, LabelOperationTimeout)
		result, err := s.setLabelsInGroup(opCtx, rollbackReq, rollbackBatchGroupID)
		cancel()

		if err != nil {
			results = append(results, BatchLabelResult{
				Container: op.ContainerName,
				Success:   false,
				Error:     err.Error(),
			})
			continue
		}

		// Mark the original operation as rolled back
		op.RollbackOccurred = true
		s.storageService.SaveUpdateOperation(ctx, op)

		results = append(results, BatchLabelResult{
			Container:   op.ContainerName,
			Success:     result.Success,
			OperationID: result.OperationID,
		})
	}

	// Trigger background check after rollback
	if s.backgroundChecker != nil {
		s.backgroundChecker.TriggerCheck()
	}

	RespondSuccess(w, map[string]any{
		"results":        results,
		"batch_group_id": rollbackBatchGroupID,
	})
}

// buildLabelRollbackRequest builds the inverse of a recorded label change: each
// label in newLabels is restored to its value in oldLabels, or removed if it did
// not exist before. It reports whether there is anything to roll back.
func (s *Server) buildLabelRollbackRequest(container string, force bool, oldLabels, newLabels map[string]string) (*SetLabelsRequest, bool) {
	req := &SetLabelsRequest{Container: container, Force: force}
	hasChanges := false
	for labelKey, newVal := range newLabels {
		oldVal, hadOldVal := oldLabels[labelKey]

		if newVal == "" {
			// Label was removed — restore it if it had a value
			if hadOldVal && oldVal != "" {
				hasChanges = true
				s.setRollbackLabel(req, labelKey, oldVal)
			}
		} else if hadOldVal && oldVal != "" {
			// Label was changed — restore old value
			if oldVal != newVal {
				hasChanges = true
				s.setRollbackLabel(req, labelKey, oldVal)
			}
		} else {
			// Label was added (didn't exist before) — remove it
			hasChanges = true
			s.setRollbackLabel(req, labelKey, "")
		}
	}
	return req, hasChanges
}

// setRollbackLabel sets a label field on the rollback request based on the label key and target value.
// An empty value for the notify labels (where an explicit "false" is meaningful)
// removes the label so the container inherits the default again.
func (s *Server) setRollbackLabel(req *SetLabelsRequest, labelKey, value string) {
	if value == "" && (labelKey == scripts.NotifyLabel || labelKey == scripts.NotifyDigestLabel) {
		if !slices.Contains(req.RemoveLabels, labelKey) {
			req.RemoveLabels = append(req.RemoveLabels, labelKey)
		}
		return
	}
	switch labelKey {
	case scripts.IgnoreLabel:
		v := value == "true"
		req.Ignore = &v
	case scripts.AllowLatestLabel:
		v := value == "true"
		req.AllowLatest = &v
	case scripts.AllowPrereleaseLabel:
		v := value == "true"
		req.AllowPrerelease = &v
	case scripts.VersionPinMajorLabel:
		v := value == "true"
		req.VersionPinMajor = &v
	case scripts.VersionPinMinorLabel:
		v := value == "true"
		req.VersionPinMinor = &v
	case scripts.VersionPinPatchLabel:
		v := value == "true"
		req.VersionPinPatch = &v
	case scripts.TagRegexLabel:
		req.TagRegex = &value
	case scripts.VersionMinLabel:
		req.VersionMin = &value
	case scripts.VersionMaxLabel:
		req.VersionMax = &value
	case scripts.PreUpdateCheckLabel:
		req.Script = &value
	case scripts.RestartAfterLabel:
		req.RestartAfter = &value
	case scripts.NotifyLabel:
		v := value == "true"
		req.Notify = &v
	case scripts.NotifyDigestLabel:
		v := value == "true"
		req.NotifyDigest = &v
	case scripts.NotifyLevelLabel:
		req.NotifyLevel = &value
	}
}

// BatchLabelsRequest represents a batch label operation
type BatchLabelsRequest struct {
	Operations []SetLabelsRequest `json:"operations"`
}

// BatchLabelResult represents a per-container result
type BatchLabelResult struct {
	Container   string `json:"container"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	OperationID string `json:"operation_id,omitempty"`
}

// handleBatchLabels applies label changes to multiple containers
// POST /api/labels/batch
func (s *Server) handleBatchLabels(w http.ResponseWriter, r *http.Request) {
	var req BatchLabelsRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if len(req.Operations) == 0 {
		RespondBadRequest(w, fmt.Errorf("no operations specified"))
		return
	}

	// Generate a batch group ID to link all operations from this user action
	batchGroupID := uuid.New().String()

	results := make([]BatchLabelResult, 0, len(req.Operations))

	for _, op := range req.Operations {
		if op.Container == "" {
			results = append(results, BatchLabelResult{
				Container: op.Container,
				Success:   false,
				Error:     "container name required",
			})
			continue
		}

		// Validate tag regex synchronously before launching async operation
		if op.TagRegex != nil && *op.TagRegex != "" {
			if err := validateRegexPattern(*op.TagRegex); err != nil {
				results = append(results, BatchLabelResult{
					Container: op.Container,
					Success:   false,
					Error:     fmt.Sprintf("invalid tag regex: %v", err),
				})
				continue
			}
		}

		// Reuse the existing setLabels logic with batch group ID
		opCopy := op
		ctx, cancel := context.WithTimeout(r.Context(), LabelOperationTimeout)
		result, err := s.setLabelsInGroup(ctx, &opCopy, batchGroupID)
		cancel()

		if err != nil {
			results = append(results, BatchLabelResult{
				Container: op.Container,
				Success:   false,
				Error:     err.Error(),
			})
			continue
		}

		results = append(results, BatchLabelResult{
			Container:   op.Container,
			Success:     result.Success,
			OperationID: result.OperationID,
		})
	}

	// Trigger background check once after all label changes
	if s.backgroundChecker != nil {
		s.backgroundChecker.TriggerCheck()
	}

	RespondSuccess(w, map[string]any{
		"results":        results,
		"batch_group_id": batchGroupID,
	})
}

// validateNoSetRemoveOverlap rejects a request that both sets and removes the same label.
func validateNoSetRemoveOverlap(req *SetLabelsRequest) error {
	if len(req.RemoveLabels) == 0 {
		return nil
	}
	set := map[string]bool{
		scripts.IgnoreLabel:          req.Ignore != nil,
		scripts.AllowLatestLabel:     req.AllowLatest != nil,
		scripts.AllowPrereleaseLabel: req.AllowPrerelease != nil,
		scripts.VersionPinMajorLabel: req.VersionPinMajor != nil,
		scripts.VersionPinMinorLabel: req.VersionPinMinor != nil,
		scripts.VersionPinPatchLabel: req.VersionPinPatch != nil,
		scripts.TagRegexLabel:        req.TagRegex != nil,
		scripts.VersionMinLabel:      req.VersionMin != nil,
		scripts.VersionMaxLabel:      req.VersionMax != nil,
		scripts.PreUpdateCheckLabel:  req.Script != nil,
		scripts.RestartAfterLabel:    req.RestartAfter != nil,
		scripts.NotifyLabel:          req.Notify != nil,
		scripts.NotifyDigestLabel:    req.NotifyDigest != nil,
		scripts.NotifyLevelLabel:     req.NotifyLevel != nil,
	}
	for _, name := range req.RemoveLabels {
		if set[name] {
			return fmt.Errorf("label %q is both set and removed", name)
		}
	}
	return nil
}

// validateRemoveLabels checks every name is a label docksmith manages.
func validateRemoveLabels(names []string) error {
	for _, name := range names {
		if !slices.Contains(docksmithLabels, name) {
			return fmt.Errorf("cannot remove label %q: not a docksmith label", name)
		}
	}
	return nil
}

// labelHistoryJSON records an operation's label changes for history and rollback:
// modified labels with their new values, removed labels with "".
func labelHistoryJSON(modified map[string]string, removed []string) string {
	if len(modified) == 0 && len(removed) == 0 {
		return ""
	}
	history := make(map[string]string, len(modified)+len(removed))
	for k, v := range modified {
		history[k] = v
	}
	for _, name := range removed {
		history[name] = ""
	}
	b, _ := json.Marshal(history)
	return string(b)
}
