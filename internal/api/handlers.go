package api

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/storage"
	"github.com/google/uuid"
)

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	RespondSuccess(w, map[string]any{
		"status": "healthy",
		"services": map[string]bool{
			"docker":  s.dockerService != nil,
			"storage": s.storageService != nil,
		},
	})
}

// handleCheck performs container discovery and update checking
// Triggers a manual check and returns cached results
func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// If background checker is available, trigger a manual check
	if s.backgroundChecker != nil {
		// Clear cache to force fresh registry queries
		s.discoveryOrchestrator.ClearCache()
		// Mark that cache was cleared so timestamp gets updated
		s.backgroundChecker.MarkCacheCleared()
		s.backgroundChecker.TriggerCheck()
		// Wait a moment for the check to start
		time.Sleep(100 * time.Millisecond)
		// Return cached results (will include the triggered check once it completes)
		s.handleGetStatus(w, r)
		return
	}

	// Fallback to direct discovery if no background checker
	result, err := s.discoveryOrchestrator.DiscoverAndCheck(ctx)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	// Return identical JSON structure as CLI
	RespondSuccess(w, result)
}

// handleTriggerCheck triggers a background check without clearing cache
// This is used by the "Background Refresh" button to update the discovery
// using existing cached registry data (respects CACHE_TTL)
func (s *Server) handleTriggerCheck(w http.ResponseWriter, r *http.Request) {
	if s.backgroundChecker == nil {
		RespondInternalError(w, fmt.Errorf("background checker not available"))
		return
	}

	// Trigger check WITHOUT clearing cache
	// This allows the background check to use cached registry data
	s.backgroundChecker.TriggerCheck()

	// Wait for check to start
	time.Sleep(100 * time.Millisecond)

	// Return success
	RespondSuccess(w, map[string]any{
		"message": "Background check triggered",
	})
}

// handleContainerRecheck performs a synchronous recheck for a single container
// This runs the pre-update check script fresh and returns the updated status
func (s *Server) handleContainerRecheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	containerName := r.PathValue("name")

	if containerName == "" {
		RespondBadRequest(w, fmt.Errorf("container name is required"))
		return
	}

	// Get the container info synchronously using the orchestrator
	result, err := s.discoveryOrchestrator.DiscoverAndCheckSingle(ctx, containerName)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	if result == nil {
		RespondNotFound(w, fmt.Errorf("container '%s' not found", containerName))
		return
	}

	RespondSuccess(w, result)
}

// handleOperations returns update operations history
// This is the EXACT same logic as: docksmith operations --json
func (s *Server) handleOperations(w http.ResponseWriter, r *http.Request) {
	if !s.requireStorage(w) {
		return
	}

	ctx := r.Context()

	// Parse query parameters (same as CLI flags)
	limit := parseIntParam(r, "limit", 50)
	status := r.URL.Query().Get("status")
	container := r.URL.Query().Get("container")

	var operations []storage.UpdateOperation
	var err error

	// Same filtering logic as CLI
	if container != "" {
		operations, err = s.storageService.GetUpdateOperationsByContainer(ctx, container, limit)
	} else if status != "" {
		operations, err = s.storageService.GetUpdateOperationsByStatus(ctx, status, limit)
	} else {
		operations, err = s.storageService.GetUpdateOperations(ctx, limit)
	}

	if err != nil {
		RespondInternalError(w, err)
		return
	}

	// Same JSON structure as CLI
	RespondSuccess(w, map[string]any{
		"operations": operations,
		"count":      len(operations),
	})
}

// handleOperationByID returns a single operation by ID
func (s *Server) handleOperationByID(w http.ResponseWriter, r *http.Request) {
	if !s.requireStorage(w) {
		return
	}

	ctx := r.Context()
	operationID := r.PathValue("id")

	operation, found, err := s.storageService.GetUpdateOperation(ctx, operationID)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	if !found {
		RespondNotFound(w, fmt.Errorf("operation not found"))
		return
	}

	RespondSuccess(w, operation)
}

