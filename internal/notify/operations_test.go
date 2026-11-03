package notify

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/storage"
)

type fakeOps struct{ ops map[string]storage.UpdateOperation }

func (f fakeOps) GetUpdateOperation(_ context.Context, id string) (storage.UpdateOperation, bool, error) {
	op, ok := f.ops[id]
	return op, ok, nil
}

type collector struct {
	mu sync.Mutex
	ev []Event
}

func (c *collector) emit(e Event) { c.mu.Lock(); c.ev = append(c.ev, e); c.mu.Unlock() }
func (c *collector) kinds() map[EventKind]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := map[EventKind]int{}
	for _, e := range c.ev {
		m[e.Kind]++
	}
	return m
}

func progress(op, container, stage, msg string) events.Event {
	return events.Event{Type: events.EventUpdateProgress, Payload: map[string]interface{}{
		"operation_id": op, "container_name": container, "stack_name": "s", "stage": stage, "message": msg,
	}}
}

func TestOperationWatcher_SingleCompleteDedup(t *testing.T) {
	c := &collector{}
	w := NewOperationWatcher(nil, fakeOps{map[string]storage.UpdateOperation{
		"op1": {OperationID: "op1", ContainerName: "web", OperationType: "single", OldVersion: "1", NewVersion: "2"},
	}}, c.emit)
	w.handle(context.Background(), progress("op1", "web", "complete", "ok"))
	w.handle(context.Background(), progress("op1", "web", "complete", "ok"))
	if k := c.kinds(); k[EventUpdateSucceeded] != 1 || len(c.ev) != 1 {
		t.Fatalf("got %v", k)
	}
	if e := c.ev[0]; e.Container != "web" || e.FromVersion != "1" || e.ToVersion != "2" || e.OperationID != "op1" {
		t.Fatalf("bad event %+v", e)
	}
}

func TestOperationWatcher_Batch(t *testing.T) {
	c := &collector{}
	op := storage.UpdateOperation{OperationID: "b", OperationType: "batch", BatchDetails: []storage.BatchContainerDetail{
		{ContainerName: "a", OldVersion: "1", NewVersion: "2"}, {ContainerName: "b", OldVersion: "1", NewVersion: "2"},
		{ContainerName: "c", OldVersion: "1", NewVersion: "2"}, {ContainerName: "d", OldVersion: "3", NewVersion: "4"},
		{ContainerName: "e", OldVersion: "5", NewVersion: "6"},
	}}
	w := NewOperationWatcher(nil, fakeOps{map[string]storage.UpdateOperation{"b": op}}, c.emit)
	ctx := context.Background()
	for _, n := range []string{"a", "b", "c"} {
		w.handle(ctx, progress("b", n, "complete", ""))
	}
	for _, n := range []string{"d", "e"} {
		w.handle(ctx, progress("b", n, "failed", "boom"))
	}
	w.handle(ctx, progress("b", "", "failed", "boom"))
	w.handle(ctx, progress("b", "", "complete", "batch done"))
	if k := c.kinds(); k[EventUpdateSucceeded] != 3 || k[EventUpdateFailed] != 2 || len(c.ev) != 5 {
		t.Fatalf("got %v", k)
	}
	for _, e := range c.ev {
		if e.Container == "d" && (e.FromVersion != "3" || e.ToVersion != "4") {
			t.Fatalf("bad versions %+v", e)
		}
	}
}

func TestOperationWatcher_EmptyContainerFailedSingle(t *testing.T) {
	c := &collector{}
	w := NewOperationWatcher(nil, fakeOps{map[string]storage.UpdateOperation{
		"o": {OperationID: "o", ContainerName: "web", OperationType: "single"},
	}}, c.emit)
	w.handle(context.Background(), progress("o", "", "failed", "boom"))
	if k := c.kinds(); k[EventUpdateFailed] != 1 || c.ev[0].Container != "web" {
		t.Fatalf("got %+v", c.ev)
	}
}

func TestOperationWatcher_Rollback(t *testing.T) {
	c := &collector{}
	w := NewOperationWatcher(nil, fakeOps{map[string]storage.UpdateOperation{
		"r": {OperationID: "r", ContainerName: "web", OperationType: "rollback"},
	}}, c.emit)
	ctx := context.Background()
	w.handle(ctx, progress("r", "web", "complete", "done"))
	if k := c.kinds(); k[EventRollback] != 1 {
		t.Fatalf("got %v", k)
	}
	w.handle(ctx, progress("r", "web", "failed", "bad"))
	if e := c.ev[1]; e.Kind != EventUpdateFailed || e.Message != "Rollback failed: bad" {
		t.Fatalf("got %+v", e)
	}
}

func TestOperationWatcher_StartViaBus(t *testing.T) {
	bus := events.NewBus()
	done := make(chan Event, 4)
	w := NewOperationWatcher(bus, fakeOps{map[string]storage.UpdateOperation{
		"x": {OperationID: "x", ContainerName: "web", OperationType: "single"},
	}}, func(e Event) { done <- e })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.Start(ctx)
	bus.Publish(progress("x", "web", "pulling", ""))
	bus.Publish(progress("x", "web", "complete", ""))
	bus.Publish(progress("x", "web", "complete", ""))
	select {
	case e := <-done:
		if e.Kind != EventUpdateSucceeded {
			t.Fatalf("got %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	select {
	case e := <-done:
		t.Fatalf("unexpected extra event %+v", e)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestOperationWatcher_NoContainersOp(t *testing.T) {
	c := &collector{}
	w := NewOperationWatcher(nil, fakeOps{map[string]storage.UpdateOperation{
		"n": {OperationID: "n", OperationType: "batch"},
	}}, c.emit)
	w.handle(context.Background(), progress("n", "", "complete", "No containers to update"))
	if len(c.ev) != 0 {
		t.Fatalf("got %+v", c.ev)
	}
}

func TestOperationWatcher_IgnoresNonUpdateOperations(t *testing.T) {
	ctx := context.Background()
	for _, typ := range []string{"restart", "stop", "start", "remove", "label_change", "fix_mismatch", "update", ""} {
		t.Run(typ, func(t *testing.T) {
			c := &collector{}
			w := NewOperationWatcher(nil, fakeOps{map[string]storage.UpdateOperation{
				"o": {OperationID: "o", ContainerName: "web", OperationType: typ, OldVersion: `{"a":"1"}`, NewVersion: `{"a":"2"}`},
			}}, c.emit)
			w.handle(ctx, progress("o", "web", "complete", "done"))
			w.handle(ctx, progress("o", "web", "failed", "boom"))
			if len(c.ev) != 0 {
				t.Fatalf("type %q produced events %+v", typ, c.ev)
			}
		})
	}
}

func TestOperationWatcher_AllowsUpdateTypes(t *testing.T) {
	ctx := context.Background()
	for _, typ := range []string{"single", "batch", "stack"} {
		c := &collector{}
		w := NewOperationWatcher(nil, fakeOps{map[string]storage.UpdateOperation{
			"o": {OperationID: "o", ContainerName: "web", OperationType: typ},
		}}, c.emit)
		w.handle(ctx, progress("o", "web", "complete", ""))
		if k := c.kinds(); k[EventUpdateSucceeded] != 1 {
			t.Fatalf("type %q: got %v", typ, k)
		}
	}
}
