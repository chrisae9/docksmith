package scripts

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/storage"
)

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	assignments map[string]storage.ScriptAssignment
	saveErr     error
	getErr      error
	deleteErr   error
	listErr     error
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		assignments: make(map[string]storage.ScriptAssignment),
	}
}

// Script assignment methods (used by Manager)
func (m *mockStorage) SaveScriptAssignment(ctx context.Context, assignment storage.ScriptAssignment) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.assignments[assignment.ContainerName] = assignment
	return nil
}

func (m *mockStorage) GetScriptAssignment(ctx context.Context, containerName string) (storage.ScriptAssignment, bool, error) {
	if m.getErr != nil {
		return storage.ScriptAssignment{}, false, m.getErr
	}
	assignment, found := m.assignments[containerName]
	return assignment, found, nil
}

func (m *mockStorage) DeleteScriptAssignment(ctx context.Context, containerName string) error {
	if m.deleteErr != nil {
		return m.deleteErr
	}
	delete(m.assignments, containerName)
	return nil
}

func (m *mockStorage) ListScriptAssignments(ctx context.Context, enabledOnly bool) ([]storage.ScriptAssignment, error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var result []storage.ScriptAssignment
	for _, a := range m.assignments {
		if !enabledOnly || a.Enabled {
			result = append(result, a)
		}
	}
	return result, nil
}

// Rest of Storage interface (not used by scripts.Manager, return empty/nil)
func (m *mockStorage) Close() error { return nil }

func (m *mockStorage) SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error {
	return nil
}
func (m *mockStorage) GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (string, bool, error) {
	return "", false, nil
}

func (m *mockStorage) LogCheck(ctx context.Context, containerName, image, currentVer, latestVer, status string, checkErr error) error {
	return nil
}
func (m *mockStorage) LogCheckBatch(ctx context.Context, checks []storage.CheckHistoryEntry) error {
	return nil
}
func (m *mockStorage) GetCheckHistory(ctx context.Context, containerName string, limit int) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}
func (m *mockStorage) GetAllCheckHistory(ctx context.Context, limit int) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}
func (m *mockStorage) GetCheckHistorySince(ctx context.Context, since time.Time) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}
func (m *mockStorage) GetCheckHistoryByTimeRange(ctx context.Context, start, end time.Time) ([]storage.CheckHistoryEntry, error) {
	return nil, nil
}

func (m *mockStorage) LogUpdate(ctx context.Context, containerName, operation, fromVer, toVer string, success bool, updateErr error) error {
	return nil
}
func (m *mockStorage) GetUpdateLog(ctx context.Context, containerName string, limit int) ([]storage.UpdateLogEntry, error) {
	return nil, nil
}
func (m *mockStorage) GetAllUpdateLog(ctx context.Context, limit int) ([]storage.UpdateLogEntry, error) {
	return nil, nil
}

func (m *mockStorage) GetConfig(ctx context.Context, key string) (string, bool, error) {
	return "", false, nil
}
func (m *mockStorage) SetConfig(ctx context.Context, key, value string) error { return nil }
func (m *mockStorage) SaveConfigSnapshot(ctx context.Context, snapshot storage.ConfigSnapshot) error {
	return nil
}
func (m *mockStorage) GetConfigHistory(ctx context.Context, limit int) ([]storage.ConfigSnapshot, error) {
	return nil, nil
}
func (m *mockStorage) GetConfigSnapshotByID(ctx context.Context, snapshotID int64) (storage.ConfigSnapshot, bool, error) {
	return storage.ConfigSnapshot{}, false, nil
}
func (m *mockStorage) RevertToSnapshot(ctx context.Context, snapshotID int64) error { return nil }

func (m *mockStorage) SaveUpdateOperation(ctx context.Context, op storage.UpdateOperation) error {
	return nil
}
func (m *mockStorage) GetUpdateOperation(ctx context.Context, operationID string) (storage.UpdateOperation, bool, error) {
	return storage.UpdateOperation{}, false, nil
}
func (m *mockStorage) GetUpdateOperations(ctx context.Context, limit int) ([]storage.UpdateOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUpdateOperationsByContainer(ctx context.Context, containerName string, limit int) ([]storage.UpdateOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUpdateOperationsByTimeRange(ctx context.Context, start, end time.Time) ([]storage.UpdateOperation, error) {
	return nil, nil
}
func (m *mockStorage) GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]storage.UpdateOperation, error) {
	return nil, nil
}
func (m *mockStorage) UpdateOperationStatus(ctx context.Context, operationID, status, errorMsg string) error {
	return nil
}

