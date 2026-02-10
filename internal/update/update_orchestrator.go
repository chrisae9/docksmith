package update

import (
	"context"
	"encoding/json"
	"errors"
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
	"github.com/chis/docksmith/internal/selfupdate"
	"github.com/chis/docksmith/internal/storage"
	dockerContainer "github.com/docker/docker/api/types/container"
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
	stackLocks     map[string]*stackLockEntry
	locksMu        sync.Mutex
	batchDetailMu  sync.Mutex // protects read-modify-write on BatchDetails
	pathTranslator *docker.PathTranslator
	ctx            context.Context    // orchestrator lifecycle context
	cancelFn       context.CancelFunc // cancels ctx on shutdown
}

// stackLockEntry tracks a stack lock with its last usage time for cleanup.
type stackLockEntry struct {
	mu       sync.Mutex
	lastUsed time.Time
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
	ctx, cancel := context.WithCancel(context.Background())
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
			FallbackWait: 3 * time.Second, // Containers without health checks just need to be "running"
		},
		stackLocks:     make(map[string]*stackLockEntry),
		pathTranslator: pathTranslator,
		ctx:            ctx,
		cancelFn:       cancel,
	}

	go orch.processQueue(orch.ctx)
	go orch.cleanupStaleLocks(orch.ctx)

	return orch
}

// Shutdown stops the orchestrator's background goroutines.
// In-progress operations will continue to completion.
func (o *UpdateOrchestrator) Shutdown() {
	if o.cancelFn != nil {
		o.cancelFn()
	}
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

	// Handle empty targetVersion - for :latest images, preserve the tag
	// This prevents compose file corruption when digest-only updates are detected
	if targetVersion == "" {
		if currentVersion == "latest" {
			// For :latest images, use "latest" to preserve the tag in compose file
			targetVersion = "latest"
			log.Printf("UPDATE: Empty target version for :latest image %s, using 'latest' as target", containerName)
		} else {
			o.releaseStackLock(stackName)
			return "", fmt.Errorf("cannot update container %s: no target version specified and current version is '%s' (not :latest)", containerName, currentVersion)
		}
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

	// Only save to storage if available
	if o.storage != nil {
		if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
			o.releaseStackLock(stackName)
			return "", fmt.Errorf("failed to save operation: %w", err)
		}
	}

	go o.executeSingleUpdate(context.Background(), operationID, targetContainer, targetVersion, stackName, false)

	return operationID, nil
}

// UpdateSingleContainerInGroup initiates an update for a single container as part of a batch group.
func (o *UpdateOrchestrator) UpdateSingleContainerInGroup(ctx context.Context, containerName, targetVersion, batchGroupID string, containerMeta map[string]storage.BatchContainerDetail, forceContainers map[string]bool) (string, error) {
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

	currentVersion := ""
	if parts := strings.Split(targetContainer.Image, ":"); len(parts) >= 2 {
		currentVersion = parts[len(parts)-1]
	}

	if targetVersion == "" {
		if currentVersion == "latest" {
			targetVersion = "latest"
		} else {
			o.releaseStackLock(stackName)
			return "", fmt.Errorf("cannot update container %s: no target version specified and current version is '%s' (not :latest)", containerName, currentVersion)
		}
	}

	// Build batch detail with metadata from frontend (resolved versions, change type)
	detail := storage.BatchContainerDetail{
		ContainerName: containerName,
		StackName:     stackName,
		OldVersion:    currentVersion,
		NewVersion:    targetVersion,
	}
	if meta, ok := containerMeta[containerName]; ok {
		detail.ChangeType = meta.ChangeType
		detail.OldResolvedVersion = meta.OldResolvedVersion
		detail.NewResolvedVersion = meta.NewResolvedVersion
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
		BatchGroupID:  batchGroupID,
		BatchDetails:  []storage.BatchContainerDetail{detail},
	}

	if o.storage != nil {
		if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
			o.releaseStackLock(stackName)
			return "", fmt.Errorf("failed to save operation: %w", err)
		}
	}

	force := forceContainers[containerName]
	go o.executeSingleUpdate(context.Background(), operationID, targetContainer, targetVersion, stackName, force)

	return operationID, nil
}

// UpdateBatchContainers initiates batch updates for multiple containers.
func (o *UpdateOrchestrator) UpdateBatchContainers(ctx context.Context, containerNames []string, targetVersions map[string]string) (string, error) {
	return o.updateBatchContainersInternal(ctx, containerNames, targetVersions, "batch", "", nil, nil)
}

// UpdateBatchContainersInGroup initiates batch updates as part of a batch group.
func (o *UpdateOrchestrator) UpdateBatchContainersInGroup(ctx context.Context, containerNames []string, targetVersions map[string]string, batchGroupID string, containerMeta map[string]storage.BatchContainerDetail, forceContainers map[string]bool) (string, error) {
	return o.updateBatchContainersInternal(ctx, containerNames, targetVersions, "batch", batchGroupID, containerMeta, forceContainers)
}

func (o *UpdateOrchestrator) updateBatchContainersInternal(ctx context.Context, containerNames []string, targetVersions map[string]string, operationType string, batchGroupID string, containerMeta map[string]storage.BatchContainerDetail, forceContainers map[string]bool) (string, error) {
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
		if err := o.queueOperation(ctx, operationID, stackName, containerNames, operationType); err != nil {
			return "", fmt.Errorf("failed to queue operation: %w", err)
		}
		return operationID, nil
	}

	op := storage.UpdateOperation{
		OperationID:   operationID,
		StackName:     stackName,
		OperationType: operationType,
		Status:        "validating",
		BatchGroupID:  batchGroupID,
	}

	// Build batch details for all containers
	batchDetails := make([]storage.BatchContainerDetail, 0, len(orderedContainers))
	for _, container := range orderedContainers {
		currentVersion := ""
		if parts := strings.Split(container.Image, ":"); len(parts) >= 2 {
			currentVersion = parts[len(parts)-1]
		}

		targetVersion := ""
		if targetVersions != nil {
			if targetVer, ok := targetVersions[container.Name]; ok {
				targetVersion = targetVer
			}
		}

		// Handle empty targetVersion - for :latest images, preserve the tag
		if targetVersion == "" && currentVersion == "latest" {
			targetVersion = "latest"
			log.Printf("UPDATE: Empty target version for :latest image %s, using 'latest' as target", container.Name)
			// Update the map so executeBatchUpdate also has the corrected version
			if targetVersions == nil {
				targetVersions = make(map[string]string)
			}
			targetVersions[container.Name] = targetVersion
		}

		// Get stack name from container labels
		containerStack := o.stackManager.DetermineStack(ctx, *container)

		detail := storage.BatchContainerDetail{
			ContainerName: container.Name,
			StackName:     containerStack,
			OldVersion:    currentVersion,
			NewVersion:    targetVersion,
		}
		if containerMeta != nil {
			if meta, ok := containerMeta[container.Name]; ok {
				detail.ChangeType = meta.ChangeType
				detail.OldResolvedVersion = meta.OldResolvedVersion
				detail.NewResolvedVersion = meta.NewResolvedVersion
			}
		}

		// Capture old image digest for rollback support
		oldDigest, err := o.dockerClient.GetImageDigest(ctx, container.Image)
		if err != nil {
			log.Printf("UPDATE: Warning - could not capture old digest for %s: %v", container.Name, err)
		} else {
			detail.OldDigest = oldDigest
		}

		batchDetails = append(batchDetails, detail)
	}
	op.BatchDetails = batchDetails

	// For single-container batches, also populate top-level fields for backwards compatibility
	if len(orderedContainers) == 1 && len(batchDetails) > 0 {
		container := orderedContainers[0]
		op.ContainerID = container.ID
		op.ContainerName = container.Name
		op.OldVersion = batchDetails[0].OldVersion
		op.NewVersion = batchDetails[0].NewVersion
	} else {
		// For multi-container batches, use a summary in container_name
		op.ContainerName = fmt.Sprintf("%d containers", len(orderedContainers))
	}

	if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
		o.releaseStackLock(stackName)
		return "", fmt.Errorf("failed to save operation: %w", err)
	}

	go o.executeBatchUpdate(context.Background(), operationID, orderedContainers, targetVersions, stackName, forceContainers)

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

	go o.executeBatchUpdate(context.Background(), operationID, orderedContainers, targetVersions, stackName, nil)

	return operationID, nil
}

