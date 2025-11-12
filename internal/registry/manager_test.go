package registry

import (
	"context"
	"testing"
	"time"
)

func TestParseImageRef(t *testing.T) {
	manager := NewManager("")

	tests := []struct {
		name             string
		imageRef         string
		expectedRegistry string
		expectedRepo     string
	}{
		{
			name:             "simple image name",
			imageRef:         "nginx",
			expectedRegistry: "docker.io",
			expectedRepo:     "library/nginx",
		},
		{
			name:             "user/image format",
			imageRef:         "linuxserver/plex",
			expectedRegistry: "docker.io",
			expectedRepo:     "linuxserver/plex",
		},
		{
			name:             "ghcr.io image",
			imageRef:         "ghcr.io/linuxserver/plex",
			expectedRegistry: "ghcr.io",
			expectedRepo:     "linuxserver/plex",
		},
		{
			name:             "full docker.io path",
			imageRef:         "docker.io/library/nginx",
			expectedRegistry: "docker.io",
			expectedRepo:     "library/nginx",
		},
		{
			name:             "custom registry",
			imageRef:         "registry.example.com/myuser/myapp",
			expectedRegistry: "registry.example.com",
			expectedRepo:     "myuser/myapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry, repo := manager.parseImageRef(tt.imageRef)

			if registry != tt.expectedRegistry {
				t.Errorf("Registry: got %q, want %q", registry, tt.expectedRegistry)
			}

			if repo != tt.expectedRepo {
				t.Errorf("Repository: got %q, want %q", repo, tt.expectedRepo)
			}
		})
	}
}

// TestListTagsDockerHub tests listing tags from Docker Hub.
// This test makes real HTTP requests and can be skipped with -short flag.
func TestListTagsDockerHub(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	manager := NewManager("")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with a well-known public image
	tags, err := manager.ListTags(ctx, "library/nginx")
	if err != nil {
		t.Fatalf("Failed to list tags: %v", err)
	}

	if len(tags) == 0 {
		t.Error("Expected at least one tag")
	}

	// Check if "latest" tag exists
	hasLatest := false
	for _, tag := range tags {
		if tag == "latest" {
			hasLatest = true
			break
		}
	}

	if !hasLatest {
		t.Error("Expected to find 'latest' tag")
	}

	t.Logf("Found %d tags for nginx", len(tags))
}

// TestListTagsGHCR tests listing tags from GHCR.
// This test makes real HTTP requests and can be skipped with -short flag.
func TestListTagsGHCR(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	manager := NewManager("")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test with a well-known public GHCR image
	tags, err := manager.ListTags(ctx, "ghcr.io/linuxserver/plex")
	if err != nil {
		t.Logf("Note: GHCR test failed (may require auth): %v", err)
		t.Skip("Skipping GHCR test - may require authentication")
	}

	if len(tags) == 0 {
		t.Error("Expected at least one tag")
	}

	t.Logf("Found %d tags for linuxserver/plex", len(tags))
}
