package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chis/docksmith/internal/compose"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/scripts"
)

// LabelOptions contains options for the label command
type LabelOptions struct {
	OutputFormat string
	Subcommand   string
	Container    string
	Ignore       *bool
	AllowLatest  *bool
	Script       string
	LabelNames   []string // For remove: list of labels to remove
	NoRestart    bool
	Force        bool
}

// LabelOperationResult represents the result of a label set/remove operation
type LabelOperationResult struct {
	Success         bool              `json:"success"`
	Container       string            `json:"container"`
	Operation       string            `json:"operation"` // "set" or "remove"
	LabelsModified  map[string]string `json:"labels_modified,omitempty"`
	LabelsRemoved   []string          `json:"labels_removed,omitempty"`
	ComposeFile     string            `json:"compose_file"`
	Restarted       bool              `json:"restarted"`
	PreCheckRan     bool              `json:"pre_check_ran"`
	PreCheckPassed  bool              `json:"pre_check_passed,omitempty"`
	Message         string            `json:"message,omitempty"`
}

// LabelCommand implements the label command
type LabelCommand struct {
	options       LabelOptions
	dockerService docker.Client
}

// NewLabelCommand creates a new label command
func NewLabelCommand() *LabelCommand {
	return &LabelCommand{
		options: LabelOptions{
			OutputFormat: "table",
		},
	}
}

// PrintUsage prints the command usage
func (c *LabelCommand) PrintUsage() {
	fmt.Println(`Manage container labels for docksmith

Usage:
  docksmith label <subcommand> <container> [flags]

Subcommands:
  get <container>          Show docksmith labels for a container
  set <container> [flags]  Set docksmith labels on a container
  remove <container> [flags]  Remove docksmith labels from a container

Flags for 'set':
  --ignore <true|false>      Set ignore flag (skip update checks)
  --allow-latest <true|false>  Allow :latest tag updates
  --script <path>            Set pre-update check script path
  --no-restart               Don't restart container after updating labels
  --force                    Skip pre-update checks before restarting
  --json                     Output in JSON format

Flags for 'remove':
  --ignore                   Remove ignore flag
  --allow-latest             Remove allow-latest flag
  --script                   Remove pre-update script
  --no-restart               Don't restart container after removing labels
  --force                    Skip pre-update checks before restarting
  --json                     Output in JSON format

Examples:
  docksmith label get nginx                     # Show labels for nginx
  docksmith label set nginx --ignore true       # Ignore nginx for updates
  docksmith label set nginx --script backup.sh  # Set pre-update script
  docksmith label remove nginx --ignore         # Remove ignore flag
  docksmith label set nginx --ignore false --no-restart  # Set without restart`)
}

// ParseFlags parses command-line flags for the label command
func (c *LabelCommand) ParseFlags(args []string) error {
	// Handle help
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		c.PrintUsage()
		return fmt.Errorf("") // Empty error to signal we showed help
	}

	c.options.Subcommand = args[0]

	// For all subcommands, container name is the first positional argument
	if len(args) < 2 {
		c.PrintUsage()
		return fmt.Errorf("missing container name")
	}
	c.options.Container = args[1]

	// Create flag set for parsing options
	fs := flag.NewFlagSet("label", flag.ContinueOnError)
	var jsonFlag bool
	var ignoreFlag, allowLatestFlag string
	var ignoreFlagSet, allowLatestFlagSet, scriptFlagSet bool

	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	fs.BoolVar(&c.options.NoRestart, "no-restart", false, "Don't restart container after updating labels")
	fs.BoolVar(&c.options.Force, "force", false, "Skip pre-update checks before restarting container")

	// For set command, flags take values
	if c.options.Subcommand == "set" {
		fs.StringVar(&ignoreFlag, "ignore", "", "Set ignore flag (true/false)")
		fs.StringVar(&allowLatestFlag, "allow-latest", "", "Set allow-latest flag (true/false)")
		fs.StringVar(&c.options.Script, "script", "", "Set pre-update script path")
	} else if c.options.Subcommand == "remove" {
		// For remove command, flags are boolean (presence indicates removal)
		fs.BoolVar(&ignoreFlagSet, "ignore", false, "Remove ignore flag")
		fs.BoolVar(&allowLatestFlagSet, "allow-latest", false, "Remove allow-latest flag")
		fs.BoolVar(&scriptFlagSet, "script", false, "Remove pre-update script")
	}

	// Parse flags starting from args[2:] (after subcommand and container)
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}

	// Get any remaining positional arguments (for custom label names)
	remainingArgs := fs.Args()

	// Process flags based on subcommand
	if c.options.Subcommand == "set" {
		// Parse ignore flag
		if ignoreFlag != "" {
			val := strings.ToLower(ignoreFlag) == "true" || ignoreFlag == "1"
			c.options.Ignore = &val
		}

		// Parse allow-latest flag
		if allowLatestFlag != "" {
			val := strings.ToLower(allowLatestFlag) == "true" || allowLatestFlag == "1"
			c.options.AllowLatest = &val
		}

		// Also support key=value format for custom labels in remaining args
		// (for future enhancement)

	} else if c.options.Subcommand == "remove" {
		// For remove, collect all flags that were set
		if ignoreFlagSet {
			c.options.LabelNames = append(c.options.LabelNames, scripts.IgnoreLabel)
		}
		if allowLatestFlagSet {
			c.options.LabelNames = append(c.options.LabelNames, scripts.AllowLatestLabel)
		}
		if scriptFlagSet {
			c.options.LabelNames = append(c.options.LabelNames, scripts.PreUpdateCheckLabel)
		}

		// Also support explicit label names as positional arguments
		for _, arg := range remainingArgs {
			c.options.LabelNames = append(c.options.LabelNames, arg)
		}
	}

	// Set JSON mode
	if GlobalJSONMode || jsonFlag {
		c.options.OutputFormat = "json"
	}

	return nil
}

