package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/output"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/docker/docker/api/types/container"
)

// RestartCommand represents the restart command
type RestartCommand struct {
	flagSet *flag.FlagSet
	force   bool
	stack   bool
	json    bool
}

// NewRestartCommand creates a new restart command
func NewRestartCommand() *RestartCommand {
	cmd := &RestartCommand{
		flagSet: flag.NewFlagSet("restart", flag.ExitOnError),
	}

	cmd.flagSet.BoolVar(&cmd.force, "force", false, "Force restart, bypassing pre-update checks")
	cmd.flagSet.BoolVar(&cmd.stack, "stack", false, "Restart entire stack instead of single container")
	cmd.flagSet.BoolVar(&cmd.json, "json", false, "Output in JSON format")

	return cmd
}

// ParseFlags parses the command flags
func (c *RestartCommand) ParseFlags(args []string) error {
	return c.flagSet.Parse(args)
}

// Run executes the restart command
func (c *RestartCommand) Run(ctx context.Context) error {
	args := c.flagSet.Args()
	if len(args) < 1 {
		return fmt.Errorf("container or stack name required")
	}

	targetName := args[0]

	// Create Docker service
	dockerService, err := docker.NewService()
	if err != nil {
		return fmt.Errorf("failed to create Docker service: %w", err)
	}
	defer dockerService.Close()

	if c.stack {
		return c.restartStack(ctx, dockerService, targetName)
	}

	return c.restartContainer(ctx, dockerService, targetName)
}

// restartContainer restarts a single container and its dependents
func (c *RestartCommand) restartContainer(ctx context.Context, dockerService *docker.Service, containerName string) error {
	if !c.json {
		if c.force {
			fmt.Printf("Force restarting container: %s\n", containerName)
		} else {
			fmt.Printf("Restarting container: %s\n", containerName)
		}
	}

	// Restart the main container
	err := dockerService.GetClient().ContainerRestart(ctx, containerName, container.StopOptions{})
	if err != nil {
		if c.json {
			output.WriteJSONError(os.Stdout, fmt.Errorf("failed to restart container: %w", err))
		}
		return fmt.Errorf("failed to restart container: %w", err)
	}

	// Find and restart dependents
	dependents, blocked, depErrors := c.restartDependentContainers(ctx, dockerService, containerName)

	// Build response
	type RestartResult struct {
		Success            bool     `json:"success"`
		Container          string   `json:"container"`
		DependentsRestarted []string `json:"dependents_restarted,omitempty"`
		DependentsBlocked   []string `json:"dependents_blocked,omitempty"`
		Errors             []string `json:"errors,omitempty"`
		Message            string   `json:"message"`
	}

	result := RestartResult{
		Success:            true,
		Container:          containerName,
		DependentsRestarted: dependents,
		DependentsBlocked:   blocked,
		Errors:             depErrors,
	}

	if len(dependents) > 0 {
		result.Message = fmt.Sprintf("Container %s and %d dependent(s) restarted", containerName, len(dependents))
	} else {
		result.Message = fmt.Sprintf("Container %s restarted", containerName)
	}

	if len(blocked) > 0 {
		result.Message += fmt.Sprintf(" (%d blocked by pre-checks)", len(blocked))
	}

	if c.json {
		output.WriteJSONData(os.Stdout, result)
	} else {
		fmt.Printf("✓ %s\n", result.Message)
		if len(dependents) > 0 {
			fmt.Printf("  Dependents restarted: %v\n", dependents)
		}
		if len(blocked) > 0 {
			fmt.Printf("  ⚠ Blocked by pre-checks: %v\n", blocked)
			for _, err := range depErrors {
				fmt.Printf("    - %s\n", err)
			}
			if !c.force {
				fmt.Printf("\n  Tip: Use --force to bypass pre-update checks\n")
			}
		}
	}

	return nil
}

