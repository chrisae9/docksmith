package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/storage"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// UpdateOrchestrator manages the complete update workflow for containers.
type UpdateOrchestrator struct {
	dockerClient   docker.Client
	dockerSDK      *dockerclient.Client
	storage        storage.Storage
	eventBus       *events.Bus
	graphBuilder   *graph.Builder
	stackManager   *docker.StackManager
	checker        *Checker
	healthCheckCfg HealthCheckConfig
	stackLocks     map[string]*sync.Mutex
	locksMu        sync.Mutex
}

// HealthCheckConfig holds health check configuration.
type HealthCheckConfig struct {
	Timeout      time.Duration
	FallbackWait time.Duration
}

// PullProgress represents image pull progress for streaming to UI.
type PullProgress struct {
	Status   string
	Progress string
	Percent  int
}

// NewUpdateOrchestrator creates a new update orchestrator.
func NewUpdateOrchestrator(
	dockerClient docker.Client,
	dockerSDK *dockerclient.Client,
	store storage.Storage,
	bus *events.Bus,
	registryManager RegistryClient,
) *UpdateOrchestrator {
	orch := &UpdateOrchestrator{
		dockerClient:   dockerClient,
		dockerSDK:      dockerSDK,
		storage:        store,
		eventBus:       bus,
		graphBuilder:   graph.NewBuilder(),
		stackManager:   docker.NewStackManager(),
		checker:        NewChecker(dockerClient, registryManager, store),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      60 * time.Second,
			FallbackWait: 10 * time.Second,
		},
		stackLocks: make(map[string]*sync.Mutex),
	}

	go orch.processQueue(context.Background())

	return orch
}

// UpdateSingleContainer initiates an update for a single container.
func (o *UpdateOrchestrator) UpdateSingleContainer(ctx context.Context, containerName, targetVersion string) (string, error) {
	operationID := uuid.New().String()

	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *docker.Container
	for _, c := range containers {
		if c.Name == containerName || c.ID == containerName {
			targetContainer = &c
			break
		}
	}

	if targetContainer == nil {
		return "", fmt.Errorf("container not found: %s", containerName)
	}

	stackName := o.stackManager.DetermineStack(ctx, *targetContainer)

	if !o.acquireStackLock(stackName) {
		if err := o.queueOperation(ctx, operationID, stackName, []string{containerName}, "single"); err != nil {
			return "", fmt.Errorf("failed to queue operation: %w", err)
		}
		return operationID, nil
	}

	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   targetContainer.ID,
		ContainerName: containerName,
		StackName:     stackName,
		OperationType: "single",
		Status:        "validating",
		NewVersion:    targetVersion,
	}

	if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
		o.releaseStackLock(stackName)
		return "", fmt.Errorf("failed to save operation: %w", err)
	}

	go o.executeSingleUpdate(context.Background(), operationID, targetContainer, targetVersion, stackName)

	return operationID, nil
}

// UpdateBatchContainers initiates batch updates for multiple containers.
func (o *UpdateOrchestrator) UpdateBatchContainers(ctx context.Context, containerNames []string, targetVersions map[string]string) (string, error) {
	operationID := uuid.New().String()

	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	containerMap := make(map[string]*docker.Container)
	for _, name := range containerNames {
		for _, c := range containers {
			if c.Name == name || c.ID == name {
				containerMap[name] = &c
				break
			}
		}
	}

	if len(containerMap) == 0 {
		return "", fmt.Errorf("no matching containers found")
	}

	depGraph := o.graphBuilder.BuildFromContainers(containers)
	updateOrder, err := depGraph.GetUpdateOrder()
	if err != nil {
		return "", fmt.Errorf("failed to determine update order: %w", err)
	}

	orderedContainers := make([]*docker.Container, 0)
	for _, name := range updateOrder {
		if c, found := containerMap[name]; found {
			orderedContainers = append(orderedContainers, c)
		}
	}

	if len(orderedContainers) == 0 {
		orderedContainers = make([]*docker.Container, 0, len(containerMap))
		for _, c := range containerMap {
			orderedContainers = append(orderedContainers, c)
		}
	}

	stackName := ""
	if len(orderedContainers) > 0 {
		stackName = o.stackManager.DetermineStack(ctx, *orderedContainers[0])
	}

	if !o.acquireStackLock(stackName) {
		if err := o.queueOperation(ctx, operationID, stackName, containerNames, "batch"); err != nil {
			return "", fmt.Errorf("failed to queue operation: %w", err)
		}
		return operationID, nil
	}

	op := storage.UpdateOperation{
		OperationID:   operationID,
		StackName:     stackName,
		OperationType: "batch",
		Status:        "validating",
	}

	if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
		o.releaseStackLock(stackName)
		return "", fmt.Errorf("failed to save operation: %w", err)
	}

	go o.executeBatchUpdate(context.Background(), operationID, orderedContainers, targetVersions, stackName)

	return operationID, nil
}

