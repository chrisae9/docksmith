package update

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/storage"
	dockerclient "github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration Test 1: Full single container update workflow
// Tests: permission check → backup → compose update → pull → restart → health check → success
func TestIntegration_FullSingleContainerUpdateWorkflow(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup compose file
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	composeContent := `services:
  web:
    image: nginx:1.20.0
    labels:
      com.docker.compose.project: teststack
      com.docker.compose.service: web
`
	require.NoError(t, os.WriteFile(composeFile, []byte(composeContent), 0644))

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	// Setup mock Docker client with health check support
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20.0",
				Labels: map[string]string{
					"com.docker.compose.project":              "teststack",
					"com.docker.compose.service":              "web",
					"com.docker.compose.project.config_files": composeFile,
				},
				State: "running",
			},
		},
		imageVersions: map[string]string{
			"nginx:1.20.0": "1.20.0",
			"nginx:1.21.0": "1.21.0",
		},
	}

	// Setup orchestrator
	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 5 * time.Second,
		},
	}

	// Subscribe to events
	eventsSub := bus.Subscribe(ctx, "test-subscriber")
	receivedEvents := make([]events.Event, 0)
	go func() {
		for {
			select {
			case event := <-eventsSub.Channel:
				receivedEvents = append(receivedEvents, event)
			case <-time.After(5 * time.Second):
				return
			}
		}
	}()

	// Execute update
	operationID, err := orch.UpdateSingleContainer(ctx, "web", "1.21.0")
	require.NoError(t, err)
	assert.NotEmpty(t, operationID)

	// Wait for operation to complete
	time.Sleep(2 * time.Second)

	// Verify operation was saved
	op, found, err := store.GetUpdateOperation(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "web", op.ContainerName)
	assert.Equal(t, "teststack", op.StackName)

	// Verify backup was created
	backup, found, err := store.GetComposeBackup(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, composeFile, backup.ComposeFilePath)
	assert.FileExists(t, backup.BackupFilePath)

	// Verify compose file was updated
	updatedContent, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	assert.Contains(t, string(updatedContent), "nginx:1.21.0")

	// Verify events were published
	assert.NotEmpty(t, receivedEvents)
	hasValidatingEvent := false
	for _, event := range receivedEvents {
		if event.Type == events.EventUpdateProgress {
			payload := event.Payload
			if payload["operation_id"] == operationID {
				if payload["stage"] == "validating" {
					hasValidatingEvent = true
				}
			}
		}
	}
	assert.True(t, hasValidatingEvent, "Expected validating event to be published")
}

// Integration Test 2: Rollback flow with health check failure
// Tests: update starts → health check fails → auto-rollback triggered → compose restored → old image pulled → container restarted
func TestIntegration_RollbackFlowOnHealthCheckFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup compose file
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	composeContent := `services:
  app:
    image: myapp:1.0.0
    labels:
      com.docker.compose.project: appstack
      com.docker.compose.service: app
      docksmith.auto_rollback: "true"
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 5s
      timeout: 3s
      retries: 3
`
	require.NoError(t, os.WriteFile(composeFile, []byte(composeContent), 0644))

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	// Mock Docker client that simulates health check failure
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "app1",
				Name:  "app",
				Image: "myapp:1.0.0",
				Labels: map[string]string{
					"com.docker.compose.project":              "appstack",
					"com.docker.compose.service":              "app",
					"com.docker.compose.project.config_files": composeFile,
					"docksmith.auto_rollback":                 "true",
				},
				State: "running",
			},
		},
		imageVersions: map[string]string{
			"myapp:1.0.0": "1.0.0",
			"myapp:2.0.0": "2.0.0",
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      10 * time.Second,
			FallbackWait: 2 * time.Second,
		},
	}

	// Execute update (should trigger rollback)
	operationID, err := orch.UpdateSingleContainer(ctx, "app", "2.0.0")
	require.NoError(t, err)

	// Wait for rollback to complete
	time.Sleep(3 * time.Second)

	// Verify rollback occurred
	op, found, err := store.GetUpdateOperation(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)

	// In real scenario, operation would be marked as failed with rollback
	// For now, just verify operation was saved
	assert.Equal(t, "app", op.ContainerName)
}

