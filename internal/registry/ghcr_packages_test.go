package registry

import (
	"context"
	"os"
	"testing"
)

func TestGHCRListTagsWithDigests(t *testing.T) {
	// Get GitHub token from environment
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set, skipping GHCR test")
	}

	client := NewGHCRClient(token)
	ctx := context.Background()

	// Test with linuxserver/plex
	tagDigests, err := client.ListTagsWithDigests(ctx, "linuxserver/plex")
	if err != nil {
		t.Fatalf("Failed to list tags with digests from GitHub Packages API: %v", err)
	}

	if len(tagDigests) == 0 {
		t.Fatal("Expected digest→tags mapping, got empty map")
	}

	// Each entry should have a sha256 digest as key
	foundLatest := false
	for digest, tags := range tagDigests {
		// Verify digest format
		if digest[:7] != "sha256:" {
			t.Errorf("Expected digest to start with 'sha256:', got: %s", digest)
		}

		if len(digest) < 64 {
			t.Errorf("Digest too short: %s", digest)
		}

		// Verify tags
		if len(tags) == 0 {
			t.Errorf("Expected tags for digest %s, got none", digest)
		}

		// Check if this digest has the "latest" tag
		for _, tag := range tags {
			if tag == "latest" {
				foundLatest = true
				t.Logf("Found latest tag pointing to digest: %s", digest[:16]+"...")
				t.Logf("  All tags for this digest: %v", tags)
			}
		}
	}

	if !foundLatest {
		t.Error("Expected to find 'latest' tag in results")
	}

	t.Logf("Successfully fetched %d digest entries from GitHub Packages API", len(tagDigests))
}

func TestGHCRAuthenticationWithToken(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set, skipping auth test")
	}

	client := NewGHCRClient(token)
	ctx := context.Background()

	// Test that we can access the GitHub Packages API with authentication
	tagDigests, err := client.ListTagsWithDigests(ctx, "linuxserver/plex")
	if err != nil {
		t.Fatalf("Authentication failed or API error: %v", err)
	}

	if len(tagDigests) == 0 {
		t.Fatal("Got empty result, authentication may have failed")
	}

	t.Logf("✓ Authentication successful, retrieved %d digests", len(tagDigests))
}

func TestGHCRDigestToVersionResolution(t *testing.T) {
	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		t.Skip("GITHUB_TOKEN not set, skipping test")
	}

	client := NewGHCRClient(token)
	ctx := context.Background()

	// Get digest→tags mapping
	tagDigests, err := client.ListTagsWithDigests(ctx, "linuxserver/plex")
	if err != nil {
		t.Fatalf("Failed to get digests: %v", err)
	}

	// Find the "latest" tag's digest
	var latestDigest string
	for digest, tags := range tagDigests {
		for _, tag := range tags {
			if tag == "latest" {
				latestDigest = digest
				break
			}
		}
		if latestDigest != "" {
			break
		}
	}

	if latestDigest == "" {
		t.Fatal("Could not find latest tag digest")
	}

	// Now check which semantic version tags point to the same digest
	versionTags := []string{}
	for digest, tags := range tagDigests {
		if digest == latestDigest {
			for _, tag := range tags {
				// Look for semantic version-like tags (start with digit or 'v')
				if len(tag) > 0 && (tag[0] >= '0' && tag[0] <= '9' || tag[0] == 'v') {
					// Exclude architecture-specific tags
					if len(tag) < 6 || (tag[:6] != "amd64-" &&
						(len(tag) < 8 || (tag[:8] != "arm64v8-" && tag[:8] != "arm32v7-"))) {
						versionTags = append(versionTags, tag)
					}
				}
			}
			break
		}
	}

	if len(versionTags) == 0 {
		t.Log("Warning: No semantic version tags found for latest digest")
	} else {
		t.Logf("✓ Found %d semantic version tags for latest: %v", len(versionTags), versionTags)
	}
}
