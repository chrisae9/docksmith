package storage

import (
	"context"
	"time"
)

// Storage defines the interface for persistent storage operations.
// Implementations must handle graceful degradation when operations fail.
type Storage interface {
	// SaveVersionCache stores a SHA-to-version resolution in the cache.
	// Parameters:
	//   - sha256: The container image digest
	//   - imageRef: The image reference (registry/repository)
	//   - version: The resolved semantic version
	//   - arch: The architecture (e.g., "amd64", "arm64")
	SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error

	// GetVersionCache retrieves a cached version resolution.
	// Returns:
	//   - version: The cached version string
	//   - found: True if a valid, non-expired cache entry exists
	//   - err: Any error that occurred during lookup
	GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (version string, found bool, err error)

	// LogCheck records a check operation in the history.
	// Parameters:
	//   - containerName: Name of the container checked
	//   - image: Full image reference
	//   - currentVer: Current version (may be empty/unknown)
	//   - latestVer: Latest available version
	//   - status: Check status (up_to_date, update_available, unknown, failed, local_image)
	//   - checkErr: Error from the check operation (nil if successful)
	LogCheck(ctx context.Context, containerName, image, currentVer, latestVer, status string, checkErr error) error

	// LogCheckBatch records multiple check operations atomically.
	// Uses a transaction to ensure all entries are saved or none are.
	// Parameters:
	//   - checks: Slice of check history entries to log
	LogCheckBatch(ctx context.Context, checks []CheckHistoryEntry) error

	// GetCheckHistory retrieves check history for a specific container.
	// Returns entries ordered by check_time DESC (most recent first).
	// Parameters:
	//   - containerName: Name of the container to query
	//   - limit: Maximum number of entries to return
	GetCheckHistory(ctx context.Context, containerName string, limit int) ([]CheckHistoryEntry, error)

	// GetAllCheckHistory retrieves check history for all containers.
	// Returns entries ordered by check_time DESC (most recent first).
	// Parameters:
	//   - limit: Maximum number of entries to return (0 for no limit)
	GetAllCheckHistory(ctx context.Context, limit int) ([]CheckHistoryEntry, error)

	// GetCheckHistorySince retrieves check history since a specific time.
	// Returns entries ordered by check_time DESC (most recent first).
	// Parameters:
	//   - since: Time to query from (inclusive)
	GetCheckHistorySince(ctx context.Context, since time.Time) ([]CheckHistoryEntry, error)

	// GetCheckHistoryByTimeRange retrieves check history within a time range.
	// Returns entries ordered by check_time DESC (most recent first).
	// Parameters:
	//   - start: Start of time range (inclusive)
	//   - end: End of time range (inclusive)
	GetCheckHistoryByTimeRange(ctx context.Context, start, end time.Time) ([]CheckHistoryEntry, error)

	// LogUpdate records an update operation in the audit log.
	// Parameters:
	//   - containerName: Name of the container being updated
	//   - operation: Type of operation (pull, restart, rollback)
	//   - fromVer: Version before update
	//   - toVer: Version after update
	//   - success: Whether the operation succeeded
	//   - updateErr: Error from the update operation (nil if successful)
	LogUpdate(ctx context.Context, containerName, operation, fromVer, toVer string, success bool, updateErr error) error

	// GetUpdateLog retrieves update log for a specific container.
	// Returns entries ordered by timestamp DESC (most recent first).
	// Parameters:
	//   - containerName: Name of the container to query
	//   - limit: Maximum number of entries to return
	GetUpdateLog(ctx context.Context, containerName string, limit int) ([]UpdateLogEntry, error)

	// GetAllUpdateLog retrieves update log for all containers.
	// Returns entries ordered by timestamp DESC (most recent first).
	// Parameters:
	//   - limit: Maximum number of entries to return (0 for no limit)
	GetAllUpdateLog(ctx context.Context, limit int) ([]UpdateLogEntry, error)

	// GetConfig retrieves a configuration value by key.
	// Returns:
	//   - value: The configuration value
	//   - found: True if the key exists
	//   - err: Any error that occurred during lookup
	GetConfig(ctx context.Context, key string) (value string, found bool, err error)

	// SetConfig stores a configuration value.
	// Updates the updated_at timestamp automatically.
	SetConfig(ctx context.Context, key, value string) error

	// SaveConfigSnapshot saves a complete configuration snapshot for rollback capability.
	// Parameters:
	//   - snapshot: ConfigSnapshot containing timestamp, config data, and changed_by identifier
	SaveConfigSnapshot(ctx context.Context, snapshot ConfigSnapshot) error

	// GetConfigHistory retrieves configuration history ordered by snapshot_time DESC.
	// Parameters:
	//   - limit: Maximum number of snapshots to return
	GetConfigHistory(ctx context.Context, limit int) ([]ConfigSnapshot, error)

	// GetConfigSnapshotByID retrieves a specific configuration snapshot by ID.
	// Returns:
	//   - snapshot: The configuration snapshot
	//   - found: True if the snapshot exists
	//   - err: Any error that occurred during lookup
	GetConfigSnapshotByID(ctx context.Context, snapshotID int64) (ConfigSnapshot, bool, error)

	// RevertToSnapshot atomically restores configuration from a snapshot.
	// Uses a transaction to ensure all-or-nothing semantics.
	// Creates a new snapshot after revert for audit trail.
	// Parameters:
	//   - snapshotID: ID of the snapshot to restore
	RevertToSnapshot(ctx context.Context, snapshotID int64) error

	// SaveUpdateOperation creates or updates an update operation record.
	// Parameters:
	//   - op: UpdateOperation containing operation details and state
	SaveUpdateOperation(ctx context.Context, op UpdateOperation) error

	// GetUpdateOperation retrieves an update operation by operation ID.
	// Returns:
	//   - operation: The update operation record
	//   - found: True if the operation exists
	//   - err: Any error that occurred during lookup
	GetUpdateOperation(ctx context.Context, operationID string) (UpdateOperation, bool, error)

	// GetUpdateOperations retrieves update operations for history display.
	// Returns entries ordered by started_at DESC (most recent first).
	// Only returns completed or failed operations (not queued/in-progress).
	// Parameters:
	//   - limit: Maximum number of entries to return (0 for no limit)
	GetUpdateOperations(ctx context.Context, limit int) ([]UpdateOperation, error)

	// GetUpdateOperationsByContainer retrieves update operations for a specific container.
	// Returns entries ordered by started_at DESC (most recent first).
	// Parameters:
	//   - containerName: Name of the container to query
	//   - limit: Maximum number of entries to return (0 for no limit)
	GetUpdateOperationsByContainer(ctx context.Context, containerName string, limit int) ([]UpdateOperation, error)

	// GetUpdateOperationsByTimeRange retrieves update operations within a time range.
	// Returns entries ordered by started_at DESC (most recent first).
	// Parameters:
	//   - start: Start of time range (inclusive)
	//   - end: End of time range (inclusive)
	GetUpdateOperationsByTimeRange(ctx context.Context, start, end time.Time) ([]UpdateOperation, error)

	// GetUpdateOperationsByStatus retrieves update operations filtered by status.
	// Returns entries ordered by created_at DESC (most recent first).
	// Parameters:
	//   - status: Status to filter by (queued, validating, complete, failed, etc.)
	//   - limit: Maximum number of entries to return (0 for no limit)
	GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]UpdateOperation, error)

	// UpdateOperationStatus updates the status and error message of an operation.
	// Also updates the updated_at timestamp automatically.
	// Parameters:
	//   - operationID: ID of the operation to update
	//   - status: New status value
	//   - errorMsg: Error message (empty string if no error)
	UpdateOperationStatus(ctx context.Context, operationID string, status string, errorMsg string) error

	// SaveComposeBackup records metadata about a compose file backup.
	// Links backup to an update operation for rollback capability.
	// Parameters:
	//   - backup: ComposeBackup containing backup metadata
	SaveComposeBackup(ctx context.Context, backup ComposeBackup) error

	// GetComposeBackup retrieves compose backup metadata by operation ID.
	// Returns:
	//   - backup: The compose backup record
	//   - found: True if the backup exists
	//   - err: Any error that occurred during lookup
	GetComposeBackup(ctx context.Context, operationID string) (ComposeBackup, bool, error)

	// GetComposeBackupsByContainer retrieves all backups for a specific container.
	// Returns entries ordered by backup_timestamp DESC (most recent first).
	// Parameters:
	//   - containerName: Name of the container to query
	GetComposeBackupsByContainer(ctx context.Context, containerName string) ([]ComposeBackup, error)

	// GetAllComposeBackups retrieves all compose backups.
	// Returns entries ordered by backup_timestamp DESC (most recent first).
	// Parameters:
	//   - limit: Maximum number of entries to return (0 for no limit)
	GetAllComposeBackups(ctx context.Context, limit int) ([]ComposeBackup, error)

	// GetRollbackPolicy retrieves the rollback policy for an entity.
	// Parameters:
	//   - entityType: Type of entity (global, container, stack)
	//   - entityID: ID of entity (container/stack name, empty for global)
	// Returns:
	//   - policy: The rollback policy record
	//   - found: True if a policy exists for this entity
	//   - err: Any error that occurred during lookup
	GetRollbackPolicy(ctx context.Context, entityType, entityID string) (RollbackPolicy, bool, error)

	// SetRollbackPolicy creates or updates a rollback policy.
	// Uses INSERT OR REPLACE to handle both create and update cases.
	// Parameters:
	//   - policy: RollbackPolicy containing policy configuration
	SetRollbackPolicy(ctx context.Context, policy RollbackPolicy) error

	// QueueUpdate adds an update operation to the queue.
	// Used when a stack is locked and operation must wait.
	// Parameters:
	//   - queue: UpdateQueue containing queue entry details
	QueueUpdate(ctx context.Context, queue UpdateQueue) error

	// DequeueUpdate retrieves and removes the oldest queued operation for a stack.
	// Returns:
	//   - queue: The dequeued update operation
	//   - found: True if an operation was found and dequeued
	//   - err: Any error that occurred during dequeue
	DequeueUpdate(ctx context.Context, stackName string) (UpdateQueue, bool, error)

	// GetQueuedUpdates retrieves all queued operations ordered by queued_at.
	// Returns entries in FIFO order (oldest first).
	GetQueuedUpdates(ctx context.Context) ([]UpdateQueue, error)

	// Close closes the database connection and releases resources.
	// Should be called when the storage is no longer needed.
	Close() error
}

