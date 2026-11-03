package notify

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/storage"
)

const seenTTL = time.Hour

// updateOperationTypes are the operation types (set by the update
// orchestrator) that represent image updates. Rollbacks are handled separately;
// every other type on the progress bus (restart, stop, label_change, ...) is ignored.
var updateOperationTypes = map[string]bool{"single": true, "batch": true, "stack": true}

// OperationLookup fetches stored update operations.
type OperationLookup interface {
	GetUpdateOperation(ctx context.Context, operationID string) (storage.UpdateOperation, bool, error)
}

// OperationWatcher turns update/rollback progress events into notification events.
type OperationWatcher struct {
	bus  *events.Bus
	ops  OperationLookup
	emit func(Event)

	mu   sync.Mutex
	seen map[string]time.Time
}

// NewOperationWatcher creates a watcher. emit must be non-blocking.
func NewOperationWatcher(bus *events.Bus, ops OperationLookup, emit func(Event)) *OperationWatcher {
	return &OperationWatcher{bus: bus, ops: ops, emit: emit, seen: make(map[string]time.Time)}
}

// Start subscribes to progress events and runs until ctx is cancelled.
func (w *OperationWatcher) Start(ctx context.Context) {
	sub, unsubscribe := w.bus.Subscribe(events.EventUpdateProgress)
	go func() {
		defer unsubscribe()
		for {
			select {
			case <-ctx.Done():
				return
			case e, ok := <-sub:
				if !ok {
					return
				}
				if stage, _ := e.Payload["stage"].(string); stage != "complete" && stage != "failed" {
					continue
				}
				// Hand off so storage lookups never stall the bus subscriber.
				go w.handle(ctx, e)
			}
		}
	}()
}

func (w *OperationWatcher) handle(ctx context.Context, e events.Event) {
	str := func(k string) string { s, _ := e.Payload[k].(string); return s }
	opID, container, stage, message := str("operation_id"), str("container_name"), str("stage"), str("message")
	if opID == "" || (stage != "complete" && stage != "failed") {
		return
	}

	key := opID + "|" + container + "|" + stage
	if !w.markSeen(key) {
		return
	}

	op, found, err := w.ops.GetUpdateOperation(ctx, opID)
	if err != nil || !found {
		if err != nil {
			log.Printf("notify: operation lookup %s failed: %v", opID, err)
		}
		w.unmark(key) // allow a later duplicate event to retry
		return
	}
	rollback := op.OperationType == "rollback"
	if !rollback && !updateOperationTypes[op.OperationType] {
		return // restart, label_change, fix_mismatch etc. are not updates
	}

	name, stack := container, str("stack_name")
	from, to := op.OldVersion, op.NewVersion
	if name == "" {
		if len(op.BatchDetails) > 0 {
			return // per-container events are published individually
		}
		name = op.ContainerName
		if name == "" {
			return
		}
	}
	if stack == "" {
		stack = op.StackName
	}
	for _, d := range op.BatchDetails {
		if d.ContainerName == name {
			from, to = d.OldVersion, d.NewVersion
			if stack == "" {
				stack = d.StackName
			}
			break
		}
	}

	ev := Event{Container: name, Stack: stack, FromVersion: from, ToVersion: to, Message: message, OperationID: opID, At: time.Now()}
	switch {
	case stage == "complete" && rollback:
		ev.Kind = EventRollback
	case stage == "complete":
		ev.Kind = EventUpdateSucceeded
	case rollback:
		ev.Kind = EventUpdateFailed
		ev.Message = "Rollback failed: " + message
	default:
		ev.Kind = EventUpdateFailed
	}
	w.emit(ev)
}

// markSeen records key and reports whether it was new, pruning stale entries.
func (w *OperationWatcher) markSeen(key string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	now := time.Now()
	for k, t := range w.seen {
		if now.Sub(t) > seenTTL {
			delete(w.seen, k)
		}
	}
	if _, ok := w.seen[key]; ok {
		return false
	}
	w.seen[key] = now
	return true
}

func (w *OperationWatcher) unmark(key string) {
	w.mu.Lock()
	delete(w.seen, key)
	w.mu.Unlock()
}
