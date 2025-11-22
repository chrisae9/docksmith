package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/bootstrap"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/update"
	"github.com/chis/docksmith/internal/version"
)

// CheckOptions contains options for the check command
type CheckOptions struct {
	OutputFormat  string
	FilterName    string
	FilterStack   string
	FilterType    string
	Verbose       bool
	Quiet         bool
	Standalone    bool
	NoProgress    bool
	CacheTTL      time.Duration
	StacksFile    string
}

// CheckCommand implements the check command
type CheckCommand struct {
	options     CheckOptions
	orchestrator *update.Orchestrator
}

// NewCheckCommand creates a new check command
func NewCheckCommand() *CheckCommand {
	return &CheckCommand{
		options: CheckOptions{
			OutputFormat: "table",
			CacheTTL:     15 * time.Minute,
		},
	}
}

// ParseFlags parses command-line flags for the check command
func (c *CheckCommand) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)

	var jsonFlag bool
	fs.StringVar(&c.options.OutputFormat, "format", c.options.OutputFormat, "Output format: table, json")
	fs.BoolVar(&jsonFlag, "json", false, "Output in JSON format (global flag)")
	fs.StringVar(&c.options.FilterName, "filter", "", "Filter by container name")
	fs.StringVar(&c.options.FilterStack, "stack", "", "Filter by stack name")
	fs.StringVar(&c.options.FilterType, "type", "", "Filter by update type: major, minor, patch")
	fs.BoolVar(&c.options.Verbose, "verbose", false, "Show detailed information")
	fs.BoolVar(&c.options.Verbose, "v", false, "Shorthand for --verbose")
	fs.BoolVar(&c.options.Quiet, "quiet", false, "Quiet mode (exit codes only)")
	fs.BoolVar(&c.options.Standalone, "standalone", false, "Show only standalone containers")
	fs.BoolVar(&c.options.NoProgress, "no-progress", false, "Disable progress indicators")
	fs.DurationVar(&c.options.CacheTTL, "cache-ttl", c.options.CacheTTL, "Cache TTL duration")
	fs.StringVar(&c.options.StacksFile, "stacks-file", "", "Manual stack definitions file")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Set JSON mode if either global flag or local flag is set
	if GlobalJSONMode || jsonFlag {
		c.options.OutputFormat = "json"
	}

	return nil
}

// Run executes the check command
func (c *CheckCommand) Run(ctx context.Context) error {
	// Initialize services
	deps, cleanup, err := bootstrap.InitializeServices(bootstrap.InitOptions{
		DefaultDBPath: "/data/docksmith.db",
		Verbose:       false,
	})
	if err != nil {
		return err
	}
	defer cleanup()

	// Create orchestrator with storage support
	c.orchestrator = update.NewOrchestrator(deps.Docker, deps.Registry)
	c.orchestrator.EnableCache(c.options.CacheTTL)
	if deps.Storage != nil {
		c.orchestrator.SetStorage(deps.Storage)
	}

	// Load manual stacks if specified
	if c.options.StacksFile != "" {
		if err := c.orchestrator.LoadManualStacks(c.options.StacksFile); err != nil {
			return fmt.Errorf("failed to load manual stacks: %w", err)
		}
	}

	// Show progress indicator unless disabled or in quiet mode
	if !c.options.NoProgress && !c.options.Quiet && c.options.OutputFormat != "json" {
		fmt.Fprintln(os.Stderr, "Discovering containers and checking for updates...")
	}

	// Discover and check
	result, err := c.orchestrator.DiscoverAndCheck(ctx)
	if err != nil {
		return fmt.Errorf("discovery failed: %w", err)
	}

	// Filter results
	filteredResult := c.filterResults(result)

	// Output results
	switch c.options.OutputFormat {
	case "json":
		return c.outputJSON(filteredResult)
	case "table", "":
		return c.outputTable(filteredResult)
	default:
		return fmt.Errorf("unknown output format: %s", c.options.OutputFormat)
	}
}

// filterResults filters the discovery result based on options
func (c *CheckCommand) filterResults(result *update.DiscoveryResult) *update.DiscoveryResult {
	filtered := &update.DiscoveryResult{
		Stacks:              make(map[string]*update.Stack),
		StandaloneContainers: []update.ContainerInfo{},
		TotalChecked:        result.TotalChecked,
		UpdatesFound:        0,
		UpToDate:           0,
		LocalImages:        0,
		Failed:             0,
	}

	// Filter containers
	for _, container := range result.Containers {
		if !c.matchesFilter(container) {
			continue
		}

		filtered.Containers = append(filtered.Containers, container)

		// Update counters
		switch container.Status {
		case update.UpdateAvailable, update.UpdateAvailableBlocked:
			filtered.UpdatesFound++
		case update.UpToDate, update.UpToDatePinnable:
			filtered.UpToDate++
		case update.LocalImage:
			filtered.LocalImages++
		case update.CheckFailed, update.MetadataUnavailable:
			filtered.Failed++
		case update.Ignored:
			filtered.Ignored++
		}
	}

	// Rebuild stacks and standalone containers
	for _, container := range filtered.Containers {
		if container.Stack != "" {
			if _, exists := filtered.Stacks[container.Stack]; !exists {
				filtered.Stacks[container.Stack] = &update.Stack{
					Name:       container.Stack,
					Containers: []update.ContainerInfo{},
				}
			}
			stack := filtered.Stacks[container.Stack]
			stack.Containers = append(stack.Containers, container)
			if container.Status == update.UpdateAvailable {
				stack.HasUpdates = true
			}
		} else {
			filtered.StandaloneContainers = append(filtered.StandaloneContainers, container)
		}
	}

	return filtered
}

