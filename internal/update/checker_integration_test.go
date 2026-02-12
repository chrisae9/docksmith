package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/storage"
)

// mockStorage implements storage.Storage for testing
type mockStorage struct {
	versionCache map[string]string // key: sha256+imageRef+arch, value: version
	checkHistory []storage.CheckHistoryEntry
	saveCalls    int
	getCalls     int
	logCalls     int
}

func newMockStorage() *mockStorage {
	return &mockStorage{
		versionCache: make(map[string]string),
		checkHistory: []storage.CheckHistoryEntry{},
	}
}

func (m *mockStorage) SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error {
	m.saveCalls++
	key := sha256 + "|" + imageRef + "|" + arch
	m.versionCache[key] = version
	return nil
}

func (m *mockStorage) GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (string, bool, error) {
	m.getCalls++
	key := sha256 + "|" + imageRef + "|" + arch
	version, found := m.versionCache[key]
	return version, found, nil
}

func (m *mockStorage) LogCheck(ctx context.Context, containerName, image, currentVer, latestVer, status string, checkErr error) error {
	m.logCalls++
	entry := storage.CheckHistoryEntry{
		ContainerName:  containerName,
		Image:          image,
		CurrentVersion: currentVer,
		LatestVersion:  latestVer,
		Status:         status,
		CheckTime:      time.Now(),
	}
	if checkErr != nil {
		entry.Error = checkErr.Error()
	}
	m.checkHistory = append(m.checkHistory, entry)
	return nil
}

func (m *mockStorage) LogCheckBatch(ctx context.Context, checks []storage.CheckHistoryEntry) error {
	m.logCalls++
	m.checkHistory = append(m.checkHistory, checks...)
	return nil
}

func (m *mockStorage) GetCheckHistory(ctx context.Context, containerName string, limit int) ([]storage.CheckHistoryEntry, error) {
	return m.checkHistory, nil
}

func (m *mockStorage) GetCheckHistoryByTimeRange(ctx context.Context, start, end time.Time) ([]storage.CheckHistoryEntry, error) {
	return m.checkHistory, nil
}

func (m *mockStorage) GetAllCheckHistory(ctx context.Context, limit int) ([]storage.CheckHistoryEntry, error) {
	return m.checkHistory, nil
}

