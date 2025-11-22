package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/chis/docksmith/internal/bootstrap"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/tui"
	"github.com/chis/docksmith/internal/update"
)

// ApplyCommand implements the interactive update command
type ApplyCommand struct {
	filterName  string
	filterStack string
	filterType  string
	verbose     bool
}

// NewApplyCommand creates a new apply command
func NewApplyCommand() *ApplyCommand {
	return &ApplyCommand{}
}

// ParseFlags parses command-line flags for the apply command
func (c *ApplyCommand) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)

	var jsonFlag bool
	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format (global flag)")
	fs.StringVar(&c.filterName, "filter", "", "Pre-filter by container name")
	fs.StringVar(&c.filterStack, "stack", "", "Pre-filter by stack name")
	fs.StringVar(&c.filterType, "type", "", "Pre-filter by change type (major, minor, patch)")
	fs.BoolVar(&c.verbose, "verbose", false, "Show debug logs during discovery")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Update global mode if local flag is set
	if jsonFlag {
		GlobalJSONMode = true
	}

	return nil
}

// Run executes the apply command (interactive mode)
func (c *ApplyCommand) Run(ctx context.Context) error {
	// Initialize services
	deps, cleanup, err := bootstrap.InitializeServices(bootstrap.InitOptions{
		DefaultDBPath: DefaultDBPath,
		Verbose:       c.verbose,
	})
	if err != nil {
		return err
	}
	defer cleanup()

	// Create discovery orchestrator
	discoveryOrchestrator := update.NewOrchestrator(deps.Docker, deps.Registry)

	// Create update orchestrator
	var updateOrchestrator *update.UpdateOrchestrator
	if deps.Storage != nil {
		updateOrchestrator = update.NewUpdateOrchestrator(
			deps.Docker,
			deps.Docker.GetClient(),
			deps.Storage,
			deps.EventBus,
			deps.Registry,
			deps.Docker.GetPathTranslator(),
		)
	}

	// Step 6: Check if JSON mode is enabled
	if GlobalJSONMode {
		// JSON mode: Run discovery and return results without TUI
		if c.verbose {
			log.Println("Running discovery in JSON mode...")
		}

		discoveryResult, err := discoveryOrchestrator.DiscoverAndCheck(ctx)
		if err != nil {
			if c.verbose {
				log.Printf("Discovery failed: %v", err)
			}
			return output.WriteJSONError(os.Stdout, err)
		}

		// Return discovery result as JSON
		return output.WriteJSONData(os.Stdout, discoveryResult)
	}

	// Step 7: Launch TUI with discovery screen (normal interactive mode)
	// The discovery screen will run the discovery process and show logs in real-time
	discoveryModel := tui.NewDiscoveryModel(discoveryOrchestrator, updateOrchestrator, ctx)
	program := tea.NewProgram(discoveryModel, tea.WithAltScreen())

	// Redirect log output to TUI for clean display
	logWriter := tui.NewLogWriter(program)
	log.SetOutput(logWriter)

	if _, err := program.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}

	return nil
}
