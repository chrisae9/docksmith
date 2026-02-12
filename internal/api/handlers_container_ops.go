package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/storage"
	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
)

// handleContainerLogs returns logs for a container
// GET /api/containers/{name}/logs
// Query params: ?tail=100&follow=false&timestamps=true
func (s *Server) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	// Parse query parameters
	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "100"
	}
	timestamps := r.URL.Query().Get("timestamps") == "true"
	follow := r.URL.Query().Get("follow") == "true"

	ctx := r.Context()

	// Find container
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	// Set up log options
	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: timestamps,
		Tail:       tail,
		Follow:     follow,
	}

	// Get logs
	reader, err := s.dockerService.GetClient().ContainerLogs(ctx, ctr.ID, options)
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to get logs: %w", err))
		return
	}
	defer reader.Close()

	// If following, stream the response
	if follow {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Transfer-Encoding", "chunked")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		flusher, ok := w.(http.Flusher)
		if !ok {
			RespondInternalError(w, fmt.Errorf("streaming not supported"))
			return
		}

		// Stream logs
		buf := make([]byte, 8192)
		for {
			select {
			case <-ctx.Done():
				return
			default:
				n, err := reader.Read(buf)
				if n > 0 {
					// Docker multiplexes stdout/stderr with an 8-byte header
					// We need to strip it for plain text output
					content := stripDockerLogHeader(buf[:n])
					w.Write(content)
					flusher.Flush()
				}
				if err == io.EOF {
					return
				}
				if err != nil {
					return
				}
			}
		}
	}

	// Non-streaming: read all and return
	logBytes, err := io.ReadAll(reader)
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to read logs: %w", err))
		return
	}

	// Strip Docker multiplexed stream headers
	content := stripDockerLogHeader(logBytes)

	RespondSuccess(w, map[string]any{
		"container": containerName,
		"logs":      string(content),
		"tail":      tail,
	})
}

// stripDockerLogHeader removes the 8-byte header that Docker adds to multiplexed streams
func stripDockerLogHeader(data []byte) []byte {
	var result []byte
	pos := 0

	for pos < len(data) {
		if pos+8 > len(data) {
			// Not enough data for header, append remaining
			result = append(result, data[pos:]...)
			break
		}

		// Read frame size from header (bytes 4-7, big endian)
		frameSize := int(data[pos+4])<<24 | int(data[pos+5])<<16 | int(data[pos+6])<<8 | int(data[pos+7])

		// Skip header
		pos += 8

		// Append frame content
		end := pos + frameSize
		if end > len(data) {
			end = len(data)
		}
		result = append(result, data[pos:end]...)
		pos = end
	}

	return result
}

// ContainerInspectResponse represents the response for container inspection
type ContainerInspectResponse struct {
	ID              string                 `json:"id"`
	Name            string                 `json:"name"`
	Image           string                 `json:"image"`
	Created         string                 `json:"created"`
	State           ContainerStateInfo     `json:"state"`
	Config          ContainerConfigInfo    `json:"config"`
	NetworkSettings NetworkSettingsInfo    `json:"network_settings"`
	Mounts          []MountInfo            `json:"mounts"`
	HostConfig      HostConfigInfo         `json:"host_config"`
	Labels          map[string]string      `json:"labels"`
	Raw             map[string]interface{} `json:"raw,omitempty"`
}

// ContainerStateInfo represents container state information
type ContainerStateInfo struct {
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	Paused     bool   `json:"paused"`
	Restarting bool   `json:"restarting"`
	OOMKilled  bool   `json:"oom_killed"`
	Dead       bool   `json:"dead"`
	Pid        int    `json:"pid"`
	ExitCode   int    `json:"exit_code"`
	Error      string `json:"error"`
	StartedAt  string `json:"started_at"`
	FinishedAt string `json:"finished_at"`
	Health     string `json:"health,omitempty"`
}

// ContainerConfigInfo represents container configuration
type ContainerConfigInfo struct {
	Hostname     string            `json:"hostname"`
	User         string            `json:"user"`
	Env          []string          `json:"env"`
	Cmd          []string          `json:"cmd"`
	Entrypoint   []string          `json:"entrypoint"`
	WorkingDir   string            `json:"working_dir"`
	ExposedPorts map[string]bool   `json:"exposed_ports"`
	Labels       map[string]string `json:"labels"`
}

