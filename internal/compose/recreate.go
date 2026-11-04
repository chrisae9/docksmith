package compose

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/docker"
)

// Overridable in tests — they must not spawn docker.
var (
	dockerCommand = func(ctx context.Context, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	}
	networkModeWaitTimeout  = 60 * time.Second
	networkModePollInterval = 1 * time.Second
	composeRetryBackoff     = []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 15 * time.Second}
)

// isNetworkNamespaceRace reports whether a failed `docker compose up` failed
// only because its network_mode provider was mid-restart.
func isNetworkNamespaceRace(output []byte) bool {
	s := string(output)
	return strings.Contains(s, "is restarting, wait until the container is running") ||
		strings.Contains(s, "cannot join network namespace")
}

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
	log.Printf("COMPOSE: Recreating service %s using compose file (host: %s, container: %s)",
		serviceName, hostComposeFilePath, containerComposeFilePath)

	// If this container joins another container's network namespace, wait for
	// that provider to be running before recreating.
	if err := r.waitForNetworkModeProvider(ctx, container); err != nil {
		return err
	}

	// First, stop and remove the existing container
	// This is necessary because docker compose up --force-recreate doesn't work when
	// the container was created via docker run instead of docker compose
	log.Printf("COMPOSE: Stopping and removing existing container %s", container.Name)
	stopOutput, _ := dockerCommand(ctx, "stop", container.Name) // Ignore errors if already stopped
	log.Printf("COMPOSE: Stop output: %s", stopOutput)

	rmOutput, _ := dockerCommand(ctx, "rm", container.Name) // Ignore errors if doesn't exist
	log.Printf("COMPOSE: Remove output: %s", rmOutput)

	// Build the docker compose up command
	// Use -d for detached mode
	// Use --project-directory with HOST path so volume mounts resolve correctly for Docker daemon
	// NOTE: For env_file to work, docksmith must have mirror mounts (same dir at host path)
	// Use --force-recreate to avoid Docker volume mount corruption issues
	args := []string{
		"compose",
		"--project-directory", hostComposeDir, // HOST path for volume mount resolution
		"-f", containerComposeFilePath, // CONTAINER path for reading the file
		"up",
		"-d",
		"--force-recreate", // Force clean recreation to avoid volume mount issues
		"--no-deps",        // Don't start linked services (we'll handle dependencies ourselves)
		serviceName,
	}

	// Note: Don't set cmd.Dir here - composeDir is a host path that doesn't exist in the container.
	// The --project-directory flag tells Docker Compose where to resolve relative paths.

	log.Printf("COMPOSE: Executing: docker %s", strings.Join(args, " "))

	// Capture output for logging, retrying while a network_mode provider restarts
	output, err := r.runComposeWithRetry(ctx, args)
	if err != nil {
		return fmt.Errorf("docker compose up failed: %w\nOutput: %s", err, output)
	}

	log.Printf("COMPOSE: Output: %s", output)
	log.Printf("COMPOSE: Successfully recreated service %s", serviceName)

	return nil
}

// runComposeWithRetry runs a docker compose command, retrying with the delays
// in composeRetryBackoff when it fails only because its network_mode provider
// is mid-restart.
func (r *Recreator) runComposeWithRetry(ctx context.Context, args []string) ([]byte, error) {
	var output []byte
	var err error

	for _, delay := range composeRetryBackoff {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return output, ctx.Err()
			case <-time.After(delay):
			}
		}

		output, err = dockerCommand(ctx, args...)
		if err == nil {
			return output, nil
		}
		if !isNetworkNamespaceRace(output) {
			return output, err
		}
		log.Printf("COMPOSE: docker compose up hit a network namespace race, retrying: %v", err)
	}

	return output, err
}

