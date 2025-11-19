package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// PreUpdateCheckLabel is the Docker label used to specify pre-update check scripts
const PreUpdateCheckLabel = "docksmith.pre-update-check"

// Client defines the interface for Docker operations.
// This interface allows for easy mocking in tests and follows
// the dependency injection pattern.
type Client interface {
	// ListContainers returns all containers (running and stopped)
	ListContainers(ctx context.Context) ([]Container, error)

	// IsLocalImage checks if an image was built locally (not pulled from registry)
	IsLocalImage(ctx context.Context, imageName string) (bool, error)

	// GetImageVersion extracts version from image labels
	GetImageVersion(ctx context.Context, imageName string) (string, error)

	// GetImageDigest gets the SHA256 digest for an image
	GetImageDigest(ctx context.Context, imageName string) (string, error)

	// Close releases resources held by the Docker client
	Close() error
}

// Container represents a Docker container with relevant metadata.
type Container struct {
	ID             string
	Name           string
	Image          string
	State          string
	HealthStatus   string // Health check status: "healthy", "unhealthy", "starting", "none"
	Labels         map[string]string
	Created        int64
	PreUpdateCheck string // Path to pre-update check script from docksmith.pre-update-check label
	Stack          string // Stack name from compose labels or manual definition
}

// StackDefinition represents a manual stack definition for grouping containers
type StackDefinition struct {
	Name       string   `json:"name"`
	Containers []string `json:"containers"`
}

// StackManager manages both compose-based and manual stack definitions
type StackManager struct {
	manualStacks map[string]string // container name -> stack name
	stackDefs    []StackDefinition
}

// NewStackManager creates a new stack manager
func NewStackManager() *StackManager {
	return &StackManager{
		manualStacks: make(map[string]string),
		stackDefs:    []StackDefinition{},
	}
}

// LoadManualStacksFromFile loads manual stack definitions from a JSON/YAML file
func (sm *StackManager) LoadManualStacksFromFile(filepath string) error {
	data, err := os.ReadFile(filepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read stack definitions: %w", err)
	}

	var defs []StackDefinition
	if err := json.Unmarshal(data, &defs); err != nil {
		return fmt.Errorf("failed to parse stack definitions: %w", err)
	}

	for _, def := range defs {
		sm.AddManualStack(def)
	}

	return nil
}

// AddManualStack adds a manual stack definition
func (sm *StackManager) AddManualStack(def StackDefinition) {
	sm.stackDefs = append(sm.stackDefs, def)
	for _, containerName := range def.Containers {
		sm.manualStacks[containerName] = def.Name
	}
}

// GetContainerStack returns the manually defined stack for a container
func (sm *StackManager) GetContainerStack(ctx context.Context, containerName string) (string, bool) {
	stack, found := sm.manualStacks[containerName]
	return stack, found
}

// DetermineStack determines the stack for a container, checking compose labels first,
// then falling back to manual definitions
func (sm *StackManager) DetermineStack(ctx context.Context, container Container) string {
	if project, ok := container.Labels["com.docker.compose.project"]; ok && project != "" {
		return project
	}

	if stack, found := sm.GetContainerStack(ctx, container.Name); found {
		return stack
	}

	return ""
}

// ExtractPreUpdateCheck extracts the pre-update check script path from container labels
func ExtractPreUpdateCheck(container Container) (string, bool) {
	if script, ok := container.Labels[PreUpdateCheckLabel]; ok && script != "" {
		return script, true
	}
	return "", false
}

// ValidatePreUpdateScript validates a pre-update check script path
func ValidatePreUpdateScript(scriptPath string) bool {
	if scriptPath == "" {
		return false
	}

	if !filepath.IsAbs(scriptPath) {
		return false
	}

	if strings.Contains(scriptPath, ";") ||
		strings.Contains(scriptPath, "&") ||
		strings.Contains(scriptPath, "|") ||
		strings.Contains(scriptPath, "`") ||
		strings.Contains(scriptPath, "$") ||
		strings.Contains(scriptPath, "\n") {
		return false
	}

	return true
}

