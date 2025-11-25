package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/chis/docksmith/cmd/docksmith/terminal"
	"github.com/chis/docksmith/internal/bootstrap"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/update"
)

// UpdateCommand represents the update command
type UpdateCommand struct {
	flagSet   *flag.FlagSet
	version   string
	timeout   time.Duration
	json      bool
	verbose   bool
	all       bool
	stack     string
	dryRun    bool
}

// NewUpdateCommand creates a new update command
func NewUpdateCommand() *UpdateCommand {
	cmd := &UpdateCommand{
		flagSet: flag.NewFlagSet("update", flag.ContinueOnError),
	}

	cmd.flagSet.StringVar(&cmd.version, "version", "", "Target version to update to (default: latest available)")
	cmd.flagSet.StringVar(&cmd.version, "v", "", "Target version (shorthand)")
	cmd.flagSet.DurationVar(&cmd.timeout, "timeout", 10*time.Minute, "Timeout for update operation")
	cmd.flagSet.BoolVar(&cmd.json, "json", false, "Output in JSON format")
	cmd.flagSet.BoolVar(&cmd.verbose, "verbose", false, "Show detailed progress")
	cmd.flagSet.BoolVar(&cmd.all, "all", false, "Update all containers with available updates")
	cmd.flagSet.StringVar(&cmd.stack, "stack", "", "Update all containers in specified stack")
	cmd.flagSet.BoolVar(&cmd.dryRun, "dry-run", false, "Show what would be updated without making changes")

	return cmd
}

// ParseFlags parses the command flags
func (c *UpdateCommand) ParseFlags(args []string) error {
	// Check for help flag before parsing
	for _, arg := range args {
		if arg == "-h" || arg == "--help" || arg == "help" {
			c.PrintUsage()
			return fmt.Errorf("") // Empty error to signal we showed help
		}
	}

	if err := c.flagSet.Parse(args); err != nil {
		return err
	}

	// Check for global JSON mode
	if GlobalJSONMode {
		c.json = true
	}

	return nil
}

// PrintUsage prints the command usage
func (c *UpdateCommand) PrintUsage() {
	fmt.Println(`Update containers to newer versions

Usage:
  docksmith update <container-name> [flags]
  docksmith update --all [flags]
  docksmith update --stack <stack-name> [flags]

Arguments:
  container-name    Name of the container to update

Flags:
  -v, --version <version>   Target version to update to (default: latest available)
  --timeout <duration>      Timeout for update operation (default: 10m)
  --all                     Update all containers with available updates
  --stack <name>            Update all containers in the specified stack
  --dry-run                 Show what would be updated without making changes
  --verbose                 Show detailed progress information
  --json                    Output in JSON format

Examples:
  docksmith update nginx                    # Update nginx to latest version
  docksmith update nginx --version 1.25.0   # Update nginx to specific version
  docksmith update --all                    # Update all containers
  docksmith update --stack myapp            # Update all containers in 'myapp' stack
  docksmith update nginx --dry-run          # Preview update without executing
  docksmith update nginx --json             # Output results as JSON`)
}

// Run executes the update command
func (c *UpdateCommand) Run(ctx context.Context) error {
	args := c.flagSet.Args()

	// Validate arguments
	if !c.all && c.stack == "" && len(args) == 0 {
		c.PrintUsage()
		return fmt.Errorf("container name required (or use --all or --stack)")
	}

	// Initialize services
	deps, cleanup, err := bootstrap.InitializeServices(bootstrap.InitOptions{
		DefaultDBPath: DefaultDBPath,
		Verbose:       c.verbose,
	})
	if err != nil {
		return err
	}
	defer cleanup()

	// Handle --all flag
	if c.all {
		return c.updateAll(ctx, deps)
	}

	// Handle --stack flag
	if c.stack != "" {
		return c.updateStack(ctx, deps, c.stack)
	}

	// Update single container
	containerName := args[0]
	return c.updateContainer(ctx, deps, containerName)
}