// Run executes the label command
func (c *LabelCommand) Run(ctx context.Context) error {
	// Initialize Docker service
	var err error
	c.dockerService, err = docker.NewService()
	if err != nil {
		return fmt.Errorf("failed to create Docker service: %w", err)
	}
	defer c.dockerService.Close()

	switch c.options.Subcommand {
	case "get":
		return c.getLabels(ctx)
	case "set":
		return c.setLabels(ctx)
	case "remove":
		return c.removeLabel(ctx)
	default:
		return fmt.Errorf("unknown subcommand: %s (use get, set, or remove)", c.options.Subcommand)
	}
}

// getLabels displays current docksmith labels for a container
func (c *LabelCommand) getLabels(ctx context.Context) error {
	container, err := c.findContainer(ctx)
	if err != nil {
		return err
	}

	docksmithLabels := make(map[string]string)
	for key, value := range container.Labels {
		if strings.HasPrefix(key, "docksmith.") {
			docksmithLabels[key] = value
		}
	}

	if c.options.OutputFormat == "json" {
		return output.WriteJSONData(os.Stdout, map[string]interface{}{
			"container": container.Name,
			"labels":    docksmithLabels,
		})
	}

	// Table output
	{
		fmt.Printf("Container: %s\n", container.Name)
		fmt.Println("\nDocksmith Labels:")
		if len(docksmithLabels) == 0 {
			fmt.Println("  (none)")
		} else {
			for key, value := range docksmithLabels {
				fmt.Printf("  %s = %s\n", key, value)
			}
		}
	}

	return nil
}

