package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/chis/docksmith/internal/bootstrap"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/update"
	"golang.org/x/term"
)

// RollbackCommand implements the rollback command
type RollbackCommand struct {
	operationID string
	force       bool
	dryRun      bool
	noRecreate  bool // Just restore compose file without recreating container
}

// NewRollbackCommand creates a new rollback command
func NewRollbackCommand() *RollbackCommand {
	return &RollbackCommand{}
}

// ParseFlags parses command-line flags for the rollback command
func (c *RollbackCommand) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)

	var jsonFlag bool
	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format (global flag)")
	fs.BoolVar(&c.force, "force", false, "Force rollback without confirmation")
	fs.BoolVar(&c.dryRun, "dry-run", false, "Show what would be done without actually rolling back")
	fs.BoolVar(&c.noRecreate, "no-recreate", false, "Only restore compose file, don't recreate container")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Set JSON mode if either global flag or local flag is set
	if GlobalJSONMode || jsonFlag {
		GlobalJSONMode = true
	}

	// Get operation ID from remaining args
	if len(fs.Args()) == 0 {
		return fmt.Errorf("operation ID required\n\nUsage: docksmith rollback <operation-id> [flags]\n\nUse 'docksmith backups' to list available backups")
	}

	c.operationID = fs.Args()[0]
	return nil
}

// Run executes the rollback command
func (c *RollbackCommand) Run(ctx context.Context) error {
	// Initialize storage
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/docksmith.db"
	}

	storageService, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer storageService.Close()

	// Get the operation to rollback
	operation, found, err := storageService.GetUpdateOperation(ctx, c.operationID)
	if err != nil {
		return fmt.Errorf("failed to query operation: %w", err)
	}
	if !found {
		return fmt.Errorf("operation not found: %s", c.operationID)
	}

	// Get the compose backup for this operation
	backup, found, err := storageService.GetComposeBackup(ctx, c.operationID)
	if err != nil {
		return fmt.Errorf("failed to query backup: %w", err)
	}
	if !found {
		return fmt.Errorf("no compose backup found for operation: %s\n\nThis operation may not have a backup, or the backup was deleted.", c.operationID)
	}

	// Check if backup file exists
	if _, err := os.Stat(backup.BackupFilePath); os.IsNotExist(err) {
		return fmt.Errorf("backup file not found: %s\n\nThe backup may have been deleted or moved.", backup.BackupFilePath)
	}

	// Display rollback information
	if GlobalJSONMode {
		return c.outputJSON(ctx, operation, backup, c.dryRun, storageService)
	}

	// Show what will be done
	fmt.Println("\n=== Rollback Information ===")
	fmt.Printf("Operation ID: %s\n", operation.OperationID)
	fmt.Printf("Container: %s\n", operation.ContainerName)
	if operation.StackName != "" {
		fmt.Printf("Stack: %s\n", operation.StackName)
	}
	fmt.Printf("Operation type: %s\n", operation.OperationType)
	fmt.Printf("Status: %s\n", operation.Status)
	if operation.OldVersion != "" {
		fmt.Printf("Current version: %s\n", operation.NewVersion)
	}
	if operation.NewVersion != "" {
		fmt.Printf("Target version: %s\n", operation.OldVersion)
	}
	fmt.Printf("\nBackup file: %s\n", backup.BackupFilePath)
	fmt.Printf("Original compose: %s\n", backup.ComposeFilePath)
	fmt.Printf("Backup timestamp: %s\n", backup.BackupTimestamp.Format("2006-01-02 15:04:05"))

	if c.dryRun {
		fmt.Printf("\n%s[DRY RUN]%s Would restore compose file and recreate container\n", colorYellow(), colorReset())
		fmt.Println("No changes will be made.")
		return nil
	}

	// Confirm unless force flag is set or stdin is not a terminal (programmatic use)
	isInteractive := term.IsTerminal(int(os.Stdin.Fd()))
	if !c.force && !GlobalJSONMode && isInteractive {
		if c.noRecreate {
			fmt.Printf("\n%sWarning: This will restore the compose file from the backup.%s\n", colorYellow(), colorReset())
			fmt.Println("You will need to manually restart the containers after rollback.")
		} else {
			fmt.Printf("\n%sWarning: This will restore the compose file and recreate the container.%s\n", colorYellow(), colorReset())
			fmt.Println("The container will be stopped, removed, and recreated with the old image.")
		}
		fmt.Print("\nContinue with rollback? (yes/no): ")

		var response string
		fmt.Scanln(&response)
		if response != "yes" && response != "y" {
			fmt.Println("Rollback cancelled.")
			return nil
		}
	} else if !c.force && !GlobalJSONMode && !isInteractive {
		// Non-interactive mode without --force, proceed automatically
		fmt.Printf("\n%sNon-interactive mode: proceeding with rollback automatically%s\n", colorYellow(), colorReset())
	}

	// Perform rollback
	if c.noRecreate {
		// Simple rollback - just restore compose file
		if err := c.performSimpleRollback(backup); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}

		// Log the rollback
		if err := storageService.LogUpdate(ctx, operation.ContainerName, "rollback",
			operation.NewVersion, operation.OldVersion, true, nil); err != nil {
			fmt.Printf("Warning: Failed to log rollback operation: %v\n", err)
		}

		fmt.Printf("\n%s✓ Compose file restored successfully%s\n", colorGreen(), colorReset())
		fmt.Printf("\nCompose file restored to: %s\n", backup.ComposeFilePath)
		fmt.Println("\nNext steps:")
		fmt.Printf("  1. Review the restored compose file\n")
		fmt.Printf("  2. Restart the container:\n")
		if operation.StackName != "" {
			fmt.Printf("     docker compose -f %s up -d %s\n", backup.ComposeFilePath, operation.ContainerName)
		} else {
			fmt.Printf("     docker compose up -d %s\n", operation.ContainerName)
		}
	} else {
		// Full rollback with container recreation
		if err := c.performFullRollback(ctx, operation, storageService); err != nil {
			return fmt.Errorf("rollback failed: %w", err)
		}
	}

	return nil
}

