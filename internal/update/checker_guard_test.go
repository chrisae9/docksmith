package update

import (
	"context"
	"testing"

	"github.com/chis/docksmith/internal/docker"
)

func TestIsValidPinTarget(t *testing.T) {
	checker := NewChecker(nil, nil, nil)
	tests := []struct {
		name    string
		current string
		target  string
		want    bool
	}{
		{"equal", "6.4.4.10685-ls324", "6.4.4.10685-ls324", true},
		{"newer build", "6.4.4.10685-ls324", "6.4.4.10686-ls324", true},
		{"newer minor", "6.4.4.10685-ls324", "6.5.0.1-ls1", true},
		{"older", "6.4.4.10685-ls324", "3.0.0.4039-ls29", false},
		{"unknown current", "", "3.0.0.4039-ls29", false},
		{"unparsable target", "6.4.4.10685-ls324", "nonsense", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checker.isValidPinTarget(tt.current, tt.target); got != tt.want {
				t.Fatalf("isValidPinTarget(%q, %q) = %v, want %v", tt.current, tt.target, got, tt.want)
			}
		})
	}
}

func TestNoDowngradeRecommendation(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{ID: "c", Name: "radarr", Image: "lscr.io/linuxserver/radarr:latest"},
		},
		imageDigests:  map[string]string{"lscr.io/linuxserver/radarr:latest": "sha256:current"},
		imageVersions: map[string]string{"lscr.io/linuxserver/radarr:latest": "6.4.4.10685-ls324"},
		localImages:   map[string]bool{},
	}
	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"lscr.io/linuxserver/radarr": {
				"amd64-v0.2.0.1504-ls81", "v0.2.0.1504-ls81",
				"amd64-3.0.0.3978-ls27", "version-3.0.0.3978", "3.0.0.4039-ls29",
			},
		},
		tagDigests: map[string]string{
			"lscr.io/linuxserver/radarr:latest": "sha256:current",
		},
		digestMappings: map[string]map[string][]string{
			"lscr.io/linuxserver/radarr": {},
		},
	}
	checker := NewChecker(mockDocker, mockRegistry, newMockStorage())

	result, err := checker.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}
	update := result.Updates[0]

	if update.Status != UpToDate {
		t.Fatalf("status = %s, want UP_TO_DATE", update.Status)
	}
	if update.LatestResolvedVersion != "" {
		t.Fatalf("LatestResolvedVersion = %q, want empty (no older tag may be recommended)", update.LatestResolvedVersion)
	}
	if update.RecommendedTag != "" {
		t.Fatalf("RecommendedTag = %q, want empty", update.RecommendedTag)
	}
}

func TestPinTargetAtRunningVersion(t *testing.T) {
	mockDocker := &mockDockerClient{
		containers: []docker.Container{
			{ID: "c", Name: "radarr", Image: "lscr.io/linuxserver/radarr:latest"},
		},
		imageDigests:  map[string]string{"lscr.io/linuxserver/radarr:latest": "sha256:current"},
		imageVersions: map[string]string{"lscr.io/linuxserver/radarr:latest": "6.4.4.10685-ls324"},
		localImages:   map[string]bool{},
	}
	mockRegistry := &mockRegistryClient{
		tags: map[string][]string{
			"lscr.io/linuxserver/radarr": {"6.4.4.10685-ls324"},
		},
		tagDigests: map[string]string{
			"lscr.io/linuxserver/radarr:latest": "sha256:current",
		},
		digestMappings: map[string]map[string][]string{
			"lscr.io/linuxserver/radarr": {},
		},
	}
	checker := NewChecker(mockDocker, mockRegistry, newMockStorage())

	result, err := checker.CheckForUpdates(context.Background())
	if err != nil {
		t.Fatalf("CheckForUpdates failed: %v", err)
	}
	update := result.Updates[0]

	if update.Status != UpToDatePinnable {
		t.Fatalf("status = %s, want UP_TO_DATE_PINNABLE", update.Status)
	}
	if update.RecommendedTag != "6.4.4.10685-ls324" {
		t.Fatalf("RecommendedTag = %q, want the running version tag", update.RecommendedTag)
	}
}
