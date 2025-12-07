package events

import (
	"sync"
	"testing"
	"time"
)

func TestNewBus(t *testing.T) {
	bus := NewBus()
	if bus == nil {
		t.Fatal("NewBus returned nil")
	}
	if bus.subscribers == nil {
		t.Fatal("subscribers map not initialized")
	}
}

func TestSubscribeAndPublish(t *testing.T) {
	bus := NewBus()

	// Subscribe to an event type
	ch, unsubscribe := bus.Subscribe("test.event")
	defer unsubscribe()

	// Publish an event
	event := Event{
		Type:    "test.event",
		Payload: map[string]interface{}{"key": "value"},
	}
	bus.Publish(event)

	// Verify event received
	select {
	case received := <-ch:
		if received.Type != event.Type {
			t.Errorf("expected type %s, got %s", event.Type, received.Type)
		}
		if received.Payload["key"] != "value" {
			t.Errorf("expected payload key=value, got %v", received.Payload)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestWildcardSubscriber(t *testing.T) {
	bus := NewBus()

	// Subscribe to wildcard
	ch, unsubscribe := bus.Subscribe("*")
	defer unsubscribe()

	// Publish different event types
	events := []Event{
		{Type: "event.one", Payload: map[string]interface{}{"n": 1}},
		{Type: "event.two", Payload: map[string]interface{}{"n": 2}},
	}

	for _, e := range events {
		bus.Publish(e)
	}

	// Verify both events received
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
			// Event received
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := NewBus()

	ch, unsubscribe := bus.Subscribe("test.event")

	// Publish before unsubscribe
	bus.Publish(Event{Type: "test.event"})

	select {
	case <-ch:
		// Event received
	case <-time.After(100 * time.Millisecond):
		t.Fatal("should have received event before unsubscribe")
	}

	// Unsubscribe
	unsubscribe()

	// Verify channel is closed
	_, ok := <-ch
	if ok {
		t.Fatal("channel should be closed after unsubscribe")
	}

	// Verify subscriber removed (no panic on publish)
	bus.Publish(Event{Type: "test.event"})
}

func TestMultipleSubscribers(t *testing.T) {
	bus := NewBus()

	ch1, unsub1 := bus.Subscribe("test.event")
	defer unsub1()
	ch2, unsub2 := bus.Subscribe("test.event")
	defer unsub2()

	bus.Publish(Event{Type: "test.event", Payload: map[string]interface{}{"test": true}})

	// Both subscribers should receive the event
	for i, ch := range []Subscriber{ch1, ch2} {
		select {
		case <-ch:
			// Event received
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("subscriber %d did not receive event", i+1)
		}
	}
}

func TestNoSubscribers(t *testing.T) {
	bus := NewBus()

	// Publishing to no subscribers should not panic
	bus.Publish(Event{Type: "test.event"})
}

func TestNonBlockingPublish(t *testing.T) {
	bus := NewBus()

	ch, unsubscribe := bus.Subscribe("test.event")
	defer unsubscribe()

	// Fill the channel buffer (100 events)
	for i := 0; i < 100; i++ {
		bus.Publish(Event{Type: "test.event", Payload: map[string]interface{}{"i": i}})
	}

	// This should not block even though buffer is full
	done := make(chan bool)
	go func() {
		bus.Publish(Event{Type: "test.event", Payload: map[string]interface{}{"overflow": true}})
		done <- true
	}()

	select {
	case <-done:
		// Publish completed without blocking
	case <-time.After(100 * time.Millisecond):
		t.Fatal("publish blocked on full channel")
	}

	// Drain the channel
	for i := 0; i < 100; i++ {
		<-ch
	}
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	bus := NewBus()

	var wg sync.WaitGroup

	// Start multiple subscribers
	subscribers := make([]Subscriber, 5)
	unsubscribers := make([]func(), 5)
	for i := 0; i < 5; i++ {
		subscribers[i], unsubscribers[i] = bus.Subscribe("concurrent.event")
	}

	// Publish concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			bus.Publish(Event{Type: "concurrent.event", Payload: map[string]interface{}{"n": n}})
		}(i)
	}

	wg.Wait()

	// Clean up
	for _, unsub := range unsubscribers {
		unsub()
	}
}

func TestMarshalEvent(t *testing.T) {
	event := Event{
		Type:    "test.event",
		Payload: map[string]interface{}{"key": "value", "num": 42},
	}

	data, err := MarshalEvent(event)
	if err != nil {
		t.Fatalf("MarshalEvent failed: %v", err)
	}

	// Verify JSON contains expected fields
	json := string(data)
	if json == "" {
		t.Fatal("MarshalEvent returned empty string")
	}

	// Basic check that it looks like valid JSON
	if json[0] != '{' || json[len(json)-1] != '}' {
		t.Errorf("MarshalEvent output doesn't look like JSON: %s", json)
	}
}

func TestEventConstants(t *testing.T) {
	// Verify event constants are defined
	if EventUpdateProgress == "" {
		t.Error("EventUpdateProgress is empty")
	}
	if EventContainerUpdated == "" {
		t.Error("EventContainerUpdated is empty")
	}
	if EventCheckProgress == "" {
		t.Error("EventCheckProgress is empty")
	}
}
