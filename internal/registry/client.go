package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	maxRetries    = 3
	initialBackoff = 1 * time.Second
)

// HTTPClient implements the Client interface using the Docker Registry V2 API.
type HTTPClient struct {
	config     *RegistryConfig
	httpClient *http.Client
	registry   string // The registry this client is configured for (e.g., "lscr.io")
}

// NewHTTPClient creates a new registry client.
func NewHTTPClient(config *RegistryConfig) *HTTPClient {
	return NewHTTPClientForRegistry(config, "")
}

// NewHTTPClientForRegistry creates a new registry client for a specific registry.
func NewHTTPClientForRegistry(config *RegistryConfig, registry string) *HTTPClient {
	if config == nil {
		config = &RegistryConfig{
			TimeoutSeconds: DefaultTimeoutSeconds,
		}
	}

	if config.TimeoutSeconds == 0 {
		config.TimeoutSeconds = DefaultTimeoutSeconds
	}

	return &HTTPClient{
		config: config,
		httpClient: &http.Client{
			Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
		},
		registry: registry,
	}
}

// doWithRetry executes an HTTP request with exponential backoff retry on transient errors.
// It retries network errors (connection refused, timeout) but not HTTP error responses.
func (c *HTTPClient) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s
			backoff := initialBackoff * time.Duration(1<<(attempt-1))
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(backoff):
			}
		}

		resp, err := c.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}

		// Check if context was cancelled
		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}

		lastErr = err
	}

	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
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

	// Make initial request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Add authentication if configured
	if c.config.Username != "" && c.config.Password != "" {
		req.SetBasicAuth(c.config.Username, c.config.Password)
	}

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tags: %w", err)
	}
	defer resp.Body.Close()

	// Handle 401 Unauthorized - try to get a token
	if resp.StatusCode == http.StatusUnauthorized {
		token, err := c.getAuthToken(ctx, resp, repo)
		if err != nil {
			return nil, fmt.Errorf("failed to authenticate: %w", err)
		}

		// Retry with token
		req, err = http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)

		resp, err = c.doWithRetry(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tags: %w", err)
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != http.StatusOK {
		return nil, handleHTTPError(resp, "fetch tags")
	}

	// Parse response
	var tagsResp tagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return tagsResp.Tags, nil
}

// getAuthToken obtains a bearer token from a registry's token service.
// It parses the WWW-Authenticate header from a 401 response to find the token endpoint.
func (c *HTTPClient) getAuthToken(ctx context.Context, resp *http.Response, repository string) (string, error) {
	authHeader := resp.Header.Get("WWW-Authenticate")
	if authHeader == "" {
		return "", fmt.Errorf("no WWW-Authenticate header in 401 response")
	}

	// Parse WWW-Authenticate header
	// Format: Bearer realm="https://auth.example.io/token",service="example.io",scope="repository:user/image:pull"
	realm := extractAuthParam(authHeader, "realm")
	service := extractAuthParam(authHeader, "service")
	scope := extractAuthParam(authHeader, "scope")

	if realm == "" {
		return "", fmt.Errorf("no realm found in WWW-Authenticate header: %s", authHeader)
	}

	// Build token URL
	tokenURL := realm
	params := []string{}
	if service != "" {
		params = append(params, "service="+service)
	}
	if scope != "" {
		params = append(params, "scope="+scope)
	} else {
		// Default scope for pulling
		params = append(params, "scope=repository:"+repository+":pull")
	}

	if len(params) > 0 {
		tokenURL += "?" + strings.Join(params, "&")
	}

	// Request token
	req, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	tokenResp, err := c.doWithRetry(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch token: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(tokenResp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", tokenResp.StatusCode, string(body))
	}

	// Parse token response
	var tokenData struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	// Some registries use "token", others use "access_token"
	token := tokenData.Token
	if token == "" {
		token = tokenData.AccessToken
	}
	if token == "" {
		return "", fmt.Errorf("no token in response")
	}

	return token, nil
}

// extractAuthParam extracts a parameter value from a WWW-Authenticate header.
func extractAuthParam(header, param string) string {
	// Match param="value" or param='value'
	re := regexp.MustCompile(param + `="([^"]*)"`)
	matches := re.FindStringSubmatch(header)
	if len(matches) > 1 {
		return matches[1]
	}
	return ""
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
// If the client has a configured registry, it uses that.
// Examples:
//   - "linuxserver/plex" with registry="lscr.io" -> "lscr.io", "linuxserver/plex"
//   - "linuxserver/plex" with registry="" -> "docker.io", "linuxserver/plex"
func (c *HTTPClient) parseRepository(repository string) (registry, repo string) {
	// Use the configured registry if set
	if c.registry != "" {
		return c.registry, repository
	}
	// Fall back to docker.io for backwards compatibility
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