// handleHistory returns unified check and update history
// This is the EXACT same logic as: docksmith history --json
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	if !s.requireStorage(w) {
		return
	}

	ctx := r.Context()

	limit := parseIntParam(r, "limit", 100)
	historyType := r.URL.Query().Get("type")

	// Fetch data - same as CLI
	var checkHistory []storage.CheckHistoryEntry
	var updateLog []storage.UpdateLogEntry
	var err error

	if historyType == "" || historyType == "check" {
		checkHistory, err = s.storageService.GetAllCheckHistory(ctx, limit)
		if err != nil {
			RespondInternalError(w, err)
			return
		}
	}

	if historyType == "" || historyType == "update" {
		updateLog, err = s.storageService.GetAllUpdateLog(ctx, limit)
		if err != nil {
			RespondInternalError(w, err)
			return
		}
	}

	// Convert to unified format - same as CLI history command
	entries := mergeHistory(checkHistory, updateLog)

	RespondSuccess(w, map[string]any{
		"history": entries,
		"count":   len(entries),
	})
}

// handlePolicies returns rollback policies
func (s *Server) handlePolicies(w http.ResponseWriter, r *http.Request) {
	if !s.requireStorage(w) {
		return
	}

	ctx := r.Context()

	// Get global policy
	globalPolicy, _, err := s.storageService.GetRollbackPolicy(ctx, "global", "")
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"global_policy": globalPolicy,
	})
}

// handleUpdate triggers a container update
// This reuses the same UpdateOrchestrator as CLI
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpdateOrchestrator(w) {
		return
	}

	ctx := r.Context()

	// Parse request body
	var req struct {
		ContainerName string `json:"container_name"`
		TargetVersion string `json:"target_version"`
	}

	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if !validateRequired(w, "container_name", req.ContainerName) {
		return
	}

	// Start update - same function as CLI
	operationID, err := s.updateOrchestrator.UpdateSingleContainer(ctx, req.ContainerName, req.TargetVersion)
	if err != nil {
		RespondOrchestratorError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"operation_id":   operationID,
		"container_name": req.ContainerName,
		"target_version": req.TargetVersion,
		"status":         "started",
	})
}

