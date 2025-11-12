package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// HTTPClient implements the Client interface using the Docker Registry V2 API.
type HTTPClient struct {
	config     *RegistryConfig
	httpClient *http.Client
}

// NewHTTPClient creates a new registry client.
func NewHTTPClient(config *RegistryConfig) *HTTPClient {
	if config == nil {
		config = &RegistryConfig{
			TimeoutSeconds: 30,
		}
	}

	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = 30
	}

	return &HTTPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
	}
}

// tagsResponse represents the JSON response from the /v2/.../tags/list endpoint.
type tagsResponse struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// ListTags returns all available tags for an image from a registry.
func (c *HTTPClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	// Parse repository to determine registry
	registry, repo := c.parseRepository(repository)

	// Build the API URL
	url := c.buildTagsURL(registry, repo)

	// Make request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication if configured
	if c.config.Username != "" && c.config.Password != "" {
		req.SetBasicAuth(c.config.Username, c.config.Password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("registry returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var tagsResp tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tagsResp.Tags, nil
}

// GetLatestTag returns the most recent tag (currently just returns "latest").
// TODO: Implement logic to find the newest versioned tag.
func (c *HTTPClient) GetLatestTag(ctx context.Context, repository string) (string, error) {
	tags, err := c.ListTags(ctx, repository)
	if err != nil {
		return "", err
	}

	// Look for "latest" tag
	for _, tag := range tags {
		if tag == "latest" {
			return tag, nil
		}
	}

	// If no "latest" tag, return the first tag
	if len(tags) > 0 {
		return tags[0], nil
	}

	return "", fmt.Errorf("no tags found for repository %s", repository)
}

// GetTagDigest returns the SHA256 digest for a specific tag.
// TODO: Implement digest fetching for generic registries
func (c *HTTPClient) GetTagDigest(ctx context.Context, repository, tag string) (string, error) {
	return "", fmt.Errorf("digest fetching not yet implemented for generic registries")
}

// ListTagsWithDigests is not implemented for generic HTTP client.
// This method is only efficiently supported by Docker Hub and GHCR clients.
func (c *HTTPClient) ListTagsWithDigests(ctx context.Context, repository string) (map[string][]string, error) {
	return nil, fmt.Errorf("ListTagsWithDigests not implemented for generic registry client")
}

// parseRepository extracts registry and repository from a full path.
// Examples:
//   - "linuxserver/plex" -> "docker.io", "linuxserver/plex"
//   - "ghcr.io/linuxserver/plex" -> "ghcr.io", "linuxserver/plex"
func (c *HTTPClient) parseRepository(repository string) (registry, repo string) {
	// For now, simple implementation
	// This will be enhanced with proper parsing logic
	return "docker.io", repository
}

// buildTagsURL constructs the registry API URL for listing tags.
func (c *HTTPClient) buildTagsURL(registry, repository string) string {
	protocol := "https"
	if c.config.Insecure {
		protocol = "http"
	}

	// Different registries have different endpoints
	switch registry {
	case "docker.io":
		// Docker Hub uses a different API
		return fmt.Sprintf("%s://hub.docker.com/v2/repositories/%s/tags", protocol, repository)
	case "ghcr.io":
		return fmt.Sprintf("%s://%s/v2/%s/tags/list", protocol, registry, repository)
	default:
		// Standard Docker Registry V2 API
		return fmt.Sprintf("%s://%s/v2/%s/tags/list", protocol, registry, repository)
	}
}
