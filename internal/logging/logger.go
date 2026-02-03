// Package logging provides structured logging with log levels and correlation IDs.
// It can be used as a drop-in replacement for the standard log package while
// adding support for structured fields and request tracing.
package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Level represents a log level.
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a log level string.
func ParseLevel(s string) Level {
	switch strings.ToUpper(s) {
	case "DEBUG":
		return LevelDebug
	case "INFO":
		return LevelInfo
	case "WARN", "WARNING":
		return LevelWarn
	case "ERROR":
		return LevelError
	default:
		return LevelInfo
	}
}

// contextKey is used for storing values in context.
type contextKey string

const (
	correlationIDKey contextKey = "correlation_id"
	fieldsKey        contextKey = "log_fields"
)

// Logger is a structured logger with level support.
type Logger struct {
	mu       sync.Mutex
	output   io.Writer
	level    Level
	json     bool // Output in JSON format
	fields   map[string]interface{}
	callerOn bool
}

// Entry represents a single log entry.
type Entry struct {
	Timestamp     string                 `json:"ts"`
	Level         string                 `json:"level"`
	Message       string                 `json:"msg"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Caller        string                 `json:"caller,omitempty"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
}

var (
	defaultLogger = New()
)

// New creates a new logger with default settings.
func New() *Logger {
	level := LevelInfo
	if lvl := os.Getenv("LOG_LEVEL"); lvl != "" {
		level = ParseLevel(lvl)
	}

	jsonFormat := os.Getenv("LOG_FORMAT") == "json"

	return &Logger{
		output:   os.Stderr,
		level:    level,
		json:     jsonFormat,
		fields:   make(map[string]interface{}),
		callerOn: false,
	}
}

// SetOutput sets the output destination for the logger.
func (l *Logger) SetOutput(w io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = w
}

// SetLevel sets the minimum log level.
func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetJSON enables or disables JSON output format.
func (l *Logger) SetJSON(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.json = enabled
}

// EnableCaller enables caller information in log entries.
func (l *Logger) EnableCaller(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.callerOn = enabled
}

// WithField returns a new logger with the given field added.
func (l *Logger) WithField(key string, value interface{}) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	newFields := make(map[string]interface{}, len(l.fields)+1)
	for k, v := range l.fields {
		newFields[k] = v
	}
	newFields[key] = value

	return &Logger{
		output:   l.output,
		level:    l.level,
		json:     l.json,
		fields:   newFields,
		callerOn: l.callerOn,
	}
}

// WithFields returns a new logger with the given fields added.
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	l.mu.Lock()
	defer l.mu.Unlock()

	newFields := make(map[string]interface{}, len(l.fields)+len(fields))
	for k, v := range l.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}

	return &Logger{
		output:   l.output,
		level:    l.level,
		json:     l.json,
		fields:   newFields,
		callerOn: l.callerOn,
	}
}

// log is the internal logging function.
func (l *Logger) log(ctx context.Context, level Level, format string, args ...interface{}) {
	if level < l.level {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	msg := format
	if len(args) > 0 {
		msg = fmt.Sprintf(format, args...)
	}

	// Get correlation ID from context
	var correlationID string
	if ctx != nil {
		if id, ok := ctx.Value(correlationIDKey).(string); ok {
			correlationID = id
		}
	}

	// Get caller info if enabled
	var caller string
	if l.callerOn {
		if _, file, line, ok := runtime.Caller(3); ok {
			// Get just the filename, not full path
			parts := strings.Split(file, "/")
			if len(parts) > 0 {
				caller = fmt.Sprintf("%s:%d", parts[len(parts)-1], line)
			}
		}
	}

	// Merge context fields with logger fields
	allFields := make(map[string]interface{}, len(l.fields))
	for k, v := range l.fields {
		allFields[k] = v
	}
	if ctx != nil {
		if ctxFields, ok := ctx.Value(fieldsKey).(map[string]interface{}); ok {
			for k, v := range ctxFields {
				allFields[k] = v
			}
		}
	}

	if l.json {
		entry := Entry{
			Timestamp:     time.Now().UTC().Format(time.RFC3339),
			Level:         level.String(),
			Message:       msg,
			CorrelationID: correlationID,
			Caller:        caller,
		}
		if len(allFields) > 0 {
			entry.Fields = allFields
		}

		data, err := json.Marshal(entry)
		if err != nil {
			fmt.Fprintf(l.output, "ERROR: failed to marshal log entry: %v\n", err)
			return
		}
		fmt.Fprintln(l.output, string(data))
	} else {
		// Human-readable format
		timestamp := time.Now().Format("2006/01/02 15:04:05")
		var parts []string

		if correlationID != "" {
			parts = append(parts, fmt.Sprintf("[%s]", correlationID[:8]))
		}

		parts = append(parts, fmt.Sprintf("[%s]", level.String()))

		if caller != "" {
			parts = append(parts, fmt.Sprintf("(%s)", caller))
		}

		parts = append(parts, msg)

		// Append fields if any
		if len(allFields) > 0 {
			fieldParts := make([]string, 0, len(allFields))
			for k, v := range allFields {
				fieldParts = append(fieldParts, fmt.Sprintf("%s=%v", k, v))
			}
			parts = append(parts, fmt.Sprintf("{%s}", strings.Join(fieldParts, ", ")))
		}

		fmt.Fprintf(l.output, "%s %s\n", timestamp, strings.Join(parts, " "))
	}
}

// Debug logs a debug message.
func (l *Logger) Debug(format string, args ...interface{}) {
	l.log(context.Background(), LevelDebug, format, args...)
}

// Info logs an info message.
func (l *Logger) Info(format string, args ...interface{}) {
	l.log(context.Background(), LevelInfo, format, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(format string, args ...interface{}) {
	l.log(context.Background(), LevelWarn, format, args...)
}

// Error logs an error message.
func (l *Logger) Error(format string, args ...interface{}) {
	l.log(context.Background(), LevelError, format, args...)
}

// DebugContext logs a debug message with context.
func (l *Logger) DebugContext(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelDebug, format, args...)
}

// InfoContext logs an info message with context.
func (l *Logger) InfoContext(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelInfo, format, args...)
}

// WarnContext logs a warning message with context.
func (l *Logger) WarnContext(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelWarn, format, args...)
}