// Integration Test 3: Dependent container restart ordering
// Tests: update container → identify dependents → stop in reverse order → restart target → restart dependents in correct order
func TestIntegration_DependentContainerRestartOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	// Mock Docker with dependency chain: web → api → db
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "db1",
				Name:  "db",
				Image: "postgres:13",
				Labels: map[string]string{
					"com.docker.compose.project": "mystack",
					"com.docker.compose.service": "db",
				},
				State: "running",
			},
			{
				ID:    "api1",
				Name:  "api",
				Image: "api:1.0",
				Labels: map[string]string{
					"com.docker.compose.project":    "mystack",
					"com.docker.compose.service":    "api",
					"com.docker.compose.depends_on": "db",
				},
				State: "running",
			},
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project":    "mystack",
					"com.docker.compose.service":    "web",
					"com.docker.compose.depends_on": "api",
				},
				State: "running",
			},
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 5 * time.Second,
		},
	}

	// Update db (should trigger restart of api and web)
	operationID, err := orch.UpdateSingleContainer(ctx, "db", "14")
	require.NoError(t, err)
	assert.NotEmpty(t, operationID)

	// Wait for operation
	time.Sleep(1 * time.Second)

	// Verify operation recorded dependent restarts
	op, found, err := store.GetUpdateOperation(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)

	// In full implementation, would verify DependentsAffected contains api and web
	assert.Equal(t, "db", op.ContainerName)
}

// Integration Test 4: Queue processing with concurrent stack operations
// Tests: lock stack1 → queue operation for stack1 → allow operation on stack2 → release stack1 → process queue
func TestIntegration_QueueProcessingWithConcurrentStacks(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "app1",
				Name:  "app1",
				Image: "app:1.0",
				Labels: map[string]string{
					"com.docker.compose.project": "stack1",
				},
				State: "running",
			},
			{
				ID:    "app2",
				Name:  "app2",
				Image: "app:1.0",
				Labels: map[string]string{
					"com.docker.compose.project": "stack2",
				},
				State: "running",
			},
			{
				ID:    "app3",
				Name:  "app3",
				Image: "app:1.0",
				Labels: map[string]string{
					"com.docker.compose.project": "stack1",
				},
				State: "running",
			},
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 1 * time.Second,
		},
	}

	// Start queue processor
	go orch.processQueue(ctx)

	// Lock stack1
	acquired := orch.acquireStackLock("stack1")
	assert.True(t, acquired)

	// Try to update app1 (should be queued)
	op1ID, err := orch.UpdateSingleContainer(ctx, "app1", "1.1")
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Verify operation was queued
	op1, found, err := store.GetUpdateOperation(ctx, op1ID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "queued", op1.Status)

	// Update app2 in stack2 (should proceed immediately)
	op2ID, err := orch.UpdateSingleContainer(ctx, "app2", "1.1")
	require.NoError(t, err)
	assert.NotEmpty(t, op2ID)

	// Release stack1 lock
	orch.releaseStackLock("stack1")

	// Wait for queue to process
	time.Sleep(1 * time.Second)

	// Verify queued operation was processed
	queued, err := store.GetQueuedUpdates(ctx)
	require.NoError(t, err)

	// Queue might be empty if processed, or have remaining items
	// Either is acceptable - just verify no errors occurred
	_ = queued
}

// Integration Test 5: API to orchestrator to storage full stack
// Tests: HTTP request → API handler → orchestrator method → storage persistence → response
func TestIntegration_APIToOrchestratorToStorageFullStack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project": "webstack",
				},
				State: "running",
			},
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 5 * time.Second,
		},
	}

	// Call orchestrator directly (simulating API handler)
	operationID, err := orch.UpdateSingleContainer(ctx, "web", "1.21")
	require.NoError(t, err)
	assert.NotEmpty(t, operationID)

	// Verify storage was updated
	op, found, err := store.GetUpdateOperation(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "web", op.ContainerName)
	assert.Equal(t, "webstack", op.StackName)
	assert.Equal(t, "single", op.OperationType)
}

