package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/storage"
	"github.com/stretchr/testify/assert"
)

// TestMockStorage is a mock storage implementation for orchestrator testing.
type TestMockStorage struct {
	mu         sync.RWMutex
	operations map[string]storage.UpdateOperation
	policies   map[string]storage.RollbackPolicy
	queue      []storage.UpdateQueue
}

func NewTestMockStorage() *TestMockStorage {
	return &TestMockStorage{
		operations: make(map[string]storage.UpdateOperation),
		policies:   make(map[string]storage.RollbackPolicy),
		queue:      make([]storage.UpdateQueue, 0),
	}
}

func (m *TestMockStorage) SaveUpdateOperation(ctx context.Context, op storage.UpdateOperation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operations[op.OperationID] = op
	return nil
}

func (m *TestMockStorage) GetUpdateOperation(ctx context.Context, operationID string) (storage.UpdateOperation, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	op, found := m.operations[operationID]
	return op, found, nil
}

func (m *TestMockStorage) GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]storage.UpdateOperation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ops := make([]storage.UpdateOperation, 0)
	for _, op := range m.operations {
		if op.Status == status {
			ops = append(ops, op)
		}
	}
	return ops, nil
}

func (m *TestMockStorage) GetUpdateOperations(ctx context.Context, limit int) ([]storage.UpdateOperation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ops := make([]storage.UpdateOperation, 0, len(m.operations))
	for _, op := range m.operations {
		ops = append(ops, op)
	}
	return ops, nil
}

func (m *TestMockStorage) GetUpdateOperationsByContainer(ctx context.Context, containerName string, limit int) ([]storage.UpdateOperation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ops := make([]storage.UpdateOperation, 0)
	for _, op := range m.operations {
		if op.ContainerName == containerName {
			ops = append(ops, op)
		}
	}
	return ops, nil
}

func (m *TestMockStorage) GetUpdateOperationsByTimeRange(ctx context.Context, start, end time.Time) ([]storage.UpdateOperation, error) {
	return nil, nil
}

func (m *TestMockStorage) GetUpdateOperationsByBatchGroup(ctx context.Context, batchGroupID string) ([]storage.UpdateOperation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var ops []storage.UpdateOperation
	for _, op := range m.operations {
		if op.BatchGroupID == batchGroupID {
			ops = append(ops, op)
		}
	}
	return ops, nil
}

func (m *TestMockStorage) UpdateOperationStatus(ctx context.Context, operationID string, status string, errorMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if op, found := m.operations[operationID]; found {
		op.Status = status
		op.ErrorMessage = errorMsg
		m.operations[operationID] = op
	}
	return nil
}

func (m *TestMockStorage) GetRollbackPolicy(ctx context.Context, entityType, entityID string) (storage.RollbackPolicy, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	key := entityType + ":" + entityID
	policy, found := m.policies[key]
	return policy, found, nil
}

func (m *TestMockStorage) SetRollbackPolicy(ctx context.Context, policy storage.RollbackPolicy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := policy.EntityType + ":" + policy.EntityID
	m.policies[key] = policy
	return nil
}

func (m *TestMockStorage) QueueUpdate(ctx context.Context, queue storage.UpdateQueue) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queue = append(m.queue, queue)
	return nil
}

func (m *TestMockStorage) DequeueUpdate(ctx context.Context, stackName string) (storage.UpdateQueue, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, q := range m.queue {
		if q.StackName == stackName {
			m.queue = append(m.queue[:i], m.queue[i+1:]...)
			return q, true, nil
		}
	}
	return storage.UpdateQueue{}, false, nil
}

func (m *TestMockStorage) GetQueuedUpdates(ctx context.Context) ([]storage.UpdateQueue, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.queue, nil
}

func (m *TestMockStorage) SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error {
	return nil
}

func (m *TestMockStorage) GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (string, bool, error) {
	return "", false, nil
}

func (m *TestMockStorage) LogCheck(ctx context.Context, containerName, image, currentVer, latestVer, status string, checkErr error) error {
	return nil
}

func (m *TestMockStorage) LogCheckBatch(ctx context.Context, checks []storage.CheckHistoryEntry) error {
	return nil
}

func (m *TestMockStorage) GetCheckHistory(ctx context.Context, containerName string, limit int) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}

func (m *TestMockStorage) GetCheckHistoryByTimeRange(ctx context.Context, start, end time.Time) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}

func (m *TestMockStorage) GetAllCheckHistory(ctx context.Context, limit int) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}

