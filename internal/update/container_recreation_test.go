package update

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	dockerclient "github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRestartContainerWithDependents_ComposeRecreation tests that compose is used for main container.
func TestRestartContainerWithDependents_ComposeRecreation(t *testing.T) {
	// Skip this test since we need actual Docker SDK client which requires Docker daemon
	t.Skip("Skipping test that requires Docker daemon")
}

// TestRestartContainerWithDependents_IncludesDependents tests dependent containers are included in compose command.
func TestRestartContainerWithDependents_IncludesDependents(t *testing.T) {
	// Skip this test since we need actual Docker SDK client which requires Docker daemon
	t.Skip("Skipping test that requires Docker daemon")
}

// TestExtractServiceNamesForCompose tests service name extraction for compose command.
func TestExtractServiceNamesForCompose(t *testing.T) {
	containers := []docker.Container{
		{
			Name: "web",
			Labels: map[string]string{
				"com.docker.compose.service": "web",
			},
		},
		{
			Name: "db",
			Labels: map[string]string{
				"com.docker.compose.service": "db",
			},
		},
		{
			Name: "standalone",
			Labels: map[string]string{
				// No compose service label
			},
		},
	}

	serviceNames := extractServiceNames(containers, []string{"web", "db", "standalone"})

	// Should extract web and db, filter out standalone
	assert.Len(t, serviceNames, 2)
	assert.Contains(t, serviceNames, "web")
	assert.Contains(t, serviceNames, "db")
}

// TestRestartContainerWithDependents_ComposeCommandStructure tests compose command format.
func TestRestartContainerWithDependents_ComposeCommandStructure(t *testing.T) {
	// This test verifies that the compose command is correctly structured.
	// We test the service name extraction which is used to build the compose command.

	tmpDir := t.TempDir()
	composeFile := filepath.Join(tmpDir, "docker-compose.yml")
	composeContent := `services:
  web:
    image: nginx:1.21
  api:
    image: api:1.0
`
	err := os.WriteFile(composeFile, []byte(composeContent), 0644)
	require.NoError(t, err)

	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "web1",
				Name:  "web",
				Image: "nginx:1.20",
				Labels: map[string]string{
					"com.docker.compose.project":              "teststack",
					"com.docker.compose.service":              "web",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
			{
				ID:    "api1",
				Name:  "api",
				Image: "api:1.0",
				Labels: map[string]string{
					"com.docker.compose.project":              "teststack",
					"com.docker.compose.service":              "api",
					"com.docker.compose.project.config_files": composeFile,
				},
			},
		},
	}

	// Test service name extraction for main container
	mainContainer := &mockDocker.containers[0]
	serviceName := extractServiceName(mainContainer)
	assert.Equal(t, "web", serviceName)

	// Test service names extraction for multiple containers
	serviceNames := extractServiceNames(mockDocker.containers, []string{"web", "api"})
	assert.Len(t, serviceNames, 2)
	assert.Contains(t, serviceNames, "web")
	assert.Contains(t, serviceNames, "api")
}

// TestRestartContainerWithDependents_NonComposeFallback tests non-compose fallback behavior.
func TestRestartContainerWithDependents_NonComposeFallback(t *testing.T) {
	mockDocker := &MockDockerClient{
		containers: []docker.Container{
			{
				ID:    "standalone1",
				Name:  "standalone",
				Image: "redis:6",
				Labels: map[string]string{
					// No compose labels
				},
			},
		},
	}

	mockStorage := NewTestMockStorage()
	bus := events.NewBus()

	// Create a mock Docker SDK client
	dockerSDK, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Skip("Docker not available, skipping test")
	}
	defer dockerSDK.Close()

	orch := &UpdateOrchestrator{
		dockerClient: mockDocker,
		dockerSDK:    dockerSDK,
		storage:      mockStorage,
		eventBus:     bus,
		graphBuilder: graph.NewBuilder(),
		stackManager: docker.NewStackManager(),
	}

	// For non-compose containers, SDK fallback should be used
	// Pass empty backupPath since standalone containers don't use compose files
	_, err = orch.restartContainerWithDependents(context.Background(), "test-op-3", "standalone", "", "")

	// Since the container doesn't exist in Docker daemon, this will fail
	// but it should attempt SDK recreation (not fail due to no compose file)
	assert.Error(t, err)
}

// TestExtractServiceName_SingleContainer tests extracting service name from a single container.
func TestExtractServiceName_SingleContainer(t *testing.T) {
	container := &docker.Container{
		Name: "web",
		Labels: map[string]string{
			"com.docker.compose.service": "web-service",
		},
	}

	serviceName := extractServiceName(container)
	assert.Equal(t, "web-service", serviceName)
}

// TestExtractServiceName_NoLabel tests extracting service name when label is missing.
func TestExtractServiceName_NoLabel(t *testing.T) {
	container := &docker.Container{
		Name:   "standalone",
		Labels: map[string]string{},
	}

	serviceName := extractServiceName(container)
	assert.Equal(t, "", serviceName)
}