func (m *mockStorage) SaveComposeBackup(ctx context.Context, backup storage.ComposeBackup) error {
	return nil
}
func (m *mockStorage) GetComposeBackup(ctx context.Context, operationID string) (storage.ComposeBackup, bool, error) {
	return storage.ComposeBackup{}, false, nil
}
func (m *mockStorage) GetComposeBackupsByContainer(ctx context.Context, containerName string) ([]storage.ComposeBackup, error) {
	return nil, nil
}
func (m *mockStorage) GetAllComposeBackups(ctx context.Context, limit int) ([]storage.ComposeBackup, error) {
	return nil, nil
}

func (m *mockStorage) GetRollbackPolicy(ctx context.Context, entityType, entityID string) (storage.RollbackPolicy, bool, error) {
	return storage.RollbackPolicy{}, false, nil
}
func (m *mockStorage) SetRollbackPolicy(ctx context.Context, policy storage.RollbackPolicy) error {
	return nil
}

func (m *mockStorage) QueueUpdate(ctx context.Context, queue storage.UpdateQueue) error { return nil }
func (m *mockStorage) DequeueUpdate(ctx context.Context, stackName string) (storage.UpdateQueue, bool, error) {
	return storage.UpdateQueue{}, false, nil
}
func (m *mockStorage) GetQueuedUpdates(ctx context.Context) ([]storage.UpdateQueue, error) {
	return nil, nil
}

// TestNewManager tests the Manager constructor
func TestNewManager(t *testing.T) {
	mockStore := newMockStorage()
	manager := NewManager(mockStore, nil)

	assert.NotNil(t, manager)
	assert.Equal(t, mockStore, manager.storage)
}

// TestDiscoverScripts tests script discovery
func TestDiscoverScripts(t *testing.T) {
	t.Run("returns empty for non-existent directory", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		// The actual behavior depends on whether /scripts exists
		scripts, err := manager.DiscoverScripts()
		require.NoError(t, err)
		assert.NotNil(t, scripts)
	})
}

// TestValidateScript tests script validation
func TestValidateScript(t *testing.T) {
	mockStore := newMockStorage()
	manager := NewManager(mockStore, nil)

	t.Run("returns error for non-existent script", func(t *testing.T) {
		err := manager.ValidateScript("nonexistent-script.sh")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "script not found")
	})
}

// TestAssignScript tests script assignment
func TestAssignScript(t *testing.T) {
	ctx := context.Background()

	t.Run("assigns script successfully (empty path)", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.AssignScript(ctx, "test-container", "", "test")
		require.NoError(t, err)

		assignment, found := mockStore.assignments["test-container"]
		assert.True(t, found)
		assert.Equal(t, "test-container", assignment.ContainerName)
		assert.Equal(t, "", assignment.ScriptPath)
		assert.True(t, assignment.Enabled)
	})

	t.Run("returns error for non-existent script", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.AssignScript(ctx, "test-container", "nonexistent.sh", "test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "script validation failed")
	})

	t.Run("preserves existing settings", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetIgnore(ctx, "test-container", true, "test")
		require.NoError(t, err)

		err = manager.AssignScript(ctx, "test-container", "", "test")
		require.NoError(t, err)

		assignment, found := mockStore.assignments["test-container"]
		assert.True(t, found)
		assert.True(t, assignment.Ignore)
	})
}

// TestUnassignScript tests script unassignment
func TestUnassignScript(t *testing.T) {
	ctx := context.Background()

	t.Run("unassigns script successfully", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.AssignScript(ctx, "test-container", "", "test")
		require.NoError(t, err)

		err = manager.UnassignScript(ctx, "test-container")
		require.NoError(t, err)

		_, found := mockStore.assignments["test-container"]
		assert.False(t, found)
	})
}