// handleBatchUpdate triggers updates for multiple containers, grouped by stack
// Containers in the same stack are updated together to respect dependencies
// Different stacks run in parallel
func (s *Server) handleBatchUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpdateOrchestrator(w) {
		return
	}

	ctx := r.Context()

	// Parse request body
	var req struct {
		Containers []struct {
			Name               string `json:"name"`
			TargetVersion      string `json:"target_version"`
			Stack              string `json:"stack"`
			Force              bool   `json:"force,omitempty"`
			ChangeType         *int   `json:"change_type,omitempty"`
			OldResolvedVersion string `json:"old_resolved_version"`
			NewResolvedVersion string `json:"new_resolved_version"`
		} `json:"containers"`
	}

	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if len(req.Containers) == 0 {
		RespondBadRequest(w, fmt.Errorf("containers array is required"))
		return
	}

	// Group containers by stack
	stackGroups := make(map[string][]string)
	targetVersions := make(map[string]string)
	containerMeta := make(map[string]storage.BatchContainerDetail)
	forceContainers := make(map[string]bool)

	for _, c := range req.Containers {
		stack := c.Stack
		if stack == "" {
			stack = "__standalone__"
		}
		stackGroups[stack] = append(stackGroups[stack], c.Name)
		if c.TargetVersion != "" {
			targetVersions[c.Name] = c.TargetVersion
		}
		containerMeta[c.Name] = storage.BatchContainerDetail{
			ChangeType:         c.ChangeType,
			OldResolvedVersion: c.OldResolvedVersion,
			NewResolvedVersion: c.NewResolvedVersion,
		}
		if c.Force {
			forceContainers[c.Name] = true
		}
	}

	// Generate a batch group ID to link all operations from this user action
	batchGroupID := uuid.New().String()

	// For each stack group, start an update operation
	operations := make([]map[string]any, 0)

	for stack, containerNames := range stackGroups {
		if len(containerNames) == 1 {
			// Single container - use regular update with group ID
			opID, err := s.updateOrchestrator.UpdateSingleContainerInGroup(ctx, containerNames[0], targetVersions[containerNames[0]], batchGroupID, containerMeta, forceContainers)
			if err != nil {
				log.Printf("Failed to start update for %s: %v", containerNames[0], err)
				operations = append(operations, map[string]any{
					"stack":      stack,
					"containers": containerNames,
					"status":     "failed",
					"error":      err.Error(),
				})
			} else {
				operations = append(operations, map[string]any{
					"stack":        stack,
					"containers":   containerNames,
					"operation_id": opID,
					"status":       "started",
				})
			}
		} else {
			// Multiple containers in same stack - use batch update with group ID
			opID, err := s.updateOrchestrator.UpdateBatchContainersInGroup(ctx, containerNames, targetVersions, batchGroupID, containerMeta, forceContainers)
			if err != nil {
				log.Printf("Failed to start batch update for stack %s: %v", stack, err)
				operations = append(operations, map[string]any{
					"stack":      stack,
					"containers": containerNames,
					"status":     "failed",
					"error":      err.Error(),
				})
			} else {
				operations = append(operations, map[string]any{
					"stack":        stack,
					"containers":   containerNames,
					"operation_id": opID,
					"status":       "started",
				})
			}
		}
	}

	RespondSuccess(w, map[string]any{
		"operations":     operations,
		"batch_group_id": batchGroupID,
		"status":         "started",
	})
}

// handleRollback triggers a rollback operation
func (s *Server) handleRollback(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpdateOrchestrator(w) {
		return
	}

	ctx := r.Context()

	// Parse request body
	var req struct {
		OperationID string `json:"operation_id"`
		Force       bool   `json:"force"`
	}

	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if !validateRequired(w, "operation_id", req.OperationID) {
		return
	}

	// Trigger the rollback operation
	rollbackOpID, err := s.updateOrchestrator.RollbackOperation(ctx, req.OperationID, req.Force)
	if err != nil {
		log.Printf("Rollback failed: %v", err)
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"operation_id":          rollbackOpID,
		"original_operation_id": req.OperationID,
		"message":               "Rollback initiated",
	})
}

// handleFixComposeMismatch triggers a fix for containers where the running image
// doesn't match what's specified in the compose file
func (s *Server) handleFixComposeMismatch(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpdateOrchestrator(w) {
		return
	}

	ctx := r.Context()
	containerName := r.PathValue("name")

	if !validateRequired(w, "container name", containerName) {
		return
	}

	operationID, err := s.updateOrchestrator.FixComposeMismatch(ctx, containerName)
	if err != nil {
		log.Printf("Fix compose mismatch failed for %s: %v", containerName, err)
		RespondOrchestratorError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"operation_id":   operationID,
		"container_name": containerName,
		"status":         "started",
		"message":        "Fix compose mismatch initiated",
	})
}

// handleOperationsByGroup returns all operations in a batch group
func (s *Server) handleOperationsByGroup(w http.ResponseWriter, r *http.Request) {
	if !s.requireStorage(w) {
		return
	}

	ctx := r.Context()
	groupID := r.PathValue("groupId")

	if !validateRequired(w, "group ID", groupID) {
		return
	}

	operations, err := s.storageService.GetUpdateOperationsByBatchGroup(ctx, groupID)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"batch_group_id": groupID,
		"operations":     operations,
		"count":          len(operations),
	})
}

