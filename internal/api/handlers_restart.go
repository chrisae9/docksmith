package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/docker/docker/api/types/container"
)

const (
	// healthCheckPollInterval is how often to check container status
	healthCheckPollInterval = 1 * time.Second
	// healthCheckTimeout is the max time to wait for a container to be healthy/running
	healthCheckTimeout = 30 * time.Second
)

// RestartContainerRequest represents a request to restart a container
type RestartContainerRequest struct {
	ContainerName string `json:"container_name"`
}

// RestartStackRequest represents a request to restart all containers in a stack
type RestartStackRequest struct {
	StackName string `json:"stack_name"`
}

// RestartResponse represents the response from a restart operation
type RestartResponse struct {
	Success         bool     `json:"success"`
	Message         string   `json:"message"`
	ContainerNames  []string `json:"container_names"`
	DependentsNames []string `json:"dependents_restarted,omitempty"`
	BlockedNames    []string `json:"dependents_blocked,omitempty"`
	Errors          []string `json:"errors,omitempty"`
}

// waitForContainerHealthy waits for a container to become healthy or running.
// For containers with health checks, polls until status is "healthy" or times out.
// For containers without health checks, polls until container is running.
func (s *Server) waitForContainerHealthy(ctx context.Context, containerName string) error {
	ctx, cancel := context.WithTimeout(ctx, healthCheckTimeout)
	defer cancel()

	ticker := time.NewTicker(healthCheckPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("health check timeout for %s", containerName)
		case <-ticker.C:
			inspect, err := s.dockerService.GetClient().ContainerInspect(ctx, containerName)
			if err != nil {
				// Container might not exist yet, keep trying
				continue
			}

			// Check if container has a health check defined
			hasHealthCheck := inspect.State != nil && inspect.State.Health != nil

			if hasHealthCheck {
				if inspect.State.Health.Status == "healthy" {
					log.Printf("Container %s is healthy", containerName)
					return nil
				}
				if inspect.State.Health.Status == "unhealthy" {
					return fmt.Errorf("container %s is unhealthy", containerName)
				}
				// Still starting, keep polling
			} else {
				// No health check - just verify it's running
				if inspect.State != nil && inspect.State.Running {
					log.Printf("Container %s is running", containerName)
					return nil
				}
			}
		}
	}
}

// findDependentContainers finds all containers that depend on the given container
func (s *Server) findDependentContainers(ctx context.Context, containerName string) ([]string, error) {
	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var dependents []string
	for _, c := range containers {
		if restartAfter, ok := c.Labels[scripts.RestartAfterLabel]; ok && restartAfter != "" {
			dependencies := strings.Split(restartAfter, ",")
			for _, dep := range dependencies {
				dep = strings.TrimSpace(dep)
				if dep == containerName {
					dependents = append(dependents, c.Name)
					break
				}
			}
		}
	}
	return dependents, nil
}

// runPreChecksForContainers runs pre-update checks for all specified containers
// Returns (allPassed bool, failedContainers []string, errors []string)
func (s *Server) runPreChecksForContainers(ctx context.Context, containerNames []string, force bool) (bool, []string, []string) {
	if force {
		return true, nil, nil
	}

	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		log.Printf("Failed to list containers: %v", err)
		return false, nil, []string{"Failed to list containers"}
	}

	containerMap := docker.CreateContainerMap(containers)

	var failedContainers []string
	var errors []string

	for _, name := range containerNames {
		c := containerMap[name]
		if c == nil {
			continue
		}

		ran, passed, err := s.executeContainerPreUpdateCheck(ctx, c, false)
		if ran && !passed {
			log.Printf("Pre-update check failed for %s: %v", name, err)
			failedContainers = append(failedContainers, name)
			errors = append(errors, fmt.Sprintf("%s: %v", name, err))
		} else if ran && passed {
			log.Printf("Pre-update check passed for %s", name)
		}
	}

	return len(failedContainers) == 0, failedContainers, errors
}

