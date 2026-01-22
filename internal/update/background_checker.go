package update

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/storage"
)

// BackgroundChecker runs container checks on a configurable interval
type BackgroundChecker struct {
	orchestrator    *Orchestrator
	dockerClient    docker.Client
	eventBus        *events.Bus
	storage         storage.Storage
	interval        time.Duration
	cache           *CheckResultCache
	stopChan        chan struct{}
	runningMu       sync.Mutex
	running         bool
	unsubscribe     func()           // Unsubscribe from event bus
	refreshTimer    *time.Timer      // Debounce timer for container update refreshes
	refreshTimerMu  sync.Mutex       // Protects refreshTimer
}

// CheckResultCache stores the latest check results
type CheckResultCache struct {
	mu                 sync.RWMutex
	result             *DiscoveryResult
	lastCacheRefresh   time.Time // When cache was last cleared (cache refresh)
	lastBackgroundRun  time.Time // When background check last ran
	checking           bool
	cacheCleared       bool      // Flag to track if cache was recently cleared
}

// NewBackgroundChecker creates a new background checker
func NewBackgroundChecker(orchestrator *Orchestrator, dockerClient docker.Client, eventBus *events.Bus, storage storage.Storage, interval time.Duration) *BackgroundChecker {
	// Try to load last_cache_refresh from database
	var lastCacheRefresh time.Time
	if storage != nil {
		ctx := context.Background()
		if timestampStr, found, err := storage.GetConfig(ctx, "last_cache_refresh"); err == nil && found {
			if parsed, err := time.Parse(time.RFC3339, timestampStr); err == nil {
				lastCacheRefresh = parsed
				log.Printf("BACKGROUND_CHECKER: Loaded last_cache_refresh from database: %s", timestampStr)
			}
		}
	}

	return &BackgroundChecker{
		orchestrator: orchestrator,
		dockerClient: dockerClient,
		eventBus:     eventBus,
		storage:      storage,
		interval:     interval,
		cache: &CheckResultCache{
			result:            nil,
			lastCacheRefresh:  lastCacheRefresh,
			lastBackgroundRun: time.Time{},
			cacheCleared:      false,
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

	// Subscribe to container update events to refresh cache after updates/rollbacks
	if bc.eventBus != nil {
		eventChan, unsubscribe := bc.eventBus.Subscribe(events.EventContainerUpdated)
		bc.unsubscribe = unsubscribe
		go bc.handleContainerUpdates(eventChan)
	}

	// Run initial check immediately
	go bc.runCheck()

	// Start ticker for periodic checks
	go bc.checkLoop()
}

// handleContainerUpdates listens for container update events and triggers cache refresh
func (bc *BackgroundChecker) handleContainerUpdates(eventChan events.Subscriber) {
	for {
		select {
		case <-bc.stopChan:
			log.Printf("BACKGROUND_CHECKER: handleContainerUpdates stopping")
			return
		case e, ok := <-eventChan:
			if !ok {
				// Channel closed (unsubscribed)
				log.Printf("BACKGROUND_CHECKER: event channel closed")
				return
			}
			// Only trigger refresh for actual container updates (not our own background check events)
			if source, ok := e.Payload["source"].(string); ok && source == "background_checker" {
				continue // Ignore our own events to prevent infinite loops
			}
			// Check if this is from an update/rollback operation
			if _, hasOpID := e.Payload["operation_id"]; hasOpID {
				log.Printf("BACKGROUND_CHECKER: Container updated, scheduling refresh in 2s")
				// Debounce: cancel existing timer and create a new one
				bc.refreshTimerMu.Lock()
				if bc.refreshTimer != nil {
					bc.refreshTimer.Stop()
				}
				bc.refreshTimer = time.AfterFunc(2*time.Second, func() {
					bc.TriggerCheck()
				})
				bc.refreshTimerMu.Unlock()
			}
		}
	}
}

// Stop stops the background checker
func (bc *BackgroundChecker) Stop() {
	bc.runningMu.Lock()
	defer bc.runningMu.Unlock()

	if !bc.running {
		return
	}

	log.Printf("BACKGROUND_CHECKER: Stopping")

	// Cancel any pending refresh timer
	bc.refreshTimerMu.Lock()
	if bc.refreshTimer != nil {
		bc.refreshTimer.Stop()
		bc.refreshTimer = nil
	}
	bc.refreshTimerMu.Unlock()

	// Unsubscribe from event bus (closes the event channel)
	if bc.unsubscribe != nil {
		bc.unsubscribe()
		bc.unsubscribe = nil
	}

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

// updateLastCacheRefreshIfNeeded updates lastCacheRefresh when cache was cleared or empty
// Must be called while holding bc.cache.mu lock
func (bc *BackgroundChecker) updateLastCacheRefreshIfNeeded(now time.Time, cacheWasEmpty bool) {
	if !bc.cache.cacheCleared && !cacheWasEmpty {
		return
	}

	log.Printf("BACKGROUND_CHECKER: Updated lastCacheRefresh (manualRefresh=%v, cacheWasEmpty=%v)", bc.cache.cacheCleared, cacheWasEmpty)
	bc.cache.lastCacheRefresh = now
	bc.cache.cacheCleared = false

	// Persist to database with timeout context to prevent goroutine leaks
	if bc.storage != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := bc.storage.SetConfig(ctx, "last_cache_refresh", now.Format(time.RFC3339)); err != nil {
				log.Printf("BACKGROUND_CHECKER: Failed to persist last_cache_refresh: %v", err)
			}
		}()
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

	// Check if cache is empty before cleanup (indicates fresh data will be fetched)
	oldestCacheTime := bc.orchestrator.GetCacheOldestEntryTime()
	cacheWasEmpty := oldestCacheTime.IsZero()

	// Clean up expired cache entries before check
	bc.orchestrator.CleanupCache()

	// Re-check after cleanup - if cache is now empty, we'll fetch fresh data
	oldestAfterCleanup := bc.orchestrator.GetCacheOldestEntryTime()
	if !cacheWasEmpty && oldestAfterCleanup.IsZero() {
		cacheWasEmpty = true // Cache became empty after cleanup (TTL expired)
		log.Printf("BACKGROUND_CHECKER: Cache expired, will fetch fresh registry data")
	}

	ctx := context.Background()
	result, err := bc.orchestrator.DiscoverAndCheck(ctx)
	if err != nil {
		log.Printf("BACKGROUND_CHECKER: Check failed: %v", err)
		// Still update cache with empty result to show we tried
		now := time.Now()
		bc.cache.mu.Lock()
		bc.cache.result = &DiscoveryResult{Containers: []ContainerInfo{}}
		bc.updateLastCacheRefreshIfNeeded(now, cacheWasEmpty)
		bc.cache.lastBackgroundRun = now
		bc.cache.mu.Unlock()
		return
	}

	// Update cache with current check time
	now := time.Now()
	bc.cache.mu.Lock()
	bc.cache.result = result
	bc.updateLastCacheRefreshIfNeeded(now, cacheWasEmpty)
	bc.cache.lastBackgroundRun = now
	bc.cache.mu.Unlock()

	duration := time.Since(startTime)
	log.Printf("BACKGROUND_CHECKER: Check completed in %v, found %d containers",
		duration, result.TotalChecked)

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

	return bc.cache.result, bc.cache.lastCacheRefresh, bc.cache.lastBackgroundRun, bc.cache.checking
}

// MarkCacheCleared marks that the cache was cleared (for cache refresh tracking)
func (bc *BackgroundChecker) MarkCacheCleared() {
	bc.cache.mu.Lock()
	defer bc.cache.mu.Unlock()
	bc.cache.cacheCleared = true
}

// TriggerCheck manually triggers a check (for manual refresh)
func (bc *BackgroundChecker) TriggerCheck() {
	go bc.runCheck()
}