// updateContainer updates a single container
func (c *UpdateCommand) updateContainer(ctx context.Context, deps *bootstrap.ServiceDependencies, containerName string) error {
	targetVersion := c.version

	if !c.json && c.verbose {
		fmt.Printf("=== Starting update for container: %s ===\n", containerName)
		if targetVersion != "" {
			fmt.Printf("Target version: %s\n", targetVersion)
		} else {
			fmt.Printf("Target version: latest (will be resolved)\n")
		}
	}

	// Create update orchestrator
	orchestrator := update.NewUpdateOrchestrator(
		deps.Docker,
		deps.Docker.GetClient(),
		deps.Storage,
		deps.EventBus,
		deps.Registry,
		deps.Docker.GetPathTranslator(),
	)

	// If no target version specified, check for latest
	if targetVersion == "" {
		checker := update.NewChecker(deps.Docker, deps.Registry, deps.Storage)
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()

		result, err := checker.CheckForUpdates(checkCtx)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		// Find the container
		var found bool
		for _, upd := range result.Updates {
			if upd.ContainerName == containerName {
				found = true
				if upd.Status != update.UpdateAvailable && upd.Status != update.UpdateAvailableBlocked {
					if c.json {
						return output.WriteJSONData(os.Stdout, map[string]interface{}{
							"success":         true,
							"container_name":  containerName,
							"current_version": upd.CurrentVersion,
							"status":          "up_to_date",
							"message":         "Container is already up to date",
						})
					}
					fmt.Printf("%s✓ Container '%s' is already up to date%s\n", terminal.Green(), containerName, terminal.Reset())
					if upd.CurrentVersion != "" {
						fmt.Printf("  Current version: %s\n", upd.CurrentVersion)
					}
					return nil
				}
				targetVersion = upd.LatestVersion
				if !c.json && c.verbose {
					fmt.Printf("✓ Found update available\n")
					fmt.Printf("  Current version: %s\n", upd.CurrentVersion)
					fmt.Printf("  Latest version:  %s\n", targetVersion)
				}
				break
			}
		}

		if !found {
			return fmt.Errorf("container '%s' not found", containerName)
		}

		if targetVersion == "" {
			return fmt.Errorf("no update available for container '%s'", containerName)
		}
	}

	// Dry run mode
	if c.dryRun {
		if c.json {
			return output.WriteJSONData(os.Stdout, map[string]interface{}{
				"dry_run":        true,
				"container_name": containerName,
				"target_version": targetVersion,
				"message":        "Would update container to specified version",
			})
		}
		fmt.Printf("%s[DRY RUN]%s Would update %s to version %s\n", terminal.Yellow(), terminal.Reset(), containerName, targetVersion)
		return nil
	}

	// Start the update
	if !c.json {
		fmt.Printf("Updating %s to %s...\n", containerName, targetVersion)
	}

	operationID, err := orchestrator.UpdateSingleContainer(ctx, containerName, targetVersion)
	if err != nil {
		return fmt.Errorf("failed to start update: %w", err)
	}

	if deps.Storage == nil {
		if c.json {
			return output.WriteJSONData(os.Stdout, map[string]interface{}{
				"success":        true,
				"operation_id":   operationID,
				"container_name": containerName,
				"target_version": targetVersion,
				"message":        "Update started (storage unavailable - cannot track progress)",
			})
		}
		fmt.Printf("⚠ Storage unavailable - cannot track detailed progress\n")
		fmt.Printf("  Operation ID: %s\n", operationID)
		fmt.Printf("  Monitor with: docker logs -f %s\n", containerName)
		return nil
	}

	// Monitor progress
	return c.monitorOperation(ctx, deps, operationID, containerName, targetVersion)
}