// matchesFilter checks if a container matches the filter criteria
func (c *CheckCommand) matchesFilter(container update.ContainerInfo) bool {
	// Name filter
	if c.options.FilterName != "" {
		if !strings.Contains(strings.ToLower(container.ContainerName),
			strings.ToLower(c.options.FilterName)) {
			return false
		}
	}

	// Stack filter
	if c.options.FilterStack != "" {
		if container.Stack != c.options.FilterStack {
			return false
		}
	}

	// Standalone filter
	if c.options.Standalone && container.Stack != "" {
		return false
	}

	// Type filter
	if c.options.FilterType != "" {
		switch strings.ToLower(c.options.FilterType) {
		case "major":
			if container.ChangeType != version.MajorChange {
				return false
			}
		case "minor":
			if container.ChangeType != version.MinorChange {
				return false
			}
		case "patch":
			if container.ChangeType != version.PatchChange {
				return false
			}
		}
	}

	return true
}

// outputJSON outputs results in JSON format
func (c *CheckCommand) outputJSON(result *update.DiscoveryResult) error {
	return output.WriteJSONData(os.Stdout, result)
}

// outputTable outputs results in human-readable table format
func (c *CheckCommand) outputTable(result *update.DiscoveryResult) error {
	if c.options.Quiet {
		// Quiet mode: only return exit code
		if result.UpdatesFound > 0 {
			os.Exit(1)
		}
		return nil
	}

	// Display stacks
	if len(result.Stacks) > 0 {
		fmt.Println("\n=== Container Stacks ===")
		fmt.Println()

		// Sort stacks by name
		stackNames := make([]string, 0, len(result.Stacks))
		for name := range result.Stacks {
			stackNames = append(stackNames, name)
		}
		sort.Strings(stackNames)

		for _, stackName := range stackNames {
			stack := result.Stacks[stackName]
			c.displayStack(stack)
		}
	}

	// Display standalone containers
	if len(result.StandaloneContainers) > 0 {
		fmt.Println("\n=== Standalone Containers ===")
		fmt.Println()
		for _, container := range result.StandaloneContainers {
			c.displayContainer(container)
		}
	}

	// Display summary
	fmt.Println("\n=== Summary ===")
	fmt.Printf("Total containers: %d\n", len(result.Containers))
	if result.UpdatesFound > 0 {
		fmt.Printf("%sUpdates available: %d%s\n", colorYellow(), result.UpdatesFound, colorReset())
	} else {
		fmt.Printf("Updates available: %d\n", result.UpdatesFound)
	}
	fmt.Printf("Up to date: %d\n", result.UpToDate)
	if result.Ignored > 0 {
		fmt.Printf("Ignored: %d\n", result.Ignored)
	}
	fmt.Printf("Local images: %d\n", result.LocalImages)
	if result.Failed > 0 {
		fmt.Printf("%sFailed checks: %d%s\n", colorRed(), result.Failed, colorReset())
	}
	fmt.Println()

	return nil
}

// displayStack displays a stack with its containers
func (c *CheckCommand) displayStack(stack *update.Stack) {
	header := fmt.Sprintf("Stack: %s", stack.Name)
	if stack.HasUpdates {
		header += fmt.Sprintf(" %s[Updates Available]%s", colorYellow(), colorReset())
	}
	fmt.Println(header)

	for _, container := range stack.Containers {
		c.displayContainer(container)
	}
	fmt.Println()
}