// executeSingleUpdate executes the update workflow for a single container.
func (o *UpdateOrchestrator) executeSingleUpdate(ctx context.Context, operationID string, container *docker.Container, targetVersion, stackName string, force bool) {
	defer o.releaseStackLock(stackName)

	log.Printf("UPDATE: Starting executeSingleUpdate for operation=%s container=%s target=%s", operationID, container.Name, targetVersion)

	// Check if this is a self-update (docksmith updating itself)
	if selfupdate.IsSelfContainer(container.ID, container.Image, container.Name) {
		log.Printf("UPDATE: Detected self-update for docksmith container %s", container.Name)
		o.executeSelfUpdate(ctx, operationID, container, targetVersion, stackName)
		return
	}

	o.publishProgress(operationID, container.Name, stackName, "validating", 0, "Validating permissions")

	if err := o.checkPermissions(ctx, container); err != nil {
		o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Permission check failed: %v", err))
		return
	}

	log.Printf("UPDATE: Permissions OK for operation=%s", operationID)

	// Run pre-update check if configured
	if scriptPath, ok := container.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
		if !force {
			log.Printf("UPDATE: Running pre-update check for container %s: %s", container.Name, scriptPath)

			// NOTE: Do NOT translate the script path - the orchestrator runs inside the container
			// where the script path (e.g., /scripts/...) is already valid
			if err := runPreUpdateCheck(ctx, container, scriptPath); err != nil {
				o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Pre-update check failed: %v", err))
				return
			}
			log.Printf("UPDATE: Pre-update check passed for container %s", container.Name)
		} else {
			log.Printf("UPDATE: Skipping pre-update check (force=true) for %s", container.Name)
		}
	}

	currentVersion, _ := o.dockerClient.GetImageVersion(ctx, container.Image)

	// Set started_at timestamp and old version before beginning actual update work
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		if op.OldVersion == "" {
			// Only set OldVersion if not already set (e.g., by batch update initialization)
			op.OldVersion = currentVersion
		}
		op.StartedAt = &now
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

	if _, err := o.restartContainerWithDependents(ctx, operationID, container.Name, stackName, newImageRef); err != nil {
		o.failOperation(ctx, operationID, "recreating", fmt.Sprintf("Recreation failed: %v", err))
		return
	}

	o.publishProgress(operationID, container.Name, stackName, "health_check", 80, "Verifying health")

	if err := o.waitForHealthy(ctx, container.Name, o.healthCheckCfg.Timeout); err != nil {
		o.failOperation(ctx, operationID, "health_check", fmt.Sprintf("Health check failed: %v", err))
		return
	}

	log.Printf("UPDATE: Health check passed for operation=%s, marking complete", operationID)
	o.publishProgress(operationID, container.Name, stackName, "complete", 100, "Update completed successfully")

	completedNow := time.Now()
	completedOp, completedFound, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if completedFound {
		completedOp.Status = "complete"
		completedOp.CompletedAt = &completedNow
		o.storage.SaveUpdateOperation(ctx, completedOp)
	}

	// Restart dependent containers (those with docksmith.restart-after label)
	// For regular updates, run pre-update checks on dependents
	depResult, depErr := o.restartDependentContainers(ctx, container.Name, false)
	if depErr != nil {
		log.Printf("UPDATE: Warning - failed to restart dependent containers for %s: %v", container.Name, depErr)
		// Don't fail the update if dependent restarts fail
	} else if depResult != nil {
		if len(depResult.Blocked) > 0 {
			log.Printf("UPDATE: Blocked dependents for %s: %v", container.Name, depResult.Blocked)
		}
		if len(depResult.Restarted) > 0 {
			log.Printf("UPDATE: Restarted dependents for %s: %v", container.Name, depResult.Restarted)
		}
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

// executeSelfUpdate handles the special case of docksmith updating itself.
// This follows a "prepare and restart" pattern:
// 1. Pull the new image
// 2. Update the compose file
// 3. Mark operation as pending_restart
// 4. Trigger docker compose up -d to restart with new image
// 5. On next startup, the operation is marked complete by resumePendingSelfUpdates()
func (o *UpdateOrchestrator) executeSelfUpdate(ctx context.Context, operationID string, container *docker.Container, targetVersion, stackName string) {
	log.Printf("SELF-UPDATE: Starting self-update for operation=%s container=%s target=%s", operationID, container.Name, targetVersion)

	o.publishProgress(operationID, container.Name, stackName, "validating", 0, "Preparing self-update")

	// Validate permissions (compose file access)
	if err := o.checkPermissions(ctx, container); err != nil {
		o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Permission check failed: %v", err))
		return
	}

	// Skip pre-update checks for self-updates since we can't reliably run scripts
	// during our own update process
	log.Printf("SELF-UPDATE: Skipping pre-update checks for self-update")

	currentVersion, _ := o.dockerClient.GetImageVersion(ctx, container.Image)

	// Set started_at timestamp and old version
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		if op.OldVersion == "" {
			op.OldVersion = currentVersion
		}
		op.StartedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	// Step 1: Update compose file with new version
	o.publishProgress(operationID, container.Name, stackName, "updating_compose", 20, "Updating compose file")

	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath == "" {
		o.failOperation(ctx, operationID, "updating_compose", "Cannot self-update: no compose file found for docksmith")
		return
	}

	resolvedPath, err := o.resolveComposeFile(composeFilePath)
	if err != nil {
		o.failOperation(ctx, operationID, "updating_compose", fmt.Sprintf("Failed to resolve compose file: %v", err))
		return
	}

	if err := o.updateComposeFile(ctx, resolvedPath, container, targetVersion); err != nil {
		o.failOperation(ctx, operationID, "updating_compose", fmt.Sprintf("Compose update failed: %v", err))
		return
	}

	// Step 2: Pull the new image
	imageRef := o.buildImageRef(container.Image, targetVersion)
	log.Printf("SELF-UPDATE: Pulling image %s", imageRef)
	o.publishProgress(operationID, container.Name, stackName, "pulling_image", 30, "Pulling new image")

	progressChan := make(chan PullProgress, 10)
	go func() {
		for progress := range progressChan {
			percent := 30 + (progress.Percent * 40 / 100) // 30-70%
			o.publishProgress(operationID, container.Name, stackName, "pulling_image", percent, progress.Status)
		}
	}()

	if err := o.pullImage(ctx, imageRef, progressChan); err != nil {
		close(progressChan)
		o.failOperation(ctx, operationID, "pulling_image", fmt.Sprintf("Image pull failed: %v", err))
		return
	}
	close(progressChan)

	// Step 3: Mark operation as pending_restart
	log.Printf("SELF-UPDATE: Image pulled, marking as pending_restart")
	o.publishProgress(operationID, container.Name, stackName, "pending_restart", 80, "Docksmith is restarting to apply update...")

	op, found, _ = o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.Status = "pending_restart"
		op.ErrorMessage = "Self-update prepared. Restarting docksmith..."
		if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("SELF-UPDATE: Warning - failed to save pending_restart status: %v", err)
		}
	}

	// Publish SSE event to notify UI that docksmith is restarting
	if o.eventBus != nil {
		o.eventBus.Publish(events.Event{
			Type: events.EventUpdateProgress,
			Payload: map[string]interface{}{
				"operation_id":   operationID,
				"container_name": container.Name,
				"stage":          "pending_restart",
				"percent":        90,
				"message":        "Docksmith is restarting to complete the update. This page will reconnect automatically.",
			},
		})
	}

	// Step 4: Trigger restart using docker compose
	// This is done in a goroutine with a small delay to allow the SSE event to be sent
	log.Printf("SELF-UPDATE: Triggering docker compose up -d to restart docksmith")
	go func() {
		// Give the SSE event time to be sent
		time.Sleep(1 * time.Second)

		// Get the host path for compose file (needed for docker compose command)
		hostComposePath := o.getComposeFilePathForHost(container)
		if hostComposePath == "" {
			hostComposePath = resolvedPath
		}

		// Run docker compose up -d to recreate with new image
		// This will kill our process, which is expected
		cmd := exec.Command("docker", "compose", "-f", hostComposePath, "up", "-d", "--force-recreate", container.Name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		log.Printf("SELF-UPDATE: Executing: docker compose -f %s up -d --force-recreate %s", hostComposePath, container.Name)
		if err := cmd.Run(); err != nil {
			// We likely won't reach here because the container will restart
			log.Printf("SELF-UPDATE: docker compose command returned: %v (this may be expected if container restarted)", err)
		}
	}()

	// Note: We don't mark as complete here - that happens on next startup via resumePendingSelfUpdates()
	log.Printf("SELF-UPDATE: Restart initiated, operation=%s will be completed on next startup", operationID)
}

// executeSelfRestart handles the special case of docksmith restarting itself.
// Since restarting kills the docksmith process, we mark the operation as pending_restart
// and complete it on the next startup.
func (o *UpdateOrchestrator) executeSelfRestart(ctx context.Context, operationID string, container *docker.Container, stackName string) {
	log.Printf("SELF-RESTART: Starting self-restart for operation=%s container=%s", operationID, container.Name)

	o.publishProgress(operationID, container.Name, stackName, "stopping", 20, "Preparing to restart docksmith...")

	// Get compose file path (needed for restart command)
	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath == "" {
		o.failOperation(ctx, operationID, "stopping", "Cannot self-restart: no compose file found for docksmith")
		return
	}

	resolvedPath, err := o.resolveComposeFile(composeFilePath)
	if err != nil {
		o.failOperation(ctx, operationID, "stopping", fmt.Sprintf("Failed to resolve compose file: %v", err))
		return
	}

	// Mark operation as pending_restart
	log.Printf("SELF-RESTART: Marking operation as pending_restart")
	o.publishProgress(operationID, container.Name, stackName, "pending_restart", 50, "Docksmith is restarting...")

	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.Status = "pending_restart"
		op.ErrorMessage = "Self-restart initiated. Docksmith is restarting..."
		if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
			log.Printf("SELF-RESTART: Warning - failed to save pending_restart status: %v", err)
		}
	}

	// Publish SSE event to notify UI that docksmith is restarting
	if o.eventBus != nil {
		o.eventBus.Publish(events.Event{
			Type: events.EventUpdateProgress,
			Payload: map[string]interface{}{
				"operation_id":   operationID,
				"container_name": container.Name,
				"stage":          "pending_restart",
				"percent":        90,
				"message":        "Docksmith is restarting. This page will reconnect automatically.",
			},
		})
	}

	// Trigger restart using docker compose
	// This is done in a goroutine with a small delay to allow the SSE event to be sent
	log.Printf("SELF-RESTART: Triggering docker compose restart")
	go func() {
		// Give the SSE event time to be sent
		time.Sleep(1 * time.Second)

		// Get the host path for compose file (needed for docker compose command)
		hostComposePath := o.getComposeFilePathForHost(container)
		if hostComposePath == "" {
			hostComposePath = resolvedPath
		}

		// Run docker compose restart to restart the container
		// This will kill our process, which is expected
		cmd := exec.Command("docker", "compose", "-f", hostComposePath, "restart", container.Name)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		log.Printf("SELF-RESTART: Executing: docker compose -f %s restart %s", hostComposePath, container.Name)
		if err := cmd.Run(); err != nil {
			// We likely won't reach here because the container will restart
			log.Printf("SELF-RESTART: docker compose command returned: %v (this may be expected if container restarted)", err)
		}
	}()

	// Note: We don't mark as complete here - that happens on next startup via resumePendingSelfUpdates()
	log.Printf("SELF-RESTART: Restart initiated, operation=%s will be completed on next startup", operationID)
}

