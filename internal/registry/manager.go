package registry

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Manager routes registry requests to the appropriate client with caching support.
type Manager struct {
	dockerHubClient *DockerHubClient
	ghcrClient      *GHCRClient
	genericClients  map[string]*HTTPClient // registry -> client
	genericClientMu sync.RWMutex
	cache           *RegistryCache
	cacheEnabled    bool
	circuitBreaker  *CircuitBreaker
}

// NewManager creates a new registry manager.
// githubToken is optional and used for GHCR authentication.
func NewManager(githubToken string) *Manager {
	return &Manager{
		dockerHubClient: NewDockerHubClient(),
		ghcrClient:      NewGHCRClient(githubToken),
		genericClients:  make(map[string]*HTTPClient),
		cache:           NewRegistryCache(15 * time.Minute),
		cacheEnabled:    true, // Enable caching by default
		circuitBreaker:  NewCircuitBreaker(),
	}
}

// Close releases resources held by the manager and its clients.
func (m *Manager) Close() {
	m.cache.Stop()
	m.dockerHubClient.Close()
	m.ghcrClient.Close()
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

// GetCircuitBreakerState returns the current state of the circuit breaker for a registry.
func (m *Manager) GetCircuitBreakerState(registry string) CircuitState {
	return m.circuitBreaker.GetState(registry)
}

// ResetCircuitBreaker resets the circuit breaker for a specific registry.
// Use this when you know a registry has recovered and want to clear its failure history.
func (m *Manager) ResetCircuitBreaker(registry string) {
	m.circuitBreaker.Reset(registry)
}

// ResetAllCircuitBreakers resets all circuit breakers.
func (m *Manager) ResetAllCircuitBreakers() {
	m.circuitBreaker.ResetAll()
}

// withCache is a generic cache wrapper that handles the check-fetch-store pattern.
// It checks the cache first, calls the fetch function if not found, and stores the result.
func withCache[T any](m *Manager, cacheKey string, ttl time.Duration, isEmpty func(T) bool, fetch func() (T, error)) (T, error) {
	var zero T

	// Check cache first
	if m.cacheEnabled {
		if cached, found := m.cache.Get(cacheKey); found {
			if val, ok := cached.(T); ok {
				return val, nil
			}
		}
	}

	// Fetch from source
	result, err := fetch()
	if err != nil {
		return zero, err
	}

	// Store in cache if result is not empty
	if m.cacheEnabled && !isEmpty(result) {
		if ttl > 0 {
			m.cache.SetWithTTL(cacheKey, result, ttl)
		} else {
			m.cache.Set(cacheKey, result)
		}
	}

	return result, nil
}

// withCircuitBreaker wraps a registry call with circuit breaker protection.
// It checks if the circuit allows the request, executes it, and records the result.
func withCircuitBreaker[T any](m *Manager, registry string, fetch func() (T, error)) (T, error) {
	var zero T

	// Check if circuit breaker allows this request
	if !m.circuitBreaker.Allow(registry) {
		return zero, fmt.Errorf("%w: %s", ErrCircuitOpen, registry)
	}

	// Execute the request
	result, err := fetch()
	if err != nil {
		m.circuitBreaker.RecordFailure(registry)
		return zero, err
	}

	m.circuitBreaker.RecordSuccess(registry)
	return result, nil
}

// ListTags returns all available tags for an image with caching support.
// The imageRef should be in the format: registry/repository
// Examples:
//   - "docker.io/library/nginx"
//   - "ghcr.io/linuxserver/plex"
//   - "linuxserver/plex" (assumes docker.io)
func (m *Manager) ListTags(ctx context.Context, imageRef string) ([]string, error) {
	registry, repository := m.parseImageRef(imageRef)
	client := m.getClient(registry)

	return withCache(m, fmt.Sprintf("tags:%s", imageRef), 0,
		func(tags []string) bool { return len(tags) == 0 },
		func() ([]string, error) {
			return withCircuitBreaker(m, registry, func() ([]string, error) {
				return client.ListTags(ctx, repository)
			})
		},
	)
}

// GetLatestTag returns the latest tag for an image with caching support.
func (m *Manager) GetLatestTag(ctx context.Context, imageRef string) (string, error) {
	registry, repository := m.parseImageRef(imageRef)
	client := m.getClient(registry)

	return withCache(m, fmt.Sprintf("latest:%s", imageRef), 0,
		func(tag string) bool { return tag == "" },
		func() (string, error) {
			return withCircuitBreaker(m, registry, func() (string, error) {
				return client.GetLatestTag(ctx, repository)
			})
		},
	)
}

// GetTagDigest returns the SHA256 digest for a specific image tag with caching support.
// imageRef format: "registry.io/repository" or "repository" (defaults to docker.io)
func (m *Manager) GetTagDigest(ctx context.Context, imageRef, tag string) (string, error) {
	registry, repo := m.parseImageRef(imageRef)
	client := m.getClient(registry)

	// Use shorter TTL for digests since they can change more frequently for mutable tags like "latest"
	return withCache(m, fmt.Sprintf("digest:%s:%s", imageRef, tag), 5*time.Minute,
		func(digest string) bool { return digest == "" },
		func() (string, error) {
			return withCircuitBreaker(m, registry, func() (string, error) {
				return client.GetTagDigest(ctx, repo, tag)
			})
		},
	)
}

// ListTagsWithDigests returns a mapping of tags to their digests.
// Uses efficient APIs (Docker Hub includes digests in tag list, GHCR uses Packages API).
func (m *Manager) ListTagsWithDigests(ctx context.Context, imageRef string) (map[string][]string, error) {
	registry, repository := m.parseImageRef(imageRef)
	client := m.getClient(registry)

	return withCache(m, fmt.Sprintf("tags-digests:%s", imageRef), 0,
		func(tagDigests map[string][]string) bool { return len(tagDigests) == 0 },
		func() (map[string][]string, error) {
			return withCircuitBreaker(m, registry, func() (map[string][]string, error) {
				return client.ListTagsWithDigests(ctx, repository)
			})
		},
	)
}

// GetGhostTags returns Docker Hub tags that have no published images for a given image.
// Returns nil for non-Docker Hub images (GHCR, etc. don't have ghost tags).
func (m *Manager) GetGhostTags(imageRef string) []string {
	registry, repository := m.parseImageRef(imageRef)
	if registry != "docker.io" {
		return nil
	}
	return m.dockerHubClient.GetGhostTags(repository)
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
		// Use registry-specific generic V2 API client
		return m.getOrCreateGenericClient(registry)
	}
}

// getOrCreateGenericClient returns or creates a generic client for the specified registry.
func (m *Manager) getOrCreateGenericClient(registry string) *HTTPClient {
	// Fast path: check if client exists
	m.genericClientMu.RLock()
	client, exists := m.genericClients[registry]
	m.genericClientMu.RUnlock()
	if exists {
		return client
	}

	// Slow path: create new client
	m.genericClientMu.Lock()
	defer m.genericClientMu.Unlock()

	// Double-check after acquiring write lock
	if client, exists = m.genericClients[registry]; exists {
		return client
	}

	// Create new registry-specific client
	client = NewHTTPClientForRegistry(nil, registry)
	m.genericClients[registry] = client
	return client
}