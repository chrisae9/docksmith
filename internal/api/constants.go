package api

import "time"

// Timeout constants for API operations
const (
	// HealthCheckPollInterval is how often to check container status during health checks
	HealthCheckPollInterval = 1 * time.Second

	// HealthCheckTimeout is the max time to wait for a container to be healthy/running
	HealthCheckTimeout = 30 * time.Second

	// LabelOperationTimeout is the max time for label set/remove operations
	LabelOperationTimeout = 3 * time.Minute

	// ContainerRestartTimeout is the default timeout for single container restart
	ContainerRestartTimeout = 60 * time.Second

	// StackRestartTimeout is the timeout for stack restart operations
	StackRestartTimeout = 120 * time.Second
)

// Docker Compose label constants
const (
	// ComposeConfigFilesLabel is the Docker label containing compose file paths
	ComposeConfigFilesLabel = "com.docker.compose.project.config_files"

	// ComposeServiceLabel is the Docker label containing the compose service name
	ComposeServiceLabel = "com.docker.compose.service"

	// ComposeProjectLabel is the Docker label containing the compose project name
	ComposeProjectLabel = "com.docker.compose.project"
)

// MaxRegexPatternLength is the maximum allowed length for regex patterns
const MaxRegexPatternLength = 500