// UpdateStack initiates an update for all containers in a stack.
func (o *UpdateOrchestrator) UpdateStack(ctx context.Context, stackName string) (string, error) {
	operationID := uuid.New().String()

	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	stackContainers := make([]docker.Container, 0)
	for _, c := range containers {
		if project, ok := c.Labels["com.docker.compose.project"]; ok && project == stackName {
			stackContainers = append(stackContainers, c)
		}
	}

	if len(stackContainers) == 0 {
		return "", fmt.Errorf("no containers found in stack: %s", stackName)
	}

	// Check for updates to determine target versions for each container
	targetVersions := make(map[string]string)
	if o.checker != nil {
		for _, container := range stackContainers {
			update := o.checker.checkContainer(ctx, container)
			if update.Status == UpdateAvailable && update.LatestVersion != "" {
				targetVersions[container.Name] = update.LatestVersion
			}
		}
	}

	depGraph := o.graphBuilder.BuildFromContainers(stackContainers)
	updateOrder, err := depGraph.GetUpdateOrder()
	if err != nil {
		return "", fmt.Errorf("failed to determine update order: %w", err)
	}

	orderedContainers := make([]*docker.Container, 0)
	for _, name := range updateOrder {
		for _, c := range stackContainers {
			if c.Name == name || c.ID == name {
				orderedContainers = append(orderedContainers, &c)
				break
			}
		}
	}

	if !o.acquireStackLock(stackName) {
		containerNames := make([]string, len(stackContainers))
		for i, c := range stackContainers {
			containerNames[i] = c.Name
		}
		if err := o.queueOperation(ctx, operationID, stackName, containerNames, "stack"); err != nil {
			return "", fmt.Errorf("failed to queue operation: %w", err)
		}
		return operationID, nil
	}

	op := storage.UpdateOperation{
		OperationID:   operationID,
		StackName:     stackName,
		OperationType: "stack",
		Status:        "validating",
	}

	if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
		o.releaseStackLock(stackName)
		return "", fmt.Errorf("failed to save operation: %w", err)
	}

	go o.executeBatchUpdate(context.Background(), operationID, orderedContainers, targetVersions, stackName)

	return operationID, nil
}

