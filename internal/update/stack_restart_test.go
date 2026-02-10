package update

import (
	"sort"
	"testing"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/scripts"
)

// TestComputeStackRestartLevels tests the dependency-ordered restart level computation
func TestComputeStackRestartLevels(t *testing.T) {
	orch := &UpdateOrchestrator{}

	tests := []struct {
		name       string
		containers []*docker.Container
		// expected[i] is the set of container names at level i (order within level doesn't matter)
		expected [][]string
	}{
		{
			name: "no dependencies - all in level 0",
			containers: []*docker.Container{
				{Name: "a", Labels: map[string]string{graph.ServiceLabel: "svc-a"}},
				{Name: "b", Labels: map[string]string{graph.ServiceLabel: "svc-b"}},
				{Name: "c", Labels: map[string]string{graph.ServiceLabel: "svc-c"}},
			},
			expected: [][]string{{"a", "b", "c"}},
		},
		{
			name: "linear chain via depends_on",
			containers: []*docker.Container{
				{Name: "app", Labels: map[string]string{
					graph.ServiceLabel:    "app",
					graph.DependsOnLabel: "api:service_started:false",
				}},
				{Name: "api", Labels: map[string]string{
					graph.ServiceLabel:    "api",
					graph.DependsOnLabel: "db:service_started:false",
				}},
				{Name: "db", Labels: map[string]string{
					graph.ServiceLabel: "db",
				}},
			},
			expected: [][]string{{"db"}, {"api"}, {"app"}},
		},
		{
			name: "diamond dependency",
			containers: []*docker.Container{
				{Name: "frontend", Labels: map[string]string{
					graph.ServiceLabel:    "frontend",
					graph.DependsOnLabel: "api:service_started:false,cache:service_started:false",
				}},
				{Name: "api", Labels: map[string]string{
					graph.ServiceLabel:    "api",
					graph.DependsOnLabel: "db:service_started:false",
				}},
				{Name: "cache", Labels: map[string]string{
					graph.ServiceLabel:    "cache",
					graph.DependsOnLabel: "db:service_started:false",
				}},
				{Name: "db", Labels: map[string]string{
					graph.ServiceLabel: "db",
				}},
			},
			expected: [][]string{{"db"}, {"api", "cache"}, {"frontend"}},
		},
		{
			name: "network_mode dependency",
			containers: []*docker.Container{
				{Name: "traefik-ts", Labels: map[string]string{
					graph.ServiceLabel:      "traefik-ts",
					graph.NetworkModeLabel: "service:tailscale",
				}},
				{Name: "tailscale", Labels: map[string]string{
					graph.ServiceLabel: "tailscale",
				}},
			},
			expected: [][]string{{"tailscale"}, {"traefik-ts"}},
		},
		{
			name: "restart-after dependency",
			containers: []*docker.Container{
				{Name: "app", Labels: map[string]string{
					graph.ServiceLabel:          "app",
					scripts.RestartAfterLabel: "db",
				}},
				{Name: "db", Labels: map[string]string{
					graph.ServiceLabel: "db",
				}},
			},
			expected: [][]string{{"db"}, {"app"}},
		},
		{
			name: "mixed dependency types",
			containers: []*docker.Container{
				{Name: "proxy", Labels: map[string]string{
					graph.ServiceLabel:      "proxy",
					graph.NetworkModeLabel: "service:vpn",
					graph.DependsOnLabel:   "web:service_started:false",
				}},
				{Name: "web", Labels: map[string]string{
					graph.ServiceLabel:          "web",
					scripts.RestartAfterLabel: "db",
				}},
				{Name: "vpn", Labels: map[string]string{
					graph.ServiceLabel: "vpn",
				}},
				{Name: "db", Labels: map[string]string{
					graph.ServiceLabel: "db",
				}},
			},
			expected: [][]string{{"vpn", "db"}, {"web"}, {"proxy"}},
		},
		{
			name: "single container",
			containers: []*docker.Container{
				{Name: "solo", Labels: map[string]string{graph.ServiceLabel: "solo"}},
			},
			expected: [][]string{{"solo"}},
		},
		{
			name: "circular dependency breaks into same level",
			containers: []*docker.Container{
				{Name: "a", Labels: map[string]string{
					graph.ServiceLabel:    "a",
					graph.DependsOnLabel: "b:service_started:false",
				}},
				{Name: "b", Labels: map[string]string{
					graph.ServiceLabel:    "b",
					graph.DependsOnLabel: "a:service_started:false",
				}},
			},
			expected: [][]string{{"a", "b"}},
		},
		{
			name: "dependency on container outside the set is ignored",
			containers: []*docker.Container{
				{Name: "app", Labels: map[string]string{
					graph.ServiceLabel:    "app",
					graph.DependsOnLabel: "external-db:service_started:false",
				}},
			},
			expected: [][]string{{"app"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			levels := orch.computeStackRestartLevels(tt.containers)

			if len(levels) != len(tt.expected) {
				t.Fatalf("Expected %d levels, got %d: %v", len(tt.expected), len(levels), levels)
			}

			for i, expectedLevel := range tt.expected {
				actualLevel := levels[i]
				if len(actualLevel) != len(expectedLevel) {
					t.Errorf("Level %d: expected %d containers %v, got %d containers %v",
						i, len(expectedLevel), expectedLevel, len(actualLevel), actualLevel)
					continue
				}

				// Sort both for comparison (order within level is non-deterministic)
				sort.Strings(actualLevel)
				sortedExpected := make([]string, len(expectedLevel))
				copy(sortedExpected, expectedLevel)
				sort.Strings(sortedExpected)

				for j := range sortedExpected {
					if actualLevel[j] != sortedExpected[j] {
						t.Errorf("Level %d: expected %v, got %v", i, sortedExpected, actualLevel)
						break
					}
				}
			}
		})
	}
}
