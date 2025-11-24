package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// DockerHubClient implements the Client interface for Docker Hub.
type DockerHubClient struct {
	httpClient  *http.Client
	rateLimiter *time.Ticker
}

// NewDockerHubClient creates a new Docker Hub client.
func NewDockerHubClient() *DockerHubClient {
	return &DockerHubClient{
		httpClient: &http.Client{
			Timeout: DefaultHTTPTimeout,
		},
		rateLimiter: time.NewTicker(DefaultRateLimitInterval), // 10 requests per second max
	}
}

// getMaxPages determines optimal page limit based on repository type
// Official images (library/*) tend to have many tags, user images have fewer
func (c *DockerHubClient) getMaxPages(repository string) int {
	// Official images (library/nginx, library/postgres) - typically 200-500 tags
	if strings.HasPrefix(repository, "library/") {
		return 5
	}

	// Well-known organizations with many tags
	wellKnownOrgs := []string{"linuxserver/", "bitnami/", "homeassistant/"}
	for _, org := range wellKnownOrgs {
		if strings.HasPrefix(repository, org) {
			return 4 // Slightly fewer than official images
		}
	}

	// User repositories - typically < 200 tags
	// Start conservative to reduce API calls
	return 2 // 200 tags should be sufficient for most user repos
}

// dockerHubTagsResponse represents Docker Hub's API response.
type dockerHubTagsResponse struct {
	Count    int              `json:"count"`
	Next     string           `json:"next"`
	Previous string           `json:"previous"`
	Results  []dockerHubTag   `json:"results"`
}

type dockerHubTag struct {
	Name         string            `json:"name"`
	FullSize     int64             `json:"full_size"`
	LastUpdated  string            `json:"last_updated"`
	LastPushed   string            `json:"last_pushed"`
	V2           bool              `json:"v2"`
	Digest       string            `json:"digest"` // Manifest list digest (for multi-arch images)
	Images       []dockerHubImage  `json:"images"`
}

type dockerHubImage struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
	Digest       string `json:"digest"`
	Size         int64  `json:"size"`
}

// ListTags returns all available tags for a Docker Hub image.
// Repository format: "namespace/repository" (e.g., "library/nginx", "linuxserver/plex")
func (c *DockerHubClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	// Docker Hub API endpoint
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", repository)

	tags := []string{}
	// Adaptive maxPages based on repository type (reduces API calls for smaller repos)
	maxPages := c.getMaxPages(repository)
	pageCount := 0

	for url != "" && pageCount < maxPages {
		// Rate limiting
		<-c.rateLimiter.C

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tags: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, handleHTTPError(resp, "docker hub tags request")
		}

		var tagsResp dockerHubTagsResponse
		if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, tag := range tagsResp.Results {
			tags = append(tags, tag.Name)
		}

		// Handle pagination
		url = tagsResp.Next
		pageCount++

		// If we got less than 100 results, we've reached the end
		if len(tagsResp.Results) < 100 {
			break
		}
	}

	return tags, nil
}

// GetLatestTag returns "latest" if it exists, otherwise the first tag.
func (c *DockerHubClient) GetLatestTag(ctx context.Context, repository string) (string, error) {
	tags, err := c.ListTags(ctx, repository)
	if err != nil {
		return "", err
	}

	for _, tag := range tags {
		if tag == "latest" {
			return tag, nil
		}
	}

	if len(tags) > 0 {
		return tags[0], nil
	}

	return "", fmt.Errorf("no tags found")
}

// GetTagDigest returns the SHA256 digest for a specific tag.
// Uses Docker Hub's v2 registry API to fetch the manifest digest.
func (c *DockerHubClient) GetTagDigest(ctx context.Context, repository, tag string) (string, error) {
	// Normalize repository (add library/ prefix for official images)
	if len(repository) > 0 && repository[0] != '/' && len(repository) < 256 {
		hasSlash := false
		for i := 0; i < len(repository); i++ {
			if repository[i] == '/' {
				hasSlash = true
				break
			}
		}
		if !hasSlash {
			repository = "library/" + repository
		}
	}

	// Docker Hub v2 API for manifest
	// First we need to get a token for the repository
	tokenURL := fmt.Sprintf("https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull", repository)

	// Rate limiting for token request
	<-c.rateLimiter.C

	tokenReq, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	tokenResp, err := c.httpClient.Do(tokenReq)
	if err != nil {
		return "", fmt.Errorf("failed to get auth token: %w", err)
	}
	defer tokenResp.Body.Close()

	if tokenResp.StatusCode != http.StatusOK {
		return "", handleHTTPError(tokenResp, "docker hub auth token request")
	}

	var tokenData struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(tokenResp.Body).Decode(&tokenData); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	// Now fetch the manifest with the token
	manifestURL := fmt.Sprintf("https://registry-1.docker.io/v2/%s/manifests/%s", repository, tag)

	// Rate limiting for manifest request
	<-c.rateLimiter.C

	manifestReq, err := http.NewRequestWithContext(ctx, "HEAD", manifestURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create manifest request: %w", err)
	}

	manifestReq.Header.Set("Authorization", "Bearer "+tokenData.Token)
	manifestReq.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

	manifestResp, err := c.httpClient.Do(manifestReq)
	if err != nil {
		return "", fmt.Errorf("failed to fetch manifest: %w", err)
	}
	defer manifestResp.Body.Close()

	if manifestResp.StatusCode != http.StatusOK {
		return "", handleHTTPError(manifestResp, "docker hub manifest request")
	}

	// The digest is in the Docker-Content-Digest header
	digest := manifestResp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("no digest found in response headers")
	}

	return digest, nil
}

// ListTagsWithDigests returns a mapping of tags to their digests.
// This is more efficient than calling GetTagDigest for each tag individually.
func (c *DockerHubClient) ListTagsWithDigests(ctx context.Context, repository string) (map[string][]string, error) {
	// Docker Hub API endpoint
	url := fmt.Sprintf("https://hub.docker.com/v2/repositories/%s/tags?page_size=100", repository)

	tagDigests := make(map[string][]string)
	// Adaptive maxPages based on repository type
	maxPages := c.getMaxPages(repository)
	pageCount := 0

	for url != "" && pageCount < maxPages {
		// Rate limiting
		<-c.rateLimiter.C

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch tags: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, handleHTTPError(resp, "docker hub tags request")
		}

		var tagsResp dockerHubTagsResponse
		if err := json.NewDecoder(resp.Body).Decode(&tagsResp); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, tag := range tagsResp.Results {
			// Collect digests from all architectures for this tag
			var digests []string

			// Add the manifest list digest first (if available) - this is what RepoDigests contains for multi-arch images
			if tag.Digest != "" {
				digests = append(digests, tag.Digest)
			}

			// Also add per-architecture digests for single-arch images
			for _, img := range tag.Images {
				if img.Digest != "" {
					digests = append(digests, img.Digest)
				}
			}
			if len(digests) > 0 {
				tagDigests[tag.Name] = digests
			}
		}

		// Handle pagination
		url = tagsResp.Next
		pageCount++

		// If we got less than 100 results, we've reached the end
		if len(tagsResp.Results) < 100 {
			break
		}
	}

	return tagDigests, nil
}
