package graph

import (
	"strings"

	"github.com/chis/docksmith/internal/docker"
)

const (
	// DependsOnLabel is the Docker Compose label for dependencies
	DependsOnLabel = "com.docker.compose.depends_on"

	// ProjectLabel groups containers into stacks
	ProjectLabel = "com.docker.compose.project"

	// ServiceLabel identifies the service name within a compose project
	ServiceLabel = "com.docker.compose.service"

	// NetworkModeLabel identifies network_mode dependencies (e.g., "service:tailscale")
	NetworkModeLabel = "com.docker.compose.network_mode"
)

// Builder constructs dependency graphs from container data.
type Builder struct{}

// NewBuilder creates a new graph builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// BuildFromContainers creates a dependency graph from a list of containers.
// It parses Docker Compose labels to identify dependencies.
func (b *Builder) BuildFromContainers(containers []docker.Container) *Graph {
	graph := NewGraph()

	// First pass: create nodes for all containers
	for _, container := range containers {
		node := b.containerToNode(container)
		graph.AddNode(node)
	}

	return graph
}

// containerToNode converts a Docker container to a graph node.
func (b *Builder) containerToNode(container docker.Container) *Node {
	node := &Node{
		ID:           container.Name,
		Dependencies: b.parseDependencies(container.Labels),
		Metadata: map[string]string{
			"id":           container.ID,
			"image":        container.Image,
			"state":        container.State,
			"project":      container.Labels[ProjectLabel],
			"service":      container.Labels[ServiceLabel],
			"network_mode": container.Labels[NetworkModeLabel],
		},
	}

	return node
}

// parseDependencies extracts dependency names from Docker Compose labels.
// Includes both depends_on and network_mode dependencies.
// Format: "service1:condition:value,service2:condition:value"
// Example: "vpn:service_started:false,torrent:service_started:false"
func (b *Builder) parseDependencies(labels map[string]string) []string {
	deps := []string{}

	// Parse depends_on label
	dependsOn, exists := labels[DependsOnLabel]
	if exists && dependsOn != "" {
		deps = append(deps, ParseDependsOn(dependsOn)...)
	}

	// Parse network_mode label (e.g., "service:tailscale" -> depends on tailscale)
	networkModeDep := b.parseNetworkModeDependency(labels)
	if networkModeDep != "" {
		// Avoid duplicates
		isDuplicate := false
		for _, d := range deps {
			if d == networkModeDep {
				isDuplicate = true
				break
			}
		}
		if !isDuplicate {
			deps = append(deps, networkModeDep)
		}
	}

	return deps
}

// parseNetworkModeDependency extracts service dependency from network_mode label.
// Returns the service name if network_mode is "service:X", empty string otherwise.
func (b *Builder) parseNetworkModeDependency(labels map[string]string) string {
	networkMode := labels[NetworkModeLabel]
	if strings.HasPrefix(networkMode, "service:") {
		return strings.TrimPrefix(networkMode, "service:")
	}
	return ""
}

// ParseDependsOn parses a depends_on label value into service names.
// Format: "service1:condition:value,service2:condition:value"
// Example: "vpn:service_started:false,torrent:service_started:false"
func ParseDependsOn(dependsOn string) []string {
	if dependsOn == "" {
		return []string{}
	}

	var dependencies []string
	parts := strings.Split(dependsOn, ",")

	for _, part := range parts {
		// Each part is "service:condition:value"
		subParts := strings.Split(strings.TrimSpace(part), ":")
		if len(subParts) > 0 {
			serviceName := strings.TrimSpace(subParts[0])
			if serviceName != "" {
				dependencies = append(dependencies, serviceName)
			}
		}
	}

	return dependencies
}