package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/storage"
)

const (
	workQueueSize = 64
	workerCount   = 2
	recordTimeout = 10 * time.Second
)

// Send policy: each attempt is bounded by sendTimeout; failed attempts are
// retried after each of retryDelays. Vars so tests can shorten them.
var (
	sendTimeout = 30 * time.Second
	retryDelays = []time.Duration{5 * time.Second, 15 * time.Second}
)

// CurrentVersions reports a container's current version and whether it exists.
type CurrentVersions func(container string) (version string, exists bool)

type sendJob struct {
	svc storage.NotifyServiceRow
	msg Message
}

// Dispatcher routes events to services: immediate sends go through a bounded
// queue to background workers; summary items are stored as pending rows.
// All methods are safe for concurrent use.
type Dispatcher struct {
	store    storage.NotifyStore
	sender   Sender
	settings func() Settings
	work     chan sendJob // never closed; producers may outlive the workers
	stop     chan struct{}
	stopOnce sync.Once
}

// NewDispatcher creates a dispatcher. Call Start to begin sending.
func NewDispatcher(store storage.NotifyStore, sender Sender, settings func() Settings) *Dispatcher {
	return &Dispatcher{
		store:    store,
		sender:   sender,
		settings: settings,
		work:     make(chan sendJob, workQueueSize),
		stop:     make(chan struct{}),
	}
}

// Start launches the send workers; they exit when ctx is done. It does not block.
func (d *Dispatcher) Start(ctx context.Context) {
	for i := 0; i < workerCount; i++ {
		go d.worker(ctx)
	}
	go func() {
		<-ctx.Done()
		d.stopOnce.Do(func() { close(d.stop) })
	}()
}

func (d *Dispatcher) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-d.work:
			_ = d.deliver(ctx, j.svc, j.msg)
		}
	}
}

// Dispatch routes evs to every enabled service by each kind's global timing:
// off kinds are dropped, immediate kinds are sent now, summary kinds are stored
// as pending. It never blocks on sending: pending rows are written
// synchronously, and immediate messages are queued (or dropped with a log line
// if the queue is full or the dispatcher has shut down).
func (d *Dispatcher) Dispatch(ctx context.Context, evs []Event) {
	if len(evs) == 0 {
		return
	}
	s := d.settings()
	var now, later []Event
	for _, e := range evs {
		switch eventTiming(s, e.Kind) {
		case TimingImmediate:
			now = append(now, e)
		case TimingSummary:
			later = append(later, e)
		}
	}
	if len(now) == 0 && len(later) == 0 {
		return
	}
	svcs, err := d.store.ListNotifyServices(ctx)
	if err != nil {
		log.Printf("notify: list services: %v", err)
		return
	}
	for _, svc := range svcs {
		if !svc.Enabled {
			continue
		}
		for _, e := range later {
			d.storePending(ctx, svc, e)
		}
		if len(now) > 0 {
			d.enqueue(sendJob{svc: svc, msg: Render(s, now, false)})
		}
	}
}

// eventTiming is the timing for kind. Digest changes only reach Dispatch when
// enabled globally or by a container's docksmith.notify-digest=true label, so
// with the global setting off they go into the summary.
func eventTiming(s Settings, kind EventKind) Timing {
	t := s.Timing[kind]
	if kind == EventDigestChanged && t == TimingOff {
		return TimingSummary
	}
	return t
}

func (d *Dispatcher) storePending(ctx context.Context, svc storage.NotifyServiceRow, e Event) {
	payload, err := json.Marshal(e)
	if err != nil {
		log.Printf("notify: encode pending %s for %q: %v", e.Kind, e.Container, err)
		return
	}
	row := storage.NotifyPendingRow{
		ServiceID:     svc.ID,
		DedupKey:      dedupKey(e),
		Kind:          string(e.Kind),
		ContainerName: e.Container,
		Payload:       string(payload),
		CreatedAt:     time.Now(),
	}
	if err := d.store.UpsertNotifyPending(ctx, row); err != nil {
		log.Printf("notify: store pending %s for service %q: %v", e.Kind, svc.Name, err)
	}
}

func dedupKey(e Event) string {
	switch e.Kind {
	case EventNewVersion, EventDigestChanged:
		return string(e.Kind) + "|" + e.Container
	}
	ref := e.OperationID
	if ref == "" {
		ref = strconv.FormatInt(e.At.Unix(), 10)
	}
	return string(e.Kind) + "|" + e.Container + "|" + ref
}

func (d *Dispatcher) enqueue(j sendJob) {
	select {
	case <-d.stop:
		log.Printf("notify: shutting down; dropped message %q for service %q", j.msg.Title, j.svc.Name)
		return
	default:
	}
	select {
	case d.work <- j:
	default:
		log.Printf("notify: send queue full; dropped message %q for service %q", j.msg.Title, j.svc.Name)
	}
}

