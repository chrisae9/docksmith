package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/chis/docksmith/internal/update"
)

// ContainerStatus represents the status of a container update
type ContainerStatus int

const (
	StatusPending ContainerStatus = iota
	StatusInProgress
	StatusSuccess
	StatusFailed
	StatusSkipped
)

// ContainerProgress tracks the progress of a single container update
type ContainerProgress struct {
	Name    string
	Status  ContainerStatus
	Message string
	Error   error
}

// UpdateCompleteMsg is sent when all updates are complete
type UpdateCompleteMsg struct {
	Success bool
	Error   error
}

// ContainerUpdateMsg is sent when a container update status changes
type ContainerUpdateMsg struct {
	Name    string
	Status  ContainerStatus
	Message string
	Error   error
}

// ProgressModel shows real-time update progress
type ProgressModel struct {
	// Backend data
	updateOrch *update.UpdateOrchestrator
	plan       *update.UpdatePlan
	ctx        context.Context
	cancel     context.CancelFunc

	// Progress tracking
	containerProgress map[string]*ContainerProgress
	currentIndex      int
	startTime         time.Time

	// UI state
	completed bool
	success   bool
	error     string
	width     int
	height    int

	// Log messages
	logs     []LogMsg
	maxLogs  int
}

// NewProgressModel creates a new progress screen
func NewProgressModel(updateOrch *update.UpdateOrchestrator, plan *update.UpdatePlan) ProgressModel {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)

	m := ProgressModel{
		updateOrch:        updateOrch,
		plan:              plan,
		ctx:               ctx,
		cancel:            cancel,
		containerProgress: make(map[string]*ContainerProgress),
		currentIndex:      0,
		startTime:         time.Now(),
		logs:              make([]LogMsg, 0),
		maxLogs:           20, // Keep last 20 log messages
	}

	// Initialize progress tracking for all containers
	for _, name := range plan.ExecutionOrder {
		m.containerProgress[name] = &ContainerProgress{
			Name:   name,
			Status: StatusPending,
		}
	}

	return m
}

// Init starts the update process
func (m ProgressModel) Init() tea.Cmd {
	return m.updateNextContainer()
}

// Update handles messages and updates the model
func (m ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case ContainerUpdateMsg:
		// Update container status
		if progress, exists := m.containerProgress[msg.Name]; exists {
			progress.Status = msg.Status
			progress.Message = msg.Message
			progress.Error = msg.Error
		}

		// If this container completed, move to next
		if msg.Status == StatusSuccess || msg.Status == StatusFailed || msg.Status == StatusSkipped {
			m.currentIndex++

			// Check if all containers are done
			if m.currentIndex >= len(m.plan.ExecutionOrder) {
				m.completed = true
				m.success = m.allSuccessful()
				return m, nil
			}

			// Start next container
			return m, m.updateNextContainer()
		}

		return m, nil

	case UpdateCompleteMsg:
		m.completed = true
		m.success = msg.Success
		if msg.Error != nil {
			m.error = msg.Error.Error()
		}
		return m, nil

	case LogMsg:
		// Add log message and keep only the last maxLogs
		m.logs = append(m.logs, msg)
		if len(m.logs) > m.maxLogs {
			m.logs = m.logs[len(m.logs)-m.maxLogs:]
		}
		return m, nil
	}

	return m, nil
}

// handleKey processes keyboard input
func (m ProgressModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Cancel updates
		m.cancel()
		return m, tea.Quit

	case "q":
		// Only allow quit if completed
		if m.completed {
			return m, tea.Quit
		}
	}

	return m, nil
}

// updateNextContainer starts updating the next container in the execution order
func (m ProgressModel) updateNextContainer() tea.Cmd {
	if m.currentIndex >= len(m.plan.ExecutionOrder) {
		return nil
	}

	containerName := m.plan.ExecutionOrder[m.currentIndex]

	// Mark as in progress and perform update
	return func() tea.Msg {
		// Get container info from plan
		var containerInfo *update.ContainerInfo
		for i := range m.plan.AffectedContainers {
			if m.plan.AffectedContainers[i].ContainerName == containerName {
				containerInfo = &m.plan.AffectedContainers[i]
				break
			}
		}

		if containerInfo == nil {
			return ContainerUpdateMsg{
				Name:    containerName,
				Status:  StatusFailed,
				Message: "Container not found in plan",
				Error:   fmt.Errorf("container not found"),
			}
		}

		// Perform the update using UpdateOrchestrator
		// The orchestrator expects target version as a string
		targetVersion := containerInfo.LatestVersion
		if targetVersion == "" {
			// Fallback to current version if no update available (rebuild case)
			targetVersion = containerInfo.CurrentVersion
		}

		_, err := m.updateOrch.UpdateSingleContainer(m.ctx, containerName, targetVersion)

		// Determine status based on result
		status := StatusSuccess
		message := "Updated successfully"

		if err != nil {
			status = StatusFailed
			message = "Update failed"
		}

		// Return completion message
		return ContainerUpdateMsg{
			Name:    containerName,
			Status:  status,
			Message: message,
			Error:   err,
		}
	}
}