// waitForNetworkModeProvider blocks until the container supplying this
// container's network namespace (network_mode: service:<name> or
// container:<name>) is running, or the wait timeout expires.
func (r *Recreator) waitForNetworkModeProvider(ctx context.Context, c *docker.Container) error {
	provider, ok := resolveNetworkModeProvider(c)
	if !ok {
		return nil
	}

	project := c.Labels["com.docker.compose.project"]
	log.Printf("COMPOSE: Waiting for network_mode provider %s:%s to be running", provider.prefix, provider.target)

	waitCtx, cancel := context.WithTimeout(ctx, networkModeWaitTimeout)
	defer cancel()

	providerName := provider.target
	firstListing := true
	for {
		containers, err := r.dockerClient.ListContainers(waitCtx)
		if err != nil {
			log.Printf("COMPOSE: Failed to list containers while waiting for network_mode provider: %v", err)
		} else {
			if firstListing && provider.prefix == "container" && !provider.found(containers, project) {
				// The referenced container is stale (e.g. the provider was
				// recreated and our HostConfig still points at its old ID) or
				// simply gone. Don't stall for the full timeout: let compose
				// try and the retry path handle the daemon's real error.
				log.Printf("COMPOSE: network_mode provider %s not found, not waiting", provider.target)
				return nil
			}
			firstListing = false
		}
		for _, candidate := range containers {
			if !provider.matches(candidate, project) {
				continue
			}
			providerName = candidate.Name
			if strings.EqualFold(candidate.State, "running") {
				log.Printf("COMPOSE: network_mode provider %s is running", provider.target)
				return nil
			}
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("network mode provider %s is not running (waited %s)", providerName, networkModeWaitTimeout)
		case <-time.After(networkModePollInterval):
		}
	}
}

// networkModeProvider identifies which container supplies a container's
// network namespace.
type networkModeProvider struct {
	prefix string // "container" or "service"
	target string // container ID, container name, or compose service name
}

// resolveNetworkModeProvider resolves the provider from the Docker API's
// authoritative HostConfig.NetworkMode field, falling back to the compose
// network_mode label for service: references (which Docker rewrites to
// container:<id> on the created container).
func resolveNetworkModeProvider(c *docker.Container) (networkModeProvider, bool) {
	if c.NetworkMode != "" {
		// The Docker API is authoritative: only "container:<id>" shares a
		// namespace. "host", "none" and "default" have no provider to wait for.
		if id := containerNetworkModeID(c.NetworkMode); id != "" {
			return networkModeProvider{prefix: "container", target: id}, true
		}
		return networkModeProvider{}, false
	}

	// No API value (older payloads, tests): fall back to the compose label.
	label := c.Labels["com.docker.compose.network_mode"]
	if label == "" {
		return networkModeProvider{}, false
	}
	prefix, target, ok := strings.Cut(label, ":")
	if !ok || target == "" || (prefix != "service" && prefix != "container") {
		return networkModeProvider{}, false
	}
	return networkModeProvider{prefix: prefix, target: target}, true
}

// containerNetworkModeID extracts the container ID from a Docker
// HostConfig.NetworkMode value of the form "container:<id>".
func containerNetworkModeID(networkMode string) string {
	if !strings.HasPrefix(networkMode, "container:") {
		return ""
	}
	return strings.TrimPrefix(networkMode, "container:")
}

// matches reports whether the given container supplies the network namespace.
// A container: target may be an ID (full or abbreviated) or a container name,
// since the daemon stores the value verbatim and only resolves it at start.
func (p networkModeProvider) matches(candidate docker.Container, project string) bool {
	switch p.prefix {
	case "container":
		return containerIDMatches(candidate.ID, p.target) || candidate.Name == p.target
	case "service":
		return candidate.Labels["com.docker.compose.service"] == p.target &&
			candidate.Labels["com.docker.compose.project"] == project
	default:
		return false
	}
}

// found reports whether any listed container supplies the network namespace.
func (p networkModeProvider) found(containers []docker.Container, project string) bool {
	for _, candidate := range containers {
		if p.matches(candidate, project) {
			return true
		}
	}
	return false
}

