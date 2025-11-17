package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Version is the docksmith version
const Version = "dev"

// Response is a standardized JSON wrapper for all command outputs
// Provides consistent structure with metadata for CLI, TUI, and future UI
type Response struct {
	Success   bool        `json:"success"`
	Data      interface{} `json:"data,omitempty"`
	Error     string      `json:"error,omitempty"`
	Timestamp string      `json:"timestamp"` // RFC3339 format
	Version   string      `json:"version"`   // docksmith version
}

// SuccessResponse creates a successful response with data
func SuccessResponse(data interface{}) Response {
	return Response{
		Success:   true,
		Data:      data,
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   Version,
	}
}

// ErrorResponse creates an error response
func ErrorResponse(err error) Response {
	return Response{
		Success:   false,
		Error:     err.Error(),
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   Version,
	}
}

// ErrorMessageResponse creates an error response from a string message
func ErrorMessageResponse(message string) Response {
	return Response{
		Success:   false,
		Error:     message,
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   Version,
	}
}

// WriteJSON writes a Response as indented JSON to the given writer
func WriteJSON(w io.Writer, response Response) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(response); err != nil {
		return fmt.Errorf("failed to encode JSON: %w", err)
	}
	return nil
}

// WriteJSONData is a convenience function that wraps data in a success response and writes it
func WriteJSONData(w io.Writer, data interface{}) error {
	return WriteJSON(w, SuccessResponse(data))
}

// WriteJSONError is a convenience function that wraps an error in a response and writes it
func WriteJSONError(w io.Writer, err error) error {
	return WriteJSON(w, ErrorResponse(err))
}