// NetworkSettingsInfo represents container network settings
type NetworkSettingsInfo struct {
	IPAddress   string                    `json:"ip_address"`
	Gateway     string                    `json:"gateway"`
	MacAddress  string                    `json:"mac_address"`
	Ports       map[string][]PortBinding  `json:"ports"`
	Networks    map[string]NetworkInfo    `json:"networks"`
}

// PortBinding represents a port binding
type PortBinding struct {
	HostIP   string `json:"host_ip"`
	HostPort string `json:"host_port"`
}

// NetworkInfo represents network endpoint information
type NetworkInfo struct {
	NetworkID   string   `json:"network_id"`
	EndpointID  string   `json:"endpoint_id"`
	Gateway     string   `json:"gateway"`
	IPAddress   string   `json:"ip_address"`
	MacAddress  string   `json:"mac_address"`
	Aliases     []string `json:"aliases"`
}

// MountInfo represents a container mount
type MountInfo struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Mode        string `json:"mode"`
	RW          bool   `json:"rw"`
}

// HostConfigInfo represents host configuration
type HostConfigInfo struct {
	Binds           []string          `json:"binds"`
	NetworkMode     string            `json:"network_mode"`
	RestartPolicy   RestartPolicyInfo `json:"restart_policy"`
	PortBindings    map[string][]PortBinding `json:"port_bindings"`
	Memory          int64             `json:"memory"`
	MemorySwap      int64             `json:"memory_swap"`
	CPUShares       int64             `json:"cpu_shares"`
	CPUPeriod       int64             `json:"cpu_period"`
	CPUQuota        int64             `json:"cpu_quota"`
	Privileged      bool              `json:"privileged"`
	ReadonlyRootfs  bool              `json:"readonly_rootfs"`
}

// RestartPolicyInfo represents restart policy
type RestartPolicyInfo struct {
	Name              string `json:"name"`
	MaximumRetryCount int    `json:"maximum_retry_count"`
}

