package update

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/docker"
)

// MockFailingDockerService simulates Docker daemon failures
type MockFailingDockerService struct {
	shouldFail bool
	failCount  int
	callCount  int
}

func (m *MockFailingDockerService) ListContainers(ctx context.Context) ([]docker.Container, error) {
	m.callCount++
	if m.shouldFail {
		return nil, errors.New("failed to connect to Docker daemon")
	}
	if m.callCount <= m.failCount {
		return nil, errors.New("temporary connection failure")
	}
	return []docker.Container{
		{
			ID:    "test123",
			Name:  "test-container",
			Image: "nginx:1.25.3",
		},
	}, nil
}

func (m *MockFailingDockerService) IsLocalImage(ctx context.Context, image string) (bool, error) {
	if m.shouldFail {
		return false, errors.New("failed to check image")
	}
	return false, nil
}

func (m *MockFailingDockerService) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	if m.shouldFail {
		return "", errors.New("failed to get image version")
	}
	return "1.25.3", nil
}

func (m *MockFailingDockerService) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	if m.shouldFail {
		return "", errors.New("failed to get image digest")
	}
	return "sha256:abc123", nil
}

func (m *MockFailingDockerService) Close() error {
	return nil
}

// MockFailingRegistryManager simulates registry failures
type MockFailingRegistryManager struct {
	shouldTimeout bool
	shouldFail    bool
	callCount     int
	failUntil     int
}

func (m *MockFailingRegistryManager) ListTags(ctx context.Context, image string) ([]string, error) {
	m.callCount++

	if m.shouldTimeout {
		time.Sleep(100 * time.Millisecond)
		return nil, context.DeadlineExceeded
	}

	if m.shouldFail || m.callCount <= m.failUntil {
		return nil, errors.New("registry authentication failed")
	}

	return []string{"1.25.3", "1.25.2", "1.25.1"}, nil
}

func (m *MockFailingRegistryManager) GetLatestTag(ctx context.Context, image string) (string, error) {
	tags, err := m.ListTags(ctx, image)
	if err != nil {
		return "", err
	}
	if len(tags) == 0 {
		return "", errors.New("no tags found")
	}
	return tags[0], nil
}

func (m *MockFailingRegistryManager) GetTagDigest(ctx context.Context, imageRef, tag string) (string, error) {
	if m.shouldTimeout || m.shouldFail {
		return "", errors.New("failed to get tag digest")
	}
	return "sha256:def456", nil
}

func (m *MockFailingRegistryManager) ListTagsWithDigests(ctx context.Context, imageRef string) (map[string][]string, error) {
	if m.shouldTimeout || m.shouldFail {
		return nil, errors.New("failed to get tag digests")
	}
	return map[string][]string{}, nil
}