// allSuccessful checks if all container updates were successful
func (m ProgressModel) allSuccessful() bool {
	for _, progress := range m.containerProgress {
		if progress.Status == StatusFailed {
			return false
		}
	}
	return true
}

// View renders the progress screen
func (m ProgressModel) View() string {
	var sections []string

	// Title
	title := TitleStyle.Render("Updating Containers")
	sections = append(sections, title)

	// Overall progress
	sections = append(sections, m.renderOverallProgress())

	// Container progress list
	sections = append(sections, m.renderContainerList())

	// Log viewer
	if len(m.logs) > 0 {
		sections = append(sections, m.renderLogs())
	}

	// Completion message
	if m.completed {
		sections = append(sections, m.renderCompletion())
	}

	// Help footer
	sections = append(sections, m.renderHelp())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// renderOverallProgress shows overall progress statistics
func (m ProgressModel) renderOverallProgress() string {
	total := len(m.plan.ExecutionOrder)
	completed := 0
	successful := 0
	failed := 0

	for _, progress := range m.containerProgress {
		switch progress.Status {
		case StatusSuccess:
			completed++
			successful++
		case StatusFailed:
			completed++
			failed++
		case StatusSkipped:
			completed++
		}
	}

	elapsed := time.Since(m.startTime).Round(time.Second)

	lines := []string{
		fmt.Sprintf("Progress: %d/%d containers", completed, total),
		fmt.Sprintf("Successful: %d | Failed: %d", successful, failed),
		fmt.Sprintf("Elapsed: %s", elapsed),
	}

	return lipgloss.NewStyle().
		Foreground(ColorInfo).
		Render(strings.Join(lines, " | ")) + "\n"
}

// renderContainerList shows the status of each container
func (m ProgressModel) renderContainerList() string {
	lines := make([]string, 0)

	// Show up to 15 most recent containers
	startIdx := 0
	if len(m.plan.ExecutionOrder) > 15 {
		startIdx = len(m.plan.ExecutionOrder) - 15
	}

	for i := startIdx; i < len(m.plan.ExecutionOrder); i++ {
		name := m.plan.ExecutionOrder[i]
		progress := m.containerProgress[name]

		line := m.renderContainerProgress(progress, i+1)
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderContainerProgress renders a single container's progress
func (m ProgressModel) renderContainerProgress(progress *ContainerProgress, index int) string {
	// Status icon and color
	var icon string
	var color lipgloss.Color

	switch progress.Status {
	case StatusPending:
		icon = "○"
		color = ColorMuted
	case StatusInProgress:
		icon = "◐"
		color = ColorInfo
	case StatusSuccess:
		icon = "✓"
		color = ColorSuccess
	case StatusFailed:
		icon = "✗"
		color = ColorError
	case StatusSkipped:
		icon = "⊘"
		color = ColorMuted
	}

	// Build the line
	line := fmt.Sprintf(" %s %2d. %s", icon, index, progress.Name)

	// Add message if present
	if progress.Message != "" {
		line += lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render(fmt.Sprintf(" - %s", progress.Message))
	}

	// Add error if present
	if progress.Error != nil {
		line += "\n     " + lipgloss.NewStyle().
			Foreground(ColorError).
			Render(fmt.Sprintf("Error: %v", progress.Error))
	}

	return lipgloss.NewStyle().
		Foreground(color).
		Render(line)
}

// renderCompletion shows completion message
func (m ProgressModel) renderCompletion() string {
	if m.success {
		return lipgloss.NewStyle().
			Foreground(ColorSuccess).
			Bold(true).
			Render("✓ All updates completed successfully!\n")
	}

	msg := "✗ Updates completed with errors"
	if m.error != "" {
		msg += fmt.Sprintf("\n  %s", m.error)
	}

	return lipgloss.NewStyle().
		Foreground(ColorError).
		Bold(true).
		Render(msg + "\n")
}

// renderLogs shows recent log messages
func (m ProgressModel) renderLogs() string {
	if len(m.logs) == 0 {
		return ""
	}

	// Show header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(ColorInfo).
		Render("\nRecent Activity:")

	// Show last 10 logs
	lines := []string{header}
	startIdx := 0
	if len(m.logs) > 10 {
		startIdx = len(m.logs) - 10
	}

	for i := startIdx; i < len(m.logs); i++ {
		log := m.logs[i]
		// Format: [HH:MM:SS] message
		timestamp := log.Timestamp.Format("15:04:05")
		line := lipgloss.NewStyle().
			Foreground(ColorMuted).
			Render(fmt.Sprintf("  [%s] %s", timestamp, log.Message))
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n") + "\n"
}

// renderHelp shows keyboard shortcuts
func (m ProgressModel) renderHelp() string {
	if m.completed {
		bindings := []KeyBinding{
			{"q", "quit"},
		}
		return formatHelp(bindings)
	}

	bindings := []KeyBinding{
		{"ctrl+c", "cancel updates"},
	}
	return formatHelp(bindings)
}
