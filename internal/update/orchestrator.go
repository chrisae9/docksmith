package update

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/version"
)

// DiscoveryResult contains the complete discovery and check results
type DiscoveryResult struct {
	Containers           []ContainerInfo   `json:"containers"`
	Stacks              map[string]*Stack  `json:"stacks"`
	StandaloneContainers []ContainerInfo   `json:"standalone_containers"`
	UpdateOrder         []string           `json:"update_order"`
	TotalChecked        int                `json:"total_checked"`
	UpdatesFound        int                `json:"updates_found"`
	UpToDate           int                 `json:"up_to_date"`
	LocalImages        int                 `json:"local_images"`
	Failed             int                 `json:"failed"`
	Ignored            int                 `json:"ignored"`
	// Status endpoint specific fields (populated by background checker)
	LastCacheRefresh   string `json:"last_cache_refresh,omitempty"`   // ISO 8601 timestamp of when cache was last cleared (cache refresh)
	LastBackgroundRun  string `json:"last_background_run,omitempty"`  // ISO 8601 timestamp of when background check last ran
	Checking           bool   `json:"checking,omitempty"`             // Whether a check is currently in progress
	NextCheck          string `json:"next_check,omitempty"`           // ISO 8601 timestamp of next scheduled check
	CheckInterval      string `json:"check_interval,omitempty"`       // Duration string like "5m0s"
	CacheTTL           string `json:"cache_ttl,omitempty"`            // Duration string like "1h0m0s"
}

