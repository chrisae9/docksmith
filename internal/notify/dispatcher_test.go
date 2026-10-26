package notify

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/storage"
)

// These tests avoid relying on -race (unavailable without cgo): concurrency is
// checked through deadlines and explicit synchronisation on channels.

type sendCall struct {
	id  string // cfg["id"], identifies the service
	msg Message
}

type fakeSender struct {
	mu      sync.Mutex
	calls   []sendCall
	err     error
	block   chan struct{} // when non-nil, Send blocks until it is closed (ignoring ctx)
	entered chan struct{} // receives once per call, if non-nil
	onSend  func()        // called during each Send, if non-nil
}

func (f *fakeSender) Send(ctx context.Context, _ string, cfg map[string]string, msg Message) error {
	f.mu.Lock()
	f.calls = append(f.calls, sendCall{cfg["id"], msg})
	err, block, entered, onSend := f.err, f.block, f.entered, f.onSend
	f.mu.Unlock()
	if onSend != nil {
		onSend()
	}
	if entered != nil {
		select {
		case entered <- struct{}{}:
		default:
		}
	}
	if block != nil {
		<-block
	}
	return err
}

func (f *fakeSender) callsFor(id string) []sendCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sendCall
	for _, c := range f.calls {
		if c.id == id {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeSender) total() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func newDispatchStore(t *testing.T) *storage.SQLiteStorage {
	t.Helper()
	st, err := storage.NewSQLiteStorage(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("init storage: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func addService(t *testing.T, st storage.NotifyStore, id string, enabled bool) int64 {
	t.Helper()
	sid, err := st.SaveNotifyService(context.Background(), storage.NotifyServiceRow{
		Name: id, Type: "webhook", Enabled: enabled, Config: map[string]string{"id": id},
	})
	if err != nil {
		t.Fatalf("save service: %v", err)
	}
	return sid
}

func pending(t *testing.T, st storage.NotifyStore, sid int64) []storage.NotifyPendingRow {
	t.Helper()
	rows, err := st.ListNotifyPending(context.Background(), sid)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	return rows
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func staticSettings(s Settings) func() Settings { return func() Settings { return s } }

// withTiming returns default settings with the given kinds' timing overridden.
func withTiming(host string, t Timing, kinds ...EventKind) Settings {
	s := DefaultSettings(host)
	for _, k := range kinds {
		s.Timing[k] = t
	}
	return s
}

func newVersion(c, from, to string) Event {
	return Event{Kind: EventNewVersion, Container: c, FromVersion: from, ToVersion: to, Level: LevelPatch, At: time.Now()}
}

// Every enabled service gets the same events, routed by the global timing.
func TestDispatch_RoutesByGlobalTiming(t *testing.T) {
	st := newDispatchStore(t)
	a := addService(t, st, "a", true)
	b := addService(t, st, "b", true)
	fs := &fakeSender{}
	d := NewDispatcher(st, fs, staticSettings(withTiming("engager", TimingOff, EventComposeMismatch)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	// new_version defaults to summary: a pending row per service, nothing sent.
	d.Dispatch(ctx, []Event{newVersion("sonarr", "4.0.9", "4.0.10")})
	for _, sid := range []int64{a, b} {
		rows := pending(t, st, sid)
		if len(rows) != 1 || rows[0].DedupKey != "new_version|sonarr" || rows[0].Kind != "new_version" || rows[0].ContainerName != "sonarr" {
			t.Fatalf("service %d pending rows %+v", sid, rows)
		}
	}

	// update_failed defaults to immediate: sent to both services.
	d.Dispatch(ctx, []Event{{Kind: EventUpdateFailed, Container: "sonarr", OperationID: "op1", At: time.Now()}})
	waitFor(t, "update_failed to both services", func() bool {
		return len(fs.callsFor("a")) == 1 && len(fs.callsFor("b")) == 1
	})
	if got := fs.callsFor("a")[0].msg.Title; got != "[engager] Update failed: sonarr" {
		t.Fatalf("title %q", got)
	}

	// An Off kind goes nowhere.
	d.Dispatch(ctx, []Event{{Kind: EventComposeMismatch, Container: "sonarr", At: time.Now()}})
	time.Sleep(50 * time.Millisecond)
	if fs.total() != 2 || len(pending(t, st, a)) != 1 || len(pending(t, st, b)) != 1 {
		t.Fatalf("off kind was delivered: sends=%d", fs.total())
	}
}

// Digest changes only reach Dispatch when enabled; with the global setting Off
// (a container opted in by label) they go into the summary.
func TestDispatch_LabelOptedDigestGoesToSummary(t *testing.T) {
	st := newDispatchStore(t)
	sid := addService(t, st, "s", true)
	d := NewDispatcher(st, &fakeSender{}, staticSettings(DefaultSettings("h")))
	d.Dispatch(context.Background(), []Event{{Kind: EventDigestChanged, Container: "a", At: time.Now()}})
	if n := len(pending(t, st, sid)); n != 1 {
		t.Fatalf("pending = %d, want 1", n)
	}
	if n := len(d.work); n != 0 {
		t.Fatalf("enqueued %d sends, want 0", n)
	}
}

func TestDispatch_GroupsEventsPerService(t *testing.T) {
	st := newDispatchStore(t)
	addService(t, st, "imm", true)
	fs := &fakeSender{}
	d := NewDispatcher(st, fs, staticSettings(withTiming("engager", TimingImmediate, EventNewVersion)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)
	d.Dispatch(ctx, []Event{newVersion("a", "1.0.0", "1.0.1"), newVersion("b", "1.0.0", "1.0.1")})
	waitFor(t, "send", func() bool { return fs.total() == 1 })
	if got := fs.callsFor("imm")[0].msg.Title; got != "[engager] 2 container updates available" {
		t.Fatalf("title %q", got)
	}
}

func TestDispatch_PendingDedupKeys(t *testing.T) {
	st := newDispatchStore(t)
	sum := addService(t, st, "sum", true)
	d := NewDispatcher(st, &fakeSender{}, staticSettings(DefaultSettings("h")))
	at := time.Unix(1700000000, 0)
	d.Dispatch(context.Background(), []Event{
		{Kind: EventDigestChanged, Container: "a", At: at},
		{Kind: EventUpdateSucceeded, Container: "a", OperationID: "op1", At: at},
		{Kind: EventUpdateSucceeded, Container: "b", At: at},
	})
	keys := map[string]bool{}
	for _, r := range pending(t, st, sum) {
		keys[r.DedupKey] = true
	}
	for _, k := range []string{"digest_changed|a", "update_succeeded|a|op1", "update_succeeded|b|1700000000"} {
		if !keys[k] {
			t.Errorf("missing dedup key %q (have %v)", k, keys)
		}
	}
}

func TestDispatch_DisabledServicesGetNothing(t *testing.T) {
	st := newDispatchStore(t)
	off := addService(t, st, "off", false)
	d := NewDispatcher(st, &fakeSender{}, staticSettings(DefaultSettings("h")))

	// Not started: routing is synchronous, so the queue shows exactly what was enqueued.
	d.Dispatch(context.Background(), []Event{
		newVersion("sonarr", "1.0.0", "1.0.1"),
		{Kind: EventUpdateFailed, Container: "sonarr", OperationID: "op1", At: time.Now()},
	})
	if n := len(d.work); n != 0 {
		t.Fatalf("enqueued %d sends, want 0", n)
	}
	if n := len(pending(t, st, off)); n != 0 {
		t.Fatalf("disabled service has %d pending rows", n)
	}
}

func TestFlush_PrunesAppliedAndSuperseded(t *testing.T) {
	st := newDispatchStore(t)
	sum := addService(t, st, "sum", true)
	fs := &fakeSender{}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("engager")))
	ctx := context.Background()

	d.Dispatch(ctx, []Event{newVersion("sonarr", "4.0.9", "4.0.10"), newVersion("radarr", "5.0.0", "5.1.0")})
	d.Dispatch(ctx, []Event{newVersion("sonarr", "4.0.9", "4.0.11"), newVersion("absent", "1.0.0", "1.0.1")})
	current := func(c string) (string, bool) {
		switch c {
		case "sonarr":
			return "4.0.9", true
		case "radarr":
			return "5.1.0", true // already applied
		}
		return "", false // e.g. briefly missing from the last check: kept, the notifier prunes after its grace period
	}
	now := time.Now()
	d.FlushSummaries(ctx, current, now)

	calls := fs.callsFor("sum")
	if len(calls) != 1 {
		t.Fatalf("sends = %d, want 1", len(calls))
	}
	msg := calls[0].msg
	if msg.Title != "[engager] Docksmith summary: 2 items" {
		t.Fatalf("title %q", msg.Title)
	}
	var lines []string
	for _, l := range strings.Split(msg.Text, "\n") {
		if strings.Contains(l, "sonarr") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 1 || !strings.Contains(lines[0], "4.0.11") || strings.Contains(msg.Text, "4.0.10") {
		t.Fatalf("want one sonarr line with 4.0.11, got:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "radarr") {
		t.Fatalf("applied update in summary:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, "absent") {
		t.Fatalf("item for a container missing from the last check was dropped:\n%s", msg.Text)
	}
	if n := len(pending(t, st, sum)); n != 0 {
		t.Fatalf("pending rows after flush = %d", n)
	}
	row, _, _ := st.GetNotifyService(ctx, sum)
	if row.LastSummaryAt == nil || row.LastSuccessAt == nil {
		t.Fatalf("summary/success time not recorded: %+v", row)
	}

	// Only an already-applied update pending: nothing is sent.
	d.Dispatch(ctx, []Event{newVersion("radarr", "5.0.0", "5.1.0")})
	d.FlushSummaries(ctx, current, now)
	if n := fs.total(); n != 1 {
		t.Fatalf("sends = %d, want still 1", n)
	}
	if n := len(pending(t, st, sum)); n != 0 {
		t.Fatalf("stale pending rows left: %d", n)
	}
}

func TestFlush_EmptySendsNothing(t *testing.T) {
	st := newDispatchStore(t)
	sum := addService(t, st, "sum", true)
	fs := &fakeSender{}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("h")))
	d.FlushSummaries(context.Background(), func(string) (string, bool) { return "", false }, time.Now())
	if n := fs.total(); n != 0 {
		t.Fatalf("sends = %d", n)
	}
	row, _, _ := st.GetNotifyService(context.Background(), sum)
	if row.LastSummaryAt != nil {
		t.Fatal("summary time recorded for empty flush")
	}
}

func TestFlush_FailureKeepsPending(t *testing.T) {
	defer setRetryDelays(time.Millisecond, time.Millisecond)()
	st := newDispatchStore(t)
	sum := addService(t, st, "sum", true)
	fs := &fakeSender{err: errors.New("smtp down")}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("h")))
	ctx := context.Background()
	d.Dispatch(ctx, []Event{{Kind: EventUpdateSucceeded, Container: "a", OperationID: "op", At: time.Now()}})
	d.FlushSummaries(ctx, nil, time.Now())
	if n := len(pending(t, st, sum)); n != 1 {
		t.Fatalf("pending rows = %d, want 1 kept", n)
	}
	row, _, _ := st.GetNotifyService(ctx, sum)
	if row.LastSummaryAt != nil || row.LastError != "smtp down" {
		t.Fatalf("unexpected service state: %+v", row)
	}
}

func TestDispatch_DoesNotBlockOnHungSender(t *testing.T) {
	st := newDispatchStore(t)
	addService(t, st, "imm", true)
	release := make(chan struct{})
	defer close(release)
	fs := &fakeSender{block: release, entered: make(chan struct{}, 1)}
	d := NewDispatcher(st, fs, staticSettings(withTiming("h", TimingImmediate, EventNewVersion)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Dispatch(ctx, []Event{newVersion("a", "1.0.0", "1.0.1")})
	select {
	case <-fs.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("sender never called")
	}
	// Every worker is now hung or about to be; flood well past the queue size.
	for i := 0; i < 3*workQueueSize; i++ {
		start := time.Now()
		d.Dispatch(ctx, []Event{newVersion("a", "1.0.0", "1.0.1")})
		if el := time.Since(start); el > 100*time.Millisecond {
			t.Fatalf("Dispatch #%d took %v", i, el)
		}
	}
}

func TestDispatch_SafeAfterShutdown(t *testing.T) {
	st := newDispatchStore(t)
	addService(t, st, "imm", true)
	d := NewDispatcher(st, &fakeSender{}, staticSettings(DefaultSettings("h")))
	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx)
	cancel()

	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 2*workQueueSize; j++ {
					d.Dispatch(context.Background(), []Event{newVersion("a", "1.0.0", "1.0.1")})
				}
			}()
		}
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Dispatch blocked after shutdown")
	}
}

func TestSend_FailureRecorded(t *testing.T) {
	defer setRetryDelays(time.Millisecond, time.Millisecond)()
	st := newDispatchStore(t)
	imm := addService(t, st, "imm", true)
	fs := &fakeSender{err: errors.New("connection refused")}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("h")))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.Start(ctx)

	d.Dispatch(ctx, []Event{{Kind: EventUpdateFailed, Container: "a", At: time.Now()}})
	waitFor(t, "last_error recorded", func() bool {
		row, _, _ := st.GetNotifyService(ctx, imm)
		return row.LastError == "connection refused" && row.LastErrorAt != nil
	})
	if n := fs.total(); n != 3 {
		t.Fatalf("attempts = %d, want 3 (1 + 2 retries)", n)
	}
}

