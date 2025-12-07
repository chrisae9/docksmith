package output

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSuccessResponse(t *testing.T) {
	data := map[string]string{"key": "value"}
	resp := SuccessResponse(data)

	if !resp.Success {
		t.Error("Success should be true")
	}
	if resp.Data == nil {
		t.Error("Data should not be nil")
	}
	if resp.Error != "" {
		t.Error("Error should be empty")
	}
	if resp.Version != Version {
		t.Errorf("Version should be %s, got %s", Version, resp.Version)
	}
	if resp.Timestamp == "" {
		t.Error("Timestamp should not be empty")
	}

	// Verify timestamp is valid RFC3339
	_, err := time.Parse(time.RFC3339, resp.Timestamp)
	if err != nil {
		t.Errorf("Timestamp is not valid RFC3339: %v", err)
	}
}

func TestErrorResponse(t *testing.T) {
	err := errors.New("test error")
	resp := ErrorResponse(err)

	if resp.Success {
		t.Error("Success should be false")
	}
	if resp.Error != "test error" {
		t.Errorf("Error should be 'test error', got '%s'", resp.Error)
	}
	if resp.Data != nil {
		t.Error("Data should be nil for error response")
	}
	if resp.Version != Version {
		t.Errorf("Version should be %s, got %s", Version, resp.Version)
	}
}

func TestErrorMessageResponse(t *testing.T) {
	resp := ErrorMessageResponse("custom error message")

	if resp.Success {
		t.Error("Success should be false")
	}
	if resp.Error != "custom error message" {
		t.Errorf("Error should be 'custom error message', got '%s'", resp.Error)
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	resp := SuccessResponse(map[string]int{"count": 42})

	err := WriteJSON(&buf, resp)
	if err != nil {
		t.Fatalf("WriteJSON failed: %v", err)
	}

	output := buf.String()

	// Verify it's valid JSON
	var parsed Response
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	// Verify fields
	if !parsed.Success {
		t.Error("Parsed success should be true")
	}

	// Verify indentation (should have newlines due to SetIndent)
	if !strings.Contains(output, "\n") {
		t.Error("Output should be indented (contain newlines)")
	}
}

func TestWriteJSONData(t *testing.T) {
	var buf bytes.Buffer
	data := struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}{"test", 5}

	err := WriteJSONData(&buf, data)
	if err != nil {
		t.Fatalf("WriteJSONData failed: %v", err)
	}

	// Parse and verify
	var parsed Response
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	if !parsed.Success {
		t.Error("Success should be true")
	}

	// Check data was serialized
	dataMap, ok := parsed.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Data should be a map")
	}
	if dataMap["name"] != "test" {
		t.Errorf("Data name should be 'test', got %v", dataMap["name"])
	}
}

func TestWriteJSONError(t *testing.T) {
	var buf bytes.Buffer
	err := WriteJSONError(&buf, errors.New("something went wrong"))
	if err != nil {
		t.Fatalf("WriteJSONError failed: %v", err)
	}

	var parsed Response
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	if parsed.Success {
		t.Error("Success should be false")
	}
	if parsed.Error != "something went wrong" {
		t.Errorf("Error should be 'something went wrong', got '%s'", parsed.Error)
	}
}

func TestWriteJSONErrorWithData(t *testing.T) {
	var buf bytes.Buffer
	partialData := map[string]string{"partial": "result"}
	err := WriteJSONErrorWithData(&buf, errors.New("partial failure"), partialData)
	if err != nil {
		t.Fatalf("WriteJSONErrorWithData failed: %v", err)
	}

	var parsed Response
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("Output is not valid JSON: %v", err)
	}

	if parsed.Success {
		t.Error("Success should be false for error with data")
	}
	if parsed.Error != "partial failure" {
		t.Errorf("Error should be 'partial failure', got '%s'", parsed.Error)
	}
	if parsed.Data == nil {
		t.Error("Data should not be nil for error with data")
	}

	// Check the partial data
	dataMap, ok := parsed.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Data should be a map")
	}
	if dataMap["partial"] != "result" {
		t.Errorf("Data partial should be 'result', got %v", dataMap["partial"])
	}
}

func TestResponseStruct(t *testing.T) {
	// Test JSON serialization
	resp := Response{
		Success:   true,
		Data:      "test data",
		Timestamp: "2024-01-01T00:00:00Z",
		Version:   "1.0.0",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Verify JSON structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if parsed["success"] != true {
		t.Error("success field incorrect")
	}
	if parsed["data"] != "test data" {
		t.Error("data field incorrect")
	}
	if parsed["timestamp"] != "2024-01-01T00:00:00Z" {
		t.Error("timestamp field incorrect")
	}
	if parsed["version"] != "1.0.0" {
		t.Error("version field incorrect")
	}
}

func TestVersionConstant(t *testing.T) {
	// Version should be defined
	if Version == "" {
		t.Error("Version constant should not be empty")
	}
}

func TestResponseOmitEmptyData(t *testing.T) {
	// Error response should omit empty data
	resp := Response{
		Success:   false,
		Error:     "error",
		Timestamp: "2024-01-01T00:00:00Z",
		Version:   "1.0.0",
	}

	data, _ := json.Marshal(resp)
	jsonStr := string(data)

	// Data should be omitted when nil (omitempty tag)
	if strings.Contains(jsonStr, `"data"`) {
		t.Error("data field should be omitted when nil")
	}
}

func TestResponseOmitEmptyError(t *testing.T) {
	// Success response should omit empty error
	resp := Response{
		Success:   true,
		Data:      "test",
		Timestamp: "2024-01-01T00:00:00Z",
		Version:   "1.0.0",
	}

	data, _ := json.Marshal(resp)
	jsonStr := string(data)

	// Error should be omitted when empty (omitempty tag)
	if strings.Contains(jsonStr, `"error"`) {
		t.Error("error field should be omitted when empty")
	}
}
