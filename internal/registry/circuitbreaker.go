package registry

import (
	"errors"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	// CircuitClosed is the normal operating state - requests pass through.
	CircuitClosed CircuitState = iota
	// CircuitOpen means the circuit is tripped - requests fail fast.
	CircuitOpen
	// CircuitHalfOpen allows a single probe request to test recovery.
	CircuitHalfOpen
)

// Default circuit breaker configuration.
const (
	DefaultFailureThreshold = 5               // Number of consecutive failures to open circuit
	DefaultResetTimeout     = 30 * time.Second // Time before attempting recovery probe
)

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open: registry temporarily unavailable")

// CircuitBreaker implements the circuit breaker pattern for registry calls.
// It tracks failures per registry and opens the circuit after consecutive failures,
// preventing cascading failures and allowing the system to fail fast.
type CircuitBreaker struct {
	mu               sync.RWMutex
	circuits         map[string]*circuitState
	failureThreshold int
	resetTimeout     time.Duration
}

// circuitState tracks the state of a single circuit (per registry).
type circuitState struct {
	state            CircuitState
	failures         int
	lastFailure      time.Time
	lastStateChange  time.Time
}

// NewCircuitBreaker creates a new circuit breaker with default settings.
func NewCircuitBreaker() *CircuitBreaker {
	return &CircuitBreaker{
		circuits:         make(map[string]*circuitState),
		failureThreshold: DefaultFailureThreshold,
		resetTimeout:     DefaultResetTimeout,
	}
}

// NewCircuitBreakerWithConfig creates a circuit breaker with custom settings.
func NewCircuitBreakerWithConfig(failureThreshold int, resetTimeout time.Duration) *CircuitBreaker {
	if failureThreshold <= 0 {
		failureThreshold = DefaultFailureThreshold
	}
	if resetTimeout <= 0 {
		resetTimeout = DefaultResetTimeout
	}
	return &CircuitBreaker{
		circuits:         make(map[string]*circuitState),
		failureThreshold: failureThreshold,
		resetTimeout:     resetTimeout,
	}
}

// Allow checks if a request to the given registry should be allowed.
// Returns true if the request can proceed, false if it should fail fast.
func (cb *CircuitBreaker) Allow(registry string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	circuit := cb.getOrCreateCircuit(registry)

	switch circuit.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if enough time has passed to try a probe
		if time.Since(circuit.lastStateChange) >= cb.resetTimeout {
			circuit.state = CircuitHalfOpen
			circuit.lastStateChange = time.Now()
			return true // Allow probe request
		}
		return false

	case CircuitHalfOpen:
		// Only one probe request allowed - subsequent requests fail fast
		// until the probe completes (success or failure)
		return false

	default:
		return true
	}
}

// RecordSuccess records a successful request, potentially closing an open circuit.
func (cb *CircuitBreaker) RecordSuccess(registry string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	circuit := cb.getOrCreateCircuit(registry)
	circuit.failures = 0

	if circuit.state != CircuitClosed {
		circuit.state = CircuitClosed
		circuit.lastStateChange = time.Now()
	}
}

// RecordFailure records a failed request, potentially opening the circuit.
func (cb *CircuitBreaker) RecordFailure(registry string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	circuit := cb.getOrCreateCircuit(registry)
	circuit.failures++
	circuit.lastFailure = time.Now()

	switch circuit.state {
	case CircuitClosed:
		if circuit.failures >= cb.failureThreshold {
			circuit.state = CircuitOpen
			circuit.lastStateChange = time.Now()
		}

	case CircuitHalfOpen:
		// Probe failed - reopen the circuit
		circuit.state = CircuitOpen
		circuit.lastStateChange = time.Now()
	}
}

// GetState returns the current state of the circuit for a registry.
func (cb *CircuitBreaker) GetState(registry string) CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if circuit, exists := cb.circuits[registry]; exists {
		// Check if open circuit should transition to half-open
		if circuit.state == CircuitOpen && time.Since(circuit.lastStateChange) >= cb.resetTimeout {
			return CircuitHalfOpen
		}
		return circuit.state
	}
	return CircuitClosed
}

// GetStats returns statistics for a registry's circuit.
func (cb *CircuitBreaker) GetStats(registry string) (state CircuitState, failures int, lastFailure time.Time) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	if circuit, exists := cb.circuits[registry]; exists {
		return circuit.state, circuit.failures, circuit.lastFailure
	}
	return CircuitClosed, 0, time.Time{}
}

// Reset resets the circuit for a specific registry.
func (cb *CircuitBreaker) Reset(registry string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	delete(cb.circuits, registry)
}

// ResetAll resets all circuits.
func (cb *CircuitBreaker) ResetAll() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.circuits = make(map[string]*circuitState)
}

// getOrCreateCircuit returns the circuit for a registry, creating if needed.
// Must be called with lock held.
func (cb *CircuitBreaker) getOrCreateCircuit(registry string) *circuitState {
	if circuit, exists := cb.circuits[registry]; exists {
		return circuit
	}

	circuit := &circuitState{
		state:           CircuitClosed,
		lastStateChange: time.Now(),
	}
	cb.circuits[registry] = circuit
	return circuit
}
