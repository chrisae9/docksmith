package update

import (
	"fmt"
	"os"
	"strings"

	"github.com/chis/docksmith/internal/docker"
	"gopkg.in/yaml.v3"
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

// parseVersionFromBackup reads a compose backup file and extracts the version for a given service.
// The version is extracted from the image field's tag (e.g., "nginx:1.20.0" -> "1.20.0").
func parseVersionFromBackup(backupPath, serviceName string) (string, error) {
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read backup file: %w", err)
	}

	// Parse the compose file
	var compose struct {
		Services map[string]struct {
			Image string `yaml:"image"`
		} `yaml:"services"`
	}

	if err := yaml.Unmarshal(data, &compose); err != nil {
		return "", fmt.Errorf("failed to parse backup YAML: %w", err)
	}

	// Find the service
	service, ok := compose.Services[serviceName]
	if !ok {
		return "", fmt.Errorf("service %s not found in backup", serviceName)
	}

	// Check if image field exists
	if service.Image == "" {
		return "", fmt.Errorf("service %s has no image field", serviceName)
	}

	// Extract version from image tag
	// Format: [registry/]image[:tag] or [registry/]image[@digest]
	image := service.Image

	// Handle digest reference
	if idx := strings.LastIndex(image, "@"); idx != -1 {
		return image[idx+1:], nil
	}

	// Handle tag reference
	if idx := strings.LastIndex(image, ":"); idx != -1 {
		// Make sure the colon is not part of port (e.g., registry:5000/image)
		afterColon := image[idx+1:]
		// If it contains a slash, it's part of the image path, not a tag
		if !strings.Contains(afterColon, "/") {
			return afterColon, nil
		}
	}

	// No explicit tag means :latest
	return "latest", nil
}
