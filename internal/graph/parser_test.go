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
