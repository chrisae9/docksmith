package tui

import (
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// LogMsg is sent when a log line is captured
type LogMsg struct {
	Timestamp time.Time
	Message   string
}

// LogWriter is a custom io.Writer that captures log output for the TUI
type LogWriter struct {
	program *tea.Program
	mu      sync.Mutex
}

// NewLogWriter creates a new log writer that sends log messages to the TUI
func NewLogWriter(program *tea.Program) *LogWriter {
	return &LogWriter{
		program: program,
	}
}

// Write implements io.Writer
func (w *LogWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	msg := strings.TrimSpace(string(p))
	if msg != "" && w.program != nil {
		w.program.Send(LogMsg{
			Timestamp: time.Now(),
			Message:   msg,
		})
	}

	return len(p), nil
}