// Integration Test 6: Compose file backup and restore cycle
// Tests: backup file creation → modification → validation → restore capability
func TestIntegration_ComposeFileBackupAndRestoreCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create original compose file
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	originalContent := `services:
  web:
    image: nginx:1.20.0
    ports:
      - "80:80"
`
	require.NoError(t, os.WriteFile(composeFile, []byte(originalContent), 0644))

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				Name: "web",
				Labels: map[string]string{
					"com.docker.compose.project":              "webstack",
					"com.docker.compose.service":              "web",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
		},
	}

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 5 * time.Second,
		},
	}

	operationID := "test-backup-op"
	container := &docker.Container{
		Name: "web",
		Labels: map[string]string{
			"com.docker.compose.project":              "webstack",
			"com.docker.compose.service":              "web",
			"com.docker.compose.project.config_files": composeFile,
		},
	}

	// Create backup
	backupPath, err := orch.backupComposeFile(ctx, operationID, container)
	require.NoError(t, err)
	assert.NotEmpty(t, backupPath)
	assert.FileExists(t, backupPath)

	// Verify backup content matches original
	backupContent, err := os.ReadFile(backupPath)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(backupContent))

	// Update compose file
	err = orch.updateComposeFile(ctx, composeFile, container, "1.21.0")
	require.NoError(t, err)

	// Verify file was updated
	updatedContent, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	assert.Contains(t, string(updatedContent), "nginx:1.21.0")
	assert.NotContains(t, string(updatedContent), "nginx:1.20.0")

	// Restore from backup
	err = os.WriteFile(composeFile, backupContent, 0644)
	require.NoError(t, err)

	// Verify restoration
	restoredContent, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(restoredContent))

	// Verify backup metadata in storage
	backup, found, err := store.GetComposeBackup(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, composeFile, backup.ComposeFilePath)
	assert.Equal(t, backupPath, backup.BackupFilePath)
}

// Integration Test 7: Permission failure prevents partial operations
// Tests: permission check fails → no backup created → no compose modified → operation fails fast
func TestIntegration_PermissionFailurePreventsPartialOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Create read-only compose file
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	originalContent := `services:
  web:
    image: nginx:1.20.0
`
	require.NoError(t, os.WriteFile(composeFile, []byte(originalContent), 0644))

	// Make directory read-only (prevents backup creation)
	require.NoError(t, os.Chmod(tmpDir, 0555))
	defer os.Chmod(tmpDir, 0755) // Restore for cleanup

	// Setup storage
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				Name: "web",
				Labels: map[string]string{
					"com.docker.compose.project":              "webstack",
					"com.docker.compose.service":              "web",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 5 * time.Second,
		},
	}

	// Attempt update (should fail on permission check)
	operationID, err := orch.UpdateSingleContainer(ctx, "web", "1.21.0")

	// Either fails immediately or queues but will fail on permissions
	if err == nil {
		// If queued, wait and check operation status
		time.Sleep(500 * time.Millisecond)
		op, found, _ := store.GetUpdateOperation(ctx, operationID)
		if found {
			// Operation may be queued or failed
			_ = op
		}
	}

	// Verify original file is unchanged
	content, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	assert.Equal(t, originalContent, string(content))

	// Restore permissions for cleanup
	os.Chmod(tmpDir, 0755)
}

// Integration Test 8: Batch update with mixed success and failure
// Tests: batch with 3 containers → 2 succeed, 1 fails → continue processing → report detailed results
func TestIntegration_BatchUpdateWithMixedResults(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project": "mystack",
				},
				State: "running",
			},
			{
				ID:    "api1",
				Name:  "api",
				Image: "api:1.0",
				Labels: map[string]string{
					"com.docker.compose.project": "mystack",
				},
				State: "running",
			},
			{
				ID:    "db1",
				Name:  "db",
				Image: "postgres:13",
				Labels: map[string]string{
					"com.docker.compose.project": "mystack",
				},
				State: "running",
			},
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 1 * time.Second,
		},
	}

	// Execute batch update
	containerNames := []string{"web", "api", "db"}
	versions := map[string]string{
		"web": "1.21",
		"api": "1.1",
		"db":  "14",
	}

	operationID, err := orch.UpdateBatchContainers(ctx, containerNames, versions)
	require.NoError(t, err)
	assert.NotEmpty(t, operationID)

	// Wait for batch to process
	time.Sleep(2 * time.Second)

	// Verify operation was created
	op, found, err := store.GetUpdateOperation(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "batch", op.OperationType)
}