// executeBatchUpdate executes batch update workflow.
func (o *UpdateOrchestrator) executeBatchUpdate(ctx context.Context, operationID string, containers []*docker.Container, targetVersions map[string]string, stackName string, forceContainers map[string]bool) {
	defer o.releaseStackLock(stackName)

	// Check if Docker SDK is initialized (required for container operations)
	if o.dockerSDK == nil {
		o.failOperation(ctx, operationID, "validating", "Docker SDK not initialized")
		return
	}

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

	// Check if docksmith (self) is in the batch - if so, separate it and update last
	var selfContainer *docker.Container
	var otherContainers []*docker.Container
	for _, container := range updateContainers {
		if selfupdate.IsSelfContainer(container.ID, container.Image, container.Name) {
			selfContainer = container
			log.Printf("BATCH UPDATE: Detected docksmith in batch, will update last")
		} else {
			otherContainers = append(otherContainers, container)
		}
	}

	// If only docksmith is in the batch, use self-update flow directly
	if selfContainer != nil && len(otherContainers) == 0 {
		log.Printf("BATCH UPDATE: Only docksmith in batch, using self-update flow")
		o.executeSelfUpdate(ctx, operationID, selfContainer, targetVersions[selfContainer.Name], stackName)
		return
	}

	// If docksmith is included with other containers, update others first
	// then trigger self-update which will restart docksmith
	if selfContainer != nil {
		updateContainers = otherContainers
		log.Printf("BATCH UPDATE: Will update %d containers first, then self-update docksmith", len(otherContainers))
	}

	// Set started_at timestamp before beginning actual update work
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.StartedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	// Phase 1: Update all compose files first (10-30%)
	o.publishProgress(operationID, "", stackName, "updating_compose", 10, fmt.Sprintf("Updating %d compose files", len(updateContainers)))

	for i, container := range updateContainers {
		progress := 10 + (i * 20 / len(updateContainers))
		o.publishProgress(operationID, container.Name, stackName, "updating_compose", progress, fmt.Sprintf("Updating compose for %s", container.Name))

		// Update compose file with new version
		targetVersion := targetVersions[container.Name]
		composeFilePath := o.getComposeFilePath(container)
		if composeFilePath != "" {
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

		baseProgress := 30 + (i * 30 / len(updateContainers))
		progressPerContainer := 30 / len(updateContainers)
		o.publishProgress(operationID, container.Name, stackName, "pulling_image", baseProgress, fmt.Sprintf("Pulling %s", newImageRef))

		progressChan := make(chan PullProgress, 10)
		pullDone := make(chan error, 1)

		go func() {
			pullDone <- o.pullImage(ctx, newImageRef, progressChan)
			close(progressChan)
		}()

		// Process pull progress events and publish them
		for progress := range progressChan {
			// Scale the pull progress (0-100) within the container's progress slice
			pullPercent := baseProgress + (progress.Percent * progressPerContainer / 100)
			o.publishProgress(operationID, container.Name, stackName, "pulling_image", pullPercent, progress.Status)
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

	// Build a map of batch container names for network_mode dependency checking
	batchNames := make(map[string]bool)
	for _, c := range orderedContainers {
		batchNames[c.Name] = true
		// Also add service name for matching network_mode: service:xxx
		if svc := c.Labels["com.docker.compose.service"]; svc != "" {
			batchNames[svc] = true
		}
	}

	// Identify containers with network_mode dependencies on other batch containers
	// These MUST use compose-based recreation to handle the network namespace correctly
	networkModeContainers := make(map[string]bool)
	for _, cont := range orderedContainers {
		hasNetworkDep, depName := o.hasNetworkModeDependency(cont, batchNames)
		if hasNetworkDep {
			networkModeContainers[cont.Name] = true
			log.Printf("BATCH UPDATE: Container %s has network_mode dependency on %s - will use compose recreation", cont.Name, depName)
		}
	}

	// Stop containers in reverse order (dependents first)
	// Only stop containers that will use SDK recreation - compose handles its own stop
	o.publishProgress(operationID, "", stackName, "recreating", 65, "Stopping containers")
	for i := len(orderedContainers) - 1; i >= 0; i-- {
		cont := orderedContainers[i]
		if networkModeContainers[cont.Name] {
			// Skip stopping - compose will handle this
			log.Printf("BATCH UPDATE: Skipping stop for %s (will use compose)", cont.Name)
			continue
		}
		log.Printf("BATCH UPDATE: Stopping %s", cont.Name)
		timeout := 10
		if err := o.dockerSDK.ContainerStop(ctx, cont.Name, dockerContainer.StopOptions{Timeout: &timeout}); err != nil {
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

		// Check if this container needs compose-based recreation (network_mode dependency)
		if networkModeContainers[cont.Name] {
			log.Printf("BATCH UPDATE: Using compose recreation for %s (network_mode dependency)", cont.Name)
			if err := o.recreateContainerWithCompose(ctx, cont); err != nil {
				log.Printf("BATCH UPDATE: Compose recreation failed for %s: %v", cont.Name, err)
				failCount++
				continue
			}
			log.Printf("BATCH UPDATE: Successfully recreated %s with compose", cont.Name)
			successCount++
			continue
		}

		// SDK-based recreation for containers without network_mode dependencies
		inspect, err := o.dockerSDK.ContainerInspect(ctx, cont.Name)
		if err != nil {
			log.Printf("BATCH UPDATE: Failed to inspect %s: %v", cont.Name, err)
			failCount++
			continue
		}

		// Save old image for recovery if creation fails
		oldImage := inspect.Config.Image

		// Remove container
		if err := o.dockerSDK.ContainerRemove(ctx, cont.Name, dockerContainer.RemoveOptions{}); err != nil {
			log.Printf("BATCH UPDATE: Failed to remove %s: %v", cont.Name, err)
			failCount++
			continue
		}

		// Create with new image
		newConfig := inspect.Config
		newConfig.Image = newImageRef

		// Clear hostname if using container network mode (hostname is inherited from the network source)
		// Without this, Docker returns "conflicting options: hostname and the network mode"
		if strings.HasPrefix(string(inspect.HostConfig.NetworkMode), "container:") {
			newConfig.Hostname = ""
			newConfig.Domainname = ""
		}

		networkingConfig := &network.NetworkingConfig{
			EndpointsConfig: inspect.NetworkSettings.Networks,
		}

		_, createErr := o.dockerSDK.ContainerCreate(ctx, newConfig, inspect.HostConfig, networkingConfig, nil, cont.Name)
		if createErr != nil {
			log.Printf("BATCH UPDATE: SDK creation failed for %s: %v, attempting fallback", cont.Name, createErr)

			// Fallback 1: Try compose-based recreation
			composeErr := o.recreateContainerWithCompose(ctx, cont)
			if composeErr == nil {
				log.Printf("BATCH UPDATE: Compose fallback succeeded for %s", cont.Name)
				successCount++
				continue
			}
			log.Printf("BATCH UPDATE: Compose fallback failed for %s: %v", cont.Name, composeErr)

			// Fallback 2: Try to restore with old image to prevent orphaning
			log.Printf("BATCH UPDATE: Attempting to restore %s with old image %s", cont.Name, oldImage)
			restoreConfig := inspect.Config
			restoreConfig.Image = oldImage
			if strings.HasPrefix(string(inspect.HostConfig.NetworkMode), "container:") {
				restoreConfig.Hostname = ""
				restoreConfig.Domainname = ""
			}

			_, restoreErr := o.dockerSDK.ContainerCreate(ctx, restoreConfig, inspect.HostConfig, networkingConfig, nil, cont.Name)
			if restoreErr != nil {
				log.Printf("BATCH UPDATE: CRITICAL - Failed to restore %s with old image: %v (container orphaned!)", cont.Name, restoreErr)
				failCount++
				continue
			}

			// Start the restored container
			if startErr := o.dockerSDK.ContainerStart(ctx, cont.Name, dockerContainer.StartOptions{}); startErr != nil {
				log.Printf("BATCH UPDATE: Failed to start restored container %s: %v", cont.Name, startErr)
			} else {
				log.Printf("BATCH UPDATE: Restored %s with old image (update failed but container recovered)", cont.Name)
			}
			failCount++
			continue
		}

		// Start container
		if err := o.dockerSDK.ContainerStart(ctx, cont.Name, dockerContainer.StartOptions{}); err != nil {
			log.Printf("BATCH UPDATE: Failed to start %s: %v", cont.Name, err)
			failCount++
			continue
		}

		log.Printf("BATCH UPDATE: Successfully recreated %s with %s", cont.Name, newImageRef)
		successCount++
	}

	// Phase 4: Health check (90-95%)
	o.publishProgress(operationID, "", stackName, "health_check", 90, "Verifying container health")

	// Wait for all containers to be healthy
	for _, cont := range orderedContainers {
		if err := o.waitForHealthy(ctx, cont.Name, o.healthCheckCfg.Timeout); err != nil {
			log.Printf("BATCH UPDATE: Health check warning for %s: %v", cont.Name, err)
		}
	}

	// Phase 5: Restart dependent containers (95-99%)
	// For batch updates, we need to restart containers that depend on any of the updated containers
	o.publishProgress(operationID, "", stackName, "restarting_dependents", 95, "Restarting dependent containers")

	// Track which dependents we've already restarted to avoid duplicates
	restartedDependents := make(map[string]bool)
	var allRestarted []string
	var allBlocked []string

	for _, cont := range orderedContainers {
		// Restart dependents for this container
		depResult, depErr := o.restartDependentContainers(ctx, cont.Name, false)
		if depErr != nil {
			log.Printf("BATCH UPDATE: Warning - failed to restart dependents for %s: %v", cont.Name, depErr)
			continue
		}

		if depResult != nil {
			// Track restarted dependents (avoiding duplicates)
			for _, dep := range depResult.Restarted {
				if !restartedDependents[dep] {
					restartedDependents[dep] = true
					allRestarted = append(allRestarted, dep)
				}
			}
			// Track blocked dependents
			for _, dep := range depResult.Blocked {
				if !restartedDependents[dep] {
					allBlocked = append(allBlocked, dep)
				}
			}
		}
	}

	if len(allRestarted) > 0 {
		log.Printf("BATCH UPDATE: Restarted dependents: %v", allRestarted)
		o.publishProgress(operationID, "", stackName, "restarting_dependents", 98, fmt.Sprintf("Restarted dependents: %s", strings.Join(allRestarted, ", ")))
	}
	if len(allBlocked) > 0 {
		log.Printf("BATCH UPDATE: Blocked dependents: %v", allBlocked)
		o.publishProgress(operationID, "", stackName, "restarting_dependents", 98, fmt.Sprintf("Blocked dependents: %s", strings.Join(allBlocked, ", ")))
	}

	// Check if we need to trigger self-update for docksmith (deferred to end of batch)
	if selfContainer != nil {
		log.Printf("BATCH UPDATE: All other containers done, now triggering docksmith self-update")

		// Update the operation status to indicate self-update is starting
		batchOp, batchFound, _ := o.storage.GetUpdateOperation(ctx, operationID)
		if batchFound {
			batchOp.Status = "in_progress"
			batchOp.ErrorMessage = fmt.Sprintf("Batch update: %d completed, now updating docksmith...", successCount)
			o.storage.SaveUpdateOperation(ctx, batchOp)
		}

		// Trigger self-update - this will restart docksmith
		// The operation will be marked complete on next startup
		o.executeSelfUpdate(ctx, operationID, selfContainer, targetVersions[selfContainer.Name], stackName)
		// We won't reach code below this as docksmith restarts
		return
	}

	status := "complete"
	message := fmt.Sprintf("Batch update completed: %d succeeded, %d failed", successCount, failCount)
	if failCount > 0 && successCount == 0 {
		status = "failed"
	}

	completedNow := time.Now()
	completedOp, completedFound, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if completedFound {
		completedOp.Status = status
		completedOp.CompletedAt = &completedNow
		completedOp.ErrorMessage = message
		o.storage.SaveUpdateOperation(ctx, completedOp)
	}

	o.publishProgress(operationID, "", stackName, status, 100, message)
}

// checkPermissions validates Docker access and file permissions.
// checkDockerAccess validates only Docker socket connectivity.
// Use this for operations that don't need compose file access (e.g., restart, stop).
func (o *UpdateOrchestrator) checkDockerAccess(ctx context.Context) error {
	if o.dockerSDK != nil {
		if _, err := o.dockerSDK.Ping(ctx); err != nil {
			return fmt.Errorf("docker socket access denied: %w", err)
		}
	}
	return nil
}

func (o *UpdateOrchestrator) checkPermissions(ctx context.Context, container *docker.Container) error {
	if err := o.checkDockerAccess(ctx); err != nil {
		return err
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
	} else if errors.Is(err, os.ErrPermission) {
		return "", fmt.Errorf("permission denied accessing %s", path)
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
		} else if errors.Is(err, os.ErrPermission) {
			return "", fmt.Errorf("permission denied accessing %s", alternatePath)
		}
	}

	return "", fmt.Errorf("file not found (tried %s)", path)
}

// updateComposeFile updates the image tag in the compose file.
// Handles include-based compose setups automatically.
func (o *UpdateOrchestrator) updateComposeFile(ctx context.Context, composeFilePath string, container *docker.Container, newTag string) error {
	// Prevent compose file corruption from empty tags
	if newTag == "" {
		return fmt.Errorf("cannot update compose file: target version is empty")
	}

	serviceName := container.Labels["com.docker.compose.service"]
	if serviceName == "" {
		return fmt.Errorf("container has no service label")
	}

	// Load compose file (handles include-based setups)
	composeFile, err := compose.LoadComposeFileOrIncluded(composeFilePath, serviceName)
	if err != nil {
		return fmt.Errorf("failed to load compose file: %w", err)
	}

	// Find the service
	service, err := composeFile.FindServiceByContainerName(serviceName)
	if err != nil {
		return fmt.Errorf("failed to find service %s: %w", serviceName, err)
	}

	// Update the image tag in the service node
	if service.Node.Kind != yaml.MappingNode {
		return fmt.Errorf("service node is not a mapping")
	}

	// Find and update the image field
	imageUpdated := false
	for i := 0; i < len(service.Node.Content); i += 2 {
		keyNode := service.Node.Content[i]
		valueNode := service.Node.Content[i+1]

		if keyNode.Value == "image" {
			currentImage := valueNode.Value

			// Handle env var image specs (e.g., ${OPENCLAW_IMAGE:-openclaw:latest})
			if compose.ContainsEnvVar(currentImage) {
				updated, ok := compose.ReplaceTagInEnvVar(currentImage, newTag)
				if !ok {
					log.Printf("UPDATE: Cannot update env var image for %s (no default value): %s", serviceName, currentImage)
					return nil
				}
				log.Printf("UPDATE: Updating env var image for %s: %s -> %s", serviceName, currentImage, updated)
				valueNode.Value = updated
				imageUpdated = true

				// Also update the .env file if the variable is defined there
				envVarName := compose.ExtractEnvVarName(currentImage)
				if envVarName != "" {
					composeDir := filepath.Dir(composeFile.Path)
					envVars := compose.LoadDotEnv(composeDir)
					if _, exists := envVars[envVarName]; exists {
						if err := compose.UpdateDotEnvVar(composeDir, envVarName, newTag); err != nil {
							log.Printf("UPDATE: Warning: failed to update .env file for %s: %v", envVarName, err)
						} else {
							log.Printf("UPDATE: Updated .env variable %s with new tag %s", envVarName, newTag)
						}
					}
				}
				break
			}

			// Parse current image and update tag
			parts := strings.Split(currentImage, ":")
			if len(parts) > 1 {
				parts[len(parts)-1] = newTag
			} else {
				parts = append(parts, newTag)
			}

			valueNode.Value = strings.Join(parts, ":")
			imageUpdated = true
			break
		}
	}

	if !imageUpdated {
		return fmt.Errorf("service %s has no image field", serviceName)
	}

	// Save the compose file (uses the actual file path from composeFile.Path)
	if err := composeFile.Save(); err != nil {
		return fmt.Errorf("failed to save compose file: %w", err)
	}

	return nil
}

// pullImage pulls a Docker image with retry logic.
func (o *UpdateOrchestrator) pullImage(ctx context.Context, imageRef string, progressChan chan<- PullProgress) error {
	if o.dockerSDK == nil {
		return fmt.Errorf("docker SDK not initialized")
	}

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

		err = func() error {
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
		}()
		if err != nil {
			if attempt < maxRetries-1 {
				continue
			}
			return err
		}

		return nil
	}

	return fmt.Errorf("failed to pull image after retries")
}

// restartContainerWithDependents recreates a container using docker compose.
// Note: This does NOT automatically restart dependent containers.
// Explicit restart dependencies should use the docksmith.restart-after label.
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

// hasNetworkModeDependency checks if a container has network_mode: service:* pointing to
// another container in the batch. If so, it returns true and the name of the dependency.
// Containers with network_mode dependencies must use compose-based recreation.
func (o *UpdateOrchestrator) hasNetworkModeDependency(cont *docker.Container, batchNames map[string]bool) (bool, string) {
	networkMode := cont.Labels[graph.NetworkModeLabel]
	if strings.HasPrefix(networkMode, "service:") {
		depName := strings.TrimPrefix(networkMode, "service:")
		if batchNames[depName] {
			return true, depName
		}
	}
	return false, ""
}

// recreateContainerWithCompose recreates a single container using docker compose.
// This is used for containers with network_mode dependencies during batch updates.
func (o *UpdateOrchestrator) recreateContainerWithCompose(ctx context.Context, cont *docker.Container) error {
	hostComposePath := o.getComposeFilePathForHost(cont)
	containerComposePath := o.getComposeFilePath(cont)

	if hostComposePath == "" || containerComposePath == "" {
		return fmt.Errorf("no compose file path available for container %s", cont.Name)
	}

	recreator := compose.NewRecreator(o.dockerClient)
	return recreator.RecreateWithCompose(ctx, cont, hostComposePath, containerComposePath)
}

// DependentRestartResult contains the results of restarting dependent containers
type DependentRestartResult struct {
	Restarted []string // Containers successfully restarted
	Blocked   []string // Containers blocked by pre-update check failure
	Errors    []string // Error messages for blocked containers
}

// DependentPreCheckResult contains the result of validating dependent containers' pre-update checks.
type DependentPreCheckResult struct {
	Dependents []string          // All dependent container names
	Failed     []string          // Dependents that failed pre-update checks
	Errors     map[string]string // Error messages keyed by container name
}

// validateDependentPreChecks finds containers that depend on the given container and runs
// their pre-update checks WITHOUT restarting anything. This allows failing early if a
// dependent's pre-update check would fail, before the main container is restarted.
func (o *UpdateOrchestrator) validateDependentPreChecks(ctx context.Context, containerName string) (*DependentPreCheckResult, error) {
	result := &DependentPreCheckResult{
		Dependents: make([]string, 0),
		Failed:     make([]string, 0),
		Errors:     make(map[string]string),
	}

	// Get all containers to find those that depend on the container being restarted
	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to list containers: %w", err)
	}

	// Find all containers that have containerName in their restart-after label
	var dependentContainers []*docker.Container
	for i := range containers {
		c := &containers[i]
		if restartAfter, ok := c.Labels[scripts.RestartAfterLabel]; ok && restartAfter != "" {
			dependencies := strings.Split(restartAfter, ",")
			for _, dep := range dependencies {
				dep = strings.TrimSpace(dep)
				if dep == containerName {
					result.Dependents = append(result.Dependents, c.Name)
					dependentContainers = append(dependentContainers, c)
					break
				}
			}
		}
	}

	if len(dependentContainers) == 0 {
		log.Printf("RESTART: No containers depend on %s", containerName)
		return result, nil
	}

	log.Printf("RESTART: Found %d dependent container(s) for %s: %v", len(dependentContainers), containerName, result.Dependents)

	// Run pre-update checks for each dependent (without restarting)
	for _, depContainer := range dependentContainers {
		if scriptPath, ok := depContainer.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
			log.Printf("RESTART: Pre-validating pre-update check for dependent %s", depContainer.Name)

			if err := runPreUpdateCheck(ctx, depContainer, scriptPath); err != nil {
				log.Printf("RESTART: Pre-update check FAILED for dependent %s: %v", depContainer.Name, err)
				result.Failed = append(result.Failed, depContainer.Name)
				result.Errors[depContainer.Name] = err.Error()
			} else {
				log.Printf("RESTART: Pre-update check passed for dependent %s", depContainer.Name)
			}
		}
	}

	return result, nil
}