// performSimpleRollback copies the backup file to the original compose file location
func (c *RollbackCommand) performSimpleRollback(backup storage.ComposeBackup) error {
	// Read backup file
	backupContent, err := os.ReadFile(backup.BackupFilePath)
	if err != nil {
		return fmt.Errorf("failed to read backup file: %w", err)
	}

	// Create a backup of the current file before overwriting
	currentContent, err := os.ReadFile(backup.ComposeFilePath)
	if err == nil {
		// Save current file with .before-rollback suffix
		beforeRollbackPath := backup.ComposeFilePath + ".before-rollback"
		if err := os.WriteFile(beforeRollbackPath, currentContent, 0644); err != nil {
			fmt.Printf("Warning: Failed to create pre-rollback backup: %v\n", err)
		} else {
			fmt.Printf("Current compose file backed up to: %s\n", beforeRollbackPath)
		}
	}

	// Write backup content to original location
	if err := os.WriteFile(backup.ComposeFilePath, backupContent, 0644); err != nil {
		return fmt.Errorf("failed to write compose file: %w", err)
	}

	return nil
}

// performFullRollback uses the UpdateOrchestrator for full container recreation
func (c *RollbackCommand) performFullRollback(ctx context.Context, operation storage.UpdateOperation, storageService *storage.SQLiteStorage) error {
	// Initialize services (storage already initialized, passed as parameter)
	deps, cleanup, err := bootstrap.InitializeServices(bootstrap.InitOptions{
		DefaultDBPath: "/data/docksmith.db", // Not used since storage already initialized
		Verbose:       false,
	})
	if err != nil {
		return err
	}
	defer cleanup()

	// Initialize update orchestrator (use passed storage instead of bootstrap's)
	orchestrator := update.NewUpdateOrchestrator(
		deps.Docker,
		deps.Docker.GetClient(),
		storageService,
		deps.EventBus,
		deps.Registry,
		deps.Docker.GetPathTranslator(),
	)

	// Subscribe to progress events for CLI output
	progressChan, unsubscribe := deps.EventBus.Subscribe("*")
	defer unsubscribe()

	fmt.Printf("\n%sStarting full rollback...%s\n", colorYellow(), colorReset())

	// Start the rollback operation
	rollbackOpID, err := orchestrator.RollbackOperation(ctx, c.operationID)
	if err != nil {
		return err
	}

	fmt.Printf("Rollback operation ID: %s\n\n", rollbackOpID)

	// Monitor progress
	timeout := time.After(10 * time.Minute)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastStatus := ""
	for {
		select {
		case event, ok := <-progressChan:
			if !ok {
				return fmt.Errorf("progress channel closed unexpectedly")
			}

			// Extract event data from payload
			stage, _ := event.Payload["stage"].(string)
			progress, _ := event.Payload["progress"].(float64)
			message, _ := event.Payload["message"].(string)

			// Display progress
			if stage != lastStatus {
				lastStatus = stage
				icon := getStageIcon(stage)
				fmt.Printf("%s %s [%d%%] %s\n", icon, stage, int(progress), message)
			}

			// Check if complete or failed
			if stage == "complete" {
				fmt.Printf("\n%s✓ Rollback completed successfully%s\n", colorGreen(), colorReset())
				return nil
			}
			if stage == "failed" {
				return fmt.Errorf("rollback failed: %s", message)
			}

		case <-ticker.C:
			// Check operation status periodically
			op, found, err := storageService.GetUpdateOperation(ctx, rollbackOpID)
			if err != nil {
				log.Printf("Warning: failed to check operation status: %v", err)
				continue
			}
			if !found {
				continue
			}

			if op.Status == "complete" {
				fmt.Printf("\n%s✓ Rollback completed successfully%s\n", colorGreen(), colorReset())
				return nil
			}
			if op.Status == "failed" {
				errMsg := "unknown error"
				if op.ErrorMessage != "" {
					errMsg = op.ErrorMessage
				}
				return fmt.Errorf("rollback failed: %s", errMsg)
			}

		case <-timeout:
			return fmt.Errorf("rollback timed out after 10 minutes")

		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// getStageIcon returns an emoji icon for the rollback stage
func getStageIcon(stage string) string {
	switch stage {
	case "updating_compose":
		return "📝"
	case "validating":
		return "🔍"
	case "pulling_image":
		return "⬇️"
	case "recreating":
		return "🔄"
	case "health_check":
		return "❤️"
	case "complete":
		return "✅"
	case "failed":
		return "❌"
	default:
		return "⏳"
	}
}

// outputJSON outputs rollback information in JSON format
func (c *RollbackCommand) outputJSON(ctx context.Context, operation storage.UpdateOperation, backup storage.ComposeBackup, dryRun bool, storageService *storage.SQLiteStorage) error {
	data := map[string]interface{}{
		"operation":    operation,
		"backup":       backup,
		"dry_run":      dryRun,
		"can_rollback": true,
	}

	// Check if backup file exists
	if _, err := os.Stat(backup.BackupFilePath); os.IsNotExist(err) {
		data["can_rollback"] = false
		data["error"] = fmt.Sprintf("backup file not found: %s", backup.BackupFilePath)
	}

	if dryRun {
		data["message"] = "Dry run - no changes made"
		return output.WriteJSONData(os.Stdout, data)
	}

	// In JSON mode without dry-run, perform the rollback
	if data["can_rollback"].(bool) {
		if c.noRecreate {
			if err := c.performSimpleRollback(backup); err != nil {
				return output.WriteJSONError(os.Stdout, err)
			}
			data["message"] = "Compose file restored successfully"
		} else {
			// Full rollback
			if err := c.performFullRollback(ctx, operation, storageService); err != nil {
				return output.WriteJSONError(os.Stdout, err)
			}
			data["message"] = "Full rollback completed successfully"
		}
	}

	return output.WriteJSONData(os.Stdout, data)
}