// executeSingleUpdate executes the update workflow for a single container.
func (o *UpdateOrchestrator) executeSingleUpdate(ctx context.Context, operationID string, container *docker.Container, targetVersion, stackName string) {
	defer o.releaseStackLock(stackName)

	log.Printf("UPDATE: Starting executeSingleUpdate for operation=%s container=%s target=%s", operationID, container.Name, targetVersion)

	o.publishProgress(operationID, container.Name, stackName, "validating", 0, "Validating permissions")

	if err := o.checkPermissions(ctx, container); err != nil {
		o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Permission check failed: %v", err))
		return
	}

	log.Printf("UPDATE: Permissions OK for operation=%s, creating backup", operationID)
	o.publishProgress(operationID, container.Name, stackName, "backup", 10, "Creating compose file backup")

	backupPath, err := o.backupComposeFile(ctx, operationID, container)
	if err != nil {
		o.failOperation(ctx, operationID, "backup", fmt.Sprintf("Backup failed: %v", err))
		return
	}
	log.Printf("UPDATE: Backup created for operation=%s at %s", operationID, backupPath)

	currentVersion, _ := o.dockerClient.GetImageVersion(ctx, container.Image)

	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.OldVersion = currentVersion
		o.storage.SaveUpdateOperation(ctx, op)
	}

	o.publishProgress(operationID, container.Name, stackName, "updating_compose", 20, "Updating compose file")

	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath != "" {
		resolvedPath, err := o.resolveComposeFile(composeFilePath)
		if err != nil {
			o.failOperation(ctx, operationID, "updating_compose", fmt.Sprintf("Failed to resolve compose file: %v", err))
			return
		}
		if err := o.updateComposeFile(ctx, resolvedPath, container, targetVersion); err != nil {
			o.failOperation(ctx, operationID, "updating_compose", fmt.Sprintf("Compose update failed: %v", err))
			return
		}
	}

	imageRef := o.buildImageRef(container.Image, targetVersion)

	log.Printf("UPDATE: Pulling image for operation=%s, imageRef=%s", operationID, imageRef)
	o.publishProgress(operationID, container.Name, stackName, "pulling_image", 30, "Pulling new image")

	progressChan := make(chan PullProgress, 10)
	go func() {
		for progress := range progressChan {
			percent := 30 + (progress.Percent * 30 / 100)
			o.publishProgress(operationID, container.Name, stackName, "pulling_image", percent, progress.Status)
		}
	}()

	if err := o.pullImage(ctx, imageRef, progressChan); err != nil {
		close(progressChan)
		o.failOperation(ctx, operationID, "pulling_image", fmt.Sprintf("Image pull failed: %v", err))
		return
	}
	close(progressChan)

	log.Printf("UPDATE: Image pulled for operation=%s, recreating container", operationID)
	o.publishProgress(operationID, container.Name, stackName, "recreating", 60, "Recreating container and dependents")

	// Build the full image reference with new version
	newImageRef := strings.Split(container.Image, ":")[0] + ":" + targetVersion

	dependentsRestarted, err := o.restartContainerWithDependents(ctx, operationID, container.Name, stackName, newImageRef)
	if err != nil {
		o.failOperation(ctx, operationID, "recreating", fmt.Sprintf("Recreation failed: %v", err))
		o.attemptRollback(ctx, operationID, container, backupPath)
		return
	}

	if len(dependentsRestarted) > 0 {
		op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
		if found {
			op.DependentsAffected = dependentsRestarted
			o.storage.SaveUpdateOperation(ctx, op)
		}
	}

	o.publishProgress(operationID, container.Name, stackName, "health_check", 80, "Verifying health")

	if err := o.waitForHealthy(ctx, container.Name, o.healthCheckCfg.Timeout); err != nil {
		shouldRollback, _ := o.shouldAutoRollback(ctx, container.Name)
		if shouldRollback {
			o.publishProgress(operationID, container.Name, stackName, "rolling_back", 85, "Health check failed, rolling back")
			if err := o.rollbackUpdate(ctx, operationID, container, backupPath); err != nil {
				o.failOperation(ctx, operationID, "failed", fmt.Sprintf("Rollback failed: %v", err))
				return
			}
			o.failOperation(ctx, operationID, "failed", "Health check failed, rolled back successfully")
			return
		}
		o.failOperation(ctx, operationID, "health_check", fmt.Sprintf("Health check failed: %v", err))
		return
	}

	log.Printf("UPDATE: Health check passed for operation=%s, marking complete", operationID)
	o.publishProgress(operationID, container.Name, stackName, "complete", 100, "Update completed successfully")

	now := time.Now()
	op, found, _ = o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.Status = "complete"
		op.CompletedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	// Publish container updated event for dashboard refresh
	if o.eventBus != nil {
		o.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"container_id":   container.ID,
				"container_name": container.Name,
				"operation_id":   operationID,
				"status":         "updated",
			},
		})
	}
}

// executeBatchUpdate executes batch update workflow.
func (o *UpdateOrchestrator) executeBatchUpdate(ctx context.Context, operationID string, containers []*docker.Container, targetVersions map[string]string, stackName string) {
	defer o.releaseStackLock(stackName)

	successCount := 0
	failCount := 0

	for i, container := range containers {
		progress := (i * 100) / len(containers)
		o.publishProgress(operationID, container.Name, stackName, "processing", progress, fmt.Sprintf("Processing %s (%d/%d)", container.Name, i+1, len(containers)))

		// Get target version from map, skip if not provided
		targetVersion := ""
		if targetVersions != nil {
			if v, ok := targetVersions[container.Name]; ok {
				targetVersion = v
			}
		}

		// Skip containers without a detected update version
		if targetVersion == "" {
			o.publishProgress(operationID, container.Name, stackName, "skipped", progress, fmt.Sprintf("Skipping %s (no update available)", container.Name))
			continue
		}

		childOpID := uuid.New().String()
		o.executeSingleUpdate(ctx, childOpID, container, targetVersion, stackName)

		childOp, found, _ := o.storage.GetUpdateOperation(ctx, childOpID)
		if found && childOp.Status == "complete" {
			successCount++
		} else {
			failCount++
		}
	}

	status := "complete"
	message := fmt.Sprintf("Batch update completed: %d succeeded, %d failed", successCount, failCount)
	if failCount > 0 && successCount == 0 {
		status = "failed"
	}

	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.Status = status
		op.CompletedAt = &now
		op.ErrorMessage = message
		o.storage.SaveUpdateOperation(ctx, op)
	}

	o.publishProgress(operationID, "", stackName, status, 100, message)
}

