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

// HistoryEntry represents a unified history entry (check or update)
type HistoryEntry struct {
	Timestamp     time.Time `json:"timestamp"`
	Type          string    `json:"type"` // "check" or "update"
	ContainerName string    `json:"container_name"`
	Image         string    `json:"image,omitempty"`
	CurrentVer    string    `json:"current_version,omitempty"`
	LatestVer     string    `json:"latest_version,omitempty"`
	FromVer       string    `json:"from_version,omitempty"`
	ToVer         string    `json:"to_version,omitempty"`
	Status        string    `json:"status"`
	Operation     string    `json:"operation,omitempty"` // for update entries
	Success       bool      `json:"success,omitempty"`   // for update entries
	Error         string    `json:"error,omitempty"`
}

// HistoryCommand implements the history command
type HistoryCommand struct {
	limit           int
	filterContainer string
	filterType      string // "check" or "update"
	since           string
	verbose         bool
}

// NewHistoryCommand creates a new history command
func NewHistoryCommand() *HistoryCommand {
	return &HistoryCommand{
		limit: 100,
	}
}

// ParseFlags parses command-line flags for the history command
func (c *HistoryCommand) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)

	var jsonFlag bool
	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format (global flag)")
	fs.IntVar(&c.limit, "limit", c.limit, "Maximum number of entries to show per type")
	fs.StringVar(&c.filterContainer, "container", "", "Filter by container name")
	fs.StringVar(&c.filterType, "type", "", "Filter by type: check, update")
	fs.StringVar(&c.since, "since", "", "Show history since time (e.g., '24h', '7d', '2024-01-01')")
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

// Run executes the history command
func (c *HistoryCommand) Run(ctx context.Context) error {
	// Initialize storage
	storageService, err := InitializeStorage()
	if err != nil {
		return err
	}
	defer storageService.Close()

	// Fetch check history and update log
	var checkHistory []storage.CheckHistoryEntry
	var updateLog []storage.UpdateLogEntry

	if c.filterType == "" || c.filterType == "check" {
		if c.since != "" {
			sinceTime, err := parseSinceTime(c.since)
			if err != nil {
				return fmt.Errorf("invalid --since value: %w", err)
			}
			checkHistory, err = storageService.GetCheckHistorySince(ctx, sinceTime)
			if err != nil {
				return fmt.Errorf("failed to query check history: %w", err)
			}
		} else {
			checkHistory, err = storageService.GetAllCheckHistory(ctx, c.limit)
			if err != nil {
				return fmt.Errorf("failed to query check history: %w", err)
			}
		}
	}

	if c.filterType == "" || c.filterType == "update" {
		updateLog, err = storageService.GetAllUpdateLog(ctx, c.limit)
		if err != nil {
			return fmt.Errorf("failed to query update log: %w", err)
		}
	}

	// Convert to unified history entries
	entries := c.mergeHistory(checkHistory, updateLog)

	// Apply filters
	entries = c.filterHistory(entries)

	// Output results
	if GlobalJSONMode {
		return c.outputJSON(entries)
	}
	return c.outputTable(entries)
}

// mergeHistory merges check history and update log into a unified timeline
func (c *HistoryCommand) mergeHistory(checks []storage.CheckHistoryEntry, updates []storage.UpdateLogEntry) []HistoryEntry {
	var entries []HistoryEntry

	// Convert check history
	for _, check := range checks {
		entries = append(entries, HistoryEntry{
			Timestamp:     check.CheckTime,
			Type:          "check",
			ContainerName: check.ContainerName,
			Image:         check.Image,
			CurrentVer:    check.CurrentVersion,
			LatestVer:     check.LatestVersion,
			Status:        check.Status,
			Error:         check.Error,
		})
	}

	// Convert update log
	for _, update := range updates {
		entries = append(entries, HistoryEntry{
			Timestamp:     update.Timestamp,
			Type:          "update",
			ContainerName: update.ContainerName,
			FromVer:       update.FromVersion,
			ToVer:         update.ToVersion,
			Operation:     update.Operation,
			Success:       update.Success,
			Status:        getUpdateStatus(update),
			Error:         update.Error,
		})
	}

	// Sort by timestamp (most recent first)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})

	return entries
}

// getUpdateStatus converts update log success to status string
func getUpdateStatus(update storage.UpdateLogEntry) string {
	if update.Success {
		return "success"
	}
	return "failed"
}

// filterHistory applies filters to history entries
func (c *HistoryCommand) filterHistory(entries []HistoryEntry) []HistoryEntry {
	if c.filterContainer == "" {
		return entries
	}

	var filtered []HistoryEntry
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.ContainerName), strings.ToLower(c.filterContainer)) {
			filtered = append(filtered, entry)
		}
	}

	return filtered
}

// outputJSON outputs results in JSON format
func (c *HistoryCommand) outputJSON(entries []HistoryEntry) error {
	return output.WriteJSONData(os.Stdout, map[string]interface{}{
		"history": entries,
		"count":   len(entries),
	})
}

// outputTable outputs results in human-readable table format
func (c *HistoryCommand) outputTable(entries []HistoryEntry) error {
	if len(entries) == 0 {
		fmt.Println("No history found.")
		return nil
	}

	fmt.Printf("\n=== Container History (showing %d entries) ===\n\n", len(entries))

	// Group by date for better readability
	var currentDate string
	for _, entry := range entries {
		entryDate := entry.Timestamp.Format("2006-01-02")
		if entryDate != currentDate {
			if currentDate != "" {
				fmt.Println()
			}
			fmt.Printf("--- %s ---\n", entryDate)
			currentDate = entryDate
		}

		c.displayEntry(entry)
	}

	return nil
}

// displayEntry displays a single history entry
func (c *HistoryCommand) displayEntry(entry HistoryEntry) {
	timeStr := entry.Timestamp.Format("15:04:05")

	var icon, color, details string

	if entry.Type == "check" {
		icon = "🔍"
		switch entry.Status {
		case storage.CheckStatusUpdateAvailable:
			color = terminal.Yellow()
			details = fmt.Sprintf("Update available: %s → %s", entry.CurrentVer, entry.LatestVer)
		case storage.CheckStatusUpToDate:
			color = terminal.Green()
			details = fmt.Sprintf("Up to date: %s", entry.CurrentVer)
		case storage.CheckStatusFailed:
			color = terminal.Red()
			details = "Check failed"
			if c.verbose && entry.Error != "" {
				details += fmt.Sprintf(" (%s)", entry.Error)
			}
		case storage.CheckStatusLocalImage:
			color = terminal.Gray()
			details = "Local image (no remote)"
		default:
			color = ""
			details = entry.Status
		}
	} else {
		// Update entry
		if entry.Success {
			icon = "✓"
			color = terminal.Green()
			details = fmt.Sprintf("Updated: %s → %s", entry.FromVer, entry.ToVer)
		} else {
			icon = "✗"
			color = terminal.Red()
			details = fmt.Sprintf("Update failed: %s", entry.Operation)
			if c.verbose && entry.Error != "" {
				details += fmt.Sprintf(" (%s)", entry.Error)
			}
		}
	}

	fmt.Printf("  %s%s%s [%s] %s: %s\n",
		color, icon, terminal.Reset(),
		timeStr,
		entry.ContainerName,
		details,
	)

	// Verbose details
	if c.verbose {
		if entry.Type == "check" && entry.Image != "" {
			fmt.Printf("      Image: %s\n", entry.Image)
		}
		if entry.Type == "update" && entry.Operation != "" {
			fmt.Printf("      Operation: %s\n", entry.Operation)
		}
	}
}
