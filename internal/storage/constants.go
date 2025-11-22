package storage

// Operation status constants
const (
	StatusComplete      = "complete"
	StatusFailed        = "failed"
	StatusQueued        = "queued"
	StatusValidating    = "validating"
	StatusBackup        = "backup"
	StatusPullingImage  = "pulling_image"
	StatusRecreating    = "recreating"
	StatusHealthCheck   = "health_check"
	StatusRollingBack   = "rolling_back"
	StatusInProgress    = "in_progress"
)

// Check status constants
const (
	CheckStatusUpToDate        = "up_to_date"
	CheckStatusUpdateAvailable = "update_available"
	CheckStatusFailed          = "failed"
	CheckStatusLocalImage      = "local_image"
)
