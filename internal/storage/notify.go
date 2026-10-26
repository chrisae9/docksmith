package storage

import (
	"context"
	"time"
)

// Settings JSON lives in the config table under NotifySettingsKey; the seeded
// flag uses NotifySeededKey.
const (
	NotifySettingsKey = "notify_settings"
	NotifySeededKey   = "notify_seeded"
)

// NotifyServiceRow is a configured notification service.
type NotifyServiceRow struct {
	ID                                         int64
	Name, Type, Delivery                       string
	Enabled                                    bool
	Events                                     []string
	Config                                     map[string]string
	LastSuccessAt, LastErrorAt, LastSummaryAt *time.Time
	LastError                                  string
}

// NotifyContainerState is the per-container notification dedup state.
type NotifyContainerState struct {
	ContainerName, LastNotifiedVersion, LastNotifiedDigest string
	Blocked, CheckFailNotified, ComposeMismatch            bool
	CheckFailCount                                         int
}

// NotifyPendingRow is a queued (digest) notification awaiting delivery.
type NotifyPendingRow struct {
	ID, ServiceID                          int64
	DedupKey, Kind, ContainerName, Payload string
	CreatedAt                              time.Time
}

// NotifyStore is the narrow storage interface used by the notification system.
type NotifyStore interface {
	ListNotifyServices(ctx context.Context) ([]NotifyServiceRow, error)
	GetNotifyService(ctx context.Context, id int64) (NotifyServiceRow, bool, error)
	SaveNotifyService(ctx context.Context, row NotifyServiceRow) (int64, error) // insert when ID==0, else update
	DeleteNotifyService(ctx context.Context, id int64) error
	RecordNotifySendResult(ctx context.Context, id int64, sendErr error, at time.Time) error
	SetNotifySummarySent(ctx context.Context, id int64, at time.Time) error
	LoadNotifyContainerStates(ctx context.Context) (map[string]NotifyContainerState, error)
	ReplaceNotifyContainerStates(ctx context.Context, states map[string]NotifyContainerState) error // one transaction; rows not in map are deleted
	UpsertNotifyPending(ctx context.Context, row NotifyPendingRow) error                            // on (service_id, dedup_key) conflict: replace payload, kind and created_at
	ListNotifyPending(ctx context.Context, serviceID int64) ([]NotifyPendingRow, error)
	DeleteNotifyPending(ctx context.Context, ids []int64) error
	DeleteNotifyPendingForContainer(ctx context.Context, containerName string) error
}

var _ NotifyStore = (*SQLiteStorage)(nil)