// handleContainerInspect returns detailed container inspection data
// GET /api/containers/{name}/inspect
func (s *Server) handleContainerInspect(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	ctx := r.Context()

	// Find container
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	// Inspect container
	inspect, err := s.dockerService.GetClient().ContainerInspect(ctx, ctr.ID)
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to inspect container: %w", err))
		return
	}

	// Build response with nil guards for edge cases (dead/corrupted containers)
	response := ContainerInspectResponse{
		ID:      inspect.ID,
		Name:    strings.TrimPrefix(inspect.Name, "/"),
		Created: inspect.Created,
	}

	// Populate Config fields if available
	if inspect.Config != nil {
		response.Image = inspect.Config.Image
		response.Config = ContainerConfigInfo{
			Hostname:   inspect.Config.Hostname,
			User:       inspect.Config.User,
			Env:        inspect.Config.Env,
			Cmd:        inspect.Config.Cmd,
			Entrypoint: inspect.Config.Entrypoint,
			WorkingDir: inspect.Config.WorkingDir,
			Labels:     inspect.Config.Labels,
		}
		response.Labels = inspect.Config.Labels

		// Parse exposed ports
		if inspect.Config.ExposedPorts != nil {
			response.Config.ExposedPorts = make(map[string]bool)
			for port := range inspect.Config.ExposedPorts {
				response.Config.ExposedPorts[string(port)] = true
			}
		}
	}

	// Populate State fields if available
	if inspect.State != nil {
		response.State = ContainerStateInfo{
			Status:     inspect.State.Status,
			Running:    inspect.State.Running,
			Paused:     inspect.State.Paused,
			Restarting: inspect.State.Restarting,
			OOMKilled:  inspect.State.OOMKilled,
			Dead:       inspect.State.Dead,
			Pid:        inspect.State.Pid,
			ExitCode:   inspect.State.ExitCode,
			Error:      inspect.State.Error,
			StartedAt:  inspect.State.StartedAt,
			FinishedAt: inspect.State.FinishedAt,
		}
		if inspect.State.Health != nil {
			response.State.Health = inspect.State.Health.Status
		}
	}

	// Parse network settings if available
	if inspect.NetworkSettings != nil {
		response.NetworkSettings = NetworkSettingsInfo{
			IPAddress:  inspect.NetworkSettings.IPAddress,
			Gateway:    inspect.NetworkSettings.Gateway,
			MacAddress: inspect.NetworkSettings.MacAddress,
		}

		if inspect.NetworkSettings.Ports != nil {
			response.NetworkSettings.Ports = make(map[string][]PortBinding)
			for port, bindings := range inspect.NetworkSettings.Ports {
				var portBindings []PortBinding
				for _, b := range bindings {
					portBindings = append(portBindings, PortBinding{
						HostIP:   b.HostIP,
						HostPort: b.HostPort,
					})
				}
				response.NetworkSettings.Ports[string(port)] = portBindings
			}
		}

		if inspect.NetworkSettings.Networks != nil {
			response.NetworkSettings.Networks = make(map[string]NetworkInfo)
			for name, net := range inspect.NetworkSettings.Networks {
				response.NetworkSettings.Networks[name] = NetworkInfo{
					NetworkID:   net.NetworkID,
					EndpointID:  net.EndpointID,
					Gateway:     net.Gateway,
					IPAddress:   net.IPAddress,
					MacAddress:  net.MacAddress,
					Aliases:     net.Aliases,
				}
			}
		}
	}

	// Parse mounts
	for _, mount := range inspect.Mounts {
		response.Mounts = append(response.Mounts, MountInfo{
			Type:        string(mount.Type),
			Source:      mount.Source,
			Destination: mount.Destination,
			Mode:        mount.Mode,
			RW:          mount.RW,
		})
	}

	// Parse host config
	if inspect.HostConfig != nil {
		response.HostConfig = HostConfigInfo{
			Binds:          inspect.HostConfig.Binds,
			NetworkMode:    string(inspect.HostConfig.NetworkMode),
			Memory:         inspect.HostConfig.Memory,
			MemorySwap:     inspect.HostConfig.MemorySwap,
			CPUShares:      inspect.HostConfig.CPUShares,
			CPUPeriod:      inspect.HostConfig.CPUPeriod,
			CPUQuota:       inspect.HostConfig.CPUQuota,
			Privileged:     inspect.HostConfig.Privileged,
			ReadonlyRootfs: inspect.HostConfig.ReadonlyRootfs,
		}

		if inspect.HostConfig.RestartPolicy.Name != "" {
			response.HostConfig.RestartPolicy = RestartPolicyInfo{
				Name:              string(inspect.HostConfig.RestartPolicy.Name),
				MaximumRetryCount: inspect.HostConfig.RestartPolicy.MaximumRetryCount,
			}
		}

		if inspect.HostConfig.PortBindings != nil {
			response.HostConfig.PortBindings = make(map[string][]PortBinding)
			for port, bindings := range inspect.HostConfig.PortBindings {
				var portBindings []PortBinding
				for _, b := range bindings {
					portBindings = append(portBindings, PortBinding{
						HostIP:   b.HostIP,
						HostPort: b.HostPort,
					})
				}
				response.HostConfig.PortBindings[string(port)] = portBindings
			}
		}
	}

	RespondSuccess(w, response)
}

// handleContainerStop stops a running container
// POST /api/containers/{name}/stop
// Query params: ?timeout=10
func (s *Server) handleContainerStop(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	timeout := parsePositiveIntParam(r, "timeout", 10)

	ctx := r.Context()

	// Find container
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	// Check if already stopped
	if ctr.State != "running" {
		RespondBadRequest(w, fmt.Errorf("container is not running (state: %s)", ctr.State))
		return
	}

	// Generate operation ID and record operation start
	operationID := uuid.New().String()
	now := time.Now()

	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   ctr.ID,
		ContainerName: containerName,
		StackName:     ctr.Stack,
		OperationType: "stop",
		Status:        "in_progress",
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.storageService != nil {
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save stop operation to database: %v", err)
		}
	}

	// Stop the container
	stopOptions := container.StopOptions{
		Timeout: &timeout,
	}
	err = s.dockerService.GetClient().ContainerStop(ctx, ctr.ID, stopOptions)
	if err != nil {
		// Record failure
		if s.storageService != nil {
			s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
		}
		RespondInternalError(w, fmt.Errorf("failed to stop container: %w", err))
		return
	}

	// Record success
	if s.storageService != nil {
		completedAt := time.Now()
		op.Status = "complete"
		op.CompletedAt = &completedAt
		op.UpdatedAt = completedAt
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save stop operation completion: %v", err)
		}
	}

	RespondSuccess(w, map[string]any{
		"container":    containerName,
		"status":       "stopped",
		"operation_id": operationID,
		"message":      fmt.Sprintf("Container %s stopped successfully", containerName),
	})
}

