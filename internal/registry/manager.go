package registry

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Manager routes registry requests to the appropriate client with caching support.
type Manager struct {
	dockerHubClient *DockerHubClient
	ghcrClient      *GHCRClient
	genericClient   *HTTPClient
	cache           *RegistryCache
	cacheEnabled    bool
}

// NewManager creates a new registry manager.
// githubToken is optional and used for GHCR authentication.
func NewManager(githubToken string) *Manager {
	return &Manager{
		dockerHubClient: NewDockerHubClient(),
		ghcrClient:      NewGHCRClient(githubToken),
		genericClient:   NewHTTPClient(nil),
		cache:           NewRegistryCache(15 * time.Minute),
		cacheEnabled:    true, // Enable caching by default
	}
}

// EnableCache enables the caching layer
func (m *Manager) EnableCache(enabled bool) {
	m.cacheEnabled = enabled
}

// SetCacheTTL sets the cache TTL
func (m *Manager) SetCacheTTL(ttl time.Duration) {
	m.cache.SetTTL(ttl)
}

// ClearCache clears all cached entries
func (m *Manager) ClearCache() {
	m.cache.Clear()
}

// ListTags returns all available tags for an image with caching support.
// The imageRef should be in the format: registry/repository
// Examples:
//   - "docker.io/library/nginx"
//   - "ghcr.io/linuxserver/plex"
//   - "linuxserver/plex" (assumes docker.io)
func (m *Manager) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	// Check cache first
	if m.cacheEnabled {
		cacheKey := fmt.Sprintf("tags:%s", imageRef)
		if cached, found := m.cache.Get(cacheKey); found {
			if tags, ok := cached.([]string); ok {
				return tags, nil
			}
		}
	}

	registry, repository := m.parseImageRef(imageRef)

	client := m.getClient(registry)
	tags, err := client.ListTags(ctx, repository)
	if err != nil {
		return nil, err
	}

	// Store in cache
	if m.cacheEnabled && len(tags) > 0 {
		cacheKey := fmt.Sprintf("tags:%s", imageRef)
		m.cache.Set(cacheKey, tags)
	}

	return tags, nil
}

// GetLatestTag returns the latest tag for an image with caching support.
func (m *Manager) GetLatestTag(ctx context.Context, imageRef string) (string, error) {
	// Check cache first
	if m.cacheEnabled {
		cacheKey := fmt.Sprintf("latest:%s", imageRef)
		if cached, found := m.cache.Get(cacheKey); found {
			if tag, ok := cached.(string); ok {
				return tag, nil
			}
		}
	}

	registry, repository := m.parseImageRef(imageRef)

	client := m.getClient(registry)
	tag, err := client.GetLatestTag(ctx, repository)
	if err != nil {
		return "", err
	}

	// Store in cache
	if m.cacheEnabled && tag != "" {
		cacheKey := fmt.Sprintf("latest:%s", imageRef)
		m.cache.Set(cacheKey, tag)
	}

	return tag, nil
}

// GetTagDigest returns the SHA256 digest for a specific image tag with caching support.
// imageRef format: "registry.io/repository" or "repository" (defaults to docker.io)
func (m *Manager) GetTagDigest(ctx context.Context, imageRef, tag string) (string, error) {
	// Check cache first
	if m.cacheEnabled {
		cacheKey := fmt.Sprintf("digest:%s:%s", imageRef, tag)
		if cached, found := m.cache.Get(cacheKey); found {
			if digest, ok := cached.(string); ok {
				return digest, nil
			}
		}
	}

	registry, repo := m.parseImageRef(imageRef)
	client := m.getClient(registry)
	digest, err := client.GetTagDigest(ctx, repo, tag)
	if err != nil {
		return "", err
	}

	// Store in cache with a shorter TTL for digests (5 minutes)
	// since they can change more frequently for mutable tags like "latest"
	if m.cacheEnabled && digest != "" {
		cacheKey := fmt.Sprintf("digest:%s:%s", imageRef, tag)
		m.cache.SetWithTTL(cacheKey, digest, 5*time.Minute)
	}

	return digest, nil
}

// ListTagsWithDigests returns a mapping of tags to their digests.
// Uses efficient APIs (Docker Hub includes digests in tag list, GHCR uses Packages API).
func (m *Manager) ListTagsWithDigests(ctx context.Context, imageRef string) (map[string][]string, error) {
	// Check cache first
	if m.cacheEnabled {
		cacheKey := fmt.Sprintf("tags-digests:%s", imageRef)
		if cached, found := m.cache.Get(cacheKey); found {
			if tagDigests, ok := cached.(map[string][]string); ok {
				return tagDigests, nil
			}
		}
	}

	registry, repository := m.parseImageRef(imageRef)
	client := m.getClient(registry)

	tagDigests, err := client.ListTagsWithDigests(ctx, repository)
	if err != nil {
		return nil, err
	}

	// Store in cache
	if m.cacheEnabled && len(tagDigests) > 0 {
		cacheKey := fmt.Sprintf("tags-digests:%s", imageRef)
		m.cache.Set(cacheKey, tagDigests)
	}

	return tagDigests, nil
}

// parseImageRef splits an image reference into registry and repository.
func (m *Manager) parseImageRef(imageRef string) (registry, repository string) {
	parts := strings.Split(imageRef, "/")

	switch len(parts) {
	case 1:
		// Just image name: "nginx" -> docker.io/library/nginx
		return "docker.io", "library/" + parts[0]
	case 2:
		// Check if first part looks like a registry
		if strings.Contains(parts[0], ".") || parts[0] == "localhost" {
			// "ghcr.io/image" or "localhost/image"
			return parts[0], parts[1]
		}
		// "user/image" -> docker.io/user/image
		return "docker.io", imageRef
	default:
		// "registry.com/user/image"
		return parts[0], strings.Join(parts[1:], "/")
	}
}

// getClient returns the appropriate client for the given registry.
func (m *Manager) getClient(registry string) Client {
	switch registry {
	case "docker.io":
		return m.dockerHubClient
	case "ghcr.io":
		return m.ghcrClient
	default:
		// Use generic V2 API client for other registries
		return m.genericClient
	}
}