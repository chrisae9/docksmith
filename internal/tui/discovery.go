package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/chis/docksmith/internal/update"
)

// DiscoveryCompleteMsg is sent when discovery finishes
type DiscoveryCompleteMsg struct {
	Result *update.DiscoveryResult
	Error  error
}

// DiscoveryModel shows the discovery progress screen
type DiscoveryModel struct {
	discoveryOrch *update.Orchestrator
	updateOrch    *update.UpdateOrchestrator
	ctx           context.Context

	// UI state
	logs      []LogMsg
	maxLogs   int
	completed bool
	result    *update.DiscoveryResult
	error     error
	width     int
	height    int
}

// NewDiscoveryModel creates a new discovery screen
func NewDiscoveryModel(discoveryOrch *update.Orchestrator, updateOrch *update.UpdateOrchestrator, ctx context.Context) DiscoveryModel {
	return DiscoveryModel{
		discoveryOrch: discoveryOrch,
		updateOrch:    updateOrch,
		ctx:           ctx,
		logs:          make([]LogMsg, 0),
		maxLogs:       100, // Keep more logs during discovery
	}
}

// Init starts the discovery process
func (m DiscoveryModel) Init() tea.Cmd {
	return m.runDiscovery()
}

// runDiscovery runs the discovery process in the background
func (m DiscoveryModel) runDiscovery() tea.Cmd {
	return func() tea.Msg {
		result, err := m.discoveryOrch.DiscoverAndCheck(m.ctx)
		return DiscoveryCompleteMsg{
			Result: result,
			Error:  err,
		}
	}
}

// Update handles messages and updates the model
func (m DiscoveryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case LogMsg:
		// Add log message and keep only the last maxLogs
		m.logs = append(m.logs, msg)
		if len(m.logs) > m.maxLogs {
			m.logs = m.logs[len(m.logs)-m.maxLogs:]
		}
		return m, nil

	case DiscoveryCompleteMsg:
		m.completed = true
		m.result = msg.Result
		m.error = msg.Error

		// If successful, transition to selection screen
		if msg.Error == nil && msg.Result != nil {
			selectionModel := NewSelectionModel(m.discoveryOrch, m.updateOrch, msg.Result)
			return selectionModel, nil
		}
		return m, nil
	}

	return m, nil
}

// View renders the discovery screen
func (m DiscoveryModel) View() string {
	var sections []string

	// Title
	if m.completed {
		if m.error != nil {
			title := TitleStyle.Render("Discovery Failed")
			sections = append(sections, title)
		} else {
			title := TitleStyle.Render("Discovery Complete")
			sections = append(sections, title)
		}
	} else {
		title := TitleStyle.Render("Discovering Containers...")
		sections = append(sections, title)
	}

	// Progress info
	if !m.completed {
		info := lipgloss.NewStyle().
			Foreground(ColorInfo).
			Render("Checking for container updates and analyzing tags...\n")
		sections = append(sections, info)
	} else if m.result != nil {
		summary := lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Render(fmt.Sprintf("Found %d containers, %d updates available\n",
				m.result.TotalChecked, m.result.UpdatesFound))
		sections = append(sections, summary)
	}

	// Show recent logs
	if len(m.logs) > 0 {
		sections = append(sections, m.renderLogs())
	}

	// Error message
	if m.error != nil {
		errMsg := lipgloss.NewStyle().
			Foreground(ColorError).
			Bold(true).
			Render(fmt.Sprintf("\nError: %v\n", m.error))
		sections = append(sections, errMsg)
	}

	// Help footer
	if m.completed {
		if m.error != nil {
			help := formatHelp([]KeyBinding{{"q", "quit"}})
			sections = append(sections, help)
		}
	} else {
		help := formatHelp([]KeyBinding{{"ctrl+c", "cancel"}})
		sections = append(sections, help)
	}

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderLogs shows recent log messages
func (m DiscoveryModel) renderLogs() string {
	if len(m.logs) == 0 {
		return ""
	}

	// Calculate how many logs to show based on terminal height
	maxVisible := 20
	if m.height > 0 {
		maxVisible = m.height - 10 // Leave room for header/footer
	}

	// Show most recent logs
	lines := []string{}
	startIdx := 0
	if len(m.logs) > maxVisible {
		startIdx = len(m.logs) - maxVisible
	}

	for i := startIdx; i < len(m.logs); i++ {
		log := m.logs[i]
		// Format: [HH:MM:SS] message
		timestamp := log.Timestamp.Format("15:04:05")

		// Color code based on log content
		color := ColorMuted
		if strings.Contains(log.Message, "ERROR") || strings.Contains(log.Message, "Failed") {
			color = ColorError
		} else if strings.Contains(log.Message, "UPDATE") || strings.Contains(log.Message, "PROGRESS") {
			color = ColorInfo
		}

		line := lipgloss.NewStyle().
			Foreground(color).
			Render(fmt.Sprintf("[%s] %s", timestamp, log.Message))
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n") + "\n"
}
