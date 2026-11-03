package notify

import (
	"fmt"

	"github.com/chis/docksmith/internal/version"
)

// Level is the significance of a version change.
type Level int

const (
	LevelBuild Level = iota + 1
	LevelPatch
	LevelMinor
	LevelMajor
)

// ParseLevel parses "build", "patch", "minor" or "major".
func ParseLevel(s string) (Level, error) {
	switch s {
	case "build":
		return LevelBuild, nil
	case "patch":
		return LevelPatch, nil
	case "minor":
		return LevelMinor, nil
	case "major":
		return LevelMajor, nil
	}
	return 0, fmt.Errorf("invalid level %q", s)
}

func (l Level) String() string {
	switch l {
	case LevelBuild:
		return "build"
	case LevelPatch:
		return "patch"
	case LevelMinor:
		return "minor"
	case LevelMajor:
		return "major"
	}
	return "unknown"
}

// Classify returns the level of the change from current to latest.
// ok is false for unparseable tags, equal versions, downgrades or unknown changes.
func Classify(current, latest string) (Level, bool) {
	p := version.NewParser()
	from, to := p.ParseTag(current), p.ParseTag(latest)
	if from == nil || to == nil {
		return 0, false
	}
	switch version.NewComparator().GetChangeType(from, to) {
	case version.BuildChange:
		return LevelBuild, true
	case version.PatchChange:
		return LevelPatch, true
	case version.MinorChange:
		return LevelMinor, true
	case version.MajorChange:
		return LevelMajor, true
	}
	return 0, false
}
