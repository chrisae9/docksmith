package update

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/chis/docksmith/internal/docker"
)

func TestOrderIngressLast(t *testing.T) {
	in := []*docker.Container{
		{Name: "tailscale", Image: "tailscale/tailscale:latest"},
		{Name: "kometa", Image: "kometar/kometa:latest"},
		{Name: "traefik-ts", Image: "ghcr.io/traefik/traefik:v3"},
		{Name: "flaresolverr", Image: "flaresolverr/flaresolverr:latest"},
		{Name: "docksmith", Image: "ghcr.io/rooday/docksmith:1.2.3"},
	}
	got := orderIngressLast(in)
	names := make([]string, len(got))
	for i, c := range got {
		names[i] = c.Name
	}
	assert.Equal(t, []string{"kometa", "flaresolverr", "docksmith", "tailscale", "traefik-ts"}, names)
}