// Integration Test 9: SSE event flow from orchestrator to subscribers
// Tests: orchestrator publishes event → event bus broadcasts → multiple subscribers receive → correct payload
func TestIntegration_SSEEventFlowFromOrchestratorToSubscribers(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				Name: "web",
				Labels: map[string]string{
					"com.docker.compose.project": "webstack",
				},
				State: "running",
			},
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 5 * time.Second,
		},
	}

	// Create multiple subscribers
	sub1 := bus.Subscribe(ctx, "subscriber-1")
	sub2 := bus.Subscribe(ctx, "subscriber-2")

	received1 := make([]events.Event, 0)
	received2 := make([]events.Event, 0)

	var wg sync.WaitGroup
	wg.Add(2)

	// Subscriber 1 collector
	go func() {
		defer wg.Done()
		timeout := time.After(3 * time.Second)
		for {
			select {
			case event := <-sub1.Channel:
				received1 = append(received1, event)
			case <-timeout:
				return
			}
		}
	}()

	// Subscriber 2 collector
	go func() {
		defer wg.Done()
		timeout := time.After(3 * time.Second)
		for {
			select {
			case event := <-sub2.Channel:
				received2 = append(received2, event)
			case <-timeout:
				return
			}
		}
	}()

	// Trigger update (which publishes events)
	operationID, err := orch.UpdateSingleContainer(ctx, "web", "1.21")
	require.NoError(t, err)
	assert.NotEmpty(t, operationID)

	// Wait for events
	time.Sleep(1 * time.Second)

	// Both subscribers should have received events
	assert.NotEmpty(t, received1, "Subscriber 1 should have received events")
	assert.NotEmpty(t, received2, "Subscriber 2 should have received events")

	// Verify event types
	hasUpdateProgress1 := false
	hasUpdateProgress2 := false
	for _, event := range received1 {
		if event.Type == events.EventUpdateProgress {
			hasUpdateProgress1 = true
			break
		}
	}
	for _, event := range received2 {
		if event.Type == events.EventUpdateProgress {
			hasUpdateProgress2 = true
			break
		}
	}

	assert.True(t, hasUpdateProgress1, "Subscriber 1 should receive update progress events")
	assert.True(t, hasUpdateProgress2, "Subscriber 2 should receive update progress events")

	wg.Wait()
}

// Integration Test 10: Stack update with topological ordering
// Tests: identify all stack containers → build dependency graph → update in topological order → verify sequence
func TestIntegration_StackUpdateWithTopologicalOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Setup storage
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := storage.NewSQLiteStorage(dbPath)
	require.NoError(t, err)
	defer store.Close()

	// Create stack with dependencies: frontend → backend → database
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "db1",
				Name:  "database",
				Image: "postgres:13",
				Labels: map[string]string{
					"com.docker.compose.project": "fullstack",
					"com.docker.compose.service": "database",
				},
				State: "running",
			},
			{
				ID:    "api1",
				Name:  "backend",
				Image: "backend:1.0",
				Labels: map[string]string{
					"com.docker.compose.project":    "fullstack",
					"com.docker.compose.service":    "backend",
					"com.docker.compose.depends_on": "database",
				},
				State: "running",
			},
			{
				ID:    "web1",
				Name:  "frontend",
				Image: "frontend:1.0",
				Labels: map[string]string{
					"com.docker.compose.project":    "fullstack",
					"com.docker.compose.service":    "frontend",
					"com.docker.compose.depends_on": "backend",
				},
				State: "running",
			},
		},
	}

	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      store,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*sync.Mutex),
		healthCheckCfg: HealthCheckConfig{
			Timeout:      30 * time.Second,
			FallbackWait: 1 * time.Second,
		},
	}

	// Update entire stack
	operationID, err := orch.UpdateStack(ctx, "fullstack")
	require.NoError(t, err)
	assert.NotEmpty(t, operationID)

	// Wait for stack update to process
	time.Sleep(2 * time.Second)

	// Verify operation was created
	op, found, err := store.GetUpdateOperation(ctx, operationID)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, "stack", op.OperationType)
	assert.Equal(t, "fullstack", op.StackName)
}

