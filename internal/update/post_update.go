package update

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/scripts"
)

// PostUpdateAction represents an action to run after a successful update
type PostUpdateAction struct {
	Type   string   // "restart", "compose-restart", "script", "exec"
	Params []string // Additional parameters (container names, script path, command, etc.)
}

// PostUpdateHandler handles post-update actions
type PostUpdateHandler struct {
	dockerClient docker.Client
}

// NewPostUpdateHandler creates a new post-update handler
func NewPostUpdateHandler(dockerClient docker.Client) *PostUpdateHandler {
	return &PostUpdateHandler{
		dockerClient: dockerClient,
	}
}

// ParsePostUpdateLabel parses the docksmith.post-update label
// Format examples:
//   - restart:torrent
//   - restart:torrent,plex
//   - compose-restart:torrent
//   - script:/scripts/post-update.sh
//   - exec:curl https://example.com/notify
func ParsePostUpdateLabel(label string) (*PostUpdateAction, error) {
	if label == "" {
		return nil, nil
	}

	parts := strings.SplitN(label, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid post-update label format: %s (expected type:params)", label)
	}

	actionType := strings.TrimSpace(parts[0])
	params := strings.TrimSpace(parts[1])

	switch actionType {
	case "restart", "compose-restart":
		// Split comma-separated container/service names
		containerNames := strings.Split(params, ",")
		for i := range containerNames {
			containerNames[i] = strings.TrimSpace(containerNames[i])
		}
		return &PostUpdateAction{
			Type:   actionType,
			Params: containerNames,
		}, nil

	case "script":
		return &PostUpdateAction{
			Type:   "script",
			Params: []string{params},
		}, nil

	case "exec":
		return &PostUpdateAction{
			Type:   "exec",
			Params: []string{params},
		}, nil

	default:
		return nil, fmt.Errorf("unknown post-update action type: %s", actionType)
	}
}

// ExecutePostUpdateActions executes post-update actions for a container
func (h *PostUpdateHandler) ExecutePostUpdateActions(ctx context.Context, container docker.Container, composePath string) error {
	// Check for post-update label
	postUpdateLabel, ok := container.Labels["docksmith.post-update"]
	if !ok {
		return nil // No post-update actions
	}

	action, err := ParsePostUpdateLabel(postUpdateLabel)
	if err != nil {
		return fmt.Errorf("failed to parse post-update label: %w", err)
	}

	if action == nil {
		return nil
	}

	log.Printf("POST-UPDATE: Executing %s action for container %s", action.Type, container.Name)

	switch action.Type {
	case "restart":
		return h.executeRestart(ctx, action.Params)
	case "compose-restart":
		return h.executeComposeRestart(ctx, composePath, action.Params)
	case "script":
		return h.executeScript(ctx, action.Params[0], container)
	case "exec":
		return h.executeCommand(ctx, action.Params[0])
	default:
		return fmt.Errorf("unknown action type: %s", action.Type)
	}
}

// executeRestart restarts one or more containers using docker restart
func (h *PostUpdateHandler) executeRestart(ctx context.Context, containerNames []string) error {
	for _, name := range containerNames {
		log.Printf("POST-UPDATE: Restarting container %s", name)

		// Use docker CLI for restart
		cmd := exec.CommandContext(ctx, "docker", "restart", name)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to restart container %s: %w (output: %s)", name, err, output)
		}

		log.Printf("POST-UPDATE: Successfully restarted %s", name)
	}
	return nil
}

// executeComposeRestart restarts services using docker compose restart
func (h *PostUpdateHandler) executeComposeRestart(ctx context.Context, composePath string, serviceNames []string) error {
	if composePath == "" {
		return fmt.Errorf("compose file path not available for compose-restart")
	}

	composeDir := filepath.Dir(composePath)
	log.Printf("POST-UPDATE: Restarting compose services %v in %s", serviceNames, composeDir)

	args := []string{"compose", "-f", composePath, "restart"}
	args = append(args, serviceNames...)

	cmd := exec.CommandContext(ctx, "docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to compose-restart services %v: %w (output: %s)", serviceNames, err, output)
	}

	log.Printf("POST-UPDATE: Successfully restarted services %v", serviceNames)
	return nil
}

// executeScript executes a custom script
func (h *PostUpdateHandler) executeScript(ctx context.Context, scriptPath string, container docker.Container) error {
	// Construct full path if not absolute
	fullPath := scriptPath
	if !filepath.IsAbs(scriptPath) {
		fullPath = filepath.Join(scripts.ScriptsDir, scriptPath)
	}

	log.Printf("POST-UPDATE: Executing script %s for container %s", fullPath, container.Name)

	// Execute with timeout
	scriptCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(scriptCtx, fullPath, container.ID, container.Name)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("post-update script failed: %w (output: %s)", err, output)
	}

	log.Printf("POST-UPDATE: Script output: %s", output)
	return nil
}

// executeCommand executes an arbitrary command
func (h *PostUpdateHandler) executeCommand(ctx context.Context, command string) error {
	log.Printf("POST-UPDATE: Executing command: %s", command)

	// Execute with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("post-update command failed: %w (output: %s)", err, output)
	}

	log.Printf("POST-UPDATE: Command output: %s", output)
	return nil
}