func (m *mockStorage) GetCheckHistorySince(ctx context.Context, since time.Time) ([]storage.CheckHistoryEntry, error) {
	return m.checkHistory, nil
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

func (m *mockStorage) SetConfig(ctx context.Context, key, value string) error {
	return nil
}


func (m *mockStorage) SaveConfigSnapshot(ctx context.Context, snapshot storage.ConfigSnapshot) error {
	return nil
}

func (m *mockStorage) GetConfigHistory(ctx context.Context, limit int) ([]storage.ConfigSnapshot, error) {
	return nil, nil
}

func (m *mockStorage) GetConfigSnapshotByID(ctx context.Context, snapshotID int64) (storage.ConfigSnapshot, bool, error) {
	return storage.ConfigSnapshot{}, false, nil
}

func (m *mockStorage) RevertToSnapshot(ctx context.Context, snapshotID int64) error {
	return nil
}

func (m *mockStorage) SaveUpdateOperation(ctx context.Context, op storage.UpdateOperation) error {
	return nil
}

func (m *mockStorage) GetUpdateOperation(ctx context.Context, operationID string) (storage.UpdateOperation, bool, error) {
	return storage.UpdateOperation{}, false, nil
}

func (m *mockStorage) GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]storage.UpdateOperation, error) {
	return nil, nil
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

func (m *mockStorage) GetUpdateOperationsByBatchGroup(ctx context.Context, batchGroupID string) ([]storage.UpdateOperation, error) {
	return nil, nil
}

func (m *mockStorage) UpdateOperationStatus(ctx context.Context, operationID string, status string, errorMsg string) error {
	return nil
}

func (m *mockStorage) GetRollbackPolicy(ctx context.Context, entityType, entityID string) (storage.RollbackPolicy, bool, error) {
	return storage.RollbackPolicy{}, false, nil
}

func (m *mockStorage) SetRollbackPolicy(ctx context.Context, policy storage.RollbackPolicy) error {
	return nil
}

func (m *mockStorage) QueueUpdate(ctx context.Context, queue storage.UpdateQueue) error {
	return nil
}

func (m *mockStorage) DequeueUpdate(ctx context.Context, stackName string) (storage.UpdateQueue, bool, error) {
	return storage.UpdateQueue{}, false, nil
}

func (m *mockStorage) GetQueuedUpdates(ctx context.Context) ([]storage.UpdateQueue, error) {
	return nil, nil
}

func (m *mockStorage) SaveScriptAssignment(ctx context.Context, assignment storage.ScriptAssignment) error {
	return nil
}

func (m *mockStorage) GetScriptAssignment(ctx context.Context, containerName string) (storage.ScriptAssignment, bool, error) {
	return storage.ScriptAssignment{}, false, nil
}

func (m *mockStorage) ListScriptAssignments(ctx context.Context, enabledOnly bool) ([]storage.ScriptAssignment, error) {
	return nil, nil
}

func (m *mockStorage) DeleteScriptAssignment(ctx context.Context, containerName string) error {
	return nil
}

func (m *mockStorage) Close() error {
	return nil
}

// TestCheckerUseCacheBeforeRegistryAPICall tests that checker queries cache before making registry API calls
func TestCheckerUseCacheBeforeRegistryAPICall(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{
				ID:    "test-container",
				Name:  "test",
				Image: "docker.io/library/nginx:latest",
			},
		},
		imageDigests: map[string]string{
			"docker.io/library/nginx:latest": "sha256:abc123",
		},
		imageVersions: map[string]string{},
		localImages:   map[string]bool{},
	}

	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"docker.io/library/nginx": {"1.25.0", "1.24.0"},
		},
		tagDigests: map[string]string{},
		digestMappings: map[string]map[string][]string{
			"docker.io/library/nginx": {
				"1.25.0": {"sha256:abc123"},
			},
		},
	}

	mockStore := newMockStorage()
	// Pre-populate cache with version resolution (normalized digest without sha256: prefix)
	mockStore.SaveVersionCache(context.Background(), "abc123", "docker.io/library/nginx", "1.25.0", "amd64")

	checker := NewChecker(mockDocker, mockRegistry, mockStore)

	ctx := context.Background()
	result, err := checker.CheckForUpdates(ctx)

	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}

	if len(result.Updates) != 1 {
		t.Fatalf("Expected 1 update, got %d", len(result.Updates))
	}

	update := result.Updates[0]
	if update.CurrentVersion != "1.25.0" {
		t.Errorf("Expected current version to be resolved from cache as 1.25.0, got %s", update.CurrentVersion)
	}

	// Verify cache was queried
	if mockStore.getCalls == 0 {
		t.Error("Expected cache to be queried, but it wasn't")
	}
}

// TestCheckerSavesSuccessfulResolutionToCache tests that checker saves successful registry resolutions to cache
func TestCheckerSavesSuccessfulResolutionToCache(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{
				ID:    "test-container",
				Name:  "test",
				Image: "docker.io/library/nginx:latest",
			},
		},
		imageDigests: map[string]string{
			"docker.io/library/nginx:latest": "sha256:def456",
		},
		imageVersions: map[string]string{},
		localImages:   map[string]bool{},
	}

	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"docker.io/library/nginx": {"1.26.0", "1.25.0"},
		},
		tagDigests: map[string]string{},
		digestMappings: map[string]map[string][]string{
			"docker.io/library/nginx": {
				"1.26.0": {"sha256:def456"},
			},
		},
	}

	mockStore := newMockStorage()

	checker := NewChecker(mockDocker, mockRegistry, mockStore)

	ctx := context.Background()
	_, err := checker.CheckForUpdates(ctx)

	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}

	// Verify SaveVersionCache was called
	if mockStore.saveCalls == 0 {
		t.Error("Expected SaveVersionCache to be called, but it wasn't")
	}

	// Verify version was saved to cache (normalized digest without sha256: prefix)
	version, found, err := mockStore.GetVersionCache(ctx, "def456", "docker.io/library/nginx", "amd64")
	if err != nil {
		t.Fatalf("Failed to get from cache: %v", err)
	}

	if !found {
		t.Error("Expected version to be saved to cache, but it wasn't found")
	}

	if version != "1.26.0" {
		t.Errorf("Expected cached version to be 1.26.0, got %s", version)
	}
}

