package version

import "testing"

func TestCompare(t *testing.T) {
	comparator := NewComparator()

	tests := []struct {
		name     string
		v1       *Version
		v2       *Version
		expected int
	}{
		{
			name:     "equal versions",
			v1:       &Version{Major: 1, Minor: 2, Patch: 3},
			v2:       &Version{Major: 1, Minor: 2, Patch: 3},
			expected: 0,
		},
		{
			name:     "v1 < v2 (major)",
			v1:       &Version{Major: 1, Minor: 2, Patch: 3},
			v2:       &Version{Major: 2, Minor: 0, Patch: 0},
			expected: -1,
		},
		{
			name:     "v1 > v2 (major)",
			v1:       &Version{Major: 2, Minor: 0, Patch: 0},
			v2:       &Version{Major: 1, Minor: 9, Patch: 9},
			expected: 1,
		},
		{
			name:     "v1 < v2 (minor)",
			v1:       &Version{Major: 1, Minor: 2, Patch: 3},
			v2:       &Version{Major: 1, Minor: 3, Patch: 0},
			expected: -1,
		},
		{
			name:     "v1 < v2 (patch)",
			v1:       &Version{Major: 1, Minor: 2, Patch: 3},
			v2:       &Version{Major: 1, Minor: 2, Patch: 4},
			expected: -1,
		},
		{
			name:     "prerelease < release",
			v1:       &Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha"},
			v2:       &Version{Major: 1, Minor: 0, Patch: 0},
			expected: -1,
		},
		{
			name:     "release > prerelease",
			v1:       &Version{Major: 1, Minor: 0, Patch: 0},
			v2:       &Version{Major: 1, Minor: 0, Patch: 0, Prerelease: "alpha"},
			expected: 1,
		},
		{
			name:     "fully-specified tag preferred over shortened (v3.41.0 > v3.41)",
			v1:       &Version{Major: 3, Minor: 41, Patch: 0, Original: "v3.41.0"},
			v2:       &Version{Major: 3, Minor: 41, Patch: 0, Original: "v3.41"},
			expected: 1,
		},
		{
			name:     "shortened tag less than fully-specified (v3.41 < v3.41.0)",
			v1:       &Version{Major: 3, Minor: 41, Patch: 0, Original: "v3.41"},
			v2:       &Version{Major: 3, Minor: 41, Patch: 0, Original: "v3.41.0"},
			expected: -1,
		},
		{
			name:     "major only tag less than major.minor (v3 < v3.0)",
			v1:       &Version{Major: 3, Minor: 0, Patch: 0, Original: "v3"},
			v2:       &Version{Major: 3, Minor: 0, Patch: 0, Original: "v3.0"},
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := comparator.Compare(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("Compare(%v, %v) = %d, want %d", tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}

func TestGetChangeType(t *testing.T) {
	comparator := NewComparator()

	tests := []struct {
		name     string
		from     *Version
		to       *Version
		expected ChangeType
	}{
		{
			name:     "no change",
			from:     &Version{Major: 1, Minor: 2, Patch: 3},
			to:       &Version{Major: 1, Minor: 2, Patch: 3},
			expected: NoChange,
		},
		{
			name:     "patch change",
			from:     &Version{Major: 1, Minor: 2, Patch: 3},
			to:       &Version{Major: 1, Minor: 2, Patch: 4},
			expected: PatchChange,
		},
		{
			name:     "minor change",
			from:     &Version{Major: 1, Minor: 2, Patch: 3},
			to:       &Version{Major: 1, Minor: 3, Patch: 0},
			expected: MinorChange,
		},
		{
			name:     "major change",
			from:     &Version{Major: 1, Minor: 2, Patch: 3},
			to:       &Version{Major: 2, Minor: 0, Patch: 0},
			expected: MajorChange,
		},
		{
			name:     "downgrade",
			from:     &Version{Major: 2, Minor: 0, Patch: 0},
			to:       &Version{Major: 1, Minor: 9, Patch: 9},
			expected: Downgrade,
		},
		{
			name:     "minor change with patch decrease",
			from:     &Version{Major: 1, Minor: 2, Patch: 5},
			to:       &Version{Major: 1, Minor: 3, Patch: 0},
			expected: MinorChange,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := comparator.GetChangeType(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("GetChangeType(%v, %v) = %v, want %v",
					tt.from, tt.to, result, tt.expected)
			}
		})
	}
}

func TestIsNewer(t *testing.T) {
	comparator := NewComparator()

	v1 := &Version{Major: 1, Minor: 2, Patch: 3}
	v2 := &Version{Major: 1, Minor: 2, Patch: 4}
	v3 := &Version{Major: 1, Minor: 2, Patch: 3}

	if !comparator.IsNewer(v1, v2) {
		t.Error("v2 should be newer than v1")
	}

	if comparator.IsNewer(v2, v1) {
		t.Error("v1 should not be newer than v2")
	}

	if comparator.IsNewer(v1, v3) {
		t.Error("v3 should not be newer than v1 (equal)")
	}
}

func TestIsEqual(t *testing.T) {
	comparator := NewComparator()

	v1 := &Version{Major: 1, Minor: 2, Patch: 3}
	v2 := &Version{Major: 1, Minor: 2, Patch: 3}
	v3 := &Version{Major: 1, Minor: 2, Patch: 4}

	if !comparator.IsEqual(v1, v2) {
		t.Error("v1 and v2 should be equal")
	}

	if comparator.IsEqual(v1, v3) {
		t.Error("v1 and v3 should not be equal")
	}
}
