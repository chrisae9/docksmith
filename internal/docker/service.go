package docker

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

// Service implements the Client interface using the Docker SDK.
type Service struct {
	cli            *client.Client
	pathTranslator *PathTranslator
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

	// Initialize path translator for host->container path mapping
	pathTranslator := NewPathTranslator(cli)

	return &Service{
		cli:            cli,
		pathTranslator: pathTranslator,
	}, nil
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
	imageInfo, err := s.cli.ImageInspect(ctx, imageName)
	if err != nil {
		return false, fmt.Errorf("failed to inspect image %s: %w", imageName, err)
	}

	// If RepoDigests is empty, the image was built locally
	return len(imageInfo.RepoDigests) == 0, nil
}

// GetImageVersion extracts the version from image labels.
// It checks common version labels used by container images.
func (s *Service) GetImageVersion(ctx context.Context, imageName string) (string, error) {
	imageInfo, err := s.cli.ImageInspect(ctx, imageName)
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

	if imageInfo.Config == nil {
		return "", nil
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
	imageInfo, err := s.cli.ImageInspect(ctx, imageName)
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
func (s *Service) convertContainer(c container.Summary) Container {
	// Container names start with '/', so we trim it
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	// Extract health status from the Status field
	// Status format: "Up 2 minutes (healthy)" or "Up 5 seconds (unhealthy)" or just "Up 10 minutes"
	healthStatus := "none"
	if strings.Contains(c.Status, "(healthy)") {
		healthStatus = "healthy"
	} else if strings.Contains(c.Status, "(unhealthy)") {
		healthStatus = "unhealthy"
	} else if strings.Contains(c.Status, "(health: starting)") {
		healthStatus = "starting"
	}

	// Extract stack name from compose labels
	stack := ""
	if project, ok := c.Labels["com.docker.compose.project"]; ok && project != "" {
		stack = project
	}

	return Container{
		ID:           c.ID,
		Name:         name,
		Image:        c.Image,
		State:        c.State,
		HealthStatus: healthStatus,
		Labels:       c.Labels,
		Created:      c.Created,
		Stack:        stack,
	}
}

// GetClient returns the underlying Docker SDK client.
// This is used by components that need direct access to the Docker SDK.
func (s *Service) GetClient() *client.Client {
	return s.cli
}

// GetPathTranslator returns the path translator for host->container path mapping.
// This is used by components that need to translate file paths between host and container.
func (s *Service) GetPathTranslator() *PathTranslator {
	return s.pathTranslator
}

// FindDependentContainers finds all containers that have a restart dependency on the given container.
// It checks the "docksmith.restart-after" label for comma-separated container names.
// Returns a list of container names that should be restarted when the specified container restarts.
func (s *Service) FindDependentContainers(ctx context.Context, containerName string, restartAfterLabel string) ([]string, error) {
	containers, err := s.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var dependents []string
	for _, c := range containers {
		if restartAfter, ok := c.Labels[restartAfterLabel]; ok && restartAfter != "" {
			// Parse comma-separated list of dependencies
			dependencies := strings.Split(restartAfter, ",")
			for _, dep := range dependencies {
				dep = strings.TrimSpace(dep)
				if dep == containerName {
					dependents = append(dependents, c.Name)
					break
				}
			}
		}
	}

	return dependents, nil
}

// CreateContainerMap creates a lookup map from container names to container pointers.
// This is a common pattern used throughout the codebase for quick container lookups.
func CreateContainerMap(containers []Container) map[string]*Container {
	containerMap := make(map[string]*Container)
	for i := range containers {
		containerMap[containers[i].Name] = &containers[i]
	}
	return containerMap
}

// GetContainerByName finds a container by name using Docker's filter API.
// This is more efficient than listing all containers and searching.
// Returns the container if found, or an error if not found.
func (s *Service) GetContainerByName(ctx context.Context, containerName string) (*Container, error) {
	// Use Docker's filter API for O(1) lookup on the daemon side
	filterArgs := filters.NewArgs()
	filterArgs.Add("name", "^/"+containerName+"$") // Exact match with regex anchors

	containers, err := s.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to find container: %w", err)
	}

	// The name filter can return partial matches, so verify exact match
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.TrimPrefix(name, "/") == containerName {
				result := s.convertContainer(c)
				return &result, nil
			}
		}
	}

	return nil, fmt.Errorf("container not found: %s", containerName)
}

