package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newNotifyTestStorage(t *testing.T) (*SQLiteStorage, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	s, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dbPath
}

func TestNotifyServices_CRUD(t *testing.T) {
	s, _ := newNotifyTestStorage(t)
	ctx := context.Background()

	id, err := s.SaveNotifyService(ctx, NotifyServiceRow{
		Name: "tg", Type: "telegram", Delivery: "immediate", Enabled: true,
		Events: []string{"update_available"}, Config: map[string]string{"token": "x"},
	})
	if err != nil || id == 0 {
		t.Fatalf("insert: id=%d err=%v", id, err)
	}

	got, ok, err := s.GetNotifyService(ctx, id)
	if err != nil || !ok {
		t.Fatalf("get: ok=%v err=%v", ok, err)
	}
	if got.Name != "tg" || !got.Enabled || got.Config["token"] != "x" || len(got.Events) != 1 || got.LastSuccessAt != nil {
		t.Fatalf("unexpected row: %+v", got)
	}

	got.Name = "tg2"
	got.Enabled = false
	if id2, err := s.SaveNotifyService(ctx, got); err != nil || id2 != id {
		t.Fatalf("update: id=%d err=%v", id2, err)
	}
	got, _, _ = s.GetNotifyService(ctx, id)
	if got.Name != "tg2" || got.Enabled {
		t.Fatalf("update not applied: %+v", got)
	}

	at := time.Now().UTC().Truncate(time.Second)
	if err := s.RecordNotifySendResult(ctx, id, errors.New("boom"), at); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetNotifyService(ctx, id)
	if got.LastError != "boom" || got.LastErrorAt == nil {
		t.Fatalf("error not recorded: %+v", got)
	}
	if err := s.RecordNotifySendResult(ctx, id, nil, at); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotifySummarySent(ctx, id, at); err != nil {
		t.Fatal(err)
	}
	got, _, _ = s.GetNotifyService(ctx, id)
	if got.LastError != "" || got.LastSuccessAt == nil || got.LastSummaryAt == nil || !got.LastSummaryAt.Equal(at) {
		t.Fatalf("success/summary not recorded: %+v", got)
	}

	list, err := s.ListNotifyServices(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}

	if err := s.UpsertNotifyPending(ctx, NotifyPendingRow{ServiceID: id, DedupKey: "k", Kind: "update", ContainerName: "c", Payload: "{}"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteNotifyService(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.GetNotifyService(ctx, id); ok {
		t.Fatal("service still present")
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM notify_pending`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("pending not cascaded: n=%d err=%v", n, err)
	}
}

func TestNotifyPending_UpsertReplacesByDedupKey(t *testing.T) {
	s, _ := newNotifyTestStorage(t)
	ctx := context.Background()
	id, err := s.SaveNotifyService(ctx, NotifyServiceRow{Name: "a", Type: "x", Delivery: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	base := time.Now().UTC().Add(-time.Hour)
	for i, p := range []string{`{"v":1}`, `{"v":2}`} {
		row := NotifyPendingRow{ServiceID: id, DedupKey: "c:1.0", Kind: []string{"a", "b"}[i],
			ContainerName: "c", Payload: p, CreatedAt: base.Add(time.Duration(i) * time.Minute)}
		if err := s.UpsertNotifyPending(ctx, row); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.ListNotifyPending(ctx, id)
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if rows[0].Payload != `{"v":2}` || rows[0].Kind != "b" || !rows[0].CreatedAt.After(base.Add(30*time.Second)) {
		t.Fatalf("not replaced: %+v", rows[0])
	}

	if err := s.DeleteNotifyPendingForContainer(ctx, "c"); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListNotifyPending(ctx, id); len(rows) != 0 {
		t.Fatalf("expected empty, got %v", rows)
	}
	_ = s.UpsertNotifyPending(ctx, NotifyPendingRow{ServiceID: id, DedupKey: "z", Kind: "k", Payload: "{}"})
	rows, _ = s.ListNotifyPending(ctx, id)
	if err := s.DeleteNotifyPending(ctx, []int64{rows[0].ID}); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ListNotifyPending(ctx, id); len(rows) != 0 {
		t.Fatalf("expected empty, got %v", rows)
	}
}

func TestNotifyContainerStates_ReplaceDropsMissing(t *testing.T) {
	s, dbPath := newNotifyTestStorage(t)
	ctx := context.Background()

	err := s.ReplaceNotifyContainerStates(ctx, map[string]NotifyContainerState{
		"a": {ContainerName: "a", LastNotifiedVersion: "1.0", Blocked: true, CheckFailCount: 3, ComposeMismatch: true},
		"b": {ContainerName: "b", LastNotifiedDigest: "sha256:x", CheckFailNotified: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	s.Close()

	s2, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	got, err := s2.LoadNotifyContainerStates(ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("got=%v err=%v", got, err)
	}
	if a := got["a"]; a.LastNotifiedVersion != "1.0" || !a.Blocked || a.CheckFailCount != 3 || !a.ComposeMismatch || a.CheckFailNotified {
		t.Fatalf("a wrong: %+v", a)
	}
	if b := got["b"]; b.LastNotifiedDigest != "sha256:x" || !b.CheckFailNotified || b.Blocked {
		t.Fatalf("b wrong: %+v", b)
	}

	if err := s2.ReplaceNotifyContainerStates(ctx, map[string]NotifyContainerState{"a": got["a"]}); err != nil {
		t.Fatal(err)
	}
	got, _ = s2.LoadNotifyContainerStates(ctx)
	if _, ok := got["b"]; ok || len(got) != 1 {
		t.Fatalf("b not dropped: %v", got)
	}
}

func TestNotifyPending_SurvivesReopen(t *testing.T) {
	s, dbPath := newNotifyTestStorage(t)
	ctx := context.Background()
	id, err := s.SaveNotifyService(ctx, NotifyServiceRow{Name: "a", Type: "x", Delivery: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"k1", "k2"} {
		if err := s.UpsertNotifyPending(ctx, NotifyPendingRow{ServiceID: id, DedupKey: k, Kind: "update", ContainerName: "c", Payload: `{"k":"` + k + `"}`}); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	s2, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	rows, err := s2.ListNotifyPending(ctx, id)
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if rows[0].DedupKey != "k1" || rows[1].Payload != `{"k":"k2"}` {
		t.Fatalf("unexpected rows: %+v", rows)
	}
}
