package version

import "strings"

// Comparator compares semantic versions.
type Comparator struct{}

// NewComparator creates a new version comparator.
func NewComparator() *Comparator {
	return &Comparator{}
}

// Compare compares two versions and returns:
//   -1 if v1 < v2
//    0 if v1 == v2
//    1 if v1 > v2
func (c *Comparator) Compare(v1, v2 *Version) int {
	if v1 == nil || v2 == nil {
		if v1 == nil && v2 == nil {
			return 0
		}
		if v1 == nil {
			return -1
		}
		return 1
	}

	// Compare major version
	if v1.Major != v2.Major {
		if v1.Major < v2.Major {
			return -1
		}
		return 1
	}

	// Compare minor version
	if v1.Minor != v2.Minor {
		if v1.Minor < v2.Minor {
			return -1
		}
		return 1
	}

	// Compare patch version
	if v1.Patch != v2.Patch {
		if v1.Patch < v2.Patch {
			return -1
		}
		return 1
	}

	// Compare prerelease
	// Per semver spec: pre-release versions have lower precedence
	// 1.0.0-alpha < 1.0.0
	if v1.Prerelease != v2.Prerelease {
		if v1.Prerelease == "" {
			return 1 // Release is greater than prerelease
		}
		if v2.Prerelease == "" {
			return -1 // Prerelease is less than release
		}
		// Both have prerelease, compare lexically
		return strings.Compare(v1.Prerelease, v2.Prerelease)
	}

	return 0
}

// GetChangeType determines the type of change between two versions.
// from is the current version, to is the new version.
func (c *Comparator) GetChangeType(from, to *Version) ChangeType {
	cmp := c.Compare(from, to)

	if cmp == 0 {
		return NoChange
	}

	if cmp > 0 {
		return Downgrade
	}

	// Version increased (cmp < 0)
	if from.Major != to.Major {
		return MajorChange
	}

	if from.Minor != to.Minor {
		return MinorChange
	}

	return PatchChange
}

// IsNewer returns true if v2 is newer than v1.
func (c *Comparator) IsNewer(v1, v2 *Version) bool {
	return c.Compare(v1, v2) < 0
}

// IsEqual returns true if versions are equal.
func (c *Comparator) IsEqual(v1, v2 *Version) bool {
	return c.Compare(v1, v2) == 0
}