// TestSetIgnore tests the ignore flag
func TestSetIgnore(t *testing.T) {
	ctx := context.Background()

	t.Run("sets ignore to true", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetIgnore(ctx, "test-container", true, "test")
		require.NoError(t, err)

		assignment, found := mockStore.assignments["test-container"]
		assert.True(t, found)
		assert.True(t, assignment.Ignore)
	})

	t.Run("sets ignore to false", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetIgnore(ctx, "test-container", true, "test")
		require.NoError(t, err)

		err = manager.SetIgnore(ctx, "test-container", false, "test")
		require.NoError(t, err)

		assignment, found := mockStore.assignments["test-container"]
		assert.True(t, found)
		assert.False(t, assignment.Ignore)
	})

	t.Run("preserves other settings", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetAllowLatest(ctx, "test-container", true, "test")
		require.NoError(t, err)

		err = manager.SetIgnore(ctx, "test-container", true, "test")
		require.NoError(t, err)

		assignment, found := mockStore.assignments["test-container"]
		assert.True(t, found)
		assert.True(t, assignment.Ignore)
		assert.True(t, assignment.AllowLatest)
	})
}

// TestSetAllowLatest tests the allow-latest flag
func TestSetAllowLatest(t *testing.T) {
	ctx := context.Background()

	t.Run("sets allow_latest to true", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetAllowLatest(ctx, "test-container", true, "test")
		require.NoError(t, err)

		assignment, found := mockStore.assignments["test-container"]
		assert.True(t, found)
		assert.True(t, assignment.AllowLatest)
	})

	t.Run("sets allow_latest to false", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetAllowLatest(ctx, "test-container", true, "test")
		require.NoError(t, err)

		err = manager.SetAllowLatest(ctx, "test-container", false, "test")
		require.NoError(t, err)

		assignment, found := mockStore.assignments["test-container"]
		assert.True(t, found)
		assert.False(t, assignment.AllowLatest)
	})
}

// TestListAssignments tests listing assignments
func TestListAssignments(t *testing.T) {
	ctx := context.Background()

	t.Run("lists all assignments", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetIgnore(ctx, "container1", true, "test")
		require.NoError(t, err)
		err = manager.SetIgnore(ctx, "container2", false, "test")
		require.NoError(t, err)

		assignments, err := manager.ListAssignments(ctx, false)
		require.NoError(t, err)
		assert.Len(t, assignments, 2)
	})

	t.Run("lists enabled only", func(t *testing.T) {
		mockStore := newMockStorage()
		mockStore.assignments["enabled"] = storage.ScriptAssignment{
			ContainerName: "enabled",
			Enabled:       true,
		}
		mockStore.assignments["disabled"] = storage.ScriptAssignment{
			ContainerName: "disabled",
			Enabled:       false,
		}

		manager := NewManager(mockStore, nil)

		assignments, err := manager.ListAssignments(ctx, true)
		require.NoError(t, err)
		assert.Len(t, assignments, 1)
		assert.Equal(t, "enabled", assignments[0].ContainerName)
	})
}

// TestGetAssignment tests getting a single assignment
func TestGetAssignment(t *testing.T) {
	ctx := context.Background()

	t.Run("gets existing assignment", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		err := manager.SetIgnore(ctx, "test-container", true, "test")
		require.NoError(t, err)

		assignment, found, err := manager.GetAssignment(ctx, "test-container")
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, "test-container", assignment.ContainerName)
		assert.True(t, assignment.Ignore)
	})

	t.Run("returns not found for missing assignment", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		assignment, found, err := manager.GetAssignment(ctx, "nonexistent")
		require.NoError(t, err)
		assert.False(t, found)
		assert.Equal(t, Assignment{}, assignment)
	})
}

