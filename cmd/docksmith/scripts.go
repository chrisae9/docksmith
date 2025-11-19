package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/config"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/storage"
)

// ScriptsOptions contains options for the scripts command
type ScriptsOptions struct {
	OutputFormat string
	Subcommand   string
	Args         []string
}

// ScriptsCommand implements the scripts command
type ScriptsCommand struct {
	options ScriptsOptions
	manager *scripts.Manager
	storage storage.Storage
	config  *config.Config
}

// NewScriptsCommand creates a new scripts command
func NewScriptsCommand() *ScriptsCommand {
	return &ScriptsCommand{
		options: ScriptsOptions{
			OutputFormat: "table",
		},
	}
}

// ParseFlags parses command-line flags for the scripts command
func (c *ScriptsCommand) ParseFlags(args []string) error {
	// First argument should be the subcommand
	if len(args) == 0 {
		return fmt.Errorf("missing subcommand (list, assigned, assign, unassign)")
	}

	c.options.Subcommand = args[0]
	c.options.Args = args[1:]

	// Parse global flags
	fs := flag.NewFlagSet("scripts", flag.ContinueOnError)
	var jsonFlag bool
	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format")
	fs.Parse(c.options.Args)

	// Set JSON mode if either global flag or local flag is set
	if GlobalJSONMode || jsonFlag {
		c.options.OutputFormat = "json"
	}

	// Update args after flag parsing
	c.options.Args = fs.Args()

	return nil
}

// Run executes the scripts command
func (c *ScriptsCommand) Run(ctx context.Context) error {
	// Initialize storage
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/docksmith.db"
	}

	var err error
	c.storage, err = storage.NewSQLiteStorage(dbPath)
	if err != nil {
		return fmt.Errorf("failed to initialize storage: %w", err)
	}
	defer c.storage.Close()

	// Initialize config
	c.config = &config.Config{}
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/data/docksmith.yaml"
	}
	if err := c.config.Load(ctx, c.storage, configPath); err != nil {
		log.Printf("Warning: Failed to load config: %v", err)
	}

	// Initialize script manager
	c.manager = scripts.NewManager(c.storage, c.config)

	// Route to subcommand
	switch c.options.Subcommand {
	case "list":
		return c.runList(ctx)
	case "assigned":
		return c.runAssigned(ctx)
	case "assign":
		return c.runAssign(ctx)
	case "unassign":
		return c.runUnassign(ctx)
	default:
		return fmt.Errorf("unknown subcommand: %s (available: list, assigned, assign, unassign)", c.options.Subcommand)
	}
}