// TestCheckerLogsCheckResultsToHistory tests that checker logs check results to history
func TestCheckerLogsCheckResultsToHistory(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{
				ID:    "test-container-1",
				Name:  "nginx",
				Image: "docker.io/library/nginx:1.25.0",
			},
			{
				ID:    "test-container-2",
				Name:  "redis",
				Image: "docker.io/library/redis:7.0.0",
			},
		},
		imageDigests: map[string]string{
			"docker.io/library/nginx:1.25.0": "sha256:nginx123",
			"docker.io/library/redis:7.0.0":  "sha256:redis123",
		},
		imageVersions: map[string]string{
			"docker.io/library/nginx:1.25.0": "1.25.0",
			"docker.io/library/redis:7.0.0":  "7.0.0",
		},
		localImages: map[string]bool{},
	}

	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"docker.io/library/nginx": {"1.26.0", "1.25.0"},
			"docker.io/library/redis": {"7.2.0", "7.0.0"},
		},
		tagDigests:     map[string]string{},
		digestMappings: map[string]map[string][]string{},
	}

	mockStore := newMockStorage()

	checker := NewChecker(mockDocker, mockRegistry, mockStore)

	ctx := context.Background()
	_, err := checker.CheckForUpdates(ctx)

	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}

	// Verify checks were logged
	if mockStore.logCalls == 0 {
		t.Error("Expected checks to be logged, but no log calls were made")
	}

	if len(mockStore.checkHistory) != 2 {
		t.Errorf("Expected 2 check history entries, got %d", len(mockStore.checkHistory))
	}

	// Verify first entry
	entry1 := mockStore.checkHistory[0]
	if entry1.ContainerName != "nginx" {
		t.Errorf("Expected container name 'nginx', got '%s'", entry1.ContainerName)
	}
	if entry1.CurrentVersion != "1.25.0" {
		t.Errorf("Expected current version '1.25.0', got '%s'", entry1.CurrentVersion)
	}
	if entry1.LatestVersion != "1.26.0" {
		t.Errorf("Expected latest version '1.26.0', got '%s'", entry1.LatestVersion)
	}
}

// TestCheckerWorksWithNilStorage tests backwards compatibility - checker works when storage is nil
func TestCheckerWorksWithNilStorage(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{
				ID:    "test-container",
				Name:  "test",
				Image: "docker.io/library/nginx:1.25.0",
			},
		},
		imageDigests: map[string]string{
			"docker.io/library/nginx:1.25.0": "sha256:abc123",
		},
		imageVersions: map[string]string{
			"docker.io/library/nginx:1.25.0": "1.25.0",
		},
		localImages: map[string]bool{},
	}

	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"docker.io/library/nginx": {"1.26.0", "1.25.0"},
		},
		tagDigests:     map[string]string{},
		digestMappings: map[string]map[string][]string{},
	}

	// Pass nil storage - should work without errors
	checker := NewChecker(mockDocker, mockRegistry, nil)

	ctx := context.Background()
	result, err := checker.CheckForUpdates(ctx)

	if err != nil {
		t.Fatalf("CheckForUpdates failed with nil storage: %v", err)
	}

	if len(result.Updates) != 1 {
		t.Fatalf("Expected 1 update, got %d", len(result.Updates))
	}

	update := result.Updates[0]
	if update.CurrentVersion != "1.25.0" {
		t.Errorf("Expected current version 1.25.0, got %s", update.CurrentVersion)
	}

	if update.Status != UpdateAvailable {
		t.Errorf("Expected status UPDATE_AVAILABLE, got %s", update.Status)
	}
}

