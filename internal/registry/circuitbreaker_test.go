package registry

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestCircuitBreakerStartsClosed(t *testing.T) {
	cb := NewCircuitBreaker()

	state := cb.GetState("docker.io")
	if state != CircuitClosed {
		t.Errorf("Expected circuit to start closed, got %v", state)
	}

	// Should allow requests
	if !cb.Allow("docker.io") {
		t.Error("Expected Allow to return true for new circuit")
	}
}

func TestCircuitBreakerOpensAfterThreshold(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(3, 30*time.Second)

	// Record failures up to threshold
	for i := 0; i < 3; i++ {
		cb.Allow("docker.io") // Must call Allow before RecordFailure
		cb.RecordFailure("docker.io")
	}

	// Circuit should now be open
	state := cb.GetState("docker.io")
	if state != CircuitOpen {
		t.Errorf("Expected circuit to be open after 3 failures, got %v", state)
	}

	// Should not allow requests
	if cb.Allow("docker.io") {
		t.Error("Expected Allow to return false when circuit is open")
	}
}

func TestCircuitBreakerResetsOnSuccess(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(3, 30*time.Second)

	// Record 2 failures (below threshold)
	for i := 0; i < 2; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	// Record success - should reset failure count
	cb.RecordSuccess("docker.io")

	// Record 2 more failures - should not open circuit since count reset
	for i := 0; i < 2; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	// Circuit should still be closed
	state := cb.GetState("docker.io")
	if state != CircuitClosed {
		t.Errorf("Expected circuit to remain closed after success reset, got %v", state)
	}
}

func TestCircuitBreakerTransitionsToHalfOpen(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(2, 50*time.Millisecond)

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	if cb.GetState("docker.io") != CircuitOpen {
		t.Fatal("Expected circuit to be open")
	}

	// Wait for reset timeout
	time.Sleep(60 * time.Millisecond)

	// Should transition to half-open and allow one request
	state := cb.GetState("docker.io")
	if state != CircuitHalfOpen {
		t.Errorf("Expected circuit to be half-open after timeout, got %v", state)
	}

	// Should allow one probe request
	if !cb.Allow("docker.io") {
		t.Error("Expected Allow to return true for half-open circuit probe")
	}

	// Subsequent requests should be blocked until probe completes
	if cb.Allow("docker.io") {
		t.Error("Expected Allow to return false for second request in half-open state")
	}
}

func TestCircuitBreakerClosesOnSuccessfulProbe(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(2, 50*time.Millisecond)

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	// Wait for half-open
	time.Sleep(60 * time.Millisecond)

	// Allow probe request
	cb.Allow("docker.io")

	// Record success - should close circuit
	cb.RecordSuccess("docker.io")

	state := cb.GetState("docker.io")
	if state != CircuitClosed {
		t.Errorf("Expected circuit to close after successful probe, got %v", state)
	}

	// Should allow normal requests
	if !cb.Allow("docker.io") {
		t.Error("Expected Allow to return true after circuit closed")
	}
}

func TestCircuitBreakerReopensOnFailedProbe(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(2, 50*time.Millisecond)

	// Open the circuit
	for i := 0; i < 2; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	// Wait for half-open
	time.Sleep(60 * time.Millisecond)

	// Allow probe request
	cb.Allow("docker.io")

	// Record failure - should reopen circuit
	cb.RecordFailure("docker.io")

	state := cb.GetState("docker.io")
	if state != CircuitOpen {
		t.Errorf("Expected circuit to reopen after failed probe, got %v", state)
	}
}

func TestCircuitBreakerIsolatesRegistries(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(2, 30*time.Second)

	// Open circuit for docker.io
	for i := 0; i < 2; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	// docker.io should be open
	if cb.GetState("docker.io") != CircuitOpen {
		t.Error("Expected docker.io circuit to be open")
	}

	// ghcr.io should still be closed
	if cb.GetState("ghcr.io") != CircuitClosed {
		t.Error("Expected ghcr.io circuit to be closed")
	}

	// Should allow requests to ghcr.io
	if !cb.Allow("ghcr.io") {
		t.Error("Expected Allow to return true for ghcr.io")
	}
}

func TestCircuitBreakerReset(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(2, 30*time.Second)

	// Open circuit
	for i := 0; i < 2; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	if cb.GetState("docker.io") != CircuitOpen {
		t.Fatal("Expected circuit to be open")
	}

	// Reset the circuit
	cb.Reset("docker.io")

	// Should be closed now
	if cb.GetState("docker.io") != CircuitClosed {
		t.Error("Expected circuit to be closed after reset")
	}

	// Should allow requests
	if !cb.Allow("docker.io") {
		t.Error("Expected Allow to return true after reset")
	}
}

func TestCircuitBreakerResetAll(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(2, 30*time.Second)

	// Open circuits for multiple registries
	for _, reg := range []string{"docker.io", "ghcr.io", "quay.io"} {
		for i := 0; i < 2; i++ {
			cb.Allow(reg)
			cb.RecordFailure(reg)
		}
	}

	// Reset all
	cb.ResetAll()

	// All should be closed
	for _, reg := range []string{"docker.io", "ghcr.io", "quay.io"} {
		if cb.GetState(reg) != CircuitClosed {
			t.Errorf("Expected %s circuit to be closed after reset all", reg)
		}
	}
}

func TestCircuitBreakerConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(100, 30*time.Second)

	var wg sync.WaitGroup
	numGoroutines := 50
	numOperations := 100

	// Run concurrent operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			registry := "docker.io"
			if id%2 == 0 {
				registry = "ghcr.io"
			}

			for j := 0; j < numOperations; j++ {
				if cb.Allow(registry) {
					if j%3 == 0 {
						cb.RecordFailure(registry)
					} else {
						cb.RecordSuccess(registry)
					}
				}
				cb.GetState(registry)
			}
		}(i)
	}

	wg.Wait()

	// Should not panic or deadlock - if we get here, test passes
}

func TestErrCircuitOpen(t *testing.T) {
	// Verify ErrCircuitOpen can be used with errors.Is
	wrappedErr := errors.New("wrapped: " + ErrCircuitOpen.Error())
	if errors.Is(wrappedErr, ErrCircuitOpen) {
		t.Error("Expected errors.Is to return false for non-wrapped error")
	}
}

func TestCircuitBreakerGetStats(t *testing.T) {
	cb := NewCircuitBreakerWithConfig(5, 30*time.Second)

	// Record some failures
	for i := 0; i < 3; i++ {
		cb.Allow("docker.io")
		cb.RecordFailure("docker.io")
	}

	state, failures, lastFailure := cb.GetStats("docker.io")
	if state != CircuitClosed {
		t.Errorf("Expected state closed, got %v", state)
	}
	if failures != 3 {
		t.Errorf("Expected 3 failures, got %d", failures)
	}
	if lastFailure.IsZero() {
		t.Error("Expected lastFailure to be set")
	}
}