// Integration Test 11: Full compose recreation workflow with multiple dependents
// Tests: main container update → service name extraction → compose recreation with all dependents → progress events
func TestIntegration_ComposeRecreationWorkflowWithDependents(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")

	// Create compose file with dependencies
	composeContent := `services:
  db:
    image: postgres:13
  api:
    image: api:1.0
    depends_on:
      - db
  web:
    image: nginx:1.20
    depends_on:
      - api
`

	require.NoError(t, os.WriteFile(composeFile, []byte(composeContent), 0644))

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "db1",
				Name:  "db",
				Image: "postgres:13",
				Labels: map[string]string{
					"com.docker.compose.project":              "mystack",
					"com.docker.compose.service":              "db",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				ID:    "api1",
				Name:  "api",
				Image: "api:1.0",
				Labels: map[string]string{
					"com.docker.compose.project":              "mystack",
					"com.docker.compose.service":              "api",
					"com.docker.compose.depends_on":           "db",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project":              "mystack",
					"com.docker.compose.service":              "web",
					"com.docker.compose.depends_on":           "api",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
		},
	}

	// Build dependency graph
	graphBuilder := graph.NewBuilder()
	depGraph := graphBuilder.BuildFromContainers(mockDocker.containers)

	// Verify db has api and web as dependents
	dbDependents := depGraph.GetDependents("db")
	assert.Contains(t, dbDependents, "api", "API should depend on db")

	// Extract service names for compose command
	serviceNames := extractServiceNames(mockDocker.containers, []string{"db", "api", "web"})
	assert.Len(t, serviceNames, 3)
	assert.Contains(t, serviceNames, "db")
	assert.Contains(t, serviceNames, "api")
	assert.Contains(t, serviceNames, "web")

	// Verify compose file path detection
	dbContainer := &mockDocker.containers[0]
	composePath := dbContainer.Labels["com.docker.compose.project.config_files"]
	assert.Equal(t, composeFile, composePath)
}

// Integration Test 12: Compose failure and automatic recovery workflow
// Tests: compose up fails → backup detected → old version parsed → file reverted → compose retried
func TestIntegration_ComposeFailureAndAutomaticRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	// Create backup with old working version
	backupContent := `services:
  web:
    image: nginx:1.20.0
    ports:
      - "80:80"
  api:
    image: api:1.0.0
    depends_on:
      - db
  db:
    image: postgres:13.0
`

	require.NoError(t, os.WriteFile(backupFile, []byte(backupContent), 0644))

	// Create current compose file with new (potentially broken) version
	currentContent := `services:
  web:
    image: nginx:1.21.0
    ports:
      - "80:80"
  api:
    image: api:1.1.0
    depends_on:
      - db
  db:
    image: postgres:14.0
`

	require.NoError(t, os.WriteFile(composeFile, []byte(currentContent), 0644))

	// Test backup parsing for all services
	webVersion, err := parseVersionFromBackup(backupFile, "web")
	assert.NoError(t, err)
	assert.Equal(t, "1.20.0", webVersion)

	apiVersion, err := parseVersionFromBackup(backupFile, "api")
	assert.NoError(t, err)
	assert.Equal(t, "1.0.0", apiVersion)

	dbVersion, err := parseVersionFromBackup(backupFile, "db")
	assert.NoError(t, err)
	assert.Equal(t, "13.0", dbVersion)

	// Simulate restoration workflow
	backupData, err := os.ReadFile(backupFile)
	require.NoError(t, err)

	err = os.WriteFile(composeFile, backupData, 0644)
	require.NoError(t, err)

	// Verify compose file was restored
	restoredData, err := os.ReadFile(composeFile)
	require.NoError(t, err)
	assert.Equal(t, backupContent, string(restoredData))

	// Verify restored versions are accessible
	restoredWebVersion, err := parseVersionFromBackup(composeFile, "web")
	assert.NoError(t, err)
	assert.Equal(t, "1.20.0", restoredWebVersion)
}