func (m *TestMockStorage) GetCheckHistorySince(ctx context.Context, since time.Time) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}

func (m *TestMockStorage) LogUpdate(ctx context.Context, containerName, operation, fromVer, toVer string, success bool, updateErr error) error {
	return nil
}

func (m *TestMockStorage) GetUpdateLog(ctx context.Context, containerName string, limit int) ([]storage.UpdateLogEntry, error) {
	return nil, nil
}

func (m *TestMockStorage) GetAllUpdateLog(ctx context.Context, limit int) ([]storage.UpdateLogEntry, error) {
	return nil, nil
}

func (m *TestMockStorage) GetConfig(ctx context.Context, key string) (string, bool, error) {
	return "", false, nil
}

func (m *TestMockStorage) SetConfig(ctx context.Context, key, value string) error {
	return nil
}

func (m *TestMockStorage) SaveConfigSnapshot(ctx context.Context, snapshot storage.ConfigSnapshot) error {
	return nil
}

func (m *TestMockStorage) GetConfigHistory(ctx context.Context, limit int) ([]storage.ConfigSnapshot, error) {
	return nil, nil
}

func (m *TestMockStorage) GetConfigSnapshotByID(ctx context.Context, snapshotID int64) (storage.ConfigSnapshot, bool, error) {
	return storage.ConfigSnapshot{}, false, nil
}

func (m *TestMockStorage) RevertToSnapshot(ctx context.Context, snapshotID int64) error {
	return nil
}

func (m *TestMockStorage) SaveScriptAssignment(ctx context.Context, assignment storage.ScriptAssignment) error {
	return nil
}

func (m *TestMockStorage) GetScriptAssignment(ctx context.Context, containerName string) (storage.ScriptAssignment, bool, error) {
	return storage.ScriptAssignment{}, false, nil
}

func (m *TestMockStorage) ListScriptAssignments(ctx context.Context, enabledOnly bool) ([]storage.ScriptAssignment, error) {
	return nil, nil
}

func (m *TestMockStorage) DeleteScriptAssignment(ctx context.Context, containerName string) error {
	return nil
}

func (m *TestMockStorage) Close() error {
	return nil
}

// Test: Single container update happy path
func TestUpdateSingleContainer_HappyPath(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "container1",
				Name:  "test-container",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project": "test-stack",
				},
			},
		},
	}
	mockStorage := NewTestMockStorage()
	bus := events.NewBus()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		eventBus:     bus,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*stackLockEntry),
	}

	operationID, err := orch.UpdateSingleContainer(context.Background(), "test-container", "1.21")

	assert.NoError(t, err)
	assert.NotEmpty(t, operationID)

	time.Sleep(100 * time.Millisecond)

	op, found, _ := mockStorage.GetUpdateOperation(context.Background(), operationID)
	assert.True(t, found)
	assert.Equal(t, "test-container", op.ContainerName)
	assert.Equal(t, "test-stack", op.StackName)
}

// Test: Batch update with dependency ordering
func TestUpdateBatchContainers_DependencyOrdering(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "app1",
				Name:  "app",
				Image: "app:1.0",
				Labels: map[string]string{
					"com.docker.compose.project":    "mystack",
					"com.docker.compose.depends_on": "db",
				},
			},
			{
				ID:    "db1",
				Name:  "db",
				Image: "postgres:13",
				Labels: map[string]string{
					"com.docker.compose.project": "mystack",
				},
			},
		},
	}
	mockStorage := NewTestMockStorage()
	bus := events.NewBus()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*stackLockEntry),
	}

	operationID, err := orch.UpdateBatchContainers(context.Background(), []string{"app", "db"}, nil)

	assert.NoError(t, err)
	assert.NotEmpty(t, operationID)

	op, found, _ := mockStorage.GetUpdateOperation(context.Background(), operationID)
	assert.True(t, found)
	assert.Equal(t, "batch", op.OperationType)
}

// Test: Stack-level update
func TestUpdateStack(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project": "mystack",
				},
			},
			{
				ID:    "api1",
				Name:  "api",
				Image: "api:1.0",
				Labels: map[string]string{
					"com.docker.compose.project": "mystack",
				},
			},
		},
	}
	mockStorage := NewTestMockStorage()
	bus := events.NewBus()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*stackLockEntry),
	}

	operationID, err := orch.UpdateStack(context.Background(), "mystack")

	assert.NoError(t, err)
	assert.NotEmpty(t, operationID)

	op, found, _ := mockStorage.GetUpdateOperation(context.Background(), operationID)
	assert.True(t, found)
	assert.Equal(t, "stack", op.OperationType)
	assert.Equal(t, "mystack", op.StackName)
}