// setLabels updates labels in the compose file and restarts the container
func (c *LabelCommand) setLabels(ctx context.Context) error {
	// Validate that at least one label is being set
	if c.options.Ignore == nil && c.options.AllowLatest == nil && c.options.Script == "" {
		return fmt.Errorf("no labels specified (use --ignore, --allow-latest, or --script)")
	}

	container, err := c.findContainer(ctx)
	if err != nil {
		return err
	}

	// Get compose file path
	composeFilePath, serviceName, err := c.getComposeFileInfo(container)
	if err != nil {
		return err
	}

	if composeFilePath == "" {
		return fmt.Errorf("container %s is not managed by docker compose", container.Name)
	}

	// Initialize result for JSON output
	result := &LabelOperationResult{
		Success:        false,
		Container:      container.Name,
		Operation:      "set",
		LabelsModified: make(map[string]string),
		ComposeFile:    composeFilePath,
		Restarted:      false,
		PreCheckRan:    false,
	}

	isJSON := c.options.OutputFormat == "json"

	if !isJSON {
		fmt.Printf("Updating labels for container: %s\n", container.Name)
		fmt.Printf("Compose file: %s\n", composeFilePath)
		fmt.Printf("Service: %s\n\n", serviceName)
	}

	// Run pre-update check before modifying anything (unless --force or --no-restart)
	if !c.options.NoRestart && !c.options.Force {
		result.PreCheckRan = true
		if err := c.runPreUpdateCheck(ctx, container); err != nil {
			return fmt.Errorf("pre-update check failed: %w (use --force to skip)", err)
		}
		result.PreCheckPassed = true
	} else if c.options.Force && !isJSON {
		fmt.Println("Skipping pre-update check (--force flag set)")
	}

	// Load compose file (handles include-based setups)
	composeFile, err := compose.LoadComposeFileOrIncluded(composeFilePath, serviceName)
	if err != nil {
		return fmt.Errorf("failed to load compose file: %w", err)
	}

	// Find service
	service, err := composeFile.FindServiceByContainerName(serviceName)
	if err != nil {
		return fmt.Errorf("failed to find service: %w", err)
	}

	// Apply label updates
	if c.options.Ignore != nil {
		value := "false"
		if *c.options.Ignore {
			value = "true"
		}
		if err := service.SetLabel(scripts.IgnoreLabel, value); err != nil {
			return fmt.Errorf("failed to set ignore label: %w", err)
		}
		result.LabelsModified[scripts.IgnoreLabel] = value
	}

	if c.options.AllowLatest != nil {
		value := "false"
		if *c.options.AllowLatest {
			value = "true"
		}
		if err := service.SetLabel(scripts.AllowLatestLabel, value); err != nil {
			return fmt.Errorf("failed to set allow-latest label: %w", err)
		}
		result.LabelsModified[scripts.AllowLatestLabel] = value
	}

	if c.options.Script != "" {
		if err := service.SetLabel(scripts.PreUpdateCheckLabel, c.options.Script); err != nil {
			return fmt.Errorf("failed to set script label: %w", err)
		}
		result.LabelsModified[scripts.PreUpdateCheckLabel] = c.options.Script
	}

	// Save compose file
	if err := composeFile.Save(); err != nil {
		return fmt.Errorf("failed to save compose file: %w", err)
	}
	if !isJSON {
		fmt.Println("Updated compose file")
	}

	// Restart container to apply labels (unless --no-restart is set)
	if !c.options.NoRestart {
		if !isJSON {
			fmt.Println("\nRestarting container to apply labels...")
		}
		if err := c.restartContainer(ctx, composeFilePath, serviceName); err != nil {
			return fmt.Errorf("failed to restart container: %w", err)
		}
		result.Restarted = true
		if !isJSON {
			fmt.Println("Container restarted successfully")
		}

		// Verify labels were applied
		if !isJSON {
			fmt.Println("\nVerifying labels...")
		}
		if err := c.verifyLabels(ctx, container.Name); err != nil {
			if !isJSON {
				log.Printf("Warning: %v", err)
			}
		} else if !isJSON {
			fmt.Println("Labels verified successfully")
		}
	} else if !isJSON {
		fmt.Println("\nSkipped container restart (--no-restart flag set)")
		fmt.Println("Labels will be applied on next container restart")
	}

	// Clean up backup

	result.Success = true
	result.Message = fmt.Sprintf("%d label(s) set successfully", len(result.LabelsModified))

	if isJSON {
		return output.WriteJSONData(os.Stdout, result)
	}

	fmt.Println("\n✓ Labels updated successfully")
	return nil
}

// removeLabel removes labels from the compose file
func (c *LabelCommand) removeLabel(ctx context.Context) error {
	if len(c.options.LabelNames) == 0 {
		return fmt.Errorf("no labels specified to remove (use --ignore, --allow-latest, --script, or label name)")
	}

	container, err := c.findContainer(ctx)
	if err != nil {
		return err
	}

	composeFilePath, serviceName, err := c.getComposeFileInfo(container)
	if err != nil {
		return err
	}

	if composeFilePath == "" {
		return fmt.Errorf("container %s is not managed by docker compose", container.Name)
	}

	// Initialize result for JSON output
	result := &LabelOperationResult{
		Success:       false,
		Container:     container.Name,
		Operation:     "remove",
		LabelsRemoved: c.options.LabelNames,
		ComposeFile:   composeFilePath,
		Restarted:     false,
		PreCheckRan:   false,
	}

	isJSON := c.options.OutputFormat == "json"

	if !isJSON {
		if len(c.options.LabelNames) == 1 {
			fmt.Printf("Removing label '%s' from container: %s\n", c.options.LabelNames[0], container.Name)
		} else {
			fmt.Printf("Removing %d labels from container: %s\n", len(c.options.LabelNames), container.Name)
			for _, label := range c.options.LabelNames {
				fmt.Printf("  - %s\n", label)
			}
		}
	}

	// Run pre-update check before modifying anything (unless --force or --no-restart)
	if !c.options.NoRestart && !c.options.Force {
		result.PreCheckRan = true
		if err := c.runPreUpdateCheck(ctx, container); err != nil {
			return fmt.Errorf("pre-update check failed: %w (use --force to skip)", err)
		}
		result.PreCheckPassed = true
	} else if c.options.Force && !isJSON {
		fmt.Println("Skipping pre-update check (--force flag set)")
	}

	// Load compose file (handles include-based setups)
	composeFile, err := compose.LoadComposeFileOrIncluded(composeFilePath, serviceName)
	if err != nil {
		return fmt.Errorf("failed to load compose file: %w", err)
	}

	// Find service
	service, err := composeFile.FindServiceByContainerName(serviceName)
	if err != nil {
		return fmt.Errorf("failed to find service: %w", err)
	}

	// Remove all specified labels
	for _, labelName := range c.options.LabelNames {
		if err := service.RemoveLabel(labelName); err != nil {
			return fmt.Errorf("failed to remove label %s: %w", labelName, err)
		}
	}

	// Save compose file
	if err := composeFile.Save(); err != nil {
		return fmt.Errorf("failed to save compose file: %w", err)
	}

	// Restart container
	if !c.options.NoRestart {
		if err := c.restartContainer(ctx, composeFilePath, serviceName); err != nil {
			return fmt.Errorf("failed to restart container: %w", err)
		}
		result.Restarted = true
	}

	result.Success = true
	result.Message = fmt.Sprintf("%d label(s) removed successfully", len(c.options.LabelNames))

	if isJSON {
		return output.WriteJSONData(os.Stdout, result)
	}

	if len(c.options.LabelNames) == 1 {
		fmt.Println("\n✓ Label removed successfully")
	} else {
		fmt.Printf("\n✓ %d labels removed successfully\n", len(c.options.LabelNames))
	}
	return nil
}

