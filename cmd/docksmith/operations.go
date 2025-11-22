package main

import (
	"github.com/chis/docksmith/cmd/docksmith/terminal"
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/storage"
)

// OperationsCommand implements the operations command
type OperationsCommand struct {
	limit         int
	filterStatus  string
	filterContainer string
	since         string
	verbose       bool
}

// NewOperationsCommand creates a new operations command
func NewOperationsCommand() *OperationsCommand {
	return &OperationsCommand{
		limit: 50,
	}
}

// ParseFlags parses command-line flags for the operations command
func (c *OperationsCommand) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("operations", flag.ExitOnError)

	var jsonFlag bool
	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format (global flag)")
	fs.IntVar(&c.limit, "limit", c.limit, "Maximum number of operations to show (0 for no limit)")
	fs.StringVar(&c.filterStatus, "status", "", "Filter by status: complete, failed, queued, etc.")
	fs.StringVar(&c.filterContainer, "container", "", "Filter by container name")
	fs.StringVar(&c.since, "since", "", "Show operations since time (e.g., '24h', '7d', '2024-01-01')")
	fs.BoolVar(&c.verbose, "verbose", false, "Show detailed information")
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

// Run executes the operations command
func (c *OperationsCommand) Run(ctx context.Context) error {
	// Initialize storage
	storageService, err := InitializeStorage()
	if err != nil {
		return err
	}
	defer storageService.Close()

	// Parse since time if provided
	var operations []storage.UpdateOperation
	if c.since != "" {
		sinceTime, err := parseSinceTime(c.since)
		if err != nil {
			return fmt.Errorf("invalid --since value: %w", err)
		}
		operations, err = storageService.GetUpdateOperationsByTimeRange(ctx, sinceTime, time.Now())
		if err != nil {
			return fmt.Errorf("failed to query operations: %w", err)
		}
	} else if c.filterContainer != "" {
		operations, err = storageService.GetUpdateOperationsByContainer(ctx, c.filterContainer, c.limit)
		if err != nil {
			return fmt.Errorf("failed to query operations for container: %w", err)
		}
	} else if c.filterStatus != "" {
		operations, err = storageService.GetUpdateOperationsByStatus(ctx, c.filterStatus, c.limit)
		if err != nil {
			return fmt.Errorf("failed to query operations by status: %w", err)
		}
	} else {
		operations, err = storageService.GetUpdateOperations(ctx, c.limit)
		if err != nil {
			return fmt.Errorf("failed to query operations: %w", err)
		}
	}

	// Apply additional filters if needed
	filteredOps := c.filterOperations(operations)

	// Output results
	if GlobalJSONMode {
		return c.outputJSON(filteredOps)
	}
	return c.outputTable(filteredOps)
}

// filterOperations applies additional filtering to operations
func (c *OperationsCommand) filterOperations(operations []storage.UpdateOperation) []storage.UpdateOperation {
	if c.filterContainer == "" && c.filterStatus == "" {
		return operations
	}

	var filtered []storage.UpdateOperation
	for _, op := range operations {
		// Container filter
		if c.filterContainer != "" {
			if !strings.Contains(strings.ToLower(op.ContainerName), strings.ToLower(c.filterContainer)) {
				continue
			}
		}

		// Status filter
		if c.filterStatus != "" {
			if !strings.EqualFold(op.Status, c.filterStatus) {
				continue
			}
		}

		filtered = append(filtered, op)
	}

	return filtered
}

// outputJSON outputs results in JSON format
func (c *OperationsCommand) outputJSON(operations []storage.UpdateOperation) error {
	return output.WriteJSONData(os.Stdout, map[string]interface{}{
		"operations": operations,
		"count":      len(operations),
	})
}

// outputTable outputs results in human-readable table format
func (c *OperationsCommand) outputTable(operations []storage.UpdateOperation) error {
	if len(operations) == 0 {
		fmt.Println("No operations found.")
		return nil
	}

	fmt.Printf("\n=== Update Operations (showing %d) ===\n\n", len(operations))

	// Group by status for better organization
	statusGroups := make(map[string][]storage.UpdateOperation)
	for _, op := range operations {
		statusGroups[op.Status] = append(statusGroups[op.Status], op)
	}

	// Sort statuses for consistent output
	statuses := make([]string, 0, len(statusGroups))
	for status := range statusGroups {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)

	// Display by status group
	for _, status := range statuses {
		ops := statusGroups[status]
		fmt.Printf("--- %s (%d) ---\n", strings.ToUpper(status), len(ops))

		for _, op := range ops {
			c.displayOperation(op)
		}
		fmt.Println()
	}

	return nil
}