// Test: Permission pre-check failure
func TestCheckPermissions_FailsWithoutAccess(t *testing.T) {
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")

	os.WriteFile(composeFile, []byte("services:\n  test:\n    image: nginx"), 0444)
	os.Chmod(tmpDir, 0444)

	container := &docker.Container{
		Name: "test",
		Labels: map[string]string{
			"com.docker.compose.project.config_files": composeFile,
		},
	}

	orch := &UpdateOrchestrator{}

	err := orch.checkPermissions(context.Background(), container)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "permission")

	os.Chmod(tmpDir, 0755)
}

// Test: Compose file update
func TestUpdateComposeFile(t *testing.T) {
	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	composeContent := `services:
  web:
    image: nginx:1.20
`

	os.WriteFile(composeFile, []byte(composeContent), 0644)

	orch := &UpdateOrchestrator{}

	container := &docker.Container{
		Name: "web",
		Labels: map[string]string{
			"com.docker.compose.service": "web",
		},
	}

	err := orch.updateComposeFile(context.Background(), composeFile, container, "1.21")

	assert.NoError(t, err)

	data, _ := os.ReadFile(composeFile)
	assert.Contains(t, string(data), "nginx:1.21")
}

// Test: updateComposeFile correctly handles env var image syntax without corruption.
// This was the exact bug: ${OPENCLAW_IMAGE:-openclaw:latest} was split by ":" naively,
// destroying the closing "}" and producing invalid interpolation syntax.
func TestUpdateComposeFile_EnvVarImage(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		newTag   string
		expected string
	}{
		{
			name:     "env var with colon-dash default",
			image:    "${OPENCLAW_IMAGE:-openclaw:latest}",
			newTag:   "2026.2.6",
			expected: "${OPENCLAW_IMAGE:-openclaw:2026.2.6}",
		},
		{
			name:     "env var with dash default",
			image:    "${IMG-nginx:1.25}",
			newTag:   "1.26",
			expected: "${IMG-nginx:1.26}",
		},
		{
			name:     "env var with registry port in default",
			image:    "${IMG:-registry:5000/myapp:v1}",
			newTag:   "v2",
			expected: "${IMG:-registry:5000/myapp:v2}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			composeFile := filepath.Join(tmpDir, "docker-compose.yml")
			composeContent := fmt.Sprintf("services:\n  gateway:\n    image: %s\n", tt.image)
			os.WriteFile(composeFile, []byte(composeContent), 0644)

			orch := &UpdateOrchestrator{}
			container := &docker.Container{
				Name: "gateway",
				Labels: map[string]string{
					"com.docker.compose.service": "gateway",
				},
			}

			err := orch.updateComposeFile(context.Background(), composeFile, container, tt.newTag)
			assert.NoError(t, err)

			data, _ := os.ReadFile(composeFile)
			content := string(data)
			assert.Contains(t, content, tt.expected,
				"compose file should contain properly updated env var, got: %s", content)
			// Verify no corruption: closing brace must be present
			assert.NotContains(t, content, "${"+tt.image[2:len(tt.image)-1],
				"original image should not remain unchanged")
		})
	}
}

// Test: Auto-rollback policy resolution
func TestShouldAutoRollback_PolicyResolution(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				Name: "test-container",
				Labels: map[string]string{
					"docksmith.auto_rollback": "true",
				},
			},
		},
	}
	mockStorage := NewTestMockStorage()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		stackManager: docker.NewStackManager(),
	}

	shouldRollback, err := orch.shouldAutoRollback(context.Background(), "test-container")

	assert.NoError(t, err)
	assert.True(t, shouldRollback)
}

// Test: Queue operation when stack is locked
func TestQueueOperation_WhenStackLocked(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				Name: "test-container",
				Labels: map[string]string{
					"com.docker.compose.project": "test-stack",
				},
			},
		},
	}
	mockStorage := NewTestMockStorage()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*stackLockEntry),
	}

	orch.acquireStackLock("test-stack")

	operationID, err := orch.UpdateSingleContainer(context.Background(), "test-container", "latest")

	assert.NoError(t, err)
	assert.NotEmpty(t, operationID)

	op, found, _ := mockStorage.GetUpdateOperation(context.Background(), operationID)
	assert.True(t, found)
	assert.Equal(t, "queued", op.Status)

	queued, _ := mockStorage.GetQueuedUpdates(context.Background())
	assert.Len(t, queued, 1)
}

