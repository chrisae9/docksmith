package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/chis/docksmith/internal/update"
	"github.com/chis/docksmith/internal/version"
)

// SelectionModel handles the container selection screen
// ZERO business logic - just presentation and input collection
type SelectionModel struct {
	// Backend data (from discovery - shared, no duplication)
	discoveryResult   *update.DiscoveryResult
	discoveryOrch     *update.Orchestrator
	updateOrch        *update.UpdateOrchestrator

	// UI state
	cursor            int                       // Current cursor position in visible list
	selections        map[string]bool           // Selected container names
	grouping          GroupingMode              // How to group containers
	filters           FilterState               // Active filters
	bypassedChecks    map[string]bool           // Containers with bypassed checks
	visibleContainers []update.ContainerInfo    // Filtered and sorted list

	// Dimensions
	width  int
	height int

	// Navigation state
	error string
}

// GroupingMode defines how containers are grouped
type GroupingMode int

const (
	GroupBySeverity GroupingMode = iota
	GroupByStack
	GroupFlat
)

// FilterState tracks active filters
type FilterState struct {
	Stack         string // Empty = show all
	ChangeType    string // Empty = show all (major, minor, patch)
	ShowBlocked   bool   // Show containers blocked by pre-update checks
	ShowMigration bool   // Show :latest migration opportunities
}

// NewSelectionModel creates a new selection screen
func NewSelectionModel(discoveryOrch *update.Orchestrator, updateOrch *update.UpdateOrchestrator, discoveryResult *update.DiscoveryResult) SelectionModel {
	m := SelectionModel{
		discoveryOrch:   discoveryOrch,
		updateOrch:      updateOrch,
		discoveryResult: discoveryResult,
		selections:      make(map[string]bool),
		bypassedChecks:  make(map[string]bool),
		grouping:        GroupBySeverity, // Default to severity grouping
		filters: FilterState{
			ShowBlocked:   true, // Show blocked by default
			ShowMigration: true, // Show migrations by default (helps migrate :latest to semver)
		},
	}
	m.rebuildVisibleList()
	return m
}

// Init initializes the selection model
func (m SelectionModel) Init() tea.Cmd {
	return nil
}

// Update handles messages and updates the model
func (m SelectionModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
func (m SelectionModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		return m, tea.Quit

	case "r":
		// Recheck for updates - re-run discovery
		discoveryModel := NewDiscoveryModel(m.discoveryOrch, m.updateOrch, context.Background())
		return discoveryModel, discoveryModel.Init()

	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.visibleContainers)-1 {
			m.cursor++
		}

	case " ": // Space to toggle selection
		if len(m.visibleContainers) > 0 {
			container := m.visibleContainers[m.cursor]
			m.selections[container.ContainerName] = !m.selections[container.ContainerName]
		}

	case "a": // Select all in current view
		for _, container := range m.visibleContainers {
			// Only select if not blocked (unless bypassed)
			if container.Status != update.UpdateAvailableBlocked || m.bypassedChecks[container.ContainerName] {
				m.selections[container.ContainerName] = true
			}
		}

	case "A": // Deselect all
		m.selections = make(map[string]bool)

	case "b": // Toggle blocked filter
		m.filters.ShowBlocked = !m.filters.ShowBlocked
		m.rebuildVisibleList()
		if m.cursor >= len(m.visibleContainers) {
			m.cursor = len(m.visibleContainers) - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
		}

	case "m": // Toggle migration filter
		m.filters.ShowMigration = !m.filters.ShowMigration
		m.rebuildVisibleList()
		if m.cursor >= len(m.visibleContainers) {
			m.cursor = len(m.visibleContainers) - 1
			if m.cursor < 0 {
				m.cursor = 0
			}
		}

	case "g": // Cycle grouping mode
		switch m.grouping {
		case GroupBySeverity:
			m.grouping = GroupByStack
		case GroupByStack:
			m.grouping = GroupFlat
		case GroupFlat:
			m.grouping = GroupBySeverity
		}
		m.rebuildVisibleList()

	case "B": // Bypass pre-update check for current container
		if len(m.visibleContainers) > 0 {
			container := m.visibleContainers[m.cursor]
			if container.Status == update.UpdateAvailableBlocked {
				m.bypassedChecks[container.ContainerName] = !m.bypassedChecks[container.ContainerName]
			}
		}

	case "enter":
		// Validate selections and proceed to preview
		selectedList := m.getSelectedList()
		if len(selectedList) == 0 {
			m.error = "No containers selected"
			return m, nil
		}

		// Transition to preview screen
		previewModel, err := NewPreviewModel(
			m.discoveryOrch,
			m.updateOrch,
			m.discoveryResult,
			selectedList,
			m.bypassedChecks,
		)
		if err != nil {
			m.error = fmt.Sprintf("Failed to build plan: %v", err)
			return m, nil
		}

		return previewModel, nil
	}

	return m, nil
}