// TestCheckerCacheHitReducesRegistryAPICalls tests that cache hits reduce registry API calls
func TestCheckerCacheHitReducesRegistryAPICalls(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{
				ID:    "test-container",
				Name:  "test",
				Image: "docker.io/library/nginx:latest",
			},
		},
		imageDigests: map[string]string{
			"docker.io/library/nginx:latest": "sha256:cached123",
		},
		imageVersions: map[string]string{},
		localImages:   map[string]bool{},
	}

	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"docker.io/library/nginx": {"1.27.0", "1.26.0"},
		},
		tagDigests: map[string]string{},
		digestMappings: map[string]map[string][]string{
			"docker.io/library/nginx": {
				"1.26.0": {"sha256:cached123"},
			},
		},
		listTagsWithDigestsCalls: 0,
	}

	mockStore := newMockStorage()
	// Pre-populate cache (normalized digest without sha256: prefix)
	mockStore.SaveVersionCache(context.Background(), "cached123", "docker.io/library/nginx", "1.26.0", "amd64")

	checker := NewChecker(mockDocker, mockRegistry, mockStore)

	ctx := context.Background()
	_, err := checker.CheckForUpdates(ctx)

	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}

	// Verify that ListTagsWithDigests was NOT called (cache hit)
	if mockRegistry.listTagsWithDigestsCalls > 0 {
		t.Errorf("Expected ListTagsWithDigests to not be called due to cache hit, but it was called %d times", mockRegistry.listTagsWithDigestsCalls)
	}
}

// TestCheckerHandlesStorageErrors tests graceful handling when storage operations fail
func TestCheckerHandlesStorageErrors(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{
				ID:    "test-container",
				Name:  "test",
				Image: "docker.io/library/nginx:1.25.0",
			},
		},
		imageDigests: map[string]string{
			"docker.io/library/nginx:1.25.0": "sha256:abc123",
		},
		imageVersions: map[string]string{
			"docker.io/library/nginx:1.25.0": "1.25.0",
		},
		localImages: map[string]bool{},
	}

	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"docker.io/library/nginx": {"1.26.0", "1.25.0"},
		},
		tagDigests:     map[string]string{},
		digestMappings: map[string]map[string][]string{},
	}

	// Storage that fails operations
	failingStorage := &failingStorage{}

	checker := NewChecker(mockDocker, mockRegistry, failingStorage)

	ctx := context.Background()
	result, err := checker.CheckForUpdates(ctx)

	// Should not fail completely - check should still succeed
	if err != nil {
		t.Fatalf("CheckForUpdates should not fail when storage errors occur: %v", err)
	}

	if len(result.Updates) != 1 {
		t.Fatalf("Expected 1 update result despite storage errors, got %d", len(result.Updates))
	}

	// Check should still work
	if result.Updates[0].Status != UpdateAvailable {
		t.Errorf("Expected UPDATE_AVAILABLE status despite storage errors, got %s", result.Updates[0].Status)
	}
}

// failingStorage is a mock storage that always returns errors
type failingStorage struct{}

func (f *failingStorage) SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error {
	return errors.New("storage error: save failed")
}

func (f *failingStorage) GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (string, bool, error) {
	return "", false, errors.New("storage error: get failed")
}

func (f *failingStorage) LogCheck(ctx context.Context, containerName, image, currentVer, latestVer, status string, checkErr error) error {
	return errors.New("storage error: log failed")
}

func (f *failingStorage) LogCheckBatch(ctx context.Context, checks []storage.CheckHistoryEntry) error {
	return errors.New("storage error: batch log failed")
}

