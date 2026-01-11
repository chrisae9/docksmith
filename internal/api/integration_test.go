package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationServer provides a test HTTP server with mock dependencies
type TestIntegrationServer struct {
	server  *httptest.Server
	storage *MockStorage
}

// newTestIntegrationServer creates a test server with mock storage
func newTestIntegrationServer(t *testing.T) *TestIntegrationServer {
	mockStorage := NewMockStorage()

	// Create a minimal Server with just storage
	s := &Server{
		storageService: mockStorage,
	}

	// Create mux and register routes
	mux := http.NewServeMux()

	// Register routes that work without Docker
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/operations", s.handleOperations)
	mux.HandleFunc("GET /api/operations/{id}", s.handleOperationByID)
	mux.HandleFunc("GET /api/history", s.handleHistory)
	mux.HandleFunc("GET /api/policies", s.handlePolicies)

	testServer := httptest.NewServer(mux)

	return &TestIntegrationServer{
		server:  testServer,
		storage: mockStorage,
	}
}

func (ts *TestIntegrationServer) Close() {
	ts.server.Close()
}

func (ts *TestIntegrationServer) URL() string {
	return ts.server.URL
}

// Helper methods for HTTP requests
func (ts *TestIntegrationServer) GET(path string) (*http.Response, error) {
	return http.Get(ts.server.URL + path)
}

func (ts *TestIntegrationServer) POST(path string, body string) (*http.Response, error) {
	return http.Post(ts.server.URL+path, "application/json", strings.NewReader(body))
}

// parseJSONResponse parses JSON response body
func parseJSONResponse(t *testing.T, resp *http.Response) map[string]interface{} {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	defer resp.Body.Close()

	var result map[string]interface{}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err, "Failed to parse JSON: %s", string(body))
	return result
}

// ============================================================================
// Integration Tests - Health Endpoint
// ============================================================================

func TestIntegration_Health(t *testing.T) {
	ts := newTestIntegrationServer(t)
	defer ts.Close()

	resp, err := ts.GET("/api/health")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)

	result := parseJSONResponse(t, resp)
	assert.True(t, result["success"].(bool))

	data := result["data"].(map[string]interface{})
	assert.Equal(t, "healthy", data["status"])
}

// ============================================================================
// Integration Tests - Operations Endpoint
// ============================================================================

func TestIntegration_Operations(t *testing.T) {
	t.Run("empty operations list", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		resp, err := ts.GET("/api/operations")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		assert.True(t, result["success"].(bool))

		data := result["data"].(map[string]interface{})
		assert.Equal(t, float64(0), data["count"])
	})

	t.Run("returns operations", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		// Add test data
		now := time.Now()
		ts.storage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-001",
			ContainerName: "nginx",
			Status:        "complete",
			OldVersion:    "1.24.0",
			NewVersion:    "1.25.0",
			CreatedAt:     now,
		})
		ts.storage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-002",
			ContainerName: "redis",
			Status:        "failed",
			ErrorMessage:  "pull failed",
			CreatedAt:     now.Add(-time.Hour),
		})

		resp, err := ts.GET("/api/operations")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		data := result["data"].(map[string]interface{})
		assert.Equal(t, float64(2), data["count"])

		operations := data["operations"].([]interface{})
		assert.Len(t, operations, 2)
	})

	t.Run("filters by status", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		ts.storage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-001",
			ContainerName: "nginx",
			Status:        "complete",
		})
		ts.storage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-002",
			ContainerName: "redis",
			Status:        "failed",
		})

		resp, err := ts.GET("/api/operations?status=failed")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		data := result["data"].(map[string]interface{})
		operations := data["operations"].([]interface{})
		assert.Len(t, operations, 1)
		assert.Equal(t, "failed", operations[0].(map[string]interface{})["status"])
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		for i := 0; i < 10; i++ {
			ts.storage.AddOperation(storage.UpdateOperation{
				OperationID:   fmt.Sprintf("op-%03d", i),
				ContainerName: "nginx",
				Status:        "complete",
			})
		}

		resp, err := ts.GET("/api/operations?limit=3")
		require.NoError(t, err)
		defer resp.Body.Close()

		result := parseJSONResponse(t, resp)
		data := result["data"].(map[string]interface{})
		operations := data["operations"].([]interface{})
		assert.Len(t, operations, 3)
	})
}

