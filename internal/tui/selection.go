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
			// Only allow selection for actionable statuses
			if m.isSelectable(container) {
				m.selections[container.ContainerName] = !m.selections[container.ContainerName]
			}
		}

	case "a": // Select all in current view
		for _, container := range m.visibleContainers {
			// Only select selectable containers
			if m.isSelectable(container) {
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

// isSelectable determines if a container can be selected for updates
func (m *SelectionModel) isSelectable(container update.ContainerInfo) bool {
	// Only allow selection for actionable statuses
	switch container.Status {
	case update.UpdateAvailable:
		return true
	case update.UpdateAvailableBlocked:
		// Allowed if bypassed
		return m.bypassedChecks[container.ContainerName]
	case update.UpToDatePinnable:
		return true
	default:
		// UpToDate, LocalImage, Ignored, CheckFailed - not selectable
		return false
	}
}

// shouldShowContainer determines if a container should be shown based on filters
func (m *SelectionModel) shouldShowContainer(container update.ContainerInfo) bool {
	// Filter by blocked status
	if container.Status == update.UpdateAvailableBlocked && !m.filters.ShowBlocked {
		return false
	}

	// Filter by migration status
	if container.Status == update.UpToDatePinnable && !m.filters.ShowMigration {
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

// sortBySeverity sorts containers by status and change severity
func (m *SelectionModel) sortBySeverity(containers []update.ContainerInfo) {
	sort.Slice(containers, func(i, j int) bool {
		// Primary sort: by status priority (updates first, then migrations, then up-to-date, etc.)
		statusPriorityI := m.getStatusPriority(containers[i].Status)
		statusPriorityJ := m.getStatusPriority(containers[j].Status)
		if statusPriorityI != statusPriorityJ {
			return statusPriorityI > statusPriorityJ // Higher priority first
		}

		// Secondary sort: by change type severity (within same status)
		severityI := m.getChangeSeverity(containers[i].ChangeType)
		severityJ := m.getChangeSeverity(containers[j].ChangeType)
		if severityI != severityJ {
			return severityI > severityJ // Higher severity first
		}

		// Tertiary sort: by name
		return containers[i].ContainerName < containers[j].ContainerName
	})
}

// getStatusPriority returns a priority score for status sorting (higher = more important)
func (m *SelectionModel) getStatusPriority(status update.UpdateStatus) int {
	switch status {
	case update.UpdateAvailable:
		return 10 // Updates first
	case update.UpdateAvailableBlocked:
		return 9 // Blocked updates next
	case update.UpToDatePinnable:
		return 8 // Migrations
	case update.UpToDate:
		return 5 // Up to date containers
	case update.LocalImage:
		return 4 // Local images
	case update.Ignored:
		return 3 // Ignored
	case update.CheckFailed, update.MetadataUnavailable:
		return 2 // Failed checks last
	default:
		return 1
	}
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

	// Compact title for narrow terminals
	title := "Select Containers"
	if m.width > 60 {
		title = "Select Containers to Update"
	}
	sections = append(sections, TitleStyle.Render(title))

	if len(m.visibleContainers) == 0 {
		// Empty state with help
		sections = append(sections, m.renderEmpty())

		// Help footer for empty state
		bindings := []KeyBinding{
			{"r", "recheck"},
			{"q", "quit"},
		}
		help := formatHelp(bindings)
		sections = append(sections, help)

		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Calculate lines reserved for UI chrome
	// UI elements: title(2) + summary(1) + help(2) = 5 lines minimum
	// Add small buffer for safety
	reservedLines := 6

	// Check if we'll show filter
	var filterStatus string
	if m.width > 50 {
		filterStatus = m.renderFilterStatus()
	}

	// Calculate available height for container list
	// If height not set yet (first render), use generous default
	availableHeight := m.height - reservedLines
	if m.height == 0 {
		availableHeight = 30 // Reasonable default before WindowSizeMsg arrives
	}

	// Render container list with remaining height
	containerList := m.renderContainerListWithHeight(availableHeight)

	// Now actually append sections
	if filterStatus != "" {
		sections = append(sections, filterStatus)
	}
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

// renderEmpty shows a message when no containers are found
func (m SelectionModel) renderEmpty() string {
	msg := "No containers found."

	if m.filters.Stack != "" || m.filters.ChangeType != "" || !m.filters.ShowBlocked || !m.filters.ShowMigration {
		msg += "\n\nFilters are active. Try adjusting or clearing filters."
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

// renderContainerListWithHeight renders the list of containers with viewport scrolling
// availableHeight is the number of lines available for the container list
func (m SelectionModel) renderContainerListWithHeight(availableHeight int) string {
	if len(m.visibleContainers) == 0 {
		return ""
	}

	// Use all available height for viewport
	// IMPORTANT: Don't limit viewport, let it use all available space
	viewportHeight := availableHeight
	if viewportHeight < 1 {
		viewportHeight = 1 // Absolute minimum
	}

	// Build all lines first with group tracking
	type lineInfo struct {
		content      string
		containerIdx int
		groupName    string // Track which group this line belongs to
		isGroupHeader bool
	}
	allLines := make([]lineInfo, 0)

	var lastGroup string
	for i, container := range m.visibleContainers {
		currentGroup := ""
		// Add group header if grouping changed
		if m.grouping != GroupFlat {
			currentGroup = m.getGroupName(container)
			if currentGroup != lastGroup {
				allLines = append(allLines, lineInfo{
					content:       m.renderGroupHeader(currentGroup),
					containerIdx:  -1, // Not a container line
					groupName:     currentGroup,
					isGroupHeader: true,
				})
				lastGroup = currentGroup
			}
		}

		// Render container line
		isSelected := m.selections[container.ContainerName]
		isCursor := i == m.cursor
		line := m.renderContainerItem(container, isSelected, isCursor)
		allLines = append(allLines, lineInfo{
			content:       line,
			containerIdx:  i,
			groupName:     currentGroup,
			isGroupHeader: false,
		})
	}

	// Find the line index of the cursor and its group
	cursorLineIdx := 0
	cursorGroupName := ""
	for i, line := range allLines {
		if line.containerIdx == m.cursor {
			cursorLineIdx = i
			cursorGroupName = line.groupName
			break
		}
	}

	// Calculate viewport window - simple and predictable
	start := 0
	end := len(allLines)

	// Only scroll if we have more lines than viewport
	if len(allLines) > viewportHeight {
		// Keep cursor centered in viewport
		preferredStart := cursorLineIdx - (viewportHeight / 2)
		if preferredStart < 0 {
			preferredStart = 0
		}
		preferredEnd := preferredStart + viewportHeight
		if preferredEnd > len(allLines) {
			preferredEnd = len(allLines)
			preferredStart = preferredEnd - viewportHeight
			if preferredStart < 0 {
				preferredStart = 0
			}
		}

		start = preferredStart
		end = preferredEnd
	}

	// Find the cursor's group header position
	cursorGroupHeaderIdx := -1
	if m.grouping != GroupFlat && cursorGroupName != "" {
		for i := cursorLineIdx; i >= 0; i-- {
			if allLines[i].isGroupHeader && allLines[i].groupName == cursorGroupName {
				cursorGroupHeaderIdx = i
				break
			}
		}
	}

	// Determine if we need sticky header
	showStickyHeader := cursorGroupHeaderIdx >= 0 && cursorGroupHeaderIdx < start

	// Render visible lines
	visibleLines := make([]string, 0)

	// Add sticky header at the very top if needed
	if showStickyHeader {
		stickyHeader := lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTitle).
			Background(lipgloss.Color("236")). // Subtle dark background
			Width(m.width - 4).                // Full width minus padding
			Render("▸ " + cursorGroupName)
		visibleLines = append(visibleLines, stickyHeader)
	}

	// Add viewport content
	for i := start; i < end; i++ {
		visibleLines = append(visibleLines, allLines[i].content)
	}

	// Add scroll indicators
	result := strings.Join(visibleLines, "\n")
	if start > 0 {
		result = lipgloss.NewStyle().Foreground(ColorMuted).Render("  ▲ More above") + "\n" + result
	}
	if end < len(allLines) {
		result = result + "\n" + lipgloss.NewStyle().Foreground(ColorMuted).Render("  ▼ More below")
	}

	return result
}

// getGroupName returns the group name for a container based on current grouping
func (m SelectionModel) getGroupName(container update.ContainerInfo) string {
	switch m.grouping {
	case GroupBySeverity:
		// Group by status first for special cases
		switch container.Status {
		case update.UpToDatePinnable:
			return "Tag Migrations"
		case update.UpToDate:
			return "Up to Date"
		case update.LocalImage:
			return "Local Images"
		case update.Ignored:
			return "Ignored"
		case update.CheckFailed, update.MetadataUnavailable:
			return "Failed Checks"
		}

		// Then group updates by change type
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

// renderGroupHeader renders a group header (compact, no top margin for mobile)
func (m SelectionModel) renderGroupHeader(groupName string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorTitle).
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