// handleContainerStart starts a stopped container
// POST /api/containers/{name}/start
func (s *Server) handleContainerStart(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	ctx := r.Context()

	// Find container
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	// Check if already running
	if ctr.State == "running" {
		RespondBadRequest(w, fmt.Errorf("container is already running"))
		return
	}

	// Generate operation ID and record operation start
	operationID := uuid.New().String()
	now := time.Now()

	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   ctr.ID,
		ContainerName: containerName,
		StackName:     ctr.Stack,
		OperationType: "start",
		Status:        "in_progress",
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.storageService != nil {
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save start operation to database: %v", err)
		}
	}

	// Start the container
	err = s.dockerService.GetClient().ContainerStart(ctx, ctr.ID, container.StartOptions{})
	if err != nil {
		if s.storageService != nil {
			s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
		}
		RespondInternalError(w, fmt.Errorf("failed to start container: %w", err))
		return
	}

	// Record success
	if s.storageService != nil {
		completedAt := time.Now()
		op.Status = "complete"
		op.CompletedAt = &completedAt
		op.UpdatedAt = completedAt
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save start operation completion: %v", err)
		}
	}

	RespondSuccess(w, map[string]any{
		"container":    containerName,
		"status":       "started",
		"operation_id": operationID,
		"message":      fmt.Sprintf("Container %s started successfully", containerName),
	})
}

// handleContainerRestart restarts a container
// POST /api/containers/{name}/restart
// Query params: ?timeout=10
func (s *Server) handleContainerRestart(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	timeout := parsePositiveIntParam(r, "timeout", 10)

	ctx := r.Context()

	// Find container
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	// Generate operation ID and record operation start
	operationID := uuid.New().String()
	now := time.Now()

	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   ctr.ID,
		ContainerName: containerName,
		StackName:     ctr.Stack,
		OperationType: "restart",
		Status:        "in_progress",
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.storageService != nil {
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save restart operation to database: %v", err)
		}
	}

	// Restart the container
	restartOptions := container.StopOptions{
		Timeout: &timeout,
	}
	err = s.dockerService.GetClient().ContainerRestart(ctx, ctr.ID, restartOptions)
	if err != nil {
		if s.storageService != nil {
			s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
		}
		RespondInternalError(w, fmt.Errorf("failed to restart container: %w", err))
		return
	}

	// Record success
	if s.storageService != nil {
		completedAt := time.Now()
		op.Status = "complete"
		op.CompletedAt = &completedAt
		op.UpdatedAt = completedAt
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save restart operation completion: %v", err)
		}
	}

	RespondSuccess(w, map[string]any{
		"container":    containerName,
		"status":       "restarted",
		"operation_id": operationID,
		"message":      fmt.Sprintf("Container %s restarted successfully", containerName),
	})
}

// handleContainerRemove removes a container
// DELETE /api/containers/{name}
// Query params: ?force=false&volumes=false
func (s *Server) handleContainerRemove(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	// Parse options
	force := r.URL.Query().Get("force") == "true"
	removeVolumes := r.URL.Query().Get("volumes") == "true"

	ctx := r.Context()

	// Find container
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	// Check if running and force not set
	if ctr.State == "running" && !force {
		RespondBadRequest(w, fmt.Errorf("container is running, use force=true to remove"))
		return
	}

	// Generate operation ID and record operation start
	operationID := uuid.New().String()
	now := time.Now()

	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   ctr.ID,
		ContainerName: containerName,
		StackName:     ctr.Stack,
		OperationType: "remove",
		Status:        "in_progress",
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.storageService != nil {
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save remove operation to database: %v", err)
		}
	}

	// Remove the container
	removeOptions := container.RemoveOptions{
		Force:         force,
		RemoveVolumes: removeVolumes,
	}
	err = s.dockerService.GetClient().ContainerRemove(ctx, ctr.ID, removeOptions)
	if err != nil {
		// Record failure
		if s.storageService != nil {
			s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
		}
		RespondInternalError(w, fmt.Errorf("failed to remove container: %w", err))
		return
	}

	// Record success
	if s.storageService != nil {
		completedAt := time.Now()
		op.Status = "complete"
		op.CompletedAt = &completedAt
		op.UpdatedAt = completedAt
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save remove operation completion: %v", err)
		}
	}

	RespondSuccess(w, map[string]any{
		"container":    containerName,
		"status":       "removed",
		"operation_id": operationID,
		"force":        force,
		"volumes":      removeVolumes,
		"message":      fmt.Sprintf("Container %s removed successfully", containerName),
	})
}

