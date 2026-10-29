package update

import (
	"context"
	"testing"

	"github.com/chis/docksmith/internal/docker"
)

func TestCheckContainerSetsUsingFloatingTag(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  bool
	}{
		{"latest", "reg.example/app:latest", true},
		{"latest-alpine", "reg.example/app:latest-alpine", true},
		{"stable", "reg.example/app:stable", true},
		{"main", "reg.example/app:main", true},
		{"pinned semver", "reg.example/app:1.25.3", false},
		{"major only", "reg.example/app:16", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDocker := &mockDockerClient{
				containers:    []docker.Container{{ID: "c", Name: "app", Image: tt.image}},
				imageDigests:  map[string]string{tt.image: "sha256:abc"},
				imageVersions: map[string]string{tt.image: "1.25.3"},
				localImages:   map[string]bool{},
			}
			mockRegistry := &mockRegistryClient{
				tags: map[string][]string{
					"reg.example/app": {"latest", "stable", "main", "1.25.3", "16"},
				},
				tagDigests:     map[string]string{tt.image: "sha256:abc"},
				digestMappings: map[string]map[string][]string{"reg.example/app": {}},
			}
			checker := NewChecker(mockDocker, mockRegistry, newMockStorage())

			result, err := checker.CheckForUpdates(context.Background())
			if err != nil {
				t.Fatalf("CheckForUpdates failed: %v", err)
			}
			if len(result.Updates) != 1 {
				t.Fatalf("got %d updates, want 1", len(result.Updates))
			}
			if got := result.Updates[0].UsingFloatingTag; got != tt.want {
				t.Fatalf("UsingFloatingTag = %v, want %v", got, tt.want)
			}
		})
	}
}
