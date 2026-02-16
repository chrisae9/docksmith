package api

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
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

// publishContainerProgress emits an SSE progress event for container operations
func (s *Server) publishContainerProgress(operationID, containerName, stackName, stage string, percent int, message string) {
	if s.eventBus == nil {
		return
	}
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

// publishContainerUpdated emits an SSE event when a container operation completes or fails
func (s *Server) publishContainerUpdated(operationID, containerName, status string) {
	if s.eventBus == nil {
		return
	}
	s.eventBus.Publish(events.Event{
		Type: events.EventContainerUpdated,
		Payload: map[string]interface{}{
			"operation_id":   operationID,
			"container_name": containerName,
			"status":         status,
		},
	})
}

// containerOpConfig defines the varying parts of a container operation (single or batch).
type containerOpConfig struct {
	opType      string // storage operation type: "stop", "start", "restart", "remove"
	stage       string // SSE progress stage: "stopping", "starting", "restarting", "removing"
	successVerb string // past tense for messages: "stopped", "started", "restarted", "removed"
}

// capitalize returns s with the first letter uppercased. Only handles ASCII.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// runSingleContainerOp executes a single-container operation with full operation lifecycle:
// find container → validate → record op → publish progress → execute → handle result → respond.
func (s *Server) runSingleContainerOp(
	w http.ResponseWriter, r *http.Request,
	cfg containerOpConfig,
	validate func(ctr *docker.Container) error,
	execute func(ctx context.Context, ctrID string) error,
	extraResponse map[string]any,
) {
	containerName := r.PathValue("name")
	if !validateRequired(w, "container name", containerName) {
		return
	}

	ctx := r.Context()
	ctr, err := s.findContainerByName(ctx, containerName)
	if err != nil {
		RespondNotFound(w, fmt.Errorf("container not found: %s", containerName))
		return
	}

	if validate != nil {
		if err := validate(ctr); err != nil {
			RespondBadRequest(w, err)
			return
		}
	}

	operationID := uuid.New().String()
	now := time.Now()
	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   ctr.ID,
		ContainerName: containerName,
		StackName:     ctr.Stack,
		OperationType: cfg.opType,
		Status:        "in_progress",
		StartedAt:     &now,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if s.storageService != nil {
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save %s operation to database: %v", cfg.opType, err)
		}
	}

	s.publishContainerProgress(operationID, containerName, ctr.Stack, cfg.stage, 30,
		fmt.Sprintf("%s container...", capitalize(cfg.stage)))

	if err := execute(ctx, ctr.ID); err != nil {
		s.publishContainerProgress(operationID, containerName, ctr.Stack, "failed", 0, err.Error())
		s.publishContainerUpdated(operationID, containerName, "failed")
		if s.storageService != nil {
			s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
		}
		RespondInternalError(w, fmt.Errorf("failed to %s container: %w", cfg.opType, err))
		return
	}

	s.publishContainerProgress(operationID, containerName, ctr.Stack, "complete", 100,
		fmt.Sprintf("Container %s %s successfully", containerName, cfg.successVerb))
	s.publishContainerUpdated(operationID, containerName, "complete")

	if s.storageService != nil {
		completedAt := time.Now()
		op.Status = "complete"
		op.CompletedAt = &completedAt
		op.UpdatedAt = completedAt
		if err := s.storageService.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("Failed to save %s operation completion: %v", cfg.opType, err)
		}
	}

	resp := map[string]any{
		"container":    containerName,
		"status":       cfg.successVerb,
		"operation_id": operationID,
		"message":      fmt.Sprintf("Container %s %s successfully", containerName, cfg.successVerb),
	}
	for k, v := range extraResponse {
		resp[k] = v
	}
	RespondSuccess(w, resp)
}

// handleContainerStop stops a running container
// POST /api/containers/{name}/stop?timeout=10
func (s *Server) handleContainerStop(w http.ResponseWriter, r *http.Request) {
	timeout := parsePositiveIntParam(r, "timeout", 10)
	s.runSingleContainerOp(w, r,
		containerOpConfig{"stop", "stopping", "stopped"},
		nil, // No validation — already-stopped containers are handled gracefully below
		func(ctx context.Context, ctrID string) error {
			err := s.dockerService.GetClient().ContainerStop(ctx, ctrID, container.StopOptions{Timeout: &timeout})
			if err != nil && (strings.Contains(err.Error(), "is not running") || strings.Contains(err.Error(), "already stopped")) {
				return nil // Already stopped — idempotent, like docker compose down
			}
			return err
		},
		nil,
	)
}

// handleContainerStart starts a stopped container
// POST /api/containers/{name}/start
func (s *Server) handleContainerStart(w http.ResponseWriter, r *http.Request) {
	s.runSingleContainerOp(w, r,
		containerOpConfig{"start", "starting", "started"},
		nil, // No validation — already-running containers are handled gracefully below
		func(ctx context.Context, ctrID string) error {
			err := s.dockerService.GetClient().ContainerStart(ctx, ctrID, container.StartOptions{})
			if err != nil && strings.Contains(err.Error(), "already started") {
				return nil // Already running — idempotent
			}
			return err
		},
		nil,
	)
}