// handleRollbackContainers rolls back specific containers from an operation
func (s *Server) handleRollbackContainers(w http.ResponseWriter, r *http.Request) {
	if !s.requireUpdateOrchestrator(w) {
		return
	}

	ctx := r.Context()

	var req struct {
		OperationID    string   `json:"operation_id"`
		ContainerNames []string `json:"container_names"`
		Force          bool     `json:"force"`
	}

	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if !validateRequired(w, "operation_id", req.OperationID) {
		return
	}

	if len(req.ContainerNames) == 0 {
		RespondBadRequest(w, fmt.Errorf("container_names array is required"))
		return
	}

	rollbackOpID, err := s.updateOrchestrator.RollbackContainers(ctx, req.OperationID, req.ContainerNames, req.Force)
	if err != nil {
		log.Printf("Per-container rollback failed: %v", err)
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"operation_id": rollbackOpID,
		"message":      "Per-container rollback initiated",
	})
}

// Helper functions

// HistoryEntry represents a unified history entry (same as CLI)
type HistoryEntry struct {
	Timestamp     time.Time `json:"timestamp"`
	Type          string    `json:"type"`
	ContainerName string    `json:"container_name"`
	Image         string    `json:"image,omitempty"`
	CurrentVer    string    `json:"current_version,omitempty"`
	LatestVer     string    `json:"latest_version,omitempty"`
	FromVer       string    `json:"from_version,omitempty"`
	ToVer         string    `json:"to_version,omitempty"`
	Status        string    `json:"status"`
	Operation     string    `json:"operation,omitempty"`
	Success       bool      `json:"success,omitempty"`
	Error         string    `json:"error,omitempty"`
}

// mergeHistory merges check and update history - same as CLI history command
func mergeHistory(checks []storage.CheckHistoryEntry, updates []storage.UpdateLogEntry) []HistoryEntry {
	var entries []HistoryEntry

	for _, check := range checks {
		entries = append(entries, HistoryEntry{
			Timestamp:     check.CheckTime,
			Type:          "check",
			ContainerName: check.ContainerName,
			Image:         check.Image,
			CurrentVer:    check.CurrentVersion,
			LatestVer:     check.LatestVersion,
			Status:        check.Status,
			Error:         check.Error,
		})
	}

	for _, update := range updates {
		status := "success"
		if !update.Success {
			status = "failed"
		}
		entries = append(entries, HistoryEntry{
			Timestamp:     update.Timestamp,
			Type:          "update",
			ContainerName: update.ContainerName,
			FromVer:       update.FromVersion,
			ToVer:         update.ToVersion,
			Operation:     update.Operation,
			Success:       update.Success,
			Status:        status,
			Error:         update.Error,
		})
	}

	return entries
}

// handleEvents provides Server-Sent Events for real-time update progress
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // Prevent proxy buffering

	// Flush headers immediately
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}
	flusher.Flush()

	// Disable write deadline for this long-lived SSE connection
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Time{})

	// Subscribe to all events
	eventChan, unsubscribe := s.eventBus.Subscribe("*")
	defer unsubscribe()

	log.Printf("SSE client connected")

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"status\":\"connected\"}\n\n")
	flusher.Flush()

	// Heartbeat keeps connection alive through proxies (Traefik idle timeout ~30s)
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	// Stream events
	for {
		select {
		case <-r.Context().Done():
			log.Printf("SSE client disconnected")
			return
		case <-heartbeat.C:
			// SSE comment — invisible to EventSource but keeps the connection alive
			fmt.Fprintf(w, ": keepalive\n\n")
			flusher.Flush()
		case event, ok := <-eventChan:
			if !ok {
				return
			}

			// Convert event to JSON
			eventData, err := events.MarshalEvent(event)
			if err != nil {
				log.Printf("Error marshaling event: %v", err)
				continue
			}

			// Send as SSE, reset heartbeat since we just sent data
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, eventData)
			flusher.Flush()
			heartbeat.Reset(15 * time.Second)
		}
	}
}

