package events

import (
	"encoding/json"
	"sync"
)

// Event types for the update workflow
const (
	EventUpdateProgress   = "update.progress"
	EventContainerUpdated = "container.updated"
	EventCheckProgress    = "check.progress"
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
	mu          sync.RWMutex
	subscribers map[string][]Subscriber
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

// Publish sends an event to all subscribers of that event type
func (b *Bus) Publish(event Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// Also publish to wildcard subscribers
	for _, ch := range b.subscribers[event.Type] {
		select {
		case ch <- event:
		default:
			// Channel full, skip (non-blocking)
		}
	}

	// Publish to wildcard subscribers ("*")
	for _, ch := range b.subscribers["*"] {
		select {
		case ch <- event:
		default:
		}
	}
}

// MarshalEvent converts an event to JSON
func MarshalEvent(event Event) ([]byte, error) {
	return json.Marshal(event)
}
