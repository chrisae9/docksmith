package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/events"
)

// TestEventsHandlerReturnsOnShutdown verifies that an open SSE connection does
// not block server shutdown until the deadline expires.
func TestEventsHandlerReturnsOnShutdown(t *testing.T) {
	s := &Server{
		eventBus:   events.NewBus(),
		shutdownCh: make(chan struct{}),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodGet, "/api/events", nil)
		s.handleEvents(httptest.NewRecorder(), req)
	}()

	close(s.shutdownCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handleEvents did not return after shutdown was signalled")
	}
}
