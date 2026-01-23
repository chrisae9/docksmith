package api

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// RateLimiter implements a sliding window rate limiter.
// It tracks requests per client (by IP) and enforces limits.
type RateLimiter struct {
	mu       sync.Mutex
	clients  map[string]*clientWindow
	limit    int           // Max requests per window
	window   time.Duration // Window duration
	cleanup  time.Duration // Cleanup interval for expired entries
	stopChan chan struct{}
}

// clientWindow tracks requests for a single client.
type clientWindow struct {
	timestamps []time.Time
	lastAccess time.Time
}

// RateLimitConfig holds configuration for the rate limiter.
type RateLimitConfig struct {
	RequestsPerMinute int           // Max requests per minute (default: 60)
	BurstSize         int           // Allow burst above limit (default: 10)
	CleanupInterval   time.Duration // How often to clean expired entries (default: 5m)
}

// DefaultRateLimitConfig returns sensible defaults for API rate limiting.
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerMinute: 60,
		BurstSize:         10,
		CleanupInterval:   5 * time.Minute,
	}
}

// NewRateLimiter creates a new rate limiter with the given configuration.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	if cfg.RequestsPerMinute <= 0 {
		cfg.RequestsPerMinute = 60
	}
	// BurstSize of 0 is valid - don't override it
	if cfg.BurstSize < 0 {
		cfg.BurstSize = 0
	}
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = 5 * time.Minute
	}

	rl := &RateLimiter{
		clients:  make(map[string]*clientWindow),
		limit:    cfg.RequestsPerMinute + cfg.BurstSize,
		window:   time.Minute,
		cleanup:  cfg.CleanupInterval,
		stopChan: make(chan struct{}),
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Allow checks if a request from the given client should be allowed.
// Returns true if allowed, false if rate limited.
func (rl *RateLimiter) Allow(clientID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	client, exists := rl.clients[clientID]
	if !exists {
		client = &clientWindow{
			timestamps: make([]time.Time, 0, rl.limit),
		}
		rl.clients[clientID] = client
	}

	client.lastAccess = now

	// Remove timestamps outside the window
	valid := make([]time.Time, 0, len(client.timestamps))
	for _, ts := range client.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	client.timestamps = valid

	// Check if under limit
	if len(client.timestamps) >= rl.limit {
		return false
	}

	// Record this request
	client.timestamps = append(client.timestamps, now)
	return true
}

// GetRemaining returns the number of remaining requests for a client.
func (rl *RateLimiter) GetRemaining(clientID string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	client, exists := rl.clients[clientID]
	if !exists {
		return rl.limit
	}

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Count valid timestamps
	count := 0
	for _, ts := range client.timestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	remaining := rl.limit - count
	if remaining < 0 {
		remaining = 0
	}
	return remaining
}

// Reset clears the rate limit for a specific client.
func (rl *RateLimiter) Reset(clientID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.clients, clientID)
}

// Stop stops the cleanup goroutine.
func (rl *RateLimiter) Stop() {
	close(rl.stopChan)
}

// cleanupLoop periodically removes expired client entries.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopChan:
			return
		case <-ticker.C:
			rl.cleanupExpired()
		}
	}
}

// cleanupExpired removes client entries that haven't been accessed recently.
func (rl *RateLimiter) cleanupExpired() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window * 2)

	for clientID, client := range rl.clients {
		if client.lastAccess.Before(cutoff) {
			delete(rl.clients, clientID)
		}
	}
}

// --- Middleware ---

// RateLimitMiddleware creates an HTTP middleware that enforces rate limiting.
// It uses the client's IP address as the identifier.
func RateLimitMiddleware(rl *RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := getClientIP(r)

			if !rl.Allow(clientID) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.limit))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
				return
			}

			// Add rate limit headers
			remaining := rl.GetRemaining(clientID)
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			next.ServeHTTP(w, r)
		})
	}
}

// getClientIP extracts the client IP from the request.
// It checks X-Forwarded-For and X-Real-IP headers first (for reverse proxies),
// then falls back to RemoteAddr.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (may contain multiple IPs)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP (original client)
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr (strip port)
	addr := r.RemoteAddr
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i]
		}
	}
	return addr
}

// --- Path-specific rate limiting ---

// PathRateLimiter allows different rate limits for different paths.
type PathRateLimiter struct {
	defaultLimiter *RateLimiter
	pathLimiters   map[string]*RateLimiter
	mu             sync.RWMutex
}

// NewPathRateLimiter creates a rate limiter with path-specific limits.
func NewPathRateLimiter(defaultCfg RateLimitConfig) *PathRateLimiter {
	return &PathRateLimiter{
		defaultLimiter: NewRateLimiter(defaultCfg),
		pathLimiters:   make(map[string]*RateLimiter),
	}
}

// SetPathLimit sets a specific rate limit for a path prefix.
func (prl *PathRateLimiter) SetPathLimit(pathPrefix string, cfg RateLimitConfig) {
	prl.mu.Lock()
	defer prl.mu.Unlock()
	prl.pathLimiters[pathPrefix] = NewRateLimiter(cfg)
}

// Allow checks if a request should be allowed based on client and path.
func (prl *PathRateLimiter) Allow(clientID, path string) bool {
	limiter, _ := prl.GetLimiterForPath(path)
	return limiter.Allow(clientID)
}

// GetLimiterForPath returns the rate limiter and its limit for the given path.
func (prl *PathRateLimiter) GetLimiterForPath(path string) (*RateLimiter, int) {
	prl.mu.RLock()
	defer prl.mu.RUnlock()

	// Check for path-specific limiter
	for prefix, limiter := range prl.pathLimiters {
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return limiter, limiter.limit
		}
	}

	return prl.defaultLimiter, prl.defaultLimiter.limit
}

// Stop stops all rate limiters.
func (prl *PathRateLimiter) Stop() {
	prl.mu.Lock()
	defer prl.mu.Unlock()

	prl.defaultLimiter.Stop()
	for _, limiter := range prl.pathLimiters {
		limiter.Stop()
	}
}

// PathRateLimitMiddleware creates middleware using a path-aware rate limiter.
func PathRateLimitMiddleware(prl *PathRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			clientID := getClientIP(r)
			path := r.URL.Path

			limiter, limit := prl.GetLimiterForPath(path)

			if !limiter.Allow(clientID) {
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
				w.Header().Set("X-RateLimit-Remaining", "0")
				http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
				return
			}

			// Add rate limit headers
			remaining := limiter.GetRemaining(clientID)
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			next.ServeHTTP(w, r)
		})
	}
}