// checkPermissions validates Docker access and file permissions.
func (o *UpdateOrchestrator) checkPermissions(ctx context.Context, container *docker.Container) error {
	if _, err := o.dockerSDK.Ping(ctx); err != nil {
		return fmt.Errorf("docker socket access denied: %w", err)
	}

	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath != "" {
		// Try to access the compose file, handling both .yml and .yaml extensions
		resolvedPath, err := o.resolveComposeFile(composeFilePath)
		if err != nil {
			return fmt.Errorf("cannot access compose file at %s: %w (ensure path is mounted in container)", composeFilePath, err)
		}

		// Check write permissions in compose directory
		backupDir := filepath.Dir(resolvedPath)
		testFile := filepath.Join(backupDir, ".docksmith-permission-test")
		if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
			return fmt.Errorf("no write permission in compose directory %s: %w", backupDir, err)
		}
		os.Remove(testFile)
	}

	return nil
}

// resolveComposeFile resolves the compose file path, trying alternate extensions if needed.
func (o *UpdateOrchestrator) resolveComposeFile(path string) (string, error) {
	// Try the path as-is
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	// Try alternate extension (.yaml <-> .yml)
	alternatePath := ""
	if strings.HasSuffix(path, ".yaml") {
		alternatePath = strings.TrimSuffix(path, ".yaml") + ".yml"
	} else if strings.HasSuffix(path, ".yml") {
		alternatePath = strings.TrimSuffix(path, ".yml") + ".yaml"
	}

	if alternatePath != "" {
		if _, err := os.Stat(alternatePath); err == nil {
			return alternatePath, nil
		}
	}

	return "", fmt.Errorf("file not found (tried %s)", path)
}

// backupComposeFile creates a timestamped backup of the compose file.
func (o *UpdateOrchestrator) backupComposeFile(ctx context.Context, operationID string, container *docker.Container) (string, error) {
	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath == "" {
		return "", nil
	}

	// Resolve the actual file path (handles extension variations)
	resolvedPath, err := o.resolveComposeFile(composeFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve compose file: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.backup.%s", resolvedPath, timestamp)

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("failed to read compose file: %w", err)
	}

	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write backup: %w", err)
	}

	if _, err := os.Stat(backupPath); err != nil {
		return "", fmt.Errorf("backup validation failed: %w", err)
	}

	stackName := o.stackManager.DetermineStack(ctx, *container)
	backup := storage.ComposeBackup{
		OperationID:     operationID,
		ContainerName:   container.Name,
		StackName:       stackName,
		ComposeFilePath: resolvedPath,
		BackupFilePath:  backupPath,
		BackupTimestamp: time.Now(),
	}

	if err := o.storage.SaveComposeBackup(ctx, backup); err != nil {
		return "", fmt.Errorf("failed to save backup metadata: %w", err)
	}

	return backupPath, nil
}