// runList lists available scripts in /scripts folder
func (c *ScriptsCommand) runList(ctx context.Context) error {
	scriptsList, err := c.manager.DiscoverScripts()
	if err != nil {
		return fmt.Errorf("failed to discover scripts: %w", err)
	}

	if c.options.OutputFormat == "json" {
		output := map[string]interface{}{
			"scripts": scriptsList,
			"count":   len(scriptsList),
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Table output
	if len(scriptsList) == 0 {
		fmt.Println("No scripts found in /scripts folder")
		return nil
	}

	fmt.Printf("Available Scripts (%d):\n", len(scriptsList))
	fmt.Println()
	fmt.Printf("%-30s  %-10s  %-8s  %s\n", "Name", "Size", "Exec", "Modified")
	fmt.Printf("%-30s  %-10s  %-8s  %s\n", strings.Repeat("-", 30), strings.Repeat("-", 10), strings.Repeat("-", 8), strings.Repeat("-", 20))

	for _, script := range scriptsList {
		execMark := "✓"
		if !script.Executable {
			execMark = "✗"
		}

		sizeStr := fmt.Sprintf("%d bytes", script.Size)
		if script.Size > 1024 {
			sizeStr = fmt.Sprintf("%.1f KB", float64(script.Size)/1024)
		}

		modifiedStr := script.ModifiedTime.Format("2006-01-02 15:04")

		fmt.Printf("%-30s  %-10s  %-8s  %s\n", script.Name, sizeStr, execMark, modifiedStr)
	}

	fmt.Println()
	fmt.Println("✓ = Executable, ✗ = Not executable")
	fmt.Println()
	fmt.Println("Usage: docksmith scripts assign <container> <script-name>")

	return nil
}

// runAssigned lists script assignments
func (c *ScriptsCommand) runAssigned(ctx context.Context) error {
	assignments, err := c.manager.ListAssignments(ctx, false)
	if err != nil {
		return fmt.Errorf("failed to list assignments: %w", err)
	}

	if c.options.OutputFormat == "json" {
		output := map[string]interface{}{
			"assignments": assignments,
			"count":       len(assignments),
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Table output
	if len(assignments) == 0 {
		fmt.Println("No script assignments found")
		fmt.Println()
		fmt.Println("Usage: docksmith scripts assign <container> <script-name>")
		return nil
	}

	fmt.Printf("Script Assignments (%d):\n", len(assignments))
	fmt.Println()
	fmt.Printf("%-25s  %-35s  %-8s  %s\n", "Container", "Script", "Enabled", "Assigned")
	fmt.Printf("%-25s  %-35s  %-8s  %s\n", strings.Repeat("-", 25), strings.Repeat("-", 35), strings.Repeat("-", 8), strings.Repeat("-", 20))

	for _, assignment := range assignments {
		enabledStr := "yes"
		if !assignment.Enabled {
			enabledStr = "no"
		}

		assignedStr := assignment.AssignedAt.Format("2006-01-02 15:04")

		fmt.Printf("%-25s  %-35s  %-8s  %s\n",
			assignment.ContainerName,
			assignment.ScriptPath,
			enabledStr,
			assignedStr)
	}

	fmt.Println()
	fmt.Println("Note: Container must be restarted for label changes to take effect")
	fmt.Println("      Run: docker compose -f <compose-file> up -d <container>")

	return nil
}

// runAssign assigns a script to a container
func (c *ScriptsCommand) runAssign(ctx context.Context) error {
	if len(c.options.Args) < 2 {
		return fmt.Errorf("usage: docksmith scripts assign <container> <script-name>")
	}

	containerName := c.options.Args[0]
	scriptPath := c.options.Args[1]

	// Assign script
	if err := c.manager.AssignScript(ctx, containerName, scriptPath, "cli"); err != nil {
		return fmt.Errorf("failed to assign script: %w", err)
	}

	if c.options.OutputFormat == "json" {
		output := map[string]interface{}{
			"success":       true,
			"container":     containerName,
			"script":        scriptPath,
			"assigned_at":   time.Now().Format(time.RFC3339),
			"next_step":     "Restart container for changes to take effect",
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Table output
	fmt.Println("✓ Script assigned successfully")
	fmt.Println()
	fmt.Printf("  Container: %s\n", containerName)
	fmt.Printf("  Script:    %s\n", scriptPath)
	fmt.Println()
	fmt.Println("IMPORTANT: Restart the container for the pre-update check to take effect:")
	fmt.Println()
	fmt.Println("  1. Find the compose file for this container")
	fmt.Println("  2. Run: docker compose -f <compose-file> up -d <container>")
	fmt.Println()
	fmt.Println("The script will run before any future updates to this container.")

	return nil
}

// runUnassign removes a script assignment from a container
func (c *ScriptsCommand) runUnassign(ctx context.Context) error {
	if len(c.options.Args) < 1 {
		return fmt.Errorf("usage: docksmith scripts unassign <container>")
	}

	containerName := c.options.Args[0]

	// Unassign script
	if err := c.manager.UnassignScript(ctx, containerName); err != nil {
		return fmt.Errorf("failed to unassign script: %w", err)
	}

	if c.options.OutputFormat == "json" {
		output := map[string]interface{}{
			"success":     true,
			"container":   containerName,
			"unassigned_at": time.Now().Format(time.RFC3339),
			"next_step":   "Restart container for changes to take effect",
		}
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	// Table output
	fmt.Println("✓ Script unassigned successfully")
	fmt.Println()
	fmt.Printf("  Container: %s\n", containerName)
	fmt.Println()
	fmt.Println("IMPORTANT: Restart the container for changes to take effect:")
	fmt.Println()
	fmt.Println("  1. Find the compose file for this container")
	fmt.Println("  2. Run: docker compose -f <compose-file> up -d <container>")
	fmt.Println()
	fmt.Println("The pre-update check has been removed from this container.")

	return nil
}
