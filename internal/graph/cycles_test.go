package graph

import (
	"testing"
)

func TestHasCycles(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *Graph
		hasCycle  bool
	}{
		{
			name: "no cycle - linear chain",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "a", Dependencies: []string{}})
				g.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
				g.AddNode(&Node{ID: "c", Dependencies: []string{"b"}})
				return g
			},
			hasCycle: false,
		},
		{
			name: "simple cycle - a -> b -> a",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "a", Dependencies: []string{"b"}})
				g.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
				return g
			},
			hasCycle: true,
		},
		{
			name: "three-node cycle - a -> b -> c -> a",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "a", Dependencies: []string{"c"}})
				g.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
				g.AddNode(&Node{ID: "c", Dependencies: []string{"b"}})
				return g
			},
			hasCycle: true,
		},
		{
			name: "no cycle - diamond",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "a", Dependencies: []string{}})
				g.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
				g.AddNode(&Node{ID: "c", Dependencies: []string{"a"}})
				g.AddNode(&Node{ID: "d", Dependencies: []string{"b", "c"}})
				return g
			},
			hasCycle: false,
		},
		{
			name: "self-loop",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "a", Dependencies: []string{"a"}})
				return g
			},
			hasCycle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := tt.setupFunc()
			result := graph.HasCycles()

			if result != tt.hasCycle {
				t.Errorf("Expected HasCycles=%v, got %v", tt.hasCycle, result)
			}
		})
	}
}

func TestFindCycle(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *Graph
		wantCycle bool
	}{
		{
			name: "no cycle",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "a", Dependencies: []string{}})
				g.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
				return g
			},
			wantCycle: false,
		},
		{
			name: "simple cycle",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "a", Dependencies: []string{"b"}})
				g.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})
				return g
			},
			wantCycle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := tt.setupFunc()
			cycle := graph.FindCycle()

			if tt.wantCycle && cycle == nil {
				t.Error("Expected to find cycle but got nil")
			}
			if !tt.wantCycle && cycle != nil {
				t.Errorf("Expected no cycle but found: %v", cycle)
			}
		})
	}
}

func TestTopologicalSortWithCycle(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "a", Dependencies: []string{"b"}})
	g.AddNode(&Node{ID: "b", Dependencies: []string{"a"}})

	_, err := g.TopologicalSort()
	if err == nil {
		t.Error("Expected error for graph with cycle")
	}
}

func TestRealWorldDiamondPattern(t *testing.T) {
	// Test the exact scenario: radarr→[vpn,torrent], torrent→[vpn], vpn→[]
	// This is a valid diamond pattern, NOT a cycle
	g := NewGraph()
	g.AddNode(&Node{ID: "vpn", Dependencies: []string{}})
	g.AddNode(&Node{ID: "torrent", Dependencies: []string{"vpn"}})
	g.AddNode(&Node{ID: "radarr", Dependencies: []string{"vpn", "torrent"}})

	if g.HasCycles() {
		t.Error("Diamond pattern incorrectly detected as cycle")
		if cycle := g.FindCycle(); cycle != nil {
			t.Errorf("False cycle found: %v", cycle)
		}
	}

	// Should be able to topologically sort
	order, err := g.TopologicalSort()
	if err != nil {
		t.Errorf("Topological sort failed on valid diamond: %v", err)
	}

	// Verify vpn comes before torrent, and both come before radarr
	vpnIdx, torrentIdx, radarrIdx := -1, -1, -1
	for i, id := range order {
		switch id {
		case "vpn":
			vpnIdx = i
		case "torrent":
			torrentIdx = i
		case "radarr":
			radarrIdx = i
		}
	}

	if vpnIdx > torrentIdx {
		t.Error("vpn should come before torrent in update order")
	}
	if vpnIdx > radarrIdx {
		t.Error("vpn should come before radarr in update order")
	}
	if torrentIdx > radarrIdx {
		t.Error("torrent should come before radarr in update order")
	}
}