// updateComposeFile updates the image tag in the compose file.
func (o *UpdateOrchestrator) updateComposeFile(ctx context.Context, composeFilePath string, container *docker.Container, newTag string) error {
	data, err := os.ReadFile(composeFilePath)
	if err != nil {
		return fmt.Errorf("failed to read compose file: %w", err)
	}

	var compose map[string]interface{}
	if err := yaml.Unmarshal(data, &compose); err != nil {
		return fmt.Errorf("failed to parse compose file: %w", err)
	}

	services, ok := compose["services"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid compose file: no services section")
	}

	serviceName := container.Labels["com.docker.compose.service"]
	if serviceName == "" {
		return fmt.Errorf("container has no service label")
	}

	service, ok := services[serviceName].(map[string]interface{})
	if !ok {
		return fmt.Errorf("service %s not found in compose file", serviceName)
	}

	imageStr, ok := service["image"].(string)
	if !ok {
		return fmt.Errorf("service %s has no image field", serviceName)
	}

	parts := strings.Split(imageStr, ":")
	if len(parts) > 1 {
		parts[len(parts)-1] = newTag
	} else {
		parts = append(parts, newTag)
	}
	service["image"] = strings.Join(parts, ":")

	modifiedData, err := yaml.Marshal(compose)
	if err != nil {
		return fmt.Errorf("failed to marshal compose file: %w", err)
	}

	tempFile := composeFilePath + ".tmp"
	if err := os.WriteFile(tempFile, modifiedData, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tempFile, composeFilePath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// pullImage pulls a Docker image with retry logic.
func (o *UpdateOrchestrator) pullImage(ctx context.Context, imageRef string, progressChan chan<- PullProgress) error {
	maxRetries := 3
	backoff := time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(backoff)
			backoff *= 2
		}

		reader, err := o.dockerSDK.ImagePull(ctx, imageRef, image.PullOptions{})
		if err != nil {
			if attempt < maxRetries-1 {
				continue
			}
			return fmt.Errorf("failed to pull image after %d attempts: %w", maxRetries, err)
		}
		defer reader.Close()

		decoder := json.NewDecoder(reader)
		for {
			var event map[string]interface{}
			if err := decoder.Decode(&event); err != nil {
				if err == io.EOF {
					break
				}
				return fmt.Errorf("failed to decode pull progress: %w", err)
			}

			if status, ok := event["status"].(string); ok {
				progress := PullProgress{Status: status}
				if progressDetail, ok := event["progressDetail"].(map[string]interface{}); ok {
					if current, ok := progressDetail["current"].(float64); ok {
						if total, ok := progressDetail["total"].(float64); ok && total > 0 {
							progress.Percent = int((current / total) * 100)
						}
					}
				}
				select {
				case progressChan <- progress:
				default:
				}
			}
		}

		return nil
	}

	return fmt.Errorf("failed to pull image after retries")
}

// restartContainerWithDependents recreates a container and its dependents using Docker SDK.
// This function stops and starts containers in the proper order to handle dependencies.
func (o *UpdateOrchestrator) restartContainerWithDependents(ctx context.Context, operationID, containerName, stackName, newImageRef string) ([]string, error) {
	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Build dependency graph to identify dependent containers
	depGraph := o.graphBuilder.BuildFromContainers(containers)
	dependents := depGraph.GetDependents(containerName)

	log.Printf("UPDATE: Stopping container %s and %d dependents", containerName, len(dependents))

	// Stop dependents first (in reverse dependency order)
	for i := len(dependents) - 1; i >= 0; i-- {
		dependent := dependents[i]
		log.Printf("UPDATE: Stopping dependent container %s", dependent)
		o.publishProgress(operationID, containerName, stackName, "recreating", 62, fmt.Sprintf("Stopping dependent: %s", dependent))

		timeout := 10
		if err := o.dockerSDK.ContainerStop(ctx, dependent, container.StopOptions{Timeout: &timeout}); err != nil {
			log.Printf("UPDATE: Warning - failed to stop dependent %s: %v", dependent, err)
		}
	}

	// Get container config before removing it
	inspect, err := o.dockerSDK.ContainerInspect(ctx, containerName)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container %s: %w", containerName, err)
	}

	// Stop main container
	log.Printf("UPDATE: Stopping main container %s", containerName)
	o.publishProgress(operationID, containerName, stackName, "recreating", 65, "Stopping container")

	timeout := 10
	if err := o.dockerSDK.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout}); err != nil {
		return nil, fmt.Errorf("failed to stop container %s: %w", containerName, err)
	}

	// Remove main container so it gets recreated with new image
	log.Printf("UPDATE: Removing container %s to force recreation", containerName)
	o.publishProgress(operationID, containerName, stackName, "recreating", 67, "Removing old container")

	if err := o.dockerSDK.ContainerRemove(ctx, containerName, container.RemoveOptions{}); err != nil {
		return nil, fmt.Errorf("failed to remove container %s: %w", containerName, err)
	}

	// Recreate container with updated image
	log.Printf("UPDATE: Recreating container %s with new image %s", containerName, newImageRef)
	o.publishProgress(operationID, containerName, stackName, "recreating", 68, "Creating new container")

	// Update the image in the config
	newConfig := inspect.Config
	newConfig.Image = newImageRef

	// Build network config from existing networks
	networkingConfig := &network.NetworkingConfig{
		EndpointsConfig: inspect.NetworkSettings.Networks,
	}

	if _, err := o.dockerSDK.ContainerCreate(ctx, newConfig, inspect.HostConfig, networkingConfig, nil, containerName); err != nil {
		return nil, fmt.Errorf("failed to create container %s: %w", containerName, err)
	}

	// Start the newly created container
	log.Printf("UPDATE: Starting main container %s", containerName)
	o.publishProgress(operationID, containerName, stackName, "recreating", 70, "Starting container")

	if err := o.dockerSDK.ContainerStart(ctx, containerName, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container %s: %w", containerName, err)
	}

	// Start dependents (in normal dependency order)
	for i, dependent := range dependents {
		log.Printf("UPDATE: Starting dependent container %s", dependent)
		o.publishProgress(operationID, containerName, stackName, "recreating", 70+((i+1)*5), fmt.Sprintf("Starting dependent: %s", dependent))

		if err := o.dockerSDK.ContainerStart(ctx, dependent, container.StartOptions{}); err != nil {
			log.Printf("UPDATE: Warning - failed to start dependent %s: %v", dependent, err)
		}
	}

	log.Printf("UPDATE: Successfully restarted container %s and %d dependents", containerName, len(dependents))

	return dependents, nil
}

