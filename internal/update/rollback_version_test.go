package update

import (
	"testing"

	"github.com/chis/docksmith/internal/storage"
)

// TestResolveRollbackVersion tests the rollback version resolution priority chain
func TestResolveRollbackVersion(t *testing.T) {
	tests := []struct {
		name             string
		detail           storage.BatchContainerDetail
		expectedVersion  string
		expectedStrategy string
	}{
		{
			name: "tag rollback when versions differ",
			detail: storage.BatchContainerDetail{
				OldVersion: "1.0",
				NewVersion: "2.0",
			},
			expectedVersion:  "1.0",
			expectedStrategy: "tag",
		},
		{
			name: "resolved rollback when tags same but resolved versions differ",
			detail: storage.BatchContainerDetail{
				OldVersion:         "latest",
				NewVersion:         "latest",
				OldResolvedVersion: "v1.0",
				NewResolvedVersion: "v2.0",
			},
			expectedVersion:  "v1.0",
			expectedStrategy: "resolved",
		},
		{
			name: "digest rollback when tags and resolved are same",
			detail: storage.BatchContainerDetail{
				OldVersion:         "latest",
				NewVersion:         "latest",
				OldResolvedVersion: "v1.0",
				NewResolvedVersion: "v1.0",
				OldDigest:          "sha256:abc123",
			},
			expectedVersion:  "sha256:abc123",
			expectedStrategy: "digest",
		},
		{
			name: "not rollbackable when everything is the same",
			detail: storage.BatchContainerDetail{
				OldVersion:         "latest",
				NewVersion:         "latest",
				OldResolvedVersion: "v1.0",
				NewResolvedVersion: "v1.0",
			},
			expectedVersion:  "",
			expectedStrategy: "none",
		},
		{
			name: "tag rollback takes priority over resolved",
			detail: storage.BatchContainerDetail{
				OldVersion:         "1.0",
				NewVersion:         "2.0",
				OldResolvedVersion: "v1.0",
				NewResolvedVersion: "v2.0",
				OldDigest:          "sha256:abc123",
			},
			expectedVersion:  "1.0",
			expectedStrategy: "tag",
		},
		{
			name: "resolved rollback takes priority over digest",
			detail: storage.BatchContainerDetail{
				OldVersion:         "latest",
				NewVersion:         "latest",
				OldResolvedVersion: "v1.0",
				NewResolvedVersion: "v2.0",
				OldDigest:          "sha256:abc123",
			},
			expectedVersion:  "v1.0",
			expectedStrategy: "resolved",
		},
		{
			name: "digest rollback when old resolved is empty",
			detail: storage.BatchContainerDetail{
				OldVersion: "latest",
				NewVersion: "latest",
				OldDigest:  "sha256:def456",
			},
			expectedVersion:  "sha256:def456",
			expectedStrategy: "digest",
		},
		{
			name: "empty detail is not rollbackable",
			detail: storage.BatchContainerDetail{
				OldVersion: "",
				NewVersion: "",
			},
			expectedVersion:  "",
			expectedStrategy: "none",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, strategy := resolveRollbackVersion(tt.detail)

			if version != tt.expectedVersion {
				t.Errorf("Expected version %q, got %q", tt.expectedVersion, version)
			}
			if strategy != tt.expectedStrategy {
				t.Errorf("Expected strategy %q, got %q", tt.expectedStrategy, strategy)
			}
		})
	}
}
