package api

import (
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/update"
)

// handleGetStatus returns the cached container check results
func (s *Server) handleGetStatus(w http.ResponseWriter, r *http.Request) {
	if s.backgroundChecker == nil {
		// Fallback to live check if background checker not available
		s.handleCheck(w, r)
		return
	}

	cachedResult, lastCheck, lastBackgroundRun, checking := s.backgroundChecker.GetCachedResults()

	// Make a deep copy to avoid race conditions and data corruption
	var result *update.DiscoveryResult
	if cachedResult == nil {
		result = &update.DiscoveryResult{
			Containers: []update.ContainerInfo{},
		}
	} else {
		// Deep copy the result to prevent shared slice/map references
		result = &update.DiscoveryResult{
			Containers:           make([]update.ContainerInfo, len(cachedResult.Containers)),
			Stacks:              make(map[string]*update.Stack),
			StandaloneContainers: make([]update.ContainerInfo, len(cachedResult.StandaloneContainers)),
			UpdateOrder:         make([]string, len(cachedResult.UpdateOrder)),
			TotalChecked:        cachedResult.TotalChecked,
			UpdatesFound:        cachedResult.UpdatesFound,
			UpToDate:           cachedResult.UpToDate,
			LocalImages:        cachedResult.LocalImages,
			Failed:             cachedResult.Failed,
			Ignored:            cachedResult.Ignored,
		}

		// Copy containers slice
		copy(result.Containers, cachedResult.Containers)

		// Copy standalone containers slice
		copy(result.StandaloneContainers, cachedResult.StandaloneContainers)

		// Copy update order slice
		copy(result.UpdateOrder, cachedResult.UpdateOrder)

		// Deep copy stacks map
		for name, stack := range cachedResult.Stacks {
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

	// Add status-specific fields to the result
	result.LastCheck = lastCheck.Format(time.RFC3339)
	if !lastBackgroundRun.IsZero() {
		result.LastBackgroundRun = lastBackgroundRun.Format(time.RFC3339)
	}
	result.Checking = checking
	if !lastBackgroundRun.IsZero() && s.checkInterval > 0 {
		nextCheck := lastBackgroundRun.Add(s.checkInterval)
		result.NextCheck = nextCheck.Format(time.RFC3339)
	}
	result.CheckInterval = s.checkInterval.String()
	result.CacheTTL = s.cacheTTL.String()

	RespondSuccess(w, result)
}