// waitForHealthy waits for a container to become healthy or confirms it's running.
// For containers with health checks, polls until status is "healthy" or times out.
// For containers without health checks, waits briefly then verifies the container is running.
func (o *UpdateOrchestrator) waitForHealthy(ctx context.Context, containerName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inspect, err := o.dockerSDK.ContainerInspect(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	hasHealthCheck := inspect.State != nil && inspect.State.Health != nil

	if hasHealthCheck {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return fmt.Errorf("health check timeout")
			case <-ticker.C:
				inspect, err := o.dockerSDK.ContainerInspect(ctx, containerName)
				if err != nil {
					return fmt.Errorf("failed to inspect container: %w", err)
				}

				if inspect.State.Health.Status == "healthy" {
					return nil
				}

				if inspect.State.Health.Status == "unhealthy" {
					return fmt.Errorf("container is unhealthy")
				}
			}
		}
	} else {
		time.Sleep(o.healthCheckCfg.FallbackWait)

		inspect, err := o.dockerSDK.ContainerInspect(ctx, containerName)
		if err != nil {
			return fmt.Errorf("failed to inspect container: %w", err)
		}

		if !inspect.State.Running {
			return fmt.Errorf("container is not running")
		}

		return nil
	}
}

// shouldAutoRollback determines if auto-rollback should be performed based on container labels,
// stack-level policy, or global policy configuration.
func (o *UpdateOrchestrator) shouldAutoRollback(ctx context.Context, containerName string) (bool, error) {
	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return false, err
	}

	var targetContainer *docker.Container
	for _, c := range containers {
		if c.Name == containerName {
			targetContainer = &c
			break
		}
	}

	if targetContainer == nil {
		return false, fmt.Errorf("container not found")
	}

	if label, ok := targetContainer.Labels["docksmith.auto_rollback"]; ok {
		return strings.ToLower(label) == "true", nil
	}

	stackName := o.stackManager.DetermineStack(ctx, *targetContainer)
	if stackName != "" {
		policy, found, _ := o.storage.GetRollbackPolicy(ctx, "stack", stackName)
		if found {
			return policy.AutoRollbackEnabled, nil
		}
	}

	policy, found, _ := o.storage.GetRollbackPolicy(ctx, "global", "")
	if found {
		return policy.AutoRollbackEnabled, nil
	}

	inspect, err := o.dockerSDK.ContainerInspect(ctx, containerName)
	if err != nil {
		return false, nil
	}

	hasHealthCheck := inspect.State != nil && inspect.State.Health != nil
	if !hasHealthCheck {
		return false, nil
	}

	return false, nil
}

