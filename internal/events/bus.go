package events

// Event types for the update workflow
const (
	EventUpdateProgress     = "update.progress"
	EventContainerUpdated   = "container.updated"
)

// Event represents an event in the system
type Event struct {
	Type    string
	Payload map[string]interface{}
}

// Bus is a no-op event bus for CLI mode (no web UI)
type Bus struct{}

// NewBus creates a new no-op event bus
func NewBus() *Bus {
	return &Bus{}
}

// Publish does nothing in CLI mode (no subscribers)
func (b *Bus) Publish(event Event) {
	// No-op: CLI doesn't need real-time events
}

// Subscribe does nothing in CLI mode
func (b *Bus) Subscribe(topic string, handler func(interface{})) {
	// No-op: CLI doesn't need subscriptions
}
