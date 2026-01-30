package update

import (
	"fmt"
	"sort"

	"github.com/chis/docksmith/internal/graph"
	"github.com/chis/docksmith/internal/version"
)

// UpdatePlan represents a validated plan for updating containers
// This is shared by both CLI and TUI - zero duplication
type UpdatePlan struct {
	// User selections
	SelectedContainers []ContainerInfo

	// Computed affected containers (includes dependents)
	AffectedContainers []ContainerInfo

	// Ordered list of container names to update
	ExecutionOrder []string

	// Dependencies that will be restarted but not updated
	RestartOnlyDependents []string

	// Warnings about the plan
	Warnings []string

	// Statistics
	Stats PlanStats
}

// PlanStats provides summary statistics about the update plan
type PlanStats struct {
	TotalSelected      int
	TotalAffected      int
	ByChangeType       map[string]int // major, minor, patch, rebuild
	ByStack            map[string]int // stack name -> count
	BlockedBypassed    int            // Pre-update checks bypassed
	SemverMigrations   int            // :latest -> :vX.Y.Z
}

// Planner builds and validates update plans
type Planner struct {
	orchestrator *Orchestrator
}

// NewPlanner creates a new update planner
func NewPlanner(orchestrator *Orchestrator) *Planner {
	return &Planner{
		orchestrator: orchestrator,
	}
}

// BuildPlan creates an update plan from user selections and discovery result
// This function contains ALL planning logic - used by both CLI and TUI
func (p *Planner) BuildPlan(selections []string, discoveryResult *DiscoveryResult, options PlanOptions) (*UpdatePlan, error) {
	plan := &UpdatePlan{
		SelectedContainers: make([]ContainerInfo, 0),
		AffectedContainers: make([]ContainerInfo, 0),
		RestartOnlyDependents: make([]string, 0),
		Warnings: make([]string, 0),
		Stats: PlanStats{
			ByChangeType: make(map[string]int),
			ByStack:      make(map[string]int),
		},
	}

	// Build map of all containers for quick lookup
	containerMap := make(map[string]ContainerInfo)
	for _, container := range discoveryResult.Containers {
		containerMap[container.ContainerName] = container
	}

	// Collect selected containers
	for _, name := range selections {
		container, exists := containerMap[name]
		if !exists {
			return nil, fmt.Errorf("container %s not found in discovery result", name)
		}
		plan.SelectedContainers = append(plan.SelectedContainers, container)
	}

	// Compute affected containers (includes dependents if requested)
	if options.IncludeDependents {
		affected, restartOnly, err := p.computeAffectedContainers(plan.SelectedContainers, containerMap, discoveryResult)
		if err != nil {
			return nil, fmt.Errorf("failed to compute affected containers: %w", err)
		}
		plan.AffectedContainers = affected
		plan.RestartOnlyDependents = restartOnly
	} else {
		plan.AffectedContainers = plan.SelectedContainers
	}

	// Compute execution order using dependency graph
	order, err := p.computeExecutionOrder(plan.AffectedContainers, containerMap)
	if err != nil {
		return nil, fmt.Errorf("failed to compute execution order: %w", err)
	}
	plan.ExecutionOrder = order

	// Compute statistics
	p.computeStats(plan)

	// Generate warnings
	p.generateWarnings(plan, options)

	return plan, nil
}

// PlanOptions configures how the plan is built
type PlanOptions struct {
	IncludeDependents bool     // Auto-include dependent containers
	BypassChecks      []string // Container names to bypass pre-update checks
	AllowDowngrades   bool     // Allow version downgrades
}

// computeAffectedContainers determines which containers will be affected by the update
// Returns: (affected containers, restart-only dependents, error)
func (p *Planner) computeAffectedContainers(selected []ContainerInfo, containerMap map[string]ContainerInfo, discoveryResult *DiscoveryResult) ([]ContainerInfo, []string, error) {
	affected := make(map[string]ContainerInfo)
	restartOnly := make([]string, 0)

	// Start with selected containers
	for _, container := range selected {
		affected[container.ContainerName] = container
	}

	// Find all containers that depend on selected containers
	// Use the discovery result's dependency information
	for _, container := range discoveryResult.Containers {
		// Check if this container depends on any selected container
		for _, dep := range container.Dependencies {
			if _, isSelected := affected[dep]; isSelected {
				// This container depends on something we're updating
				if _, alreadyAffected := affected[container.ContainerName]; !alreadyAffected {
					// Check if this dependent also has an update available
					if container.Status == UpdateAvailable || container.Status == UpdateAvailableBlocked {
						// Add to affected (will be updated)
						affected[container.ContainerName] = container
					} else {
						// Just restart it (no update needed)
						restartOnly = append(restartOnly, container.ContainerName)
					}
				}
			}
		}
	}

	// Convert map to slice
	affectedSlice := make([]ContainerInfo, 0, len(affected))
	for _, container := range affected {
		affectedSlice = append(affectedSlice, container)
	}

	return affectedSlice, restartOnly, nil
}