// monitorOperation monitors an update operation until completion
func (c *UpdateCommand) monitorOperation(ctx context.Context, deps *bootstrap.ServiceDependencies, operationID, containerName, targetVersion string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(c.timeout)
	lastStatus := ""

	for {
		select {
		case <-timeout:
			if c.json {
				return output.WriteJSONData(os.Stdout, map[string]interface{}{
					"success":        false,
					"operation_id":   operationID,
					"container_name": containerName,
					"error":          "Update timed out",
				})
			}
			return fmt.Errorf("update timed out after %v", c.timeout)

		case <-ticker.C:
			op, found, err := deps.Storage.GetUpdateOperation(ctx, operationID)
			if err != nil {
				if c.verbose && !c.json {
					fmt.Printf("⚠ Error checking status: %v\n", err)
				}
				continue
			}

			if !found {
				continue
			}

			// Show progress updates
			if op.Status != lastStatus && c.verbose && !c.json {
				fmt.Printf("→ Status: %s\n", op.Status)
				lastStatus = op.Status
			}

			// Check if complete
			if op.Status == "completed" || op.Status == "complete" {
				if c.json {
					return output.WriteJSONData(os.Stdout, map[string]interface{}{
						"success":              true,
						"operation_id":         operationID,
						"container_name":       containerName,
						"old_version":          op.OldVersion,
						"new_version":          op.NewVersion,
						"dependents_restarted": op.DependentsAffected,
						"completed_at":         op.CompletedAt,
					})
				}

				fmt.Printf("%s✓ Update completed successfully%s\n", terminal.Green(), terminal.Reset())
				fmt.Printf("  Container: %s\n", containerName)
				if op.OldVersion != "" {
					fmt.Printf("  %s → %s\n", op.OldVersion, op.NewVersion)
				}
				if len(op.DependentsAffected) > 0 {
					fmt.Printf("  Dependents restarted: %v\n", op.DependentsAffected)
				}
				return nil
			}

			if op.Status == "failed" {
				if c.json {
					return output.WriteJSONData(os.Stdout, map[string]interface{}{
						"success":        false,
						"operation_id":   operationID,
						"container_name": containerName,
						"error":          op.ErrorMessage,
						"status":         op.Status,
					})
				}

				fmt.Printf("%s✗ Update failed%s\n", terminal.Red(), terminal.Reset())
				fmt.Printf("  Error: %s\n", op.ErrorMessage)
				return fmt.Errorf("update failed: %s", op.ErrorMessage)
			}
		}
	}
}

