package compose

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/docker"
)

// Recreator handles compose-based container recreation
type Recreator struct {
	dockerClient docker.Client
}

// NewRecreator creates a new compose-based recreator
func NewRecreator(dockerClient docker.Client) *Recreator {
	return &Recreator{
		dockerClient: dockerClient,
	}
}

// RecreateWithCompose recreates a container using docker compose up -d
// This is the preferred method as it handles all dependencies, network modes, etc. automatically
// hostComposeFilePath: Path on the HOST for --project-directory (resolves relative volume mounts)
// containerComposeFilePath: Path in the CONTAINER for -f flag (where docker compose can read the file)
func (r *Recreator) RecreateWithCompose(ctx context.Context, container *docker.Container, hostComposeFilePath, containerComposeFilePath string) error {
	if hostComposeFilePath == "" || containerComposeFilePath == "" {
		return fmt.Errorf("no compose file path available for container %s", container.Name)
	}

	// Get service name from compose labels
	serviceName, ok := container.Labels["com.docker.compose.service"]
	if !ok || serviceName == "" {
		return fmt.Errorf("container %s has no com.docker.compose.service label", container.Name)
	}

	hostComposeDir := filepath.Dir(hostComposeFilePath)
	containerComposeDir := filepath.Dir(containerComposeFilePath)
	log.Printf("COMPOSE: Recreating service %s using compose file (host: %s, container: %s)",
		serviceName, hostComposeFilePath, containerComposeFilePath)

	// Ensure host path is accessible inside the container via symlink
	// This allows docker compose to find env_file and other project files at the host path
	if err := ensureHostPathAccessible(hostComposeDir, containerComposeDir); err != nil {
		log.Printf("COMPOSE: Warning: Could not create symlink for host path: %v", err)
	}

	// Build the docker compose up command
	// Use -d for detached mode
	// Use --project-directory with HOST path so volume mounts resolve correctly for Docker daemon
	// The symlink above ensures env_file and other project files are accessible at the host path
	// Use --force-recreate to avoid Docker volume mount corruption issues
	args := []string{
		"compose",
		"--project-directory", hostComposeDir,  // HOST path for volume mount resolution
		"-f", containerComposeFilePath,         // CONTAINER path for reading the file
		"up",
		"-d",
		"--force-recreate", // Force clean recreation to avoid volume mount issues
		"--no-deps",        // Don't start linked services (we'll handle dependencies ourselves)
		serviceName,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	// Note: Don't set cmd.Dir here - composeDir is a host path that doesn't exist in the container.
	// The --project-directory flag tells Docker Compose where to resolve relative paths.

	log.Printf("COMPOSE: Executing: docker %s", strings.Join(args, " "))

	// Capture output for logging
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up failed: %w\nOutput: %s", err, output)
	}

	log.Printf("COMPOSE: Output: %s", output)
	log.Printf("COMPOSE: Successfully recreated service %s", serviceName)

	return nil
}

// RecreateMultipleServices recreates multiple services from the same stack
// Used when updating multiple containers or when dependencies need to be restarted
// hostComposeFilePath: Path on the HOST for --project-directory (resolves relative volume mounts)
// containerComposeFilePath: Path in the CONTAINER for -f flag (where docker compose can read the file)
func (r *Recreator) RecreateMultipleServices(ctx context.Context, hostComposeFilePath, containerComposeFilePath string, serviceNames []string) error {
	if hostComposeFilePath == "" || containerComposeFilePath == "" {
		return fmt.Errorf("no compose file path provided")
	}

	if len(serviceNames) == 0 {
		return fmt.Errorf("no services specified")
	}

	hostComposeDir := filepath.Dir(hostComposeFilePath)
	containerComposeDir := filepath.Dir(containerComposeFilePath)
	log.Printf("COMPOSE: Recreating services %v using compose file (host: %s, container: %s)",
		serviceNames, hostComposeFilePath, containerComposeFilePath)

	// Ensure host path is accessible inside the container via symlink
	// This allows docker compose to find env_file and other project files at the host path
	if err := ensureHostPathAccessible(hostComposeDir, containerComposeDir); err != nil {
		log.Printf("COMPOSE: Warning: Could not create symlink for host path: %v", err)
	}

	// Build the docker compose up command for multiple services
	// Use --project-directory with HOST path so volume mounts resolve correctly for Docker daemon
	// The symlink above ensures env_file and other project files are accessible at the host path
	// Use --force-recreate to avoid Docker volume mount corruption issues
	args := []string{
		"compose",
		"--project-directory", hostComposeDir,  // HOST path for volume mount resolution
		"-f", containerComposeFilePath,         // CONTAINER path for reading the file
		"up",
		"-d",
		"--force-recreate", // Force clean recreation to avoid volume mount issues
	}
	args = append(args, serviceNames...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	// Note: Don't set cmd.Dir here - composeDir is a host path that doesn't exist in the container.
	// The --project-directory flag tells Docker Compose where to resolve relative paths.

	log.Printf("COMPOSE: Executing: docker %s", strings.Join(args, " "))

	// Capture output for logging
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose up failed: %w\nOutput: %s", err, output)
	}

	log.Printf("COMPOSE: Output: %s", output)
	log.Printf("COMPOSE: Successfully recreated services %v", serviceNames)

	return nil
}