// deliver sends msg with the timeout/retry policy and records the result.
// A send interrupted because ctx ended (shutdown) is neither retried nor recorded.
func (d *Dispatcher) deliver(ctx context.Context, svc storage.NotifyServiceRow, msg Message) error {
	var err error
	for attempt := 0; ; attempt++ {
		err = d.sendOnce(ctx, svc, msg)
		if err == nil || ctx.Err() != nil || attempt >= len(retryDelays) {
			break
		}
		t := time.NewTimer(retryDelays[attempt])
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
		}
		if ctx.Err() != nil {
			break
		}
	}
	if err != nil && ctx.Err() != nil {
		log.Printf("notify: send to service %q aborted by shutdown", svc.Name)
		return err
	}
	d.record(ctx, svc, err)
	return err
}

func (d *Dispatcher) sendOnce(ctx context.Context, svc storage.NotifyServiceRow, msg Message) error {
	sctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	return d.sender.Send(sctx, svc.Type, svc.Config, msg)
}

func (d *Dispatcher) record(ctx context.Context, svc storage.NotifyServiceRow, sendErr error) {
	if sendErr != nil {
		// Sender errors are already redacted of secrets.
		log.Printf("notify: send to service %q failed: %v", svc.Name, sendErr)
	}
	rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recordTimeout)
	defer cancel()
	if err := d.store.RecordNotifySendResult(rctx, svc.ID, sendErr, time.Now()); err != nil {
		log.Printf("notify: record send result for service %q: %v", svc.Name, err)
	}
}

// FlushSummaries sends each enabled service's pending items as one summary.
// new_version items whose container is already at (or past) the announced
// version are dropped; items for containers missing from the last check are
// kept (the notifier prunes them after its grace period). Nothing is sent when no items remain. Sent
// rows are deleted only after a successful send. It sends synchronously.
func (d *Dispatcher) FlushSummaries(ctx context.Context, current CurrentVersions, now time.Time) {
	s := d.settings()
	svcs, err := d.store.ListNotifyServices(ctx)
	if err != nil {
		log.Printf("notify: list services: %v", err)
		return
	}
	for _, svc := range svcs {
		if !svc.Enabled || ctx.Err() != nil {
			continue
		}
		rows, err := d.store.ListNotifyPending(ctx, svc.ID)
		if err != nil {
			log.Printf("notify: list pending for service %q: %v", svc.Name, err)
			continue
		}
		var keep []Event
		var keptRows []storage.NotifyPendingRow
		var drop []storage.NotifyPendingRow
		for _, r := range rows {
			var e Event
			if err := json.Unmarshal([]byte(r.Payload), &e); err != nil {
				log.Printf("notify: dropping unreadable pending item %d: %v", r.ID, err)
				drop = append(drop, r)
				continue
			}
			if e.Kind == EventNewVersion && current != nil {
				// Only drop updates already applied. A container missing from the
				// latest check keeps its items: the notifier prunes them once the
				// container has been gone past its grace period.
				cur, exists := current(e.Container)
				if exists && !comparisonUnknown(cur, e.ToVersion) {
					if _, newer := Classify(cur, e.ToVersion); !newer {
						drop = append(drop, r)
						continue
					}
				}
			}
			keep = append(keep, e)
			keptRows = append(keptRows, r)
		}
		// Conditional deletes: a row replaced by a newer upsert (same id, new
		// payload) since it was listed survives, on both paths.
		if err := d.store.DeleteNotifyPendingIfUnchanged(ctx, drop); err != nil {
			log.Printf("notify: delete stale pending for service %q: %v", svc.Name, err)
		}
		if len(keep) == 0 {
			continue
		}
		if err := d.deliver(ctx, svc, Render(s, keep, true)); err != nil {
			continue
		}
		if err := d.store.DeleteNotifyPendingIfUnchanged(ctx, keptRows); err != nil {
			log.Printf("notify: delete sent pending for service %q: %v", svc.Name, err)
			continue
		}
		if err := d.store.SetNotifySummarySent(ctx, svc.ID, now); err != nil {
			log.Printf("notify: set summary time for service %q: %v", svc.Name, err)
		}
	}
}

// SendTest synchronously sends a test message to a service (enabled or not)
// with a single attempt, records the result (unless the caller's ctx ended),
// and returns the send error.
func (d *Dispatcher) SendTest(ctx context.Context, serviceID int64) error {
	svc, ok, err := d.store.GetNotifyService(ctx, serviceID)
	if err != nil {
		return fmt.Errorf("load notification service: %w", err)
	}
	if !ok {
		return fmt.Errorf("notification service %d not found", serviceID)
	}
	title := "Test notification"
	if name := d.settings().ServerName; name != "" {
		title = "[" + name + "] " + title
	}
	text := fmt.Sprintf("This is a test notification from Docksmith for service %q.", svc.Name)
	msg := Message{Title: title, Text: text, HTML: "<p>" + html.EscapeString(text) + "</p>"}
	err = d.sendOnce(ctx, svc, msg)
	if err != nil && ctx.Err() != nil {
		return err // the caller gave up (e.g. request cancelled); not the service's fault
	}
	d.record(ctx, svc, err)
	return err
}