// ListImages returns all Docker images with usage information.
// This is a convenience wrapper that fetches containers internally.
func (s *Service) ListImages(ctx context.Context) ([]ImageInfo, error) {
	containers, err := s.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	return s.ListImagesWithContainers(ctx, containers)
}

// ListImagesWithContainers returns all Docker images using pre-fetched containers.
// This avoids redundant container listing when called from handleExplorer.
func (s *Service) ListImagesWithContainers(ctx context.Context, containers []container.Summary) ([]ImageInfo, error) {
	// Get all images
	images, err := s.cli.ImageList(ctx, image.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	// Build set of image IDs in use
	imagesInUse := make(map[string]bool)
	for _, c := range containers {
		imagesInUse[c.ImageID] = true
	}

	result := make([]ImageInfo, 0, len(images))
	for _, img := range images {
		// Filter out intermediate images (those with no tags and no repository)
		tags := img.RepoTags
		if tags == nil {
			tags = []string{}
		}

		// Check if dangling (no tags or only <none>:<none>)
		dangling := len(tags) == 0 || (len(tags) == 1 && tags[0] == "<none>:<none>")
		if dangling {
			tags = []string{} // Clean up the tags list
		}

		result = append(result, ImageInfo{
			ID:       img.ID,
			Tags:     tags,
			Size:     img.Size,
			Created:  img.Created,
			InUse:    imagesInUse[img.ID],
			Dangling: dangling,
		})
	}

	return result, nil
}

// ListNetworks returns all Docker networks with container associations.
// Network inspections are parallelized for better performance.
func (s *Service) ListNetworks(ctx context.Context) ([]NetworkInfo, error) {
	networks, err := s.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list networks: %w", err)
	}

	// Default networks that come with Docker
	defaultNetworks := map[string]bool{
		"bridge": true,
		"host":   true,
		"none":   true,
	}

	// Pre-allocate result slice with exact size
	result := make([]NetworkInfo, len(networks))

	// Use WaitGroup for parallel network inspection
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error
	var failCount int

	for i, net := range networks {
		wg.Add(1)
		go func(idx int, n network.Summary) {
			defer wg.Done()

			containerNames := make([]string, 0)

			// Inspect network to get container details
			netInspect, err := s.cli.NetworkInspect(ctx, n.ID, network.InspectOptions{})
			if err != nil {
				// Record first error and count failures
				mu.Lock()
				failCount++
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()
			} else {
				for _, endpoint := range netInspect.Containers {
					name := strings.TrimPrefix(endpoint.Name, "/")
					if name != "" {
						containerNames = append(containerNames, name)
					}
				}
			}

			result[idx] = NetworkInfo{
				ID:         n.ID,
				Name:       n.Name,
				Driver:     n.Driver,
				Scope:      n.Scope,
				Containers: containerNames,
				IsDefault:  defaultNetworks[n.Name],
				Created:    n.Created.Unix(),
			}
		}(i, net)
	}

	wg.Wait()

	// If all inspections failed, return error; otherwise return partial results
	if firstErr != nil && failCount == len(networks) {
		return nil, fmt.Errorf("failed to inspect networks: %w", firstErr)
	}

	return result, nil
}

// ListVolumes returns all Docker volumes with container associations.
// This is a convenience wrapper that fetches containers internally.
func (s *Service) ListVolumes(ctx context.Context) ([]VolumeInfo, error) {
	containers, err := s.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}
	return s.ListVolumesWithContainers(ctx, containers)
}

