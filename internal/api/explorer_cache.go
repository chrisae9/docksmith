package api

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// explorerCacheTTL is how long a computed explorer response stays fresh.
// The Containers page gates on /api/explorer, so a short cache collapses the
// batch of requests it makes under container churn into a single compute.
const explorerCacheTTL = 3 * time.Second

type explorerEntry struct {
	data *ExplorerData
	at   time.Time
}

// explorerCache caches successful explore results per key and collapses
// concurrent misses for the same key into a single compute.
type explorerCache struct {
	ttl     time.Duration
	mu      sync.Mutex
	entries map[string]explorerEntry
	group   singleflight.Group
}

func newExplorerCache(ttl time.Duration) *explorerCache {
	return &explorerCache{
		ttl:     ttl,
		entries: make(map[string]explorerEntry),
	}
}

// lookup returns a cached entry if it is still within the TTL.
func (c *explorerCache) lookup(key string) (*ExplorerData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok || time.Since(entry.at) >= c.ttl {
		return nil, false
	}
	return entry.data, true
}

// get returns the cached result for key, or computes it. Concurrent misses for
// the same key share a single compute, and only successful results are cached.
func (c *explorerCache) get(ctx context.Context, key string, compute func(context.Context) (*ExplorerData, error)) (*ExplorerData, error) {
	if data, ok := c.lookup(key); ok {
		return data, nil
	}

	value, err, _ := c.group.Do(key, func() (any, error) {
		// The cache may have been filled while this caller waited to enter the
		// flight (or by the flight that just finished), so check again before
		// paying for a compute.
		if data, ok := c.lookup(key); ok {
			return data, nil
		}

		data, err := compute(ctx)
		if err != nil {
			return nil, err
		}
		if data == nil {
			return nil, nil
		}

		c.mu.Lock()
		c.entries[key] = explorerEntry{data: data, at: time.Now()}
		c.mu.Unlock()

		return data, nil
	})
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	return value.(*ExplorerData), nil
}
