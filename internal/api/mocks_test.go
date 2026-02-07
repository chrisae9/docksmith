package api

import (
	"context"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/update"
)

// MockStorage implements storage.Storage for testing handlers
type MockStorage struct {
	mu sync.RWMutex

	// Storage data
	operations        []storage.UpdateOperation
	checkHistory      []storage.CheckHistoryEntry
	updateLog         []storage.UpdateLogEntry
	policies          map[string]storage.RollbackPolicy
	configs           map[string]string
	scriptAssignments map[string]storage.ScriptAssignment

	// Error injection
	GetError  error
	SaveError error
}

// NewMockStorage creates a new mock storage instance
func NewMockStorage() *MockStorage {
	return &MockStorage{
		operations:        make([]storage.UpdateOperation, 0),
		checkHistory:      make([]storage.CheckHistoryEntry, 0),
		updateLog:         make([]storage.UpdateLogEntry, 0),
		policies:          make(map[string]storage.RollbackPolicy),
		configs:           make(map[string]string),
		scriptAssignments: make(map[string]storage.ScriptAssignment),
	}
}

// AddOperation adds a test operation to the mock
func (m *MockStorage) AddOperation(op storage.UpdateOperation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.operations = append(m.operations, op)
}

// AddCheckHistory adds a test check history entry
func (m *MockStorage) AddCheckHistory(entry storage.CheckHistoryEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkHistory = append(m.checkHistory, entry)
}

// AddUpdateLog adds a test update log entry
func (m *MockStorage) AddUpdateLog(entry storage.UpdateLogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateLog = append(m.updateLog, entry)
}

// Implement required Storage interface methods

func (m *MockStorage) SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error {
	return m.SaveError
}

func (m *MockStorage) GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (string, bool, error) {
	return "", false, m.GetError
}

func (m *MockStorage) LogCheck(ctx context.Context, containerName, image, currentVer, latestVer, status string, checkErr error) error {
	return m.SaveError
}

func (m *MockStorage) LogCheckBatch(ctx context.Context, checks []storage.CheckHistoryEntry) error {
	return m.SaveError
}