// ErrorContext logs an error message with context.
func (l *Logger) ErrorContext(ctx context.Context, format string, args ...interface{}) {
	l.log(ctx, LevelError, format, args...)
}

// Printf provides compatibility with standard log package.
func (l *Logger) Printf(format string, args ...interface{}) {
	l.log(context.Background(), LevelInfo, format, args...)
}

// Println provides compatibility with standard log package.
func (l *Logger) Println(args ...interface{}) {
	l.log(context.Background(), LevelInfo, "%s", fmt.Sprint(args...))
}

// --- Context helpers ---

// WithCorrelationID returns a new context with the correlation ID set.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, correlationIDKey, id)
}

// GetCorrelationID retrieves the correlation ID from context.
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey).(string); ok {
		return id
	}
	return ""
}

// WithLogFields returns a new context with additional log fields.
func WithLogFields(ctx context.Context, fields map[string]interface{}) context.Context {
	existing := make(map[string]interface{})
	if ctxFields, ok := ctx.Value(fieldsKey).(map[string]interface{}); ok {
		for k, v := range ctxFields {
			existing[k] = v
		}
	}
	for k, v := range fields {
		existing[k] = v
	}
	return context.WithValue(ctx, fieldsKey, existing)
}

// --- Package-level functions using default logger ---

// Default returns the default logger.
func Default() *Logger {
	return defaultLogger
}

// SetDefault sets the default logger.
func SetDefault(l *Logger) {
	defaultLogger = l
}

// Debug logs a debug message using the default logger.
func Debug(format string, args ...interface{}) {
	defaultLogger.log(context.Background(), LevelDebug, format, args...)
}

// Info logs an info message using the default logger.
func Info(format string, args ...interface{}) {
	defaultLogger.log(context.Background(), LevelInfo, format, args...)
}

// Warn logs a warning message using the default logger.
func Warn(format string, args ...interface{}) {
	defaultLogger.log(context.Background(), LevelWarn, format, args...)
}

// Error(format string, args ...interface{}) logs an error message using the default logger.
func Error(format string, args ...interface{}) {
	defaultLogger.log(context.Background(), LevelError, format, args...)
}

// DebugContext logs a debug message with context using the default logger.
func DebugContext(ctx context.Context, format string, args ...interface{}) {
	defaultLogger.log(ctx, LevelDebug, format, args...)
}

// InfoContext logs an info message with context using the default logger.
func InfoContext(ctx context.Context, format string, args ...interface{}) {
	defaultLogger.log(ctx, LevelInfo, format, args...)
}

// WarnContext logs a warning message with context using the default logger.
func WarnContext(ctx context.Context, format string, args ...interface{}) {
	defaultLogger.log(ctx, LevelWarn, format, args...)
}

// ErrorContext logs an error message with context using the default logger.
func ErrorContext(ctx context.Context, format string, args ...interface{}) {
	defaultLogger.log(ctx, LevelError, format, args...)
}

// Printf provides compatibility with standard log package using the default logger.
func Printf(format string, args ...interface{}) {
	defaultLogger.log(context.Background(), LevelInfo, format, args...)
}

// Println provides compatibility with standard log package using the default logger.
func Println(args ...interface{}) {
	defaultLogger.log(context.Background(), LevelInfo, "%s", fmt.Sprint(args...))
}
