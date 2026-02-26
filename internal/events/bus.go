package events

import (
	"encoding/json"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Event types for the update workflow
const (
	EventUpdateProgress   = "update.progress"
	EventContainerUpdated = "container.updated"
	EventCheckProgress    = "check.progress"
	EventDroppedWarning   = "system.events_dropped" // Published when events are being dropped
)

// Event represents an event in the system
type Event struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// Subscriber is a channel that receives events
type Subscriber chan Event

// Bus manages event subscriptions and publishing
type Bus struct {
	mu              sync.RWMutex
	subscribers     map[string][]Subscriber
	droppedCount    atomic.Int64  // Total dropped events for monitoring
	lastDropWarning time.Time     // Rate limit drop warnings
	dropWarningMu   sync.Mutex
}

// NewBus creates a new event bus
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[string][]Subscriber),
	}
}

// Subscribe registers a subscriber for a specific event type
// Returns a channel that receives events and an unsubscribe function
func (b *Bus) Subscribe(eventType string) (Subscriber, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(chan Event, 100) // Buffer to avoid blocking publishers
	b.subscribers[eventType] = append(b.subscribers[eventType], ch)

	// Return unsubscribe function
	unsubscribe := func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		subs := b.subscribers[eventType]
		for i, sub := range subs {
			if sub == ch {
				b.subscribers[eventType] = append(subs[:i], subs[i+1:]...)
				close(ch)
				break
			}
		}
	}

	return ch, unsubscribe
}

// Publish sends an event to all subscribers of that event type.
// Uses a brief retry with backoff before dropping events to handle transient congestion.
func (b *Bus) Publish(event Event) {
	// Snapshot subscribers under lock, then release before sending.
	// This avoids deadlock: sendWithRetry -> recordDroppedEvent -> RLock (reentrant).
	b.mu.RLock()
	typeSubs := make([]Subscriber, len(b.subscribers[event.Type]))
	copy(typeSubs, b.subscribers[event.Type])
	wildcardSubs := make([]Subscriber, len(b.subscribers["*"]))
	copy(wildcardSubs, b.subscribers["*"])
	b.mu.RUnlock()

	// Don't retry for drop warning events to avoid recursion
	maxRetries := 3
	if event.Type == EventDroppedWarning {
		maxRetries = 1
	}

	// Publish to event type subscribers
	for _, ch := range typeSubs {
		b.sendWithRetry(ch, event, maxRetries)
	}

	// Publish to wildcard subscribers ("*")
	for _, ch := range wildcardSubs {
		b.sendWithRetry(ch, event, maxRetries)
	}
}

// sendWithRetry attempts to send an event to a channel with brief retries.
// Returns true if sent successfully, false if dropped.
func (b *Bus) sendWithRetry(ch Subscriber, event Event, maxRetries int) bool {
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case ch <- event:
			return true
		default:
			if attempt < maxRetries-1 {
				// Brief backoff before retry: 1ms, 2ms, 4ms
				time.Sleep(time.Millisecond * time.Duration(1<<attempt))
			}
		}
	}

	// Event dropped after retries
	b.recordDroppedEvent(event.Type)
	return false
}

// recordDroppedEvent tracks dropped events and publishes a warning if needed.
func (b *Bus) recordDroppedEvent(eventType string) {
	count := b.droppedCount.Add(1)
	log.Printf("EVENT BUS: dropped event %s after retries (total dropped: %d)", eventType, count)

	// Rate-limit drop warnings to avoid flooding (max once per 10 seconds)
	b.dropWarningMu.Lock()
	defer b.dropWarningMu.Unlock()

	if time.Since(b.lastDropWarning) > 10*time.Second {
		b.lastDropWarning = time.Now()

		// Publish a warning event (non-blocking, single attempt)
		// This allows the UI to show a "events may be delayed" warning
		warningEvent := Event{
			Type: EventDroppedWarning,
			Payload: map[string]interface{}{
				"dropped_count": count,
				"message":       "Event buffer full - some updates may be delayed",
			},
		}

		// Try to send to wildcard subscribers only (to avoid recursion)
		b.mu.RLock()
		for _, ch := range b.subscribers["*"] {
			select {
			case ch <- warningEvent:
			default:
				// Can't send warning either, just log
			}
		}
		b.mu.RUnlock()
	}
}

// GetDroppedCount returns the total number of dropped events.
// Useful for monitoring and diagnostics.
func (b *Bus) GetDroppedCount() int64 {
	return b.droppedCount.Load()
}

// ResetDroppedCount resets the dropped event counter.
func (b *Bus) ResetDroppedCount() {
	b.droppedCount.Store(0)
}

// MarshalEvent converts an event to JSON
func MarshalEvent(event Event) ([]byte, error) {
	return json.Marshal(event)
}
