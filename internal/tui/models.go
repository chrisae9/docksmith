package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/chis/docksmith/internal/update"
	"github.com/chis/docksmith/internal/version"
)

// Shared color scheme
var (
	// Status colors
	ColorSuccess = lipgloss.Color("42")  // Green
	ColorWarning = lipgloss.Color("226") // Yellow
	ColorError   = lipgloss.Color("196") // Red
	ColorInfo    = lipgloss.Color("39")  // Blue
	ColorMuted   = lipgloss.Color("240") // Gray

	// Change type colors
	ColorMajor   = lipgloss.Color("196") // Red (breaking changes)
	ColorMinor   = lipgloss.Color("226") // Yellow (new features)
	ColorPatch   = lipgloss.Color("42")  // Green (bug fixes)
	ColorRebuild = lipgloss.Color("39")  // Blue (same version)

	// UI element colors
	ColorSelected   = lipgloss.Color("212") // Pink
	ColorUnselected = lipgloss.Color("250") // Light gray
	ColorBorder     = lipgloss.Color("240") // Gray
	ColorTitle      = lipgloss.Color("212") // Pink
)

// Shared styles
var (
	// Title styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorTitle).
			MarginBottom(1)

	// Status badge styles
	BadgeStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Bold(true)

	SuccessBadge = BadgeStyle.Copy().
			Background(ColorSuccess).
			Foreground(lipgloss.Color("0"))

	WarningBadge = BadgeStyle.Copy().
			Background(ColorWarning).
			Foreground(lipgloss.Color("0"))

	ErrorBadge = BadgeStyle.Copy().
			Background(ColorError).
			Foreground(lipgloss.Color("255"))

	InfoBadge = BadgeStyle.Copy().
		Background(ColorInfo).
		Foreground(lipgloss.Color("255"))

	// List item styles
	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(ColorSelected).
				Bold(true)

	UnselectedItemStyle = lipgloss.NewStyle().
				Foreground(ColorUnselected)

	// Box styles
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	// Help text style
	HelpStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginTop(1)
)

// formatContainerName formats a container name with its tag
func formatContainerName(container update.ContainerInfo) string {
	if container.CurrentTag != "" {
		return fmt.Sprintf("%s [:%s]", container.ContainerName, container.CurrentTag)
	}
	return container.ContainerName
}

// formatVersionChange formats the version change display
func formatVersionChange(container update.ContainerInfo) string {
	if container.CurrentVersion != "" && container.LatestVersion != "" {
		return fmt.Sprintf("%s → %s", container.CurrentVersion, container.LatestVersion)
	}
	return ""
}

// getChangeTypeBadge returns a styled badge for the change type
// Checks status first to handle migrations correctly
func getChangeTypeBadge(container update.ContainerInfo) string {
	// Check status first - migrations should show MIGRATE, not REBUILD
	if container.Status == update.UpToDatePinnable {
		return WarningBadge.Render("MIGRATE")
	}

	switch container.ChangeType {
	case version.MajorChange:
		return BadgeStyle.Copy().
			Background(ColorMajor).
			Foreground(lipgloss.Color("255")).
			Render("MAJOR")
	case version.MinorChange:
		return BadgeStyle.Copy().
			Background(ColorMinor).
			Foreground(lipgloss.Color("0")).
			Render("MINOR")
	case version.PatchChange:
		return BadgeStyle.Copy().
			Background(ColorPatch).
			Foreground(lipgloss.Color("0")).
			Render("PATCH")
	case version.NoChange:
		return BadgeStyle.Copy().
			Background(ColorRebuild).
			Foreground(lipgloss.Color("255")).
			Render("REBUILD")
	case version.Downgrade:
		return ErrorBadge.Render("DOWNGRADE")
	default:
		return InfoBadge.Render("UNKNOWN")
	}
}

// getStatusBadge returns a styled badge for the update status
func getStatusBadge(status update.UpdateStatus) string {
	switch status {
	case update.UpdateAvailable:
		return WarningBadge.Render("UPDATE")
	case update.UpdateAvailableBlocked:
		return ErrorBadge.Render("BLOCKED")
	case update.UpToDate:
		return SuccessBadge.Render("UP TO DATE")
	case update.UpToDatePinnable:
		return WarningBadge.Render("MIGRATE")
	case update.LocalImage:
		return InfoBadge.Render("LOCAL")
	case update.Ignored:
		return lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render("IGNORED")
	default:
		return InfoBadge.Render("UNKNOWN")
	}
}

// formatContainerLine formats a single container info line for display
func formatContainerLine(container update.ContainerInfo, selected bool) string {
	style := UnselectedItemStyle
	checkbox := "[ ]"
	if selected {
		style = SelectedItemStyle
		checkbox = "[✓]"
	}

	// Build the line
	name := formatContainerName(container)
	versionChange := formatVersionChange(container)
	changeTypeBadge := getChangeTypeBadge(container)

	line := fmt.Sprintf("%s %s", checkbox, name)
	if versionChange != "" {
		line += fmt.Sprintf(" %s", versionChange)
	}
	line += fmt.Sprintf(" %s", changeTypeBadge)

	// Add stack info if available
	if container.Stack != "" {
		line += lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render(fmt.Sprintf(" [%s]", container.Stack))
	}

	// Add blocked reason if present
	if container.Status == update.UpdateAvailableBlocked && container.PreUpdateCheckFail != "" {
		line += "\n    " + lipgloss.NewStyle().
			Foreground(ColorWarning).
			Render("⚠ "+container.PreUpdateCheckFail)
	}

	return style.Render(line)
}

// formatStackHeader formats a stack header with update count
func formatStackHeader(stackName string, updateCount, totalCount int) string {
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorTitle).
		MarginTop(1)

	header := fmt.Sprintf("Stack: %s (%d/%d with updates)", stackName, updateCount, totalCount)
	return headerStyle.Render(header)
}

// formatHelpLine formats a help line showing keybindings
func formatHelpLine(keys, description string) string {
	keyStyle := lipgloss.NewStyle().
		Foreground(ColorInfo).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	return fmt.Sprintf("%s %s", keyStyle.Render(keys), descStyle.Render(description))
}

// KeyBinding represents a keyboard shortcut
type KeyBinding struct {
	Key         string
	Description string
}

// formatHelp formats multiple keybindings as a help footer
func formatHelp(bindings []KeyBinding) string {
	lines := make([]string, len(bindings))
	for i, binding := range bindings {
		lines[i] = formatHelpLine(binding.Key, binding.Description)
	}
	return HelpStyle.Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
}
