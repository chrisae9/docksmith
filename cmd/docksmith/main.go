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

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/registry"
	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/update"
	"github.com/chis/docksmith/internal/version"
)

// ContainerRegistryInfo contains registry information for a container.
type ContainerRegistryInfo struct {
	Name       string   `json:"name"`
	Image      string   `json:"image"`
	Registry   string   `json:"registry"`
	Repository string   `json:"repository"`
	CurrentTag string   `json:"current_tag"`
	Tags       []string `json:"available_tags"`
	IsLocal    bool     `json:"is_local"`
	Error      string   `json:"error,omitempty"`
	Timestamp  string   `json:"timestamp"`
}

// RegistryReport contains all container registry information.
type RegistryReport struct {
	Containers []ContainerRegistryInfo `json:"containers"`
	Generated  string                  `json:"generated"`
}

// runCheckCommand checks for available updates and displays them.
func runCheckCommand(token string) {
	fmt.Println("=== Checking for Updates ===")
	fmt.Println()

	// Create Docker service
	dockerService, err := docker.NewService()
	if err != nil {
		log.Fatalf("Failed to create Docker service: %v", err)
	}
	defer dockerService.Close()

	// Initialize storage (optional - graceful degradation)
	var storageService storage.Storage
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/docksmith.db"
	}

	storageService, err = storage.NewSQLiteStorage(dbPath)
	if err != nil {
		log.Printf("Warning: Failed to initialize storage (continuing without persistence): %v", err)
		storageService = nil
	} else {
		defer storageService.Close()
	}

	// Create registry manager
	registryManager := registry.NewManager(token)

	// Create update checker with storage
	checker := update.NewChecker(dockerService, registryManager, storageService)

	// Check for updates
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result, err := checker.CheckForUpdates(ctx)
	if err != nil {
		log.Fatalf("Failed to check for updates: %v", err)
	}

	// Display results
	for _, upd := range result.Updates {
		if upd.Status == update.LocalImage {
			fmt.Printf("Container: %s\n", upd.ContainerName)
			fmt.Printf("  Current:  %s\n", upd.Image)
			fmt.Printf("  Status:   LOCAL IMAGE (skipped)\n\n")
			continue
		}

		if upd.Status == update.CheckFailed {
			fmt.Printf("Container: %s\n", upd.ContainerName)
			fmt.Printf("  Current:  %s\n", upd.Image)
			fmt.Printf("  Status:   CHECK FAILED\n")
			fmt.Printf("  Error:    %s\n\n", upd.Error)
			continue
		}

		fmt.Printf("Container: %s\n", upd.ContainerName)
		fmt.Printf("  Current:  %s", upd.Image)
		if upd.CurrentVersion != "" {
			fmt.Printf(" (%s)", upd.CurrentVersion)
		}
		fmt.Println()

		if upd.LatestVersion != "" {
			fmt.Printf("  Latest:   %s\n", upd.LatestVersion)
		}

		if upd.Status == update.UpdateAvailable {
			changeStr := "Unknown"
			switch upd.ChangeType {
			case version.MajorChange:
				changeStr = "Major"
			case version.MinorChange:
				changeStr = "Minor"
			case version.PatchChange:
				changeStr = "Patch"
			}
			if upd.CurrentVersion != "" && upd.LatestVersion != "" {
				fmt.Printf("  Change:   %s → %s (%s)\n", upd.CurrentVersion, upd.LatestVersion, changeStr)
			}
			fmt.Printf("  Status:   UPDATE AVAILABLE\n\n")
		} else if upd.Status == update.UpToDate {
			fmt.Printf("  Status:   UP TO DATE\n\n")
		} else {
			fmt.Printf("  Status:   %s\n\n", upd.Status)
		}
	}

	// Display summary
	fmt.Println("=== Summary ===")
	fmt.Printf("Total containers: %d\n", result.TotalChecked)
	fmt.Printf("Updates available: %d\n", result.UpdatesFound)
	fmt.Printf("Up to date: %d\n", result.UpToDate)
	fmt.Printf("Local images: %d\n", result.LocalImages)
	if result.Failed > 0 {
		fmt.Printf("Failed checks: %d\n", result.Failed)
	}
}