// containerIDMatches reports whether two container ID references identify the
// same container, allowing short IDs to match their full ID in either direction.
func containerIDMatches(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

// RestartWithCompose restarts a container using docker compose restart
// This is simpler and faster than RecreateWithCompose when no config changes are needed.
// It stops and starts the same container without creating a new one.
func (r *Recreator) RestartWithCompose(ctx context.Context, container *docker.Container, hostComposeFilePath, containerComposeFilePath string) error {
	if hostComposeFilePath == "" || containerComposeFilePath == "" {
		return fmt.Errorf("no compose file path available for container %s", container.Name)
	}

	// Get service name from compose labels
	serviceName, ok := container.Labels["com.docker.compose.service"]
	if !ok || serviceName == "" {
		return fmt.Errorf("container %s has no com.docker.compose.service label", container.Name)
	}

	hostComposeDir := filepath.Dir(hostComposeFilePath)
	log.Printf("COMPOSE: Restarting service %s using compose file (host: %s, container: %s)",
		serviceName, hostComposeFilePath, containerComposeFilePath)

	// Build the docker compose restart command
	args := []string{
		"compose",
		"--project-directory", hostComposeDir,
		"-f", containerComposeFilePath,
		"restart",
		serviceName,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	log.Printf("COMPOSE: Executing: docker %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose restart failed: %w\nOutput: %s", err, output)
	}

	log.Printf("COMPOSE: Output: %s", output)
	log.Printf("COMPOSE: Successfully restarted service %s", serviceName)

	return nil
}

// StopWithCompose stops a container using docker compose stop
func (r *Recreator) StopWithCompose(ctx context.Context, container *docker.Container, hostComposeFilePath, containerComposeFilePath string) error {
	if hostComposeFilePath == "" || containerComposeFilePath == "" {
		return fmt.Errorf("no compose file path available for container %s", container.Name)
	}

	serviceName, ok := container.Labels["com.docker.compose.service"]
	if !ok || serviceName == "" {
		return fmt.Errorf("container %s has no com.docker.compose.service label", container.Name)
	}

	hostComposeDir := filepath.Dir(hostComposeFilePath)
	log.Printf("COMPOSE: Stopping service %s", serviceName)

	args := []string{
		"compose",
		"--project-directory", hostComposeDir,
		"-f", containerComposeFilePath,
		"stop",
		serviceName,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	log.Printf("COMPOSE: Executing: docker %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose stop failed: %w\nOutput: %s", err, output)
	}

	log.Printf("COMPOSE: Successfully stopped service %s", serviceName)
	return nil
}

// StartWithCompose starts a container using docker compose start
func (r *Recreator) StartWithCompose(ctx context.Context, container *docker.Container, hostComposeFilePath, containerComposeFilePath string) error {
	if hostComposeFilePath == "" || containerComposeFilePath == "" {
		return fmt.Errorf("no compose file path available for container %s", container.Name)
	}

	serviceName, ok := container.Labels["com.docker.compose.service"]
	if !ok || serviceName == "" {
		return fmt.Errorf("container %s has no com.docker.compose.service label", container.Name)
	}

	hostComposeDir := filepath.Dir(hostComposeFilePath)
	log.Printf("COMPOSE: Starting service %s", serviceName)

	args := []string{
		"compose",
		"--project-directory", hostComposeDir,
		"-f", containerComposeFilePath,
		"start",
		serviceName,
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	log.Printf("COMPOSE: Executing: docker %s", strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker compose start failed: %w\nOutput: %s", err, output)
	}

	log.Printf("COMPOSE: Successfully started service %s", serviceName)
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
	log.Printf("COMPOSE: Recreating services %v using compose file (host: %s, container: %s)",
		serviceNames, hostComposeFilePath, containerComposeFilePath)

	// Build the docker compose up command for multiple services
	// Use --project-directory with HOST path so volume mounts resolve correctly for Docker daemon
	// NOTE: For env_file to work, docksmith must have mirror mounts (same dir at host path)
	// Use --force-recreate to avoid Docker volume mount corruption issues
	args := []string{
		"compose",
		"--project-directory", hostComposeDir, // HOST path for volume mount resolution
		"-f", containerComposeFilePath, // CONTAINER path for reading the file
		"up",
		"-d",
		"--force-recreate", // Force clean recreation to avoid volume mount issues
	}
	args = append(args, serviceNames...)

	// Note: Don't set cmd.Dir here - composeDir is a host path that doesn't exist in the container.
	// The --project-directory flag tells Docker Compose where to resolve relative paths.

	log.Printf("COMPOSE: Executing: docker %s", strings.Join(args, " "))

	// Capture output for logging, retrying while a network_mode provider restarts
	output, err := r.runComposeWithRetry(ctx, args)
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
