package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/scripts"
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
	pathTranslator *docker.PathTranslator
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
	pathTranslator *docker.PathTranslator,
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
		stackLocks:     make(map[string]*sync.Mutex),
		pathTranslator: pathTranslator,
	}

	go orch.processQueue(context.Background())

	return orch
}

// GetStorage returns the storage instance for accessing operation status
func (o *UpdateOrchestrator) GetStorage() storage.Storage {
	return o.storage
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

	// Extract current version from container's image tag
	currentVersion := ""
	if parts := strings.Split(targetContainer.Image, ":"); len(parts) >= 2 {
		currentVersion = parts[len(parts)-1]
	}

	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   targetContainer.ID,
		ContainerName: containerName,
		StackName:     stackName,
		OperationType: "single",
		Status:        "validating",
		OldVersion:    currentVersion,
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

	// For single-container batches, populate container details like a single update
	if len(orderedContainers) == 1 {
		container := orderedContainers[0]
		currentVersion := ""
		if parts := strings.Split(container.Image, ":"); len(parts) >= 2 {
			currentVersion = parts[len(parts)-1]
		}
		op.ContainerID = container.ID
		op.ContainerName = container.Name
		op.OldVersion = currentVersion
		if targetVersions != nil {
			if targetVer, ok := targetVersions[container.Name]; ok {
				op.NewVersion = targetVer
			}
		}
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
	if found && op.OldVersion == "" {
		// Only set OldVersion if not already set (e.g., by batch update initialization)
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

	_, err = o.restartContainerWithDependents(ctx, operationID, container.Name, stackName, newImageRef)
	if err != nil {
		o.failOperation(ctx, operationID, "recreating", fmt.Sprintf("Recreation failed: %v", err))
		o.attemptRollback(ctx, operationID, container, backupPath)
		return
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

	// Restart dependent containers (those with docksmith.restart-depends-on label)
	if err := o.restartDependentContainers(ctx, container.Name); err != nil {
		log.Printf("UPDATE: Warning - failed to restart dependent containers for %s: %v", container.Name, err)
		// Don't fail the update if dependent restarts fail
	}

	// Execute post-update actions if configured
	if postUpdateHandler := NewPostUpdateHandler(o.dockerClient); postUpdateHandler != nil {
		// Use host path for docker compose commands
		composeFilePath := o.getComposeFilePathForHost(container)
		if err := postUpdateHandler.ExecutePostUpdateActions(ctx, *container, composeFilePath); err != nil {
			log.Printf("POST-UPDATE: Warning - post-update actions failed for %s: %v", container.Name, err)
			// Don't fail the update if post-update actions fail
		}
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

	// Filter containers that have target versions
	updateContainers := make([]*docker.Container, 0)
	for _, container := range containers {
		if targetVersions != nil {
			if _, ok := targetVersions[container.Name]; ok {
				updateContainers = append(updateContainers, container)
			}
		}
	}

	if len(updateContainers) == 0 {
		o.publishProgress(operationID, "", stackName, "complete", 100, "No containers to update")
		return
	}

	// Phase 1: Backup and update all compose files first (10-30%)
	o.publishProgress(operationID, "", stackName, "backup", 10, fmt.Sprintf("Backing up %d compose files", len(updateContainers)))

	backupPaths := make(map[string]string)
	for i, container := range updateContainers {
		progress := 10 + (i * 20 / len(updateContainers))
		o.publishProgress(operationID, container.Name, stackName, "backup", progress, fmt.Sprintf("Backing up %s", container.Name))

		backupPath, err := o.backupComposeFile(ctx, operationID, container)
		if err != nil {
			o.failOperation(ctx, operationID, "backup", fmt.Sprintf("Failed to backup %s: %v", container.Name, err))
			return
		}
		backupPaths[container.Name] = backupPath

		// Update compose file with new version
		if backupPath != "" {
			targetVersion := targetVersions[container.Name]
			composeFilePath := o.getComposeFilePath(container)
			resolvedPath, err := o.resolveComposeFile(composeFilePath)
			if err != nil {
				o.failOperation(ctx, operationID, "updating_compose", fmt.Sprintf("Failed to resolve compose for %s: %v", container.Name, err))
				return
			}

			if err := o.updateComposeFile(ctx, resolvedPath, container, targetVersion); err != nil {
				o.failOperation(ctx, operationID, "updating_compose", fmt.Sprintf("Failed to update compose for %s: %v", container.Name, err))
				return
			}
		}
	}

	// Phase 2: Pull all images (30-60%)
	o.publishProgress(operationID, "", stackName, "pulling_image", 30, fmt.Sprintf("Pulling %d images", len(updateContainers)))

	for i, container := range updateContainers {
		targetVersion := targetVersions[container.Name]
		newImageRef := strings.Split(container.Image, ":")[0] + ":" + targetVersion

		progress := 30 + (i * 30 / len(updateContainers))
		o.publishProgress(operationID, container.Name, stackName, "pulling_image", progress, fmt.Sprintf("Pulling %s", newImageRef))

		progressChan := make(chan PullProgress, 10)
		pullDone := make(chan error, 1)

		go func() {
			pullDone <- o.pullImage(ctx, newImageRef, progressChan)
			close(progressChan)
		}()

		// Drain progress channel
		for range progressChan {
		}

		if err := <-pullDone; err != nil {
			log.Printf("BATCH UPDATE: Warning - failed to pull %s: %v", newImageRef, err)
		}
	}

	// Phase 3: Recreate all containers respecting dependency order (60-90%)
	o.publishProgress(operationID, "", stackName, "recreating", 60, "Recreating containers in dependency order")

	// Build dependency graph
	allContainers, _ := o.dockerClient.ListContainers(ctx)
	depGraph := o.graphBuilder.BuildFromContainers(allContainers)
	updateOrder, _ := depGraph.GetUpdateOrder()

	// Order our containers by dependency order
	orderedContainers := make([]*docker.Container, 0)
	containerMap := make(map[string]*docker.Container)
	for _, c := range updateContainers {
		containerMap[c.Name] = c
	}

	for _, name := range updateOrder {
		if c, found := containerMap[name]; found {
			orderedContainers = append(orderedContainers, c)
		}
	}

	// If not in dependency graph, add remaining
	if len(orderedContainers) < len(updateContainers) {
		for _, c := range updateContainers {
			found := false
			for _, oc := range orderedContainers {
				if oc.Name == c.Name {
					found = true
					break
				}
			}
			if !found {
				orderedContainers = append(orderedContainers, c)
			}
		}
	}

	// Stop containers in reverse order (dependents first)
	o.publishProgress(operationID, "", stackName, "recreating", 65, "Stopping containers")
	for i := len(orderedContainers) - 1; i >= 0; i-- {
		cont := orderedContainers[i]
		log.Printf("BATCH UPDATE: Stopping %s", cont.Name)
		timeout := 10
		if err := o.dockerSDK.ContainerStop(ctx, cont.Name, container.StopOptions{Timeout: &timeout}); err != nil {
			log.Printf("BATCH UPDATE: Warning - failed to stop %s: %v", cont.Name, err)
		}
	}

	// Remove and recreate in dependency order
	successCount := 0
	failCount := 0

	for i, cont := range orderedContainers {
		targetVersion := targetVersions[cont.Name]
		newImageRef := strings.Split(cont.Image, ":")[0] + ":" + targetVersion

		progress := 70 + (i * 20 / len(orderedContainers))
		o.publishProgress(operationID, cont.Name, stackName, "recreating", progress, fmt.Sprintf("Recreating %s", cont.Name))

		inspect, err := o.dockerSDK.ContainerInspect(ctx, cont.Name)
		if err != nil {
			log.Printf("BATCH UPDATE: Failed to inspect %s: %v", cont.Name, err)
			failCount++
			continue
		}

		// Remove container
		if err := o.dockerSDK.ContainerRemove(ctx, cont.Name, container.RemoveOptions{}); err != nil {
			log.Printf("BATCH UPDATE: Failed to remove %s: %v", cont.Name, err)
			failCount++
			continue
		}

		// Create with new image
		newConfig := inspect.Config
		newConfig.Image = newImageRef
		networkingConfig := &network.NetworkingConfig{
			EndpointsConfig: inspect.NetworkSettings.Networks,
		}

		if _, err := o.dockerSDK.ContainerCreate(ctx, newConfig, inspect.HostConfig, networkingConfig, nil, cont.Name); err != nil {
			log.Printf("BATCH UPDATE: Failed to create %s: %v", cont.Name, err)
			failCount++
			continue
		}

		// Start container
		if err := o.dockerSDK.ContainerStart(ctx, cont.Name, container.StartOptions{}); err != nil {
			log.Printf("BATCH UPDATE: Failed to start %s: %v", cont.Name, err)
			failCount++
			continue
		}

		log.Printf("BATCH UPDATE: Successfully recreated %s with %s", cont.Name, newImageRef)
		successCount++
	}

	// Phase 4: Health check (90-100%)
	o.publishProgress(operationID, "", stackName, "health_check", 90, "Verifying container health")

	// Wait for all containers to be healthy
	for _, cont := range orderedContainers {
		if err := o.waitForHealthy(ctx, cont.Name, o.healthCheckCfg.Timeout); err != nil {
			log.Printf("BATCH UPDATE: Health check warning for %s: %v", cont.Name, err)
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

// backupComposeFile validates the compose file exists.
// No longer creates physical backup files - rollback uses database state instead.
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

	// Just validate the compose file is readable
	if _, err := os.ReadFile(resolvedPath); err != nil {
		return "", fmt.Errorf("failed to read compose file: %w", err)
	}

	log.Printf("UPDATE: Validated compose file for backup: %s", resolvedPath)
	// Return empty string - no physical backup file created
	return "", nil
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

// restartContainerWithDependents recreates a container using docker compose.
// Note: This does NOT automatically restart dependent containers.
// Explicit restart dependencies should use the docksmith.restart-depends-on label.
func (o *UpdateOrchestrator) restartContainerWithDependents(ctx context.Context, operationID, containerName, stackName, newImageRef string) ([]string, error) {
	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *docker.Container
	var hostComposePath, containerComposePath string
	for _, c := range containers {
		if c.Name == containerName {
			targetContainer = &c
			// Get both host path (for --project-directory) and container path (for -f flag)
			hostComposePath = o.getComposeFilePathForHost(&c)
			containerComposePath = o.getComposeFilePath(&c)
			break
		}
	}

	if targetContainer == nil {
		return nil, fmt.Errorf("container %s not found", containerName)
	}

	// If we have a compose file, use compose-based recreation (preferred)
	if hostComposePath != "" && containerComposePath != "" {
		log.Printf("UPDATE: Using compose-based recreation for %s", containerName)
		o.publishProgress(operationID, containerName, stackName, "recreating", 65, "Recreating with docker compose")

		// Create compose recreator
		recreator := compose.NewRecreator(o.dockerClient)

		// Recreate the main service (compose file already updated with new image tag)
		// Use host path for --project-directory and container path for -f
		if err := recreator.RecreateWithCompose(ctx, targetContainer, hostComposePath, containerComposePath); err != nil {
			return nil, fmt.Errorf("compose recreation failed: %w", err)
		}

		o.publishProgress(operationID, containerName, stackName, "recreating", 70, "Container recreated")

		log.Printf("UPDATE: Successfully recreated %s using docker compose", containerName)
		return nil, nil
	}

	// Fallback to SDK-based recreation for non-compose containers
	log.Printf("UPDATE: No compose file available, using SDK-based recreation")
	return o.restartContainerWithSDK(ctx, operationID, containerName, stackName, newImageRef)
}

// restartContainerWithSDK is the old SDK-based recreation method (fallback for non-compose containers)
func (o *UpdateOrchestrator) restartContainerWithSDK(ctx context.Context, operationID, containerName, stackName, newImageRef string) ([]string, error) {
	// For now, return an error - we're moving to compose-based updates
	// This fallback can be implemented later if needed for non-compose containers
	return nil, fmt.Errorf("SDK-based recreation not supported - container must be managed by docker compose")
}

// restartDependentContainers finds and restarts containers that depend on the given container
// via the docksmith.restart-depends-on label
func (o *UpdateOrchestrator) restartDependentContainers(ctx context.Context, containerName string) error {
	// Get docker service to find dependents
	dockerService, err := docker.NewService()
	if err != nil {
		return fmt.Errorf("failed to create docker service: %w", err)
	}
	defer dockerService.Close()

	// Find containers that have this container in their restart-depends-on label
	dependents, err := dockerService.FindDependentContainers(ctx, containerName, scripts.RestartDependsOnLabel)
	if err != nil {
		return fmt.Errorf("failed to find dependent containers: %w", err)
	}

	if len(dependents) == 0 {
		log.Printf("UPDATE: No dependent containers found for %s", containerName)
		return nil
	}

	log.Printf("UPDATE: Found %d dependent container(s) for %s: %v", len(dependents), containerName, dependents)

	// Get full container info for pre-update checks
	containers, err := dockerService.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	containerMap := make(map[string]*docker.Container)
	for i := range containers {
		containerMap[containers[i].Name] = &containers[i]
	}

	// Restart each dependent
	for _, dep := range dependents {
		depContainer := containerMap[dep]
		if depContainer == nil {
			log.Printf("UPDATE: Dependent container %s not found, skipping", dep)
			continue
		}

		// Run pre-update check if configured
		if scriptPath, ok := depContainer.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
			log.Printf("UPDATE: Running pre-update check for dependent %s", dep)

			// Translate path if needed
			translatedPath := scriptPath
			if o.pathTranslator != nil {
				translatedPath = o.pathTranslator.TranslateToHost(scriptPath)
			}

			if err := runPreUpdateCheck(ctx, depContainer, translatedPath); err != nil {
				log.Printf("UPDATE: Pre-update check failed for dependent %s: %v (skipping restart)", dep, err)
				continue
			}
			log.Printf("UPDATE: Pre-update check passed for dependent %s", dep)
		}

		// Restart the dependent container
		log.Printf("UPDATE: Restarting dependent container: %s", dep)
		err := dockerService.GetClient().ContainerRestart(ctx, dep, container.StopOptions{})
		if err != nil {
			log.Printf("UPDATE: Failed to restart dependent %s: %v", dep, err)
			continue
		}

		log.Printf("UPDATE: Successfully restarted dependent container: %s", dep)
	}

	return nil
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

// RollbackOperation performs a rollback of a previous update operation.
// Creates a new rollback operation, updates the compose file, and recreates the container.
func (o *UpdateOrchestrator) RollbackOperation(ctx context.Context, originalOperationID string) (string, error) {
	// Get original operation
	origOp, found, err := o.storage.GetUpdateOperation(ctx, originalOperationID)
	if err != nil {
		return "", fmt.Errorf("failed to get operation: %w", err)
	}
	if !found {
		return "", fmt.Errorf("operation not found: %s", originalOperationID)
	}

	// Find the container
	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *docker.Container
	for i := range containers {
		if containers[i].Name == origOp.ContainerName {
			targetContainer = &containers[i]
			break
		}
	}
	if targetContainer == nil {
		return "", fmt.Errorf("container %s not found", origOp.ContainerName)
	}

	// Use the old version from the database - no backup file needed
	targetVersion := origOp.OldVersion
	if targetVersion == "" {
		return "", fmt.Errorf("no old version found in operation %s", originalOperationID)
	}

	log.Printf("ROLLBACK: Rolling back %s from %s to %s", origOp.ContainerName, origOp.NewVersion, targetVersion)

	// Create rollback operation
	rollbackOpID := uuid.New().String()
	rollbackOp := storage.UpdateOperation{
		OperationID:    rollbackOpID,
		ContainerID:    targetContainer.ID,
		ContainerName:  origOp.ContainerName,
		StackName:      origOp.StackName,
		OperationType:  "rollback",
		Status:         "in_progress",
		OldVersion:     origOp.NewVersion, // Current version (what we're rolling back from)
		NewVersion:     targetVersion,     // Target version from database (what we're rolling back to)
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		StartedAt:      func() *time.Time { t := time.Now(); return &t }(),
	}

	if err := o.storage.SaveUpdateOperation(ctx, rollbackOp); err != nil {
		return "", fmt.Errorf("failed to save rollback operation: %w", err)
	}

	// Execute rollback in background with a fresh context (not tied to HTTP request)
	go o.executeRollback(context.Background(), rollbackOpID, originalOperationID, targetContainer, targetVersion)

	return rollbackOpID, nil
}

// executeRollback performs the actual rollback process using the old version from database
func (o *UpdateOrchestrator) executeRollback(ctx context.Context, rollbackOpID, originalOpID string, container *docker.Container, oldVersion string) {
	stackName := container.Labels["com.docker.compose.project"]

	// Stage 1: Update compose file with old version (10-20%)
	o.publishProgress(rollbackOpID, container.Name, stackName, "updating_compose", 10, "Updating compose file with old version")

	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath != "" {
		resolvedPath, err := o.resolveComposeFile(composeFilePath)
		if err != nil {
			o.failOperation(ctx, rollbackOpID, "updating_compose", fmt.Sprintf("Failed to resolve compose file: %v", err))
			return
		}

		// Update the compose file with the old version
		if err := o.updateComposeFile(ctx, resolvedPath, container, oldVersion); err != nil {
			o.failOperation(ctx, rollbackOpID, "updating_compose", fmt.Sprintf("Failed to update compose file: %v", err))
			return
		}

		log.Printf("ROLLBACK: Updated compose file with old version %s for container %s", oldVersion, container.Name)
	}

	// Build full image reference (e.g., "traefik:3.6.1")
	imageParts := strings.Split(container.Image, ":")
	oldImageTag := container.Image
	if len(imageParts) > 0 {
		oldImageTag = imageParts[0] + ":" + oldVersion
	}

	o.publishProgress(rollbackOpID, container.Name, stackName, "validating", 20, fmt.Sprintf("Target image: %s", oldImageTag))
	log.Printf("ROLLBACK: Old image reference: %s", oldImageTag)

	// Stage 3: Pull old image (30-60%)
	o.publishProgress(rollbackOpID, container.Name, stackName, "pulling_image", 30, fmt.Sprintf("Pulling old image: %s", oldImageTag))

	progressChan := make(chan PullProgress, 10)
	pullDone := make(chan error, 1)

	go func() {
		pullDone <- o.pullImage(ctx, oldImageTag, progressChan)
		close(progressChan) // Close channel when pull is done
	}()

	// Monitor pull progress
	for progress := range progressChan {
		percent := 30
		if progress.Percent > 0 {
			percent = 30 + int(float64(progress.Percent)*0.3) // 30-60%
		}
		o.publishProgress(rollbackOpID, container.Name, stackName, "pulling_image", percent, progress.Status)
	}

	if err := <-pullDone; err != nil {
		log.Printf("ROLLBACK: Warning - failed to pull old image: %v (may already exist locally)", err)
	}

	// Stage 4: Recreate container with old image (60-80%)
	o.publishProgress(rollbackOpID, container.Name, stackName, "recreating", 60, "Recreating container with old image")

	if _, err := o.restartContainerWithDependents(ctx, rollbackOpID, container.Name, stackName, oldImageTag); err != nil {
		o.failOperation(ctx, rollbackOpID, "recreating", fmt.Sprintf("Failed to recreate container: %v", err))
		return
	}

	// Stage 5: Health check (80-95%)
	o.publishProgress(rollbackOpID, container.Name, stackName, "health_check", 80, "Verifying container health")

	if err := o.waitForHealthy(ctx, container.Name, o.healthCheckCfg.Timeout); err != nil {
		log.Printf("ROLLBACK: Warning - health check failed: %v", err)
		o.publishProgress(rollbackOpID, container.Name, stackName, "health_check", 90, fmt.Sprintf("Health check warning: %v", err))
	} else {
		o.publishProgress(rollbackOpID, container.Name, stackName, "health_check", 95, "Health check passed")
	}

	// Restart dependent containers (those with docksmith.restart-depends-on label)
	if err := o.restartDependentContainers(ctx, container.Name); err != nil {
		log.Printf("ROLLBACK: Warning - failed to restart dependent containers for %s: %v", container.Name, err)
		// Don't fail the rollback if dependent restarts fail
	}

	// Stage 6: Complete (100%)
	// Update rollback operation status BEFORE publishing complete event
	// This ensures the database is updated before the CLI sees the event and exits
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, rollbackOpID)
	if found {
		op.Status = "complete"
		op.CompletedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	// Mark original operation as rolled back
	origOp, found, _ := o.storage.GetUpdateOperation(ctx, originalOpID)
	if found {
		origOp.RollbackOccurred = true
		o.storage.SaveUpdateOperation(ctx, origOp)
	}

	// Now publish the complete event (CLI will see this and may exit)
	o.publishProgress(rollbackOpID, container.Name, stackName, "complete", 100, "Rollback completed successfully")

	// Publish container updated event
	if o.eventBus != nil {
		o.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"operation_id":   rollbackOpID,
				"container_id":   container.ID,
				"container_name": container.Name,
				"stack_name":     stackName,
				"status":         "complete",
			},
		})
	}

	log.Printf("ROLLBACK: Successfully completed rollback for container %s", container.Name)
}

// getComposeFilePath extracts the compose file path from container labels.
// It translates host paths to container paths based on volume mounts.
func (o *UpdateOrchestrator) getComposeFilePath(container *docker.Container) string {
	if path, ok := container.Labels["com.docker.compose.project.config_files"]; ok {
		paths := strings.Split(path, ",")
		if len(paths) > 0 {
			hostPath := strings.TrimSpace(paths[0])
			if o.pathTranslator != nil {
				return o.pathTranslator.TranslateToContainer(hostPath)
			}
			return hostPath
		}
	}
	return ""
}

// getComposeFilePathForHost extracts the compose file path from container labels.
// Returns the ORIGINAL host path WITHOUT translation for use with docker compose commands.
// Docker compose runs on the host (via Docker socket), so it needs host paths, not container paths.
func (o *UpdateOrchestrator) getComposeFilePathForHost(container *docker.Container) string {
	if path, ok := container.Labels["com.docker.compose.project.config_files"]; ok {
		paths := strings.Split(path, ",")
		if len(paths) > 0 {
			containerPath := strings.TrimSpace(paths[0])
			// Translate container path back to host path
			if o.pathTranslator != nil {
				return o.pathTranslator.TranslateToHost(containerPath)
			}
			return containerPath
		}
	}
	return ""
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

// runPreUpdateCheck runs a pre-update check script for a container
func runPreUpdateCheck(ctx context.Context, container *docker.Container, scriptPath string) error {
	if !docker.ValidatePreUpdateScript(scriptPath) {
		return fmt.Errorf("invalid pre-update script path: %s", scriptPath)
	}

	// Execute the check script with timeout
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, scriptPath, container.ID, container.Name)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("script exited with code %d: %s", exitErr.ExitCode(), string(output))
		}
		return fmt.Errorf("failed to execute script: %w", err)
	}

	return nil
}