// restartDependentContainers finds and restarts containers that have the given container
// listed in their docksmith.restart-after label.
// If skipPreChecks is true, pre-update checks are skipped (used for rollback operations).
// Returns a DependentRestartResult with information about which dependents were restarted or blocked.
func (o *UpdateOrchestrator) restartDependentContainers(ctx context.Context, containerName string, skipPreChecks bool, visited ...map[string]bool) (*DependentRestartResult, error) {
	// Cycle guard: track visited containers to prevent infinite recursion from circular restart-after labels
	var visitedSet map[string]bool
	if len(visited) > 0 && visited[0] != nil {
		visitedSet = visited[0]
	} else {
		visitedSet = make(map[string]bool)
	}
	if visitedSet[containerName] {
		log.Printf("UPDATE: Skipping %s - already visited in this restart chain (cycle detected)", containerName)
		return &DependentRestartResult{
			Restarted: make([]string, 0),
			Blocked:   make([]string, 0),
			Errors:    make([]string, 0),
		}, nil
	}
	visitedSet[containerName] = true
	result := &DependentRestartResult{
		Restarted: make([]string, 0),
		Blocked:   make([]string, 0),
		Errors:    make([]string, 0),
	}

	// Get all containers to find those that depend on the restarted container
	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to list containers: %w", err)
	}

	// Find all containers that have containerName in their restart-after label
	var dependents []string
	for _, c := range containers {
		if restartAfter, ok := c.Labels[scripts.RestartAfterLabel]; ok && restartAfter != "" {
			// Parse comma-separated list of dependencies
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

	if len(dependents) == 0 {
		log.Printf("UPDATE: No containers depend on %s", containerName)
		return result, nil
	}

	log.Printf("UPDATE: Found %d dependent container(s) for %s: %v", len(dependents), containerName, dependents)

	// Create container map for lookups
	containerMap := docker.CreateContainerMap(containers)

	// Restart each dependent container
	for _, depName := range dependents {
		depContainer := containerMap[depName]
		if depContainer == nil {
			log.Printf("UPDATE: Dependent container %s not found, skipping", depName)
			continue
		}

		// Run pre-update check if configured (unless skipped for rollback)
		if !skipPreChecks {
			if scriptPath, ok := depContainer.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
				log.Printf("UPDATE: Running pre-update check for dependent %s", depName)

				// NOTE: Do NOT translate the script path - the orchestrator runs inside the container
				// where the script path (e.g., /scripts/...) is already valid
				if err := runPreUpdateCheck(ctx, depContainer, scriptPath); err != nil {
					log.Printf("UPDATE: Pre-update check failed for dependent %s: %v (skipping restart)", depName, err)
					result.Blocked = append(result.Blocked, depName)
					result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", depName, err))
					continue
				}
				log.Printf("UPDATE: Pre-update check passed for dependent %s", depName)
			}
		} else {
			log.Printf("UPDATE: Skipping pre-update check for dependent %s (rollback operation)", depName)
		}

		// Restart the dependent container
		// Use compose-based recreation if available - this is required for containers with
		// network_mode: service:X because docker restart fails when the parent container
		// was recreated (the old network namespace no longer exists)
		log.Printf("UPDATE: Restarting dependent container: %s", depName)

		var restartErr error
		composeFilePath := o.getComposeFilePath(depContainer)
		if composeFilePath != "" {
			hostComposeFilePath := o.getComposeFilePathForHost(depContainer)
			recreator := compose.NewRecreator(o.dockerClient)

			log.Printf("UPDATE: Using compose-based recreation for dependent %s", depName)
			restartErr = recreator.RecreateWithCompose(ctx, depContainer, hostComposeFilePath, composeFilePath)
		} else {
			// Fallback to docker restart for non-compose containers
			log.Printf("UPDATE: Using docker restart for dependent %s (no compose file)", depName)
			restartErr = o.dockerSDK.ContainerRestart(ctx, depName, dockerContainer.StopOptions{})
		}

		if restartErr != nil {
			log.Printf("UPDATE: Failed to restart dependent %s: %v", depName, restartErr)
			result.Blocked = append(result.Blocked, depName)
			result.Errors = append(result.Errors, fmt.Sprintf("%s: restart failed: %v", depName, restartErr))
			continue
		}

		// Wait for dependent to be healthy/running
		if healthErr := o.waitForHealthy(ctx, depName, o.healthCheckCfg.Timeout); healthErr != nil {
			log.Printf("UPDATE: Health check warning for dependent %s: %v", depName, healthErr)
			// Don't fail - container was restarted
		}

		log.Printf("UPDATE: Successfully restarted dependent container: %s", depName)
		result.Restarted = append(result.Restarted, depName)

		// Recursively restart this container's dependents (cascade the restart chain)
		cascadeResult, cascadeErr := o.restartDependentContainers(ctx, depName, skipPreChecks, visitedSet)
		if cascadeErr != nil {
			log.Printf("UPDATE: Warning - failed to restart cascaded dependents for %s: %v", depName, cascadeErr)
			// Don't fail the parent operation if cascaded restarts fail
		}
		// Merge cascade results
		if cascadeResult != nil {
			result.Restarted = append(result.Restarted, cascadeResult.Restarted...)
			result.Blocked = append(result.Blocked, cascadeResult.Blocked...)
			result.Errors = append(result.Errors, cascadeResult.Errors...)
		}
	}

	return result, nil
}