// displayContainer displays a single container
func (c *CheckCommand) displayContainer(container update.ContainerInfo) {
	status := c.formatStatus(container)

	// Show tag info alongside container name
	tagInfo := ""
	if container.CurrentTag != "" {
		tagInfo = fmt.Sprintf(" [:%s]", container.CurrentTag)
	}
	fmt.Printf("  • %s%s - %s\n", container.ContainerName, tagInfo, status)

	// Show recommended semver tag for pinnable containers (always visible, not just in verbose)
	if container.RecommendedTag != "" && container.Status == update.UpToDatePinnable {
		fmt.Printf("    → Migrate to: %s\n", container.RecommendedTag)
	}

	// Show pre-update check status (always visible, not just in verbose)
	if container.PreUpdateCheck != "" {
		if container.Status == update.UpdateAvailableBlocked && container.PreUpdateCheckFail != "" {
			fmt.Printf("    ⚠ Pre-update check failed: %s\n", container.PreUpdateCheckFail)
		} else if container.PreUpdateCheckPass {
			fmt.Printf("    ✓ Pre-update check passed\n")
		}
	}

	if c.options.Verbose {
		fmt.Printf("    Image: %s\n", container.Image)
		if container.CurrentVersion != "" {
			fmt.Printf("    Current: %s\n", container.CurrentVersion)
		}
		if container.LatestVersion != "" {
			fmt.Printf("    Latest: %s\n", container.LatestVersion)
		}
		if container.Service != "" {
			fmt.Printf("    Service: %s\n", container.Service)
		}
		if len(container.Dependencies) > 0 {
			fmt.Printf("    Dependencies: %s\n", strings.Join(container.Dependencies, ", "))
		}
		if container.PreUpdateCheck != "" {
			fmt.Printf("    Pre-update check: %s\n", container.PreUpdateCheck)
		}
		if container.Error != "" {
			fmt.Printf("    Error: %s\n", container.Error)
		}
	}
}

// formatStatus formats the status with color coding
func (c *CheckCommand) formatStatus(container update.ContainerInfo) string {
	switch container.Status {
	case update.UpdateAvailable:
		changeStr := "unknown"
		color := colorYellow()
		switch container.ChangeType {
		case version.MajorChange:
			changeStr = "major"
			color = colorRed()
		case version.MinorChange:
			changeStr = "minor"
			color = colorYellow()
		case version.PatchChange:
			changeStr = "patch"
			color = colorGreen()
		case version.Downgrade:
			changeStr = "downgrade"
			color = colorRed()
		case version.NoChange:
			changeStr = "rebuild"
			color = colorGreen()
		}
		// Show current → new version
		if container.CurrentVersion != "" && container.LatestVersion != "" {
			return fmt.Sprintf("%sUPDATE AVAILABLE%s (%s → %s, %s)", color, colorReset(), container.CurrentVersion, container.LatestVersion, changeStr)
		}
		return fmt.Sprintf("%sUPDATE AVAILABLE (%s)%s", color, changeStr, colorReset())
	case update.UpdateAvailableBlocked:
		changeStr := "unknown"
		switch container.ChangeType {
		case version.MajorChange:
			changeStr = "major"
		case version.MinorChange:
			changeStr = "minor"
		case version.PatchChange:
			changeStr = "patch"
		case version.Downgrade:
			changeStr = "downgrade"
		case version.NoChange:
			changeStr = "rebuild"
		}
		// Show that update is blocked
		if container.CurrentVersion != "" && container.LatestVersion != "" {
			return fmt.Sprintf("%sUPDATE AVAILABLE - BLOCKED%s (%s → %s, %s)", colorYellow(), colorReset(), container.CurrentVersion, container.LatestVersion, changeStr)
		}
		return fmt.Sprintf("%sUPDATE AVAILABLE - BLOCKED%s", colorYellow(), colorReset())
	case update.UpToDate:
		// Show current version
		if container.CurrentVersion != "" {
			return fmt.Sprintf("%sUP TO DATE%s (%s)", colorGreen(), colorReset(), container.CurrentVersion)
		}
		return fmt.Sprintf("%sUP TO DATE%s", colorGreen(), colorReset())
	case update.UpToDatePinnable:
		// Show current version and indicate migration needed
		if container.CurrentVersion != "" {
			return fmt.Sprintf("%sUP TO DATE - MIGRATE TO SEMVER%s (%s)", colorYellow(), colorReset(), container.CurrentVersion)
		}
		return fmt.Sprintf("%sUP TO DATE - MIGRATE TO SEMVER%s", colorYellow(), colorReset())
	case update.LocalImage:
		return "LOCAL IMAGE"
	case update.Ignored:
		return fmt.Sprintf("%sIGNORED%s", colorGray(), colorReset())
	case update.CheckFailed:
		return fmt.Sprintf("%sCHECK FAILED%s", colorRed(), colorReset())
	case update.MetadataUnavailable:
		return fmt.Sprintf("%sMETADATA UNAVAILABLE%s (use -v for details)", colorYellow(), colorReset())
	case update.ComposeMismatch:
		return fmt.Sprintf("%sCOMPOSE MISMATCH%s (container image differs from compose file)", colorRed(), colorReset())
	case update.Unknown:
		// Show more helpful message for UNKNOWN status
		if container.Error != "" {
			return fmt.Sprintf("%sUNKNOWN%s (use -v for details)", colorYellow(), colorReset())
		}
		return fmt.Sprintf("%sUNKNOWN%s", colorYellow(), colorReset())
	default:
		return string(container.Status)
	}
}

// Color codes for terminal output
func colorRed() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[31m"
}

func colorGreen() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[32m"
}

func colorYellow() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[33m"
}

func colorGray() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[90m"
}

func colorReset() string {
	if os.Getenv("NO_COLOR") != "" {
		return ""
	}
	return "\033[0m"
}
