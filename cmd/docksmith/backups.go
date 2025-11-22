package main

import (
	"github.com/chis/docksmith/cmd/docksmith/terminal"
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/storage"
)

// BackupsCommand implements the backups command
type BackupsCommand struct {
	limit           int
	filterContainer string
	filterStack     string
	verbose         bool
}

// NewBackupsCommand creates a new backups command
func NewBackupsCommand() *BackupsCommand {
	return &BackupsCommand{
		limit: 50,
	}
}

// ParseFlags parses command-line flags for the backups command
func (c *BackupsCommand) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("backups", flag.ExitOnError)

	var jsonFlag bool
	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format (global flag)")
	fs.IntVar(&c.limit, "limit", c.limit, "Maximum number of backups to show (0 for no limit)")
	fs.StringVar(&c.filterContainer, "container", "", "Filter by container name")
	fs.StringVar(&c.filterStack, "stack", "", "Filter by stack name")
	fs.BoolVar(&c.verbose, "verbose", false, "Show detailed information including file paths")
	fs.BoolVar(&c.verbose, "v", false, "Shorthand for --verbose")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Set JSON mode if either global flag or local flag is set
	if GlobalJSONMode || jsonFlag {
		GlobalJSONMode = true
	}

	return nil
}

// Run executes the backups command
func (c *BackupsCommand) Run(ctx context.Context) error {
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

	// Fetch backups
	var backups []storage.ComposeBackup
	if c.filterContainer != "" {
		backups, err = storageService.GetComposeBackupsByContainer(ctx, c.filterContainer)
		if err != nil {
			return fmt.Errorf("failed to query backups for container: %w", err)
		}
	} else {
		backups, err = storageService.GetAllComposeBackups(ctx, c.limit)
		if err != nil {
			return fmt.Errorf("failed to query backups: %w", err)
		}
	}

	// Apply additional filters
	backups = c.filterBackups(backups)

	// Output results
	if GlobalJSONMode {
		return c.outputJSON(backups)
	}
	return c.outputTable(backups)
}

// filterBackups applies additional filtering to backups
func (c *BackupsCommand) filterBackups(backups []storage.ComposeBackup) []storage.ComposeBackup {
	if c.filterStack == "" {
		return backups
	}

	var filtered []storage.ComposeBackup
	for _, backup := range backups {
		if strings.EqualFold(backup.StackName, c.filterStack) {
			filtered = append(filtered, backup)
		}
	}

	return filtered
}

// outputJSON outputs results in JSON format
func (c *BackupsCommand) outputJSON(backups []storage.ComposeBackup) error {
	return output.WriteJSONData(os.Stdout, map[string]interface{}{
		"backups": backups,
		"count":   len(backups),
	})
}

// outputTable outputs results in human-readable table format
func (c *BackupsCommand) outputTable(backups []storage.ComposeBackup) error {
	if len(backups) == 0 {
		fmt.Println("No compose backups found.")
		return nil
	}

	fmt.Printf("\n=== Compose File Backups (showing %d) ===\n\n", len(backups))

	// Group by stack for better organization
	stackGroups := make(map[string][]storage.ComposeBackup)
	standaloneBackups := []storage.ComposeBackup{}

	for _, backup := range backups {
		if backup.StackName != "" {
			stackGroups[backup.StackName] = append(stackGroups[backup.StackName], backup)
		} else {
			standaloneBackups = append(standaloneBackups, backup)
		}
	}

	// Display stacks
	if len(stackGroups) > 0 {
		fmt.Println("--- Stacks ---")
		for stackName, stackBackups := range stackGroups {
			fmt.Printf("\n%s (%d backups):\n", stackName, len(stackBackups))
			for _, backup := range stackBackups {
				c.displayBackup(backup)
			}
		}
		fmt.Println()
	}

	// Display standalone
	if len(standaloneBackups) > 0 {
		fmt.Println("--- Standalone Containers ---")
		for _, backup := range standaloneBackups {
			c.displayBackup(backup)
		}
		fmt.Println()
	}

	fmt.Println("💡 Tip: Use 'docksmith rollback <operation-id>' to restore a backup")
	return nil
}

// displayBackup displays a single backup entry
func (c *BackupsCommand) displayBackup(backup storage.ComposeBackup) {
	timeStr := backup.BackupTimestamp.Format("2006-01-02 15:04:05")

	// Basic info
	fmt.Printf("  • [%s] %s\n", timeStr, backup.ContainerName)

	// Operation ID (for rollback)
	fmt.Printf("    Operation ID: %s\n", backup.OperationID)

	// Verbose details
	if c.verbose {
		fmt.Printf("    Compose file: %s\n", backup.ComposeFilePath)
		fmt.Printf("    Backup file: %s\n", backup.BackupFilePath)

		// Check if backup file exists
		if _, err := os.Stat(backup.BackupFilePath); err == nil {
			fmt.Printf("    Status: %s✓ File exists%s\n", terminal.Green(), terminal.Reset())
		} else {
			fmt.Printf("    Status: %s✗ File not found%s\n", terminal.Red(), terminal.Reset())
		}
	}
}
