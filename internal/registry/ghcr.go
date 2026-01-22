package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// GHCRClient implements the Client interface for GitHub Container Registry.
type GHCRClient struct {
	httpClient  *http.Client
	githubPAT   string
	tokenCache  map[string]tokenCacheEntry
	tokenMutex  sync.RWMutex
	rateLimiter *time.Ticker
}

// tokenCacheEntry stores a cached token with expiry.
type tokenCacheEntry struct {
	token     string
	expiresAt time.Time
}

// NewGHCRClient creates a new GHCR client.
// githubPAT is optional - if not provided, will attempt to read from Docker config or use anonymous access.
func NewGHCRClient(githubPAT string) *GHCRClient {
	// If no PAT provided, try to read from Docker config
	if githubPAT == "" {
		githubPAT = readGHCRCredsFromDockerConfig()
	}

	return &GHCRClient{
		httpClient: &http.Client{
			Timeout: DefaultHTTPTimeout,
		},
		githubPAT:   githubPAT,
		tokenCache:  make(map[string]tokenCacheEntry),
		rateLimiter: time.NewTicker(DefaultRateLimitInterval), // 10 requests per second max
	}
}

// doWithRetry executes an HTTP request with exponential backoff retry on transient errors.
func (c *GHCRClient) doWithRetry(req *http.Request) (*http.Response, error) {
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
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

		if req.Context().Err() != nil {
			return nil, req.Context().Err()
		}

		lastErr = err
	}

	return nil, fmt.Errorf("after %d retries: %w", maxRetries, lastErr)
}

// getMaxPages determines optimal page limit based on repository type
// Well-known orgs tend to have many tags, user repos have fewer
func (c *GHCRClient) getMaxPages(repository string, isV2API bool) int {
	// Check for well-known organizations with many tags
	wellKnownOrgs := []string{"linuxserver/", "homeassistant/", "home-assistant/"}
	for _, org := range wellKnownOrgs {
		if strings.HasPrefix(repository, org) {
			if isV2API {
				return 5 // V2 API can handle more
			}
			return 3 // Packages API
		}
	}

	// User repositories - typically fewer tags
	if isV2API {
		return 3 // 300 tags for V2
	}
	return 2 // 200 versions for Packages API
}

// dockerConfigAuth represents auth entry in Docker config
type dockerConfigAuth struct {
	Auth string `json:"auth"`
}

// dockerConfig represents ~/.docker/config.json structure
type dockerConfig struct {
	Auths map[string]dockerConfigAuth `json:"auths"`
}

// readGHCRCredsFromDockerConfig reads GHCR credentials from ~/.docker/config.json
func readGHCRCredsFromDockerConfig() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	configPath := filepath.Join(homeDir, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	var config dockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return ""
	}

	// Look for ghcr.io credentials
	auth, found := config.Auths["ghcr.io"]
	if !found {
		return ""
	}

	// Decode base64 auth (format: username:password)
	decoded, err := base64.StdEncoding.DecodeString(auth.Auth)
	if err != nil {
		return ""
	}

	// Split username:password
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return ""
	}

	// For GHCR, the password is the PAT
	return parts[1]
}

// ghcrTagList represents the tag list response.
type ghcrTagList struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
}