// waitForHealthy waits for a container to become healthy or confirms it's running.
// For containers with health checks, polls until status is "healthy" or times out.
// For containers without health checks, verifies the container is running (fast path).
func (o *UpdateOrchestrator) waitForHealthy(ctx context.Context, containerName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	inspect, err := o.dockerSDK.ContainerInspect(ctx, containerName)
	if err != nil {
		return fmt.Errorf("failed to inspect container: %w", err)
	}

	hasHealthCheck := inspect.State != nil && inspect.State.Health != nil

	if hasHealthCheck {
		// Check immediately first - container might already be healthy
		if inspect.State.Health.Status == "healthy" {
			return nil
		}
		if inspect.State.Health.Status == "unhealthy" {
			return fmt.Errorf("container is unhealthy")
		}

		// Poll until healthy or timeout
		ticker := time.NewTicker(1 * time.Second)
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
		// Fast path: container without health check - just verify it's running
		// Check immediately - container is usually already running after compose up
		if inspect.State.Running {
			return nil
		}

		// Brief poll if not running yet (e.g., during restart)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		// Use FallbackWait as the timeout for containers without health checks
		fallbackCtx, fallbackCancel := context.WithTimeout(ctx, o.healthCheckCfg.FallbackWait)
		defer fallbackCancel()

		for {
			select {
			case <-fallbackCtx.Done():
				return fmt.Errorf("container did not start within timeout")
			case <-ticker.C:
				inspect, err := o.dockerSDK.ContainerInspect(ctx, containerName)
				if err != nil {
					return fmt.Errorf("failed to inspect container: %w", err)
				}

				if inspect.State.Running {
					return nil
				}
			}
		}
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

// resolveRollbackVersion determines the rollback strategy and target version for a container.
// Returns the version/digest to roll back to and the strategy used.
// Priority: tag > resolved > digest > none
func resolveRollbackVersion(detail storage.BatchContainerDetail) (version string, strategy string) {
	// Priority 1: Tag differs — standard tag-based rollback
	if detail.OldVersion != detail.NewVersion {
		return detail.OldVersion, "tag"
	}
	// Priority 2: Resolved version differs — pin to old resolved version
	if detail.OldResolvedVersion != "" && detail.OldResolvedVersion != detail.NewResolvedVersion {
		return detail.OldResolvedVersion, "resolved"
	}
	// Priority 3: Has old digest — digest-based rollback
	if detail.OldDigest != "" {
		return detail.OldDigest, "digest"
	}
	// Not rollbackable
	return "", "none"
}

// RollbackOperation performs a rollback of a previous update operation.
// Creates a new rollback operation, updates the compose file, and recreates the container.
func (o *UpdateOrchestrator) RollbackOperation(ctx context.Context, originalOperationID string, force bool) (string, error) {
	// Get original operation
	origOp, found, err := o.storage.GetUpdateOperation(ctx, originalOperationID)
	if err != nil {
		return "", fmt.Errorf("failed to get operation: %w", err)
	}
	if !found {
		return "", fmt.Errorf("operation not found: %s", originalOperationID)
	}

	// Check if this is a batch operation
	if len(origOp.BatchDetails) > 0 {
		log.Printf("ROLLBACK: Rolling back batch operation %s with %d containers", originalOperationID, len(origOp.BatchDetails))

		// Use resolveRollbackVersion to determine strategy per container
		targetVersions := make(map[string]string)
		containerNames := make([]string, 0, len(origOp.BatchDetails))
		digestRollbacks := make(map[string]storage.BatchContainerDetail) // containers needing digest-based rollback
		var skippedContainers []string

		for _, detail := range origOp.BatchDetails {
			version, strategy := resolveRollbackVersion(detail)
			log.Printf("ROLLBACK: %s strategy=%s version=%s (old=%s new=%s)", detail.ContainerName, strategy, version, detail.OldVersion, detail.NewVersion)

			switch strategy {
			case "tag", "resolved":
				targetVersions[detail.ContainerName] = version
				containerNames = append(containerNames, detail.ContainerName)
			case "digest":
				digestRollbacks[detail.ContainerName] = detail
			case "none":
				log.Printf("ROLLBACK: Skipping %s — identical tags with no saved digest", detail.ContainerName)
				skippedContainers = append(skippedContainers, detail.ContainerName)
			}
		}

		if len(containerNames) == 0 && len(digestRollbacks) == 0 {
			return "", fmt.Errorf("no containers can be rolled back — all use identical tags with no saved digest (skipped: %s)", strings.Join(skippedContainers, ", "))
		}

		// Handle tag/resolved rollbacks via batch pipeline
		var rollbackOpID string
		if len(containerNames) > 0 {
			rollbackOpID, err = o.updateBatchContainersInternal(ctx, containerNames, targetVersions, "rollback", "", nil, nil)
			if err != nil {
				return "", err
			}
		}

		// Handle digest rollbacks via single-container rollback path
		if len(digestRollbacks) > 0 {
			containers, listErr := o.dockerClient.ListContainers(ctx)
			if listErr != nil {
				return "", fmt.Errorf("failed to list containers for digest rollback: %w", listErr)
			}
			containerMap := make(map[string]*docker.Container)
			for i := range containers {
				containerMap[containers[i].Name] = &containers[i]
			}

			for name, detail := range digestRollbacks {
				targetContainer := containerMap[name]
				if targetContainer == nil {
					log.Printf("ROLLBACK: Container %s not found for digest rollback, skipping", name)
					continue
				}

				digestOpID := uuid.New().String()
				if rollbackOpID == "" {
					rollbackOpID = digestOpID // Use first digest rollback ID if no batch rollback
				}

				digestOp := storage.UpdateOperation{
					OperationID:   digestOpID,
					ContainerID:   targetContainer.ID,
					ContainerName: name,
					StackName:     detail.StackName,
					OperationType: "rollback",
					Status:        "in_progress",
					OldVersion:    detail.NewVersion,
					NewVersion:    detail.OldVersion + " (digest)",
					CreatedAt:     time.Now(),
					UpdatedAt:     time.Now(),
					StartedAt:     func() *time.Time { t := time.Now(); return &t }(),
				}
				if saveErr := o.storage.SaveUpdateOperation(ctx, digestOp); saveErr != nil {
					log.Printf("ROLLBACK: Failed to save digest rollback operation for %s: %v", name, saveErr)
					continue
				}

				go o.executeDigestRollback(context.Background(), digestOpID, targetContainer, detail)
			}
		}

		// Mark original operation as rolled back
		origOp.RollbackOccurred = true
		if saveErr := o.storage.SaveUpdateOperation(ctx, origOp); saveErr != nil {
			log.Printf("ROLLBACK: Failed to mark original operation as rolled back: %v", saveErr)
		}

		return rollbackOpID, nil
	}

	// Single container rollback (existing logic)
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

	// Find dependent containers and check their pre-update scripts BEFORE starting rollback
	if !force {
		dependents := o.findDependentContainerNames(containers, origOp.ContainerName)
		if len(dependents) > 0 {
			containerMap := docker.CreateContainerMap(containers)
			var failedChecks []string

			for _, depName := range dependents {
				depContainer := containerMap[depName]
				if depContainer == nil {
					continue
				}

				if scriptPath, ok := depContainer.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
					log.Printf("ROLLBACK: Running pre-update check for dependent %s", depName)
					if err := runPreUpdateCheck(ctx, depContainer, scriptPath); err != nil {
						log.Printf("ROLLBACK: Pre-update check failed for dependent %s: %v", depName, err)
						failedChecks = append(failedChecks, depName)
					}
				}
			}

			if len(failedChecks) > 0 {
				return "", fmt.Errorf("pre-update check failed for dependent container(s): %s (use force to skip)", strings.Join(failedChecks, ", "))
			}
		}
	}

	log.Printf("ROLLBACK: Rolling back %s from %s to %s (force=%v)", origOp.ContainerName, origOp.NewVersion, targetVersion, force)

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
	// Pass force flag so dependent restarts know whether to skip pre-checks
	go o.executeRollback(context.Background(), rollbackOpID, originalOperationID, targetContainer, targetVersion, force)

	return rollbackOpID, nil
}

// findDependentContainerNames finds all container names that depend on the given container
func (o *UpdateOrchestrator) findDependentContainerNames(containers []docker.Container, containerName string) []string {
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
	return dependents
}

// RollbackContainers rolls back specific containers from an operation.
// It filters the original operation's batch_details to only the requested container names,
// then creates a new rollback operation for those containers.
func (o *UpdateOrchestrator) RollbackContainers(ctx context.Context, operationID string, containerNames []string, force bool) (string, error) {
	if o.storage == nil {
		return "", fmt.Errorf("storage not available")
	}

	origOp, found, err := o.storage.GetUpdateOperation(ctx, operationID)
	if err != nil {
		return "", fmt.Errorf("failed to get operation: %w", err)
	}
	if !found {
		return "", fmt.Errorf("operation %s not found", operationID)
	}

	// Build target versions from batch_details using smart resolution
	targetVersions := make(map[string]string)
	requestedNames := make(map[string]bool)
	for _, name := range containerNames {
		requestedNames[name] = true
	}

	digestRollbacks := make(map[string]storage.BatchContainerDetail)
	var skippedContainers []string
	rollbackNames := make([]string, 0, len(containerNames))

	if len(origOp.BatchDetails) > 0 {
		for _, detail := range origOp.BatchDetails {
			if !requestedNames[detail.ContainerName] {
				continue
			}
			version, strategy := resolveRollbackVersion(detail)
			log.Printf("ROLLBACK-CONTAINERS: %s strategy=%s version=%s", detail.ContainerName, strategy, version)

			switch strategy {
			case "tag", "resolved":
				targetVersions[detail.ContainerName] = version
				rollbackNames = append(rollbackNames, detail.ContainerName)
			case "digest":
				digestRollbacks[detail.ContainerName] = detail
			case "none":
				skippedContainers = append(skippedContainers, detail.ContainerName)
			}
		}
	} else {
		// Single operation — validate container name matches
		if !requestedNames[origOp.ContainerName] {
			return "", fmt.Errorf("container %s not found in operation %s", containerNames[0], operationID)
		}
		targetVersions[origOp.ContainerName] = origOp.OldVersion
		rollbackNames = append(rollbackNames, origOp.ContainerName)
	}

	if len(rollbackNames) == 0 && len(digestRollbacks) == 0 {
		if len(skippedContainers) > 0 {
			return "", fmt.Errorf("no containers can be rolled back — all use identical tags with no saved digest (skipped: %s)", strings.Join(skippedContainers, ", "))
		}
		return "", fmt.Errorf("no matching containers found in operation %s for rollback", operationID)
	}

	// Handle tag/resolved rollbacks via batch pipeline
	var rollbackOpID string
	if len(rollbackNames) > 0 {
		rollbackOpID, err = o.updateBatchContainersInternal(ctx, rollbackNames, targetVersions, "rollback", "", nil, nil)
		if err != nil {
			return "", err
		}
	}

	// Handle digest rollbacks via single-container path
	if len(digestRollbacks) > 0 {
		containers, listErr := o.dockerClient.ListContainers(ctx)
		if listErr != nil {
			return "", fmt.Errorf("failed to list containers for digest rollback: %w", listErr)
		}
		containerMap := make(map[string]*docker.Container)
		for i := range containers {
			containerMap[containers[i].Name] = &containers[i]
		}

		for name, detail := range digestRollbacks {
			targetContainer := containerMap[name]
			if targetContainer == nil {
				log.Printf("ROLLBACK-CONTAINERS: Container %s not found for digest rollback, skipping", name)
				continue
			}

			digestOpID := uuid.New().String()
			if rollbackOpID == "" {
				rollbackOpID = digestOpID
			}

			digestOp := storage.UpdateOperation{
				OperationID:   digestOpID,
				ContainerID:   targetContainer.ID,
				ContainerName: name,
				StackName:     detail.StackName,
				OperationType: "rollback",
				Status:        "in_progress",
				OldVersion:    detail.NewVersion,
				NewVersion:    detail.OldVersion + " (digest)",
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
				StartedAt:     func() *time.Time { t := time.Now(); return &t }(),
			}
			if saveErr := o.storage.SaveUpdateOperation(ctx, digestOp); saveErr != nil {
				log.Printf("ROLLBACK-CONTAINERS: Failed to save digest rollback operation for %s: %v", name, saveErr)
				continue
			}

			go o.executeDigestRollback(context.Background(), digestOpID, targetContainer, detail)
		}
	}

	// Mark original operation as rolled back only after rollback successfully started
	origOp.RollbackOccurred = true
	if err := o.storage.SaveUpdateOperation(ctx, origOp); err != nil {
		log.Printf("ROLLBACK: Failed to mark original operation as rolled back: %v", err)
	}

	return rollbackOpID, nil
}

// executeRollback performs the actual rollback process using the old version from database
func (o *UpdateOrchestrator) executeRollback(ctx context.Context, rollbackOpID, originalOpID string, container *docker.Container, oldVersion string, force bool) {
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

	// Restart dependent containers (those with docksmith.restart-after label)
	// For rollback, skip pre-update checks - rollback is a recovery operation
	depResult, depErr := o.restartDependentContainers(ctx, container.Name, true)
	if depErr != nil {
		log.Printf("ROLLBACK: Warning - failed to restart dependent containers for %s: %v", container.Name, depErr)
		// Don't fail the rollback if dependent restarts fail
	} else if depResult != nil && len(depResult.Restarted) > 0 {
		log.Printf("ROLLBACK: Restarted dependents for %s: %v", container.Name, depResult.Restarted)
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

// executeDigestRollback performs a digest-based rollback for a container.
// This is used when the tag hasn't changed (e.g., :latest, REBUILD) but we have the old digest.
// It pulls the old image by digest, re-tags it as the current tag, then recreates the container.
func (o *UpdateOrchestrator) executeDigestRollback(ctx context.Context, rollbackOpID string, container *docker.Container, detail storage.BatchContainerDetail) {
	stackName := container.Labels["com.docker.compose.project"]

	// Extract repo from image (e.g., "ghcr.io/user/repo" from "ghcr.io/user/repo:latest")
	imageParts := strings.Split(container.Image, ":")
	repo := imageParts[0]
	currentTag := "latest"
	if len(imageParts) > 1 {
		currentTag = imageParts[len(imageParts)-1]
	}

	// Stage 1: Pull old image by digest (10-50%)
	digestRef := repo + "@" + detail.OldDigest
	digestShort := detail.OldDigest
	if len(digestShort) > 19 {
		digestShort = digestShort[:19]
	}
	o.publishProgress(rollbackOpID, container.Name, stackName, "pulling_image", 10, fmt.Sprintf("Pulling old image by digest: %s", digestShort))
	log.Printf("DIGEST ROLLBACK: Pulling %s for container %s", digestRef, container.Name)

	progressChan := make(chan PullProgress, 10)
	pullDone := make(chan error, 1)

	go func() {
		pullDone <- o.pullImage(ctx, digestRef, progressChan)
		close(progressChan)
	}()

	for progress := range progressChan {
		percent := 10
		if progress.Percent > 0 {
			percent = 10 + int(float64(progress.Percent)*0.4) // 10-50%
		}
		o.publishProgress(rollbackOpID, container.Name, stackName, "pulling_image", percent, progress.Status)
	}

	if err := <-pullDone; err != nil {
		o.failOperation(ctx, rollbackOpID, "pulling_image", fmt.Sprintf("Old image digest no longer available in registry: %v", err))
		return
	}

	// Stage 2: Re-tag the digest image as the current tag (50-60%)
	o.publishProgress(rollbackOpID, container.Name, stackName, "pulling_image", 55, fmt.Sprintf("Re-tagging as %s:%s", repo, currentTag))
	log.Printf("DIGEST ROLLBACK: Re-tagging %s as %s:%s", digestRef, repo, currentTag)

	if err := o.dockerSDK.ImageTag(ctx, digestRef, repo+":"+currentTag); err != nil {
		o.failOperation(ctx, rollbackOpID, "pulling_image", fmt.Sprintf("Failed to re-tag image: %v", err))
		return
	}

	// Stage 3: Recreate container (60-80%) — compose sees the local image with the right tag
	o.publishProgress(rollbackOpID, container.Name, stackName, "recreating", 60, "Recreating container with old image")

	if _, err := o.restartContainerWithDependents(ctx, rollbackOpID, container.Name, stackName, container.Image); err != nil {
		o.failOperation(ctx, rollbackOpID, "recreating", fmt.Sprintf("Failed to recreate container: %v", err))
		return
	}

	// Stage 4: Health check (80-95%)
	o.publishProgress(rollbackOpID, container.Name, stackName, "health_check", 80, "Verifying container health")

	if err := o.waitForHealthy(ctx, container.Name, o.healthCheckCfg.Timeout); err != nil {
		log.Printf("DIGEST ROLLBACK: Warning - health check failed: %v", err)
		o.publishProgress(rollbackOpID, container.Name, stackName, "health_check", 90, fmt.Sprintf("Health check warning: %v", err))
	} else {
		o.publishProgress(rollbackOpID, container.Name, stackName, "health_check", 95, "Health check passed")
	}

	// Restart dependent containers
	depResult, depErr := o.restartDependentContainers(ctx, container.Name, true)
	if depErr != nil {
		log.Printf("DIGEST ROLLBACK: Warning - failed to restart dependent containers for %s: %v", container.Name, depErr)
	} else if depResult != nil && len(depResult.Restarted) > 0 {
		log.Printf("DIGEST ROLLBACK: Restarted dependents for %s: %v", container.Name, depResult.Restarted)
	}

	// Stage 5: Complete (100%)
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, rollbackOpID)
	if found {
		op.Status = "complete"
		op.CompletedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	o.publishProgress(rollbackOpID, container.Name, stackName, "complete", 100, "Digest-based rollback completed successfully")

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

	log.Printf("DIGEST ROLLBACK: Successfully completed digest-based rollback for container %s", container.Name)
}

// FixComposeMismatch fixes a container where the running image doesn't match the compose file.
// This pulls the image specified in the compose file and recreates the container.
func (o *UpdateOrchestrator) FixComposeMismatch(ctx context.Context, containerName string) (string, error) {
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

	// Get compose file path to read the expected image
	composeFilePath := o.getComposeFilePath(targetContainer)
	if composeFilePath == "" {
		return "", fmt.Errorf("no compose file found for container %s", containerName)
	}

	resolvedPath, err := o.resolveComposeFile(composeFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve compose file: %w", err)
	}

	stackName := o.stackManager.DetermineStack(ctx, *targetContainer)

	if !o.acquireStackLock(stackName) {
		if err := o.queueOperation(ctx, operationID, stackName, []string{containerName}, "fix_mismatch"); err != nil {
			return "", fmt.Errorf("failed to queue operation: %w", err)
		}
		return operationID, nil
	}

	// Get service name from labels
	serviceName := targetContainer.Labels["com.docker.compose.service"]
	if serviceName == "" {
		o.releaseStackLock(stackName)
		return "", fmt.Errorf("container %s has no compose service label", containerName)
	}

	// Read the compose file to get the expected image tag
	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		o.releaseStackLock(stackName)
		return "", fmt.Errorf("failed to read compose file: %w", err)
	}

	expectedImage, err := extractImageFromCompose(content, serviceName)
	if err != nil {
		o.releaseStackLock(stackName)
		return "", fmt.Errorf("failed to extract image from compose file: %w", err)
	}

	log.Printf("FIX_MISMATCH: Container %s running %s, compose expects %s", containerName, targetContainer.Image, expectedImage)

	// Extract current and target versions for operation tracking
	currentVersion := ""
	if parts := strings.Split(targetContainer.Image, ":"); len(parts) >= 2 {
		currentVersion = parts[len(parts)-1]
	}
	targetVersion := ""
	if parts := strings.Split(expectedImage, ":"); len(parts) >= 2 {
		targetVersion = parts[len(parts)-1]
	}

	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerID:   targetContainer.ID,
		ContainerName: containerName,
		StackName:     stackName,
		OperationType: "fix_mismatch",
		Status:        "validating",
		OldVersion:    currentVersion,
		NewVersion:    targetVersion,
	}

	if o.storage != nil {
		if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
			o.releaseStackLock(stackName)
			return "", fmt.Errorf("failed to save operation: %w", err)
		}
	}

	go o.executeFixMismatch(context.Background(), operationID, targetContainer, expectedImage, stackName)

	return operationID, nil
}

