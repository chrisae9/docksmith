package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/output"
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
	Success        bool     `json:"success"`
	Message        string   `json:"message"`
	ContainerNames []string `json:"container_names"`
	Errors         []string `json:"errors,omitempty"`
}

// handleRestartContainer restarts a single container
func (s *Server) handleRestartContainer(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if containerName == "" {
		w.WriteHeader(http.StatusBadRequest)
		output.WriteJSONError(w, fmt.Errorf("container name is required"))
		return
	}

	log.Printf("Restarting container: %s", containerName)

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Restart the container using Docker client
	err := s.dockerService.GetClient().ContainerRestart(ctx, containerName, container.StopOptions{})
	if err != nil {
		log.Printf("Failed to restart container %s: %v", containerName, err)
		w.WriteHeader(http.StatusInternalServerError)
		output.WriteJSONError(w, fmt.Errorf("failed to restart container: %w", err))
		return
	}

	log.Printf("Successfully restarted container: %s", containerName)

	response := RestartResponse{
		Success:        true,
		Message:        fmt.Sprintf("Container %s restarted successfully", containerName),
		ContainerNames: []string{containerName},
	}

	output.WriteJSONData(w, response)
}

// handleRestartStack restarts all containers in a stack
func (s *Server) handleRestartStack(w http.ResponseWriter, r *http.Request) {
	stackName := r.PathValue("name")
	if stackName == "" {
		w.WriteHeader(http.StatusBadRequest)
		output.WriteJSONError(w, fmt.Errorf("stack name is required"))
		return
	}

	log.Printf("Restarting stack: %s", stackName)

	// Get all containers in the stack
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	containers, err := s.dockerService.ListContainers(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		output.WriteJSONError(w, fmt.Errorf("failed to list containers: %w", err))
		return
	}

	// Filter containers by stack
	var stackContainers []string
	for _, cont := range containers {
		if stack, ok := cont.Labels["com.docker.compose.project"]; ok && stack == stackName {
			stackContainers = append(stackContainers, cont.Name)
		}
	}

	if len(stackContainers) == 0 {
		w.WriteHeader(http.StatusNotFound)
		output.WriteJSONError(w, fmt.Errorf("no containers found in stack: %s", stackName))
		return
	}

	log.Printf("Found %d containers in stack %s", len(stackContainers), stackName)

	// Restart each container
	restartCtx, restartCancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer restartCancel()

	var errors []string
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
		}
	}

	success := successCount > 0
	message := fmt.Sprintf("Restarted %d/%d containers in stack %s", successCount, len(stackContainers), stackName)

	if len(errors) > 0 {
		log.Printf("Stack restart completed with errors: %s", message)
	} else {
		log.Printf("Stack restart completed successfully: %s", message)
	}

	response := RestartResponse{
		Success:        success,
		Message:        message,
		ContainerNames: stackContainers,
		Errors:         errors,
	}

	if success {
		output.WriteJSONData(w, response)
	} else {
		w.WriteHeader(http.StatusInternalServerError)
		output.WriteJSONData(w, response)
	}
}

// handleRestartContainerBody handles restart via POST body (alternative to path param)
func (s *Server) handleRestartContainerBody(w http.ResponseWriter, r *http.Request) {
	var req RestartContainerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		output.WriteJSONError(w, fmt.Errorf("invalid request body: %w", err))
		return
	}

	if req.ContainerName == "" {
		w.WriteHeader(http.StatusBadRequest)
		output.WriteJSONError(w, fmt.Errorf("container_name is required"))
		return
	}

	log.Printf("Restarting container: %s", req.ContainerName)

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	err := s.dockerService.GetClient().ContainerRestart(ctx, req.ContainerName, container.StopOptions{})
	if err != nil {
		log.Printf("Failed to restart container %s: %v", req.ContainerName, err)
		w.WriteHeader(http.StatusInternalServerError)
		output.WriteJSONError(w, fmt.Errorf("failed to restart container: %w", err))
		return
	}

	log.Printf("Successfully restarted container: %s", req.ContainerName)

	response := RestartResponse{
		Success:        true,
		Message:        fmt.Sprintf("Container %s restarted successfully", req.ContainerName),
		ContainerNames: []string{req.ContainerName},
	}

	output.WriteJSONData(w, response)
}