// TestDockerDaemonUnavailable tests handling of Docker daemon failures
func TestDockerDaemonUnavailable(t *testing.T) {
	dockerService := &MockFailingDockerService{shouldFail: true}
	registryManager := &MockSuccessRegistryManager{}

	checker := NewChecker(dockerService, registryManager, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := checker.CheckForUpdates(ctx)

	// Should return error when Docker daemon is unavailable
	if err == nil {
		t.Error("Expected error when Docker daemon is unavailable, got nil")
	}

	// Error should be informative
	if err != nil && err.Error() == "" {
		t.Error("Error message should not be empty")
	}

	// Result should still be usable (not panic)
	if result == nil {
		t.Error("Result should not be nil even on error")
	}
}

// TestRegistryTimeout tests handling of registry timeouts
func TestRegistryTimeout(t *testing.T) {
	dockerService := &MockSuccessDockerService{
		containers: []docker.Container{
			{ID: "test1", Name: "nginx", Image: "nginx:1.25.3"},
		},
	}
	registryManager := &MockFailingRegistryManager{shouldTimeout: true}

	checker := NewChecker(dockerService, registryManager, nil)

	// Use a short timeout to trigger the timeout scenario
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := checker.CheckForUpdates(ctx)

	// Should not fail entirely - should handle timeouts gracefully
	if err != nil && result == nil {
		t.Error("Should return partial results even on timeout")
	}

	// Should have checked at least one container
	if result != nil && result.TotalChecked == 0 {
		t.Error("Should attempt to check containers before timing out")
	}
}

// TestRegistryAuthenticationFailure tests handling of auth failures
func TestRegistryAuthenticationFailure(t *testing.T) {
	dockerService := &MockSuccessDockerService{
		containers: []docker.Container{
			{ID: "test1", Name: "nginx", Image: "ghcr.io/myorg/private:1.0.0"},
		},
	}
	registryManager := &MockFailingRegistryManager{shouldFail: true}

	checker := NewChecker(dockerService, registryManager, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := checker.CheckForUpdates(ctx)

	// Should not fail entirely - continue with other containers
	if err != nil {
		t.Logf("Warning: Got error (expected to continue gracefully): %v", err)
	}

	// Should mark this container as failed but continue
	if result == nil {
		t.Fatal("Result should not be nil")
	}

	if result.Failed == 0 {
		t.Error("Expected at least one failed check")
	}

	// The failed container should have error information
	if len(result.Updates) > 0 && result.Updates[0].Status == CheckFailed {
		if result.Updates[0].Error == "" {
			t.Error("Failed container should have error message")
		}
	}
}

// TestMalformedVersionHandling tests handling of unparseable versions
func TestMalformedVersionHandling(t *testing.T) {
	dockerService := &MockSuccessDockerService{
		containers: []docker.Container{
			{ID: "test1", Name: "app1", Image: "myapp:weird-tag-format-!!!"},
			{ID: "test2", Name: "app2", Image: "myapp:1.2.3"}, // Valid for comparison
		},
	}
	registryManager := &MockSuccessRegistryManager{}

	checker := NewChecker(dockerService, registryManager, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := checker.CheckForUpdates(ctx)

	// Should not fail entirely
	if err != nil {
		t.Logf("Got error: %v", err)
	}

	// Should have checked both containers
	if result == nil {
		t.Fatal("Result should not be nil")
	}

	if result.TotalChecked != 2 {
		t.Errorf("Expected 2 containers checked, got %d", result.TotalChecked)
	}
}

// TestPartialFailureRecovery tests that checker continues after partial failures
func TestPartialFailureRecovery(t *testing.T) {
	dockerService := &MockSuccessDockerService{
		containers: []docker.Container{
			{ID: "test1", Name: "nginx", Image: "nginx:1.25.3"},
			{ID: "test2", Name: "postgres", Image: "postgres:16.1"},
			{ID: "test3", Name: "redis", Image: "redis:7.2.3"},
		},
	}

	// Registry fails for first 2 calls, then succeeds
	registryManager := &MockFailingRegistryManager{failUntil: 2}

	checker := NewChecker(dockerService, registryManager, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, _ := checker.CheckForUpdates(ctx)

	// Should complete despite partial failures
	if result == nil {
		t.Fatal("Result should not be nil")
	}

	// Should have attempted all containers
	if result.TotalChecked != 3 {
		t.Errorf("Expected 3 containers checked, got %d", result.TotalChecked)
	}

	// Should have some failures and some successes
	if result.Failed == 0 {
		t.Error("Expected some failed checks")
	}

	t.Logf("Results: Total=%d, Failed=%d, Success=%d",
		result.TotalChecked, result.Failed, result.TotalChecked-result.Failed)
}

// TestContextCancellation tests handling of context cancellation
func TestContextCancellation(t *testing.T) {
	dockerService := &MockSuccessDockerService{
		containers: []docker.Container{
			{ID: "test1", Name: "nginx", Image: "nginx:1.25.3"},
			{ID: "test2", Name: "postgres", Image: "postgres:16.1"},
		},
	}
	registryManager := &MockSuccessRegistryManager{}

	checker := NewChecker(dockerService, registryManager, nil)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel immediately
	cancel()

	result, err := checker.CheckForUpdates(ctx)

	// Should handle cancellation gracefully
	if err == nil {
		t.Log("Warning: Expected error on cancelled context")
	}

	// Should return what it has so far
	if result == nil {
		t.Error("Result should not be nil even on cancellation")
	}
}

// TestEmptyContainerList tests handling of no containers
func TestEmptyContainerList(t *testing.T) {
	dockerService := &MockSuccessDockerService{
		containers: []docker.Container{},
	}
	registryManager := &MockSuccessRegistryManager{}

	checker := NewChecker(dockerService, registryManager, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := checker.CheckForUpdates(ctx)

	// Should not error on empty list
	if err != nil {
		t.Errorf("Should not error on empty container list: %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}

	if result.TotalChecked != 0 {
		t.Errorf("Expected 0 containers checked, got %d", result.TotalChecked)
	}
}

// TestRateLimitHandling tests handling of rate limit responses
func TestRateLimitHandling(t *testing.T) {
	// This is a placeholder for future rate limit handling
	// In a real implementation, you'd mock rate limit errors and verify retry logic
	t.Skip("Rate limit handling not yet implemented")

	dockerService := &MockSuccessDockerService{
		containers: []docker.Container{
			{ID: "test1", Name: "nginx", Image: "nginx:1.25.3"},
		},
	}

	// Would need a MockRateLimitedRegistryManager
	registryManager := &MockSuccessRegistryManager{}

	checker := NewChecker(dockerService, registryManager, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, _ := checker.CheckForUpdates(ctx)

	// Should retry and eventually succeed or fail gracefully
	if result == nil {
		t.Error("Result should not be nil")
	}
}

// Helper: MockSuccessDockerService for successful operations
type MockSuccessDockerService struct {
	containers []docker.Container
}

func (m *MockSuccessDockerService) ListContainers(ctx context.Context) ([]docker.Container, error) {
	return m.containers, nil
}

func (m *MockSuccessDockerService) IsLocalImage(ctx context.Context, image string) (bool, error) {
	return false, nil
}

func (m *MockSuccessDockerService) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	return "1.25.3", nil
}

func (m *MockSuccessDockerService) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	return "sha256:abc123", nil
}

func (m *MockSuccessDockerService) Close() error {
	return nil
}

// Helper: MockSuccessRegistryManager for successful operations
type MockSuccessRegistryManager struct{}

func (m *MockSuccessRegistryManager) ListTags(ctx context.Context, image string) ([]string, error) {
	return []string{"1.25.3", "1.25.2", "1.25.1", "latest"}, nil
}

func (m *MockSuccessRegistryManager) GetLatestTag(ctx context.Context, image string) (string, error) {
	return "1.25.3", nil
}

func (m *MockSuccessRegistryManager) GetTagDigest(ctx context.Context, imageRef, tag string) (string, error) {
	return "sha256:def456", nil
}

func (m *MockSuccessRegistryManager) ListTagsWithDigests(ctx context.Context, imageRef string) (map[string][]string, error) {
	return map[string][]string{}, nil
}
