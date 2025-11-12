package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/chis/docksmith/internal/update"
)

// PreviewModel shows the update plan before execution
// ZERO business logic - displays plan built by backend
type PreviewModel struct {
	// Backend data (from planner - shared, no duplication)
	discoveryOrch   *update.Orchestrator
	updateOrch      *update.UpdateOrchestrator
	planner         *update.Planner
	plan            *update.UpdatePlan

	// Original selections for going back
	selections      []string
	discoveryResult *update.DiscoveryResult
	bypassedChecks  map[string]bool

	// UI state
	error  string
	width  int
	height int
}

// NewPreviewModel creates a new preview screen
func NewPreviewModel(
	discoveryOrch *update.Orchestrator,
	updateOrch *update.UpdateOrchestrator,
	discoveryResult *update.DiscoveryResult,
	selections []string,
	bypassedChecks map[string]bool,
) (PreviewModel, error) {
	m := PreviewModel{
		discoveryOrch:   discoveryOrch,
		updateOrch:      updateOrch,
		discoveryResult: discoveryResult,
		selections:      selections,
		bypassedChecks:  bypassedChecks,
	}

	// Create planner
	m.planner = update.NewPlanner(discoveryOrch)

	// Build update plan using backend planner
	options := update.PlanOptions{
		IncludeDependents: true, // Auto-include dependents
		BypassChecks:      m.getBypassList(),
		AllowDowngrades:   false,
	}

	plan, err := m.planner.BuildPlan(selections, discoveryResult, options)
	if err != nil {
		return m, fmt.Errorf("failed to build plan: %w", err)
	}

	m.plan = plan
	return m, nil
}

// getBypassList returns list of containers with bypassed checks
func (m PreviewModel) getBypassList() []string {
	bypassed := make([]string, 0)
	for name, isBypassed := range m.bypassedChecks {
		if isBypassed {
			bypassed = append(bypassed, name)
		}
	}
	return bypassed
}

// Init initializes the preview model
func (m PreviewModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model
func (m PreviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil
	}
	return m, nil
}

// handleKey processes keyboard input
func (m PreviewModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "esc":
		// Go back to selection screen
		selectionModel := NewSelectionModel(m.discoveryOrch, m.updateOrch, m.discoveryResult)
		// Restore previous selections
		for _, name := range m.selections {
			selectionModel.selections[name] = true
		}
		// Restore bypassed checks
		selectionModel.bypassedChecks = m.bypassedChecks
		selectionModel.rebuildVisibleList()
		return selectionModel, nil

	case "enter":
		// Validate plan before proceeding
		if err := m.planner.ValidatePlan(m.plan); err != nil {
			m.error = err.Error()
			return m, nil
		}

		// Check if update orchestrator is available
		if m.updateOrch == nil {
			m.error = "Update orchestrator not available (storage may have failed to initialize)"
			return m, nil
		}

		// Transition to progress screen and start execution
		progressModel := NewProgressModel(m.discoveryOrch, m.updateOrch, m.plan)
		return progressModel, progressModel.Init()
	}

	return m, nil
}

// View renders the preview screen
func (m PreviewModel) View() string {
	var sections []string

	// Title
	title := TitleStyle.Render("Update Plan Preview")
	sections = append(sections, title)

	// Error message if present
	if m.error != "" {
		sections = append(sections, ErrorBadge.Render("⚠ "+m.error))
		sections = append(sections, "")
	}

	// Summary statistics
	sections = append(sections, m.renderStats())

	// Execution order
	sections = append(sections, m.renderExecutionOrder())

	// Warnings
	if len(m.plan.Warnings) > 0 {
		sections = append(sections, m.renderWarnings())
	}

	// Restart-only dependents
	if len(m.plan.RestartOnlyDependents) > 0 {
		sections = append(sections, m.renderRestartOnly())
	}

	// Help footer
	sections = append(sections, m.renderHelp())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderStats shows summary statistics
func (m PreviewModel) renderStats() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("Summary:"),
		fmt.Sprintf("  Selected:  %d containers", m.plan.Stats.TotalSelected),
	}

	if m.plan.Stats.TotalAffected > m.plan.Stats.TotalSelected {
		lines = append(lines, fmt.Sprintf("  Affected:  %d containers (includes dependents)", m.plan.Stats.TotalAffected))
	}

	// Change type breakdown
	if len(m.plan.Stats.ByChangeType) > 0 {
		changeTypes := make([]string, 0)
		for changeType, count := range m.plan.Stats.ByChangeType {
			if count > 0 {
				changeTypes = append(changeTypes, fmt.Sprintf("%s(%d)", changeType, count))
			}
		}
		if len(changeTypes) > 0 {
			lines = append(lines, fmt.Sprintf("  By type:   %s", strings.Join(changeTypes, ", ")))
		}
	}

	// Stack breakdown
	if len(m.plan.Stats.ByStack) > 1 { // Only show if multiple stacks
		lines = append(lines, fmt.Sprintf("  Stacks:    %d affected", len(m.plan.Stats.ByStack)))
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderExecutionOrder shows the order containers will be updated
func (m PreviewModel) renderExecutionOrder() string {
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("Execution Order (dependencies first):"),
	}

	// Build container map for quick lookup
	containerMap := make(map[string]update.ContainerInfo)
	for _, container := range m.plan.AffectedContainers {
		containerMap[container.ContainerName] = container
	}

	// Show first 20 containers (or all if less)
	maxDisplay := 20
	displayCount := len(m.plan.ExecutionOrder)
	if displayCount > maxDisplay {
		displayCount = maxDisplay
	}

	for i := 0; i < displayCount; i++ {
		name := m.plan.ExecutionOrder[i]
		container, exists := containerMap[name]

		line := fmt.Sprintf("  %2d. %s", i+1, name)

		if exists {
			// Add version change if available
			versionChange := formatVersionChange(container)
			if versionChange != "" {
				line += fmt.Sprintf(" (%s)", versionChange)
			}

			// Add change type badge
			badge := getChangeTypeBadge(container)
			line += " " + badge

			// Add stack if available
			if container.Stack != "" {
				line += lipgloss.NewStyle().
					Foreground(ColorMuted).
					Render(fmt.Sprintf(" [%s]", container.Stack))
			}
		}

		lines = append(lines, line)
	}

	// Show "and X more..." if truncated
	if len(m.plan.ExecutionOrder) > maxDisplay {
		remaining := len(m.plan.ExecutionOrder) - maxDisplay
		lines = append(lines, lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render(fmt.Sprintf("  ... and %d more", remaining)))
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderWarnings shows plan warnings
func (m PreviewModel) renderWarnings() string {
	lines := []string{
		lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorWarning).
			Render("Warnings:"),
	}

	for _, warning := range m.plan.Warnings {
		lines = append(lines, lipgloss.NewStyle().
			Foreground(ColorWarning).
			Render("  ⚠ "+warning))
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderRestartOnly shows containers that will be restarted but not updated
func (m PreviewModel) renderRestartOnly() string {
	lines := []string{
		lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorInfo).
			Render("Restart-Only (no update):"),
	}

	for _, name := range m.plan.RestartOnlyDependents {
		lines = append(lines, lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render(fmt.Sprintf("  • %s", name)))
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderHelp shows keyboard shortcuts
func (m PreviewModel) renderHelp() string {
	bindings := []KeyBinding{
		{"enter", "confirm and execute"},
		{"esc", "back to selection"},
		{"q", "quit"},
	}
	return formatHelp(bindings)
}
