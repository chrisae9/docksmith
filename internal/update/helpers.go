package update

import (
	"github.com/chis/docksmith/internal/docker"
)

// extractServiceName extracts the Docker Compose service name from a container's labels.
// Returns empty string if the container is not part of a compose stack.
func extractServiceName(container *docker.Container) string {
	if container == nil {
		return ""
	}
	return container.Labels["com.docker.compose.service"]
}

// extractServiceNames extracts Docker Compose service names for the given container names.
// It filters out containers that are not part of a compose stack (no service label).
func extractServiceNames(containers []docker.Container, containerNames []string) []string {
	// Create a map for quick lookup of container names to include
	includeMap := make(map[string]bool)
	for _, name := range containerNames {
		includeMap[name] = true
	}

	// Extract service names for containers that match and have a service label
	var serviceNames []string
	for _, container := range containers {
		if !includeMap[container.Name] {
			continue
		}
		serviceName := container.Labels["com.docker.compose.service"]
		if serviceName != "" {
			serviceNames = append(serviceNames, serviceName)
		}
	}

	return serviceNames
}
