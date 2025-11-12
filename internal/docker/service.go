package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// Service implements the Client interface using the Docker SDK.
type Service struct {
	cli *client.Client
}

// NewService creates a new Docker service that connects to the Docker socket.
// It uses the default Docker host from environment variables or defaults to
// unix:///var/run/docker.sock on Unix systems.
func NewService() (*Service, error) {
	cli, err := client.NewClientWithOpts(
		client.FromEnv,
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &Service{cli: cli}, nil
}

// ListContainers retrieves all containers from the Docker daemon.
// It returns both running and stopped containers.
func (s *Service) ListContainers(ctx context.Context) ([]Container, error) {
	containers, err := s.cli.ContainerList(ctx, container.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]Container, 0, len(containers))
	for _, c := range containers {
		result = append(result, s.convertContainer(c))
	}

	return result, nil
}

// IsLocalImage checks if an image was built locally by inspecting its RepoDigests.
// Images pulled from a registry will have RepoDigests populated with the registry digest.
// Locally built images will have empty RepoDigests.
func (s *Service) IsLocalImage(ctx context.Context, imageName string) (bool, error) {
	imageInfo, _, err := s.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}

	// If RepoDigests is empty, the image was built locally
	return len(imageInfo.RepoDigests) == 0, nil
}

// GetImageVersion extracts the version from image labels.
// It checks common version labels used by container images.
func (s *Service) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	imageInfo, _, err := s.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}

	// Check common version label keys in order of preference
	versionLabels := []string{
		"org.opencontainers.image.version",
		"build_version",
		"version",
		"VERSION",
	}

	for _, label := range versionLabels {
		if val, ok := imageInfo.Config.Labels[label]; ok && val != "" {
			// For build_version from LinuxServer, extract just the version part
			// Format: "Linuxserver.io version:- 5.28.0.10274-ls286 Build-date:- ..."
			if label == "build_version" && strings.Contains(val, "version:-") {
				parts := strings.Split(val, "version:-")
				if len(parts) > 1 {
					versionPart := strings.TrimSpace(parts[1])
					// Extract version before " Build-date"
					if idx := strings.Index(versionPart, " Build-date"); idx > 0 {
						return versionPart[:idx], nil
					}
					return versionPart, nil
				}
			}
			return val, nil
		}
	}

	return "", nil // No version found
}

// GetImageDigest gets the SHA256 digest for an image.
// Returns the digest from RepoDigests if available (format: registry/repo@sha256:...)
func (s *Service) GetImageDigest(ctx context.Context, imageName string) (string, error) {
	imageInfo, _, err := s.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		return "", fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}

	// RepoDigests contains the digest with registry info: ghcr.io/linuxserver/radarr@sha256:abc123...
	if len(imageInfo.RepoDigests) > 0 {
		// Extract just the sha256 part
		digest := imageInfo.RepoDigests[0]
		if idx := strings.Index(digest, "@"); idx > 0 {
			return digest[idx+1:], nil // Returns "sha256:abc123..."
		}
		return digest, nil
	}

	// Fallback to image ID if no RepoDigest (shouldn't happen for registry images)
	return imageInfo.ID, nil
}

// Close releases resources held by the Docker client.
func (s *Service) Close() error {
	if s.cli != nil {
		return s.cli.Close()
	}
	return nil
}

// convertContainer transforms the Docker SDK container type into our domain model.
func (s *Service) convertContainer(c types.Container) Container {
	// Container names start with '/', so we trim it
	name := strings.TrimPrefix(c.Names[0], "/")
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	return Container{
		ID:      c.ID,
		Name:    name,
		Image:   c.Image,
		State:   c.State,
		Labels:  c.Labels,
		Created: c.Created,
	}
}

// GetClient returns the underlying Docker SDK client.
// This is used by components that need direct access to the Docker SDK.
func (s *Service) GetClient() *client.Client {
	return s.cli
}
