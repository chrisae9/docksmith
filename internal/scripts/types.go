package scripts

import (
	"os"
	"time"
)

// Script represents metadata about a pre-update check script.
type Script struct {
	// Name is the filename of the script
	Name string `json:"name"`

	// Path is the absolute path to the script
	Path string `json:"path"`

	// RelativePath is the path relative to /scripts folder
	RelativePath string `json:"relative_path"`

	// Executable indicates if the script has execute permissions
	Executable bool `json:"executable"`

	// Size is the file size in bytes
	Size int64 `json:"size"`

	// ModifiedTime is when the script was last modified
	ModifiedTime time.Time `json:"modified_time"`

	// FileInfo is the underlying os.FileInfo
	FileInfo os.FileInfo `json:"-"`
}

// Assignment represents a script assignment to a container.
type Assignment struct {
	// ContainerName is the name of the container
	ContainerName string `json:"container_name"`

	// ScriptPath is the path to the script (relative to /scripts)
	ScriptPath string `json:"script_path"`

	// Enabled indicates if the assignment is active
	Enabled bool `json:"enabled"`

	// Ignore indicates if the container should be ignored from update checks
	Ignore bool `json:"ignore"`

	// AllowLatest indicates if :latest tag is allowed (no migration warning)
	AllowLatest bool `json:"allow_latest"`

	// AssignedAt is when the assignment was created
	AssignedAt time.Time `json:"assigned_at"`

	// AssignedBy indicates who assigned it ('cli' or 'ui')
	AssignedBy string `json:"assigned_by,omitempty"`

	// UpdatedAt is when the assignment was last updated
	UpdatedAt time.Time `json:"updated_at"`
}