// executeFixMismatch pulls the image specified in compose file and recreates the container
func (o *UpdateOrchestrator) executeFixMismatch(ctx context.Context, operationID string, container *docker.Container, expectedImage, stackName string) {
	defer o.releaseStackLock(stackName)

	log.Printf("FIX_MISMATCH: Starting fix for operation=%s container=%s expected=%s", operationID, container.Name, expectedImage)

	o.publishProgress(operationID, container.Name, stackName, "validating", 0, "Validating permissions")

	if err := o.checkPermissions(ctx, container); err != nil {
		o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Permission check failed: %v", err))
		return
	}

	// Set started_at timestamp
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.StartedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	// Pull the expected image
	log.Printf("FIX_MISMATCH: Pulling image %s", expectedImage)
	o.publishProgress(operationID, container.Name, stackName, "pulling_image", 20, fmt.Sprintf("Pulling image: %s", expectedImage))

	progressChan := make(chan PullProgress, 10)
	go func() {
		for progress := range progressChan {
			percent := 20 + (progress.Percent * 40 / 100)
			o.publishProgress(operationID, container.Name, stackName, "pulling_image", percent, progress.Status)
		}
	}()

	if err := o.pullImage(ctx, expectedImage, progressChan); err != nil {
		close(progressChan)
		o.failOperation(ctx, operationID, "pulling_image", fmt.Sprintf("Image pull failed: %v", err))
		return
	}
	close(progressChan)

	// Recreate the container using docker compose up -d
	log.Printf("FIX_MISMATCH: Recreating container %s", container.Name)
	o.publishProgress(operationID, container.Name, stackName, "recreating", 60, "Recreating container")

	if _, err := o.restartContainerWithDependents(ctx, operationID, container.Name, stackName, expectedImage); err != nil {
		o.failOperation(ctx, operationID, "recreating", fmt.Sprintf("Recreation failed: %v", err))
		return
	}

	// Health check
	o.publishProgress(operationID, container.Name, stackName, "health_check", 80, "Verifying health")

	if err := o.waitForHealthy(ctx, container.Name, o.healthCheckCfg.Timeout); err != nil {
		o.failOperation(ctx, operationID, "health_check", fmt.Sprintf("Health check failed: %v", err))
		return
	}

	log.Printf("FIX_MISMATCH: Health check passed for operation=%s, marking complete", operationID)
	o.publishProgress(operationID, container.Name, stackName, "complete", 100, "Fix completed successfully")

	completedNow := time.Now()
	completedOp, completedFound, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if completedFound {
		completedOp.Status = "complete"
		completedOp.CompletedAt = &completedNow
		o.storage.SaveUpdateOperation(ctx, completedOp)
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

// extractImageFromCompose extracts the image for a service from compose file content
func extractImageFromCompose(content []byte, serviceName string) (string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		return "", fmt.Errorf("failed to parse compose file: %w", err)
	}

	if root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return "", fmt.Errorf("invalid compose file structure")
	}

	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return "", fmt.Errorf("compose file root is not a mapping")
	}

	// Find services section
	for i := 0; i < len(doc.Content)-1; i += 2 {
		if doc.Content[i].Value == "services" {
			servicesNode := doc.Content[i+1]
			if servicesNode.Kind != yaml.MappingNode {
				return "", fmt.Errorf("services section is not a mapping")
			}

			// Find the service
			for j := 0; j < len(servicesNode.Content)-1; j += 2 {
				if servicesNode.Content[j].Value == serviceName {
					serviceNode := servicesNode.Content[j+1]
					if serviceNode.Kind != yaml.MappingNode {
						return "", fmt.Errorf("service %s is not a mapping", serviceName)
					}

					// Find the image key
					for k := 0; k < len(serviceNode.Content)-1; k += 2 {
						if serviceNode.Content[k].Value == "image" {
							return serviceNode.Content[k+1].Value, nil
						}
					}
					return "", fmt.Errorf("no image key found for service %s", serviceName)
				}
			}
			return "", fmt.Errorf("service %s not found in compose file", serviceName)
		}
	}

	return "", fmt.Errorf("no services section found in compose file")
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
	entry, exists := o.stackLocks[stackName]
	if !exists {
		entry = &stackLockEntry{}
		o.stackLocks[stackName] = entry
	}
	o.locksMu.Unlock()

	locked := entry.mu.TryLock()
	if locked {
		o.locksMu.Lock()
		entry.lastUsed = time.Now()
		o.locksMu.Unlock()
	}
	return locked
}

// releaseStackLock releases a stack lock.
func (o *UpdateOrchestrator) releaseStackLock(stackName string) {
	o.locksMu.Lock()
	entry, exists := o.stackLocks[stackName]
	o.locksMu.Unlock()

	if exists {
		entry.mu.Unlock()
	}
}

// cleanupStaleLocks periodically removes stack locks that haven't been used recently.
// This prevents unbounded memory growth from accumulating locks for stacks that no longer exist.
func (o *UpdateOrchestrator) cleanupStaleLocks(ctx context.Context) {
	const (
		cleanupInterval = 10 * time.Minute
		staleThreshold  = 30 * time.Minute
	)

	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.locksMu.Lock()
			now := time.Now()
			for stackName, entry := range o.stackLocks {
				// Only remove if lock is not held and hasn't been used recently
				if entry.mu.TryLock() {
					if now.Sub(entry.lastUsed) > staleThreshold {
						delete(o.stackLocks, stackName)
						log.Printf("CLEANUP: Removed stale stack lock for %s", stackName)
					}
					entry.mu.Unlock()
				}
			}
			o.locksMu.Unlock()
		}
	}
}

// queueOperation adds an operation to the queue.
func (o *UpdateOrchestrator) queueOperation(ctx context.Context, operationID, stackName string, containers []string, operationType string) error {
	queue := storage.UpdateQueue{
		OperationID:   operationID,
		StackName:     stackName,
		Containers:    containers,
		OperationType: operationType,
		QueuedAt:      time.Now(),
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
	// Recover from panics to prevent queue processor from dying silently
	defer func() {
		if r := recover(); r != nil {
			log.Printf("QUEUE: PANIC recovered in queue processor: %v", r)
			// Restart the queue processor after a brief delay
			time.Sleep(5 * time.Second)
			go o.processQueue(ctx)
		}
	}()

	// Skip queue processing if storage is unavailable
	if o.storage == nil {
		log.Printf("QUEUE: Storage unavailable, queue processing disabled")
		return
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("QUEUE: Queue processor stopping")
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

						if len(targetContainers) == 0 {
							log.Printf("QUEUE: No matching containers found for operation %s, releasing lock", q.OperationID)
							o.releaseStackLock(q.StackName)
							o.failOperation(ctx, q.OperationID, "queued", "Queued containers no longer exist")
							continue
						}

						// Dispatch based on the stored operation type
						opCtx := context.Background()
						switch q.OperationType {
						case "restart":
							if len(targetContainers) == 1 {
								go o.executeRestart(opCtx, q.OperationID, targetContainers[0], q.StackName, false)
							} else {
								levels := o.computeStackRestartLevels(targetContainers)
								go o.executeStackRestart(opCtx, q.OperationID, targetContainers, levels, q.StackName, false)
							}
						case "fix_mismatch":
							if len(targetContainers) == 1 {
								expectedImage, err := o.deriveExpectedImage(targetContainers[0])
								if err != nil {
									log.Printf("QUEUE: Failed to derive expected image for fix_mismatch %s: %v", q.OperationID, err)
									o.releaseStackLock(q.StackName)
									o.failOperation(ctx, q.OperationID, "queued", fmt.Sprintf("Failed to derive expected image: %v", err))
									continue
								}
								go o.executeFixMismatch(opCtx, q.OperationID, targetContainers[0], expectedImage, q.StackName)
							} else {
								log.Printf("QUEUE: fix_mismatch with multiple containers not supported, operation %s", q.OperationID)
								o.releaseStackLock(q.StackName)
								o.failOperation(ctx, q.OperationID, "queued", "fix_mismatch only supports single containers")
							}
						default: // "single", "batch", "stack"
							if len(targetContainers) == 1 {
								go o.executeSingleUpdate(opCtx, q.OperationID, targetContainers[0], "latest", q.StackName, false)
							} else {
								go o.executeBatchUpdate(opCtx, q.OperationID, targetContainers, nil, q.StackName, nil)
							}
						}
					}
				}
			}
		}
	}
}