// GroupTagsByPattern groups image tags by their version pattern type
func GroupTagsByPattern(tags []string) map[string][]string {
	groups := map[string][]string{
		"semantic": []string{},
		"date":     []string{},
		"hash":     []string{},
		"meta":     []string{},
	}

	semanticPattern := regexp.MustCompile(`^\d+\.\d+(\.\d+)?(-[a-zA-Z0-9.-]+)?$`)
	datePattern := regexp.MustCompile(`^\d{4}[\.\-]?\d{2}[\.\-]?\d{2}`)
	hashPattern := regexp.MustCompile(`^([a-f0-9]{7,40}|sha\d+-[a-f0-9]+)`)
	metaTags := map[string]bool{
		"latest": true, "stable": true, "main": true,
		"master": true, "develop": true, "dev": true,
		"edge": true, "nightly": true, "beta": true,
		"alpha": true, "rc": true,
	}

	for _, tag := range tags {
		tag = strings.TrimPrefix(tag, "v")

		// Check meta tags first
		if metaTags[strings.ToLower(tag)] {
			groups["meta"] = append(groups["meta"], tag)
		// Check date pattern before semantic (dates look like semantic versions)
		} else if datePattern.MatchString(tag) {
			groups["date"] = append(groups["date"], tag)
		// Check hash patterns
		} else if hashPattern.MatchString(tag) {
			groups["hash"] = append(groups["hash"], tag)
		// Check semantic last (most permissive pattern)
		} else if semanticPattern.MatchString(tag) {
			groups["semantic"] = append(groups["semantic"], tag)
		} else {
			// Default to meta for unknown patterns
			groups["meta"] = append(groups["meta"], tag)
		}
	}

	for key := range groups {
		sort.Strings(groups[key])
	}

	return groups
}

// ParseDateBasedTag attempts to parse date-based version tags
func ParseDateBasedTag(tag string) (time.Time, error) {
	formats := []string{
		"2006.01.02",
		"2006-01-02",
		"20060102",
		"2006.1.2",
		"2006-1-2",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, tag); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unable to parse date from tag: %s", tag)
}

// IsCommitHash checks if a tag appears to be a commit hash
func IsCommitHash(tag string) bool {
	if matched, _ := regexp.MatchString(`^[a-f0-9]{7,40}$`, tag); matched {
		return true
	}

	if matched, _ := regexp.MatchString(`^sha\d+-[a-f0-9]+`, tag); matched {
		return true
	}

	return false
}

// CompareVersionGroups compares versions within the same group type
func CompareVersionGroups(group string, v1, v2 string) int {
	switch group {
	case "date":
		t1, err1 := ParseDateBasedTag(v1)
		t2, err2 := ParseDateBasedTag(v2)
		if err1 == nil && err2 == nil {
			if t1.Before(t2) {
				return -1
			} else if t1.After(t2) {
				return 1
			}
			return 0
		}
	case "hash":
		if v1 == v2 {
			return 0
		}
		return -2 // Indicates incomparable
	}

	if v1 < v2 {
		return -1
	} else if v1 > v2 {
		return 1
	}
	return 0
}

// GHCRTokenCache provides token caching for GHCR authentication
type GHCRTokenCache struct {
	token     string
	expiresAt time.Time
}

// NewGHCRTokenCache creates a new token cache
func NewGHCRTokenCache() *GHCRTokenCache {
	return &GHCRTokenCache{}
}

// GetToken returns the cached token if still valid
func (c *GHCRTokenCache) GetToken() (string, bool) {
	if c.token == "" || time.Now().After(c.expiresAt) {
		return "", false
	}
	return c.token, true
}

// SetToken caches a token with expiration
func (c *GHCRTokenCache) SetToken(token string, ttl time.Duration) {
	c.token = token
	c.expiresAt = time.Now().Add(ttl)
}

// DetermineGHCRAuthStrategy determines the authentication strategy for GHCR
func DetermineGHCRAuthStrategy(hasToken bool, isPublicImage bool) (bool, error) {
	if hasToken {
		return true, nil
	}

	if isPublicImage {
		return false, nil
	}

	return false, fmt.Errorf("private GHCR image requires authentication token")
}
