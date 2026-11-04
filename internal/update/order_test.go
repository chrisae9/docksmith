package update

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/graph"
)

const (
	tailscaleFullID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	gluetunFullID   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func batchNames(containers []*docker.Container) []string {
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name
	}
	return names
}

// batchGraph builds the Compose dependency graph shape produced by
// graph.Builder for the given container name -> dependencies map.
func batchGraph(deps map[string][]string) *graph.Graph {
	g := graph.NewGraph()
	for name, d := range deps {
		g.AddNode(&graph.Node{ID: name, Dependencies: d})
	}
	return g
}

// With no edges, ingress containers still go down last, preserving the input
// order within each group.
func TestOrderUpdateBatch_IngressLastWhenIndependent(t *testing.T) {
	in := []*docker.Container{
		{Name: "tailscale", Image: "tailscale/tailscale:latest"},
		{Name: "kometa", Image: "kometar/kometa:latest"},
		{Name: "traefik-ts", Image: "ghcr.io/traefik/traefik:v3"},
		{Name: "flaresolverr", Image: "flaresolverr/flaresolverr:latest"},
		{Name: "docksmith", Image: "ghcr.io/rooday/docksmith:1.2.3"},
	}

	got := orderUpdateBatch(in, batchGraph(nil))

	assert.Equal(t, []string{"kometa", "flaresolverr", "docksmith", "tailscale", "traefik-ts"}, batchNames(got))
}

// A Compose depends_on edge wins over the ingress-last preference: a dependent
// must never be recreated before the ingress container it depends on.
func TestOrderUpdateBatch_DependsOnBeatsIngressLast(t *testing.T) {
	app := &docker.Container{ID: "app-id", Name: "app", Image: "example/app:1"}
	traefik := &docker.Container{ID: "traefik-id", Name: "traefik", Image: "traefik:v3"}

	got := orderUpdateBatch(
		[]*docker.Container{app, traefik},
		batchGraph(map[string][]string{"app": {"traefik"}}),
	)

	assert.Equal(t, []string{"traefik", "app"}, batchNames(got))
}

// The reviewer's mixed case: app depends_on db (Compose), and db uses the
// namespace of vpn (Docker network mode). vpn must precede db, and db must
// precede app, even though the input starts in dependency order for depends_on.
func TestOrderUpdateBatch_MixedDependencyKinds(t *testing.T) {
	db := &docker.Container{ID: "db-id", Name: "db", NetworkMode: "container:" + gluetunFullID}
	app := &docker.Container{ID: "app-id", Name: "app"}
	vpn := &docker.Container{ID: gluetunFullID, Name: "vpn"}

	got := orderUpdateBatch(
		[]*docker.Container{db, app, vpn},
		batchGraph(map[string][]string{"app": {"db"}}),
	)

	assert.Equal(t, []string{"vpn", "db", "app"}, batchNames(got))
}

// Network-mode providers precede their dependents in all identity forms the
// Docker API can report: full ID, short ID prefix, and container name.
func TestOrderUpdateBatch_ProviderBeforeDependent(t *testing.T) {
	tests := []struct {
		name      string
		dependent *docker.Container
		provider  *docker.Container
		want      []string
	}{
		{
			name:      "full id",
			dependent: &docker.Container{ID: "traefik-ts-id", Name: "traefik-ts", NetworkMode: "container:" + tailscaleFullID},
			provider:  &docker.Container{ID: tailscaleFullID, Name: "tailscale"},
			want:      []string{"tailscale", "traefik-ts"},
		},
		{
			name:      "short id prefix",
			dependent: &docker.Container{ID: "qbittorrent-id", Name: "qbittorrent", NetworkMode: "container:" + gluetunFullID[:12]},
			provider:  &docker.Container{ID: gluetunFullID, Name: "gluetun"},
			want:      []string{"gluetun", "qbittorrent"},
		},
		{
			name:      "provider short id in batch",
			dependent: &docker.Container{ID: "dependent-id", Name: "dependent", NetworkMode: "container:" + tailscaleFullID},
			provider:  &docker.Container{ID: tailscaleFullID[:12], Name: "tailscale"},
			want:      []string{"tailscale", "dependent"},
		},
		{
			name:      "name reference",
			dependent: &docker.Container{ID: "app-id", Name: "app", NetworkMode: "container:provider-name"},
			provider:  &docker.Container{ID: "cccccccccccccccccccccccccccccccccccccccc", Name: "provider-name"},
			want:      []string{"provider-name", "app"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := orderUpdateBatch([]*docker.Container{tt.dependent, tt.provider}, batchGraph(nil))
			assert.Equal(t, tt.want, batchNames(got))
		})
	}
}

// A stale target (provider not in this batch) must not reorder anything.
func TestOrderUpdateBatch_StaleTargetPreservesInputOrder(t *testing.T) {
	first := &docker.Container{ID: "first-id", Name: "first", NetworkMode: "container:deadbeefdeadbeef"}
	second := &docker.Container{ID: "second-id", Name: "second"}

	got := orderUpdateBatch([]*docker.Container{first, second}, batchGraph(nil))

	assert.Equal(t, []string{"first", "second"}, batchNames(got))
}

// A chain A <- B <- C is emitted provider-first as A, B, C regardless of the
// input order.
func TestOrderUpdateBatch_NetworkModeChainOfThree(t *testing.T) {
	a := &docker.Container{ID: "aaaaaaaaaaaaaaaa", Name: "a"}
	b := &docker.Container{ID: "bbbbbbbbbbbbbbbb", Name: "b", NetworkMode: "container:aaaaaaaaaaaaaaaa"}
	c := &docker.Container{ID: "cccccccccccccccc", Name: "c", NetworkMode: "container:bbbbbbbbbbbbbbbb"}

	got := orderUpdateBatch([]*docker.Container{c, b, a}, batchGraph(nil))

	assert.Equal(t, []string{"a", "b", "c"}, batchNames(got))
}

// Cycles across either edge kind must terminate and must not drop containers.
func TestOrderUpdateBatch_CycleTerminates(t *testing.T) {
	x := &docker.Container{ID: "xxxxxxxxxxxxxxxx", Name: "x", NetworkMode: "container:yyyyyyyyyyyyyyyy"}
	y := &docker.Container{ID: "yyyyyyyyyyyyyyyy", Name: "y", NetworkMode: "container:xxxxxxxxxxxxxxxx"}
	in := []*docker.Container{x, y}

	done := make(chan []*docker.Container, 1)
	go func() { done <- orderUpdateBatch(in, batchGraph(nil)) }()

	select {
	case got := <-done:
		require.Len(t, got, 2)
		assert.ElementsMatch(t, []string{"x", "y"}, batchNames(got))
	case <-time.After(2 * time.Second):
		t.Fatal("orderUpdateBatch did not terminate on a cycle")
	}
}

// The returned slice must be independent of the caller's slice.
func TestOrderUpdateBatch_DoesNotMutateInput(t *testing.T) {
	dependent := &docker.Container{ID: "dependent-id", Name: "dependent", NetworkMode: "container:provider-id"}
	provider := &docker.Container{ID: "provider-id", Name: "provider"}
	in := []*docker.Container{dependent, provider}

	got := orderUpdateBatch(in, batchGraph(nil))

	assert.Equal(t, []string{"dependent", "provider"}, batchNames(in), "input slice must not be reordered")
	assert.Equal(t, []string{"provider", "dependent"}, batchNames(got))
}