// restartDependentContainers finds and restarts containers that depend on the given container
// PRE-CHECKS ARE ALREADY DONE - this only restarts
func (s *Server) restartDependentContainers(ctx context.Context, containerName string) ([]string, []string) {
	dependents, err := s.findDependentContainers(ctx, containerName)
	if err != nil || len(dependents) == 0 {
		return nil, nil
	}

	log.Printf("Restarting %d dependent container(s) for %s: %v", len(dependents), containerName, dependents)

	var restarted []string
	var errors []string

	for _, dep := range dependents {
		log.Printf("Restarting dependent container: %s", dep)
		if restartErr := s.dockerService.GetClient().ContainerRestart(ctx, dep, container.StopOptions{}); restartErr != nil {
			errMsg := fmt.Sprintf("Failed to restart dependent %s: %v", dep, restartErr)
			log.Printf("%s", errMsg)
			errors = append(errors, errMsg)
		} else {
			// Wait for dependent to be healthy/running
			if healthErr := s.waitForContainerHealthy(ctx, dep); healthErr != nil {
				log.Printf("Health check warning for dependent %s: %v", dep, healthErr)
				// Don't fail - container was restarted
			}
			log.Printf("Successfully restarted dependent container: %s", dep)
			restarted = append(restarted, dep)
		}
	}

	return restarted, errors
}

// handleStartRestart initiates a restart operation via the orchestrator with SSE progress events.
// This is the preferred method for restarting containers as it provides real-time progress updates.
func (s *Server) handleStartRestart(w http.ResponseWriter, r *http.Request) {
	if s.updateOrchestrator == nil {
		RespondInternalError(w, fmt.Errorf("restart service unavailable"))
		return
	}

	containerName := r.PathValue("name")
	if containerName == "" {
		RespondBadRequest(w, fmt.Errorf("container name is required"))
		return
	}

	force := r.URL.Query().Get("force") == "true"

	log.Printf("Starting restart operation for container: %s (force=%v)", containerName, force)

	ctx := r.Context()

	// Start restart via orchestrator - returns operation ID for SSE tracking
	operationID, err := s.updateOrchestrator.RestartSingleContainer(ctx, containerName, force)
	if err != nil {
		log.Printf("Failed to start restart for %s: %v", containerName, err)
		RespondInternalError(w, err)
		return
	}

	log.Printf("Restart operation started: %s for container %s", operationID, containerName)

	RespondSuccess(w, map[string]any{
		"operation_id":   operationID,
		"container_name": containerName,
		"force":          force,
		"status":         "started",
	})
}

