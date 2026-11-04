package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	"github.com/moby/moby/api/types/container"
	dockerclient "github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStandaloneContainerDetection_EmptyComposePath tests standalone container detection via empty compose path.
func TestStandaloneContainerDetection_EmptyComposePath(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:     "standalone1",
				Name:   "redis",
				Image:  "redis:6",
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
	// Use an isolated Engine HTTP fixture, never the developer daemon.
	dockerSDK, err := newSDKContractClient(t)
	if err != nil {
		t.Fatal(err)
	}
	defer dockerSDK.Close()

	// Verify we can call ContainerInspect method
	// Note: We're just testing the API is available, not actual inspection
	// since we don't want to depend on specific containers existing
	ctx := context.Background()
	_, err = dockerSDK.ContainerInspect(ctx, "nonexistent-container-test", dockerclient.ContainerInspectOptions{})

	// We expect an error since container doesn't exist, but the method should be callable
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such container", "Should fail with 'No such container' error")
}

// TestSDKFallback_ContainerRemoveAPI tests that ContainerRemove API is available.
func TestSDKFallback_ContainerRemoveAPI(t *testing.T) {
	// This test verifies the SDK remove approach would work
	// Use an isolated Engine HTTP fixture, never the developer daemon.
	dockerSDK, err := newSDKContractClient(t)
	if err != nil {
		t.Fatal(err)
	}
	defer dockerSDK.Close()

	// Verify we can call ContainerRemove method
	ctx := context.Background()
	_, err = dockerSDK.ContainerRemove(ctx, "nonexistent-container-test", dockerclient.ContainerRemoveOptions{})

	// We expect an error since container doesn't exist, but the method should be callable
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "No such container", "Should fail with 'No such container' error")
}

// TestSDKFallback_ContainerCreateAPI tests that ContainerCreate API is available.
func TestSDKFallback_ContainerCreateAPI(t *testing.T) {
	// This test verifies the SDK create approach would work
	// Use an isolated Engine HTTP fixture, never the developer daemon.
	dockerSDK, err := newSDKContractClient(t)
	if err != nil {
		t.Fatal(err)
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

	// Create only against our isolated HTTP fixture.
	_, err = dockerSDK.ContainerCreate(ctx, dockerclient.ContainerCreateOptions{Config: config, HostConfig: hostConfig})

	require.NoError(t, err)
}

// TestSDKFallback_ContainerStartAPI tests that ContainerStart API is available.
func TestSDKFallback_ContainerStartAPI(t *testing.T) {
	// This test verifies the SDK start approach would work
	// Use an isolated Engine HTTP fixture, never the developer daemon.
	dockerSDK, err := newSDKContractClient(t)
	if err != nil {
		t.Fatal(err)
	}
	defer dockerSDK.Close()

	// Verify we can call ContainerStart method
	ctx := context.Background()
	_, err = dockerSDK.ContainerStart(ctx, "nonexistent-container-test", dockerclient.ContainerStartOptions{})

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

// newSDKContractClient prevents SDK API tests from touching a live daemon.
func newSDKContractClient(t *testing.T) (*dockerclient.Client, error) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", "1.47")
			return
		}
		if strings.HasSuffix(r.URL.Path, "/containers/create") {
			var config struct {
				Image      string
				HostConfig *container.HostConfig
			}
			assert.NoError(t, json.NewDecoder(r.Body).Decode(&config))
			assert.Equal(t, "alpine:latest", config.Image)
			assert.NotNil(t, config.HostConfig)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"created-container"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"No such container: nonexistent-container-test"}`))
	}))
	t.Cleanup(server.Close)
	return dockerclient.New(dockerclient.WithHost("tcp://" + strings.TrimPrefix(server.URL, "http://")))
}
