package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLogLevels(t *testing.T) {
	tests := []struct {
		name     string
		setLevel Level
		logLevel Level
		shouldLog bool
	}{
		{"Debug at Debug level", LevelDebug, LevelDebug, true},
		{"Info at Debug level", LevelDebug, LevelInfo, true},
		{"Debug at Info level", LevelInfo, LevelDebug, false},
		{"Info at Info level", LevelInfo, LevelInfo, true},
		{"Warn at Info level", LevelInfo, LevelWarn, true},
		{"Info at Warn level", LevelWarn, LevelInfo, false},
		{"Error at Warn level", LevelWarn, LevelError, true},
		{"Warn at Error level", LevelError, LevelWarn, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := New()
			logger.SetOutput(&buf)
			logger.SetLevel(tt.setLevel)

			switch tt.logLevel {
			case LevelDebug:
				logger.Debug("test message")
			case LevelInfo:
				logger.Info("test message")
			case LevelWarn:
				logger.Warn("test message")
			case LevelError:
				logger.Error("test message")
			}

			hasOutput := buf.Len() > 0
			if hasOutput != tt.shouldLog {
				t.Errorf("Expected shouldLog=%v, got output=%q", tt.shouldLog, buf.String())
			}
		})
	}
}

func TestJSONFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(true)

	logger.Info("test message %d", 42)

	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v\nOutput: %s", err, buf.String())
	}

	if entry.Level != "INFO" {
		t.Errorf("Expected level INFO, got %s", entry.Level)
	}

	if entry.Message != "test message 42" {
		t.Errorf("Expected message 'test message 42', got '%s'", entry.Message)
	}

	if entry.Timestamp == "" {
		t.Error("Expected timestamp to be set")
	}
}

func TestHumanReadableFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(false)

	logger.Info("hello world")

	output := buf.String()
	if !strings.Contains(output, "[INFO]") {
		t.Errorf("Expected [INFO] in output, got: %s", output)
	}

	if !strings.Contains(output, "hello world") {
		t.Errorf("Expected 'hello world' in output, got: %s", output)
	}
}

func TestCorrelationID(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(true)

	ctx := WithCorrelationID(context.Background(), "test-correlation-123")
	logger.InfoContext(ctx, "test message")

	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.CorrelationID != "test-correlation-123" {
		t.Errorf("Expected correlation ID 'test-correlation-123', got '%s'", entry.CorrelationID)
	}
}

func TestWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(true)

	loggerWithFields := logger.WithFields(map[string]interface{}{
		"user_id": 123,
		"action":  "login",
	})

	loggerWithFields.Info("user action")

	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Fields == nil {
		t.Fatal("Expected fields to be set")
	}

	if entry.Fields["user_id"] != float64(123) { // JSON numbers are float64
		t.Errorf("Expected user_id=123, got %v", entry.Fields["user_id"])
	}

	if entry.Fields["action"] != "login" {
		t.Errorf("Expected action='login', got %v", entry.Fields["action"])
	}
}

func TestWithField(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(true)

	logger.WithField("key", "value").Info("test")

	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Fields["key"] != "value" {
		t.Errorf("Expected key='value', got %v", entry.Fields["key"])
	}
}

func TestContextFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(true)

	ctx := WithLogFields(context.Background(), map[string]interface{}{
		"request_id": "req-456",
	})

	logger.InfoContext(ctx, "test message")

	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Fields["request_id"] != "req-456" {
		t.Errorf("Expected request_id='req-456', got %v", entry.Fields["request_id"])
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"DEBUG", LevelDebug},
		{"debug", LevelDebug},
		{"INFO", LevelInfo},
		{"info", LevelInfo},
		{"WARN", LevelWarn},
		{"warn", LevelWarn},
		{"WARNING", LevelWarn},
		{"ERROR", LevelError},
		{"error", LevelError},
		{"invalid", LevelInfo}, // Default
		{"", LevelInfo},        // Default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.level.String()
			if result != tt.expected {
				t.Errorf("Level.String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestPrintfCompatibility(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)

	logger.Printf("formatted %s %d", "message", 42)

	output := buf.String()
	if !strings.Contains(output, "formatted message 42") {
		t.Errorf("Printf compatibility failed, got: %s", output)
	}
}

func TestPrintlnCompatibility(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)

	logger.Println("simple message")

	output := buf.String()
	if !strings.Contains(output, "simple message") {
		t.Errorf("Println compatibility failed, got: %s", output)
	}
}

func TestPackageLevelFunctions(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	SetDefault(logger)

	Info("package level info")

	output := buf.String()
	if !strings.Contains(output, "package level info") {
		t.Errorf("Package-level function failed, got: %s", output)
	}
}

func TestGetCorrelationID(t *testing.T) {
	// Without correlation ID
	ctx := context.Background()
	if id := GetCorrelationID(ctx); id != "" {
		t.Errorf("Expected empty string, got %q", id)
	}

	// With correlation ID
	ctx = WithCorrelationID(ctx, "test-id")
	if id := GetCorrelationID(ctx); id != "test-id" {
		t.Errorf("Expected 'test-id', got %q", id)
	}
}

func TestFieldsChaining(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(true)

	// Chain multiple WithField calls
	logger.WithField("a", 1).WithField("b", 2).WithField("c", 3).Info("chained")

	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if len(entry.Fields) != 3 {
		t.Errorf("Expected 3 fields, got %d", len(entry.Fields))
	}
}

func TestLoggerDoesNotMutateOriginal(t *testing.T) {
	var buf bytes.Buffer
	logger := New()
	logger.SetOutput(&buf)
	logger.SetJSON(true)

	// Create derived logger with field
	derived := logger.WithField("derived", true)

	// Log with original logger
	buf.Reset()
	logger.Info("original")

	var entry Entry
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Fields != nil && entry.Fields["derived"] != nil {
		t.Error("Original logger should not have derived field")
	}

	// Log with derived logger
	buf.Reset()
	derived.Info("derived")

	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if entry.Fields["derived"] != true {
		t.Error("Derived logger should have derived field")
	}
}