func TestIntegration_OperationByID(t *testing.T) {
	t.Run("returns operation when found", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		ts.storage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-123",
			ContainerName: "nginx",
			Status:        "complete",
			OldVersion:    "1.24.0",
			NewVersion:    "1.25.0",
		})

		resp, err := ts.GET("/api/operations/op-123")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		assert.True(t, result["success"].(bool))

		data := result["data"].(map[string]interface{})
		assert.Equal(t, "op-123", data["operation_id"])
		assert.Equal(t, "nginx", data["container_name"])
	})

	t.Run("returns 404 for non-existent operation", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		resp, err := ts.GET("/api/operations/nonexistent")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

// ============================================================================
// Integration Tests - History Endpoint
// ============================================================================

func TestIntegration_History(t *testing.T) {
	t.Run("returns merged check and update history", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		now := time.Now()
		ts.storage.AddCheckHistory(storage.CheckHistoryEntry{
			ContainerName:  "nginx",
			Image:          "nginx:1.24",
			CurrentVersion: "1.24.0",
			LatestVersion:  "1.25.0",
			Status:         "UPDATE_AVAILABLE",
			CheckTime:      now,
		})
		ts.storage.AddUpdateLog(storage.UpdateLogEntry{
			ContainerName: "nginx",
			Operation:     "update",
			FromVersion:   "1.24.0",
			ToVersion:     "1.25.0",
			Success:       true,
			Timestamp:     now.Add(time.Minute),
		})

		resp, err := ts.GET("/api/history")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		data := result["data"].(map[string]interface{})
		history := data["history"].([]interface{})
		assert.Len(t, history, 2)
	})

	t.Run("filters by type=check", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		now := time.Now()
		ts.storage.AddCheckHistory(storage.CheckHistoryEntry{
			ContainerName: "nginx",
			Status:        "UP_TO_DATE",
			CheckTime:     now,
		})
		ts.storage.AddUpdateLog(storage.UpdateLogEntry{
			ContainerName: "nginx",
			Operation:     "update",
			Success:       true,
			Timestamp:     now,
		})

		resp, err := ts.GET("/api/history?type=check")
		require.NoError(t, err)
		defer resp.Body.Close()

		result := parseJSONResponse(t, resp)
		data := result["data"].(map[string]interface{})
		history := data["history"].([]interface{})
		assert.Len(t, history, 1)
		assert.Equal(t, "check", history[0].(map[string]interface{})["type"])
	})

	t.Run("filters by type=update", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		now := time.Now()
		ts.storage.AddCheckHistory(storage.CheckHistoryEntry{
			ContainerName: "nginx",
			Status:        "UP_TO_DATE",
			CheckTime:     now,
		})
		ts.storage.AddUpdateLog(storage.UpdateLogEntry{
			ContainerName: "nginx",
			Operation:     "update",
			Success:       true,
			Timestamp:     now,
		})

		resp, err := ts.GET("/api/history?type=update")
		require.NoError(t, err)
		defer resp.Body.Close()

		result := parseJSONResponse(t, resp)
		data := result["data"].(map[string]interface{})
		history := data["history"].([]interface{})
		assert.Len(t, history, 1)
		assert.Equal(t, "update", history[0].(map[string]interface{})["type"])
	})
}

// ============================================================================
// Integration Tests - Policies Endpoint
// ============================================================================

func TestIntegration_Policies(t *testing.T) {
	t.Run("returns policies", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		ts.storage.SetRollbackPolicy(context.Background(), storage.RollbackPolicy{
			EntityType:          "global",
			EntityID:            "",
			AutoRollbackEnabled: true,
			HealthCheckRequired: true,
		})

		resp, err := ts.GET("/api/policies")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		assert.True(t, result["success"].(bool))
	})
}

// ============================================================================
// Integration Tests - Error Handling
// ============================================================================

func TestIntegration_StorageErrors(t *testing.T) {
	t.Run("operations returns error on storage failure", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		ts.storage.GetError = fmt.Errorf("database connection failed")

		resp, err := ts.GET("/api/operations")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		assert.False(t, result["success"].(bool))
		assert.Contains(t, result["error"].(string), "database connection failed")
	})

	t.Run("history returns error on storage failure", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		ts.storage.GetError = fmt.Errorf("database connection failed")

		resp, err := ts.GET("/api/history")
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		assert.False(t, result["success"].(bool))
	})
}

// ============================================================================
// Integration Tests - Multiple Requests
// ============================================================================