// displayOperation displays a single operation
func (c *OperationsCommand) displayOperation(op storage.UpdateOperation) {
	// Format time
	var timeStr string
	if op.StartedAt != nil {
		timeStr = op.StartedAt.Format("2006-01-02 15:04:05")
	} else {
		timeStr = op.CreatedAt.Format("2006-01-02 15:04:05")
	}

	// Status indicator
	statusIcon := "•"
	statusColor := ""
	switch op.Status {
	case storage.StatusComplete:
		statusIcon = "✓"
		statusColor = terminal.Green()
	case storage.StatusFailed:
		statusIcon = "✗"
		statusColor = terminal.Red()
	case storage.StatusQueued, storage.StatusValidating, storage.StatusBackup, storage.StatusPullingImage:
		statusIcon = "→"
		statusColor = terminal.Yellow()
	}

	// Build container info
	containerInfo := op.ContainerName
	if op.StackName != "" {
		containerInfo += fmt.Sprintf(" (stack: %s)", op.StackName)
	}

	// Version info
	versionInfo := ""
	if op.OldVersion != "" && op.NewVersion != "" {
		versionInfo = fmt.Sprintf(" %s → %s", op.OldVersion, op.NewVersion)
	} else if op.NewVersion != "" {
		versionInfo = fmt.Sprintf(" → %s", op.NewVersion)
	}

	// Basic line
	fmt.Printf("  %s%s%s [%s] %s%s\n",
		statusColor, statusIcon, terminal.Reset(),
		timeStr,
		containerInfo,
		versionInfo,
	)

	// Operation ID in verbose mode
	if c.verbose {
		fmt.Printf("    ID: %s\n", op.OperationID)
		fmt.Printf("    Type: %s\n", op.OperationType)
	}

	// Show duration if completed
	if op.StartedAt != nil && op.CompletedAt != nil {
		duration := op.CompletedAt.Sub(*op.StartedAt)
		fmt.Printf("    Duration: %s\n", formatDuration(duration))
	}

	// Show dependents if any
	if len(op.DependentsAffected) > 0 {
		fmt.Printf("    Dependents restarted: %s\n", strings.Join(op.DependentsAffected, ", "))
	}

	// Show error if failed
	if op.Status == "failed" && op.ErrorMessage != "" {
		fmt.Printf("    %sError: %s%s\n", terminal.Red(), op.ErrorMessage, terminal.Reset())
	}

	// Show rollback indicator
	if op.RollbackOccurred {
		fmt.Printf("    %s⚠ Rollback occurred%s\n", terminal.Yellow(), terminal.Reset())
	}
}

// parseSinceTime parses a since time string (e.g., "24h", "7d", "2024-01-01")
func parseSinceTime(since string) (time.Time, error) {
	// Try parsing as duration first (e.g., "24h", "7d")
	if strings.HasSuffix(since, "h") || strings.HasSuffix(since, "m") || strings.HasSuffix(since, "s") {
		duration, err := time.ParseDuration(since)
		if err == nil {
			return time.Now().Add(-duration), nil
		}
	}

	// Try parsing as duration with days (e.g., "7d")
	if strings.HasSuffix(since, "d") {
		daysStr := strings.TrimSuffix(since, "d")
		var days int
		_, err := fmt.Sscanf(daysStr, "%d", &days)
		if err == nil {
			return time.Now().AddDate(0, 0, -days), nil
		}
	}

	// Try parsing as RFC3339 date
	t, err := time.Parse(time.RFC3339, since)
	if err == nil {
		return t, nil
	}

	// Try parsing as simple date (YYYY-MM-DD)
	t, err = time.Parse("2006-01-02", since)
	if err == nil {
		return t, nil
	}

	return time.Time{}, fmt.Errorf("unable to parse time: %s (try '24h', '7d', or '2006-01-02')", since)
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