// ListTags returns all available tags for a GHCR image, sorted by most recent first.
func (c *GHCRClient) ListTags(ctx context.Context, repository string) ([]string, error) {
	// Rate limiting
	<-c.rateLimiter.C

	// Parse repository to extract owner and package name
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid repository format: %s", repository)
	}

	owner := parts[0]
	packageName := strings.Join(parts[1:], "/")

	// Use GitHub Packages API to get tags sorted by date (most recent first)
	// Try both /orgs and /users endpoints
	urls := []string{
		fmt.Sprintf("https://api.github.com/orgs/%s/packages/container/%s/versions?per_page=100", owner, packageName),
		fmt.Sprintf("https://api.github.com/users/%s/packages/container/%s/versions?per_page=100", owner, packageName),
	}

	var allTags []string
	seenTags := make(map[string]bool) // Deduplicate tags

	for _, baseURL := range urls {
		page := 1
		// Adaptive maxPages based on repository type (reduces API calls)
		maxPages := c.getMaxPages(repository, false) // false = Packages API

		for page <= maxPages {
			url := fmt.Sprintf("%s&page=%d", baseURL, page)
			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				break
			}

			// GitHub Packages API prefers authentication but works without for public packages
			if c.githubPAT != "" {
				req.Header.Set("Authorization", "Bearer "+c.githubPAT)
			}
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

			resp, err := c.doWithRetry(req)
			if err != nil {
				break
			}

			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
				// Try next URL
				resp.Body.Close()
				break
			}

			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				break
			}

			var versions []githubPackageVersion
			if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
				resp.Body.Close()
				break
			}
			resp.Body.Close()

			if len(versions) == 0 {
				// No more versions
				break
			}

			// Extract all tags from versions (already sorted by date, newest first)
			for _, version := range versions {
				for _, tag := range version.Metadata.Container.Tags {
					if !seenTags[tag] {
						allTags = append(allTags, tag)
						seenTags[tag] = true
					}
				}
			}

			// If we got less than 100 versions, we're done
			if len(versions) < 100 {
				break
			}

			page++
			// Rate limit between pages
			<-c.rateLimiter.C
		}

		// If we got tags successfully, return them
		if len(allTags) > 0 {
			return allTags, nil
		}
	}

	// Always try V2 API as fallback to get complete tag list
	// GitHub Packages API may not include older versions that were cleaned up
	if len(allTags) == 0 {
		return c.listTagsV2(ctx, repository)
	}

	// Also try V2 API to supplement GitHub Packages results
	// This ensures we don't miss tags that exist in registry but not in Packages API
	v2Tags, err := c.listTagsV2(ctx, repository)
	if err == nil && len(v2Tags) > len(allTags) {
		// V2 API found more tags, use that instead
		return v2Tags, nil
	}

	return allTags, nil
}