func TestIntegration_MultipleRequests(t *testing.T) {
	ts := newTestIntegrationServer(t)
	defer ts.Close()

	// Add some test data
	for i := 0; i < 5; i++ {
		ts.storage.AddOperation(storage.UpdateOperation{
			OperationID:   fmt.Sprintf("op-%03d", i),
			ContainerName: "nginx",
			Status:        "complete",
		})
	}

	// Make multiple requests to verify server handles them correctly
	for i := 0; i < 10; i++ {
		resp, err := ts.GET("/api/operations")
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		result := parseJSONResponse(t, resp)
		assert.True(t, result["success"].(bool))
		resp.Body.Close()
	}
}

// ============================================================================
// Integration Tests - Concurrent Requests
// ============================================================================

func TestIntegration_ConcurrentRequests(t *testing.T) {
	t.Run("concurrent GET operations", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		// Add test data
		for i := 0; i < 20; i++ {
			ts.storage.AddOperation(storage.UpdateOperation{
				OperationID:   fmt.Sprintf("op-%03d", i),
				ContainerName: fmt.Sprintf("container-%d", i%5),
				Status:        "complete",
			})
		}

		// Run 20 concurrent GET requests
		var wg sync.WaitGroup
		errors := make(chan error, 20)

		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := ts.GET("/api/operations")
				if err != nil {
					errors <- err
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("expected 200, got %d", resp.StatusCode)
					return
				}

				result := parseJSONResponse(t, resp)
				if success, ok := result["success"].(bool); !ok || !success {
					errors <- fmt.Errorf("request failed")
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("concurrent request failed: %v", err)
		}
	})

	t.Run("concurrent requests to different endpoints", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		// Add test data
		now := time.Now()
		for i := 0; i < 10; i++ {
			ts.storage.AddOperation(storage.UpdateOperation{
				OperationID:   fmt.Sprintf("op-%03d", i),
				ContainerName: "nginx",
				Status:        "complete",
			})
			ts.storage.AddCheckHistory(storage.CheckHistoryEntry{
				ContainerName: "nginx",
				Status:        "UP_TO_DATE",
				CheckTime:     now,
			})
		}

		var wg sync.WaitGroup
		errors := make(chan error, 30)

		// 10 requests to /api/health
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := ts.GET("/api/health")
				if err != nil {
					errors <- err
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("health: expected 200, got %d", resp.StatusCode)
				}
			}()
		}

		// 10 requests to /api/operations
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := ts.GET("/api/operations")
				if err != nil {
					errors <- err
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("operations: expected 200, got %d", resp.StatusCode)
				}
			}()
		}

		// 10 requests to /api/history
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := ts.GET("/api/history")
				if err != nil {
					errors <- err
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("history: expected 200, got %d", resp.StatusCode)
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("concurrent request failed: %v", err)
		}
	})

	t.Run("concurrent reads and writes to storage", func(t *testing.T) {
		ts := newTestIntegrationServer(t)
		defer ts.Close()

		var wg sync.WaitGroup
		errors := make(chan error, 100)

		// Concurrent writes
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				ts.storage.AddOperation(storage.UpdateOperation{
					OperationID:   fmt.Sprintf("op-%03d", idx),
					ContainerName: fmt.Sprintf("container-%d", idx),
					Status:        "complete",
				})
			}(i)
		}

		// Concurrent reads while writing
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := ts.GET("/api/operations")
				if err != nil {
					errors <- err
					return
				}
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("expected 200, got %d", resp.StatusCode)
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("concurrent read/write failed: %v", err)
		}
	})
}

func TestIntegration_ConcurrentOperationByID(t *testing.T) {
	ts := newTestIntegrationServer(t)
	defer ts.Close()

	// Add specific operations
	for i := 0; i < 10; i++ {
		ts.storage.AddOperation(storage.UpdateOperation{
			OperationID:   fmt.Sprintf("op-%03d", i),
			ContainerName: "nginx",
			Status:        "complete",
			OldVersion:    "1.24.0",
			NewVersion:    "1.25.0",
		})
	}

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent requests to different operation IDs
	for i := 0; i < 10; i++ {
		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func(opIdx int) {
				defer wg.Done()
				opID := fmt.Sprintf("op-%03d", opIdx)
				resp, err := ts.GET("/api/operations/" + opID)
				if err != nil {
					errors <- err
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					errors <- fmt.Errorf("operation %s: expected 200, got %d", opID, resp.StatusCode)
					return
				}

				result := parseJSONResponse(t, resp)
				data := result["data"].(map[string]interface{})
				if data["operation_id"] != opID {
					errors <- fmt.Errorf("operation %s: got wrong ID %s", opID, data["operation_id"])
				}
			}(i)
		}
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent operation lookup failed: %v", err)
	}
}