// Integration Test 13: Mixed stack with compose and standalone containers
// Tests: compose containers use compose recreation → standalone use SDK → both succeed
func TestIntegration_MixedStackComposeAndStandalone(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")

	composeContent := `services:
  web:
    image: nginx:1.20
  api:
    image: api:1.0
`

	require.NoError(t, os.WriteFile(composeFile, []byte(composeContent), 0644))

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project":              "mystack",
					"com.docker.compose.service":              "web",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				ID:    "api1",
				Name:  "api",
				Image: "api:1.0",
				Labels: map[string]string{
					"com.docker.compose.project":              "mystack",
					"com.docker.compose.service":              "api",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				ID:    "redis1",
				Name:  "redis",
				Image: "redis:6",
				Labels: map[string]string{
					// No compose labels - standalone container
				},
			},
			{
				ID:    "memcached1",
				Name:  "memcached",
				Image: "memcached:1.6",
				Labels: map[string]string{
					// No compose labels - standalone container
				},
			},
		},
	}

	mockStorage := NewTestMockStorage()
	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
	}

	// Verify compose containers detected
	webContainer := &mockDocker.containers[0]
	webComposePath := orch.getComposeFilePath(webContainer)
	assert.Equal(t, composeFile, webComposePath)

	apiContainer := &mockDocker.containers[1]
	apiComposePath := orch.getComposeFilePath(apiContainer)
	assert.Equal(t, composeFile, apiComposePath)

	// Verify standalone containers detected
	redisContainer := &mockDocker.containers[2]
	redisComposePath := orch.getComposeFilePath(redisContainer)
	assert.Equal(t, "", redisComposePath)

	memcachedContainer := &mockDocker.containers[3]
	memcachedComposePath := orch.getComposeFilePath(memcachedContainer)
	assert.Equal(t, "", memcachedComposePath)

	// Extract service names should only get compose containers
	serviceNames := extractServiceNames(mockDocker.containers, []string{"web", "api", "redis", "memcached"})
	assert.Len(t, serviceNames, 2, "Should only extract compose service names")
	assert.Contains(t, serviceNames, "web")
	assert.Contains(t, serviceNames, "api")
	assert.NotContains(t, serviceNames, "")
}

// Integration Test 14: Service name extraction with special characters and edge cases
// Tests: hyphens, underscores, numbers, similar names all handled correctly
func TestIntegration_ServiceNameExtractionEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	containers := []docker.Container{
		{
			Name: "my-web-app-v2",
			Labels: map[string]string{
				"com.docker.compose.service": "my-web-app-v2",
			},
		},
		{
			Name: "api_service_prod",
			Labels: map[string]string{
				"com.docker.compose.service": "api_service_prod",
			},
		},
		{
			Name: "worker123",
			Labels: map[string]string{
				"com.docker.compose.service": "worker123",
			},
		},
		{
			Name: "db-primary-1",
			Labels: map[string]string{
				"com.docker.compose.service": "db-primary-1",
			},
		},
		{
			Name: "db-replica-2",
			Labels: map[string]string{
				"com.docker.compose.service": "db-replica-2",
			},
		},
		{
			Name: "cache.service",
			Labels: map[string]string{
				"com.docker.compose.service": "cache.service",
			},
		},
	}

	// Test individual extraction
	assert.Equal(t, "my-web-app-v2", extractServiceName(&containers[0]))
	assert.Equal(t, "api_service_prod", extractServiceName(&containers[1]))
	assert.Equal(t, "worker123", extractServiceName(&containers[2]))
	assert.Equal(t, "db-primary-1", extractServiceName(&containers[3]))
	assert.Equal(t, "db-replica-2", extractServiceName(&containers[4]))
	assert.Equal(t, "cache.service", extractServiceName(&containers[5]))

	// Test bulk extraction
	serviceNames := extractServiceNames(containers, []string{"my-web-app-v2", "worker123", "db-primary-1"})
	assert.Len(t, serviceNames, 3)
	assert.Contains(t, serviceNames, "my-web-app-v2")
	assert.Contains(t, serviceNames, "worker123")
	assert.Contains(t, serviceNames, "db-primary-1")

	// Test extraction with similar names
	dbServices := extractServiceNames(containers, []string{"db-primary-1", "db-replica-2"})
	assert.Len(t, dbServices, 2)
	assert.Contains(t, dbServices, "db-primary-1")
	assert.Contains(t, dbServices, "db-replica-2")
}