// computeExecutionOrder determines the order in which containers should be updated
func (p *Planner) computeExecutionOrder(containers []ContainerInfo, containerMap map[string]ContainerInfo) ([]string, error) {
	// Build dependency graph
	dependencyGraph := graph.NewGraph()

	// Create nodes with their dependencies
	for _, container := range containers {
		// Filter dependencies to only include those in the update set
		validDeps := make([]string, 0)
		for _, dep := range container.Dependencies {
			// Only include dependency if it's also being updated
			if _, exists := containerMap[dep]; exists {
				validDeps = append(validDeps, dep)
			}
		}

		node := &graph.Node{
			ID:           container.ContainerName,
			Dependencies: validDeps,
			Metadata:     make(map[string]string),
		}
		dependencyGraph.AddNode(node)
	}

	// Get update order (dependencies first)
	order, err := dependencyGraph.GetUpdateOrder()
	if err != nil {
		return nil, fmt.Errorf("circular dependency detected: %w", err)
	}

	return order, nil
}

// computeStats calculates statistics about the update plan
func (p *Planner) computeStats(plan *UpdatePlan) {
	plan.Stats.TotalSelected = len(plan.SelectedContainers)
	plan.Stats.TotalAffected = len(plan.AffectedContainers)

	for _, container := range plan.AffectedContainers {
		// Count by change type
		changeType := "unknown"
		switch container.ChangeType {
		case version.NoChange:
			changeType = "rebuild"
		case version.PatchChange:
			changeType = "patch"
		case version.MinorChange:
			changeType = "minor"
		case version.MajorChange:
			changeType = "major"
		case version.Downgrade:
			changeType = "downgrade"
		}
		plan.Stats.ByChangeType[changeType]++

		// Count by stack
		if container.Stack != "" {
			plan.Stats.ByStack[container.Stack]++
		} else {
			plan.Stats.ByStack["standalone"]++
		}

		// Count semver migrations
		if container.Status == UpToDatePinnable {
			plan.Stats.SemverMigrations++
		}

		// Count bypassed checks
		if container.Status == UpdateAvailableBlocked && container.PreUpdateCheckFail != "" {
			// Will be counted when we add bypass support
		}
	}
}

// generateWarnings adds warnings about the plan
func (p *Planner) generateWarnings(plan *UpdatePlan, options PlanOptions) {
	// Check for major version updates
	majorCount := plan.Stats.ByChangeType["major"]
	if majorCount > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d major version update(s) - may have breaking changes", majorCount))
	}

	// Check for mixed stacks (some containers in stack updating, others not)
	stackWarnings := p.checkMixedStacks(plan)
	plan.Warnings = append(plan.Warnings, stackWarnings...)

	// Check for bypassed pre-update checks
	if len(options.BypassChecks) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("Pre-update checks bypassed for %d container(s)", len(options.BypassChecks)))
	}

	// Check for restart-only dependents
	if len(plan.RestartOnlyDependents) > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf("%d dependent container(s) will be restarted without updates", len(plan.RestartOnlyDependents)))
	}
}

// checkMixedStacks checks if we're updating some containers in a stack but not others
func (p *Planner) checkMixedStacks(plan *UpdatePlan) []string {
	warnings := make([]string, 0)

	// Group selected containers by stack
	selectedByStack := make(map[string]int)
	for _, container := range plan.SelectedContainers {
		if container.Stack != "" {
			selectedByStack[container.Stack]++
		}
	}

	// Compare with total containers in stack
	for stackName, selectedCount := range selectedByStack {
		totalInStack := plan.Stats.ByStack[stackName]
		if totalInStack > selectedCount {
			warnings = append(warnings, fmt.Sprintf("Stack '%s': updating %d of %d containers", stackName, selectedCount, totalInStack))
		}
	}

	return warnings
}

// ValidatePlan performs validation checks on the update plan
// ALL validation logic is here - used by both CLI and TUI
func (p *Planner) ValidatePlan(plan *UpdatePlan) error {
	// Check for empty plan
	if len(plan.SelectedContainers) == 0 {
		return fmt.Errorf("no containers selected")
	}

	// Check for circular dependencies (should be caught by topological sort)
	if len(plan.ExecutionOrder) != len(plan.AffectedContainers) {
		return fmt.Errorf("execution order mismatch: %d containers but %d in order", len(plan.AffectedContainers), len(plan.ExecutionOrder))
	}

	// Check for blocked containers without bypass
	blockedCount := 0
	for _, container := range plan.AffectedContainers {
		if container.Status == UpdateAvailableBlocked && container.PreUpdateCheckFail != "" {
			blockedCount++
		}
	}
	if blockedCount > 0 {
		return fmt.Errorf("%d container(s) blocked by pre-update checks - review and bypass if needed", blockedCount)
	}

	return nil
}

// GetSummary returns a human-readable summary of the plan
func (p *Planner) GetSummary(plan *UpdatePlan) string {
	summary := "Update Plan Summary:\n"
	summary += fmt.Sprintf("  Selected: %d containers\n", plan.Stats.TotalSelected)
	if plan.Stats.TotalAffected > plan.Stats.TotalSelected {
		summary += fmt.Sprintf("  Affected: %d containers (includes dependents)\n", plan.Stats.TotalAffected)
	}

	// Change types
	if len(plan.Stats.ByChangeType) > 0 {
		summary += "  By type: "
		types := make([]string, 0)
		for changeType, count := range plan.Stats.ByChangeType {
			types = append(types, fmt.Sprintf("%s(%d)", changeType, count))
		}
		sort.Strings(types)
		summary += fmt.Sprintf("%v\n", types)
	}

	// Warnings
	if len(plan.Warnings) > 0 {
		summary += "\nWarnings:\n"
		for _, warning := range plan.Warnings {
			summary += fmt.Sprintf("  ⚠ %s\n", warning)
		}
	}

	return summary
}