// BatchContainerRequest represents a batch container operation request
type BatchContainerRequest struct {
	Containers []string `json:"containers"`
	Timeout    *int     `json:"timeout,omitempty"`
	Force      bool     `json:"force,omitempty"`
}

// BatchContainerResult represents a per-container result in a batch operation
type BatchContainerResult struct {
	Container   string `json:"container"`
	Success     bool   `json:"success"`
	OperationID string `json:"operation_id,omitempty"`
	Error       string `json:"error,omitempty"`
}

// handleBatchStart starts multiple containers with a shared batch_group_id
// POST /api/containers/batch/start
func (s *Server) handleBatchStart(w http.ResponseWriter, r *http.Request) {
	var req BatchContainerRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if len(req.Containers) == 0 {
		RespondBadRequest(w, fmt.Errorf("containers array is required"))
		return
	}

	ctx := r.Context()
	batchGroupID := uuid.New().String()
	results := make([]BatchContainerResult, 0, len(req.Containers))

	for _, containerName := range req.Containers {
		ctr, err := s.findContainerByName(ctx, containerName)
		if err != nil {
			results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: "container not found"})
			continue
		}

		if ctr.State == "running" {
			results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: "already running"})
			continue
		}

		operationID := uuid.New().String()
		now := time.Now()
		op := storage.UpdateOperation{
			OperationID:   operationID,
			ContainerID:   ctr.ID,
			ContainerName: containerName,
			StackName:     ctr.Stack,
			OperationType: "start",
			Status:        "in_progress",
			BatchGroupID:  batchGroupID,
			StartedAt:     &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if s.storageService != nil {
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		if err := s.dockerService.GetClient().ContainerStart(ctx, ctr.ID, container.StartOptions{}); err != nil {
			if s.storageService != nil {
				s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
			}
			results = append(results, BatchContainerResult{Container: containerName, Success: false, OperationID: operationID, Error: err.Error()})
			continue
		}

		if s.storageService != nil {
			completedAt := time.Now()
			op.Status = "complete"
			op.CompletedAt = &completedAt
			op.UpdatedAt = completedAt
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		results = append(results, BatchContainerResult{Container: containerName, Success: true, OperationID: operationID})
	}

	RespondSuccess(w, map[string]any{
		"results":        results,
		"batch_group_id": batchGroupID,
	})
}

// handleBatchStop stops multiple containers with a shared batch_group_id
// POST /api/containers/batch/stop
func (s *Server) handleBatchStop(w http.ResponseWriter, r *http.Request) {
	var req BatchContainerRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if len(req.Containers) == 0 {
		RespondBadRequest(w, fmt.Errorf("containers array is required"))
		return
	}

	timeout := 10
	if req.Timeout != nil && *req.Timeout > 0 {
		timeout = *req.Timeout
	}

	ctx := r.Context()
	batchGroupID := uuid.New().String()
	results := make([]BatchContainerResult, 0, len(req.Containers))

	for _, containerName := range req.Containers {
		ctr, err := s.findContainerByName(ctx, containerName)
		if err != nil {
			results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: "container not found"})
			continue
		}

		if ctr.State != "running" {
			results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: fmt.Sprintf("not running (state: %s)", ctr.State)})
			continue
		}

		operationID := uuid.New().String()
		now := time.Now()
		op := storage.UpdateOperation{
			OperationID:   operationID,
			ContainerID:   ctr.ID,
			ContainerName: containerName,
			StackName:     ctr.Stack,
			OperationType: "stop",
			Status:        "in_progress",
			BatchGroupID:  batchGroupID,
			StartedAt:     &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if s.storageService != nil {
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		stopOptions := container.StopOptions{Timeout: &timeout}
		if err := s.dockerService.GetClient().ContainerStop(ctx, ctr.ID, stopOptions); err != nil {
			if s.storageService != nil {
				s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
			}
			results = append(results, BatchContainerResult{Container: containerName, Success: false, OperationID: operationID, Error: err.Error()})
			continue
		}

		if s.storageService != nil {
			completedAt := time.Now()
			op.Status = "complete"
			op.CompletedAt = &completedAt
			op.UpdatedAt = completedAt
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		results = append(results, BatchContainerResult{Container: containerName, Success: true, OperationID: operationID})
	}

	RespondSuccess(w, map[string]any{
		"results":        results,
		"batch_group_id": batchGroupID,
	})
}

// handleBatchRestart restarts multiple containers with a shared batch_group_id
// POST /api/containers/batch/restart
func (s *Server) handleBatchRestart(w http.ResponseWriter, r *http.Request) {
	var req BatchContainerRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if len(req.Containers) == 0 {
		RespondBadRequest(w, fmt.Errorf("containers array is required"))
		return
	}

	timeout := 10
	if req.Timeout != nil && *req.Timeout > 0 {
		timeout = *req.Timeout
	}

	ctx := r.Context()
	batchGroupID := uuid.New().String()
	results := make([]BatchContainerResult, 0, len(req.Containers))

	for _, containerName := range req.Containers {
		ctr, err := s.findContainerByName(ctx, containerName)
		if err != nil {
			results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: "container not found"})
			continue
		}

		operationID := uuid.New().String()
		now := time.Now()
		op := storage.UpdateOperation{
			OperationID:   operationID,
			ContainerID:   ctr.ID,
			ContainerName: containerName,
			StackName:     ctr.Stack,
			OperationType: "restart",
			Status:        "in_progress",
			BatchGroupID:  batchGroupID,
			StartedAt:     &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if s.storageService != nil {
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		restartOptions := container.StopOptions{Timeout: &timeout}
		if err := s.dockerService.GetClient().ContainerRestart(ctx, ctr.ID, restartOptions); err != nil {
			if s.storageService != nil {
				s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
			}
			results = append(results, BatchContainerResult{Container: containerName, Success: false, OperationID: operationID, Error: err.Error()})
			continue
		}

		if s.storageService != nil {
			completedAt := time.Now()
			op.Status = "complete"
			op.CompletedAt = &completedAt
			op.UpdatedAt = completedAt
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		results = append(results, BatchContainerResult{Container: containerName, Success: true, OperationID: operationID})
	}

	RespondSuccess(w, map[string]any{
		"results":        results,
		"batch_group_id": batchGroupID,
	})
}

// handleBatchRemove removes multiple containers with a shared batch_group_id
// POST /api/containers/batch/remove
func (s *Server) handleBatchRemove(w http.ResponseWriter, r *http.Request) {
	var req BatchContainerRequest
	if !decodeJSONRequest(w, r, &req) {
		return
	}

	if len(req.Containers) == 0 {
		RespondBadRequest(w, fmt.Errorf("containers array is required"))
		return
	}

	ctx := r.Context()
	batchGroupID := uuid.New().String()
	results := make([]BatchContainerResult, 0, len(req.Containers))

	for _, containerName := range req.Containers {
		ctr, err := s.findContainerByName(ctx, containerName)
		if err != nil {
			results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: "container not found"})
			continue
		}

		operationID := uuid.New().String()
		now := time.Now()
		op := storage.UpdateOperation{
			OperationID:   operationID,
			ContainerID:   ctr.ID,
			ContainerName: containerName,
			StackName:     ctr.Stack,
			OperationType: "remove",
			Status:        "in_progress",
			BatchGroupID:  batchGroupID,
			StartedAt:     &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if s.storageService != nil {
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		removeOptions := container.RemoveOptions{
			Force:         req.Force || ctr.State == "running",
			RemoveVolumes: false,
		}
		if err := s.dockerService.GetClient().ContainerRemove(ctx, ctr.ID, removeOptions); err != nil {
			if s.storageService != nil {
				s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
			}
			results = append(results, BatchContainerResult{Container: containerName, Success: false, OperationID: operationID, Error: err.Error()})
			continue
		}

		if s.storageService != nil {
			completedAt := time.Now()
			op.Status = "complete"
			op.CompletedAt = &completedAt
			op.UpdatedAt = completedAt
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		results = append(results, BatchContainerResult{Container: containerName, Success: true, OperationID: operationID})
	}

	RespondSuccess(w, map[string]any{
		"results":        results,
		"batch_group_id": batchGroupID,
	})
}

// handleContainerStats returns container resource usage statistics
// GET /api/containers/{name}/stats
func (s *Server) handleContainerStats(w http.ResponseWriter, r *http.Request) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	ctx := r.Context()

	// Find container
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	// Create a context with timeout for stats
	statsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Get one-shot stats (not streaming)
	stats, err := s.dockerService.GetClient().ContainerStats(statsCtx, ctr.ID, false)
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to get container stats: %w", err))
		return
	}
	defer stats.Body.Close()

	// Read stats JSON
	statsBytes, err := io.ReadAll(stats.Body)
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to read stats: %w", err))
		return
	}

	// Return raw stats - let frontend parse what it needs
	w.Header().Set("Content-Type", "application/json")
	w.Write(statsBytes)
}