// ListVolumesWithContainers returns all Docker volumes using pre-fetched containers.
// This avoids redundant container listing when called from handleExplorer.
func (s *Service) ListVolumesWithContainers(ctx context.Context, containers []container.Summary) ([]VolumeInfo, error) {
	volumeList, err := s.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}

	// Build map of volume name -> container names using it
	volumeUsers := make(map[string][]string)
	for _, c := range containers {
		containerName := ""
		if len(c.Names) > 0 {
			containerName = strings.TrimPrefix(c.Names[0], "/")
		}
		for _, mount := range c.Mounts {
			if mount.Type == "volume" && mount.Name != "" {
				volumeUsers[mount.Name] = append(volumeUsers[mount.Name], containerName)
			}
		}
	}

	// Get volume sizes via disk usage API (call once, not per volume)
	volumeSizes := make(map[string]int64)
	du, err := s.cli.DiskUsage(ctx, types.DiskUsageOptions{
		Types: []types.DiskUsageObject{types.VolumeObject},
	})
	if err == nil && du.Volumes != nil {
		for _, v := range du.Volumes {
			volumeSizes[v.Name] = v.UsageData.Size
		}
	}

	result := make([]VolumeInfo, 0, len(volumeList.Volumes))
	for _, vol := range volumeList.Volumes {
		size, found := volumeSizes[vol.Name]
		if !found {
			size = -1 // -1 indicates unknown
		}

		// Ensure containerList is never nil (always return empty array in JSON)
		containerList := volumeUsers[vol.Name]
		if containerList == nil {
			containerList = []string{}
		}

		// Parse volume creation time (format: RFC3339)
		var created int64
		if vol.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, vol.CreatedAt); err == nil {
				created = t.Unix()
			}
		}

		result = append(result, VolumeInfo{
			Name:       vol.Name,
			Driver:     vol.Driver,
			MountPoint: vol.Mountpoint,
			Containers: containerList,
			Size:       size,
			Created:    created,
		})
	}

	return result, nil
}

// ContainerExplorerItem represents a container for the explorer view
type ContainerExplorerItem struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Image        string   `json:"image"`
	State        string   `json:"state"`
	HealthStatus string   `json:"health_status"`
	Stack        string   `json:"stack,omitempty"`
	Created      int64    `json:"created"`
	Networks     []string `json:"networks"` // Network names this container is connected to
}

// ListContainersForExplorer returns containers formatted for the explorer view.
func (s *Service) ListContainersForExplorer(ctx context.Context) ([]ContainerExplorerItem, error) {
	rawContainers, err := s.cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	result := make([]ContainerExplorerItem, 0, len(rawContainers))
	for _, c := range rawContainers {
		// Extract container name (trim leading /)
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}

		// Extract stack name from Docker Compose labels
		stack := ""
		if project, ok := c.Labels["com.docker.compose.project"]; ok && project != "" {
			stack = project
		}

		// Extract health status from Status field
		healthStatus := "none"
		if strings.Contains(c.Status, "(healthy)") {
			healthStatus = "healthy"
		} else if strings.Contains(c.Status, "(unhealthy)") {
			healthStatus = "unhealthy"
		} else if strings.Contains(c.Status, "(health: starting)") {
			healthStatus = "starting"
		}

		// Extract network names
		networks := make([]string, 0)
		if c.NetworkSettings != nil && c.NetworkSettings.Networks != nil {
			for networkName := range c.NetworkSettings.Networks {
				networks = append(networks, networkName)
			}
		}

		result = append(result, ContainerExplorerItem{
			ID:           c.ID,
			Name:         name,
			Image:        c.Image,
			State:        c.State,
			HealthStatus: healthStatus,
			Stack:        stack,
			Created:      c.Created,
			Networks:     networks,
		})
	}

	return result, nil
}

// RemoveNetwork removes a Docker network by ID or name.
func (s *Service) RemoveNetwork(ctx context.Context, networkID string) error {
	return s.cli.NetworkRemove(ctx, networkID)
}

// RemoveVolume removes a Docker volume by name.
func (s *Service) RemoveVolume(ctx context.Context, volumeName string, force bool) error {
	return s.cli.VolumeRemove(ctx, volumeName, force)
}

// RemoveImage removes a Docker image by ID.
func (s *Service) RemoveImage(ctx context.Context, imageID string, force bool) ([]image.DeleteResponse, error) {
	return s.cli.ImageRemove(ctx, imageID, image.RemoveOptions{
		Force:         force,
		PruneChildren: true,
	})
}

