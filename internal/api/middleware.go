package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/chis/docksmith/internal/logging"
)

// contextKey is used for storing values in request context.
type contextKey string

const (
	correlationIDKey contextKey = "correlation_id"
	requestStartKey  contextKey = "request_start"
)

// CorrelationIDMiddleware adds a correlation ID to each request.
// The ID is generated if not present in the X-Correlation-ID header.
func CorrelationIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check for existing correlation ID
		correlationID := r.Header.Get("X-Correlation-ID")
		if correlationID == "" {
			correlationID = generateCorrelationID()
		}

		// Add to response header
		w.Header().Set("X-Correlation-ID", correlationID)

		// Add to request context
		ctx := context.WithValue(r.Context(), correlationIDKey, correlationID)
		ctx = logging.WithCorrelationID(ctx, correlationID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestLoggingMiddleware logs incoming requests and their duration.
func RequestLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		// Add start time to context
		ctx := context.WithValue(r.Context(), requestStartKey, start)

		// Get correlation ID
		correlationID := GetCorrelationID(r.Context())

		// Log request start (debug level for high-frequency endpoints)
		if isHighFrequencyEndpoint(r.URL.Path) {
			logging.DebugContext(ctx, "Request started: %s %s", r.Method, r.URL.Path)
		} else {
			logging.InfoContext(ctx, "Request started: %s %s", r.Method, r.URL.Path)
		}

		next.ServeHTTP(wrapped, r.WithContext(ctx))

		// Log request completion
		duration := time.Since(start)
		statusCode := wrapped.statusCode

		fields := map[string]interface{}{
			"method":      r.Method,
			"path":        r.URL.Path,
			"status":      statusCode,
			"duration_ms": duration.Milliseconds(),
			"client_ip":   getClientIP(r),
		}
		if correlationID != "" {
			fields["correlation_id"] = correlationID
		}

		logger := logging.Default().WithFields(fields)

		if statusCode >= 500 {
			logger.Error("Request failed: %s %s - %d", r.Method, r.URL.Path, statusCode)
		} else if statusCode >= 400 {
			logger.Warn("Request error: %s %s - %d", r.Method, r.URL.Path, statusCode)
		} else if isHighFrequencyEndpoint(r.URL.Path) {
			logger.Debug("Request completed: %s %s - %d (%dms)", r.Method, r.URL.Path, statusCode, duration.Milliseconds())
		} else {
			logger.Info("Request completed: %s %s - %d (%dms)", r.Method, r.URL.Path, statusCode, duration.Milliseconds())
		}
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// isHighFrequencyEndpoint returns true for endpoints that are called frequently
// and should use debug logging to avoid log spam.
func isHighFrequencyEndpoint(path string) bool {
	highFrequencyPaths := []string{
		"/api/health",
		"/api/events",
		"/api/status",
	}

	for _, p := range highFrequencyPaths {
		if path == p {
			return true
		}
	}
	return false
}

// GetCorrelationID retrieves the correlation ID from a request context.
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok {
		return id
	}
	return ""
}

// generateCorrelationID generates a random correlation ID.
func generateCorrelationID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID
		return fmt.Sprintf("req-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// ChainMiddleware chains multiple middleware functions together.
// Middleware is applied in the order provided (first middleware wraps outermost).
func ChainMiddleware(h http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	// Apply in reverse order so first middleware is outermost
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h
}
