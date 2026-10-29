package api

import (
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/update"
)

// handleGetStatus returns the cached container check results in slim form.
// The full payload (including per-container available tags and the stacks
// map) is reserved for /api/check.
func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	if s.backgroundChecker == nil {
		// Fallback to live check if background checker not available
		s.handleCheck(w, r)
		return
	}
	s.writeStatus(w, true)
}

// writeStatus writes the cached check results in either slim or full form.
// Slim mode omits available_tags on every container and the stacks map;
// counts, timestamps and the remaining fields are identical in both modes.
func (s *Server) writeStatus(w http.ResponseWriter, slim bool) {
	cachedResult, lastCheck, lastBackgroundRun, checking := s.backgroundChecker.GetCachedResults()

	result := buildStatusResult(cachedResult, lastCheck, lastBackgroundRun, checking, s.checkInterval, s.cacheTTL, slim)

	RespondSuccess(w, result)
}

// buildStatusResult makes a race-safe copy of cached and adds the
// status-specific fields. It never mutates cached: containers are copied by
// value, so clearing AvailableTags only affects the returned copy.
func buildStatusResult(cached *update.DiscoveryResult, lastCheck, lastBackgroundRun time.Time, checking bool, checkInterval, cacheTTL time.Duration, slim bool) *update.DiscoveryResult {
	// Make a deep copy to avoid race conditions and data corruption
	var result *update.DiscoveryResult
	if cached == nil {
		result = &update.DiscoveryResult{
			Containers: []update.ContainerInfo{},
		}
	} else {
		// Deep copy the result to prevent shared slice/map references
		result = &update.DiscoveryResult{
			Containers:           make([]update.ContainerInfo, len(cached.Containers)),
			StandaloneContainers: make([]update.ContainerInfo, len(cached.StandaloneContainers)),
			UpdateOrder:          make([]string, len(cached.UpdateOrder)),
			TotalChecked:         cached.TotalChecked,
			UpdatesFound:         cached.UpdatesFound,
			UpToDate:             cached.UpToDate,
			LocalImages:          cached.LocalImages,
			Failed:               cached.Failed,
			Ignored:              cached.Ignored,
		}

		// Copy containers slice
		copy(result.Containers, cached.Containers)

		// Copy standalone containers slice
		copy(result.StandaloneContainers, cached.StandaloneContainers)

		// Copy update order slice
		copy(result.UpdateOrder, cached.UpdateOrder)

		if slim {
			// Drop the heavy per-container tag lists; the stacks map stays nil.
			for i := range result.Containers {
				result.Containers[i].AvailableTags = nil
			}
			for i := range result.StandaloneContainers {
				result.StandaloneContainers[i].AvailableTags = nil
			}
		} else {
			// Deep copy stacks map
			result.Stacks = make(map[string]*update.Stack, len(cached.Stacks))
			for name, stack := range cached.Stacks {
				stackCopy := &update.Stack{
					Name:           stack.Name,
					Containers:     make([]update.ContainerInfo, len(stack.Containers)),
					HasUpdates:     stack.HasUpdates,
					AllUpdatable:   stack.AllUpdatable,
					UpdatePriority: stack.UpdatePriority,
				}
				copy(stackCopy.Containers, stack.Containers)
				result.Stacks[name] = stackCopy
			}
		}
	}

	// Add status-specific fields to the result
	if !lastCheck.IsZero() {
		result.LastCacheRefresh = lastCheck.Format(time.RFC3339)
	}
	if !lastBackgroundRun.IsZero() {
		result.LastBackgroundRun = lastBackgroundRun.Format(time.RFC3339)
	}
	result.Checking = checking
	if !lastBackgroundRun.IsZero() && checkInterval > 0 {
		nextCheck := lastBackgroundRun.Add(checkInterval)
		result.NextCheck = nextCheck.Format(time.RFC3339)
	}
	result.CheckInterval = checkInterval.String()
	result.CacheTTL = cacheTTL.String()

	return result
}