// deriveExpectedImage reads the compose file for a container and returns the expected image reference.
// Used by processQueue to reconstruct fix_mismatch parameters from a queued operation.
func (o *UpdateOrchestrator) deriveExpectedImage(container *docker.Container) (string, error) {
	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath == "" {
		return "", fmt.Errorf("no compose file found for container %s", container.Name)
	}

	resolvedPath, err := o.resolveComposeFile(composeFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve compose file: %w", err)
	}

	serviceName := container.Labels["com.docker.compose.service"]
	if serviceName == "" {
		return "", fmt.Errorf("container %s has no compose service label", container.Name)
	}

	content, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", fmt.Errorf("failed to read compose file: %w", err)
	}

	return extractImageFromCompose(content, serviceName)
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

// RestartSingleContainer initiates a restart for a single container with SSE progress events.
// This is the main entry point for restarting containers via the API.
func (o *UpdateOrchestrator) RestartSingleContainer(ctx context.Context, containerName string, force bool) (string, error) {
	operationID := uuid.New().String()

	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	var targetContainer *docker.Container
	for _, c := range containers {
		if c.Name == containerName {
			targetContainer = &c
			break
		}
	}

	if targetContainer == nil {
		return "", fmt.Errorf("container not found: %s", containerName)
	}

	stackName := o.stackManager.DetermineStack(ctx, *targetContainer)

	// Find all dependents first
	dependents := o.findDependentContainerNames(containers, containerName)

	// Create operation record
	op := storage.UpdateOperation{
		OperationID:        operationID,
		ContainerID:        targetContainer.ID,
		ContainerName:      containerName,
		StackName:          stackName,
		OperationType:      "restart",
		Status:             "validating",
		DependentsAffected: dependents,
		CreatedAt:          time.Now(),
	}

	if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
		log.Printf("RESTART: Failed to save operation: %v", err)
	}

	// Check if stack is locked
	if stackName != "" && !o.acquireStackLock(stackName) {
		// Queue the operation
		if err := o.queueOperation(ctx, operationID, stackName, []string{containerName}, "restart"); err != nil {
			return "", fmt.Errorf("failed to queue operation: %w", err)
		}
		o.publishProgress(operationID, containerName, stackName, "queued", 0, "Operation queued - stack is busy")
		return operationID, nil
	}

	// Start restart in background with a fresh context (not tied to HTTP request)
	go o.executeRestart(context.Background(), operationID, targetContainer, stackName, force)

	return operationID, nil
}

// executeRestart performs the actual restart process with SSE progress events.
func (o *UpdateOrchestrator) executeRestart(ctx context.Context, operationID string, container *docker.Container, stackName string, force bool) {
	if stackName != "" {
		defer o.releaseStackLock(stackName)
	}

	log.Printf("RESTART: Starting executeRestart for operation=%s container=%s", operationID, container.Name)

	// Stage 1: Validating (0-10%)
	o.publishProgress(operationID, container.Name, stackName, "validating", 0, "Validating permissions")

	// Restart only needs Docker socket access, not compose file access.
	// The compose file is optional — executeRestart falls back to Docker API if unavailable.
	if err := o.checkDockerAccess(ctx); err != nil {
		o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Permission check failed: %v", err))
		return
	}

	log.Printf("RESTART: Permissions OK for operation=%s", operationID)

	// Stage 2: Pre-update check for main container (10-15%)
	if !force {
		if scriptPath, ok := container.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
			o.publishProgress(operationID, container.Name, stackName, "validating", 10, "Running pre-update check")
			log.Printf("RESTART: Running pre-update check for container %s: %s", container.Name, scriptPath)

			if err := runPreUpdateCheck(ctx, container, scriptPath); err != nil {
				o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Pre-update check failed: %v", err))
				return
			}
			log.Printf("RESTART: Pre-update check passed for container %s", container.Name)
		}
	} else {
		log.Printf("RESTART: Skipping pre-update check for %s (force=true)", container.Name)
	}

	// Stage 2b: Pre-validate dependent containers' pre-update checks (15-20%)
	// This ensures we fail BEFORE restarting the main container if any dependent would fail
	if !force {
		o.publishProgress(operationID, container.Name, stackName, "validating", 15, "Validating dependent containers")
		log.Printf("RESTART: Pre-validating dependent containers for %s", container.Name)

		depPreCheck, err := o.validateDependentPreChecks(ctx, container.Name)
		if err != nil {
			log.Printf("RESTART: Warning - failed to validate dependent pre-checks: %v", err)
			// Continue anyway - we'll handle failures during actual restart
		} else if len(depPreCheck.Failed) > 0 {
			// One or more dependents failed their pre-update checks - fail the entire operation
			failedNames := strings.Join(depPreCheck.Failed, ", ")
			errMsg := fmt.Sprintf("Dependent container(s) failed pre-update check: %s. Use force restart to skip checks.", failedNames)
			log.Printf("RESTART: %s", errMsg)
			o.failOperation(ctx, operationID, "validating", errMsg)
			return
		} else if len(depPreCheck.Dependents) > 0 {
			log.Printf("RESTART: All %d dependent(s) passed pre-update checks", len(depPreCheck.Dependents))
			o.publishProgress(operationID, container.Name, stackName, "validating", 18, fmt.Sprintf("All %d dependent(s) validated", len(depPreCheck.Dependents)))
		}
	} else {
		log.Printf("RESTART: Skipping dependent pre-update checks for %s (force=true)", container.Name)
	}

	// Set started_at timestamp
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.StartedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	// Check if this is a self-restart (docksmith restarting itself)
	if selfupdate.IsSelfContainer(container.ID, container.Image, container.Name) {
		log.Printf("SELF-RESTART: Detected self-restart for docksmith container %s", container.Name)
		o.executeSelfRestart(ctx, operationID, container, stackName)
		return
	}

	// Stage 3: Stopping container (20-40%)
	o.publishProgress(operationID, container.Name, stackName, "stopping", 20, "Stopping container")

	// Stage 4: Starting container (40-60%)
	o.publishProgress(operationID, container.Name, stackName, "starting", 40, "Restarting container")

	// Restart the container using docker compose if available
	composeFilePath := o.getComposeFilePath(container)
	if composeFilePath != "" {
		hostComposeFilePath := o.getComposeFilePathForHost(container)
		recreator := compose.NewRecreator(o.dockerClient)

		// Use RestartWithCompose for simple restart (no config changes)
		if err := recreator.RestartWithCompose(ctx, container, hostComposeFilePath, composeFilePath); err != nil {
			// Fall back to Docker API
			log.Printf("RESTART: Compose restart failed, falling back to Docker API: %v", err)
			if restartErr := o.dockerSDK.ContainerRestart(ctx, container.Name, dockerContainer.StopOptions{}); restartErr != nil {
				o.failOperation(ctx, operationID, "starting", fmt.Sprintf("Failed to restart container: %v", restartErr))
				return
			}
		}
	} else {
		// No compose file - use Docker API directly
		if err := o.dockerSDK.ContainerRestart(ctx, container.Name, dockerContainer.StopOptions{}); err != nil {
			o.failOperation(ctx, operationID, "starting", fmt.Sprintf("Failed to restart container: %v", err))
			return
		}
	}

	o.publishProgress(operationID, container.Name, stackName, "starting", 60, "Container restarted")
	log.Printf("RESTART: Container %s restarted", container.Name)

	// Stage 5: Health check (60-80%)
	o.publishProgress(operationID, container.Name, stackName, "health_check", 60, "Verifying container health")

	if err := o.waitForHealthy(ctx, container.Name, o.healthCheckCfg.Timeout); err != nil {
		log.Printf("RESTART: Health check warning for %s: %v", container.Name, err)
		o.publishProgress(operationID, container.Name, stackName, "health_check", 70, fmt.Sprintf("Health check warning: %v", err))
		// Don't fail the restart - container was restarted, just health check had issues
	} else {
		o.publishProgress(operationID, container.Name, stackName, "health_check", 80, "Health check passed")
		log.Printf("RESTART: Health check passed for %s", container.Name)
	}

	// Stage 6: Restart dependent containers (80-95%)
	o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 80, "Restarting dependent containers")

	// For restart, skip pre-checks on dependents if force was used
	depResult, err := o.restartDependentContainers(ctx, container.Name, force)
	if err != nil {
		log.Printf("RESTART: Warning - failed to restart dependent containers for %s: %v", container.Name, err)
		o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 90, fmt.Sprintf("Warning: %v", err))
		// Don't fail the restart if dependent restarts fail
	} else if depResult != nil {
		// Publish detailed dependent status
		if len(depResult.Blocked) > 0 {
			blockedMsg := fmt.Sprintf("Blocked dependents: %s", strings.Join(depResult.Blocked, ", "))
			o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 85, blockedMsg)
			log.Printf("RESTART: %s", blockedMsg)
		}
		if len(depResult.Restarted) > 0 {
			restartedMsg := fmt.Sprintf("Restarted dependents: %s", strings.Join(depResult.Restarted, ", "))
			o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 90, restartedMsg)
			log.Printf("RESTART: %s", restartedMsg)
		}
		// Final summary
		if len(depResult.Blocked) > 0 && len(depResult.Restarted) == 0 {
			o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 95, fmt.Sprintf("Warning: All %d dependent(s) blocked by pre-update checks", len(depResult.Blocked)))
		} else if len(depResult.Blocked) > 0 {
			o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 95, fmt.Sprintf("Dependents: %d restarted, %d blocked", len(depResult.Restarted), len(depResult.Blocked)))
		} else if len(depResult.Restarted) > 0 {
			o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 95, fmt.Sprintf("Dependents restarted: %s", strings.Join(depResult.Restarted, ", ")))
		} else {
			o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 95, "No dependents to restart")
		}
	} else {
		o.publishProgress(operationID, container.Name, stackName, "restarting_dependents", 95, "No dependents to restart")
	}

	// Stage 7: Complete (100%)
	completedNow := time.Now()
	completedOp, completedFound, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if completedFound {
		completedOp.Status = "complete"
		completedOp.CompletedAt = &completedNow
		o.storage.SaveUpdateOperation(ctx, completedOp)
	}

	o.publishProgress(operationID, container.Name, stackName, "complete", 100, "Restart completed successfully")
	log.Printf("RESTART: Successfully completed restart for container %s", container.Name)

	// Publish container updated event for dashboard refresh
	if o.eventBus != nil {
		o.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"container_id":   container.ID,
				"container_name": container.Name,
				"operation_id":   operationID,
				"status":         "restarted",
			},
		})
	}
}

// RestartStack initiates a restart for all containers in a stack as a single operation.
// It builds a dependency graph to determine restart order, groups containers into
// parallelizable levels, and restarts level by level.
func (o *UpdateOrchestrator) RestartStack(ctx context.Context, stackName string, containerNames []string, force bool) (string, error) {
	operationID := uuid.New().String()

	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to list containers: %w", err)
	}

	// Filter to requested containers
	var stackContainers []*docker.Container
	for i := range containers {
		for _, name := range containerNames {
			if containers[i].Name == name {
				stackContainers = append(stackContainers, &containers[i])
				break
			}
		}
	}

	if len(stackContainers) == 0 {
		return "", fmt.Errorf("no matching containers found in stack %s", stackName)
	}

	// Build dependency graph and compute restart levels
	levels := o.computeStackRestartLevels(stackContainers)

	// Create batch details for all containers
	batchDetails := make([]storage.BatchContainerDetail, 0, len(stackContainers))
	for _, c := range stackContainers {
		batchDetails = append(batchDetails, storage.BatchContainerDetail{
			ContainerName: c.Name,
			StackName:     stackName,
			Status:        "pending",
			Message:       "Waiting to restart",
		})
	}

	// Create a single operation for the entire stack restart
	op := storage.UpdateOperation{
		OperationID:   operationID,
		ContainerName: stackName,
		StackName:     stackName,
		OperationType: "restart",
		Status:        "validating",
		BatchDetails:  batchDetails,
		CreatedAt:     time.Now(),
	}

	if err := o.storage.SaveUpdateOperation(ctx, op); err != nil {
		return "", fmt.Errorf("failed to save operation: %w", err)
	}

	// Acquire stack lock
	if !o.acquireStackLock(stackName) {
		if err := o.queueOperation(ctx, operationID, stackName, containerNames, "restart"); err != nil {
			return "", fmt.Errorf("failed to queue operation: %w", err)
		}
		o.publishProgress(operationID, "", stackName, "queued", 0, "Operation queued - stack is busy")
		return operationID, nil
	}

	go o.executeStackRestart(context.Background(), operationID, stackContainers, levels, stackName, force)

	return operationID, nil
}