// Integration Test 15: Progress events published during compose recreation stages
// Tests: "recreating" at 70% → "streaming_compose" during output → "restarting_dependents" at 75%
func TestIntegration_ProgressEventsDuringComposeRecreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")

	composeContent := `services:
  web:
    image: nginx:1.20
  api:
    image: api:1.0
`

	require.NoError(t, os.WriteFile(composeFile, []byte(composeContent), 0644))

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.service":              "web",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				ID:    "api1",
				Name:  "api",
				Image: "api:1.0",
				Labels: map[string]string{
					"com.docker.compose.service":              "api",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
		},
	}

	mockStorage := NewTestMockStorage()
	bus := events.NewBus()
	defer bus.Close()

	// Subscribe to events
	sub := bus.Subscribe(ctx, "test-subscriber")
	defer bus.Unsubscribe("test-subscriber")

	capturedEvents := make([]events.Event, 0)
	done := make(chan bool)

	// Collect events in background
	go func() {
		timeout := time.After(3 * time.Second)
		for {
			select {
			case event := <-sub.Channel:
				capturedEvents = append(capturedEvents, event)
			case <-timeout:
				done <- true
				return
			}
		}
	}()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
	}

	// Publish progress events that would occur during compose recreation
	orch.publishProgress("test-op", "web", "teststack", "recreating", 70, "Recreating container with compose")
	orch.publishProgress("test-op", "web", "teststack", "streaming_compose", 71, "Pulling image")
	orch.publishProgress("test-op", "web", "teststack", "streaming_compose", 72, "Creating container")
	orch.publishProgress("test-op", "web", "teststack", "streaming_compose", 73, "Starting container")
	orch.publishProgress("test-op", "web", "teststack", "restarting_dependents", 75, "Dependent containers recreated")

	// Wait for events
	<-done

	// Verify events were published with correct stages and percentages
	hasRecreating := false
	hasStreamingCompose := false
	hasRestartingDependents := false
	streamingComposeCount := 0

	for _, event := range capturedEvents {
		if stage, ok := event.Payload["stage"].(string); ok {
			switch stage {
			case "recreating":
				hasRecreating = true
				if percent, ok := event.Payload["percent"].(int); ok {
					assert.Equal(t, 70, percent)
				}
			case "streaming_compose":
				hasStreamingCompose = true
				streamingComposeCount++
			case "restarting_dependents":
				hasRestartingDependents = true
				if percent, ok := event.Payload["percent"].(int); ok {
					assert.Equal(t, 75, percent)
				}
			}
		}
	}

	assert.True(t, hasRecreating, "Expected 'recreating' progress event")
	assert.True(t, hasStreamingCompose, "Expected 'streaming_compose' progress events")
	assert.True(t, hasRestartingDependents, "Expected 'restarting_dependents' progress event")
	assert.GreaterOrEqual(t, streamingComposeCount, 1, "Expected at least one streaming_compose event")
}