// handleRestartContainer restarts a single container
func (s *Server) handleRestartContainer(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if containerName == "" {
		RespondBadRequest(w, fmt.Errorf("container name is required"))
		return
	}

	// Check for force parameter
	force := r.URL.Query().Get("force") == "true"

	log.Printf("Restarting container: %s (force=%v)", containerName, force)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Find all dependent containers
	dependents, err := s.findDependentContainers(ctx, containerName)
	if err != nil {
		log.Printf("Failed to find dependents: %v", err)
	}

	// Build list of all containers to check
	allContainers := []string{containerName}
	allContainers = append(allContainers, dependents...)

	// Run pre-checks on ALL containers FIRST (before any restart)
	allPassed, failedContainers, checkErrors := s.runPreChecksForContainers(ctx, allContainers, force)
	if !allPassed {
		log.Printf("Pre-update checks failed for containers: %v", failedContainers)
		errMsg := fmt.Sprintf("Pre-update check failed for: %s", strings.Join(failedContainers, ", "))
		if len(checkErrors) > 0 {
			errMsg = checkErrors[0] // Return the first error message
		}
		// Return error with data about affected containers so UI can show correct total
		RespondErrorWithData(w, http.StatusInternalServerError, fmt.Errorf("%s (use force to skip)", errMsg), RestartResponse{
			Success:         false,
			ContainerNames:  []string{containerName},
			DependentsNames: dependents,
		})
		return
	}

	// All checks passed - now restart the main container
	if err := s.dockerService.GetClient().ContainerRestart(ctx, containerName, container.StopOptions{}); err != nil {
		log.Printf("Failed to restart container %s: %v", containerName, err)
		RespondInternalError(w, fmt.Errorf("failed to restart container: %w", err))
		return
	}

	// Wait for container to be healthy/running
	if err := s.waitForContainerHealthy(ctx, containerName); err != nil {
		log.Printf("Health check failed for %s: %v", containerName, err)
		// Don't fail the operation - container was restarted, just health check timed out
	}

	log.Printf("Successfully restarted container: %s", containerName)

	// Restart dependent containers (no pre-checks needed - already done)
	restarted, depErrors := s.restartDependentContainers(ctx, containerName)

	message := fmt.Sprintf("Container %s restarted successfully", containerName)
	if len(restarted) > 0 {
		message = fmt.Sprintf("Container %s and %d dependent(s) restarted successfully", containerName, len(restarted))
	}

	response := RestartResponse{
		Success:         true,
		Message:         message,
		ContainerNames:  []string{containerName},
		DependentsNames: restarted,
		BlockedNames:    nil, // No blocked anymore - we check all first
		Errors:          depErrors,
	}

	RespondSuccess(w, response)
}

// handleRestartStack restarts all containers in a stack
func (s *Server) handleRestartStack(w http.ResponseWriter, r *http.Request) {
	stackName := r.PathValue("name")
	if stackName == "" {
		RespondBadRequest(w, fmt.Errorf("stack name is required"))
		return
	}

	log.Printf("Restarting stack: %s", stackName)

	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()

	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to list containers: %w", err))
		return
	}

	// Filter containers by stack
	var stackContainers []string
	for _, cont := range containers {
		if stack, ok := cont.Labels[composeProjectLabel]; ok && stack == stackName {
			stackContainers = append(stackContainers, cont.Name)
		}
	}

	if len(stackContainers) == 0 {
		RespondNotFound(w, fmt.Errorf("no containers found in stack: %s", stackName))
		return
	}

	log.Printf("Found %d containers in stack %s", len(stackContainers), stackName)

	// Find all dependents for each container in the stack
	allContainersToCheck := make([]string, 0)
	allContainersToCheck = append(allContainersToCheck, stackContainers...)

	for _, containerName := range stackContainers {
		dependents, _ := s.findDependentContainers(ctx, containerName)
		allContainersToCheck = append(allContainersToCheck, dependents...)
	}

	// Run pre-checks on ALL containers FIRST
	allPassed, failedContainers, checkErrors := s.runPreChecksForContainers(ctx, allContainersToCheck, false)
	if !allPassed {
		log.Printf("Pre-update checks failed for stack %s: %v", stackName, failedContainers)
		errMsg := fmt.Sprintf("Pre-update check failed for: %s", strings.Join(failedContainers, ", "))
		if len(checkErrors) > 0 {
			errMsg = checkErrors[0]
		}
		RespondInternalError(w, fmt.Errorf("%s", errMsg))
		return
	}

	// All checks passed - restart each container
	var errors []string
	var allDependents []string
	successCount := 0

	for _, containerName := range stackContainers {
		log.Printf("Restarting container %s in stack %s", containerName, stackName)
		if err := s.dockerService.GetClient().ContainerRestart(ctx, containerName, container.StopOptions{}); err != nil {
			errMsg := fmt.Sprintf("Failed to restart %s: %v", containerName, err)
			log.Printf("%s", errMsg)
			errors = append(errors, errMsg)
		} else {
			// Wait for container to be healthy/running
			if healthErr := s.waitForContainerHealthy(ctx, containerName); healthErr != nil {
				log.Printf("Health check warning for %s: %v", containerName, healthErr)
				// Don't fail - container was restarted
			}
			successCount++
			log.Printf("Successfully restarted %s", containerName)

			// Restart dependent containers (pre-checks already done)
			restarted, depErrors := s.restartDependentContainers(ctx, containerName)
			allDependents = append(allDependents, restarted...)
			errors = append(errors, depErrors...)
		}
	}

	success := successCount > 0
	message := fmt.Sprintf("Restarted %d/%d containers in stack %s", successCount, len(stackContainers), stackName)
	if len(allDependents) > 0 {
		message = fmt.Sprintf("Restarted %d/%d containers in stack %s and %d dependent(s)", successCount, len(stackContainers), stackName, len(allDependents))
	}

	if len(errors) > 0 {
		log.Printf("Stack restart completed with errors: %s", message)
	} else {
		log.Printf("Stack restart completed successfully: %s", message)
	}

	response := RestartResponse{
		Success:         success,
		Message:         message,
		ContainerNames:  stackContainers,
		DependentsNames: allDependents,
		Errors:          errors,
	}

	if success {
		RespondSuccess(w, response)
	} else {
		RespondInternalError(w, fmt.Errorf("%s", message))
	}
}

