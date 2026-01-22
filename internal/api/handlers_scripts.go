package api

import (
	"fmt"
	"net/http"
)

// handleScriptsList returns available scripts in /scripts folder
// This is the EXACT same logic as: docksmith scripts list --json
func (s *Server) handleScriptsList(w http.ResponseWriter, r *http.Request) {
	if !s.requireScriptManager(w) {
		return
	}

	scripts, err := s.scriptManager.DiscoverScripts()
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	// Same JSON structure as CLI
	RespondSuccess(w, map[string]any{
		"scripts": scripts,
		"count":   len(scripts),
	})
}

// handleScriptsAssigned returns script assignments
// This is the EXACT same logic as: docksmith scripts assigned --json
func (s *Server) handleScriptsAssigned(w http.ResponseWriter, r *http.Request) {
	if !s.requireScriptManager(w) {
		return
	}

	ctx := r.Context()

	assignments, err := s.scriptManager.ListAssignments(ctx, false)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	// Same JSON structure as CLI
	RespondSuccess(w, map[string]any{
		"assignments": assignments,
		"count":       len(assignments),
	})
}

// handleScriptsAssign assigns a script to a container
// This is the EXACT same logic as: docksmith scripts assign <container> <script> --json
func (s *Server) handleScriptsAssign(w http.ResponseWriter, r *http.Request) {
	if !s.requireScriptManager(w) {
		return
	}

	ctx := r.Context()

	// Parse request body
	var req struct {
		ContainerName string `json:"container_name"`
		ScriptPath    string `json:"script_path"`
	}

	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if req.ContainerName == "" || req.ScriptPath == "" {
		RespondBadRequest(w, fmt.Errorf("container_name and script_path are required"))
		return
	}

	// Assign script
	if err := s.scriptManager.AssignScript(ctx, req.ContainerName, req.ScriptPath, "ui"); err != nil {
		RespondInternalError(w, err)
		return
	}

	// Same JSON structure as CLI
	RespondSuccess(w, map[string]any{
		"success":   true,
		"container": req.ContainerName,
		"script":    req.ScriptPath,
		"message":   "Script assigned successfully. Restart container for changes to take effect.",
	})
}

// handleScriptsUnassign removes a script assignment from a container
// This is the EXACT same logic as: docksmith scripts unassign <container> --json
func (s *Server) handleScriptsUnassign(w http.ResponseWriter, r *http.Request) {
	if !s.requireScriptManager(w) {
		return
	}

	ctx := r.Context()

	// Get container name from path parameter
	containerName := r.PathValue("container")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	// Unassign script
	if err := s.scriptManager.UnassignScript(ctx, containerName); err != nil {
		RespondInternalError(w, err)
		return
	}

	// Same JSON structure as CLI
	RespondSuccess(w, map[string]any{
		"success":   true,
		"container": containerName,
		"message":   "Script unassigned successfully. Restart container for changes to take effect.",
	})
}

// handleSettingsIgnore sets the ignore flag for a container
// POST /api/settings/ignore with body: {"container_name": "...", "ignore": true/false}
func (s *Server) handleSettingsIgnore(w http.ResponseWriter, r *http.Request) {
	if !s.requireScriptManager(w) {
		return
	}

	ctx := r.Context()

	// Parse request body
	var req struct {
		ContainerName string `json:"container_name"`
		Ignore        bool   `json:"ignore"`
	}

	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if !validateRequired(w, "container_name", req.ContainerName) {
		return
	}

	// Set ignore flag
	if err := s.scriptManager.SetIgnore(ctx, req.ContainerName, req.Ignore, "ui"); err != nil {
		RespondInternalError(w, err)
		return
	}

	message := "Container will be checked for updates"
	if req.Ignore {
		message = "Container will be ignored from update checks"
	}

	RespondSuccess(w, map[string]any{
		"success":   true,
		"container": req.ContainerName,
		"ignore":    req.Ignore,
		"message":   message + ". Changes apply on next check.",
	})
}

// handleSettingsAllowLatest sets the allow-latest flag for a container
// POST /api/settings/allow-latest with body: {"container_name": "...", "allow_latest": true/false}
func (s *Server) handleSettingsAllowLatest(w http.ResponseWriter, r *http.Request) {
	if !s.requireScriptManager(w) {
		return
	}

	ctx := r.Context()

	// Parse request body
	var req struct {
		ContainerName string `json:"container_name"`
		AllowLatest   bool   `json:"allow_latest"`
	}

	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if !validateRequired(w, "container_name", req.ContainerName) {
		return
	}

	// Set allow-latest flag
	if err := s.scriptManager.SetAllowLatest(ctx, req.ContainerName, req.AllowLatest, "ui"); err != nil {
		RespondInternalError(w, err)
		return
	}

	message := ":latest tag migration will be suggested"
	if req.AllowLatest {
		message = ":latest tag is allowed, migration will not be suggested"
	}

	RespondSuccess(w, map[string]any{
		"success":     true,
		"container":   req.ContainerName,
		"allow_latest": req.AllowLatest,
		"message":     message + ". Changes apply on next check.",
	})
}