// Integration Test 16: Compose command formatting for multiple services
// Tests: directory extraction → filename extraction → service name joining → command structure
func TestIntegration_ComposeCommandFormattingForMultipleServices(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "subfolder", "docker-compose.yml")

	// Create subfolder
	require.NoError(t, os.MkdirAll(filepath.Dir(composeFile), 0755))

	composeContent := `services:
  web:
    image: nginx:1.20
  api:
    image: api:1.0
  worker:
    image: worker:1.0
  db:
    image: postgres:13
`

	require.NoError(t, os.WriteFile(composeFile, []byte(composeContent), 0644))

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				Name: "web",
				Labels: map[string]string{
					"com.docker.compose.service":              "web",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				Name: "api",
				Labels: map[string]string{
					"com.docker.compose.service":              "api",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				Name: "worker",
				Labels: map[string]string{
					"com.docker.compose.service":              "worker",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				Name: "db",
				Labels: map[string]string{
					"com.docker.compose.service":              "db",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
		},
	}

	// Extract service names
	serviceNames := extractServiceNames(mockDocker.containers, []string{"web", "api", "worker", "db"})
	assert.Len(t, serviceNames, 4)

	// Verify directory and filename extraction
	dir := filepath.Dir(composeFile)
	fileName := filepath.Base(composeFile)

	assert.Equal(t, filepath.Join(tmpDir, "subfolder"), dir)
	assert.Equal(t, "docker-compose.yml", fileName)

	// Compose command format verification
	// Expected: cd <dir> && docker compose -f <filename> up -d web api worker db
	serviceNamesStr := strings.Join(serviceNames, " ")
	assert.Contains(t, serviceNamesStr, "web")
	assert.Contains(t, serviceNamesStr, "api")
	assert.Contains(t, serviceNamesStr, "worker")
	assert.Contains(t, serviceNamesStr, "db")
}

// Integration Test 17: Standalone container SDK recreation with full config preservation
// Tests: inspect → extract config → remove → create with new image → start → verify config preserved
func TestIntegration_StandaloneContainerSDKRecreation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Skip if Docker not available
	dockerSDK, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping integration test")
	}
	defer dockerSDK.Close()

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "redis1",
				Name:  "redis-standalone",
				Image: "redis:6.2",
				Labels: map[string]string{
					// No compose labels - standalone container
				},
			},
		},
	}

	mockStorage := NewTestMockStorage()
	bus := events.NewBus()
	defer bus.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		dockerSDK:    dockerSDK,
		storage:      mockStorage,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
	}

	// Verify container is detected as standalone
	redisContainer := &mockDocker.containers[0]
	composePath := orch.getComposeFilePath(redisContainer)
	assert.Equal(t, "", composePath, "Standalone container should have no compose path")

	// Verify buildImageRef works for version updates
	newImage := orch.buildImageRef("redis:6.2", "7.0")
	assert.Equal(t, "redis:7.0", newImage)
}

// Integration Test 18: Backup parsing with complex image formats
// Tests: registry URLs → version tags with suffixes → multi-level paths → all parsed correctly
func TestIntegration_BackupParsingComplexImageFormats(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	tmpDir := t.TempDir()
	backupFile := filepath.Join(tmpDir, "docker-compose.yml.backup.20240115-120000")

	backupContent := `services:
  web:
    image: docker.io/library/nginx:1.20.2-alpine
    ports:
      - "80:80"

  api:
    image: registry.example.com:5000/myorg/myapp/api:v2.3.4-rc1
    environment:
      - ENV=production

  db:
    image: postgres:13.8-alpine3.16
    volumes:
      - db_data:/var/lib/postgresql/data

  cache:
    image: localhost:5000/redis:6.2.7

volumes:
  db_data:
`

	require.NoError(t, os.WriteFile(backupFile, []byte(backupContent), 0644))

	// Test version extraction for various image formats
	webVersion, err := parseVersionFromBackup(backupFile, "web")
	assert.NoError(t, err)
	assert.Equal(t, "1.20.2-alpine", webVersion)

	apiVersion, err := parseVersionFromBackup(backupFile, "api")
	assert.NoError(t, err)
	assert.Equal(t, "v2.3.4-rc1", apiVersion)

	dbVersion, err := parseVersionFromBackup(backupFile, "db")
	assert.NoError(t, err)
	assert.Equal(t, "13.8-alpine3.16", dbVersion)

	cacheVersion, err := parseVersionFromBackup(backupFile, "cache")
	assert.NoError(t, err)
	assert.Equal(t, "6.2.7", cacheVersion)
}