// ContainerInfo extends ContainerUpdate with additional metadata
type ContainerInfo struct {
	ContainerUpdate
	ID             string            `json:"id"`
	Stack          string            `json:"stack,omitempty"`
	Service        string            `json:"service,omitempty"`
	Dependencies   []string          `json:"dependencies,omitempty"`
	PreUpdateCheck string            `json:"pre_update_check,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	ComposeLabels  map[string]string `json:"compose_labels,omitempty"`  // Docksmith labels from compose file
	LabelsOutOfSync bool             `json:"labels_out_of_sync,omitempty"` // True if compose labels differ from running container
}

// Stack represents a group of related containers
type Stack struct {
	Name           string          `json:"name"`
	Containers     []ContainerInfo `json:"containers"`
	HasUpdates     bool            `json:"has_updates"`
	AllUpdatable   bool            `json:"all_updatable"`
	UpdatePriority string          `json:"update_priority,omitempty"` // "major", "minor", "patch"
}

// StackDefinition represents a manual stack definition
type StackDefinition struct {
	Name       string   `json:"name"`
	Containers []string `json:"containers"`
}

// Orchestrator coordinates discovery and checking across all components
type Orchestrator struct {
	dockerClient     docker.Client
	registryManager  RegistryClient
	checker          *Checker
	stackManager     *docker.StackManager
	graphBuilder     *graph.Builder
	safetyChecker    *SafetyChecker
	cache            *Cache
	maxConcurrency   int
	cacheEnabled     bool
	cacheTTL         time.Duration
	eventBus         *events.Bus
}

// NewOrchestrator creates a new orchestrator
func NewOrchestrator(dockerClient docker.Client, registryManager RegistryClient) *Orchestrator {
	return &Orchestrator{
		dockerClient:    dockerClient,
		registryManager: registryManager,
		checker:         NewChecker(dockerClient, registryManager, nil), // nil storage for orchestrator (uses check command's storage)
		stackManager:    docker.NewStackManager(),
		graphBuilder:    graph.NewBuilder(),
		safetyChecker:   NewSafetyChecker(),
		cache:          NewCache(),
		maxConcurrency: 5,
		cacheEnabled:   false,
		cacheTTL:       15 * time.Minute,
	}
}

// EnableCache enables the caching layer
func (o *Orchestrator) EnableCache(ttl time.Duration) {
	o.cacheEnabled = true
	o.cacheTTL = ttl
}

// GetCacheOldestEntryTime returns when the oldest cache entry was created
func (o *Orchestrator) GetCacheOldestEntryTime() time.Time {
	return o.cache.GetOldestEntryTime()
}

// CleanupCache removes expired cache entries
func (o *Orchestrator) CleanupCache() {
	o.cache.Cleanup()
}

// SetStorage sets the storage service for the orchestrator's checker
func (o *Orchestrator) SetStorage(store storage.Storage) {
	if o.checker != nil {
		o.checker.storage = store
	}
}

// SetEventBus sets the event bus for publishing progress events
func (o *Orchestrator) SetEventBus(bus *events.Bus) {
	o.eventBus = bus
}

// ClearCache clears the cache
func (o *Orchestrator) ClearCache() {
	o.cache.Clear()
}

// SetMaxConcurrency sets the maximum number of concurrent registry queries
func (o *Orchestrator) SetMaxConcurrency(max int) {
	o.maxConcurrency = max
}

// AddManualStack adds a manual stack definition
func (o *Orchestrator) AddManualStack(def StackDefinition) {
	stackDef := docker.StackDefinition{
		Name:       def.Name,
		Containers: def.Containers,
	}
	o.stackManager.AddManualStack(stackDef)
}

// LoadManualStacks loads manual stack definitions from a file
func (o *Orchestrator) LoadManualStacks(filepath string) error {
	return o.stackManager.LoadManualStacksFromFile(filepath)
}

// DiscoverAndCheck performs complete discovery and update checking
func (o *Orchestrator) DiscoverAndCheck(ctx context.Context) (*DiscoveryResult, error) {
	// Step 1: Discover containers
	o.publishCheckProgress("discovering", 0, 0, "", "Discovering containers...")
	containers, err := o.dockerClient.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	o.publishCheckProgress("discovered", len(containers), 0, "", fmt.Sprintf("Found %d containers", len(containers)))

	result := &DiscoveryResult{
		Containers:           make([]ContainerInfo, 0, len(containers)),
		Stacks:              make(map[string]*Stack),
		StandaloneContainers: make([]ContainerInfo, 0),
		TotalChecked:        len(containers),
	}

	// Step 2: Check for updates with concurrency control
	sem := make(chan struct{}, o.maxConcurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	containerInfos := make([]ContainerInfo, len(containers))
	var checkedCount int32

	for i, container := range containers {
		wg.Add(1)
		go func(idx int, c docker.Container) {
			defer wg.Done()

			// Acquire semaphore
			sem <- struct{}{}
			defer func() { <-sem }()

			// Publish that we're checking this container
			o.publishCheckProgress("checking", len(containers), int(atomic.LoadInt32(&checkedCount)), c.Name, fmt.Sprintf("Checking %s...", c.Name))

			// Check if container should be ignored
			if o.checker.shouldIgnoreContainer(c) {
				info := ContainerInfo{
					ContainerUpdate: ContainerUpdate{
						ContainerName: c.Name,
						Image:         c.Image,
						Status:        Ignored,
					},
					ID:     c.ID,
					Labels: c.Labels,
				}
				// Determine stack for ignored container
				info.Stack = o.stackManager.DetermineStack(ctx, c)
				if service, ok := c.Labels["com.docker.compose.service"]; ok {
					info.Service = service
				}

				mu.Lock()
				containerInfos[idx] = info
				result.Ignored++
				mu.Unlock()

				atomic.AddInt32(&checkedCount, 1)
				o.publishCheckProgress("checked", len(containers), int(atomic.LoadInt32(&checkedCount)), c.Name, fmt.Sprintf("Checked %s (ignored)", c.Name))
				return
			}

			// Check for updates
			info := o.checkContainer(ctx, c)

			// Store result
			mu.Lock()
			containerInfos[idx] = info

			// Update counters
			switch info.Status {
			case UpdateAvailable:
				result.UpdatesFound++
			case UpToDate:
				result.UpToDate++
			case LocalImage:
				result.LocalImages++
			case CheckFailed:
				result.Failed++
			}
			mu.Unlock()

			atomic.AddInt32(&checkedCount, 1)
			statusMsg := "up to date"
			if info.Status == UpdateAvailable {
				statusMsg = "update available"
			} else if info.Status == LocalImage {
				statusMsg = "local image"
			}
			o.publishCheckProgress("checked", len(containers), int(atomic.LoadInt32(&checkedCount)), c.Name, fmt.Sprintf("Checked %s (%s)", c.Name, statusMsg))
		}(i, container)
	}

	wg.Wait()
	result.Containers = containerInfos

	// Step 3: Group into stacks
	o.groupIntoStacks(result)

	// Step 4: Build dependency graph and get update order
	depGraph := o.graphBuilder.BuildFromContainers(containers)
	if !depGraph.HasCycles() {
		updateOrder, _ := depGraph.GetUpdateOrder()
		result.UpdateOrder = updateOrder
	}

	// Step 5: Run pre-update checks if configured
	for i, info := range result.Containers {
		if info.PreUpdateCheck != "" && info.Status == UpdateAvailable {
			canUpdate, err := o.safetyChecker.CheckContainer(ctx, info)
			if err != nil {
				result.Containers[i].Error = fmt.Sprintf("pre-update check failed: %v", err)
			} else if !canUpdate {
				result.Containers[i].Status = Unknown
				result.Containers[i].Error = "pre-update check blocked update"
				result.UpdatesFound--
			}
		}
	}

	o.publishCheckProgress("complete", result.TotalChecked, result.TotalChecked, "",
		fmt.Sprintf("Complete: %d updates, %d current, %d local", result.UpdatesFound, result.UpToDate, result.LocalImages))

	return result, nil
}

// publishCheckProgress publishes check progress events to the event bus
func (o *Orchestrator) publishCheckProgress(stage string, total, checked int, containerName, message string) {
	if o.eventBus == nil {
		return
	}

	percent := 0
	if total > 0 {
		percent = (checked * 100) / total
	}

	o.eventBus.Publish(events.Event{
		Type: events.EventCheckProgress,
		Payload: map[string]interface{}{
			"stage":          stage,
			"total":          total,
			"checked":        checked,
			"percent":        percent,
			"container_name": containerName,
			"message":        message,
		},
	})
}

// checkContainer checks a single container for updates
func (o *Orchestrator) checkContainer(ctx context.Context, container docker.Container) ContainerInfo {
	// Check cache first if enabled (only for update check results, not container metadata)
	var update ContainerUpdate
	if o.cacheEnabled {
		containerIDSuffix := container.ID
		if len(containerIDSuffix) > 12 {
			containerIDSuffix = containerIDSuffix[:12]
		}
		cacheKey := fmt.Sprintf("%s:%s", container.Image, containerIDSuffix)
		if cached, found := o.cache.Get(cacheKey); found {
			if cachedUpdate, ok := cached.(ContainerUpdate); ok {
				update = cachedUpdate
			} else {
				// Cache miss or wrong type, do fresh check
				update = o.checker.checkContainer(ctx, container)
				if update.Status != LocalImage {
					o.cache.Set(cacheKey, update, o.cacheTTL)
				}
			}
		} else {
			// Cache miss, do fresh check
			update = o.checker.checkContainer(ctx, container)
			if update.Status != LocalImage {
				o.cache.Set(cacheKey, update, o.cacheTTL)
			}
		}
	} else {
		// Cache disabled, always do fresh check
		update = o.checker.checkContainer(ctx, container)
	}

	// Always compute container-specific metadata fresh (never cache this)
	info := ContainerInfo{
		ContainerUpdate: update,
		ID:             container.ID,
		Labels:         container.Labels,
	}

	// Determine stack (container-specific, always fresh)
	info.Stack = o.stackManager.DetermineStack(ctx, container)

	// Extract service name (container-specific, always fresh)
	if service, ok := container.Labels["com.docker.compose.service"]; ok {
		info.Service = service
	}

	// Extract dependencies (container-specific, always fresh)
	if deps, ok := container.Labels["com.docker.compose.depends_on"]; ok {
		info.Dependencies = graph.ParseDependsOn(deps)
	}

	// Check label sync between compose file and running container
	info.ComposeLabels, info.LabelsOutOfSync = o.checkLabelSync(container)

	return info
}

// checkLabelSync compares docksmith labels between compose file and running container
// Returns the compose file labels and whether they are out of sync
func (o *Orchestrator) checkLabelSync(container docker.Container) (map[string]string, bool) {
	// Get compose file path from container labels
	composeFilePath, ok := container.Labels["com.docker.compose.project.config_files"]
	if !ok || composeFilePath == "" {
		// Not a compose-managed container, no sync check needed
		return nil, false
	}

	serviceName, ok := container.Labels["com.docker.compose.service"]
	if !ok || serviceName == "" {
		return nil, false
	}

	// Load compose file
	composeFile, err := compose.LoadComposeFileOrIncluded(composeFilePath, container.Name)
	if err != nil {
		// Can't load compose file, skip sync check
		return nil, false
	}

	// Find service in compose file
	service, err := composeFile.FindServiceByContainerName(serviceName)
	if err != nil {
		return nil, false
	}

	// Extract all labels from compose file
	allComposeLabels, err := service.GetAllLabels()
	if err != nil {
		return nil, false
	}

	// Filter to only docksmith.* labels
	composeLabels := make(map[string]string)
	for key, value := range allComposeLabels {
		if strings.HasPrefix(key, "docksmith.") {
			composeLabels[key] = value
		}
	}

	// Extract docksmith.* labels from running container
	containerLabels := make(map[string]string)
	for key, value := range container.Labels {
		if strings.HasPrefix(key, "docksmith.") {
			containerLabels[key] = value
		}
	}

	// Compare labels
	outOfSync := false

	// Check if compose has labels that container doesn't have (or different values)
	for key, composeValue := range composeLabels {
		containerValue, exists := containerLabels[key]
		if !exists || containerValue != composeValue {
			outOfSync = true
			break
		}
	}

	// Check if container has docksmith labels that compose doesn't have
	if !outOfSync {
		for key := range containerLabels {
			if _, exists := composeLabels[key]; !exists {
				outOfSync = true
				break
			}
		}
	}

	return composeLabels, outOfSync
}

// groupIntoStacks groups containers into stacks
func (o *Orchestrator) groupIntoStacks(result *DiscoveryResult) {
	for _, container := range result.Containers {
		if container.Stack != "" {
			// Add to stack
			if _, exists := result.Stacks[container.Stack]; !exists {
				result.Stacks[container.Stack] = &Stack{
					Name:       container.Stack,
					Containers: make([]ContainerInfo, 0),
				}
			}

			stack := result.Stacks[container.Stack]
			stack.Containers = append(stack.Containers, container)

			// Update stack status
			if container.Status == UpdateAvailable {
				stack.HasUpdates = true

				// Track highest priority update
				if stack.UpdatePriority == "" {
					switch container.ChangeType {
					case version.MajorChange:
						stack.UpdatePriority = "major"
					case version.MinorChange:
						stack.UpdatePriority = "minor"
					case version.PatchChange:
						stack.UpdatePriority = "patch"
					}
				} else {
					// Upgrade priority if needed
					if container.ChangeType == version.MajorChange {
						stack.UpdatePriority = "major"
					} else if container.ChangeType == version.MinorChange && stack.UpdatePriority == "patch" {
						stack.UpdatePriority = "minor"
					}
				}
			}
		} else {
			// Standalone container
			result.StandaloneContainers = append(result.StandaloneContainers, container)
		}
	}

	// Check if all containers in each stack have updates
	for _, stack := range result.Stacks {
		allUpdatable := true
		for _, container := range stack.Containers {
			if container.Status != UpdateAvailable && container.Status != LocalImage {
				allUpdatable = false
				break
			}
		}
		stack.AllUpdatable = allUpdatable
	}
}

// SafetyChecker runs pre-update safety checks
type SafetyChecker struct {
	bypassChecks bool
}

// NewSafetyChecker creates a new safety checker
func NewSafetyChecker() *SafetyChecker {
	return &SafetyChecker{}
}

// SetBypass allows bypassing safety checks
func (s *SafetyChecker) SetBypass(bypass bool) {
	s.bypassChecks = bypass
}

// CheckContainer runs pre-update checks for a container
func (s *SafetyChecker) CheckContainer(ctx context.Context, container ContainerInfo) (bool, error) {
	if s.bypassChecks {
		return true, nil
	}

	if container.PreUpdateCheck == "" {
		return true, nil // No check configured, allow update
	}

	// Validate script path
	if !docker.ValidatePreUpdateScript(container.PreUpdateCheck) {
		return false, fmt.Errorf("invalid pre-update script path: %s", container.PreUpdateCheck)
	}

	// Construct full path if not already absolute
	scriptPath := container.PreUpdateCheck
	if !filepath.IsAbs(scriptPath) {
		scriptPath = filepath.Join("/scripts", scriptPath)
	}

	// Execute the check script with timeout
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, scriptPath, container.ID, container.ContainerName)
	output, err := cmd.Output()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit code means check failed (don't update)
			_ = exitErr
			return false, nil
		}
		return false, fmt.Errorf("failed to execute pre-update check: %w", err)
	}

	// Check succeeded (exit code 0), allow update
	_ = output // Could log this for audit
	return true, nil
}

// Cache provides caching for registry responses
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
}

// CacheEntry represents a cached item
type CacheEntry struct {
	Value     interface{}
	ExpiresAt time.Time
	CreatedAt time.Time
}

// NewCache creates a new cache
func NewCache() *Cache {
	return &Cache{
		entries: make(map[string]*CacheEntry),
	}
}

// Get retrieves an item from cache
func (c *Cache) Get(key string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, found := c.entries[key]
	if !found {
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}

	return entry.Value, true
}

// Set stores an item in cache
func (c *Cache) Set(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.entries[key] = &CacheEntry{
		Value:     value,
		ExpiresAt: now.Add(ttl),
		CreatedAt: now,
	}
}

// Clear removes all items from cache
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
}

// Cleanup removes expired entries
func (c *Cache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}

// GetOldestEntryTime returns the creation time of the oldest cache entry
// Returns zero time if cache is empty
func (c *Cache) GetOldestEntryTime() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var oldest time.Time
	for _, entry := range c.entries {
		if oldest.IsZero() || entry.CreatedAt.Before(oldest) {
			oldest = entry.CreatedAt
		}
	}
	return oldest
}
