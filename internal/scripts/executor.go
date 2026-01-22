package scripts

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/docker"
)

// ExecutePreUpdateCheck runs a pre-update check script with validation and timeout.
// If translatePaths is true, it will translate container paths (e.g., /scripts/xxx.sh)
// to host paths (e.g., $PWD/scripts/xxx.sh) for CLI usage.
func ExecutePreUpdateCheck(ctx context.Context, container *docker.Container, scriptPath string, translatePaths bool) error {
	// Normalize relative paths to absolute paths under /scripts/
	if !filepath.IsAbs(scriptPath) {
		scriptPath = filepath.Join("/scripts", scriptPath)
	}

	// Validate script path
	if !docker.ValidatePreUpdateScript(scriptPath) {
		return fmt.Errorf("invalid pre-update script path: %s", scriptPath)
	}

	// Translate container path to host path if needed (for CLI usage)
	translatedPath := scriptPath
	if translatePaths && strings.HasPrefix(scriptPath, "/scripts/") {
		// Try to find the script in the working directory
		scriptName := strings.TrimPrefix(scriptPath, "/scripts/")

		// Get current working directory
		cwd, err := os.Getwd()
		if err == nil {
			hostPath := filepath.Join(cwd, "scripts", scriptName)
			if _, err := os.Stat(hostPath); err == nil {
				translatedPath = hostPath
			}
		}
	}

	// Execute the check script with timeout
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(checkCtx, translatedPath, container.ID, container.Name)
	output, err := cmd.CombinedOutput()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Non-zero exit code means check failed
			return fmt.Errorf("script exited with code %d: %s", exitErr.ExitCode(), string(output))
		}
		return fmt.Errorf("failed to execute script: %w", err)
	}

	return nil
}
