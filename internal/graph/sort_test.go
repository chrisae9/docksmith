package graph

import (
	"testing"
)

func TestTopologicalSort(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func() *Graph
		wantErr   bool
		validate  func(*testing.T, []string)
	}{
		{
			name: "linear chain: vpn -> torrent -> radarr",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "vpn", Dependencies: []string{}})
				g.AddNode(&Node{ID: "torrent", Dependencies: []string{"vpn"}})
				g.AddNode(&Node{ID: "radarr", Dependencies: []string{"torrent"}})
				return g
			},
			wantErr: false,
			validate: func(t *testing.T, result []string) {
				// vpn must come before torrent, torrent before radarr
				vpnIdx, torrentIdx, radarrIdx := -1, -1, -1
				for i, id := range result {
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
					t.Error("vpn should come before torrent")
				}
				if torrentIdx > radarrIdx {
					t.Error("torrent should come before radarr")
				}
			},
		},
		{
			name: "diamond: vpn <- torrent, vpn <- radarr, both -> overseerr",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "vpn", Dependencies: []string{}})
				g.AddNode(&Node{ID: "torrent", Dependencies: []string{"vpn"}})
				g.AddNode(&Node{ID: "radarr", Dependencies: []string{"vpn"}})
				g.AddNode(&Node{ID: "overseerr", Dependencies: []string{"torrent", "radarr"}})
				return g
			},
			wantErr: false,
			validate: func(t *testing.T, result []string) {
				vpnIdx, overseerrIdx := -1, -1
				for i, id := range result {
					if id == "vpn" {
						vpnIdx = i
					}
					if id == "overseerr" {
						overseerrIdx = i
					}
				}

				if vpnIdx > overseerrIdx {
					t.Error("vpn should come before overseerr")
				}
			},
		},
		{
			name: "no dependencies",
			setupFunc: func() *Graph {
				g := NewGraph()
				g.AddNode(&Node{ID: "standalone1", Dependencies: []string{}})
				g.AddNode(&Node{ID: "standalone2", Dependencies: []string{}})
				return g
			},
			wantErr: false,
			validate: func(t *testing.T, result []string) {
				if len(result) != 2 {
					t.Errorf("Expected 2 nodes, got %d", len(result))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			graph := tt.setupFunc()
			result, err := graph.TopologicalSort()

			if tt.wantErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestGetRestartOrder(t *testing.T) {
	g := NewGraph()
	g.AddNode(&Node{ID: "vpn", Dependencies: []string{}})
	g.AddNode(&Node{ID: "torrent", Dependencies: []string{"vpn"}})
	g.AddNode(&Node{ID: "radarr", Dependencies: []string{"torrent"}})

	updateOrder, err := g.GetUpdateOrder()
	if err != nil {
		t.Fatalf("Failed to get update order: %v", err)
	}

	restartOrder, err := g.GetRestartOrder()
	if err != nil {
		t.Fatalf("Failed to get restart order: %v", err)
	}

	// Restart order should be reverse of update order
	if len(updateOrder) != len(restartOrder) {
		t.Error("Update and restart orders have different lengths")
	}

	for i := 0; i < len(updateOrder); i++ {
		if updateOrder[i] != restartOrder[len(restartOrder)-1-i] {
			t.Error("Restart order is not reverse of update order")
			break
		}
	}
}
