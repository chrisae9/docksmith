package notify

import (
	"reflect"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/storage"
)

func snap(status, cur, latest string) Snapshot {
	return Snapshot{Name: "web", Stack: "s", Status: status, CurrentVersion: cur, LatestVersion: latest}
}

func kinds(evs []Event) []EventKind {
	var k []EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func TestDetect(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := DefaultSettings("h")
	type step struct {
		snaps []Snapshot
		want  []EventKind
	}
	lbl := func(k, v string) map[string]string { return map[string]string{k: v} }
	withLabels := func(sn Snapshot, l map[string]string) Snapshot { sn.Labels = l; return sn }
	latestSnap := func(digest string, l map[string]string) Snapshot {
		return Snapshot{Name: "web", Status: "UPDATE_AVAILABLE", CurrentVersion: "latest", LatestVersion: "latest", CurrentDigest: "sha:a", LatestDigest: digest, Labels: l}
	}
	nv := []EventKind{EventNewVersion}
	tests := []struct {
		name  string
		steps []step
		check func(t *testing.T, st map[string]storage.NotifyContainerState)
	}{
		{"patch once", []step{
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.10")}, nv},
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.10")}, nil},
		}, nil},
		{"supersede", []step{
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.10")}, nv},
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.11")}, nv},
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.11")}, nil},
		}, func(t *testing.T, st map[string]storage.NotifyContainerState) {
			if st["web"].LastNotifiedVersion != "4.0.11" {
				t.Fatalf("got %+v", st["web"])
			}
		}},
		{"build bump below min", []step{
			{[]Snapshot{snap("UPDATE_AVAILABLE", "1.2.3-ls10", "1.2.3-ls11")}, nil},
		}, nil},
		{"build bump label", []step{
			{[]Snapshot{withLabels(snap("UPDATE_AVAILABLE", "1.2.3-ls10", "1.2.3-ls11"), lbl("docksmith.notify-level", "build"))}, nv},
		}, nil},
		{"invalid level label falls back", []step{
			{[]Snapshot{withLabels(snap("UPDATE_AVAILABLE", "1.2.3-ls10", "1.2.3-ls11"), lbl("docksmith.notify-level", "bogus"))}, nil},
		}, nil},
		{"latest resolved", []step{
			{[]Snapshot{{Name: "web", Status: "UPDATE_AVAILABLE", CurrentVersion: "1.2.3", LatestVersion: "latest", LatestResolvedVersion: "1.3.0"}}, nv},
		}, nil},
		{"digest off", []step{{[]Snapshot{latestSnap("sha:b", nil)}, nil}}, nil},
		{"digest on", []step{
			{[]Snapshot{latestSnap("sha:b", lbl("docksmith.notify-digest", "true"))}, []EventKind{EventDigestChanged}},
			{[]Snapshot{latestSnap("sha:b", lbl("docksmith.notify-digest", "true"))}, nil},
		}, nil},
		{"digest not for downgrade", []step{
			{[]Snapshot{{Name: "web", Status: "UPDATE_AVAILABLE", CurrentVersion: "1.3.0", LatestVersion: "latest", LatestResolvedVersion: "1.2.0", CurrentDigest: "sha:a", LatestDigest: "sha:b", Labels: lbl("docksmith.notify-digest", "true")}}, nil},
		}, nil},
		{"digest not for equal resolved", []step{
			{[]Snapshot{{Name: "web", Status: "UPDATE_AVAILABLE", CurrentVersion: "1.3.0", LatestVersion: "latest", LatestResolvedVersion: "1.3.0", CurrentDigest: "sha:a", LatestDigest: "sha:b", Labels: lbl("docksmith.notify-digest", "true")}}, nil},
		}, nil},
		{"digest for unparseable pinned tags", []step{
			{[]Snapshot{{Name: "web", Status: "UPDATE_AVAILABLE", CurrentVersion: "stable", LatestVersion: "edge", CurrentDigest: "sha:a", LatestDigest: "sha:b", Labels: lbl("docksmith.notify-digest", "true")}}, []EventKind{EventDigestChanged}},
		}, nil},
		{"muted still tracks", []step{
			{[]Snapshot{withLabels(snap("UPDATE_AVAILABLE", "4.0.9", "4.0.10"), lbl("docksmith.notify", "false"))}, nil},
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.10")}, nil},
		}, func(t *testing.T, st map[string]storage.NotifyContainerState) {
			if st["web"].LastNotifiedVersion != "4.0.10" {
				t.Fatalf("got %+v", st["web"])
			}
		}},
		{"blocked once", []step{
			{[]Snapshot{snap("UPDATE_AVAILABLE_BLOCKED", "4.0.9", "4.0.10")}, []EventKind{EventNewVersion, EventUpdateBlocked}},
			{[]Snapshot{snap("UPDATE_AVAILABLE_BLOCKED", "4.0.9", "4.0.10")}, nil},
			{[]Snapshot{snap("UPDATE_AVAILABLE_BLOCKED", "4.0.9", "4.0.10")}, nil},
		}, nil},
		{"check failing", []step{
			{[]Snapshot{snap("CHECK_FAILED", "", "")}, nil},
			{[]Snapshot{snap("METADATA_UNAVAILABLE", "", "")}, nil},
			{[]Snapshot{snap("CHECK_FAILED", "", "")}, []EventKind{EventCheckFailing}},
			{[]Snapshot{snap("CHECK_FAILED", "", "")}, nil},
			{[]Snapshot{snap("UP_TO_DATE", "", "")}, nil},
			{[]Snapshot{snap("CHECK_FAILED", "", "")}, nil},
			{[]Snapshot{snap("CHECK_FAILED", "", "")}, nil},
			{[]Snapshot{snap("CHECK_FAILED", "", "")}, []EventKind{EventCheckFailing}},
		}, nil},
		{"compose mismatch transition", []step{
			{[]Snapshot{snap("COMPOSE_MISMATCH", "", "")}, []EventKind{EventComposeMismatch}},
			{[]Snapshot{snap("COMPOSE_MISMATCH", "", "")}, nil},
			{[]Snapshot{snap("UP_TO_DATE", "", "")}, nil},
			{[]Snapshot{snap("COMPOSE_MISMATCH", "", "")}, []EventKind{EventComposeMismatch}},
		}, nil},
		{"missing kept within grace", []step{
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.10")}, nv},
			{nil, nil},
			{[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.9", "4.0.10")}, nil},
		}, func(t *testing.T, st map[string]storage.NotifyContainerState) {
			if st["web"].LastNotifiedVersion != "4.0.10" {
				t.Fatalf("got %+v", st)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prev := map[string]storage.NotifyContainerState{}
			for i, st := range tc.steps {
				before := make(map[string]storage.NotifyContainerState, len(prev))
				for k, v := range prev {
					before[k] = v
				}
				evs, next := Detect(prev, st.snaps, s, now)
				if !reflect.DeepEqual(prev, before) {
					t.Fatal("prev mutated")
				}
				got := kinds(evs)
				if len(got) != len(st.want) {
					t.Fatalf("step %d: got %v want %v", i, got, st.want)
				}
				for j := range got {
					if got[j] != st.want[j] {
						t.Fatalf("step %d: got %v want %v", i, got, st.want)
					}
				}
				prev = next
			}
			if tc.check != nil {
				tc.check(t, prev)
			}
		})
	}
}

func TestDetectDigestSettings(t *testing.T) {
	now := time.Now()
	on := DefaultSettings("h")
	on.Timing[EventDigestChanged] = TimingSummary
	sn := latestSnapForTest(nil)
	if evs, _ := Detect(nil, []Snapshot{sn}, on, now); len(evs) != 1 || evs[0].Kind != EventDigestChanged {
		t.Fatalf("global on: got %v", evs)
	}
	sn = latestSnapForTest(map[string]string{"docksmith.notify-digest": "false"})
	if evs, _ := Detect(nil, []Snapshot{sn}, on, now); len(evs) != 0 {
		t.Fatalf("label off: got %v", evs)
	}
}

func latestSnapForTest(l map[string]string) Snapshot {
	return Snapshot{Name: "web", Status: "UPDATE_AVAILABLE", CurrentVersion: "latest", LatestVersion: "latest", CurrentDigest: "sha:a", LatestDigest: "sha:b", Labels: l}
}

func TestDetectBlockedUsesResolvedTarget(t *testing.T) {
	evs, _ := Detect(nil, []Snapshot{{Name: "web", Status: "UPDATE_AVAILABLE_BLOCKED", CurrentVersion: "1.2.3", LatestVersion: "latest", LatestResolvedVersion: "1.3.0"}}, DefaultSettings("h"), time.Now())
	for _, e := range evs {
		if e.Kind == EventUpdateBlocked && e.ToVersion != "1.3.0" {
			t.Fatalf("got %+v", e)
		}
	}
	if len(evs) != 2 {
		t.Fatalf("got %v", evs)
	}
}

func TestDetectStateTransitions(t *testing.T) {
	now := time.Now()
	s := DefaultSettings("h")
	run := func(prev map[string]storage.NotifyContainerState, status string) map[string]storage.NotifyContainerState {
		_, next := Detect(prev, []Snapshot{snap(status, "", "")}, s, now)
		return next
	}
	st := run(nil, "CHECK_FAILED")
	st = run(st, "CHECK_FAILED")
	if st["web"].CheckFailCount != 2 || st["web"].CheckFailNotified {
		t.Fatalf("%+v", st["web"])
	}
	st = run(st, "CHECK_FAILED")
	if st["web"].CheckFailCount != 3 || !st["web"].CheckFailNotified {
		t.Fatalf("%+v", st["web"])
	}
	st = run(st, "UP_TO_DATE")
	if st["web"].CheckFailCount != 0 || st["web"].CheckFailNotified {
		t.Fatalf("%+v", st["web"])
	}
	st = run(st, "COMPOSE_MISMATCH")
	if !st["web"].ComposeMismatch {
		t.Fatalf("%+v", st["web"])
	}
	st = run(st, "UP_TO_DATE")
	if st["web"].ComposeMismatch {
		t.Fatalf("%+v", st["web"])
	}
	st = run(st, "UPDATE_AVAILABLE_BLOCKED")
	if !st["web"].Blocked {
		t.Fatalf("%+v", st["web"])
	}
	st = run(st, "UP_TO_DATE")
	if st["web"].Blocked {
		t.Fatalf("%+v", st["web"])
	}
}

func TestDetectEventFields(t *testing.T) {
	now := time.Now()
	evs, _ := Detect(nil, []Snapshot{{Name: "web", Stack: "s", Status: "UPDATE_AVAILABLE", CurrentVersion: "1.2.3", LatestVersion: "latest", LatestResolvedVersion: "1.3.0"}}, DefaultSettings("h"), now)
	if len(evs) != 1 {
		t.Fatalf("got %v", evs)
	}
	e := evs[0]
	if e.Level != LevelMinor || e.ToVersion != "1.3.0" || e.FromVersion != "1.2.3" || e.Stack != "s" || e.Container != "web" || !e.At.Equal(now) {
		t.Fatalf("bad event %+v", e)
	}
}

func TestMuted(t *testing.T) {
	if !Muted(map[string]string{"docksmith.notify": "false"}) || Muted(nil) || Muted(map[string]string{"docksmith.notify": "true"}) {
		t.Fatal("Muted wrong")
	}
}

// runSteps feeds each snapshot list through Detect in turn and returns the
// event kinds per step plus the final state.
func runSteps(t *testing.T, s Settings, now time.Time, steps ...[]Snapshot) ([][]EventKind, map[string]storage.NotifyContainerState) {
	t.Helper()
	var prev map[string]storage.NotifyContainerState
	var out [][]EventKind
	for _, sn := range steps {
		evs, next := Detect(prev, sn, s, now)
		out = append(out, kinds(evs))
		prev = next
	}
	return out, prev
}

func TestDetectNoRebuildSpam(t *testing.T) {
	now := time.Now()
	s := DefaultSettings("h") // min level patch
	base := func(target string, l map[string]string) []Snapshot {
		sn := snap("UPDATE_AVAILABLE", "4.0.1-ls200", target)
		sn.Labels = l
		return []Snapshot{sn}
	}

	got, st := runSteps(t, s, now, base("4.0.2-ls205", nil), base("4.0.2-ls206", nil), base("4.0.2-ls207", nil))
	if len(got[0]) != 1 || len(got[1]) != 0 || len(got[2]) != 0 {
		t.Fatalf("min=patch: got %v, want one event then none", got)
	}
	if st["web"].LastNotifiedVersion != "4.0.2-ls205" {
		t.Fatalf("last notified = %q", st["web"].LastNotifiedVersion)
	}

	build := map[string]string{"docksmith.notify-level": "build"}
	got, _ = runSteps(t, s, now, base("4.0.2-ls205", build), base("4.0.2-ls206", build))
	if len(got[0]) != 1 || len(got[1]) != 1 {
		t.Fatalf("level=build: got %v, want an event each step", got)
	}

	got, _ = runSteps(t, s, now, base("4.0.2-ls205", nil), base("4.0.3-ls210", nil))
	if len(got[1]) != 1 {
		t.Fatalf("patch bump after notified build: got %v", got)
	}
}

// A target older than the last notified version (registry moved back, or a pin
// added after the notification) is announced once, then deduplicated as usual.
func TestDetectRegistryMovesBackwards(t *testing.T) {
	now := time.Now()
	got, st := runSteps(t, DefaultSettings("h"), now,
		[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.1", "4.0.3")},
		[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.1", "4.0.2")},
		[]Snapshot{snap("UPDATE_AVAILABLE", "4.0.1", "4.0.2")},
	)
	if len(got[0]) != 1 || len(got[1]) != 1 || len(got[2]) != 0 {
		t.Fatalf("got %v", got)
	}
	if st["web"].LastNotifiedVersion != "4.0.2" {
		t.Fatalf("last notified = %q", st["web"].LastNotifiedVersion)
	}
}

// Told about 5.0.0, then pinned to 4.x: later 4.x releases must still notify.
func TestDetectPinAfterNotificationDoesNotSilence(t *testing.T) {
	now := time.Now()
	got, _ := runSteps(t, DefaultSettings("h"), now,
		[]Snapshot{snap("UPDATE_AVAILABLE", "4.3.0", "5.0.0")},
		[]Snapshot{snap("UPDATE_AVAILABLE", "4.3.0", "4.3.1")},
		[]Snapshot{snap("UPDATE_AVAILABLE", "4.3.0", "4.3.1")},
		[]Snapshot{snap("UPDATE_AVAILABLE", "4.3.0", "4.3.2")},
	)
	want := []int{1, 1, 0, 1}
	for i, n := range want {
		if countKind(got[i], EventNewVersion) != n {
			t.Fatalf("step %d: got %v, want %v new_version counts", i, got, want)
		}
	}
}

func TestDetectBlockedPerTarget(t *testing.T) {
	now := time.Now()
	s := DefaultSettings("h")
	blocked := func(target string) []Snapshot { return []Snapshot{snap("UPDATE_AVAILABLE_BLOCKED", "4.0.1", target)} }
	got, st := runSteps(t, s, now,
		blocked("4.0.2"),
		[]Snapshot{snap("CHECK_FAILED", "4.0.1", "")},
		blocked("4.0.2"),
		[]Snapshot{snap("METADATA_UNAVAILABLE", "4.0.1", "")},
		blocked("4.0.2"),
	)
	for i := 1; i < len(got); i++ {
		for _, k := range got[i] {
			if k == EventUpdateBlocked {
				t.Fatalf("step %d re-sent update_blocked after a transient failure: %v", i, got)
			}
		}
	}
	if !st["web"].Blocked || st["web"].BlockedVersion != "4.0.2" {
		t.Fatalf("state %+v", st["web"])
	}

	got, _ = runSteps(t, s, now, blocked("4.0.2"), blocked("4.0.3"))
	if n := countKind(got[1], EventUpdateBlocked); n != 1 {
		t.Fatalf("new target while blocked: got %v", got)
	}

	got, st = runSteps(t, s, now, blocked("4.0.2"), []Snapshot{snap("UP_TO_DATE", "4.0.2", "")}, blocked("4.0.2"))
	if n := countKind(got[2], EventUpdateBlocked); n != 1 {
		t.Fatalf("blocked again after leaving BLOCKED: got %v", got)
	}
	if st["web"].BlockedVersion != "4.0.2" {
		t.Fatalf("state %+v", st["web"])
	}

	// State written before blocked_version existed: blocked with no version must not re-announce.
	legacy := map[string]storage.NotifyContainerState{"web": {ContainerName: "web", Blocked: true, LastNotifiedVersion: "4.0.2", LastSeen: now}}
	evs, next := Detect(legacy, blocked("4.0.2"), s, now)
	if len(evs) != 0 || next["web"].BlockedVersion != "4.0.2" {
		t.Fatalf("legacy blocked: evs=%v state=%+v", evs, next["web"])
	}
}

func countKind(ks []EventKind, k EventKind) int {
	n := 0
	for _, x := range ks {
		if x == k {
			n++
		}
	}
	return n
}

func TestDetectAbsentContainersKeptFor24h(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := DefaultSettings("h")
	web := []Snapshot{snap("UPDATE_AVAILABLE", "4.0.1", "4.0.2")}
	other := []Snapshot{{Name: "db", Status: "UP_TO_DATE"}}

	evs, st := Detect(nil, web, s, t0)
	if len(evs) != 1 || !st["web"].LastSeen.Equal(t0) {
		t.Fatalf("evs=%v state=%+v", evs, st["web"])
	}
	_, st2 := Detect(st, other, s, t0.Add(time.Hour))
	if !reflect.DeepEqual(st2["web"], st["web"]) {
		t.Fatalf("absent container state changed: %+v -> %+v", st["web"], st2["web"])
	}
	evs, st3 := Detect(st2, web, s, t0.Add(2*time.Hour))
	if len(evs) != 0 {
		t.Fatalf("returning container re-notified: %v", evs)
	}

	_, st4 := Detect(st3, other, s, t0.Add(2*time.Hour+24*time.Hour+time.Second))
	if _, ok := st4["web"]; ok {
		t.Fatalf("container absent >24h not pruned: %+v", st4)
	}
	if _, ok := st4["db"]; !ok {
		t.Fatal("present container dropped")
	}

	// Rows without last_seen (pre-migration) get a fresh grace period.
	legacy := map[string]storage.NotifyContainerState{"old": {ContainerName: "old", LastNotifiedVersion: "1.0"}}
	_, st5 := Detect(legacy, other, s, t0)
	if o, ok := st5["old"]; !ok || !o.LastSeen.Equal(t0) || o.LastNotifiedVersion != "1.0" {
		t.Fatalf("legacy absent row: %+v", st5)
	}
}
