package graph

import (
	"testing"

	"github.com/chis/docksmith/internal/docker"
)

func TestParseDependencies(t *testing.T) {
	builder := NewBuilder()

	tests := []struct {
		name     string
		labels   map[string]string
		expected []string
	}{
		{
			name: "single dependency",
			labels: map[string]string{
				DependsOnLabel: "vpn:service_started:false",
			},
			expected: []string{"vpn"},
		},
		{
			name: "multiple dependencies",
			labels: map[string]string{
				DependsOnLabel: "vpn:service_started:false,torrent:service_started:false",
			},
			expected: []string{"vpn", "torrent"},
		},
		{
			name:     "no dependencies",
			labels:   map[string]string{},
			expected: []string{},
		},
		{
			name: "empty depends_on",
			labels: map[string]string{
				DependsOnLabel: "",
			},
			expected: []string{},
		},
		{
			name: "network_mode service dependency",
			labels: map[string]string{
				NetworkModeLabel: "service:tailscale",
			},
			expected: []string{"tailscale"},
		},
		{
			name: "network_mode container (not service) - no dependency",
			labels: map[string]string{
				NetworkModeLabel: "container:some-container",
			},
			expected: []string{},
		},
		{
			name: "both depends_on and network_mode",
			labels: map[string]string{
				DependsOnLabel:   "db:service_started:false",
				NetworkModeLabel: "service:vpn",
			},
			expected: []string{"db", "vpn"},
		},
		{
			name: "network_mode duplicate with depends_on",
			labels: map[string]string{
				DependsOnLabel:   "tailscale:service_started:false",
				NetworkModeLabel: "service:tailscale",
			},
			expected: []string{"tailscale"}, // Should deduplicate
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := builder.parseDependencies(tt.labels)

			if len(result) != len(tt.expected) {
				t.Errorf("Expected %d dependencies, got %d", len(tt.expected), len(result))
				return
			}

			for i, dep := range result {
				if dep != tt.expected[i] {
					t.Errorf("Dependency %d: expected %s, got %s", i, tt.expected[i], dep)
				}
			}
		})
	}
}

func TestBuildFromContainers(t *testing.T) {
	builder := NewBuilder()

	containers := []docker.Container{
		{
			ID:    "vpn-id",
			Name:  "vpn",
			Image: "gluetun:latest",
			State: "running",
			Labels: map[string]string{
				ProjectLabel: "torrent",
				ServiceLabel: "vpn",
			},
		},
		{
			ID:    "torrent-id",
			Name:  "torrent",
			Image: "qbittorrent:latest",
			State: "running",
			Labels: map[string]string{
				DependsOnLabel: "vpn:service_started:false",
				ProjectLabel:   "torrent",
				ServiceLabel:   "torrent",
			},
		},
	}

	graph := builder.BuildFromContainers(containers)

	// Check nodes exist
	if len(graph.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(graph.Nodes))
	}

	// Check vpn node
	vpnNode, exists := graph.GetNode("vpn")
	if !exists {
		t.Fatal("vpn node not found")
	}
	if len(vpnNode.Dependencies) != 0 {
		t.Errorf("vpn should have 0 dependencies, got %d", len(vpnNode.Dependencies))
	}

	// Check torrent node
	torrentNode, exists := graph.GetNode("torrent")
	if !exists {
		t.Fatal("torrent node not found")
	}
	if len(torrentNode.Dependencies) != 1 {
		t.Fatalf("torrent should have 1 dependency, got %d", len(torrentNode.Dependencies))
	}
	if torrentNode.Dependencies[0] != "vpn" {
		t.Errorf("torrent should depend on vpn, got %s", torrentNode.Dependencies[0])
	}

	// Check adjacency
	dependents := graph.GetDependents("vpn")
	if len(dependents) != 1 || dependents[0] != "torrent" {
		t.Errorf("vpn should have torrent as dependent")
	}
}

func TestBuildFromContainersWithNetworkMode(t *testing.T) {
	builder := NewBuilder()

	// Simulate a traefik stack with tailscale and traefik-ts containers
	// traefik-ts uses network_mode: service:tailscale
	containers := []docker.Container{
		{
			ID:    "tailscale-id",
			Name:  "tailscale",
			Image: "tailscale/tailscale:latest",
			State: "running",
			Labels: map[string]string{
				ProjectLabel: "traefik",
				ServiceLabel: "tailscale",
			},
		},
		{
			ID:    "traefik-ts-id",
			Name:  "traefik-ts",
			Image: "traefik:v3",
			State: "running",
			Labels: map[string]string{
				ProjectLabel:     "traefik",
				ServiceLabel:     "traefik-ts",
				NetworkModeLabel: "service:tailscale", // Key dependency
			},
		},
	}

	graph := builder.BuildFromContainers(containers)

	// Check nodes exist
	if len(graph.Nodes) != 2 {
		t.Errorf("Expected 2 nodes, got %d", len(graph.Nodes))
	}

	// Check tailscale node
	tailscaleNode, exists := graph.GetNode("tailscale")
	if !exists {
		t.Fatal("tailscale node not found")
	}
	if len(tailscaleNode.Dependencies) != 0 {
		t.Errorf("tailscale should have 0 dependencies, got %d", len(tailscaleNode.Dependencies))
	}

	// Check traefik-ts node - should depend on tailscale via network_mode
	traefikTsNode, exists := graph.GetNode("traefik-ts")
	if !exists {
		t.Fatal("traefik-ts node not found")
	}
	if len(traefikTsNode.Dependencies) != 1 {
		t.Fatalf("traefik-ts should have 1 dependency (from network_mode), got %d", len(traefikTsNode.Dependencies))
	}
	if traefikTsNode.Dependencies[0] != "tailscale" {
		t.Errorf("traefik-ts should depend on tailscale, got %s", traefikTsNode.Dependencies[0])
	}

	// Check network_mode is stored in metadata
	if traefikTsNode.Metadata["network_mode"] != "service:tailscale" {
		t.Errorf("traefik-ts network_mode should be 'service:tailscale', got %s", traefikTsNode.Metadata["network_mode"])
	}

	// Check adjacency - tailscale should have traefik-ts as dependent
	dependents := graph.GetDependents("tailscale")
	if len(dependents) != 1 || dependents[0] != "traefik-ts" {
		t.Errorf("tailscale should have traefik-ts as dependent, got %v", dependents)
	}

	// Check update order - tailscale should come before traefik-ts
	updateOrder, err := graph.GetUpdateOrder()
	if err != nil {
		t.Fatalf("GetUpdateOrder failed: %v", err)
	}

	tailscaleIdx := -1
	traefikTsIdx := -1
	for i, name := range updateOrder {
		if name == "tailscale" {
			tailscaleIdx = i
		}
		if name == "traefik-ts" {
			traefikTsIdx = i
		}
	}

	if tailscaleIdx == -1 || traefikTsIdx == -1 {
		t.Fatalf("Could not find both containers in update order: %v", updateOrder)
	}
	if tailscaleIdx >= traefikTsIdx {
		t.Errorf("tailscale should be updated before traefik-ts in update order, got tailscale at %d, traefik-ts at %d", tailscaleIdx, traefikTsIdx)
	}
}
