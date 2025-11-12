package registry

import (
	"sync"
	"time"
)

// CacheEntry represents a cached registry response
type CacheEntry struct {
	Value     interface{}
	ExpiresAt time.Time
}

// RegistryCache provides caching for registry API responses
type RegistryCache struct {
	mu      sync.RWMutex
	entries map[string]*CacheEntry
	ttl     time.Duration
}

// NewRegistryCache creates a new registry cache
func NewRegistryCache(ttl time.Duration) *RegistryCache {
	if ttl == 0 {
		ttl = 15 * time.Minute // Default to 15 minutes
	}
	return &RegistryCache{
		entries: make(map[string]*CacheEntry),
		ttl:     ttl,
	}
}

// Get retrieves an item from cache
func (c *RegistryCache) Get(key string) (interface{}, bool) {
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
func (c *RegistryCache) Set(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &CacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(c.ttl),
	}
}

// SetWithTTL stores an item in cache with custom TTL
func (c *RegistryCache) SetWithTTL(key string, value interface{}, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = &CacheEntry{
		Value:     value,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// Clear removes all items from cache
func (c *RegistryCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*CacheEntry)
}

// Cleanup removes expired entries
func (c *RegistryCache) Cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}

// SetTTL updates the default TTL for new cache entries
func (c *RegistryCache) SetTTL(ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = ttl
}