func (f *failingStorage) GetCheckHistory(ctx context.Context, containerName string, limit int) ([]storage.CheckHistoryEntry, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetCheckHistoryByTimeRange(ctx context.Context, start, end time.Time) ([]storage.CheckHistoryEntry, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetAllCheckHistory(ctx context.Context, limit int) ([]storage.CheckHistoryEntry, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetCheckHistorySince(ctx context.Context, since time.Time) ([]storage.CheckHistoryEntry, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) LogUpdate(ctx context.Context, containerName, operation, fromVer, toVer string, success bool, updateErr error) error {
	return errors.New("storage error")
}

func (f *failingStorage) GetUpdateLog(ctx context.Context, containerName string, limit int) ([]storage.UpdateLogEntry, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetAllUpdateLog(ctx context.Context, limit int) ([]storage.UpdateLogEntry, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetConfig(ctx context.Context, key string) (string, bool, error) {
	return "", false, errors.New("storage error")
}

func (f *failingStorage) SetConfig(ctx context.Context, key, value string) error {
	return errors.New("storage error")
}


func (f *failingStorage) SaveConfigSnapshot(ctx context.Context, snapshot storage.ConfigSnapshot) error {
	return errors.New("storage error")
}

func (f *failingStorage) GetConfigHistory(ctx context.Context, limit int) ([]storage.ConfigSnapshot, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetConfigSnapshotByID(ctx context.Context, snapshotID int64) (storage.ConfigSnapshot, bool, error) {
	return storage.ConfigSnapshot{}, false, errors.New("storage error")
}

func (f *failingStorage) RevertToSnapshot(ctx context.Context, snapshotID int64) error {
	return errors.New("storage error")
}

func (f *failingStorage) SaveUpdateOperation(ctx context.Context, op storage.UpdateOperation) error {
	return errors.New("storage error")
}

func (f *failingStorage) GetUpdateOperation(ctx context.Context, operationID string) (storage.UpdateOperation, bool, error) {
	return storage.UpdateOperation{}, false, errors.New("storage error")
}

func (f *failingStorage) GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]storage.UpdateOperation, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetUpdateOperations(ctx context.Context, limit int) ([]storage.UpdateOperation, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetUpdateOperationsByContainer(ctx context.Context, containerName string, limit int) ([]storage.UpdateOperation, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetUpdateOperationsByTimeRange(ctx context.Context, start, end time.Time) ([]storage.UpdateOperation, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) GetUpdateOperationsByBatchGroup(ctx context.Context, batchGroupID string) ([]storage.UpdateOperation, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) UpdateOperationStatus(ctx context.Context, operationID string, status string, errorMsg string) error {
	return errors.New("storage error")
}

func (f *failingStorage) GetRollbackPolicy(ctx context.Context, entityType, entityID string) (storage.RollbackPolicy, bool, error) {
	return storage.RollbackPolicy{}, false, errors.New("storage error")
}

func (f *failingStorage) SetRollbackPolicy(ctx context.Context, policy storage.RollbackPolicy) error {
	return errors.New("storage error")
}

func (f *failingStorage) QueueUpdate(ctx context.Context, queue storage.UpdateQueue) error {
	return errors.New("storage error")
}

func (f *failingStorage) DequeueUpdate(ctx context.Context, stackName string) (storage.UpdateQueue, bool, error) {
	return storage.UpdateQueue{}, false, errors.New("storage error")
}

func (f *failingStorage) GetQueuedUpdates(ctx context.Context) ([]storage.UpdateQueue, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) SaveScriptAssignment(ctx context.Context, assignment storage.ScriptAssignment) error {
	return errors.New("storage error")
}

func (f *failingStorage) GetScriptAssignment(ctx context.Context, containerName string) (storage.ScriptAssignment, bool, error) {
	return storage.ScriptAssignment{}, false, errors.New("storage error")
}

func (f *failingStorage) ListScriptAssignments(ctx context.Context, enabledOnly bool) ([]storage.ScriptAssignment, error) {
	return nil, errors.New("storage error")
}

func (f *failingStorage) DeleteScriptAssignment(ctx context.Context, containerName string) error {
	return errors.New("storage error")
}

func (f *failingStorage) Close() error {
	return errors.New("storage error")
}

// mockDockerClient is a mock implementation for testing
type mockDockerClient struct {
	containers    []docker.Container
	imageDigests  map[string]string
	imageVersions map[string]string
	localImages   map[string]bool
}

func (m *mockDockerClient) ListContainers(ctx context.Context) ([]docker.Container, error) {
	return m.containers, nil
}

func (m *mockDockerClient) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	digest, ok := m.imageDigests[imageName]
	if !ok {
		return "", errors.New("digest not found")
	}
	return digest, nil
}

func (m *mockDockerClient) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	version, ok := m.imageVersions[imageName]
	if !ok {
		return "", errors.New("version not found")
	}
	return version, nil
}

func (m *mockDockerClient) IsLocalImage(ctx context.Context, imageName string) (bool, error) {
	isLocal, ok := m.localImages[imageName]
	if !ok {
		return false, nil
	}
	return isLocal, nil
}

func (m *mockDockerClient) Close() error {
	return nil
}

// mockRegistryClient is a mock implementation for testing
type mockRegistryClient struct {
	tags                     map[string][]string
	tagDigests               map[string]string
	digestMappings           map[string]map[string][]string // imageRef -> tag -> []digests
	listTagsWithDigestsCalls int
}

func (m *mockRegistryClient) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	tags, ok := m.tags[imageRef]
	if !ok {
		return nil, errors.New("image not found")
	}
	return tags, nil
}

func (m *mockRegistryClient) GetTagDigest(ctx context.Context, imageRef, tag string) (string, error) {
	key := imageRef + ":" + tag
	digest, ok := m.tagDigests[key]
	if !ok {
		return "", errors.New("tag digest not found")
	}
	return digest, nil
}

func (m *mockRegistryClient) GetLatestTag(ctx context.Context, imageRef string) (string, error) {
	return "latest", nil
}

func (m *mockRegistryClient) ListTagsWithDigests(ctx context.Context, imageRef string) (map[string][]string, error) {
	m.listTagsWithDigestsCalls++
	mappings, ok := m.digestMappings[imageRef]
	if !ok {
		return nil, errors.New("image not found")
	}
	return mappings, nil
}

// TestLatestContainerDateTagResolution tests that a :latest container with both
// date-format (2026.2.9) and v-prefixed (v2026.2.9) tags resolves to the date-format
// tag, not the v-prefixed variant that may have an invalid manifest.
func TestLatestContainerDateTagResolution(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{
				ID:    "openclaw-gateway",
				Name:  "moltbot-openclaw-gateway-1",
				Image: "ghcr.io/openclaw/openclaw:latest",
			},
		},
		imageDigests: map[string]string{
			"ghcr.io/openclaw/openclaw:latest": "sha256:olddigest111",
		},
		imageVersions: map[string]string{},
		localImages:   map[string]bool{},
	}

	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"ghcr.io/openclaw/openclaw": {
				"latest", "2026.2.9", "v2026.2.9", "2026.2.1", "v2026.2.1",
			},
		},
		tagDigests: map[string]string{
			// Latest digest differs from current → update available
			"ghcr.io/openclaw/openclaw:latest": "sha256:newdigest222",
		},
		digestMappings: map[string]map[string][]string{
			"ghcr.io/openclaw/openclaw": {
				// resolveVersionFromDigest sees both tags sharing the same digest
				"v2026.2.9": {"sha256:newdigest222"},
				"2026.2.9":  {"sha256:newdigest222"},
				"latest":    {"sha256:newdigest222"},
			},
		},
	}

	mockStore := newMockStorage()
	checker := NewChecker(mockDocker, mockRegistry, mockStore)

	ctx := context.Background()
	result, err := checker.CheckForUpdates(ctx)
	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}

	if len(result.Updates) != 1 {
		t.Fatalf("Expected 1 update, got %d", len(result.Updates))
	}

	update := result.Updates[0]

	if update.Status != UpdateAvailable {
		t.Errorf("Expected UpdateAvailable, got %s", update.Status)
	}

	// The key assertion: LatestResolvedVersion should be "2026.2.9" (date-format),
	// NOT "v2026.2.9" (which may have an invalid manifest in the registry)
	if update.LatestResolvedVersion != "2026.2.9" {
		t.Errorf("LatestResolvedVersion: got %q, want %q — should prefer date-format tag over v-prefixed variant",
			update.LatestResolvedVersion, "2026.2.9")
	}
}
