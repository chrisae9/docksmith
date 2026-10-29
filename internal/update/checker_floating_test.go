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
		ref   string // registry/repository key used by the registry mock
		want  bool
	}{
		{"latest", "reg.example/app:latest", "reg.example/app", true},
		{"latest-alpine", "reg.example/app:latest-alpine", "reg.example/app", true},
		{"stable", "reg.example/app:stable", "reg.example/app", true},
		{"main", "reg.example/app:main", "reg.example/app", true},
		{"pinned semver", "reg.example/app:1.25.3", "reg.example/app", false},
		{"major only", "reg.example/app:16", "reg.example/app", false},
		{"dockerhub stable", "nginx:stable", "docker.io/library/nginx", true},
		{"dockerhub stable-alpine", "nginx:stable-alpine", "docker.io/library/nginx", true},
		{"dockerhub latest", "nginx:latest", "docker.io/library/nginx", true},
		{"pinned with hyphen in path", "dev-tools:1.2.3", "docker.io/library/dev-tools", false},
	}

	tags := []string{"latest", "stable", "main", "1.25.3", "16"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDocker := &mockDockerClient{
				containers:    []docker.Container{{ID: "c", Name: "app", Image: tt.image}},
				imageDigests:  map[string]string{tt.image: "sha256:abc"},
				imageVersions: map[string]string{tt.image: "1.25.3"},
				localImages:   map[string]bool{},
			}
			tagDigests := make(map[string]string, len(tags))
			for _, tag := range tags {
				tagDigests[tt.ref+":"+tag] = "sha256:abc"
			}
			mockRegistry := &mockRegistryClient{
				tags:           map[string][]string{tt.ref: tags},
				tagDigests:     tagDigests,
				digestMappings: map[string]map[string][]string{tt.ref: {}},
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
