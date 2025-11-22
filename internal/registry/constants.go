package registry

import "time"

const (
	// DefaultHTTPTimeout is the default timeout for HTTP requests to registries
	DefaultHTTPTimeout = 30 * time.Second

	// DefaultRateLimitInterval is the default interval between rate-limited requests
	DefaultRateLimitInterval = 100 * time.Millisecond

	// DefaultTimeoutSeconds is the default timeout in seconds for registry operations
	DefaultTimeoutSeconds = 30
)