func TestSendTest(t *testing.T) {
	st := newDispatchStore(t)
	imm := addService(t, st, "imm", false)
	fs := &fakeSender{}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("engager")))
	if err := d.SendTest(context.Background(), imm); err != nil {
		t.Fatalf("SendTest: %v", err)
	}
	if c := fs.callsFor("imm"); len(c) != 1 || !strings.HasPrefix(c[0].msg.Title, "[engager] ") {
		t.Fatalf("calls %+v", c)
	}
	if err := d.SendTest(context.Background(), 9999); err == nil {
		t.Fatal("expected error for unknown service")
	}
	fs.err = errors.New("bad token")
	if err := d.SendTest(context.Background(), imm); err == nil || err.Error() != "bad token" {
		t.Fatalf("err = %v", err)
	}
	row, _, _ := st.GetNotifyService(context.Background(), imm)
	if row.LastError != "bad token" {
		t.Fatalf("last_error %q", row.LastError)
	}
}

func TestScheduler_FiresWhenDueAndFollowsCronChanges(t *testing.T) {
	set := DefaultSettings("h") // "0 8 * * *"
	var mu sync.Mutex
	settings := func() Settings { mu.Lock(); defer mu.Unlock(); return set }
	s := NewScheduler(nil, settings, nil)
	var fired []time.Time
	s.flush = func(_ context.Context, _ CurrentVersions, now time.Time) { fired = append(fired, now) }

	at := func(h, m int) time.Time { return time.Date(2026, 10, 26, h, m, 0, 0, time.Local) }
	ctx := context.Background()
	s.tick(ctx, at(7, 58)) // computes next = 08:00
	s.tick(ctx, at(7, 59))
	if len(fired) != 0 {
		t.Fatalf("fired early: %v", fired)
	}
	s.tick(ctx, at(8, 0))
	s.tick(ctx, at(8, 1))
	if len(fired) != 1 || !fired[0].Equal(at(8, 0)) {
		t.Fatalf("fired = %v, want once at 08:00", fired)
	}

	mu.Lock()
	set.SummaryCron = "30 8 * * *"
	mu.Unlock()
	s.tick(ctx, at(8, 2)) // recompute: next = 08:30
	s.tick(ctx, at(8, 29))
	s.tick(ctx, at(8, 31)) // late tick still fires
	if len(fired) != 2 || !fired[1].Equal(at(8, 31)) {
		t.Fatalf("fired = %v, want second fire at 08:31", fired)
	}

	mu.Lock()
	set.SummaryCron = "not a cron"
	mu.Unlock()
	s.tick(ctx, at(9, 0))
	s.tick(ctx, time.Date(2026, 10, 27, 9, 0, 0, 0, time.Local))
	if len(fired) != 2 {
		t.Fatalf("invalid cron fired: %v", fired)
	}
}

