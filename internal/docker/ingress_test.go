package docker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsIngressContainer(t *testing.T) {
	assert.True(t, IsIngressContainer("traefik", "traefik:v3.1"))
	assert.True(t, IsIngressContainer("traefik-ts", "ghcr.io/traefik/traefik:v3.1"))
	assert.True(t, IsIngressContainer("tailscale", "tailscale/tailscale:latest"))
	assert.False(t, IsIngressContainer("qbittorrent", "lscr.io/linuxserver/qbittorrent:latest"))
	assert.False(t, IsIngressContainer("my-traefik-admin", "nginx:latest"))
}

func TestComputeIngress_TraefikEnabledAppIsNotIngress(t *testing.T) {
	got := ComputeIngress([]IngressCandidate{
		{Name: "traefik", Image: "traefik:v3.1", Labels: map[string]string{"com.docker.compose.service": "traefik"}},
		{Name: "kometa", Image: "kometar/kometa:latest", Labels: map[string]string{
			"com.docker.compose.service": "kometa",
			"traefik.enable":             "true",
		}},
	})
	assert.True(t, got["traefik"])
	assert.False(t, got["kometa"], "a container merely routed by Traefik is not ingress")
}

func TestComputeIngress_FollowsNetworkMode(t *testing.T) {
	got := ComputeIngress([]IngressCandidate{
		{Name: "tailscale", Image: "tailscale/tailscale:latest", Labels: map[string]string{"com.docker.compose.service": "tailscale"}},
		{Name: "traefik-ts", Image: "nginx:latest", Labels: map[string]string{
			"com.docker.compose.service":      "traefik-ts",
			"com.docker.compose.network_mode": "service:tailscale",
		}},
		{Name: "qbittorrent", Image: "lscr.io/linuxserver/qbittorrent:latest", Labels: map[string]string{
			"com.docker.compose.service":      "qbittorrent",
			"com.docker.compose.network_mode": "service:gluetun",
		}},
	})
	assert.True(t, got["tailscale"])
	assert.True(t, got["traefik-ts"], "a container sharing an ingress container's netns is ingress")
	assert.False(t, got["qbittorrent"])
}