// listTagsV2 uses the registry V2 API as a fallback
func (c *GHCRClient) listTagsV2(ctx context.Context, repository string) ([]string, error) {
	// Get auth token
	token, err := c.getRegistryToken(ctx, repository)
	if err != nil {
		token = ""
	}

	var allTags []string
	lastTag := ""
	// Adaptive maxPages based on repository type
	maxPages := c.getMaxPages(repository, true) // true = V2 API

	for range maxPages {
		url := fmt.Sprintf("https://ghcr.io/v2/%s/tags/list?n=100", repository)
		if lastTag != "" {
			url += "&last=" + lastTag
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")

		resp, err := c.doWithRetry(req)
		if err != nil {
			return nil, fmt.Errorf("failed to query GHCR: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			err := handleHTTPError(resp, "GHCR tags request")
			resp.Body.Close()
			return nil, err
		}

		var tagList ghcrTagList
		if err := json.NewDecoder(resp.Body).Decode(&tagList); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		if len(tagList.Tags) == 0 {
			break
		}

		allTags = append(allTags, tagList.Tags...)

		if len(tagList.Tags) < 100 {
			break
		}

		lastTag = tagList.Tags[len(tagList.Tags)-1]
		<-c.rateLimiter.C
	}

	return allTags, nil
}

// getCachedToken retrieves a cached token if it's still valid.
func (c *GHCRClient) getCachedToken(repository string) (string, bool) {
	c.tokenMutex.RLock()
	defer c.tokenMutex.RUnlock()

	if entry, found := c.tokenCache[repository]; found {
		if time.Now().Before(entry.expiresAt) {
			return entry.token, true
		}
	}
	return "", false
}

// getRegistryToken exchanges credentials for a registry-specific token.
// This implements the Docker Registry V2 token authentication flow with improvements.
func (c *GHCRClient) getRegistryToken(ctx context.Context, repository string) (string, error) {
	// Check cache first
	if token, found := c.getCachedToken(repository); found {
		return token, nil
	}

	// GHCR token endpoint
	tokenURL := fmt.Sprintf("https://ghcr.io/token?scope=repository:%s:pull", repository)

	// Strategy: Try without auth first for public repos, then with auth if needed
	attempts := []struct {
		useAuth bool
		desc    string
	}{
		{false, "anonymous"},
	}

	// Only add authenticated attempt if we have a token
	if c.githubPAT != "" {
		attempts = append(attempts, struct {
			useAuth bool
			desc    string
		}{true, "authenticated"})
	}

	var lastErr error
	for _, attempt := range attempts {
		req, err := http.NewRequestWithContext(ctx, "GET", tokenURL, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create token request: %w", err)
			continue
		}

		if attempt.useAuth && c.githubPAT != "" {
			// Use Personal Access Token as password with any username
			req.SetBasicAuth("token", c.githubPAT)
		}

		resp, err := c.doWithRetry(req)
		if err != nil {
			lastErr = fmt.Errorf("%s request failed: %w", attempt.desc, err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			var tokenResp struct {
				Token       string `json:"token"`
				AccessToken string `json:"access_token"` // Some registries use this field
				ExpiresIn   int    `json:"expires_in"`   // Seconds until expiry
			}

			if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
				lastErr = fmt.Errorf("failed to decode token response: %w", err)
				continue
			}

			token := tokenResp.Token
			if token == "" {
				token = tokenResp.AccessToken
			}

			if token != "" {
				// Cache the token
				expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
				if tokenResp.ExpiresIn == 0 {
					// Default to 5 minutes if no expiry provided
					expiresAt = time.Now().Add(5 * time.Minute)
				}

				c.tokenMutex.Lock()
				c.tokenCache[repository] = tokenCacheEntry{
					token:     token,
					expiresAt: expiresAt,
				}
				c.tokenMutex.Unlock()

				return token, nil
			}
		}

		// For anonymous access to public repos, a 401 might still work
		if !attempt.useAuth && resp.StatusCode == http.StatusUnauthorized {
			// Return empty token - the API might work without auth
			return "", nil
		}

		body, _ := io.ReadAll(resp.Body)
		lastErr = fmt.Errorf("%s request returned status %d: %s", attempt.desc, resp.StatusCode, string(body))
	}

	// If all attempts failed but we're trying anonymous, return empty token
	// Some public repos work without any token
	if c.githubPAT == "" {
		return "", nil
	}

	return "", lastErr
}

// GetLatestTag returns the most recent tag for a GHCR image.
func (c *GHCRClient) GetLatestTag(ctx context.Context, repository string) (string, error) {
	tags, err := c.ListTags(ctx, repository)
	if err != nil {
		return "", err
	}

	if len(tags) == 0 {
		return "", fmt.Errorf("no tags found for repository %s", repository)
	}

	// Filter out non-version tags
	var versionTags []string
	for _, tag := range tags {
		if tag != "latest" && tag != "main" && tag != "master" && tag != "develop" {
			versionTags = append(versionTags, tag)
		}
	}

	if len(versionTags) == 0 {
		// Fall back to "latest" if available
		for _, tag := range tags {
			if tag == "latest" {
				return "latest", nil
			}
		}
		return tags[0], nil // Return first tag if no "latest"
	}

	// Sort tags to find the most recent
	// This is a simple alphabetical sort - could be improved with semantic versioning
	sort.Strings(versionTags)
	return versionTags[len(versionTags)-1], nil
}

// GetTagDigest returns the SHA256 digest for a specific GHCR tag.
func (c *GHCRClient) GetTagDigest(ctx context.Context, repository, tag string) (string, error) {
	// Rate limiting
	<-c.rateLimiter.C

	token, err := c.getRegistryToken(ctx, repository)
	if err != nil {
		// Continue without token for public repos
		token = ""
	}

	// Use the manifest endpoint to get the digest
	url := fmt.Sprintf("https://ghcr.io/v2/%s/manifests/%s", repository, tag)

	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// Accept both v2 manifest and v2 list (multi-arch) manifests
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.oci.image.index.v1+json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return "", fmt.Errorf("failed to query GHCR: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", handleHTTPError(resp, fmt.Sprintf("GHCR manifest request for tag %s", tag))
	}

	// The digest is in the Docker-Content-Digest header
	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return "", fmt.Errorf("no digest found for tag %s", tag)
	}

	// Ensure it starts with sha256:
	if !strings.HasPrefix(digest, "sha256:") {
		digest = "sha256:" + digest
	}

	return digest, nil
}

// githubPackageVersion represents a version from GitHub Packages API
type githubPackageVersion struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"` // This is the SHA256 digest
	Metadata struct {
		PackageType string `json:"package_type"`
		Container   struct {
			Tags []string `json:"tags"`
		} `json:"container"`
	} `json:"metadata"`
}

