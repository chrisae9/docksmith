package api

import (
	"encoding/json"
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

func TestSetLabels_RemoveLabels(t *testing.T) {
	t.Run("non-docksmith name returns 400", func(t *testing.T) {
		s := &Server{}
		body := strings.NewReader(`{"container":"web","remove_labels":["com.example.other"]}`)
		req := httptest.NewRequest(http.MethodPost, "/api/labels/set", body)
		rec := httptest.NewRecorder()
		s.handleLabelsSet(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("remove_labels alone passes the no-labels guard", func(t *testing.T) {
		s := &Server{}
		body := strings.NewReader(`{"container":"web","remove_labels":["bogus"]}`)
		req := httptest.NewRequest(http.MethodPost, "/api/labels/set", body)
		rec := httptest.NewRecorder()
		s.handleLabelsSet(rec, req)
		if strings.Contains(rec.Body.String(), "no labels specified") {
			t.Fatalf("remove_labels should count as a label change: %s", rec.Body.String())
		}
	})

	t.Run("set and remove apply in one change", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "docker-compose.yml")
		yml := `services:
  web:
    image: nginx:1.25
    container_name: web
    labels:
      docksmith.notify-digest: "true"
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
		modified, err := applyLabelChanges(service, &SetLabelsRequest{
			NotifyLevel:  &level,
			RemoveLabels: []string{scripts.NotifyDigestLabel},
		})
		if err != nil {
			t.Fatal(err)
		}
		if modified[scripts.NotifyLevelLabel] != "minor" {
			t.Errorf("expected notify-level=minor in modified, got %v", modified)
		}
		labels, err := service.GetAllLabels()
		if err != nil {
			t.Fatal(err)
		}
		if labels[scripts.NotifyLevelLabel] != "minor" {
			t.Errorf("notify-level = %q, want minor", labels[scripts.NotifyLevelLabel])
		}
		if _, ok := labels[scripts.NotifyDigestLabel]; ok {
			t.Errorf("notify-digest should have been removed, labels: %v", labels)
		}
	})
}

func TestLabelHistoryJSON_CombinedSetAndRemove(t *testing.T) {
	raw := labelHistoryJSON(
		map[string]string{scripts.NotifyLevelLabel: "minor"},
		[]string{scripts.NotifyDigestLabel},
	)
	var got map[string]string
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("history is not valid JSON: %q: %v", raw, err)
	}
	if got[scripts.NotifyLevelLabel] != "minor" {
		t.Errorf("set label missing from history: %v", got)
	}
	if v, ok := got[scripts.NotifyDigestLabel]; !ok || v != "" {
		t.Errorf("removed label should be recorded with empty value, got %v", got)
	}

	if labelHistoryJSON(nil, nil) != "" {
		t.Error("expected empty history when nothing changed")
	}

	// Rollback restores a removed label from its old value via setRollbackLabel.
	req := &SetLabelsRequest{}
	(&Server{}).setRollbackLabel(req, scripts.NotifyDigestLabel, "true")
	if req.NotifyDigest == nil || !*req.NotifyDigest {
		t.Error("rollback should restore notify-digest=true")
	}
}

func loadWebService(t *testing.T, labels string) *compose.Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-compose.yml")
	yml := "services:\n  web:\n    image: nginx:1.25\n    container_name: web\n" + labels
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
	return service
}

func TestLabelRollback_AddedNotifyLabelsAreRemoved(t *testing.T) {
	s := &Server{}
	// The original op added docksmith.notify=false (mute) and notify-digest=true.
	req, changed := s.buildLabelRollbackRequest("web", false, map[string]string{},
		map[string]string{scripts.NotifyLabel: "false", scripts.NotifyDigestLabel: "true"})
	if !changed {
		t.Fatal("expected changes")
	}
	if req.Notify != nil || req.NotifyDigest != nil {
		t.Fatalf("rollback must not set explicit notify values: notify=%v digest=%v", req.Notify, req.NotifyDigest)
	}

	service := loadWebService(t, "    labels:\n      docksmith.notify: \"false\"\n      docksmith.notify-digest: \"true\"\n")
	if _, err := applyLabelChanges(service, req); err != nil {
		t.Fatal(err)
	}
	labels, err := service.GetAllLabels()
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{scripts.NotifyLabel, scripts.NotifyDigestLabel} {
		if v, ok := labels[k]; ok {
			t.Errorf("label %s = %q after rollback, want removed", k, v)
		}
	}
}

func TestLabelRollback_ChangedNotifyLabelRestoresOldValue(t *testing.T) {
	req, _ := (&Server{}).buildLabelRollbackRequest("web", false,
		map[string]string{scripts.NotifyLabel: "false"}, map[string]string{scripts.NotifyLabel: ""})
	if req.Notify == nil || *req.Notify || len(req.RemoveLabels) != 0 {
		t.Fatalf("removed mute should be restored as notify=false: %+v", req)
	}
}

func TestSetLabels_RejectsSetAndRemoveOverlap(t *testing.T) {
	s := &Server{}
	body := strings.NewReader(`{"container":"web","notify":false,"remove_labels":["docksmith.notify"]}`)
	req := httptest.NewRequest(http.MethodPost, "/api/labels/set", body)
	rec := httptest.NewRecorder()
	s.handleLabelsSet(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "both set and removed") {
		t.Fatalf("expected 400 overlap error, got %d: %s", rec.Code, rec.Body.String())
	}

	level := "minor"
	service := loadWebService(t, "")
	if _, err := applyLabelChanges(service, &SetLabelsRequest{NotifyLevel: &level, RemoveLabels: []string{scripts.NotifyLevelLabel}}); err == nil {
		t.Fatal("applyLabelChanges accepted a label that is both set and removed")
	}
}
