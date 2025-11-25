package update

import (
	"testing"

	"github.com/chis/docksmith/internal/docker"
	"github.com/stretchr/testify/assert"
)

// Test: extractServiceName returns correct service name from label
func TestExtractServiceName_ReturnsCorrectServiceName(t *testing.T) {
	container := &docker.Container{
		Name: "test-web",
		Labels: map[string]string{
			"com.docker.compose.service": "web",
		},
	}

	serviceName := extractServiceName(container)

	assert.Equal(t, "web", serviceName)
}

// Test: extractServiceName handles missing label gracefully
func TestExtractServiceName_HandlesMissingLabel(t *testing.T) {
	container := &docker.Container{
		Name:   "standalone-container",
		Labels: map[string]string{},
	}

	serviceName := extractServiceName(container)

	assert.Equal(t, "", serviceName)
}

// Test: extractServiceNames collects service names from container list
func TestExtractServiceNames_CollectsServiceNames(t *testing.T) {
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
			Name: "cache",
			Labels: map[string]string{
				"com.docker.compose.service": "cache",
			},
		},
	}

	containerNames := []string{"web", "db"}

	serviceNames := extractServiceNames(containers, containerNames)

	assert.Len(t, serviceNames, 2)
	assert.Contains(t, serviceNames, "web")
	assert.Contains(t, serviceNames, "db")
	assert.NotContains(t, serviceNames, "cache")
}

// Test: extractServiceNames filters out empty strings from non-compose containers
func TestExtractServiceNames_FiltersEmptyStrings(t *testing.T) {
	containers := []docker.Container{
		{
			Name: "web",
			Labels: map[string]string{
				"com.docker.compose.service": "web",
			},
		},
		{
			Name:   "standalone",
			Labels: map[string]string{},
		},
		{
			Name: "api",
			Labels: map[string]string{
				"com.docker.compose.service": "api",
			},
		},
	}

	containerNames := []string{"web", "standalone", "api"}

	serviceNames := extractServiceNames(containers, containerNames)

	assert.Len(t, serviceNames, 2)
	assert.Contains(t, serviceNames, "web")
	assert.Contains(t, serviceNames, "api")
	assert.NotContains(t, serviceNames, "")
}

// NOTE: Tests for runCommandWithStreaming were removed as the function
// does not currently exist in the UpdateOrchestrator. These tests should
// be added back when the streaming functionality is implemented.
