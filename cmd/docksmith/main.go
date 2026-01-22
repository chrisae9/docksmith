package main

import (
	"context"
	"fmt"
	"os"

	"github.com/chis/docksmith/internal/output"
)

func main() {
	// Handle help flags
	for _, arg := range os.Args[1:] {
		if arg == "-h" || arg == "--help" || arg == "help" {
			printUsage()
			return
		}
		if arg == "-v" || arg == "--version" || arg == "version" {
			fmt.Printf("docksmith %s\n", output.Version)
			return
		}
	}

	// Start the API server
	cmd := NewAPICommand()
	// Skip "api" subcommand if present (for docker-compose compatibility)
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "api" {
		args = args[1:]
	}
	if err := cmd.ParseFlags(args); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse flags: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := cmd.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`docksmith - Docker container update manager

Usage:
  docksmith [options]

Options:
  --port, -p <port>          Port to listen on (default: 3000)
  --static-dir, -s <dir>     Directory containing static UI files
  --help                     Show this help message
  --version                  Show version information

Environment Variables:
  DB_PATH        Path to SQLite database (default: /data/docksmith.db)
  STATIC_DIR     Directory containing static UI files (default: /app/ui/dist)
  GITHUB_TOKEN   GitHub token for accessing private registries

Examples:
  docksmith                  # Start server on port 3000
  docksmith --port 8080      # Start server on port 8080`)
}