// FindNetworkModeDependents finds containers that use network_mode: service:xxx
// pointing to the given container name
func (r *Recreator) FindNetworkModeDependents(ctx context.Context, containerName string) ([]string, error) {
	containers, err := r.dockerClient.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	dependents := []string{}

	for _, c := range containers {
		// Check if this container uses network_mode pointing to our container
		// Format in labels: "container:name" or "service:name"
		networkModeLabel := c.Labels["com.docker.compose.network_mode"]
		if networkModeLabel != "" {
			if strings.HasSuffix(networkModeLabel, ":"+containerName) {
				serviceName := c.Labels["com.docker.compose.service"]
				if serviceName != "" {
					dependents = append(dependents, serviceName)
					log.Printf("COMPOSE: Found network_mode dependent: %s uses network of %s", serviceName, containerName)
				}
			}
		}
	}

	return dependents, nil
}

// WaitForHealthy waits for a container to become healthy
func (r *Recreator) WaitForHealthy(ctx context.Context, containerName string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Give the container a moment to start
	time.Sleep(2 * time.Second)

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("health check timeout after %v", timeout)

		case <-ticker.C:
			// Use docker inspect to check container state
			cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Status}}", containerName)
			output, err := cmd.CombinedOutput()
			if err != nil {
				log.Printf("COMPOSE: Failed to inspect container %s: %v", containerName, err)
				continue
			}

			status := strings.TrimSpace(string(output))
			log.Printf("COMPOSE: Container %s status: %s", containerName, status)

			if status == "running" {
				// Check health if available
				cmd = exec.CommandContext(ctx, "docker", "inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", containerName)
				healthOutput, err := cmd.CombinedOutput()
				if err == nil {
					healthStatus := strings.TrimSpace(string(healthOutput))
					if healthStatus == "none" {
						// No health check, container is running, we're good
						return nil
					} else if healthStatus == "healthy" {
						return nil
					} else if healthStatus == "unhealthy" {
						return fmt.Errorf("container is unhealthy")
					}
					// Still starting up, continue waiting
				} else {
					// Assume no health check, container is running
					return nil
				}
			} else if status == "exited" || status == "dead" {
				return fmt.Errorf("container exited with status: %s", status)
			}
		}
	}
}

// ensureHostPathAccessible creates a symlink to make a host path accessible inside the container.
// This solves the docker-in-docker problem where:
// - Volume mounts need HOST paths (for Docker daemon to resolve on host)
// - env_file needs to be readable inside the container
// By creating a symlink from hostPath -> containerPath, both requirements are satisfied.
func ensureHostPathAccessible(hostPath, containerPath string) error {
	// If paths are the same, no symlink needed (not running in docker-in-docker or same mount)
	if hostPath == containerPath {
		return nil
	}

	// Check if host path already exists
	if _, err := os.Lstat(hostPath); err == nil {
		// Path exists - check if it's already correct
		if target, err := os.Readlink(hostPath); err == nil && target == containerPath {
			// Already a symlink to the right place
			return nil
		}
		// Path exists but isn't a symlink to containerPath - might be the actual directory in some setups
		// or a wrong symlink. For safety, don't modify existing paths.
		log.Printf("COMPOSE: Host path %s exists, skipping symlink creation", hostPath)
		return nil
	}

	// Create parent directories for the host path
	hostParent := filepath.Dir(hostPath)
	if err := os.MkdirAll(hostParent, 0755); err != nil {
		return fmt.Errorf("failed to create parent directory %s: %w", hostParent, err)
	}

	// Create symlink: hostPath -> containerPath
	if err := os.Symlink(containerPath, hostPath); err != nil {
		return fmt.Errorf("failed to create symlink %s -> %s: %w", hostPath, containerPath, err)
	}

	log.Printf("COMPOSE: Created symlink %s -> %s for docker-in-docker compatibility", hostPath, containerPath)
	return nil
}