// findContainer finds a container by name
func (c *LabelCommand) findContainer(ctx context.Context) (*docker.Container, error) {
	containers, err := c.dockerService.ListContainers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	for _, container := range containers {
		if container.Name == c.options.Container {
			return &container, nil
		}
	}

	return nil, fmt.Errorf("container not found: %s", c.options.Container)
}

// getComposeFileInfo extracts compose file path and service name from container
func (c *LabelCommand) getComposeFileInfo(container *docker.Container) (string, string, error) {
	configFiles, ok := container.Labels["com.docker.compose.project.config_files"]
	if !ok || configFiles == "" {
		return "", "", nil
	}

	paths := strings.Split(configFiles, ",")
	if len(paths) == 0 {
		return "", "", nil
	}

	composeFilePath := strings.TrimSpace(paths[0])

	serviceName, ok := container.Labels["com.docker.compose.service"]
	if !ok || serviceName == "" {
		return "", "", fmt.Errorf("container has compose file but no service label")
	}

	return composeFilePath, serviceName, nil
}

// restartContainer restarts a container using docker compose
func (c *LabelCommand) restartContainer(ctx context.Context, composeFilePath string, serviceName string) error {
	composeDir := filepath.Dir(composeFilePath)
	composeFile := filepath.Base(composeFilePath)

	cmd := exec.CommandContext(ctx, "docker", "compose", "-f", composeFile, "up", "-d", "--force-recreate", serviceName)
	cmd.Dir = composeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

// verifyLabels verifies that labels were applied to the container
func (c *LabelCommand) verifyLabels(ctx context.Context, containerName string) error {
	containers, err := c.dockerService.ListContainers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var container *docker.Container
	for _, cont := range containers {
		if cont.Name == containerName {
			container = &cont
			break
		}
	}

	if container == nil {
		return fmt.Errorf("container not found after restart")
	}

	// Build expected labels from options
	expectedLabels := make(map[string]string)
	if c.options.Ignore != nil {
		value := "false"
		if *c.options.Ignore {
			value = "true"
		}
		expectedLabels[scripts.IgnoreLabel] = value
	}
	if c.options.AllowLatest != nil {
		value := "false"
		if *c.options.AllowLatest {
			value = "true"
		}
		expectedLabels[scripts.AllowLatestLabel] = value
	}
	if c.options.Script != "" {
		expectedLabels[scripts.PreUpdateCheckLabel] = c.options.Script
	}

	// Verify each expected label
	for key, expectedValue := range expectedLabels {
		actualValue, exists := container.Labels[key]
		if !exists {
			return fmt.Errorf("label %s was not applied", key)
		}
		if actualValue != expectedValue {
			return fmt.Errorf("label %s has incorrect value: expected '%s', got '%s'", key, expectedValue, actualValue)
		}
	}

	return nil
}

// runPreUpdateCheck runs the container's pre-update check if configured
func (c *LabelCommand) runPreUpdateCheck(ctx context.Context, container *docker.Container) error {
	// Check if container has a pre-update check configured
	scriptPath, ok := container.Labels[scripts.PreUpdateCheckLabel]
	if !ok || scriptPath == "" {
		// No pre-update check configured
		return nil
	}

	isJSON := c.options.OutputFormat == "json"

	if !isJSON {
		fmt.Printf("\nRunning pre-update check: %s\n", scriptPath)
	}

	// Use shared implementation with path translation enabled (CLI runs on host)
	err := scripts.ExecutePreUpdateCheck(ctx, container, scriptPath, true)

	if !isJSON {
		if err != nil {
			fmt.Printf("Pre-update check failed: %v\n", err)
		} else {
			fmt.Printf("Pre-update check passed\n")
		}
	}

	return err
}
