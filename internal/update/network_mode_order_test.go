package update

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chis/docksmith/internal/docker"
)

const (
	tailscaleFullID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	gluetunFullID   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func networkModeOrderNames(containers []*docker.Container) []string {
	names := make([]string, len(containers))
	for i, c := range containers {
		names[i] = c.Name
	}
	return names
}

// tailscale is the network namespace provider for traefik-ts. Given in the
// wrong order (dependent first), the provider must be emitted first.
func TestOrderByNetworkMode_ProviderBeforeDependent(t *testing.T) {
	traefikTS := &docker.Container{ID: "traefik-ts-id", Name: "traefik-ts", NetworkMode: "container:" + tailscaleFullID}
	tailscale := &docker.Container{ID: tailscaleFullID, Name: "tailscale"}

	got := orderByNetworkMode([]*docker.Container{traefikTS, tailscale})

	assert.Equal(t, []string{"tailscale", "traefik-ts"}, networkModeOrderNames(got))
}

// gluetun provides the namespace for qbittorrent. Short-ID prefix form.
func TestOrderByNetworkMode_GluetunBeforeQbittorrent(t *testing.T) {
	qbittorrent := &docker.Container{ID: "qbittorrent-id", Name: "qbittorrent", NetworkMode: "container:" + gluetunFullID[:12]}
	gluetun := &docker.Container{ID: gluetunFullID, Name: "gluetun"}

	got := orderByNetworkMode([]*docker.Container{qbittorrent, gluetun})

	assert.Equal(t, []string{"gluetun", "qbittorrent"}, networkModeOrderNames(got))
}

// Compose can record the dependency by container name too.
func TestOrderByNetworkMode_NameReference(t *testing.T) {
	app := &docker.Container{ID: "app-id", Name: "app", NetworkMode: "container:provider-name"}
	provider := &docker.Container{ID: "cccccccccccccccccccccccccccccccccccccccc", Name: "provider-name"}

	got := orderByNetworkMode([]*docker.Container{app, provider})

	assert.Equal(t, []string{"provider-name", "app"}, networkModeOrderNames(got))
}

// The provider reference is longer than the batch member's ID (full ID vs
// short ID in the other direction).
func TestOrderByNetworkMode_ProviderShortIDInBatch(t *testing.T) {
	dependent := &docker.Container{ID: "dependent-id", Name: "dependent", NetworkMode: "container:" + tailscaleFullID}
	provider := &docker.Container{ID: tailscaleFullID[:12], Name: "tailscale"}

	got := orderByNetworkMode([]*docker.Container{dependent, provider})

	assert.Equal(t, []string{"tailscale", "dependent"}, networkModeOrderNames(got))
}

// A stale target (provider not in this batch) must not reorder anything.
func TestOrderByNetworkMode_StaleTargetPreservesInputOrder(t *testing.T) {
	first := &docker.Container{ID: "first-id", Name: "first", NetworkMode: "container:deadbeefdeadbeef"}
	second := &docker.Container{ID: "second-id", Name: "second"}

	got := orderByNetworkMode([]*docker.Container{first, second})

	assert.Equal(t, []string{"first", "second"}, networkModeOrderNames(got))
}

// A two-node cycle must terminate and must not drop any container.
func TestOrderByNetworkMode_CycleTerminates(t *testing.T) {
	x := &docker.Container{ID: "xxxxxxxxxxxxxxxx", Name: "x", NetworkMode: "container:yyyyyyyyyyyyyyyy"}
	y := &docker.Container{ID: "yyyyyyyyyyyyyyyy", Name: "y", NetworkMode: "container:xxxxxxxxxxxxxxxx"}
	in := []*docker.Container{x, y}

	done := make(chan []*docker.Container, 1)
	go func() { done <- orderByNetworkMode(in) }()

	select {
	case got := <-done:
		require.Len(t, got, 2)
		assert.ElementsMatch(t, []string{"x", "y"}, networkModeOrderNames(got))
	case <-time.After(2 * time.Second):
		t.Fatal("orderByNetworkMode did not terminate on a cycle")
	}
}

// A chain A <- B <- C is emitted provider-first as A, B, C regardless of the
// input order.
func TestOrderByNetworkMode_ChainOfThree(t *testing.T) {
	a := &docker.Container{ID: "aaaaaaaaaaaaaaaa", Name: "a"}
	b := &docker.Container{ID: "bbbbbbbbbbbbbbbb", Name: "b", NetworkMode: "container:aaaaaaaaaaaaaaaa"}
	c := &docker.Container{ID: "cccccccccccccccc", Name: "c", NetworkMode: "container:bbbbbbbbbbbbbbbb"}

	got := orderByNetworkMode([]*docker.Container{c, b, a})

	assert.Equal(t, []string{"a", "b", "c"}, networkModeOrderNames(got))
}

// The returned slice must be independent of the caller's slice.
func TestOrderByNetworkMode_DoesNotMutateInput(t *testing.T) {
	dependent := &docker.Container{ID: "dependent-id", Name: "dependent", NetworkMode: "container:provider-id"}
	provider := &docker.Container{ID: "provider-id", Name: "provider"}
	in := []*docker.Container{dependent, provider}

	got := orderByNetworkMode(in)

	assert.Equal(t, []string{"dependent", "provider"}, networkModeOrderNames(in), "input slice must not be reordered")
	assert.Equal(t, []string{"provider", "dependent"}, networkModeOrderNames(got))
}