// restartStack restarts all containers in a stack
func (c *RestartCommand) restartStack(ctx context.Context, dockerService *docker.Service, stackName string) error {
	if !c.json {
		fmt.Printf("Restarting stack: %s\n", stackName)
	}

	// Get all containers in the stack
	containers, err := dockerService.ListContainers(ctx)
	if err != nil {
		if c.json {
			output.WriteJSONError(os.Stdout, fmt.Errorf("failed to list containers: %w", err))
		}
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Filter containers by stack
	var stackContainers []string
	for _, cont := range containers {
		if stack, ok := cont.Labels["com.docker.compose.project"]; ok && stack == stackName {
			stackContainers = append(stackContainers, cont.Name)
		}
	}

	if len(stackContainers) == 0 {
		return fmt.Errorf("no containers found in stack: %s", stackName)
	}

	if !c.json {
		fmt.Printf("Found %d containers in stack\n", len(stackContainers))
	}

	// Restart each container
	var successCount int
	var errors []string
	var allDependents []string

	for _, containerName := range stackContainers {
		if !c.json {
			fmt.Printf("  Restarting %s...", containerName)
		}

		err := dockerService.GetClient().ContainerRestart(ctx, containerName, container.StopOptions{})
		if err != nil {
			errMsg := fmt.Sprintf("Failed to restart %s: %v", containerName, err)
			errors = append(errors, errMsg)
			if !c.json {
				fmt.Printf(" FAILED\n")
			}
			continue
		}

		successCount++
		if !c.json {
			fmt.Printf(" OK\n")
		}

		// Restart dependents (never force in stack restart)
		dependents, blocked, depErrors := c.restartDependentContainers(ctx, dockerService, containerName)
		allDependents = append(allDependents, dependents...)
		if len(blocked) > 0 {
			errors = append(errors, fmt.Sprintf("%d dependent(s) of %s blocked by pre-checks", len(blocked), containerName))
		}
		errors = append(errors, depErrors...)
	}

	// Build response
	type StackRestartResult struct {
		Success         bool     `json:"success"`
		Stack           string   `json:"stack"`
		TotalContainers int      `json:"total_containers"`
		Restarted       int      `json:"restarted"`
		Dependents      []string `json:"dependents_restarted,omitempty"`
		Errors          []string `json:"errors,omitempty"`
		Message         string   `json:"message"`
	}

	result := StackRestartResult{
		Success:         successCount > 0,
		Stack:           stackName,
		TotalContainers: len(stackContainers),
		Restarted:       successCount,
		Dependents:      allDependents,
		Errors:          errors,
		Message:         fmt.Sprintf("Restarted %d/%d containers in stack %s", successCount, len(stackContainers), stackName),
	}

	if len(allDependents) > 0 {
		result.Message += fmt.Sprintf(" and %d dependent(s)", len(allDependents))
	}

	if c.json {
		output.WriteJSONData(os.Stdout, result)
	} else {
		fmt.Printf("\n✓ %s\n", result.Message)
		if len(errors) > 0 {
			fmt.Printf("\nErrors:\n")
			for _, err := range errors {
				fmt.Printf("  - %s\n", err)
			}
		}
	}

	return nil
}

// restartDependentContainers finds and restarts containers that depend on the given container
func (c *RestartCommand) restartDependentContainers(ctx context.Context, dockerService *docker.Service, containerName string) ([]string, []string, []string) {
	dependents, err := dockerService.FindDependentContainers(ctx, containerName, scripts.RestartAfterLabel)
	if err != nil {
		log.Printf("Failed to find dependent containers for %s: %v", containerName, err)
		return nil, nil, nil
	}

	if len(dependents) == 0 {
		return nil, nil, nil
	}

	// Get full container info for pre-update checks
	containers, err := dockerService.ListContainers(ctx)
	if err != nil {
		log.Printf("Failed to list containers: %v", err)
		return nil, nil, nil
	}

	containerMap := docker.CreateContainerMap(containers)

	var restarted []string
	var blocked []string
	var errors []string

	for _, dep := range dependents {
		depContainer := containerMap[dep]
		if depContainer == nil {
			errMsg := fmt.Sprintf("Dependent container %s not found", dep)
			errors = append(errors, errMsg)
			continue
		}

		// Run pre-update check if not forced
		if !c.force {
			if scriptPath, ok := depContainer.Labels[scripts.PreUpdateCheckLabel]; ok && scriptPath != "" {
				if err := runPreUpdateCheck(ctx, depContainer, scriptPath); err != nil {
					errMsg := fmt.Sprintf("%s: %v", dep, err)
					blocked = append(blocked, dep)
					errors = append(errors, errMsg)
					continue
				}
			}
		}

		err := dockerService.GetClient().ContainerRestart(ctx, dep, container.StopOptions{})
		if err != nil {
			errMsg := fmt.Sprintf("Failed to restart dependent %s: %v", dep, err)
			errors = append(errors, errMsg)
		} else {
			restarted = append(restarted, dep)
		}
	}

	return restarted, blocked, errors
}

// runPreUpdateCheck runs a pre-update check script (copied from handlers_labels.go logic)
func runPreUpdateCheck(ctx context.Context, container *docker.Container, scriptPath string) error {
	// Use shared implementation with path translation enabled (CLI runs on host)
	return scripts.ExecutePreUpdateCheck(ctx, container, scriptPath, true)
}
