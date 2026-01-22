package version

import (
	"fmt"
	"time"
)

// Version represents a semantic version number.
type Version struct {
	Major       int
	Minor       int
	Patch       int
	Prerelease  string     // e.g., "alpha", "beta.1", "rc.2"
	Build       string     // e.g., build metadata
	BuildNumber int        // Numeric build number for comparison (e.g., 285 from "-ls285")
	Original    string     // Original string for reference
	Type        string     // "semantic", "date", "hash"
	Date        *time.Time // For date-based versions
}

// String returns the string representation of the version.
func (v Version) String() string {
	if v.Type == "date" && v.Date != nil {
		return v.Date.Format("2006.01.02")
	}

	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if v.Prerelease != "" {
		s += "-" + v.Prerelease
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// ChangeType represents the type of version change.
type ChangeType int

const (
	// NoChange indicates versions are identical
	NoChange ChangeType = iota
	// PatchChange indicates a patch-level change (0.0.X)
	PatchChange
	// MinorChange indicates a minor-level change (0.X.0)
	MinorChange
	// MajorChange indicates a major-level change (X.0.0)
	MajorChange
	// Downgrade indicates the new version is older
	Downgrade
	// UnknownChange indicates version comparison is not possible
	UnknownChange
)

// String returns the string representation of the change type.
func (ct ChangeType) String() string {
	switch ct {
	case NoChange:
		return "no change"
	case PatchChange:
		return "patch"
	case MinorChange:
		return "minor"
	case MajorChange:
		return "major"
	case Downgrade:
		return "downgrade"
	default:
		return "unknown"
	}
}

// TagInfo represents extracted information from a Docker image tag.
type TagInfo struct {
	// Full is the complete image tag
	Full string

	// Version is the parsed semantic version (if found)
	Version *Version

	// IsLatest indicates if this is a "latest" tag
	IsLatest bool

	// IsVersioned indicates if a semantic version was found
	IsVersioned bool

	// Suffix contains any additional info (e.g., "alpine", "slim")
	Suffix string

	// VersionType indicates the type of version ("semantic", "date", "hash", "meta")
	VersionType string

	// Hash contains the commit hash for hash-based versions
	Hash string

	// MetaTag contains the meta tag name (e.g., "stable", "nightly")
	MetaTag string
}