// Package notify implements notification settings, event classification and scheduling.
package notify

import "time"

// EventKind identifies a type of notification event.
type EventKind string

const (
	EventNewVersion      EventKind = "new_version"
	EventDigestChanged   EventKind = "digest_changed"
	EventUpdateSucceeded EventKind = "update_succeeded"
	EventUpdateFailed    EventKind = "update_failed"
	EventRollback        EventKind = "rollback"
	EventUpdateBlocked   EventKind = "update_blocked"
	EventCheckFailing    EventKind = "check_failing"
	EventComposeMismatch EventKind = "compose_mismatch"
)

// AllEventKinds lists every event kind.
var AllEventKinds = []EventKind{
	EventNewVersion, EventDigestChanged, EventUpdateSucceeded, EventUpdateFailed,
	EventRollback, EventUpdateBlocked, EventCheckFailing, EventComposeMismatch,
}

// Event is a single notifiable occurrence.
type Event struct {
	Kind        EventKind
	Container   string
	Stack       string
	FromVersion string
	ToVersion   string
	Message     string
	OperationID string
	Level       Level
	At          time.Time
}