// ListTagsWithDigests uses GitHub Packages API to get SHA→tags mapping efficiently.
// This is much more efficient than the registry V2 API for discovering which version
// a container is running based on its digest.
func (c *GHCRClient) ListTagsWithDigests(ctx context.Context, repository string) (map[string][]string, error) {
	// Rate limiting
	<-c.rateLimiter.C

	// Parse repository to extract owner and package name
	// Format: "owner/package" or "owner/repo/package"
	parts := strings.Split(repository, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid repository format: %s", repository)
	}

	owner := parts[0]
	packageName := strings.Join(parts[1:], "/")

	// Use GitHub Packages API
	// Try both /orgs and /users endpoints since we don't know which one
	urls := []string{
		fmt.Sprintf("https://api.github.com/orgs/%s/packages/container/%s/versions?per_page=50", owner, packageName),
		fmt.Sprintf("https://api.github.com/users/%s/packages/container/%s/versions?per_page=50", owner, packageName),
	}

	digestToTags := make(map[string][]string)

	var lastErr error
	for _, url := range urls {
		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}

		// GitHub Packages API requires authentication
		if c.githubPAT != "" {
			req.Header.Set("Authorization", "Bearer "+c.githubPAT)
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := c.doWithRetry(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to query GitHub Packages API: %w", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusForbidden {
			// Try next URL
			body, _ := io.ReadAll(resp.Body)
			lastErr = fmt.Errorf("GitHub Packages API returned %d: %s", resp.StatusCode, string(body))
			continue
		}

		if resp.StatusCode != http.StatusOK {
			return nil, handleHTTPError(resp, "GitHub Packages API request")
		}

		var versions []githubPackageVersion
		if err := json.NewDecoder(resp.Body).Decode(&versions); err != nil {
			lastErr = fmt.Errorf("failed to decode response: %w", err)
			continue
		}

		// Build digest→tags mapping first
		digestToTagsTemp := make(map[string][]string)
		for _, version := range versions {
			// The "name" field is the SHA256 digest
			digest := version.Name
			if !strings.HasPrefix(digest, "sha256:") {
				digest = "sha256:" + digest
			}

			tags := version.Metadata.Container.Tags
			if len(tags) > 0 {
				digestToTagsTemp[digest] = tags
			}
		}

		// Invert to tag→digests mapping (what the interface expects)
		for digest, tags := range digestToTagsTemp {
			for _, tag := range tags {
				digestToTags[tag] = append(digestToTags[tag], digest)
			}
		}

		// Success!
		return digestToTags, nil
	}

	// If we get here, all attempts failed
	if lastErr != nil {
		return nil, fmt.Errorf("failed to fetch from GitHub Packages API: %w", lastErr)
	}

	return nil, fmt.Errorf("no data found for repository: %s", repository)
}