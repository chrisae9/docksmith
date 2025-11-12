package registry

import "context"

// Client defines the interface for Docker registry operations.
type Client interface {
	// ListTags returns all available tags for an image
	ListTags(ctx context.Context, repository string) ([]string, error)

	// GetLatestTag returns the most recent tag for an image
	GetLatestTag(ctx context.Context, repository string) (string, error)

	// GetTagDigest returns the SHA256 digest for a specific tag
	GetTagDigest(ctx context.Context, repository, tag string) (string, error)

	// ListTagsWithDigests returns a mapping of tags to their digests.
	// This allows efficient reverse-lookup to find which tag corresponds to a digest.
	// The map key is the tag name, and the value is a slice of digests (one per architecture).
	ListTagsWithDigests(ctx context.Context, repository string) (map[string][]string, error)
}

// ImageReference contains information about a Docker image.
type ImageReference struct {
	// Registry is the registry hostname (e.g., "ghcr.io", "docker.io")
	Registry string

	// Repository is the image repository (e.g., "linuxserver/plex")
	Repository string

	// Tag is the image tag (e.g., "latest", "1.2.3")
	Tag string
}

// TagInfo contains metadata about an image tag.
type TagInfo struct {
	// Name is the tag name
	Name string

	// Digest is the image digest (sha256:...)
	Digest string

	// LastModified is when the tag was last updated
	LastModified string
}

// RegistryConfig contains configuration for registry access.
type RegistryConfig struct {
	// Username for authentication (optional)
	Username string

	// Password or token for authentication (optional)
	Password string

	// Insecure allows HTTP connections (default: false)
	Insecure bool

	// Timeout for registry requests in seconds
	TimeoutSeconds int
}
