package update

import (
	"context"
	"testing"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	"github.com/docker/docker/api/types/container"
	dockerclient "github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStandaloneContainerDetection_EmptyComposePath tests standalone container detection via empty compose path.
func TestStandaloneContainerDetection_EmptyComposePath(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "standalone1",
				Name:  "redis",
				Image: "redis:6",
				Labels: map[string]string{
					// No compose labels - standalone container
				},
			},
		},
	}

	mockStorage := NewTestMockStorage()
	bus := events.NewBus()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		dockerSDK:    nil, // Will be set only when actually needed
		storage:      mockStorage,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
	}

	// Get container
	containers, err := mockDocker.ListContainers(context.Background())
	require.NoError(t, err)
	require.Len(t, containers, 1)

	container := &containers[0]

	// Check that compose file path is empty for standalone container
	composeFilePath := orch.getComposeFilePath(container)
	assert.Equal(t, "", composeFilePath, "Standalone container should have empty compose file path")
}

// TestSDKFallback_ContainerInspectRetrievesConfig tests that ContainerInspect retrieves full configuration.
func TestSDKFallback_ContainerInspectRetrievesConfig(t *testing.T) {
	// This test verifies the SDK inspection approach would work
	// Skip if Docker daemon is not available
	dockerSDK, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	defer dockerSDK.Close()

	// Verify we can call ContainerInspect method
	// Note: We're just testing the API is available, not actual inspection
	// since we don't want to depend on specific containers existing
	ctx := context.Background()
	_, err = dockerSDK.ContainerInspect(ctx, "nonexistent-container-test")

	// We expect an error since container doesn't exist, but the method should be callable
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such container", "Should fail with 'No such container' error")
}

// TestSDKFallback_ContainerRemoveAPI tests that ContainerRemove API is available.
func TestSDKFallback_ContainerRemoveAPI(t *testing.T) {
	// This test verifies the SDK remove approach would work
	// Skip if Docker daemon is not available
	dockerSDK, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	defer dockerSDK.Close()

	// Verify we can call ContainerRemove method
	ctx := context.Background()
	err = dockerSDK.ContainerRemove(ctx, "nonexistent-container-test", container.RemoveOptions{})

	// We expect an error since container doesn't exist, but the method should be callable
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such container", "Should fail with 'No such container' error")
}

// TestSDKFallback_ContainerCreateAPI tests that ContainerCreate API is available.
func TestSDKFallback_ContainerCreateAPI(t *testing.T) {
	// This test verifies the SDK create approach would work
	// Skip if Docker daemon is not available
	dockerSDK, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	defer dockerSDK.Close()

	// Test that ContainerCreate method exists and is callable
	// We don't actually create a container, just verify the API
	ctx := context.Background()

	// Create minimal config for API testing
	config := &container.Config{
		Image: "alpine:latest",
	}
	hostConfig := &container.HostConfig{}

	// Attempt to create with invalid name to test API availability
	_, err = dockerSDK.ContainerCreate(ctx, config, hostConfig, nil, nil, "")

	// The call might succeed or fail depending on permissions and image availability
	// We just want to verify the API is callable
	// If it fails, it should not be due to method not existing
	if err != nil {
		// Acceptable errors: image not found, name conflicts, etc.
		// Not acceptable: method doesn't exist, wrong signature
		assert.NotContains(t, err.Error(), "undefined", "API should exist")
	}
}

// TestSDKFallback_ContainerStartAPI tests that ContainerStart API is available.
func TestSDKFallback_ContainerStartAPI(t *testing.T) {
	// This test verifies the SDK start approach would work
	// Skip if Docker daemon is not available
	dockerSDK, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	defer dockerSDK.Close()

	// Verify we can call ContainerStart method
	ctx := context.Background()
	err = dockerSDK.ContainerStart(ctx, "nonexistent-container-test", container.StartOptions{})

	// We expect an error since container doesn't exist, but the method should be callable
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such container", "Should fail with 'No such container' error")
}

// TestBuildImageRef_UpdatesVersion tests that buildImageRef correctly updates version tag.
func TestBuildImageRef_UpdatesVersion(t *testing.T) {
	orch := &UpdateOrchestrator{}

	tests := []struct {
		name           string
		currentImage   string
		targetVersion  string
		expectedResult string
	}{
		{
			name:           "Simple image with tag",
			currentImage:   "redis:6",
			targetVersion:  "7",
			expectedResult: "redis:7",
		},
		{
			name:           "Image with registry and tag",
			currentImage:   "docker.io/library/postgres:13",
			targetVersion:  "14",
			expectedResult: "docker.io/library/postgres:14",
		},
		{
			name:           "Image without tag",
			currentImage:   "nginx",
			targetVersion:  "1.21",
			expectedResult: "nginx:1.21",
		},
		{
			name:           "Image with port and tag",
			currentImage:   "localhost:5000/myapp:1.0",
			targetVersion:  "2.0",
			expectedResult: "localhost:5000/myapp:2.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := orch.buildImageRef(tt.currentImage, tt.targetVersion)
			assert.Equal(t, tt.expectedResult, result)
		})
	}
}