// rollbackUpdate performs rollback to the previous version using Docker SDK.
func (o *UpdateOrchestrator) rollbackUpdate(ctx context.Context, operationID string, cont *docker.Container, backupPath string) error {
	if backupPath == "" {
		backup, found, _ := o.storage.GetComposeBackup(ctx, operationID)
		if !found {
			return fmt.Errorf("no backup found for operation")
		}
		backupPath = backup.BackupFilePath
	}

	// Restore backup compose file
	composeFilePath := o.getComposeFilePath(cont)
	if composeFilePath != "" {
		data, err := os.ReadFile(backupPath)
		if err != nil {
			return fmt.Errorf("failed to read backup: %w", err)
		}

		resolvedPath, err := o.resolveComposeFile(composeFilePath)
		if err != nil {
			return fmt.Errorf("failed to resolve compose file: %w", err)
		}

		if err := os.WriteFile(resolvedPath, data, 0644); err != nil {
			return fmt.Errorf("failed to restore compose file: %w", err)
		}

		log.Printf("ROLLBACK: Restored compose file from backup for container %s", cont.Name)
	}

	// Extract old image tag from backup
	var compose map[string]interface{}
	data, _ := os.ReadFile(backupPath)
	yaml.Unmarshal(data, &compose)

	oldImageTag := ""
	serviceName := cont.Labels["com.docker.compose.service"]
	if services, ok := compose["services"].(map[string]interface{}); ok {
		if service, ok := services[serviceName].(map[string]interface{}); ok {
			if image, ok := service["image"].(string); ok {
				oldImageTag = image
			}
		}
	}

	// Pull old image
	if oldImageTag != "" {
		log.Printf("ROLLBACK: Pulling old image %s for container %s", oldImageTag, cont.Name)
		progressChan := make(chan PullProgress, 10)
		go func() {
			for range progressChan {
			}
		}()
		if err := o.pullImage(ctx, oldImageTag, progressChan); err != nil {
			log.Printf("ROLLBACK: Warning - failed to pull old image: %v", err)
		}
		close(progressChan)
	}

	// Restart container using SDK
	log.Printf("ROLLBACK: Restarting container %s", cont.Name)
	timeout := 10
	if err := o.dockerSDK.ContainerStop(ctx, cont.Name, container.StopOptions{Timeout: &timeout}); err != nil {
		log.Printf("ROLLBACK: Warning - failed to stop container: %v", err)
	}

	if err := o.dockerSDK.ContainerStart(ctx, cont.Name, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start container during rollback: %w", err)
	}

	log.Printf("ROLLBACK: Successfully restarted container %s with old version", cont.Name)

	// Verify health after rollback
	if err := o.waitForHealthy(ctx, cont.Name, o.healthCheckCfg.Timeout); err != nil {
		log.Printf("ROLLBACK: Warning - health check failed after rollback: %v", err)
	}

	// Update operation metadata
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.RollbackOccurred = true
		o.storage.SaveUpdateOperation(ctx, op)
	}

	return nil
}

// attemptRollback attempts rollback if enabled.
func (o *UpdateOrchestrator) attemptRollback(ctx context.Context, operationID string, container *docker.Container, backupPath string) {
	shouldRollback, _ := o.shouldAutoRollback(ctx, container.Name)
	if shouldRollback {
		o.rollbackUpdate(ctx, operationID, container, backupPath)
	}
}

// getComposeFilePath extracts the compose file path from container labels.
// It translates host paths to container paths based on volume mounts.
func (o *UpdateOrchestrator) getComposeFilePath(container *docker.Container) string {
	if path, ok := container.Labels["com.docker.compose.project.config_files"]; ok {
		paths := strings.Split(path, ",")
		if len(paths) > 0 {
			hostPath := strings.TrimSpace(paths[0])
			return o.translatePathToContainer(hostPath)
		}
	}
	return ""
}

// translatePathToContainer translates a host path to the equivalent container path.
// This handles the volume mount mapping: /home/chis/www -> /www
// When running outside Docker (locally), returns the original path.
func (o *UpdateOrchestrator) translatePathToContainer(hostPath string) string {
	// Check if we're running inside Docker by looking for /.dockerenv
	if _, err := os.Stat("/.dockerenv"); os.IsNotExist(err) {
		// Not in Docker - return original path (no translation needed)
		return hostPath
	}

	// Map of host prefixes to container paths (only used when running in Docker)
	pathMappings := map[string]string{
		"/home/chis/www/":     "/www/",
		"/home/chis/torrent/": "/torrent/",
	}

	for hostPrefix, containerPrefix := range pathMappings {
		if strings.HasPrefix(hostPath, hostPrefix) {
			return strings.Replace(hostPath, hostPrefix, containerPrefix, 1)
		}
	}

	// If no mapping found, return original path (might be already a container path)
	return hostPath
}

// buildImageRef constructs the full image reference with tag.
func (o *UpdateOrchestrator) buildImageRef(currentImage, targetVersion string) string {
	parts := strings.Split(currentImage, ":")
	if len(parts) > 1 {
		parts[len(parts)-1] = targetVersion
		return strings.Join(parts, ":")
	}
	return currentImage + ":" + targetVersion
}