// handleContainerRestart restarts a container
// POST /api/containers/{name}/restart?timeout=10
func (s *Server) handleContainerRestart(w http.ResponseWriter, r *http.Request) {
	timeout := parsePositiveIntParam(r, "timeout", 10)
	s.runSingleContainerOp(w, r,
		containerOpConfig{"restart", "restarting", "restarted"},
		nil,
		func(ctx context.Context, ctrID string) error {
			return s.dockerService.GetClient().ContainerRestart(ctx, ctrID, container.StopOptions{Timeout: &timeout})
		},
		nil,
	)
}

// handleContainerRemove removes a container
// DELETE /api/containers/{name}?force=false&volumes=false
func (s *Server) handleContainerRemove(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"
	removeVolumes := r.URL.Query().Get("volumes") == "true"
	s.runSingleContainerOp(w, r,
		containerOpConfig{"remove", "removing", "removed"},
		func(ctr *docker.Container) error {
			if ctr.State == "running" && !force {
				return fmt.Errorf("container is running, use force=true to remove")
			}
			return nil
		},
		func(ctx context.Context, ctrID string) error {
			return s.dockerService.GetClient().ContainerRemove(ctx, ctrID, container.RemoveOptions{Force: force, RemoveVolumes: removeVolumes})
		},
		map[string]any{"force": force, "volumes": removeVolumes},
	)
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

// runBatchContainerOp executes a batch container operation with shared batch_group_id.
func (s *Server) runBatchContainerOp(
	w http.ResponseWriter, r *http.Request,
	cfg containerOpConfig,
	req BatchContainerRequest,
	validate func(ctr *docker.Container) error,
	execute func(ctx context.Context, ctrID string) error,
) {
	ctx := r.Context()
	batchGroupID := uuid.New().String()
	results := make([]BatchContainerResult, 0, len(req.Containers))

	for _, containerName := range req.Containers {
		ctr, err := s.findContainerByName(ctx, containerName)
		if err != nil {
			results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: "container not found"})
			continue
		}

		if validate != nil {
			if err := validate(ctr); err != nil {
				results = append(results, BatchContainerResult{Container: containerName, Success: false, Error: err.Error()})
				continue
			}
		}

		operationID := uuid.New().String()
		now := time.Now()
		op := storage.UpdateOperation{
			OperationID:   operationID,
			ContainerID:   ctr.ID,
			ContainerName: containerName,
			StackName:     ctr.Stack,
			OperationType: cfg.opType,
			Status:        "in_progress",
			BatchGroupID:  batchGroupID,
			StartedAt:     &now,
			CreatedAt:     now,
			UpdatedAt:     now,
		}
		if s.storageService != nil {
			s.storageService.SaveUpdateOperation(ctx, op)
		}

		s.publishContainerProgress(operationID, containerName, ctr.Stack, cfg.stage, 30,
			fmt.Sprintf("%s container...", capitalize(cfg.stage)))

		if err := execute(ctx, ctr.ID); err != nil {
			s.publishContainerProgress(operationID, containerName, ctr.Stack, "failed", 0, err.Error())
			s.publishContainerUpdated(operationID, containerName, "failed")
			if s.storageService != nil {
				s.storageService.UpdateOperationStatus(ctx, operationID, "failed", err.Error())
			}
			results = append(results, BatchContainerResult{Container: containerName, Success: false, OperationID: operationID, Error: err.Error()})
			continue
		}

		s.publishContainerProgress(operationID, containerName, ctr.Stack, "complete", 100,
			fmt.Sprintf("Container %s %s successfully", containerName, cfg.successVerb))
		s.publishContainerUpdated(operationID, containerName, "complete")

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

// handleBatchStart starts multiple containers
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
	s.runBatchContainerOp(w, r,
		containerOpConfig{"start", "starting", "started"}, req,
		nil, // No validation — already-running containers are handled gracefully below
		func(ctx context.Context, ctrID string) error {
			err := s.dockerService.GetClient().ContainerStart(ctx, ctrID, container.StartOptions{})
			if err != nil && strings.Contains(err.Error(), "already started") {
				return nil // Already running — idempotent
			}
			return err
		},
	)
}

// handleBatchStop stops multiple containers
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
	s.runBatchContainerOp(w, r,
		containerOpConfig{"stop", "stopping", "stopped"}, req,
		nil, // No validation — already-stopped containers are handled gracefully below
		func(ctx context.Context, ctrID string) error {
			err := s.dockerService.GetClient().ContainerStop(ctx, ctrID, container.StopOptions{Timeout: &timeout})
			if err != nil && (strings.Contains(err.Error(), "is not running") || strings.Contains(err.Error(), "already stopped")) {
				return nil // Already stopped — idempotent, like docker compose down
			}
			return err
		},
	)
}

// handleBatchRestart restarts multiple containers
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
	s.runBatchContainerOp(w, r,
		containerOpConfig{"restart", "restarting", "restarted"}, req,
		nil,
		func(ctx context.Context, ctrID string) error {
			return s.dockerService.GetClient().ContainerRestart(ctx, ctrID, container.StopOptions{Timeout: &timeout})
		},
	)
}

// handleBatchRemove removes multiple containers
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
	s.runBatchContainerOp(w, r,
		containerOpConfig{"remove", "removing", "removed"}, req,
		nil,
		func(ctx context.Context, ctrID string) error {
			return s.dockerService.GetClient().ContainerRemove(ctx, ctrID, container.RemoveOptions{Force: req.Force, RemoveVolumes: false})
		},
	)
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