// updateAll updates all containers with available updates
func (c *UpdateCommand) updateAll(ctx context.Context, deps *bootstrap.ServiceDependencies) error {
	if !c.json {
		fmt.Println("Checking for available updates...")
	}

	checker := update.NewChecker(deps.Docker, deps.Registry, deps.Storage)
	result, err := checker.CheckForUpdates(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	// Find containers with updates
	var updatable []update.ContainerUpdate
	for _, upd := range result.Updates {
		if upd.Status == update.UpdateAvailable {
			updatable = append(updatable, upd)
		}
	}

	if len(updatable) == 0 {
		if c.json {
			return output.WriteJSONData(os.Stdout, map[string]interface{}{
				"success":       true,
				"updates_found": 0,
				"message":       "All containers are up to date",
			})
		}
		fmt.Printf("%s✓ All containers are up to date%s\n", terminal.Green(), terminal.Reset())
		return nil
	}

	if c.dryRun {
		if c.json {
			var updates []map[string]string
			for _, upd := range updatable {
				updates = append(updates, map[string]string{
					"container":       upd.ContainerName,
					"current_version": upd.CurrentVersion,
					"target_version":  upd.LatestVersion,
				})
			}
			return output.WriteJSONData(os.Stdout, map[string]interface{}{
				"dry_run":       true,
				"updates_found": len(updatable),
				"updates":       updates,
			})
		}
		fmt.Printf("%s[DRY RUN]%s Would update %d container(s):\n", terminal.Yellow(), terminal.Reset(), len(updatable))
		for _, upd := range updatable {
			fmt.Printf("  • %s: %s → %s\n", upd.ContainerName, upd.CurrentVersion, upd.LatestVersion)
		}
		return nil
	}

	if !c.json {
		fmt.Printf("Updating %d container(s)...\n", len(updatable))
	}

	// Update each container
	var succeeded, failed int
	var results []map[string]interface{}

	for _, upd := range updatable {
		if !c.json {
			fmt.Printf("\nUpdating %s...\n", upd.ContainerName)
		}

		err := c.updateContainer(ctx, deps, upd.ContainerName)
		result := map[string]interface{}{
			"container": upd.ContainerName,
		}

		if err != nil {
			failed++
			result["success"] = false
			result["error"] = err.Error()
		} else {
			succeeded++
			result["success"] = true
		}
		results = append(results, result)
	}

	if c.json {
		return output.WriteJSONData(os.Stdout, map[string]interface{}{
			"success":   failed == 0,
			"total":     len(updatable),
			"succeeded": succeeded,
			"failed":    failed,
			"results":   results,
		})
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Total: %d, Succeeded: %d, Failed: %d\n", len(updatable), succeeded, failed)

	return nil
}

// updateStack updates all containers in a specific stack
func (c *UpdateCommand) updateStack(ctx context.Context, deps *bootstrap.ServiceDependencies, stackName string) error {
	if !c.json {
		fmt.Printf("Checking for updates in stack '%s'...\n", stackName)
	}

	// Use Orchestrator to get stack info (ContainerInfo has Stack field)
	orchestrator := update.NewOrchestrator(deps.Docker, deps.Registry)
	if deps.Storage != nil {
		orchestrator.SetStorage(deps.Storage)
	}

	result, err := orchestrator.DiscoverAndCheck(ctx)
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	// Find containers in stack with updates
	var updatable []update.ContainerInfo
	for _, container := range result.Containers {
		if container.Stack == stackName && container.Status == update.UpdateAvailable {
			updatable = append(updatable, container)
		}
	}

	if len(updatable) == 0 {
		if c.json {
			return output.WriteJSONData(os.Stdout, map[string]interface{}{
				"success":       true,
				"stack":         stackName,
				"updates_found": 0,
				"message":       "All containers in stack are up to date",
			})
		}
		fmt.Printf("%s✓ All containers in stack '%s' are up to date%s\n", terminal.Green(), stackName, terminal.Reset())
		return nil
	}

	if c.dryRun {
		if c.json {
			var updates []map[string]string
			for _, upd := range updatable {
				updates = append(updates, map[string]string{
					"container":       upd.ContainerName,
					"current_version": upd.CurrentVersion,
					"target_version":  upd.LatestVersion,
				})
			}
			return output.WriteJSONData(os.Stdout, map[string]interface{}{
				"dry_run":       true,
				"stack":         stackName,
				"updates_found": len(updatable),
				"updates":       updates,
			})
		}
		fmt.Printf("%s[DRY RUN]%s Would update %d container(s) in stack '%s':\n", terminal.Yellow(), terminal.Reset(), len(updatable), stackName)
		for _, upd := range updatable {
			fmt.Printf("  • %s: %s → %s\n", upd.ContainerName, upd.CurrentVersion, upd.LatestVersion)
		}
		return nil
	}

	if !c.json {
		fmt.Printf("Updating %d container(s) in stack '%s'...\n", len(updatable), stackName)
	}

	// Update each container
	var succeeded, failed int
	var results []map[string]interface{}

	for _, upd := range updatable {
		if !c.json {
			fmt.Printf("\nUpdating %s...\n", upd.ContainerName)
		}

		err := c.updateContainer(ctx, deps, upd.ContainerName)
		result := map[string]interface{}{
			"container": upd.ContainerName,
		}

		if err != nil {
			failed++
			result["success"] = false
			result["error"] = err.Error()
		} else {
			succeeded++
			result["success"] = true
		}
		results = append(results, result)
	}

	if c.json {
		return output.WriteJSONData(os.Stdout, map[string]interface{}{
			"success":   failed == 0,
			"stack":     stackName,
			"total":     len(updatable),
			"succeeded": succeeded,
			"failed":    failed,
			"results":   results,
		})
	}

	fmt.Printf("\n=== Summary ===\n")
	fmt.Printf("Stack: %s\n", stackName)
	fmt.Printf("Total: %d, Succeeded: %d, Failed: %d\n", len(updatable), succeeded, failed)

	return nil
}
