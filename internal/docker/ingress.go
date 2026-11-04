package docker

import "strings"

// IngressCandidate is the subset of container identity needed to classify ingress.
type IngressCandidate struct {
	Name        string
	Image       string
	Labels      map[string]string
	ID          string
	NetworkMode string // Docker HostConfig.NetworkMode, e.g. "container:<id>"
}

// IsIngressContainer reports whether recreating this container takes down the
// Docksmith UI's own route: a Traefik or Tailscale instance.
func IsIngressContainer(name, image string) bool {
	repo := imageRepository(image)
	if repo == "traefik" || repo == "tailscale" {
		return true
	}

	lowerName := strings.ToLower(name)
	if lowerName == "traefik" || lowerName == "tailscale" {
		return true
	}
	for _, prefix := range []string{"traefik-", "traefik_", "tailscale-", "tailscale_"} {
		if strings.HasPrefix(lowerName, prefix) {
			return true
		}
	}

	return false
}

// ComputeIngress returns a set of container names that serve as (or depend on)
// the UI's ingress path.
func ComputeIngress(candidates []IngressCandidate) map[string]bool {
	ingress := make(map[string]bool, len(candidates))

	for _, c := range candidates {
		if IsIngressContainer(c.Name, c.Image) {
			ingress[c.Name] = true
		}
	}

	// A container that shares the network namespace of an ingress container
	// goes down with it, transitively.
	changed := true
	for changed {
		changed = false
		for _, c := range candidates {
			if ingress[c.Name] {
				continue
			}
			target := networkModeTarget(c.Labels)
			targetID := containerNetworkModeID(c.NetworkMode)
			if target == "" && targetID == "" {
				continue
			}
			for _, other := range candidates {
				if !ingress[other.Name] {
					continue
				}
				labelMatch := target != "" &&
					(target == other.Name || target == other.Labels["com.docker.compose.service"])
				idMatch := targetID != "" && containerIDMatches(targetID, other.ID)
				if labelMatch || idMatch {
					ingress[c.Name] = true
					changed = true
					break
				}
			}
		}
	}

	return ingress
}

// imageRepository reduces an image reference to its bare repository name:
// the digest and tag are stripped and only the last path segment is kept.
func imageRepository(image string) string {
	if idx := strings.Index(image, "@"); idx >= 0 {
		image = image[:idx]
	}
	if idx := strings.LastIndex(image, "/"); idx >= 0 {
		image = image[idx+1:]
	}
	if idx := strings.LastIndex(image, ":"); idx >= 0 {
		image = image[:idx]
	}
	return strings.ToLower(image)
}

// networkModeTarget extracts the referenced container from a compose
// network_mode label, which is either "service:<name>" or "container:<name>".
func networkModeTarget(labels map[string]string) string {
	mode := labels["com.docker.compose.network_mode"]
	switch {
	case strings.HasPrefix(mode, "service:"):
		return strings.TrimPrefix(mode, "service:")
	case strings.HasPrefix(mode, "container:"):
		return strings.TrimPrefix(mode, "container:")
	}
	return ""
}

// containerNetworkModeID extracts the container ID from a Docker
// HostConfig.NetworkMode value of the form "container:<id>".
func containerNetworkModeID(networkMode string) string {
	if !strings.HasPrefix(networkMode, "container:") {
		return ""
	}
	return strings.TrimPrefix(networkMode, "container:")
}

// containerIDMatches reports whether two container ID references identify the
// same container, allowing short IDs to match their full ID in either direction.
func containerIDMatches(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}
