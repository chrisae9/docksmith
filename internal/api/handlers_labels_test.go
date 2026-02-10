package api

import (
	"testing"

	"github.com/chis/docksmith/internal/scripts"
)

// TestSetRollbackLabelCompleteness verifies that setRollbackLabel handles all known docksmith labels.
// If a new label is added to docksmithLabels but not to setRollbackLabel, rollback will silently
// skip that label. This test catches that.
func TestSetRollbackLabelCompleteness(t *testing.T) {
	// Map of label key -> expected field to be non-nil after setRollbackLabel
	labelTests := []struct {
		labelKey string
		value    string
		check    func(req *SetLabelsRequest) bool
	}{
		{scripts.IgnoreLabel, "true", func(r *SetLabelsRequest) bool { return r.Ignore != nil }},
		{scripts.AllowLatestLabel, "true", func(r *SetLabelsRequest) bool { return r.AllowLatest != nil }},
		{scripts.AllowPrereleaseLabel, "true", func(r *SetLabelsRequest) bool { return r.AllowPrerelease != nil }},
		{scripts.VersionPinMajorLabel, "true", func(r *SetLabelsRequest) bool { return r.VersionPinMajor != nil }},
		{scripts.VersionPinMinorLabel, "true", func(r *SetLabelsRequest) bool { return r.VersionPinMinor != nil }},
		{scripts.VersionPinPatchLabel, "true", func(r *SetLabelsRequest) bool { return r.VersionPinPatch != nil }},
		{scripts.TagRegexLabel, "^v[0-9]", func(r *SetLabelsRequest) bool { return r.TagRegex != nil }},
		{scripts.VersionMinLabel, "1.0", func(r *SetLabelsRequest) bool { return r.VersionMin != nil }},
		{scripts.VersionMaxLabel, "9.0", func(r *SetLabelsRequest) bool { return r.VersionMax != nil }},
		{scripts.PreUpdateCheckLabel, "/path/to/script", func(r *SetLabelsRequest) bool { return r.Script != nil }},
		{scripts.RestartAfterLabel, "some-container", func(r *SetLabelsRequest) bool { return r.RestartAfter != nil }},
	}

	s := &Server{}

	for _, tt := range labelTests {
		t.Run(tt.labelKey, func(t *testing.T) {
			req := &SetLabelsRequest{}
			s.setRollbackLabel(req, tt.labelKey, tt.value)

			if !tt.check(req) {
				t.Errorf("setRollbackLabel(%q, %q) did not set the expected field", tt.labelKey, tt.value)
			}
		})
	}

	// Verify all docksmithLabels are covered
	coveredLabels := make(map[string]bool)
	for _, tt := range labelTests {
		coveredLabels[tt.labelKey] = true
	}

	for _, label := range docksmithLabels {
		if !coveredLabels[label] {
			t.Errorf("Label %q is in docksmithLabels but not tested in setRollbackLabel completeness test", label)
		}
	}
}
