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
	orchestrator  *Orchestrator
	dockerClient  docker.Client
	eventBus      *events.Bus
	storage       storage.Storage
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

	// Clean up expired cache entries before check
	bc.orchestrator.CleanupCache()

	ctx := context.Background()
	result, err := bc.orchestrator.DiscoverAndCheck(ctx)
	if err != nil {
		log.Printf("BACKGROUND_CHECKER: Check failed: %v", err)
		// Still update cache with empty result to show we tried
		now := time.Now()
		bc.cache.mu.Lock()
		bc.cache.result = &DiscoveryResult{Containers: []ContainerInfo{}}
		// Only update lastCacheRefresh if cache was cleared
		if bc.cache.cacheCleared {
			bc.cache.lastCacheRefresh = now
			bc.cache.cacheCleared = false
			// Persist to database
			if bc.storage != nil {
				go func() {
					if err := bc.storage.SetConfig(context.Background(), "last_cache_refresh", now.Format(time.RFC3339)); err != nil {
						log.Printf("BACKGROUND_CHECKER: Failed to persist last_cache_refresh: %v", err)
					}
				}()
			}
		}
		bc.cache.lastBackgroundRun = now
		bc.cache.mu.Unlock()
		return
	}

	// Update cache with current check time
	now := time.Now()
	bc.cache.mu.Lock()
	bc.cache.result = result
	// Only update lastCacheRefresh if cache was cleared (cache refresh)
	if bc.cache.cacheCleared {
		bc.cache.lastCacheRefresh = now
		bc.cache.cacheCleared = false
		// Persist to database
		if bc.storage != nil {
			go func() {
				if err := bc.storage.SetConfig(context.Background(), "last_cache_refresh", now.Format(time.RFC3339)); err != nil {
					log.Printf("BACKGROUND_CHECKER: Failed to persist last_cache_refresh: %v", err)
				}
			}()
		}
	}
	// Always update lastBackgroundRun (for both background check and cache refresh)
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