// handleRestartContainerBody handles restart via POST body (alternative to path param)
func (s *Server) handleRestartContainerBody(w http.ResponseWriter, r *http.Request) {
	var req RestartContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, fmt.Errorf("invalid request body: %w", err))
		return
	}

	if req.ContainerName == "" {
		RespondBadRequest(w, fmt.Errorf("container_name is required"))
		return
	}

	log.Printf("Restarting container: %s", req.ContainerName)

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Find all dependent containers
	dependents, err := s.findDependentContainers(ctx, req.ContainerName)
	if err != nil {
		log.Printf("Failed to find dependents: %v", err)
	}

	// Build list of all containers to check
	allContainers := []string{req.ContainerName}
	allContainers = append(allContainers, dependents...)

	// Run pre-checks on ALL containers FIRST (never force in body endpoint)
	allPassed, failedContainers, checkErrors := s.runPreChecksForContainers(ctx, allContainers, false)
	if !allPassed {
		log.Printf("Pre-update checks failed for containers: %v", failedContainers)
		errMsg := fmt.Sprintf("Pre-update check failed for: %s", strings.Join(failedContainers, ", "))
		if len(checkErrors) > 0 {
			errMsg = checkErrors[0]
		}
		RespondInternalError(w, fmt.Errorf("%s", errMsg))
		return
	}

	// All checks passed - restart the container
	if err := s.dockerService.GetClient().ContainerRestart(ctx, req.ContainerName, container.StopOptions{}); err != nil {
		log.Printf("Failed to restart container %s: %v", req.ContainerName, err)
		RespondInternalError(w, fmt.Errorf("failed to restart container: %w", err))
		return
	}

	// Wait for container to be healthy/running
	if err := s.waitForContainerHealthy(ctx, req.ContainerName); err != nil {
		log.Printf("Health check failed for %s: %v", req.ContainerName, err)
		// Don't fail the operation - container was restarted, just health check timed out
	}

	log.Printf("Successfully restarted container: %s", req.ContainerName)

	// Restart dependent containers (pre-checks already done)
	restarted, depErrors := s.restartDependentContainers(ctx, req.ContainerName)

	message := fmt.Sprintf("Container %s restarted successfully", req.ContainerName)
	if len(restarted) > 0 {
		message = fmt.Sprintf("Container %s and %d dependent(s) restarted successfully", req.ContainerName, len(restarted))
	}

	response := RestartResponse{
		Success:         true,
		Message:         message,
		ContainerNames:  []string{req.ContainerName},
		DependentsNames: restarted,
		Errors:          depErrors,
	}

	RespondSuccess(w, response)
}
