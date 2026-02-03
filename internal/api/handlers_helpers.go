package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"

	"github.com/chis/docksmith/internal/docker"
)

// Sentinel errors for missing services
var (
	errNoStorage            = errors.New("storage service not available")
	errNoUpdateOrchestrator = errors.New("update orchestrator not available")
	errNoScriptManager      = errors.New("script manager not available")
)

// parseIntParam parses an integer query parameter with a default value
func parseIntParam(r *http.Request, name string, defaultVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return n
}

// parsePositiveIntParam parses a positive integer query parameter with a default value.
// Returns defaultVal if the parameter is missing, invalid, or not positive.
func parsePositiveIntParam(r *http.Request, name string, defaultVal int) int {
	val := r.URL.Query().Get(name)
	if val == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(val)
	if err != nil || n <= 0 {
		return defaultVal
	}
	return n
}

// parseBoolParam parses a boolean query parameter
func parseBoolParam(r *http.Request, name string) bool {
	return r.URL.Query().Get(name) == "true"
}

// validateRequired checks that a required parameter is not empty.
// Returns true if valid, false if empty (and writes error response).
func validateRequired(w http.ResponseWriter, name, value string) bool {
	if value == "" {
		RespondBadRequest(w, fmt.Errorf("%s is required", name))
		return false
	}
	return true
}

// validateRegexPattern validates a regular expression pattern for tag filtering
func validateRegexPattern(pattern string) error {
	if pattern == "" {
		return nil // Empty is valid (no filtering)
	}

	// Security: limit pattern length to prevent resource exhaustion
	if len(pattern) > MaxRegexPatternLength {
		return fmt.Errorf("pattern too long (max %d characters)", MaxRegexPatternLength)
	}

	// Try to compile the regex
	_, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid regex: %w", err)
	}

	return nil
}

// findContainerByName searches for a container by name.
// This is a convenience wrapper around docker.Service.GetContainerByName.
func (s *Server) findContainerByName(ctx context.Context, containerName string) (*docker.Container, error) {
	return s.dockerService.GetContainerByName(ctx, containerName)
}

// requireStorage checks if storage service is available and returns an error response if not
func (s *Server) requireStorage(w http.ResponseWriter) bool {
	if s.storageService == nil {
		RespondInternalError(w, errNoStorage)
		return false
	}
	return true
}

// requireScriptManager checks if script manager is available and returns an error response if not
func (s *Server) requireScriptManager(w http.ResponseWriter) bool {
	if s.scriptManager == nil {
		RespondInternalError(w, errNoScriptManager)
		return false
	}
	return true
}

// requireUpdateOrchestrator checks if update orchestrator is available and returns an error response if not
func (s *Server) requireUpdateOrchestrator(w http.ResponseWriter) bool {
	if s.updateOrchestrator == nil {
		RespondInternalError(w, errNoUpdateOrchestrator)
		return false
	}
	return true
}