// CheckHistoryEntry represents a single check operation result.
type CheckHistoryEntry struct {
	ID             int64     `json:"id"`
	ContainerName  string    `json:"container_name"`
	Image          string    `json:"image"`
	CheckTime      time.Time `json:"check_time"`
	CurrentVersion string    `json:"current_version,omitempty"`
	LatestVersion  string    `json:"latest_version,omitempty"`
	Status         string    `json:"status"`
	Error          string    `json:"error,omitempty"`
}

// UpdateLogEntry represents a single update operation result.
type UpdateLogEntry struct {
	ID            int64     `json:"id"`
	ContainerName string    `json:"container_name"`
	Operation     string    `json:"operation"`
	FromVersion   string    `json:"from_version,omitempty"`
	ToVersion     string    `json:"to_version,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
	Success       bool      `json:"success"`
	Error         string    `json:"error,omitempty"`
}

// ConfigSnapshot represents a complete configuration state at a point in time.
// Used for configuration rollback and audit trail.
type ConfigSnapshot struct {
	ID           int64             `json:"id"`
	SnapshotTime time.Time         `json:"snapshot_time"`
	ConfigData   map[string]string `json:"config_data"`
	ChangedBy    string            `json:"changed_by"`
	CreatedAt    time.Time         `json:"created_at"`
}

// UpdateOperation represents a container update operation with full state tracking.
// Tracks progress through all stages of the update workflow.
type UpdateOperation struct {
	ID                 int64      `json:"id"`
	OperationID        string     `json:"operation_id"`
	ContainerID        string     `json:"container_id"`
	ContainerName      string     `json:"container_name"`
	StackName          string     `json:"stack_name,omitempty"`
	OperationType      string     `json:"operation_type"` // single, batch, stack
	Status             string     `json:"status"`         // queued, validating, backup, updating_compose, pulling_image, stopping, starting, health_check, restarting_dependents, complete, failed, rolling_back, cancelled
	OldVersion         string     `json:"old_version,omitempty"`
	NewVersion         string     `json:"new_version"`
	StartedAt          *time.Time `json:"started_at,omitempty"`
	CompletedAt        *time.Time `json:"completed_at,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	DependentsAffected []string   `json:"dependents_affected,omitempty"` // JSON array of container names
	RollbackOccurred   bool       `json:"rollback_occurred"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// ComposeBackup represents metadata for a compose file backup.
// Links to an update operation for rollback capability.
type ComposeBackup struct {
	ID              int64     `json:"id"`
	OperationID     string    `json:"operation_id"`
	ContainerName   string    `json:"container_name"`
	StackName       string    `json:"stack_name,omitempty"`
	ComposeFilePath string    `json:"compose_file_path"`
	BackupFilePath  string    `json:"backup_file_path"`
	BackupTimestamp time.Time `json:"backup_timestamp"`
	CreatedAt       time.Time `json:"created_at"`
}

// RollbackPolicy represents auto-rollback configuration at various levels.
// Supports hierarchical policy resolution: container > stack > global.
type RollbackPolicy struct {
	ID                   int64     `json:"id"`
	EntityType           string    `json:"entity_type"`            // global, container, stack
	EntityID             string    `json:"entity_id,omitempty"`    // container or stack name, empty for global
	AutoRollbackEnabled  bool      `json:"auto_rollback_enabled"`
	HealthCheckRequired  bool      `json:"health_check_required"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// UpdateQueue represents a queued update operation waiting for stack lock.
// Implements FIFO queue with persistence across restarts.
type UpdateQueue struct {
	ID                 int64      `json:"id"`
	OperationID        string     `json:"operation_id"`
	StackName          string     `json:"stack_name"`
	Containers         []string   `json:"containers"` // JSON array of container names
	Priority           int        `json:"priority"`
	QueuedAt           time.Time  `json:"queued_at"`
	EstimatedStartTime *time.Time `json:"estimated_start_time,omitempty"`
}