// rebuildVisibleList rebuilds the visible containers list based on filters and grouping
func (m *SelectionModel) rebuildVisibleList() {
	visible := make([]update.ContainerInfo, 0)

	for _, container := range m.discoveryResult.Containers {
		// Apply filters
		if !m.shouldShowContainer(container) {
			continue
		}

		visible = append(visible, container)
	}

	// Sort based on grouping mode
	switch m.grouping {
	case GroupBySeverity:
		m.sortBySeverity(visible)
	case GroupByStack:
		m.sortByStack(visible)
	case GroupFlat:
		sort.Slice(visible, func(i, j int) bool {
			return visible[i].ContainerName < visible[j].ContainerName
		})
	}

	m.visibleContainers = visible
}

// shouldShowContainer determines if a container should be shown based on filters
func (m *SelectionModel) shouldShowContainer(container update.ContainerInfo) bool {
	// Only show containers with updates or migration opportunities
	hasUpdate := container.Status == update.UpdateAvailable || container.Status == update.UpdateAvailableBlocked
	isMigration := container.Status == update.UpToDatePinnable

	if !hasUpdate && !isMigration {
		return false
	}

	// Filter by blocked status
	if container.Status == update.UpdateAvailableBlocked && !m.filters.ShowBlocked {
		return false
	}

	// Filter by migration status
	if isMigration && !m.filters.ShowMigration {
		return false
	}

	// Filter by stack
	if m.filters.Stack != "" && container.Stack != m.filters.Stack {
		return false
	}

	// Filter by change type
	if m.filters.ChangeType != "" {
		switch m.filters.ChangeType {
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

// sortBySeverity sorts containers by change severity (major -> minor -> patch -> rebuild)
func (m *SelectionModel) sortBySeverity(containers []update.ContainerInfo) {
	sort.Slice(containers, func(i, j int) bool {
		// Primary sort: by change type severity
		severityI := m.getChangeSeverity(containers[i].ChangeType)
		severityJ := m.getChangeSeverity(containers[j].ChangeType)
		if severityI != severityJ {
			return severityI > severityJ // Higher severity first
		}
		// Secondary sort: by name
		return containers[i].ContainerName < containers[j].ContainerName
	})
}

// getChangeSeverity returns a severity score for sorting (higher = more severe)
func (m *SelectionModel) getChangeSeverity(changeType version.ChangeType) int {
	switch changeType {
	case version.MajorChange:
		return 4
	case version.MinorChange:
		return 3
	case version.PatchChange:
		return 2
	case version.NoChange:
		return 1
	default:
		return 0
	}
}

// sortByStack sorts containers by stack name, then by name
func (m *SelectionModel) sortByStack(containers []update.ContainerInfo) {
	sort.Slice(containers, func(i, j int) bool {
		// Primary sort: by stack (standalone containers last)
		stackI := containers[i].Stack
		stackJ := containers[j].Stack
		if stackI == "" {
			stackI = "zzz_standalone" // Sort standalone to end
		}
		if stackJ == "" {
			stackJ = "zzz_standalone"
		}
		if stackI != stackJ {
			return stackI < stackJ
		}
		// Secondary sort: by name
		return containers[i].ContainerName < containers[j].ContainerName
	})
}

// getSelectedList returns a list of selected container names
func (m SelectionModel) getSelectedList() []string {
	selected := make([]string, 0)
	for name, isSelected := range m.selections {
		if isSelected {
			selected = append(selected, name)
		}
	}
	return selected
}

// View renders the selection screen
func (m SelectionModel) View() string {
	// Build the view
	var sections []string

	// Title
	title := TitleStyle.Render("Select Containers to Update")
	sections = append(sections, title)

	if len(m.visibleContainers) == 0 {
		// Empty state with help
		sections = append(sections, m.renderEmpty())

		// Help footer for empty state
		bindings := []KeyBinding{
			{"r", "recheck for updates"},
			{"q", "quit"},
		}
		help := formatHelp(bindings)
		sections = append(sections, help)

		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Filters status
	filterStatus := m.renderFilterStatus()
	if filterStatus != "" {
		sections = append(sections, filterStatus)
	}

	// Container list
	containerList := m.renderContainerList()
	sections = append(sections, containerList)

	// Selection summary
	summary := m.renderSummary()
	sections = append(sections, summary)

	// Error message if present
	if m.error != "" {
		sections = append(sections, ErrorBadge.Render(m.error))
	}

	// Help footer
	help := m.renderHelp()
	sections = append(sections, help)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderEmpty shows a message when no containers match filters
func (m SelectionModel) renderEmpty() string {
	msg := "No containers with updates or migrations available."

	hints := []string{}
	if !m.filters.ShowBlocked {
		hints = append(hints, "b=show blocked")
	}
	if !m.filters.ShowMigration {
		hints = append(hints, "m=show :latest migrations")
	}

	if len(hints) > 0 {
		msg += "\n\nTry adjusting filters: " + strings.Join(hints, ", ")
	}

	return lipgloss.NewStyle().
		Foreground(ColorMuted).
		Padding(2).
		Render(msg)
}

// renderFilterStatus shows active filters
func (m SelectionModel) renderFilterStatus() string {
	filters := make([]string, 0)

	groupingMode := ""
	switch m.grouping {
	case GroupBySeverity:
		groupingMode = "severity"
	case GroupByStack:
		groupingMode = "stack"
	case GroupFlat:
		groupingMode = "flat"
	}
	filters = append(filters, fmt.Sprintf("Group: %s", groupingMode))

	if !m.filters.ShowBlocked {
		filters = append(filters, "Hiding blocked")
	}
	if !m.filters.ShowMigration {
		filters = append(filters, "Hiding migrations")
	}
	if m.filters.Stack != "" {
		filters = append(filters, fmt.Sprintf("Stack: %s", m.filters.Stack))
	}
	if m.filters.ChangeType != "" {
		filters = append(filters, fmt.Sprintf("Type: %s", m.filters.ChangeType))
	}

	if len(filters) > 0 {
		return lipgloss.NewStyle().
			Foreground(ColorInfo).
			Render("Filters: " + strings.Join(filters, " | "))
	}
	return ""
}

// renderContainerList renders the list of containers
func (m SelectionModel) renderContainerList() string {
	lines := make([]string, 0)

	var lastGroup string
	for i, container := range m.visibleContainers {
		// Add group header if grouping changed
		if m.grouping != GroupFlat {
			currentGroup := m.getGroupName(container)
			if currentGroup != lastGroup {
				lines = append(lines, m.renderGroupHeader(currentGroup))
				lastGroup = currentGroup
			}
		}

		// Render container line
		isSelected := m.selections[container.ContainerName]
		isCursor := i == m.cursor
		line := m.renderContainerItem(container, isSelected, isCursor)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// getGroupName returns the group name for a container based on current grouping
func (m SelectionModel) getGroupName(container update.ContainerInfo) string {
	switch m.grouping {
	case GroupBySeverity:
		// Check status first - migrations should be separate from rebuilds
		if container.Status == update.UpToDatePinnable {
			return "Tag Migrations"
		}

		switch container.ChangeType {
		case version.MajorChange:
			return "Major Updates"
		case version.MinorChange:
			return "Minor Updates"
		case version.PatchChange:
			return "Patch Updates"
		case version.NoChange:
			return "Rebuilds"
		default:
			return "Other"
		}
	case GroupByStack:
		if container.Stack != "" {
			return container.Stack
		}
		return "Standalone"
	default:
		return ""
	}
}

// renderGroupHeader renders a group header
func (m SelectionModel) renderGroupHeader(groupName string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorTitle).
		MarginTop(1).
		Render(groupName + ":")
}

// renderContainerItem renders a single container item
func (m SelectionModel) renderContainerItem(container update.ContainerInfo, selected, isCursor bool) string {
	// Format the line using shared formatting
	line := formatContainerLine(container, selected)

	// Add cursor indicator
	if isCursor {
		line = "> " + line
	} else {
		line = "  " + line
	}

	// Show if check is bypassed
	if m.bypassedChecks[container.ContainerName] {
		line += lipgloss.NewStyle().
			Foreground(ColorWarning).
			Render(" [CHECK BYPASSED]")
	}

	return line
}

// renderSummary shows selection count
func (m SelectionModel) renderSummary() string {
	selectedCount := len(m.getSelectedList())
	total := len(m.visibleContainers)

	style := lipgloss.NewStyle().
		Foreground(ColorInfo).
		MarginTop(1)

	return style.Render(fmt.Sprintf("Selected: %d/%d containers", selectedCount, total))
}

// renderHelp shows keyboard shortcuts
func (m SelectionModel) renderHelp() string {
	bindings := []KeyBinding{
		{"↑/k ↓/j", "navigate"},
		{"space", "toggle selection"},
		{"a/A", "select all/none"},
		{"g", "change grouping"},
		{"b", "toggle blocked"},
		{"m", "toggle migrations"},
		{"B", "bypass check"},
		{"enter", "continue"},
		{"r", "recheck"},
		{"q", "quit"},
	}
	return formatHelp(bindings)
}