func main() {
	// Default to interactive mode if no args or only flags
	if len(os.Args) == 1 || (len(os.Args) > 1 && os.Args[1][0] == '-') {
		cmd := NewApplyCommand()
		// Parse flags if provided
		if len(os.Args) > 1 {
			if err := cmd.ParseFlags(os.Args[1:]); err != nil {
				log.Fatalf("Failed to parse flags: %v", err)
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		if err := cmd.Run(ctx); err != nil {
			log.Fatalf("Interactive mode failed: %v", err)
		}
		return
	}

	// Check for subcommands
	command := os.Args[1]

	// Handle check command
	if command == "check" {
		cmd := NewCheckCommand()
		if err := cmd.ParseFlags(os.Args[2:]); err != nil {
			log.Fatalf("Failed to parse flags: %v", err)
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := cmd.Run(ctx); err != nil {
			log.Fatalf("Check command failed: %v", err)
		}
		return
	}

	// Handle update command
	if command == "update" {
		if err := runUpdateCommand(os.Args[2:]); err != nil {
			log.Fatalf("Update command failed: %v", err)
		}
		return
	}

	// Handle debug command (old default behavior)
	if command == "debug" {
		runDebugMode()
		return
	}

	// Unknown command
	log.Fatalf("Unknown command: %s\nAvailable commands: check, update, debug\nRun with no arguments for interactive mode", command)
}

// runDebugMode runs the debug/analysis mode (old default behavior)
func runDebugMode() {
	// Parse command-line flags (for legacy/test commands)
	dryRun := flag.Bool("dry-run", false, "Run in dry-run mode (no actual updates)")
	githubToken := flag.String("github-token", "", "GitHub token for GHCR access (overrides GITHUB_TOKEN env var)")
	queryRegistry := flag.Bool("query-registry", false, "Query registry for all containers and save report")
	reportFile := flag.String("report-file", "registry-report.json", "File to save registry report")
	flag.Parse()

	// Use GITHUB_TOKEN env var if flag is not set
	token := *githubToken
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}

	if *dryRun {
		log.Println("Docksmith starting in DRY-RUN mode (no changes will be made)...")
	} else {
		log.Println("Docksmith starting...")
	}

	// Create Docker service
	dockerService, err := docker.NewService()
	if err != nil {
		log.Fatalf("Failed to create Docker service: %v", err)
	}
	defer dockerService.Close()

	log.Println("Connected to Docker daemon")

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// List containers
	containers, err := dockerService.ListContainers(ctx)
	if err != nil {
		log.Fatalf("Failed to list containers: %v", err)
	}

	fmt.Printf("\nFound %d containers\n", len(containers))

	// Build dependency graph
	builder := graph.NewBuilder()
	dependencyGraph := builder.BuildFromContainers(containers)

	fmt.Printf("Built dependency graph with %d nodes\n\n", len(dependencyGraph.Nodes))

	// Analyze versions
	extractor := version.NewExtractor()
	versionedCount := 0
	latestCount := 0
	unknownCount := 0

	for _, container := range containers {
		imgInfo := extractor.ExtractFromImage(container.Image)
		if imgInfo.Tag.IsLatest {
			latestCount++
		} else if imgInfo.Tag.IsVersioned {
			versionedCount++
		} else {
			unknownCount++
		}
	}

	fmt.Println("=== Version Analysis ===")
	fmt.Printf("Versioned tags: %d\n", versionedCount)
	fmt.Printf("Latest tags: %d\n", latestCount)
	fmt.Printf("Other/Unknown: %d\n\n", unknownCount)

	// Check for cycles
	if dependencyGraph.HasCycles() {
		fmt.Println("⚠️  WARNING: Circular dependencies detected!")
		if cycle := dependencyGraph.FindCycle(); cycle != nil {
			fmt.Printf("Cycle: %s\n\n", strings.Join(cycle, " -> "))
		}
	} else {
		fmt.Println("✓ No circular dependencies detected")
		fmt.Println()
	}

	// Display containers with dependencies
	fmt.Println("=== Containers with Dependencies ===")
	fmt.Println()
	for _, node := range dependencyGraph.Nodes {
		if len(node.Dependencies) > 0 {
			fmt.Printf("Container: %s\n", node.ID)
			fmt.Printf("  Image: %s\n", node.Metadata["image"])
			fmt.Printf("  Project: %s\n", node.Metadata["project"])
			fmt.Printf("  Depends on: %s\n", strings.Join(node.Dependencies, ", "))

			dependents := dependencyGraph.GetDependents(node.ID)
			if len(dependents) > 0 {
				fmt.Printf("  Depended on by: %s\n", strings.Join(dependents, ", "))
			}
			fmt.Println()
		}
	}

	// Show version examples
	fmt.Println("=== Version Examples ===")
	fmt.Println()
	versionExamples := make(map[string][]string)
	for _, container := range containers {
		imgInfo := extractor.ExtractFromImage(container.Image)
		category := "other"
		if imgInfo.Tag.IsLatest {
			category = "latest"
		} else if imgInfo.Tag.IsVersioned {
			category = "versioned"
		}

		if len(versionExamples[category]) < 5 {
			versionLine := fmt.Sprintf("%s: %s", container.Name, container.Image)
			if imgInfo.Tag.IsVersioned && imgInfo.Tag.Version != nil {
				versionLine += fmt.Sprintf(" (v%s)", imgInfo.Tag.Version.String())
			}
			versionExamples[category] = append(versionExamples[category], versionLine)
		}
	}

	if len(versionExamples["versioned"]) > 0 {
		fmt.Println("Versioned containers (sample):")
		for _, ex := range versionExamples["versioned"] {
			fmt.Printf("  • %s\n", ex)
		}
		fmt.Println()
	}

	if len(versionExamples["latest"]) > 0 {
		fmt.Println("Latest tag containers (sample):")
		for _, ex := range versionExamples["latest"] {
			fmt.Printf("  • %s\n", ex)
		}
		fmt.Println()
	}

	if len(versionExamples["other"]) > 0 {
		fmt.Println("Other/Unknown containers (sample):")
		for _, ex := range versionExamples["other"] {
			fmt.Printf("  • %s\n", ex)
		}
		fmt.Println()
	}

	// Query all containers if requested (one-time data collection)
	if *queryRegistry {
		fmt.Println("=== Querying All Containers ===")
		fmt.Println()
		fmt.Printf("Querying %d containers (this may take a while)...\n\n", len(containers))

		registryManager := registry.NewManager(token)
		report := RegistryReport{
			Generated:  time.Now().Format(time.RFC3339),
			Containers: make([]ContainerRegistryInfo, 0, len(containers)),
		}

		for i, container := range containers {
			fmt.Printf("[%d/%d] %s...", i+1, len(containers), container.Name)

			imgInfo := extractor.ExtractFromImage(container.Image)
			imageRef := imgInfo.Registry + "/" + imgInfo.Repository

			containerInfo := ContainerRegistryInfo{
				Name:       container.Name,
				Image:      container.Image,
				Registry:   imgInfo.Registry,
				Repository: imgInfo.Repository,
				CurrentTag: imgInfo.Tag.Full,
				Timestamp:  time.Now().Format(time.RFC3339),
			}

			// Check if this is a locally built image
			isLocal, err := dockerService.IsLocalImage(ctx, container.Image)
			if err != nil {
				// If we can't determine, assume not local and try anyway
				isLocal = false
			}

			containerInfo.IsLocal = isLocal

			if isLocal {
				fmt.Printf(" SKIPPED (local image)\n")
			} else {
				// Query registry for tags
				tagCtx, tagCancel := context.WithTimeout(ctx, 15*time.Second)
				tags, err := registryManager.ListTags(tagCtx, imageRef)
				tagCancel()

				if err != nil {
					containerInfo.Error = err.Error()
					fmt.Printf(" ERROR\n")
				} else {
					containerInfo.Tags = tags
					fmt.Printf(" %d tags\n", len(tags))
				}
			}

			report.Containers = append(report.Containers, containerInfo)
		}

		// Save report
		reportJSON, _ := json.MarshalIndent(report, "", "  ")
		os.WriteFile(*reportFile, reportJSON, 0644)

		successCount := 0
		localCount := 0
		failedCount := 0
		for _, c := range report.Containers {
			if c.IsLocal {
				localCount++
			} else if c.Error == "" {
				successCount++
			} else {
				failedCount++
			}
		}
		fmt.Printf("\nSaved to %s: %d successful, %d failed, %d local (skipped)\n",
			*reportFile, successCount, failedCount, localCount)
		os.Exit(0)
	}

	// Normal sample check
	fmt.Println("=== Registry Tag Check (Sample) ===")
	fmt.Println()
	registryManager := registry.NewManager(token)

	// Select a few sample containers to check (prefer Docker Hub for public access)
	sampleContainers := []docker.Container{}
	for _, c := range containers {
		imgInfo := extractor.ExtractFromImage(c.Image)
		// Prefer Docker Hub images for demo (public API works without auth)
		if imgInfo.Registry == "docker.io" && len(sampleContainers) < 3 {
			sampleContainers = append(sampleContainers, c)
		}
	}
	// If we don't have enough Docker Hub images, add any others
	if len(sampleContainers) < 3 {
		for _, c := range containers {
			if len(sampleContainers) >= 3 {
				break
			}
			alreadyAdded := false
			for _, sc := range sampleContainers {
				if sc.ID == c.ID {
					alreadyAdded = true
					break
				}
			}
			if !alreadyAdded {
				sampleContainers = append(sampleContainers, c)
			}
		}
	}

	for _, container := range sampleContainers {
		imgInfo := extractor.ExtractFromImage(container.Image)
		imageRef := imgInfo.Registry + "/" + imgInfo.Repository

		fmt.Printf("Checking: %s (%s)\n", container.Name, imageRef)

		tagCtx, tagCancel := context.WithTimeout(ctx, 10*time.Second)
		tags, err := registryManager.ListTags(tagCtx, imageRef)
		tagCancel()

		if err != nil {
			fmt.Printf("  Error: %v\n\n", err)
			continue
		}

		// Show first 10 tags
		displayTags := tags
		if len(displayTags) > 10 {
			displayTags = displayTags[:10]
		}

		fmt.Printf("  Available tags (%d total): %s\n\n", len(tags), strings.Join(displayTags, ", "))
	}

	if token == "" {
		fmt.Println("Note: Set GITHUB_TOKEN env var to access private GHCR images")
		fmt.Println("Get token with: gh auth token")
		fmt.Println()
	}

	// Get update order
	updateOrder, err := dependencyGraph.GetUpdateOrder()
	if err != nil {
		log.Fatalf("Failed to get update order: %v", err)
	}

	fmt.Println("=== Update Order (dependencies first) ===")
	for i, containerName := range updateOrder {
		node, _ := dependencyGraph.GetNode(containerName)
		fmt.Printf("%d. %s (%s)\n", i+1, containerName, node.Metadata["image"])
	}
	fmt.Println()

	// Get restart order
	restartOrder, err := dependencyGraph.GetRestartOrder()
	if err != nil {
		log.Fatalf("Failed to get restart order: %v", err)
	}

	fmt.Println("=== Restart Order (dependents first) ===")
	for i, containerName := range restartOrder {
		node, _ := dependencyGraph.GetNode(containerName)
		fmt.Printf("%d. %s (%s)\n", i+1, containerName, node.Metadata["image"])
	}
	fmt.Println()

	// Show dry-run status
	if *dryRun {
		fmt.Println("========================================")
		fmt.Println("DRY-RUN MODE: No changes were made")
		fmt.Println("========================================")
		fmt.Println("\nIn a real run, updates would be applied in the order shown above.")
	}

	os.Exit(0)
}