// publishProgress publishes progress events to the event bus for UI updates.
func (o *UpdateOrchestrator) publishProgress(operationID, containerName, stackName, stage string, percent int, message string) {
	if o.eventBus == nil {
		log.Printf("PROGRESS: eventBus is nil, skipping publish for operation=%s stage=%s", operationID, stage)
		return
	}
	log.Printf("PROGRESS: Publishing %s (%d%%) for operation=%s", stage, percent, operationID)

	// Get container ID for the dashboard to refresh the specific card
	ctx := context.Background()
	containers, err := o.dockerClient.ListContainers(ctx)
	var containerID string
	if err == nil {
		for _, c := range containers {
			if c.Name == containerName {
				containerID = c.ID
				break
			}
		}
	}

	o.eventBus.Publish(events.Event{
		Type: events.EventUpdateProgress,
		Payload: map[string]interface{}{
			"operation_id":   operationID,
			"container_id":   containerID,
			"container_name": containerName,
			"stack_name":     stackName,
			"stage":          stage,
			"progress":       percent,
			"message":        message,
			"timestamp":      time.Now().Unix(),
		},
	})
}

// failOperation marks an operation as failed.
func (o *UpdateOrchestrator) failOperation(ctx context.Context, operationID, stage, errorMsg string) {
	o.storage.UpdateOperationStatus(ctx, operationID, "failed", errorMsg)
	o.publishProgress(operationID, "", "", "failed", 0, errorMsg)

	// Get operation details to publish container updated event
	if op, found, _ := o.storage.GetUpdateOperation(ctx, operationID); found && o.eventBus != nil {
		o.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"container_id":   op.ContainerID,
				"container_name": op.ContainerName,
				"operation_id":   operationID,
				"status":         "failed",
			},
		})
	}
}

// acquireStackLock attempts to acquire a lock for a stack.
func (o *UpdateOrchestrator) acquireStackLock(stackName string) bool {
	o.locksMu.Lock()
	defer o.locksMu.Unlock()

	if _, exists := o.stackLocks[stackName]; !exists {
		o.stackLocks[stackName] = &sync.Mutex{}
	}

	locked := o.stackLocks[stackName].TryLock()
	return locked
}

// releaseStackLock releases a stack lock.
func (o *UpdateOrchestrator) releaseStackLock(stackName string) {
	o.locksMu.Lock()
	defer o.locksMu.Unlock()

	if lock, exists := o.stackLocks[stackName]; exists {
		lock.Unlock()
	}
}

// queueOperation adds an operation to the queue.
func (o *UpdateOrchestrator) queueOperation(ctx context.Context, operationID, stackName string, containers []string, operationType string) error {
	queue := storage.UpdateQueue{
		OperationID: operationID,
		StackName:   stackName,
		Containers:  containers,
		QueuedAt:    time.Now(),
	}

	if err := o.storage.QueueUpdate(ctx, queue); err != nil {
		return err
	}

	op := storage.UpdateOperation{
		OperationID:   operationID,
		StackName:     stackName,
		OperationType: operationType,
		Status:        "queued",
	}
	return o.storage.SaveUpdateOperation(ctx, op)
}

// processQueue processes queued operations in the background.
func (o *UpdateOrchestrator) processQueue(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			queued, err := o.storage.GetQueuedUpdates(ctx)
			if err != nil {
				continue
			}

			for _, q := range queued {
				if o.acquireStackLock(q.StackName) {
					o.storage.DequeueUpdate(ctx, q.StackName)

					_, found, _ := o.storage.GetUpdateOperation(ctx, q.OperationID)
					if found {
						containers, _ := o.dockerClient.ListContainers(ctx)
						targetContainers := make([]*docker.Container, 0)
						for _, name := range q.Containers {
							for _, c := range containers {
								if c.Name == name {
									targetContainers = append(targetContainers, &c)
									break
								}
							}
						}

						if len(targetContainers) == 1 {
							go o.executeSingleUpdate(ctx, q.OperationID, targetContainers[0], "latest", q.StackName)
						} else {
							go o.executeBatchUpdate(ctx, q.OperationID, targetContainers, nil, q.StackName)
						}
					}
				}
			}
		}
	}
}

// CancelQueuedOperation cancels a queued operation.
func (o *UpdateOrchestrator) CancelQueuedOperation(ctx context.Context, operationID string) error {
	op, found, err := o.storage.GetUpdateOperation(ctx, operationID)
	if err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("operation not found")
	}

	if op.Status != "queued" {
		return fmt.Errorf("operation is not queued (status: %s)", op.Status)
	}

	return o.storage.UpdateOperationStatus(ctx, operationID, "cancelled", "Cancelled by user")
}