// Test: hasNetworkModeDependency correctly identifies network_mode dependencies
func TestHasNetworkModeDependency(t *testing.T) {
	orch := &UpdateOrchestrator{}

	tests := []struct {
		name       string
		container  *docker.Container
		batchNames map[string]bool
		hasDep     bool
		depName    string
	}{
		{
			name: "container with network_mode: service:tailscale pointing to batch container",
			container: &docker.Container{
				Name: "traefik-ts",
				Labels: map[string]string{
					graph.NetworkModeLabel: "service:tailscale",
				},
			},
			batchNames: map[string]bool{"tailscale": true, "traefik-ts": true},
			hasDep:     true,
			depName:    "tailscale",
		},
		{
			name: "container with network_mode but dependency not in batch",
			container: &docker.Container{
				Name: "some-container",
				Labels: map[string]string{
					graph.NetworkModeLabel: "service:vpn",
				},
			},
			batchNames: map[string]bool{"some-container": true, "other": true},
			hasDep:     false,
			depName:    "",
		},
		{
			name: "container without network_mode label",
			container: &docker.Container{
				Name:   "regular-container",
				Labels: map[string]string{},
			},
			batchNames: map[string]bool{"regular-container": true},
			hasDep:     false,
			depName:    "",
		},
		{
			name: "container with network_mode: container:xxx (not service)",
			container: &docker.Container{
				Name: "container-mode",
				Labels: map[string]string{
					graph.NetworkModeLabel: "container:some-container",
				},
			},
			batchNames: map[string]bool{"container-mode": true, "some-container": true},
			hasDep:     false,
			depName:    "",
		},
		{
			name: "container with network_mode: host (not service)",
			container: &docker.Container{
				Name: "host-mode",
				Labels: map[string]string{
					graph.NetworkModeLabel: "host",
				},
			},
			batchNames: map[string]bool{"host-mode": true},
			hasDep:     false,
			depName:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hasDep, depName := orch.hasNetworkModeDependency(tt.container, tt.batchNames)
			assert.Equal(t, tt.hasDep, hasDep, "hasDep mismatch")
			assert.Equal(t, tt.depName, depName, "depName mismatch")
		})
	}
}

// Test: Batch update with network_mode containers uses correct recreation method
func TestBatchUpdateWithNetworkModeDependency(t *testing.T) {
	// Test that containers with network_mode: service:xxx are identified
	// and would be handled by compose recreation instead of SDK
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "tailscale-id",
				Name:  "tailscale",
				Image: "tailscale/tailscale:v1.60",
				Labels: map[string]string{
					"com.docker.compose.project": "traefik",
					"com.docker.compose.service": "tailscale",
				},
			},
			{
				ID:    "traefik-ts-id",
				Name:  "traefik-ts",
				Image: "traefik:v3.0",
				Labels: map[string]string{
					"com.docker.compose.project": "traefik",
					"com.docker.compose.service": "traefik-ts",
					graph.NetworkModeLabel:       "service:tailscale", // Key dependency
				},
			},
		},
	}
	mockStorage := NewTestMockStorage()
	bus := events.NewBus()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		storage:      mockStorage,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
		stackLocks:   make(map[string]*stackLockEntry),
	}

	// Build batch names map as the orchestrator would
	batchNames := map[string]bool{
		"tailscale":  true,
		"traefik-ts": true,
	}

	// Test that network_mode dependency is detected
	for _, cont := range mockDocker.containers {
		hasDep, depName := orch.hasNetworkModeDependency(&cont, batchNames)
		if cont.Name == "traefik-ts" {
			assert.True(t, hasDep, "traefik-ts should have network_mode dependency")
			assert.Equal(t, "tailscale", depName, "traefik-ts should depend on tailscale")
		} else {
			assert.False(t, hasDep, "tailscale should not have network_mode dependency")
		}
	}

	// Test the dependency graph ordering
	depGraph := orch.graphBuilder.BuildFromContainers(mockDocker.containers)
	updateOrder, err := depGraph.GetUpdateOrder()
	assert.NoError(t, err)

	// Find positions
	tailscaleIdx := -1
	traefikTsIdx := -1
	for i, name := range updateOrder {
		if name == "tailscale" {
			tailscaleIdx = i
		}
		if name == "traefik-ts" {
			traefikTsIdx = i
		}
	}

	assert.Greater(t, traefikTsIdx, tailscaleIdx, "tailscale should be updated before traefik-ts due to network_mode dependency")
}
