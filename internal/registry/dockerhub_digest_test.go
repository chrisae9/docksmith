package registry

import (
	"context"
	"testing"
)

func TestDockerHubListTagsWithDigests(t *testing.T) {
	client := NewDockerHubClient()
	ctx := context.Background()

	// Test with nginx (official image)
	tagDigests, err := client.ListTagsWithDigests(ctx, "library/nginx")
	if err != nil {
		t.Fatalf("Failed to list tags with digests: %v", err)
	}

	if len(tagDigests) == 0 {
		t.Fatal("Expected tags with digests, got empty map")
	}

	// Check that "latest" tag exists and has digests
	latestDigests, found := tagDigests["latest"]
	if !found {
		t.Error("Expected 'latest' tag to exist")
	}

	if len(latestDigests) == 0 {
		t.Error("Expected 'latest' tag to have digests")
	}

	// Verify digests are in correct format
	for _, digest := range latestDigests {
		if len(digest) < 64 {
			t.Errorf("Digest too short: %s", digest)
		}
		if digest[:7] != "sha256:" {
			t.Errorf("Expected digest to start with 'sha256:', got: %s", digest)
		}
	}

	// Check that we have multiple architectures for latest
	if len(latestDigests) < 2 {
		t.Logf("Warning: Expected multiple architecture digests for latest, got %d", len(latestDigests))
	}

	t.Logf("Successfully fetched %d tags with digests", len(tagDigests))
	t.Logf("Latest tag has %d architecture digests", len(latestDigests))
}

func TestDockerHubDigestResolution(t *testing.T) {
	client := NewDockerHubClient()
	ctx := context.Background()

	// Get tag→digest mappings
	tagDigests, err := client.ListTagsWithDigests(ctx, "library/nginx")
	if err != nil {
		t.Fatalf("Failed to list tags with digests: %v", err)
	}

	// Get digest for a specific version tag
	latestDigests := tagDigests["latest"]
	if len(latestDigests) == 0 {
		t.Fatal("No digests found for latest tag")
	}

	// Pick the first digest (amd64)
	testDigest := latestDigests[0]

	// Now find which semantic version tags point to the same digest
	foundVersionTags := []string{}
	for tag, digests := range tagDigests {
		for _, digest := range digests {
			if digest == testDigest {
				// Check if this is a semantic version tag
				if len(tag) > 0 && tag[0] >= '0' && tag[0] <= '9' {
					foundVersionTags = append(foundVersionTags, tag)
				}
				break
			}
		}
	}

	if len(foundVersionTags) == 0 {
		t.Log("Warning: No semantic version tags found matching latest digest")
	} else {
		t.Logf("Found %d semantic version tags matching latest: %v", len(foundVersionTags), foundVersionTags)
	}
}
