package notify

import (
	"fmt"
	"time"

	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/update"
	"github.com/chis/docksmith/internal/version"
)

const (
	statusUpdateAvailable        = string(update.UpdateAvailable)
	statusUpdateAvailableBlocked = string(update.UpdateAvailableBlocked)
	statusCheckFailed            = string(update.CheckFailed)
	statusMetadataUnavailable    = string(update.MetadataUnavailable)
	statusComposeMismatch        = string(update.ComposeMismatch)
)

// checkFailThreshold is the number of consecutive failed checks before notifying.
const checkFailThreshold = 3

// absentPruneAfter is how long a container may be missing from check results
// before its dedup state is dropped. Shorter absences (recreate mid-check,
// compose down/up) keep the state so the container is not re-announced.
const absentPruneAfter = 24 * time.Hour

// Snapshot is the part of a check result that Detect needs.
type Snapshot struct {
	Name, Stack, Status, CurrentVersion, LatestVersion, LatestResolvedVersion, CurrentDigest, LatestDigest string
	Labels                                                                                                 map[string]string
}

// Muted reports whether notifications are disabled for the container labels.
func Muted(labels map[string]string) bool {
	return labels[scripts.NotifyLabel] == "false"
}

// Detect diffs check results against prev and returns the events to emit plus the new state.
// It performs no I/O and does not modify prev. Muted containers are tracked but emit nothing.
// Containers in prev but not in snaps keep their state unchanged until they have
// been absent for absentPruneAfter, then are dropped from next.
func Detect(prev map[string]storage.NotifyContainerState, snaps []Snapshot, s Settings, now time.Time) (events []Event, next map[string]storage.NotifyContainerState) {
	next = make(map[string]storage.NotifyContainerState, len(snaps))
	for _, sn := range snaps {
		st := prev[sn.Name]
		st.ContainerName = sn.Name
		st.LastSeen = now
		muted := Muted(sn.Labels)
		emit := func(e Event) {
			if muted {
				return
			}
			e.Container, e.Stack, e.At = sn.Name, sn.Stack, now
			events = append(events, e)
		}
		blockedStatus := sn.Status == statusUpdateAvailableBlocked

		target := sn.LatestResolvedVersion
		if target == "" {
			target = sn.LatestVersion
		}

		if sn.Status == statusUpdateAvailable || blockedStatus {
			if lvl, ok := Classify(sn.CurrentVersion, target); ok {
				minLvl := effectiveMinLevel(sn.Labels, s)
				if lvl >= minLvl && target != st.LastNotifiedVersion && newerThanNotified(st.LastNotifiedVersion, target, minLvl) {
					emit(Event{Kind: EventNewVersion, FromVersion: sn.CurrentVersion, ToVersion: target, Level: lvl})
					st.LastNotifiedVersion = target
				}
			} else if comparisonUnknown(sn.CurrentVersion, target) && digestEnabled(sn.Labels, s) && sn.LatestDigest != "" &&
				sn.LatestDigest != sn.CurrentDigest && sn.LatestDigest != st.LastNotifiedDigest {
				emit(Event{Kind: EventDigestChanged, FromVersion: sn.CurrentVersion, ToVersion: target})
				st.LastNotifiedDigest = sn.LatestDigest
			}
		}

		switch {
		case blockedStatus:
			// A blocked row without a version predates blocked_version (or had no
			// target): adopt the current target silently rather than re-announce.
			legacy := st.Blocked && st.BlockedVersion == ""
			if !legacy && (!st.Blocked || target != st.BlockedVersion) {
				emit(Event{Kind: EventUpdateBlocked, FromVersion: sn.CurrentVersion, ToVersion: target, Message: "Update blocked by pre-update check"})
			}
			st.Blocked, st.BlockedVersion = true, target
		case sn.Status == statusCheckFailed || sn.Status == statusMetadataUnavailable:
			// A transient check failure says nothing about the block; keep it.
		default:
			st.Blocked, st.BlockedVersion = false, ""
		}

		if sn.Status == statusCheckFailed || sn.Status == statusMetadataUnavailable {
			st.CheckFailCount++
			if st.CheckFailCount >= checkFailThreshold && !st.CheckFailNotified {
				emit(Event{Kind: EventCheckFailing, Message: fmt.Sprintf("Version check failing for %d consecutive checks", st.CheckFailCount)})
				st.CheckFailNotified = true
			}
		} else {
			st.CheckFailCount, st.CheckFailNotified = 0, false
		}

		mismatch := sn.Status == statusComposeMismatch
		if mismatch && !st.ComposeMismatch {
			emit(Event{Kind: EventComposeMismatch, Message: "Running image does not match compose file"})
		}
		st.ComposeMismatch = mismatch

		next[sn.Name] = st
	}
	for name, st := range prev {
		if _, ok := next[name]; ok {
			continue
		}
		if st.LastSeen.IsZero() {
			st.LastSeen = now // row predates last_seen: start its grace period now
		} else if now.Sub(st.LastSeen) > absentPruneAfter {
			continue
		}
		next[name] = st
	}
	return events, next
}

// newerThanNotified reports whether target is worth a new notification given
// the version already notified: it must be newer than last by at least minLvl,
// or older than last (the earlier notification no longer applies). This stops
// rebuilds (e.g. -ls205 -> -ls206) of an already-announced release from
// re-notifying without letting a pin added after a notification silence later
// releases. An empty or unparseable last version imposes no restriction.
func newerThanNotified(last, target string, minLvl Level) bool {
	p := version.NewParser()
	from := p.ParseTag(last)
	if last == "" || from == nil {
		return true
	}
	if lvl, ok := Classify(last, target); ok {
		return lvl >= minLvl
	}
	// A target older than the last notified version means that notification no
	// longer applies (a pin or version-max was added, or the tag was retracted),
	// so the target is announced afresh rather than suppressed forever.
	to := p.ParseTag(target)
	return to != nil && version.NewComparator().GetChangeType(from, to) == version.Downgrade
}

// comparisonUnknown reports whether current and target cannot be compared as versions
// (empty or unparseable tags). Downgrades and equal versions are known comparisons.
func comparisonUnknown(current, target string) bool {
	p := version.NewParser()
	from, to := p.ParseTag(current), p.ParseTag(target)
	if from == nil || to == nil {
		return true
	}
	return version.NewComparator().GetChangeType(from, to) == version.UnknownChange
}

func effectiveMinLevel(labels map[string]string, s Settings) Level {
	if l, err := ParseLevel(labels[scripts.NotifyLevelLabel]); err == nil {
		return l
	}
	if l, err := ParseLevel(s.MinLevel); err == nil {
		return l
	}
	return LevelPatch
}

func digestEnabled(labels map[string]string, s Settings) bool {
	switch labels[scripts.NotifyDigestLabel] {
	case "true":
		return true
	case "false":
		return false
	}
	return s.Timing[EventDigestChanged] != TimingOff
}