// PruneReport represents the result of a prune operation.
type PruneReport struct {
	ItemsDeleted []string `json:"items_deleted"`
	SpaceReclaimed int64  `json:"space_reclaimed"`
}

// PruneContainers removes all stopped containers.
func (s *Service) PruneContainers(ctx context.Context) (*PruneReport, error) {
	report, err := s.cli.ContainersPrune(ctx, filters.NewArgs())
	if err != nil {
		return nil, fmt.Errorf("failed to prune containers: %w", err)
	}

	return &PruneReport{
		ItemsDeleted:   report.ContainersDeleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

// PruneImages removes unused (dangling) images.
func (s *Service) PruneImages(ctx context.Context, all bool) (*PruneReport, error) {
	args := filters.NewArgs()
	if !all {
		// Only prune dangling images by default
		args.Add("dangling", "true")
	}

	report, err := s.cli.ImagesPrune(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("failed to prune images: %w", err)
	}

	deleted := make([]string, 0, len(report.ImagesDeleted))
	for _, img := range report.ImagesDeleted {
		if img.Deleted != "" {
			deleted = append(deleted, img.Deleted)
		} else if img.Untagged != "" {
			deleted = append(deleted, img.Untagged)
		}
	}

	return &PruneReport{
		ItemsDeleted:   deleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

// PruneNetworks removes unused networks.
func (s *Service) PruneNetworks(ctx context.Context) (*PruneReport, error) {
	report, err := s.cli.NetworksPrune(ctx, filters.NewArgs())
	if err != nil {
		return nil, fmt.Errorf("failed to prune networks: %w", err)
	}

	return &PruneReport{
		ItemsDeleted:   report.NetworksDeleted,
		SpaceReclaimed: 0, // Networks don't report space reclaimed
	}, nil
}

// PruneVolumes removes unused volumes.
func (s *Service) PruneVolumes(ctx context.Context) (*PruneReport, error) {
	report, err := s.cli.VolumesPrune(ctx, filters.NewArgs())
	if err != nil {
		return nil, fmt.Errorf("failed to prune volumes: %w", err)
	}

	return &PruneReport{
		ItemsDeleted:   report.VolumesDeleted,
		SpaceReclaimed: int64(report.SpaceReclaimed),
	}, nil
}

// SystemPruneReport represents the combined result of a system prune.
type SystemPruneReport struct {
	ContainersDeleted []string `json:"containers_deleted"`
	ImagesDeleted     []string `json:"images_deleted"`
	NetworksDeleted   []string `json:"networks_deleted"`
	VolumesDeleted    []string `json:"volumes_deleted"`
	SpaceReclaimed    int64    `json:"space_reclaimed"`
}

// SystemPrune removes all unused Docker resources.
func (s *Service) SystemPrune(ctx context.Context, pruneVolumes bool) (*SystemPruneReport, error) {
	result := &SystemPruneReport{}

	// Prune containers
	containerReport, err := s.PruneContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("container prune failed: %w", err)
	}
	result.ContainersDeleted = containerReport.ItemsDeleted
	result.SpaceReclaimed += containerReport.SpaceReclaimed

	// Prune images
	imageReport, err := s.PruneImages(ctx, false)
	if err != nil {
		return nil, fmt.Errorf("image prune failed: %w", err)
	}
	result.ImagesDeleted = imageReport.ItemsDeleted
	result.SpaceReclaimed += imageReport.SpaceReclaimed

	// Prune networks
	networkReport, err := s.PruneNetworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("network prune failed: %w", err)
	}
	result.NetworksDeleted = networkReport.ItemsDeleted

	// Optionally prune volumes
	if pruneVolumes {
		volumeReport, err := s.PruneVolumes(ctx)
		if err != nil {
			return nil, fmt.Errorf("volume prune failed: %w", err)
		}
		result.VolumesDeleted = volumeReport.ItemsDeleted
		result.SpaceReclaimed += volumeReport.SpaceReclaimed
	}

	return result, nil
}
