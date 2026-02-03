// Package selfupdate provides self-detection utilities for docksmith to identify
// when it is updating itself, enabling special handling for self-update scenarios.
package selfupdate

import (
	"log"
	"os"
	"strings"
)

var (
	// selfContainerID is the short container ID of the docksmith container.
	// Docker sets the hostname to the short (12-char) container ID.
	selfContainerID string

	// selfImageName is the image name docksmith is running from.
	// Used as a fallback for self-detection.
	selfImageName string

	// initialized tracks whether Init() has been called.
	initialized bool
)

// Init initializes the self-detection system by capturing the current
// container's identity. This should be called early in the startup process.
func Init() {
	if initialized {
		return
	}

	// Docker sets hostname to short container ID (12 chars)
	hostname, err := os.Hostname()
	if err != nil {
		log.Printf("selfupdate: failed to get hostname: %v", err)
	} else {
		selfContainerID = hostname
		log.Printf("selfupdate: initialized with container ID: %s", selfContainerID)
	}

	// Also check for image name from environment (can be set in compose)
	selfImageName = os.Getenv("DOCKSMITH_IMAGE")
	if selfImageName == "" {
		// Default patterns to look for
		selfImageName = "docksmith"
	}

	initialized = true
}

// IsSelf checks if the given container ID matches the docksmith container.
// The containerID can be either the full ID or the short ID.
func IsSelf(containerID string) bool {
	if selfContainerID == "" {
		return false
	}

	// Docker hostname is the short (12-char) container ID
	// Full container IDs are 64 chars
	if len(containerID) >= 12 && len(selfContainerID) >= 12 {
		return strings.HasPrefix(containerID, selfContainerID) ||
			strings.HasPrefix(selfContainerID, containerID[:12])
	}

	return containerID == selfContainerID
}

// IsSelfByImage checks if the given image name/reference appears to be docksmith.
// This is a heuristic check based on the image name containing "docksmith".
func IsSelfByImage(imageName string) bool {
	imageLower := strings.ToLower(imageName)

	// Check for common docksmith image patterns
	patterns := []string{
		"docksmith",
		"ghcr.io/chrisae9/docksmith",
	}

	for _, pattern := range patterns {
		if strings.Contains(imageLower, pattern) {
			return true
		}
	}

	return false
}

// IsSelfByName checks if the given container name is the docksmith container.
// This checks for common naming patterns.
func IsSelfByName(containerName string) bool {
	nameLower := strings.ToLower(containerName)

	// Check for common docksmith container name patterns
	patterns := []string{
		"docksmith",
	}

	for _, pattern := range patterns {
		if strings.Contains(nameLower, pattern) {
			return true
		}
	}

	return false
}

// IsSelfContainer performs comprehensive self-detection using container ID,
// image name, and container name.
func IsSelfContainer(containerID, imageName, containerName string) bool {
	return IsSelf(containerID) || IsSelfByImage(imageName) || IsSelfByName(containerName)
}

// GetSelfContainerID returns the detected container ID of the docksmith container.
// Returns empty string if not initialized or detection failed.
func GetSelfContainerID() string {
	return selfContainerID
}

// IsInitialized returns whether the self-detection system has been initialized.
func IsInitialized() bool {
	return initialized
}
