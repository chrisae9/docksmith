package update

import (
	"context"
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
		stackLocks:   make(map[string]*sync.Mutex),
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
		stackLocks:   make(map[string]*sync.Mutex),
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
		stackLocks:   make(map[string]*sync.Mutex),
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
		stackLocks:   make(map[string]*sync.Mutex),
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
