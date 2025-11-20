package update

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
)

// BackgroundChecker runs container checks on a configurable interval
type BackgroundChecker struct {
	orchestrator  *Orchestrator
	dockerClient  docker.Client
	eventBus      *events.Bus
	interval      time.Duration
	cache         *CheckResultCache
	stopChan      chan struct{}
	runningMu     sync.Mutex
	running       bool
}

// CheckResultCache stores the latest check results
type CheckResultCache struct {
	mu                 sync.RWMutex
	result             *DiscoveryResult
	lastCheck          time.Time // When cache was last populated (registry query)
	lastBackgroundRun  time.Time // When background check last ran
	checking           bool
}

// NewBackgroundChecker creates a new background checker
func NewBackgroundChecker(orchestrator *Orchestrator, dockerClient docker.Client, eventBus *events.Bus, interval time.Duration) *BackgroundChecker {
	return &BackgroundChecker{
		orchestrator: orchestrator,
		dockerClient: dockerClient,
		eventBus:     eventBus,
		interval:     interval,
		cache: &CheckResultCache{
			result:    nil,
			lastCheck: time.Time{},
		},
		stopChan: make(chan struct{}),
	}
}

// Start begins the background checking loop
func (bc *BackgroundChecker) Start() {
	bc.runningMu.Lock()
	if bc.running {
		bc.runningMu.Unlock()
		return
	}
	bc.running = true
	bc.runningMu.Unlock()

	log.Printf("BACKGROUND_CHECKER: Starting with interval %v", bc.interval)

	// Run initial check immediately
	go bc.runCheck()

	// Start ticker for periodic checks
	go bc.checkLoop()
}

// Stop stops the background checker
func (bc *BackgroundChecker) Stop() {
	bc.runningMu.Lock()
	defer bc.runningMu.Unlock()

	if !bc.running {
		return
	}

	log.Printf("BACKGROUND_CHECKER: Stopping")
	close(bc.stopChan)
	bc.running = false
}

// checkLoop runs the periodic check loop
func (bc *BackgroundChecker) checkLoop() {
	ticker := time.NewTicker(bc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-bc.stopChan:
			return
		case <-ticker.C:
			bc.runCheck()
		}
	}
}

// runCheck performs a check and updates the cache
func (bc *BackgroundChecker) runCheck() {
	// Set checking flag
	bc.cache.mu.Lock()
	if bc.cache.checking {
		bc.cache.mu.Unlock()
		log.Printf("BACKGROUND_CHECKER: Check already in progress, skipping")
		return
	}
	bc.cache.checking = true
	bc.cache.mu.Unlock()

	defer func() {
		bc.cache.mu.Lock()
		bc.cache.checking = false
		bc.cache.mu.Unlock()
	}()

	log.Printf("BACKGROUND_CHECKER: Running check")
	startTime := time.Now()

	ctx := context.Background()
	result, err := bc.orchestrator.DiscoverAndCheck(ctx)
	if err != nil {
		log.Printf("BACKGROUND_CHECKER: Check failed: %v", err)
		// Still update cache with empty result to show we tried
		bc.cache.mu.Lock()
		bc.cache.result = &DiscoveryResult{Containers: []ContainerInfo{}}
		bc.cache.lastCheck = time.Now()
		bc.cache.mu.Unlock()
		return
	}

	// Get the oldest cache entry time (represents when we last queried registries)
	oldestCacheTime := bc.orchestrator.GetCacheOldestEntryTime()
	if oldestCacheTime.IsZero() {
		// No cache entries, use current time (just queried everything)
		oldestCacheTime = time.Now()
	}

	// Update cache
	bc.cache.mu.Lock()
	bc.cache.result = result
	bc.cache.lastCheck = oldestCacheTime
	bc.cache.lastBackgroundRun = time.Now()
	bc.cache.mu.Unlock()

	duration := time.Since(startTime)
	cacheAge := time.Since(oldestCacheTime)
	log.Printf("BACKGROUND_CHECKER: Check completed in %v, found %d containers, oldest cache entry: %v ago",
		duration, result.TotalChecked, cacheAge)

	// Publish event to notify UI
	if bc.eventBus != nil {
		bc.eventBus.Publish(events.Event{
			Type: events.EventContainerUpdated,
			Payload: map[string]interface{}{
				"source":          "background_checker",
				"container_count": result.TotalChecked,
				"timestamp":       time.Now().Unix(),
			},
		})
	}
}

// GetCachedResults returns the cached check results
func (bc *BackgroundChecker) GetCachedResults() (*DiscoveryResult, time.Time, time.Time, bool) {
	bc.cache.mu.RLock()
	defer bc.cache.mu.RUnlock()

	return bc.cache.result, bc.cache.lastCheck, bc.cache.lastBackgroundRun, bc.cache.checking
}

// TriggerCheck manually triggers a check (for manual refresh)
func (bc *BackgroundChecker) TriggerCheck() {
	go bc.runCheck()
}
