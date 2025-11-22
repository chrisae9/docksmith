package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/docker/docker/api/types/container"
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

// restartDependentContainers finds and restarts containers that depend on the given container
// If force is false, runs pre-update checks before restarting each dependent
func (s *Server) restartDependentContainers(ctx context.Context, containerName string, force bool) ([]string, []string, []string) {
	dependents, err := s.dockerService.FindDependentContainers(ctx, containerName, scripts.RestartDependsOnLabel)
	if err != nil {
		log.Printf("Failed to find dependent containers for %s: %v", containerName, err)
		return nil, nil, nil
	}

	if len(dependents) == 0 {
		return nil, nil, nil
	}

	log.Printf("Found %d dependent container(s) for %s: %v", len(dependents), containerName, dependents)

	// Get full container info for pre-update checks
	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		log.Printf("Failed to list containers: %v", err)
		return nil, nil, nil
	}

	containerMap := make(map[string]*docker.Container)
	for i := range containers {
		containerMap[containers[i].Name] = &containers[i]
	}

	var restarted []string
	var blocked []string
	var errors []string

	for _, dep := range dependents {
		depContainer := containerMap[dep]
		if depContainer == nil {
			errMsg := fmt.Sprintf("Dependent container %s not found", dep)
			log.Printf("%s", errMsg)
			errors = append(errors, errMsg)
			continue
		}

		// Run pre-update check if not forced
		if !force {
			if scriptPath, ok := depContainer.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
				log.Printf("Running pre-update check for dependent %s", dep)
				if err := s.runPreUpdateCheck(ctx, depContainer, scriptPath); err != nil {
					errMsg := fmt.Sprintf("%s: %v", dep, err)
					log.Printf("Pre-update check failed for %s: %v", dep, err)
					blocked = append(blocked, dep)
					errors = append(errors, errMsg)
					continue
				}
				log.Printf("Pre-update check passed for %s", dep)
			}
		}

		log.Printf("Restarting dependent container: %s", dep)
		err := s.dockerService.GetClient().ContainerRestart(ctx, dep, container.StopOptions{})
		if err != nil {
			errMsg := fmt.Sprintf("Failed to restart dependent %s: %v", dep, err)
			log.Printf("%s", errMsg)
			errors = append(errors, errMsg)
		} else {
			log.Printf("Successfully restarted dependent container: %s", dep)
			restarted = append(restarted, dep)
		}
	}

	return restarted, blocked, errors
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
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Restart the container using Docker client
	err := s.dockerService.GetClient().ContainerRestart(ctx, containerName, container.StopOptions{})
	if err != nil {
		log.Printf("Failed to restart container %s: %v", containerName, err)
		RespondInternalError(w, fmt.Errorf("failed to restart container: %w", err))
		return
	}

	log.Printf("Successfully restarted container: %s", containerName)

	// Restart dependent containers
	dependents, blocked, depErrors := s.restartDependentContainers(ctx, containerName, force)

	message := fmt.Sprintf("Container %s restarted successfully", containerName)
	if len(dependents) > 0 {
		message = fmt.Sprintf("Container %s and %d dependent(s) restarted successfully", containerName, len(dependents))
	}
	if len(blocked) > 0 {
		message += fmt.Sprintf(" (%d blocked by pre-checks)", len(blocked))
	}

	response := RestartResponse{
		Success:         true,
		Message:         message,
		ContainerNames:  []string{containerName},
		DependentsNames: dependents,
		BlockedNames:    blocked,
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

	// Get all containers in the stack
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
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

	// Restart each container
	restartCtx, restartCancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer restartCancel()

	var errors []string
	var allDependents []string
	successCount := 0

	for _, containerName := range stackContainers {
		log.Printf("Restarting container %s in stack %s", containerName, stackName)
		err := s.dockerService.GetClient().ContainerRestart(restartCtx, containerName, container.StopOptions{})
		if err != nil {
			errMsg := fmt.Sprintf("Failed to restart %s: %v", containerName, err)
			log.Printf("%s", errMsg)
			errors = append(errors, errMsg)
		} else {
			successCount++
			log.Printf("Successfully restarted %s", containerName)

			// Restart dependent containers (never force in stack restart)
			dependents, blocked, depErrors := s.restartDependentContainers(restartCtx, containerName, false)
			allDependents = append(allDependents, dependents...)
			if len(blocked) > 0 {
				errors = append(errors, fmt.Sprintf("%d dependent(s) of %s blocked by pre-checks", len(blocked), containerName))
			}
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

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := s.dockerService.GetClient().ContainerRestart(ctx, req.ContainerName, container.StopOptions{})
	if err != nil {
		log.Printf("Failed to restart container %s: %v", req.ContainerName, err)
		RespondInternalError(w, fmt.Errorf("failed to restart container: %w", err))
		return
	}

	log.Printf("Successfully restarted container: %s", req.ContainerName)

	// Restart dependent containers (never force in body endpoint)
	dependents, blocked, depErrors := s.restartDependentContainers(ctx, req.ContainerName, false)

	message := fmt.Sprintf("Container %s restarted successfully", req.ContainerName)
	if len(dependents) > 0 {
		message = fmt.Sprintf("Container %s and %d dependent(s) restarted successfully", req.ContainerName, len(dependents))
	}
	if len(blocked) > 0 {
		message += fmt.Sprintf(" (%d blocked by pre-checks)", len(blocked))
	}

	response := RestartResponse{
		Success:         true,
		Message:         message,
		ContainerNames:  []string{req.ContainerName},
		DependentsNames: dependents,
		BlockedNames:    blocked,
		Errors:          depErrors,
	}

	RespondSuccess(w, response)
}