// setRetryDelays shortens the retry backoff for a test and returns a restore func.
func setRetryDelays(d ...time.Duration) func() {
	old := retryDelays
	retryDelays = d
	return func() { retryDelays = old }
}

func TestFlush_KeepsVersionUpsertedDuringSend(t *testing.T) {
	st := newDispatchStore(t)
	sum := addService(t, st, "sum", true)
	fs := &fakeSender{}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("h")))
	ctx := context.Background()
	d.Dispatch(ctx, []Event{newVersion("sonarr", "4.0.9", "4.0.10")})
	fs.onSend = func() { d.Dispatch(ctx, []Event{newVersion("sonarr", "4.0.9", "4.0.11")}) }
	current := func(string) (string, bool) { return "4.0.9", true }
	d.FlushSummaries(ctx, current, time.Now())

	rows := pending(t, st, sum)
	if len(rows) != 1 || !strings.Contains(rows[0].Payload, "4.0.11") {
		t.Fatalf("newer pending row lost: %+v", rows)
	}
	row, _, _ := st.GetNotifyService(ctx, sum)
	if row.LastSummaryAt == nil {
		t.Fatal("summary time not recorded")
	}
}

func TestFlush_StaleDropKeepsNewerUpsert(t *testing.T) {
	st := newDispatchStore(t)
	sum := addService(t, st, "sum", true)
	fs := &fakeSender{}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("h")))
	ctx := context.Background()
	d.Dispatch(ctx, []Event{newVersion("sonarr", "4.0.9", "4.0.10")})
	// sonarr is already at 4.0.10, so the row is stale; a newer version
	// arrives while the flush is deciding (inside the current() lookup).
	current := func(string) (string, bool) {
		d.Dispatch(ctx, []Event{newVersion("sonarr", "4.0.10", "4.0.11")})
		return "4.0.10", true
	}
	d.FlushSummaries(ctx, current, time.Now())
	rows := pending(t, st, sum)
	if len(rows) != 1 || !strings.Contains(rows[0].Payload, "4.0.11") {
		t.Fatalf("newer pending row lost: %+v", rows)
	}
	if fs.total() != 0 {
		t.Fatalf("sends = %d, want 0", fs.total())
	}
}

func TestSendTest_CallerCancelNotRecorded(t *testing.T) {
	st := newDispatchStore(t)
	sid := addService(t, st, "s", true)
	fs := &fakeSender{err: context.Canceled}
	d := NewDispatcher(st, fs, staticSettings(DefaultSettings("h")))
	ctx, cancel := context.WithCancel(context.Background())
	fs.onSend = cancel
	if err := d.SendTest(ctx, sid); err == nil {
		t.Fatal("expected error")
	}
	row, _, _ := st.GetNotifyService(context.Background(), sid)
	if row.LastError != "" || row.LastErrorAt != nil {
		t.Fatalf("caller cancellation recorded as service error: %q", row.LastError)
	}
}
