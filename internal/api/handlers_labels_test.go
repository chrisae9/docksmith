package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chis/docksmith/internal/compose"
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
		{scripts.NotifyLabel, "false", func(r *SetLabelsRequest) bool { return r.Notify != nil }},
		{scripts.NotifyLevelLabel, "minor", func(r *SetLabelsRequest) bool { return r.NotifyLevel != nil }},
		{scripts.NotifyDigestLabel, "true", func(r *SetLabelsRequest) bool { return r.NotifyDigest != nil }},
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

func TestSetLabels_NotifyLevelValidation(t *testing.T) {
	s := &Server{}

	t.Run("invalid level returns 400", func(t *testing.T) {
		body := strings.NewReader(`{"container":"web","notify_level":"huge"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/labels/set", body)
		rec := httptest.NewRecorder()
		s.handleLabelsSet(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("valid level is applied", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "docker-compose.yml")
		yml := `services:
  web:
    image: nginx:1.25
    container_name: web
`
		if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
			t.Fatal(err)
		}
		cf, err := compose.LoadComposeFile(path)
		if err != nil {
			t.Fatal(err)
		}
		service, err := cf.FindServiceByContainerName("web")
		if err != nil {
			t.Fatal(err)
		}
		level := "minor"
		off := false
		modified, err := applyLabelChanges(service, &SetLabelsRequest{NotifyLevel: &level, Notify: &off, NotifyDigest: &off})
		if err != nil {
			t.Fatal(err)
		}
		if got := modified[scripts.NotifyLevelLabel]; got != "minor" {
			t.Errorf("expected notify-level=minor in modified labels, got %q", got)
		}
		if got := modified[scripts.NotifyLabel]; got != "false" {
			t.Errorf("expected notify=false in modified labels, got %q", got)
		}
		if got := modified[scripts.NotifyDigestLabel]; got != "false" {
			t.Errorf("expected notify-digest=false in modified labels, got %q", got)
		}
		labels, err := service.GetAllLabels()
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{
			scripts.NotifyLabel:       "false",
			scripts.NotifyDigestLabel: "false",
			scripts.NotifyLevelLabel:  "minor",
		} {
			if labels[key] != want {
				t.Errorf("service label %s = %q, want %q", key, labels[key], want)
			}
		}
	})
}
