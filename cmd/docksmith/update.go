package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/chis/docksmith/internal/bootstrap"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/update"
)

// runUpdateCommand updates a container to the latest or specified version
func runUpdateCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: docksmith update <container-name> [version]")
	}

	containerName := args[0]
	var targetVersion string
	if len(args) > 1 {
		targetVersion = args[1]
	}

	log.Printf("=== Starting update for container: %s ===", containerName)
	if targetVersion != "" {
		log.Printf("Target version: %s", targetVersion)
	} else {
		log.Printf("Target version: latest (will be resolved)")
	}

	// Initialize Docker client
	// Initialize services
	deps, cleanup, err := bootstrap.InitializeServices(bootstrap.InitOptions{
		DefaultDBPath: "/home/chis/www/docksmith/docksmith.db",
		Verbose:       true,
	})
	if err != nil {
		return err
	}
	defer cleanup()

	// Create update orchestrator
	log.Println("Creating update orchestrator...")
	orchestrator := update.NewUpdateOrchestrator(
		deps.Docker,
		deps.Docker.GetClient(),
		deps.Storage,
		deps.EventBus,
		deps.Registry,
		deps.Docker.GetPathTranslator(),
	)
	log.Println("✓ Update orchestrator ready")

	// If no target version specified, check for latest
	if targetVersion == "" {
		log.Println("\n=== Checking for latest version ===")
		checker := update.NewChecker(deps.Docker, deps.Registry, deps.Storage)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		result, err := checker.CheckForUpdates(ctx)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		// Find the container
		var latestVer, currentVer string
		for _, upd := range result.Updates {
			if upd.ContainerName == containerName {
				currentVer = upd.CurrentVersion
				if upd.Status != update.UpdateAvailable {
					if GlobalJSONMode {
						// Return JSON with up-to-date status
						output.WriteJSONData(os.Stdout, map[string]interface{}{
							"container_name":  containerName,
							"current_version": currentVer,
							"status":          "up_to_date",
							"message":         "Container is already up to date",
						})
						return nil
					}

					log.Printf("✓ Container '%s' is already up to date", containerName)
					log.Printf("  Current version: %s", currentVer)
					return nil
				}
				latestVer = upd.LatestVersion
				log.Printf("✓ Found update available")
				log.Printf("  Current version: %s", currentVer)
				log.Printf("  Latest version:  %s", latestVer)
				break
			}
		}

		if latestVer == "" {
			return fmt.Errorf("container '%s' not found or no update available", containerName)
		}

		targetVersion = latestVer
	}

	// Start the update
	log.Println("\n=== Starting update operation ===")
	ctx := context.Background()
	operationID, err := orchestrator.UpdateSingleContainer(ctx, containerName, targetVersion)
	if err != nil {
		return fmt.Errorf("failed to start update: %w", err)
	}

	log.Printf("✓ Update operation started")
	log.Printf("  Operation ID: %s", operationID)
	log.Printf("  Container: %s", containerName)
	log.Printf("  Target version: %s", targetVersion)
	log.Println("\nNote: The orchestrator will:")
	log.Println("  • Preserve all labels, volumes, and environment variables")
	log.Println("  • Automatically restart dependent containers in correct order")
	log.Println("  • Perform health checks before marking complete")
	log.Println("  • Auto-rollback if health checks fail (if configured)")

	if deps.Storage == nil {
		log.Println("\n⚠ Storage unavailable - cannot track detailed progress")
		log.Println("⚠ Update is running in background, check Docker logs for status")
		log.Printf("⚠ Monitor with: docker logs -f %s", containerName)
		return nil
	}

	log.Println("\n=== Monitoring progress ===")

	// Poll for status
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	timeout := time.After(10 * time.Minute)
	lastStatus := ""

	for {
		select {
		case <-timeout:
			log.Println("\n✗ Update timed out after 10 minutes")
			log.Println("  Check operation status manually or review logs")
			return fmt.Errorf("update timed out")

		case <-ticker.C:
			op, found, err := deps.Storage.GetUpdateOperation(ctx, operationID)
			if err != nil {
				log.Printf("⚠ Error checking status: %v", err)
				continue
			}

			if !found {
				log.Println("⚠ Operation not found in storage yet")
				continue
			}

			// Show progress updates
			if op.Status != lastStatus {
				log.Printf("→ Status: %s", op.Status)
				lastStatus = op.Status

				// Show dependents if available
				if len(op.DependentsAffected) > 0 {
					log.Printf("  Affected dependents: %v", op.DependentsAffected)
				}
			}

			// Check if complete
			if op.Status == "completed" || op.Status == "complete" {
				if GlobalJSONMode {
					// Output JSON response with operation details
					return output.WriteJSONData(os.Stdout, op)
				}

				log.Println("\n✓ === UPDATE COMPLETED SUCCESSFULLY ===")
				log.Printf("  Container: %s", containerName)
				log.Printf("  Old version: %s", op.OldVersion)
				log.Printf("  New version: %s", targetVersion)
				if len(op.DependentsAffected) > 0 {
					log.Printf("  Dependents restarted: %v", op.DependentsAffected)
				}
				if op.CompletedAt != nil {
					log.Printf("  Completed at: %s", op.CompletedAt.Format(time.RFC3339))
				}
				return nil
			}

			if op.Status == "failed" {
				if GlobalJSONMode {
					// Output JSON error response with operation details
					return output.WriteJSONData(os.Stdout, op)
				}

				log.Println("\n✗ === UPDATE FAILED ===")
				log.Printf("  Error: %s", op.ErrorMessage)
				log.Printf("  Final status: %s", op.Status)
				log.Println("\nTroubleshooting tips:")
				log.Println("  • Check Docker daemon logs")
				log.Printf("  • Check container logs: docker logs %s\n", containerName)
				log.Println("  • Verify compose file is valid")
				log.Println("  • Ensure sufficient disk space for image pull")
				return fmt.Errorf("update failed: %s", op.ErrorMessage)
			}
		}
	}
}