// computeStackRestartLevels builds a dependency graph for the stack containers and
// returns them grouped into levels. Containers in the same level can restart in parallel.
// Level 0 has no dependencies (within the set), level 1 depends on level 0, etc.
func (o *UpdateOrchestrator) computeStackRestartLevels(stackContainers []*docker.Container) [][]string {
	// Build service-to-container name map for this stack
	serviceToContainer := make(map[string]string)
	stackSet := make(map[string]bool)
	for _, c := range stackContainers {
		stackSet[c.Name] = true
		if svc, ok := c.Labels[graph.ServiceLabel]; ok {
			serviceToContainer[svc] = c.Name
		}
	}

	// Build dependency map: container name -> list of container names it depends on (within the stack)
	deps := make(map[string][]string)
	for _, c := range stackContainers {
		var containerDeps []string

		// Parse compose depends_on (uses service names, need translation)
		if dependsOn, ok := c.Labels[graph.DependsOnLabel]; ok && dependsOn != "" {
			for _, svc := range graph.ParseDependsOn(dependsOn) {
				if depName, ok := serviceToContainer[svc]; ok && stackSet[depName] {
					containerDeps = append(containerDeps, depName)
				}
			}
		}

		// Parse network_mode dependency
		if networkMode, ok := c.Labels[graph.NetworkModeLabel]; ok && strings.HasPrefix(networkMode, "service:") {
			svc := strings.TrimPrefix(networkMode, "service:")
			if depName, ok := serviceToContainer[svc]; ok && stackSet[depName] {
				// Avoid duplicates
				isDup := false
				for _, d := range containerDeps {
					if d == depName {
						isDup = true
						break
					}
				}
				if !isDup {
					containerDeps = append(containerDeps, depName)
				}
			}
		}

		// Parse docksmith.restart-after label (already container names)
		if restartAfter, ok := c.Labels[scripts.RestartAfterLabel]; ok && restartAfter != "" {
			for _, dep := range strings.Split(restartAfter, ",") {
				dep = strings.TrimSpace(dep)
				if stackSet[dep] {
					isDup := false
					for _, d := range containerDeps {
						if d == dep {
							isDup = true
							break
						}
					}
					if !isDup {
						containerDeps = append(containerDeps, dep)
					}
				}
			}
		}

		deps[c.Name] = containerDeps
	}

	// Compute levels via iterative in-degree reduction (similar to Kahn's algorithm)
	assigned := make(map[string]int) // container name -> level
	remaining := make(map[string]bool)
	for _, c := range stackContainers {
		remaining[c.Name] = true
	}

	var levels [][]string
	for len(remaining) > 0 {
		// Find containers whose dependencies are all already assigned
		var level []string
		for name := range remaining {
			allDepsAssigned := true
			for _, dep := range deps[name] {
				if remaining[dep] {
					allDepsAssigned = false
					break
				}
			}
			if allDepsAssigned {
				level = append(level, name)
			}
		}

		if len(level) == 0 {
			// Circular dependency - just add all remaining to break the cycle
			log.Printf("STACK-RESTART: Circular dependency detected, adding remaining containers to current level")
			for name := range remaining {
				level = append(level, name)
			}
		}

		for _, name := range level {
			assigned[name] = len(levels)
			delete(remaining, name)
		}
		levels = append(levels, level)
	}

	// Log the computed levels
	for i, level := range levels {
		log.Printf("STACK-RESTART: Level %d: %v", i, level)
	}

	return levels
}

// executeStackRestart runs the stack restart in background, level by level.
func (o *UpdateOrchestrator) executeStackRestart(ctx context.Context, operationID string, containers []*docker.Container, levels [][]string, stackName string, force bool) {
	defer o.releaseStackLock(stackName)

	log.Printf("STACK-RESTART: Starting stack restart for %s with %d container(s) in %d level(s)", stackName, len(containers), len(levels))

	// Build container map for quick lookup
	containerMap := make(map[string]*docker.Container)
	for _, c := range containers {
		containerMap[c.Name] = c
	}

	// Stage 1: Validate (0-10%)
	o.publishProgress(operationID, "", stackName, "validating", 0, "Validating permissions")

	if err := o.checkDockerAccess(ctx); err != nil {
		o.failOperation(ctx, operationID, "validating", fmt.Sprintf("Permission check failed: %v", err))
		return
	}

	// Pre-update checks for all containers
	if !force {
		o.publishProgress(operationID, "", stackName, "validating", 5, "Running pre-update checks")
		for _, c := range containers {
			if scriptPath, ok := c.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
				log.Printf("STACK-RESTART: Running pre-update check for %s", c.Name)
				if err := runPreUpdateCheck(ctx, c, scriptPath); err != nil {
					errMsg := fmt.Sprintf("Pre-update check failed for %s: %v", c.Name, err)
					o.updateBatchDetailStatus(ctx, operationID, c.Name, "failed", errMsg)
					o.failOperation(ctx, operationID, "validating", errMsg)
					return
				}
			}
		}
	}

	// Set started_at timestamp
	now := time.Now()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if found {
		op.StartedAt = &now
		o.storage.SaveUpdateOperation(ctx, op)
	}

	o.publishProgress(operationID, "", stackName, "restarting", 10, "Starting restarts")

	// Stage 2: Restart by level (10-90%)
	totalLevels := len(levels)
	allSuccess := true

	for levelIdx, level := range levels {
		levelProgress := 10 + (80 * levelIdx / totalLevels)
		o.publishProgress(operationID, "", stackName, "restarting", levelProgress,
			fmt.Sprintf("Restarting level %d/%d (%d container(s))", levelIdx+1, totalLevels, len(level)))

		log.Printf("STACK-RESTART: Restarting level %d: %v", levelIdx, level)

		// Restart all containers in this level in parallel
		var wg sync.WaitGroup
		var mu sync.Mutex
		levelFailed := false

		for _, name := range level {
			c := containerMap[name]
			if c == nil {
				continue
			}

			wg.Add(1)
			go func(container *docker.Container, containerName string) {
				defer wg.Done()

				o.updateBatchDetailStatus(ctx, operationID, containerName, "restarting", "Restarting...")

				// Check for self-restart
				if selfupdate.IsSelfContainer(container.ID, container.Image, container.Name) {
					log.Printf("STACK-RESTART: Skipping self-restart for docksmith container %s", containerName)
					o.updateBatchDetailStatus(ctx, operationID, containerName, "complete", "Skipped (self)")
					return
				}

				// Restart the container
				composeFilePath := o.getComposeFilePath(container)
				var restartErr error
				if composeFilePath != "" {
					hostComposeFilePath := o.getComposeFilePathForHost(container)
					recreator := compose.NewRecreator(o.dockerClient)
					restartErr = recreator.RestartWithCompose(ctx, container, hostComposeFilePath, composeFilePath)
					if restartErr != nil {
						log.Printf("STACK-RESTART: Compose restart failed for %s, falling back to Docker API: %v", containerName, restartErr)
						restartErr = o.dockerSDK.ContainerRestart(ctx, containerName, dockerContainer.StopOptions{})
					}
				} else {
					restartErr = o.dockerSDK.ContainerRestart(ctx, containerName, dockerContainer.StopOptions{})
				}

				if restartErr != nil {
					errMsg := fmt.Sprintf("Failed to restart: %v", restartErr)
					log.Printf("STACK-RESTART: %s: %s", containerName, errMsg)
					o.updateBatchDetailStatus(ctx, operationID, containerName, "failed", errMsg)
					mu.Lock()
					levelFailed = true
					mu.Unlock()
					return
				}

				// Wait for healthy
				if err := o.waitForHealthy(ctx, containerName, o.healthCheckCfg.Timeout); err != nil {
					log.Printf("STACK-RESTART: Health check warning for %s: %v", containerName, err)
					// Don't fail - container was restarted
				}

				log.Printf("STACK-RESTART: %s restarted successfully", containerName)
				o.updateBatchDetailStatus(ctx, operationID, containerName, "complete", "Restarted successfully")
			}(c, name)
		}

		wg.Wait()

		if levelFailed {
			allSuccess = false
			// Continue to next level even if some containers failed
			log.Printf("STACK-RESTART: Level %d had failures, continuing", levelIdx)
		}
	}

	// Stage 3: External dependents (90-95%)
	o.publishProgress(operationID, "", stackName, "restarting_dependents", 90, "Checking for external dependents")

	allContainers, err := o.dockerClient.ListContainers(ctx)
	if err == nil {
		// Find containers outside the stack that have restart-after pointing to any stack container
		stackSet := make(map[string]bool)
		for _, c := range containers {
			stackSet[c.Name] = true
		}

		var externalDeps []string
		for _, c := range allContainers {
			if stackSet[c.Name] {
				continue // Skip containers in the stack
			}
			if restartAfter, ok := c.Labels[scripts.RestartAfterLabel]; ok && restartAfter != "" {
				for _, dep := range strings.Split(restartAfter, ",") {
					dep = strings.TrimSpace(dep)
					if stackSet[dep] {
						externalDeps = append(externalDeps, c.Name)
						break
					}
				}
			}
		}

		if len(externalDeps) > 0 {
			log.Printf("STACK-RESTART: Found %d external dependent(s): %v", len(externalDeps), externalDeps)
			for _, depName := range externalDeps {
				log.Printf("STACK-RESTART: Restarting external dependent: %s", depName)
				if restartErr := o.dockerSDK.ContainerRestart(ctx, depName, dockerContainer.StopOptions{}); restartErr != nil {
					log.Printf("STACK-RESTART: Failed to restart external dependent %s: %v", depName, restartErr)
				} else if healthErr := o.waitForHealthy(ctx, depName, o.healthCheckCfg.Timeout); healthErr != nil {
					log.Printf("STACK-RESTART: Health check warning for external dependent %s: %v", depName, healthErr)
				}
			}
		}
	}

	// Stage 4: Complete (100%)
	completedNow := time.Now()
	completedOp, completedFound, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if completedFound {
		if allSuccess {
			completedOp.Status = "complete"
		} else {
			completedOp.Status = "complete"
			completedOp.ErrorMessage = "Some containers failed to restart"
		}
		completedOp.CompletedAt = &completedNow
		o.storage.SaveUpdateOperation(ctx, completedOp)
	}

	status := "complete"
	message := fmt.Sprintf("Stack %s restarted successfully (%d container(s))", stackName, len(containers))
	if !allSuccess {
		message = fmt.Sprintf("Stack %s restart completed with errors", stackName)
	}

	o.publishProgress(operationID, "", stackName, status, 100, message)
	log.Printf("STACK-RESTART: %s", message)

	// Publish container updated event for dashboard refresh
	if o.eventBus != nil {
		o.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"container_name": stackName,
				"operation_id":   operationID,
				"status":         "restarted",
			},
		})
	}
}

// updateBatchDetailStatus updates a single container's status within the operation's BatchDetails.
// Uses batchDetailMu to serialize read-modify-write cycles when called from concurrent goroutines.
func (o *UpdateOrchestrator) updateBatchDetailStatus(ctx context.Context, operationID, containerName, status, message string) {
	o.batchDetailMu.Lock()
	op, found, _ := o.storage.GetUpdateOperation(ctx, operationID)
	if !found {
		o.batchDetailMu.Unlock()
		return
	}
	var stackName string
	for i, d := range op.BatchDetails {
		if d.ContainerName == containerName {
			op.BatchDetails[i].Status = status
			op.BatchDetails[i].Message = message
			break
		}
	}
	stackName = op.StackName
	o.storage.SaveUpdateOperation(ctx, op)
	o.batchDetailMu.Unlock()

	// Publish SSE event outside the lock
	o.publishProgress(operationID, containerName, stackName, status, 0, message)
}

// runPreUpdateCheck runs a pre-update check script for a container
func runPreUpdateCheck(ctx context.Context, container *docker.Container, scriptPath string) error {
	// Use shared implementation with path translation disabled (orchestrator runs in container)
	return scripts.ExecutePreUpdateCheck(ctx, container, scriptPath, false)
}
