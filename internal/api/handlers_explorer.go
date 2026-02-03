package api

import (
	"fmt"
	"net/http"

	"github.com/chis/docksmith/internal/docker"
	"github.com/docker/docker/api/types/container"
	"golang.org/x/sync/errgroup"
)

// ExplorerData represents the combined response for the explorer endpoint
type ExplorerData struct {
	ContainerStacks      map[string][]docker.ContainerExplorerItem `json:"container_stacks"`
	StandaloneContainers []docker.ContainerExplorerItem            `json:"standalone_containers"`
	Images               []docker.ImageInfo                        `json:"images"`
	Networks             []docker.NetworkInfo                      `json:"networks"`
	Volumes              []docker.VolumeInfo                       `json:"volumes"`
}

// handleExplorer returns combined data for the explorer view
// GET /api/explorer
func (s *Server) handleExplorer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// First, fetch raw containers (needed by images and volumes to check usage)
	rawContainers, err := s.dockerService.GetClient().ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to list containers: %w", err))
		return
	}

	// Fetch remaining data in parallel using errgroup
	var containers []docker.ContainerExplorerItem
	var images []docker.ImageInfo
	var networks []docker.NetworkInfo
	var volumes []docker.VolumeInfo

	g, gCtx := errgroup.WithContext(ctx)

	// Containers for explorer (transform raw containers)
	g.Go(func() error {
		result, err := s.dockerService.ListContainersForExplorer(gCtx)
		if err != nil {
			return err
		}
		containers = result
		return nil
	})

	// Images (uses pre-fetched containers)
	g.Go(func() error {
		result, err := s.dockerService.ListImagesWithContainers(gCtx, rawContainers)
		if err != nil {
			return err
		}
		images = result
		return nil
	})

	// Networks (parallelized internally)
	g.Go(func() error {
		result, err := s.dockerService.ListNetworks(gCtx)
		if err != nil {
			return err
		}
		networks = result
		return nil
	})

	// Volumes (uses pre-fetched containers)
	g.Go(func() error {
		result, err := s.dockerService.ListVolumesWithContainers(gCtx, rawContainers)
		if err != nil {
			return err
		}
		volumes = result
		return nil
	})

	// Wait for all goroutines to complete
	if err := g.Wait(); err != nil {
		RespondInternalError(w, err)
		return
	}

	// Group containers by stack
	containerStacks := make(map[string][]docker.ContainerExplorerItem)
	var standaloneContainers []docker.ContainerExplorerItem

	for _, c := range containers {
		if c.Stack != "" {
			containerStacks[c.Stack] = append(containerStacks[c.Stack], c)
		} else {
			standaloneContainers = append(standaloneContainers, c)
		}
	}

	// Ensure empty slices are not nil (for consistent JSON)
	if standaloneContainers == nil {
		standaloneContainers = []docker.ContainerExplorerItem{}
	}

	RespondSuccess(w, ExplorerData{
		ContainerStacks:      containerStacks,
		StandaloneContainers: standaloneContainers,
		Images:               images,
		Networks:             networks,
		Volumes:              volumes,
	})
}

// handleImages returns all Docker images
// GET /api/images
func (s *Server) handleImages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	images, err := s.dockerService.ListImages(ctx)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"images": images,
		"count":  len(images),
	})
}

// handleNetworks returns all Docker networks
// GET /api/networks
func (s *Server) handleNetworks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	networks, err := s.dockerService.ListNetworks(ctx)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"networks": networks,
		"count":    len(networks),
	})
}

// handleVolumes returns all Docker volumes
// GET /api/volumes
func (s *Server) handleVolumes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	volumes, err := s.dockerService.ListVolumes(ctx)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"volumes": volumes,
		"count":   len(volumes),
	})
}

// handleRemoveNetwork removes a Docker network
// DELETE /api/networks/{id}
func (s *Server) handleRemoveNetwork(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	networkID := r.PathValue("id")

	if networkID == "" {
		RespondBadRequest(w, fmt.Errorf("network ID is required"))
		return
	}

	err := s.dockerService.RemoveNetwork(ctx, networkID)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"message": "Network removed successfully",
	})
}

// handleRemoveVolume removes a Docker volume
// DELETE /api/volumes/{name}
func (s *Server) handleRemoveVolume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	volumeName := r.PathValue("name")
	force := r.URL.Query().Get("force") == "true"

	if volumeName == "" {
		RespondBadRequest(w, fmt.Errorf("volume name is required"))
		return
	}

	err := s.dockerService.RemoveVolume(ctx, volumeName, force)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"message": "Volume removed successfully",
	})
}

// handleRemoveImage removes a Docker image
// DELETE /api/images/{id}
func (s *Server) handleRemoveImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	imageID := r.PathValue("id")
	force := r.URL.Query().Get("force") == "true"

	if imageID == "" {
		RespondBadRequest(w, fmt.Errorf("image ID is required"))
		return
	}

	deleted, err := s.dockerService.RemoveImage(ctx, imageID, force)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"message": "Image removed successfully",
		"deleted": deleted,
	})
}

// handlePruneContainers removes all stopped containers
// POST /api/prune/containers
func (s *Server) handlePruneContainers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	report, err := s.dockerService.PruneContainers(ctx)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"message":         fmt.Sprintf("Pruned %d containers", len(report.ItemsDeleted)),
		"items_deleted":   report.ItemsDeleted,
		"space_reclaimed": report.SpaceReclaimed,
	})
}

// handlePruneImages removes unused images
// POST /api/prune/images
func (s *Server) handlePruneImages(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all := r.URL.Query().Get("all") == "true"

	report, err := s.dockerService.PruneImages(ctx, all)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"message":         fmt.Sprintf("Pruned %d images", len(report.ItemsDeleted)),
		"items_deleted":   report.ItemsDeleted,
		"space_reclaimed": report.SpaceReclaimed,
	})
}

// handlePruneNetworks removes unused networks
// POST /api/prune/networks
func (s *Server) handlePruneNetworks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	report, err := s.dockerService.PruneNetworks(ctx)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"message":       fmt.Sprintf("Pruned %d networks", len(report.ItemsDeleted)),
		"items_deleted": report.ItemsDeleted,
	})
}

// handlePruneVolumes removes unused volumes
// POST /api/prune/volumes
func (s *Server) handlePruneVolumes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	report, err := s.dockerService.PruneVolumes(ctx)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	RespondSuccess(w, map[string]any{
		"message":         fmt.Sprintf("Pruned %d volumes", len(report.ItemsDeleted)),
		"items_deleted":   report.ItemsDeleted,
		"space_reclaimed": report.SpaceReclaimed,
	})
}

// handleSystemPrune removes all unused Docker resources
// POST /api/prune/system
func (s *Server) handleSystemPrune(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pruneVolumes := r.URL.Query().Get("volumes") == "true"

	report, err := s.dockerService.SystemPrune(ctx, pruneVolumes)
	if err != nil {
		RespondInternalError(w, err)
		return
	}

	totalItems := len(report.ContainersDeleted) + len(report.ImagesDeleted) +
		len(report.NetworksDeleted) + len(report.VolumesDeleted)

	RespondSuccess(w, map[string]any{
		"message":            fmt.Sprintf("System prune completed: %d items removed", totalItems),
		"containers_deleted": report.ContainersDeleted,
		"images_deleted":     report.ImagesDeleted,
		"networks_deleted":   report.NetworksDeleted,
		"volumes_deleted":    report.VolumesDeleted,
		"space_reclaimed":    report.SpaceReclaimed,
	})
}
