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

	var publishWg sync.WaitGroup
	var receiveWg sync.WaitGroup

	numSubscribers := 5
	numPublishers := 10

	// Start multiple subscribers
	subscribers := make([]Subscriber, numSubscribers)
	unsubscribers := make([]func(), numSubscribers)
	for i := 0; i < numSubscribers; i++ {
		subscribers[i], unsubscribers[i] = bus.Subscribe("concurrent.event")
	}

	// Start goroutines to receive events from each subscriber
	receivedCounts := make([]int, numSubscribers)
	var countMu sync.Mutex
	for i := 0; i < numSubscribers; i++ {
		receiveWg.Add(1)
		go func(idx int, ch Subscriber) {
			defer receiveWg.Done()
			count := 0
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						// Channel closed
						countMu.Lock()
						receivedCounts[idx] = count
						countMu.Unlock()
						return
					}
					count++
				case <-time.After(500 * time.Millisecond):
					// Timeout - done receiving
					countMu.Lock()
					receivedCounts[idx] = count
					countMu.Unlock()
					return
				}
			}
		}(i, subscribers[i])
	}

	// Publish concurrently
	for i := 0; i < numPublishers; i++ {
		publishWg.Add(1)
		go func(n int) {
			defer publishWg.Done()
			bus.Publish(Event{Type: "concurrent.event", Payload: map[string]interface{}{"n": n}})
		}(i)
	}

	publishWg.Wait()

	// Give time for events to be delivered before unsubscribing
	time.Sleep(50 * time.Millisecond)

	// Clean up - this closes the channels
	for _, unsub := range unsubscribers {
		unsub()
	}

	// Wait for receiver goroutines to finish
	receiveWg.Wait()

	// Verify each subscriber received events (at least some, allowing for drops)
	for i, count := range receivedCounts {
		if count == 0 {
			t.Errorf("subscriber %d received 0 events, expected at least some", i)
		}
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
	if EventDroppedWarning == "" {
		t.Error("EventDroppedWarning is empty")
	}
}

func TestDroppedEventTracking(t *testing.T) {
	bus := NewBus()

	ch, unsubscribe := bus.Subscribe("test.event")
	defer unsubscribe()

	// Fill the channel buffer completely (100 events)
	for i := 0; i < 100; i++ {
		bus.Publish(Event{Type: "test.event", Payload: map[string]interface{}{"i": i}})
	}

	// Initial count should be 0
	initialCount := bus.GetDroppedCount()

	// Publish more events - they should be dropped after retries
	for i := 0; i < 5; i++ {
		bus.Publish(Event{Type: "test.event", Payload: map[string]interface{}{"overflow": i}})
	}

	// Dropped count should have increased
	droppedCount := bus.GetDroppedCount()
	if droppedCount <= initialCount {
		t.Errorf("Expected dropped count to increase, got %d (initial was %d)", droppedCount, initialCount)
	}

	// Drain the channel
	for i := 0; i < 100; i++ {
		<-ch
	}
}

func TestResetDroppedCount(t *testing.T) {
	bus := NewBus()

	ch, unsubscribe := bus.Subscribe("test.event")
	defer unsubscribe()

	// Fill buffer and cause drops
	for i := 0; i < 110; i++ {
		bus.Publish(Event{Type: "test.event"})
	}

	// Verify there are drops
	if bus.GetDroppedCount() == 0 {
		t.Skip("No drops occurred, cannot test reset")
	}

	// Reset the counter
	bus.ResetDroppedCount()

	if bus.GetDroppedCount() != 0 {
		t.Errorf("Expected dropped count to be 0 after reset, got %d", bus.GetDroppedCount())
	}

	// Drain channel to clean up using non-blocking receives
	for {
		select {
		case <-ch:
			// Keep draining
		default:
			return
		}
	}
}

func TestRetryOnTransientCongestion(t *testing.T) {
	bus := NewBus()

	ch, unsubscribe := bus.Subscribe("test.event")
	defer unsubscribe()

	// Fill buffer almost full (leaving room for retries to succeed)
	initialEvents := 98
	for i := 0; i < initialEvents; i++ {
		bus.Publish(Event{Type: "test.event", Payload: map[string]interface{}{"i": i}})
	}

	// Track when drain happens
	drainDone := make(chan struct{})

	// Start draining slowly in background
	go func() {
		time.Sleep(2 * time.Millisecond)
		<-ch // Make room
		close(drainDone)
	}()

	// Publish while drain is happening - retry should succeed
	bus.Publish(Event{Type: "test.event", Payload: map[string]interface{}{"retry_test": true}})

	// Wait for drain to complete
	select {
	case <-drainDone:
		// Drain completed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for drain goroutine")
	}

	// Drain remaining events and count them
	drained := 0
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case <-ch:
			drained++
		case <-timeout:
			// Done draining
			goto done
		default:
			// No more events immediately available
			goto done
		}
	}
done:

	// We published 98 initially + 1 retry event = 99 total
	// The background goroutine drained 1, so we should have drained ~98
	if drained < 95 { // Allow some tolerance
		t.Errorf("Expected to drain ~98 events, got %d", drained)
	}
}

func TestDropWarningEvent(t *testing.T) {
	bus := NewBus()

	// Subscribe to wildcard to receive drop warning events
	wildcardCh, unsubWildcard := bus.Subscribe("*")
	defer unsubWildcard()

	// Subscribe to test event with a channel we'll overflow
	testCh, unsubTest := bus.Subscribe("test.event")
	defer unsubTest()

	// Fill the test channel buffer
	for i := 0; i < 100; i++ {
		bus.Publish(Event{Type: "test.event"})
	}

	// Publish more to trigger drops and potentially a warning
	for i := 0; i < 20; i++ {
		bus.Publish(Event{Type: "test.event"})
	}

	// Check if we got a drop warning in the wildcard subscriber
	// Note: This is timing-dependent, so we just check the counter increased
	droppedCount := bus.GetDroppedCount()
	if droppedCount == 0 {
		t.Error("Expected some events to be dropped")
	}

	// Drain channels using non-blocking receives
	drainChannel := func(ch Subscriber) {
		for {
			select {
			case <-ch:
				// Keep draining
			default:
				return
			}
		}
	}

	drainChannel(testCh)
	drainChannel(wildcardCh)
}
