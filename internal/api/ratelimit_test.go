package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestRateLimiterBasic(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 5,
		BurstSize:         2,
		CleanupInterval:   time.Minute,
	}

	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	clientID := "test-client"

	// Should allow up to limit (5 + 2 = 7) requests
	for i := 0; i < 7; i++ {
		if !rl.Allow(clientID) {
			t.Errorf("Request %d should be allowed", i+1)
		}
	}

	// 8th request should be denied
	if rl.Allow(clientID) {
		t.Error("Request 8 should be rate limited")
	}
}

func TestRateLimiterGetRemaining(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         0,
		CleanupInterval:   time.Minute,
	}

	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	clientID := "test-client"

	// Initially should have full limit available
	remaining := rl.GetRemaining(clientID)
	if remaining != 10 {
		t.Errorf("Expected 10 remaining, got %d", remaining)
	}

	// After 3 requests, should have 7 remaining
	for i := 0; i < 3; i++ {
		rl.Allow(clientID)
	}

	remaining = rl.GetRemaining(clientID)
	if remaining != 7 {
		t.Errorf("Expected 7 remaining, got %d", remaining)
	}
}

func TestRateLimiterReset(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 5,
		BurstSize:         0,
		CleanupInterval:   time.Minute,
	}

	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	clientID := "test-client"

	// Use up all requests
	for i := 0; i < 5; i++ {
		rl.Allow(clientID)
	}

	if rl.Allow(clientID) {
		t.Error("Should be rate limited")
	}

	// Reset the client
	rl.Reset(clientID)

	// Should be allowed again
	if !rl.Allow(clientID) {
		t.Error("Should be allowed after reset")
	}
}

func TestRateLimiterIsolatesClients(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         0,
		CleanupInterval:   time.Minute,
	}

	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	// Client A uses up their limit
	for i := 0; i < 2; i++ {
		rl.Allow("client-a")
	}

	// Client A should be blocked
	if rl.Allow("client-a") {
		t.Error("Client A should be rate limited")
	}

	// Client B should still be allowed
	if !rl.Allow("client-b") {
		t.Error("Client B should not be affected by Client A")
	}
}

func TestRateLimiterConcurrent(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 100,
		BurstSize:         10,
		CleanupInterval:   time.Minute,
	}

	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(clientID string) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				rl.Allow(clientID)
				rl.GetRemaining(clientID)
			}
		}("client-" + string(rune('a'+i%5)))
	}

	wg.Wait()
	// Test passes if no race condition or panic
}

func TestRateLimitMiddleware(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         0,
		CleanupInterval:   time.Minute,
	}

	rl := NewRateLimiter(cfg)
	defer rl.Stop()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	middleware := RateLimitMiddleware(rl)
	wrappedHandler := middleware(handler)

	// First 2 requests should succeed
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "192.168.1.1:12345"
		rec := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// 3rd request should be rate limited
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("Request 3: expected 429, got %d", rec.Code)
	}
}

func TestPathRateLimiter(t *testing.T) {
	defaultCfg := RateLimitConfig{
		RequestsPerMinute: 10,
		BurstSize:         0,
		CleanupInterval:   time.Minute,
	}

	prl := NewPathRateLimiter(defaultCfg)
	defer prl.Stop()

	// Set stricter limit for /api/update
	prl.SetPathLimit("/api/update", RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         0,
		CleanupInterval:   time.Minute,
	})

	clientID := "test-client"

	// /api/update should be limited to 2
	if !prl.Allow(clientID, "/api/update") {
		t.Error("First /api/update should be allowed")
	}
	if !prl.Allow(clientID, "/api/update") {
		t.Error("Second /api/update should be allowed")
	}
	if prl.Allow(clientID, "/api/update") {
		t.Error("Third /api/update should be blocked")
	}

	// Other paths should still have 10 remaining
	for i := 0; i < 10; i++ {
		if !prl.Allow(clientID, "/api/health") {
			t.Errorf("/api/health request %d should be allowed", i+1)
		}
	}
}

func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		xri        string
		expected   string
	}{
		{
			name:       "RemoteAddr only",
			remoteAddr: "192.168.1.1:12345",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For single",
			remoteAddr: "10.0.0.1:12345",
			xff:        "192.168.1.1",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For multiple",
			remoteAddr: "10.0.0.1:12345",
			xff:        "192.168.1.1, 10.0.0.2, 10.0.0.3",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xri:        "192.168.1.1",
			expected:   "192.168.1.1",
		},
		{
			name:       "X-Forwarded-For takes precedence over X-Real-IP",
			remoteAddr: "10.0.0.1:12345",
			xff:        "192.168.1.1",
			xri:        "192.168.1.2",
			expected:   "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.xff != "" {
				req.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xri != "" {
				req.Header.Set("X-Real-IP", tt.xri)
			}

			result := getClientIP(req)
			if result != tt.expected {
				t.Errorf("getClientIP() = %q, want %q", result, tt.expected)
			}
		})
	}
}