// TestMigrateLabels tests label migration
func TestMigrateLabels(t *testing.T) {
	ctx := context.Background()

	t.Run("migrates containers with labels", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		containers := []docker.Container{
			{
				Name: "ignored-container",
				Labels: map[string]string{
					IgnoreLabel: "true",
				},
			},
			{
				Name: "latest-allowed",
				Labels: map[string]string{
					AllowLatestLabel: "true",
				},
			},
			{
				Name: "with-script",
				Labels: map[string]string{
					PreUpdateCheckLabel: "check.sh",
				},
			},
			{
				Name: "no-labels",
				Labels: map[string]string{
					"other.label": "value",
				},
			},
		}

		count, err := manager.MigrateLabels(ctx, containers)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		assignment1, found := mockStore.assignments["ignored-container"]
		assert.True(t, found)
		assert.True(t, assignment1.Ignore)

		assignment2, found := mockStore.assignments["latest-allowed"]
		assert.True(t, found)
		assert.True(t, assignment2.AllowLatest)

		assignment3, found := mockStore.assignments["with-script"]
		assert.True(t, found)
		assert.Equal(t, "check.sh", assignment3.ScriptPath)

		_, found = mockStore.assignments["no-labels"]
		assert.False(t, found)
	})

	t.Run("skips already migrated containers", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		mockStore.assignments["existing"] = storage.ScriptAssignment{
			ContainerName: "existing",
			Ignore:        false,
		}

		containers := []docker.Container{
			{
				Name: "existing",
				Labels: map[string]string{
					IgnoreLabel: "true",
				},
			},
		}

		count, err := manager.MigrateLabels(ctx, containers)
		require.NoError(t, err)
		assert.Equal(t, 0, count)

		assignment := mockStore.assignments["existing"]
		assert.False(t, assignment.Ignore)
	})

	t.Run("handles various boolean formats", func(t *testing.T) {
		mockStore := newMockStorage()
		manager := NewManager(mockStore, nil)

		containers := []docker.Container{
			{Name: "true-string", Labels: map[string]string{IgnoreLabel: "true"}},
			{Name: "one-string", Labels: map[string]string{IgnoreLabel: "1"}},
			{Name: "yes-string", Labels: map[string]string{IgnoreLabel: "yes"}},
			{Name: "false-string", Labels: map[string]string{IgnoreLabel: "false"}},
		}

		count, err := manager.MigrateLabels(ctx, containers)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		assert.True(t, mockStore.assignments["true-string"].Ignore)
		assert.True(t, mockStore.assignments["one-string"].Ignore)
		assert.True(t, mockStore.assignments["yes-string"].Ignore)
		_, found := mockStore.assignments["false-string"]
		assert.False(t, found)
	})
}

// TestConstants verifies label constants
func TestConstants(t *testing.T) {
	assert.Equal(t, "/scripts", ScriptsDir)
	assert.Equal(t, "docksmith.pre-update-check", PreUpdateCheckLabel)
	assert.Equal(t, "docksmith.ignore", IgnoreLabel)
	assert.Equal(t, "docksmith.allow-latest", AllowLatestLabel)
	assert.Equal(t, "docksmith.restart-after", RestartAfterLabel)
	assert.Equal(t, "docksmith.version-pin-major", VersionPinMajorLabel)
	assert.Equal(t, "docksmith.version-pin-minor", VersionPinMinorLabel)
	assert.Equal(t, "docksmith.tag-regex", TagRegexLabel)
	assert.Equal(t, "docksmith.version-min", VersionMinLabel)
	assert.Equal(t, "docksmith.version-max", VersionMaxLabel)
}

// TestAssignmentTimestamps verifies timestamps are set correctly
func TestAssignmentTimestamps(t *testing.T) {
	ctx := context.Background()
	mockStore := newMockStorage()
	manager := NewManager(mockStore, nil)

	beforeTest := time.Now()

	err := manager.SetIgnore(ctx, "test-container", true, "test")
	require.NoError(t, err)

	afterTest := time.Now()

	assignment := mockStore.assignments["test-container"]
	assert.True(t, assignment.AssignedAt.After(beforeTest) || assignment.AssignedAt.Equal(beforeTest))
	assert.True(t, assignment.AssignedAt.Before(afterTest) || assignment.AssignedAt.Equal(afterTest))
	assert.True(t, assignment.UpdatedAt.After(beforeTest) || assignment.UpdatedAt.Equal(beforeTest))
	assert.True(t, assignment.UpdatedAt.Before(afterTest) || assignment.UpdatedAt.Equal(afterTest))
}