func (m *MockStorage) GetCheckHistory(ctx context.Context, containerName string, limit int) ([]storage.CheckHistoryEntry, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []storage.CheckHistoryEntry
	for _, h := range m.checkHistory {
		if h.ContainerName == containerName {
			result = append(result, h)
		}
	}
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (m *MockStorage) GetAllCheckHistory(ctx context.Context, limit int) ([]storage.CheckHistoryEntry, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit > 0 && len(m.checkHistory) > limit {
		return m.checkHistory[:limit], nil
	}
	return m.checkHistory, nil
}

func (m *MockStorage) GetCheckHistorySince(ctx context.Context, since time.Time) ([]storage.CheckHistoryEntry, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	return m.checkHistory, nil
}

func (m *MockStorage) GetCheckHistoryByTimeRange(ctx context.Context, start, end time.Time) ([]storage.CheckHistoryEntry, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	return m.checkHistory, nil
}

func (m *MockStorage) LogUpdate(ctx context.Context, containerName, operation, fromVer, toVer string, success bool, updateErr error) error {
	return m.SaveError
}

func (m *MockStorage) GetUpdateLog(ctx context.Context, containerName string, limit int) ([]storage.UpdateLogEntry, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []storage.UpdateLogEntry
	for _, l := range m.updateLog {
		if l.ContainerName == containerName {
			result = append(result, l)
		}
	}
	return result, nil
}

func (m *MockStorage) GetAllUpdateLog(ctx context.Context, limit int) ([]storage.UpdateLogEntry, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit > 0 && len(m.updateLog) > limit {
		return m.updateLog[:limit], nil
	}
	return m.updateLog, nil
}

func (m *MockStorage) GetConfig(ctx context.Context, key string) (string, bool, error) {
	if m.GetError != nil {
		return "", false, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	val, ok := m.configs[key]
	return val, ok, nil
}

func (m *MockStorage) SetConfig(ctx context.Context, key, value string) error {
	if m.SaveError != nil {
		return m.SaveError
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.configs[key] = value
	return nil
}

func (m *MockStorage) SaveConfigSnapshot(ctx context.Context, snapshot storage.ConfigSnapshot) error {
	return m.SaveError
}

func (m *MockStorage) GetConfigHistory(ctx context.Context, limit int) ([]storage.ConfigSnapshot, error) {
	return nil, m.GetError
}

func (m *MockStorage) GetConfigSnapshotByID(ctx context.Context, snapshotID int64) (storage.ConfigSnapshot, bool, error) {
	return storage.ConfigSnapshot{}, false, m.GetError
}

func (m *MockStorage) RevertToSnapshot(ctx context.Context, snapshotID int64) error {
	return m.SaveError
}

func (m *MockStorage) SaveUpdateOperation(ctx context.Context, op storage.UpdateOperation) error {
	if m.SaveError != nil {
		return m.SaveError
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	// Update existing or add new
	for i, existing := range m.operations {
		if existing.OperationID == op.OperationID {
			m.operations[i] = op
			return nil
		}
	}
	m.operations = append(m.operations, op)
	return nil
}

func (m *MockStorage) GetUpdateOperation(ctx context.Context, operationID string) (storage.UpdateOperation, bool, error) {
	if m.GetError != nil {
		return storage.UpdateOperation{}, false, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, op := range m.operations {
		if op.OperationID == operationID {
			return op, true, nil
		}
	}
	return storage.UpdateOperation{}, false, nil
}

func (m *MockStorage) GetUpdateOperations(ctx context.Context, limit int) ([]storage.UpdateOperation, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit > 0 && len(m.operations) > limit {
		return m.operations[:limit], nil
	}
	return m.operations, nil
}

func (m *MockStorage) GetUpdateOperationsByContainer(ctx context.Context, containerName string, limit int) ([]storage.UpdateOperation, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []storage.UpdateOperation
	for _, op := range m.operations {
		if op.ContainerName == containerName {
			result = append(result, op)
		}
	}
	return result, nil
}

func (m *MockStorage) GetUpdateOperationsByTimeRange(ctx context.Context, start, end time.Time) ([]storage.UpdateOperation, error) {
	return m.operations, m.GetError
}

func (m *MockStorage) GetUpdateOperationsByBatchGroup(ctx context.Context, batchGroupID string) ([]storage.UpdateOperation, error) {
	return m.operations, m.GetError
}

func (m *MockStorage) GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]storage.UpdateOperation, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []storage.UpdateOperation
	for _, op := range m.operations {
		if op.Status == status {
			result = append(result, op)
		}
	}
	return result, nil
}

func (m *MockStorage) UpdateOperationStatus(ctx context.Context, operationID string, status string, errorMsg string) error {
	if m.SaveError != nil {
		return m.SaveError
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, op := range m.operations {
		if op.OperationID == operationID {
			m.operations[i].Status = status
			m.operations[i].ErrorMessage = errorMsg
			return nil
		}
	}
	return nil
}

func (m *MockStorage) GetRollbackPolicy(ctx context.Context, entityType, entityID string) (storage.RollbackPolicy, bool, error) {
	if m.GetError != nil {
		return storage.RollbackPolicy{}, false, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := entityType + ":" + entityID
	policy, ok := m.policies[key]
	return policy, ok, nil
}

func (m *MockStorage) SetRollbackPolicy(ctx context.Context, policy storage.RollbackPolicy) error {
	if m.SaveError != nil {
		return m.SaveError
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	key := policy.EntityType + ":" + policy.EntityID
	m.policies[key] = policy
	return nil
}

func (m *MockStorage) QueueUpdate(ctx context.Context, queue storage.UpdateQueue) error {
	return m.SaveError
}

func (m *MockStorage) DequeueUpdate(ctx context.Context, stackName string) (storage.UpdateQueue, bool, error) {
	return storage.UpdateQueue{}, false, m.GetError
}

func (m *MockStorage) GetQueuedUpdates(ctx context.Context) ([]storage.UpdateQueue, error) {
	return nil, m.GetError
}

func (m *MockStorage) SaveScriptAssignment(ctx context.Context, assignment storage.ScriptAssignment) error {
	if m.SaveError != nil {
		return m.SaveError
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.scriptAssignments[assignment.ContainerName] = assignment
	return nil
}

func (m *MockStorage) GetScriptAssignment(ctx context.Context, containerName string) (storage.ScriptAssignment, bool, error) {
	if m.GetError != nil {
		return storage.ScriptAssignment{}, false, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	assignment, ok := m.scriptAssignments[containerName]
	return assignment, ok, nil
}

func (m *MockStorage) ListScriptAssignments(ctx context.Context, enabledOnly bool) ([]storage.ScriptAssignment, error) {
	if m.GetError != nil {
		return nil, m.GetError
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []storage.ScriptAssignment
	for _, a := range m.scriptAssignments {
		if !enabledOnly || a.Enabled {
			result = append(result, a)
		}
	}
	return result, nil
}

func (m *MockStorage) DeleteScriptAssignment(ctx context.Context, containerName string) error {
	if m.SaveError != nil {
		return m.SaveError
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.scriptAssignments, containerName)
	return nil
}

func (m *MockStorage) Close() error {
	return nil
}

// MockBackgroundChecker simulates the background checker for testing
type MockBackgroundChecker struct {
	mu           sync.RWMutex
	cachedResult *update.DiscoveryResult
	checking     bool
	lastCheck    time.Time
	triggerCount int
}

// NewMockBackgroundChecker creates a new mock background checker
func NewMockBackgroundChecker() *MockBackgroundChecker {
	return &MockBackgroundChecker{}
}

// SetCachedResult sets the cached result for testing
func (m *MockBackgroundChecker) SetCachedResult(result *update.DiscoveryResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cachedResult = result
}

// SetChecking sets the checking state for testing
func (m *MockBackgroundChecker) SetChecking(checking bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checking = checking
}

// GetCachedResults returns the cached results (implements BackgroundChecker interface)
func (m *MockBackgroundChecker) GetCachedResults() *update.DiscoveryResult {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cachedResult
}

// IsChecking returns whether a check is in progress
func (m *MockBackgroundChecker) IsChecking() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.checking
}

// TriggerCheck triggers a background check
func (m *MockBackgroundChecker) TriggerCheck() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggerCount++
	m.lastCheck = time.Now()
}

// GetTriggerCount returns how many times TriggerCheck was called
func (m *MockBackgroundChecker) GetTriggerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.triggerCount
}

// GetLastCacheRefresh returns when cache was last refreshed
func (m *MockBackgroundChecker) GetLastCacheRefresh() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCheck
}

// GetLastBackgroundRun returns when background check last ran
func (m *MockBackgroundChecker) GetLastBackgroundRun() time.Time {
	return m.GetLastCacheRefresh()
}
