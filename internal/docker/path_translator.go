package docker

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// PathTranslator translates host paths to container paths based on volume mounts
type PathTranslator struct {
	client   *client.Client
	mappings map[string]string // host prefix -> container prefix
	mu       sync.RWMutex
	inDocker bool
}

// NewPathTranslator creates a new path translator
func NewPathTranslator(client *client.Client) *PathTranslator {
	pt := &PathTranslator{
		client:   client,
		mappings: make(map[string]string),
		inDocker: false,
	}

	// Check if we're running inside Docker
	if _, err := os.Stat("/.dockerenv"); err == nil {
		pt.inDocker = true
		log.Println("Running inside Docker - path translation enabled")
	} else {
		log.Println("Running outside Docker - path translation disabled")
	}

	// Discover volume mounts if running in Docker
	if pt.inDocker {
		if err := pt.discoverMounts(); err != nil {
			log.Printf("Warning: Failed to discover volume mounts: %v", err)
		}
	}

	return pt
}

// discoverMounts discovers volume mounts by inspecting our own container
func (pt *PathTranslator) discoverMounts() error {
	ctx := context.Background()

	// Read our own hostname (which is the container ID in Docker)
	hostname, err := os.Hostname()
	if err != nil {
		return err
	}

	// Try to inspect the container by ID
	containerJSON, err := pt.client.ContainerInspect(ctx, hostname)
	if err != nil {
		log.Printf("Failed to inspect container %s: %v", hostname, err)
		// Try alternative: inspect all containers and find ourselves by looking for /.dockerenv
		// This is a fallback for edge cases
		return pt.discoverMountsAlternative()
	}

	return pt.extractMounts(containerJSON.Mounts)
}

// discoverMountsAlternative tries to find our container by looking for docksmith
func (pt *PathTranslator) discoverMountsAlternative() error {
	ctx := context.Background()

	containers, err := pt.client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return err
	}

	// Look for a container with "docksmith" in the name
	for _, c := range containers {
		for _, name := range c.Names {
			if strings.Contains(name, "docksmith") {
				containerJSON, err := pt.client.ContainerInspect(ctx, c.ID)
				if err != nil {
					continue
				}
				log.Printf("Found docksmith container: %s", name)
				return pt.extractMounts(containerJSON.Mounts)
			}
		}
	}

	log.Println("Could not find docksmith container for mount discovery")
	return nil
}

// extractMounts extracts path mappings from container mounts
func (pt *PathTranslator) extractMounts(mounts []container.MountPoint) error {
	pt.mu.Lock()
	defer pt.mu.Unlock()

	log.Println("Discovered volume mounts:")
	for _, mount := range mounts {
		if mount.Type == "bind" && mount.Source != "" && mount.Destination != "" {
			// Normalize paths (ensure trailing slash for prefix matching)
			source := mount.Source
			destination := mount.Destination

			if !strings.HasSuffix(source, "/") {
				source += "/"
			}
			if !strings.HasSuffix(destination, "/") {
				destination += "/"
			}

			pt.mappings[source] = destination
			log.Printf("  %s -> %s", source, destination)
		}
	}

	if len(pt.mappings) == 0 {
		log.Println("  (no bind mounts found)")
	}

	return nil
}

// translatePath finds the longest matching prefix and translates the path
// toContainer=true: host path -> container path, toContainer=false: container path -> host path
func (pt *PathTranslator) translatePath(path string, toContainer bool) string {
	if !pt.inDocker {
		return path
	}

	pt.mu.RLock()
	defer pt.mu.RUnlock()

	var longestMatch string
	var replacement string

	for source, dest := range pt.mappings {
		var prefix, newPrefix string
		if toContainer {
			prefix, newPrefix = source, dest
		} else {
			prefix, newPrefix = dest, source
		}

		if strings.HasPrefix(path, prefix) && len(prefix) > len(longestMatch) {
			longestMatch = prefix
			replacement = newPrefix
		}
	}

	if longestMatch != "" {
		result := strings.Replace(path, longestMatch, replacement, 1)
		direction := "host->container"
		if !toContainer {
			direction = "container->host"
		}
		log.Printf("Path translation (%s): %s -> %s", direction, path, result)
		return result
	}

	if toContainer {
		log.Printf("No path translation for: %s (returning as-is)", path)
	}
	return path
}

// TranslateToContainer translates a host path to the equivalent container path
func (pt *PathTranslator) TranslateToContainer(hostPath string) string {
	return pt.translatePath(hostPath, true)
}

// TranslateToHost translates a container path to the equivalent host path
func (pt *PathTranslator) TranslateToHost(containerPath string) string {
	return pt.translatePath(containerPath, false)
}

// GetMappings returns the current path mappings (for debugging)
func (pt *PathTranslator) GetMappings() map[string]string {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	result := make(map[string]string, len(pt.mappings))
	for k, v := range pt.mappings {
		result[k] = v
	}
	return result
}

// IsRunningInDocker returns true if docksmith is running inside a Docker container
func (pt *PathTranslator) IsRunningInDocker() bool {
	return pt.inDocker
}
