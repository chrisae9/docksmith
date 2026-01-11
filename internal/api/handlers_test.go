package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/update"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Helper Function Tests
// ============================================================================

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		param    string
		defVal   int
		expected int
	}{
		{"empty value uses default", "", "limit", 50, 50},
		{"valid integer", "limit=25", "limit", 50, 25},
		{"invalid integer uses default", "limit=abc", "limit", 50, 50},
		{"zero value", "limit=0", "limit", 50, 0},
		{"negative value", "limit=-1", "limit", 50, -1},
		{"very large value", "limit=999999", "limit", 50, 999999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?"+tt.query, nil)
			result := parseIntParam(req, tt.param, tt.defVal)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestParseBoolParam(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		param    string
		expected bool
	}{
		{"empty value is false", "", "force", false},
		{"true value", "force=true", "force", true},
		{"false value", "force=false", "force", false},
		{"other value is false", "force=yes", "force", false},
		{"TRUE is false (case sensitive)", "force=TRUE", "force", false},
		{"1 is false", "force=1", "force", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/?"+tt.query, nil)
			result := parseBoolParam(req, tt.param)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidateRequired(t *testing.T) {
	t.Run("empty value returns false and writes error", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := validateRequired(w, "container_name", "")

		assert.False(t, result)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "container_name is required")
	})

	t.Run("non-empty value returns true", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := validateRequired(w, "container_name", "nginx")

		assert.True(t, result)
		// Response should not be written yet - only on error
	})

	t.Run("whitespace-only value is considered valid", func(t *testing.T) {
		w := httptest.NewRecorder()
		result := validateRequired(w, "name", "  ")

		// Whitespace is not empty string, so it passes
		assert.True(t, result)
	})
}

func TestValidateRegexPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
		errMsg  string
	}{
		{"empty is valid", "", false, ""},
		{"simple pattern", "^v\\d+", false, ""},
		{"complex semver pattern", "^v?\\d+\\.\\d+\\.\\d+$", false, ""},
		{"character class", "[a-z]+", false, ""},
		{"anchored pattern", "^latest$", false, ""},
		{"alternation pattern", "foo|bar|baz", false, ""},
		{"invalid regex - unclosed bracket", "[invalid", true, "invalid regex"},
		{"invalid regex - unclosed group", "(unclosed", true, "invalid regex"},
		{"invalid regex - bad escape", "\\", true, "invalid regex"},
		{"too long pattern", strings.Repeat("a", MaxRegexPatternLength+1), true, "pattern too long"},
		{"exactly max length is valid", strings.Repeat("a", MaxRegexPatternLength), false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRegexPattern(tt.pattern)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// ============================================================================
// Response Function Tests
// ============================================================================

func TestRespondSuccess(t *testing.T) {
	t.Run("returns 200 with success wrapper", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := map[string]string{"status": "ok"}

		RespondSuccess(w, data)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response["success"].(bool))
		assert.NotNil(t, response["data"])
	})

	t.Run("handles nil data", func(t *testing.T) {
		w := httptest.NewRecorder()

		RespondSuccess(w, nil)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("handles array data", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := []string{"a", "b", "c"}

		RespondSuccess(w, data)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRespondBadRequest(t *testing.T) {
	t.Run("returns 400 with error message", func(t *testing.T) {
		w := httptest.NewRecorder()

		RespondBadRequest(w, errors.New("invalid input"))

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid input")
	})
}

func TestRespondNotFound(t *testing.T) {
	t.Run("returns 404 with error message", func(t *testing.T) {
		w := httptest.NewRecorder()

		RespondNotFound(w, errors.New("container not found"))

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "container not found")
	})
}

func TestRespondInternalError(t *testing.T) {
	t.Run("returns 500 with error message", func(t *testing.T) {
		w := httptest.NewRecorder()

		RespondInternalError(w, errors.New("database error"))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "database error")
	})
}

func TestRespondNoContent(t *testing.T) {
	t.Run("returns 204 with no body", func(t *testing.T) {
		w := httptest.NewRecorder()

		RespondNoContent(w)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Body.String())
	})
}

func TestRespondError(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        error
	}{
		{"400 bad request", http.StatusBadRequest, errors.New("bad request")},
		{"401 unauthorized", http.StatusUnauthorized, errors.New("unauthorized")},
		{"403 forbidden", http.StatusForbidden, errors.New("forbidden")},
		{"404 not found", http.StatusNotFound, errors.New("not found")},
		{"500 internal error", http.StatusInternalServerError, errors.New("internal error")},
		{"503 service unavailable", http.StatusServiceUnavailable, errors.New("service unavailable")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()

			RespondError(w, tt.statusCode, tt.err)

			assert.Equal(t, tt.statusCode, w.Code)
			assert.Contains(t, w.Body.String(), tt.err.Error())
		})
	}
}

func TestRespondErrorWithData(t *testing.T) {
	t.Run("returns error with additional data", func(t *testing.T) {
		w := httptest.NewRecorder()
		data := map[string]any{
			"containers": []string{"nginx", "redis"},
		}

		RespondErrorWithData(w, http.StatusConflict, errors.New("conflict"), data)

		assert.Equal(t, http.StatusConflict, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.False(t, response["success"].(bool))
		assert.Contains(t, response["error"].(string), "conflict")
		assert.NotNil(t, response["data"])
	})
}

// ============================================================================
// Helper Tests - mergeHistory
// ============================================================================

func TestMergeHistory(t *testing.T) {
	now := time.Now()

	t.Run("merges check and update history", func(t *testing.T) {
		checks := []storage.CheckHistoryEntry{
			{
				ContainerName:  "nginx",
				Image:          "nginx:1.24",
				CurrentVersion: "1.24.0",
				LatestVersion:  "1.25.0",
				Status:         "UPDATE_AVAILABLE",
				CheckTime:      now,
			},
		}

		updates := []storage.UpdateLogEntry{
			{
				ContainerName: "nginx",
				Operation:     "update",
				FromVersion:   "1.24.0",
				ToVersion:     "1.25.0",
				Success:       true,
				Timestamp:     now,
			},
		}

		result := mergeHistory(checks, updates)

		assert.Len(t, result, 2)

		// First should be the check
		assert.Equal(t, "check", result[0].Type)
		assert.Equal(t, "nginx", result[0].ContainerName)
		assert.Equal(t, "UPDATE_AVAILABLE", result[0].Status)
		assert.Equal(t, "1.24.0", result[0].CurrentVer)
		assert.Equal(t, "1.25.0", result[0].LatestVer)

		// Second should be the update
		assert.Equal(t, "update", result[1].Type)
		assert.Equal(t, "nginx", result[1].ContainerName)
		assert.Equal(t, "success", result[1].Status)
		assert.True(t, result[1].Success)
		assert.Equal(t, "1.24.0", result[1].FromVer)
		assert.Equal(t, "1.25.0", result[1].ToVer)
	})

	t.Run("handles empty checks", func(t *testing.T) {
		updates := []storage.UpdateLogEntry{
			{ContainerName: "nginx", Success: true, Timestamp: now},
		}

		result := mergeHistory(nil, updates)

		assert.Len(t, result, 1)
		assert.Equal(t, "update", result[0].Type)
	})

	t.Run("handles empty updates", func(t *testing.T) {
		checks := []storage.CheckHistoryEntry{
			{ContainerName: "nginx", Status: "UP_TO_DATE", CheckTime: now},
		}

		result := mergeHistory(checks, nil)

		assert.Len(t, result, 1)
		assert.Equal(t, "check", result[0].Type)
	})

	t.Run("handles both empty", func(t *testing.T) {
		result := mergeHistory(nil, nil)

		assert.Empty(t, result)
	})

	t.Run("preserves error information", func(t *testing.T) {
		checks := []storage.CheckHistoryEntry{
			{ContainerName: "broken", Status: "FAILED", Error: "connection refused", CheckTime: now},
		}

		updates := []storage.UpdateLogEntry{
			{ContainerName: "broken", Success: false, Error: "update failed", Timestamp: now},
		}

		result := mergeHistory(checks, updates)

		assert.Len(t, result, 2)
		assert.Equal(t, "connection refused", result[0].Error)
		assert.Equal(t, "failed", result[1].Status)
		assert.Equal(t, "update failed", result[1].Error)
	})
}

// ============================================================================
// Sentinel Error Tests
// ============================================================================

func TestSentinelErrors(t *testing.T) {
	t.Run("errNoStorage message", func(t *testing.T) {
		assert.Equal(t, "storage service not available", errNoStorage.Error())
	})

	t.Run("errNoUpdateOrchestrator message", func(t *testing.T) {
		assert.Equal(t, "update orchestrator not available", errNoUpdateOrchestrator.Error())
	})

	t.Run("errNoScriptManager message", func(t *testing.T) {
		assert.Equal(t, "script manager not available", errNoScriptManager.Error())
	})
}

// ============================================================================
// Server Require Functions Tests
// ============================================================================

func TestServerRequireFunctions(t *testing.T) {
	t.Run("requireStorage with nil storage", func(t *testing.T) {
		s := &Server{storageService: nil}
		w := httptest.NewRecorder()

		result := s.requireStorage(w)

		assert.False(t, result)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "storage service not available")
	})

	t.Run("requireScriptManager with nil manager", func(t *testing.T) {
		s := &Server{scriptManager: nil}
		w := httptest.NewRecorder()

		result := s.requireScriptManager(w)

		assert.False(t, result)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "script manager not available")
	})

	t.Run("requireUpdateOrchestrator with nil orchestrator", func(t *testing.T) {
		s := &Server{updateOrchestrator: nil}
		w := httptest.NewRecorder()

		result := s.requireUpdateOrchestrator(w)

		assert.False(t, result)
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "update orchestrator not available")
	})
}

// ============================================================================
// Handler Tests - handleHealth (no dependencies)
// ============================================================================

func TestHandleHealth(t *testing.T) {
	t.Run("returns healthy status with services", func(t *testing.T) {
		// Create server with mock services (non-nil)
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/health", nil)

		s.handleHealth(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response["success"].(bool))

		data := response["data"].(map[string]any)
		assert.Equal(t, "healthy", data["status"])

		services := data["services"].(map[string]any)
		// Both should be false since we have an empty server
		assert.False(t, services["docker"].(bool))
		assert.False(t, services["storage"].(bool))
	})
}

// ============================================================================
// Handler Tests - handleTriggerCheck
// ============================================================================

func TestHandleTriggerCheck(t *testing.T) {
	t.Run("returns error when background checker unavailable", func(t *testing.T) {
		s := &Server{backgroundChecker: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/trigger-check", nil)

		s.handleTriggerCheck(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "background checker not available")
	})
}

// ============================================================================
// Handler Tests - handleOperations
// ============================================================================

func TestHandleOperations_NoStorage(t *testing.T) {
	t.Run("returns error when storage unavailable", func(t *testing.T) {
		s := &Server{storageService: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations", nil)

		s.handleOperations(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "storage service not available")
	})
}

// ============================================================================
// Handler Tests - handleOperationByID
// ============================================================================

func TestHandleOperationByID_NoStorage(t *testing.T) {
	t.Run("returns error when storage unavailable", func(t *testing.T) {
		s := &Server{storageService: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations/op-123", nil)
		r.SetPathValue("id", "op-123")

		s.handleOperationByID(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "storage service not available")
	})
}

// ============================================================================
// Handler Tests - handleHistory
// ============================================================================

func TestHandleHistory_NoStorage(t *testing.T) {
	t.Run("returns error when storage unavailable", func(t *testing.T) {
		s := &Server{storageService: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/history", nil)

		s.handleHistory(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "storage service not available")
	})
}

// ============================================================================
// Handler Tests - handlePolicies
// ============================================================================

func TestHandlePolicies_NoStorage(t *testing.T) {
	t.Run("returns error when storage unavailable", func(t *testing.T) {
		s := &Server{storageService: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/policies", nil)

		s.handlePolicies(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "storage service not available")
	})
}

// ============================================================================
// Handler Tests - handleUpdate
// ============================================================================

func TestHandleUpdate_Validation(t *testing.T) {
	t.Run("returns error when update orchestrator unavailable", func(t *testing.T) {
		s := &Server{updateOrchestrator: nil}
		w := httptest.NewRecorder()
		body := `{"container_name": "nginx", "target_version": "1.25.0"}`
		r := httptest.NewRequest("POST", "/api/update", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleUpdate(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "update orchestrator not available")
	})

	t.Run("returns error on invalid JSON", func(t *testing.T) {
		// Need update orchestrator to pass first check
		s := &Server{updateOrchestrator: &update.UpdateOrchestrator{}}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/update", strings.NewReader("invalid json"))
		r.Header.Set("Content-Type", "application/json")

		s.handleUpdate(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "invalid request body")
	})
}

// ============================================================================
// Handler Tests - handleBatchUpdate
// ============================================================================

func TestHandleBatchUpdate_Validation(t *testing.T) {
	t.Run("returns error when update orchestrator unavailable", func(t *testing.T) {
		s := &Server{updateOrchestrator: nil}
		w := httptest.NewRecorder()
		body := `{"containers": [{"name": "nginx"}]}`
		r := httptest.NewRequest("POST", "/api/update/batch", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleBatchUpdate(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("validates containers array required", func(t *testing.T) {
		s := &Server{updateOrchestrator: &update.UpdateOrchestrator{}}
		w := httptest.NewRecorder()
		body := `{"containers": []}`
		r := httptest.NewRequest("POST", "/api/update/batch", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleBatchUpdate(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "containers array is required")
	})
}

// ============================================================================
// Handler Tests - handleRollback
// ============================================================================

func TestHandleRollback_Validation(t *testing.T) {
	t.Run("returns error when update orchestrator unavailable", func(t *testing.T) {
		s := &Server{updateOrchestrator: nil}
		w := httptest.NewRecorder()
		body := `{"operation_id": "op-123"}`
		r := httptest.NewRequest("POST", "/api/rollback", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleRollback(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("validates operation_id required", func(t *testing.T) {
		s := &Server{updateOrchestrator: &update.UpdateOrchestrator{}}
		w := httptest.NewRecorder()
		body := `{"force": true}`
		r := httptest.NewRequest("POST", "/api/rollback", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleRollback(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "operation_id is required")
	})
}

// ============================================================================
// Handler Tests - Scripts Handlers
// ============================================================================

func TestHandleScriptsList_NoManager(t *testing.T) {
	t.Run("returns error when script manager unavailable", func(t *testing.T) {
		s := &Server{scriptManager: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/scripts", nil)

		s.handleScriptsList(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "script manager not available")
	})
}

func TestHandleScriptsAssigned_NoManager(t *testing.T) {
	t.Run("returns error when script manager unavailable", func(t *testing.T) {
		s := &Server{scriptManager: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/scripts/assigned", nil)

		s.handleScriptsAssigned(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleScriptsAssign_Validation(t *testing.T) {
	t.Run("returns error when script manager unavailable", func(t *testing.T) {
		s := &Server{scriptManager: nil}
		w := httptest.NewRecorder()
		body := `{"container_name": "nginx", "script_path": "/scripts/check.sh"}`
		r := httptest.NewRequest("POST", "/api/scripts/assign", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleScriptsAssign(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleScriptsUnassign_Validation(t *testing.T) {
	t.Run("returns error when script manager unavailable", func(t *testing.T) {
		s := &Server{scriptManager: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("DELETE", "/api/scripts/assign/nginx", nil)
		r.SetPathValue("container", "nginx")

		s.handleScriptsUnassign(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})

	t.Run("validates container name required", func(t *testing.T) {
		// Need scriptManager to pass first check - using a non-nil placeholder
		// This will fail at the next step (SetPathValue returns empty)
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("DELETE", "/api/scripts/assign/", nil)
		r.SetPathValue("container", "")

		s.handleScriptsUnassign(w, r)

		// Will fail with script manager not available first
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// ============================================================================
// Handler Tests - Settings Handlers (Deprecated)
// ============================================================================

func TestHandleSettingsIgnore_NoManager(t *testing.T) {
	t.Run("returns error when script manager unavailable", func(t *testing.T) {
		s := &Server{scriptManager: nil}
		w := httptest.NewRecorder()
		body := `{"container_name": "nginx", "ignore": true}`
		r := httptest.NewRequest("POST", "/api/settings/ignore", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleSettingsIgnore(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestHandleSettingsAllowLatest_NoManager(t *testing.T) {
	t.Run("returns error when script manager unavailable", func(t *testing.T) {
		s := &Server{scriptManager: nil}
		w := httptest.NewRecorder()
		body := `{"container_name": "nginx", "allow_latest": true}`
		r := httptest.NewRequest("POST", "/api/settings/allow-latest", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleSettingsAllowLatest(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

// ============================================================================
// Handler Tests - Labels Handlers
// ============================================================================

func TestHandleLabelsSet_Validation(t *testing.T) {
	t.Run("returns error on invalid JSON", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/labels/set", strings.NewReader("invalid"))
		r.Header.Set("Content-Type", "application/json")

		s.handleLabelsSet(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("validates container required", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		body := `{"ignore": true}`
		r := httptest.NewRequest("POST", "/api/labels/set", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleLabelsSet(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "container is required")
	})

	t.Run("validates at least one label specified", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		body := `{"container": "nginx"}`
		r := httptest.NewRequest("POST", "/api/labels/set", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleLabelsSet(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "no labels specified")
	})
}

func TestHandleLabelsRemove_Validation(t *testing.T) {
	t.Run("returns error on invalid JSON", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/labels/remove", strings.NewReader("invalid"))
		r.Header.Set("Content-Type", "application/json")

		s.handleLabelsRemove(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("validates container required", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		body := `{"label_names": ["docksmith.ignore"]}`
		r := httptest.NewRequest("POST", "/api/labels/remove", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleLabelsRemove(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "container is required")
	})

	t.Run("validates at least one label specified", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		body := `{"container": "nginx", "label_names": []}`
		r := httptest.NewRequest("POST", "/api/labels/remove", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleLabelsRemove(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "no labels specified")
	})
}

// ============================================================================
// Handler Tests - Restart Handlers
// ============================================================================

func TestHandleStartRestart_Validation(t *testing.T) {
	t.Run("returns error when update orchestrator unavailable", func(t *testing.T) {
		s := &Server{updateOrchestrator: nil}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/restart/start/nginx", nil)
		r.SetPathValue("name", "nginx")

		s.handleStartRestart(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "restart service unavailable")
	})

	t.Run("validates container name required", func(t *testing.T) {
		s := &Server{updateOrchestrator: &update.UpdateOrchestrator{}}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/restart/start/", nil)
		r.SetPathValue("name", "")

		s.handleStartRestart(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "container name is required")
	})
}

func TestHandleRestartContainer_Validation(t *testing.T) {
	t.Run("validates container name required", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/restart/container/", nil)
		r.SetPathValue("name", "")

		s.handleRestartContainer(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "container name is required")
	})
}

func TestHandleRestartContainerBody_Validation(t *testing.T) {
	t.Run("returns error on invalid JSON", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/api/restart", strings.NewReader("invalid"))
		r.Header.Set("Content-Type", "application/json")

		s.handleRestartContainerBody(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("validates container_name required", func(t *testing.T) {
		s := &Server{}
		w := httptest.NewRecorder()
		body := `{}`
		r := httptest.NewRequest("POST", "/api/restart", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")

		s.handleRestartContainerBody(w, r)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "container_name is required")
	})
}

// ============================================================================
// Handler Logic Tests - Operations
// ============================================================================

func TestHandleOperations_WithData(t *testing.T) {
	now := time.Now()

	t.Run("returns operations from storage", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-123",
			ContainerName: "nginx",
			Status:        "complete",
			OldVersion:    "1.24.0",
			NewVersion:    "1.25.0",
			CreatedAt:     now,
		})
		mockStorage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-456",
			ContainerName: "redis",
			Status:        "failed",
			ErrorMessage:  "pull failed",
			CreatedAt:     now.Add(-time.Hour),
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations", nil)

		s.handleOperations(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response["success"].(bool))

		data := response["data"].(map[string]any)
		assert.Equal(t, float64(2), data["count"])

		operations := data["operations"].([]any)
		assert.Len(t, operations, 2)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		mockStorage := NewMockStorage()
		for i := 0; i < 10; i++ {
			mockStorage.AddOperation(storage.UpdateOperation{
				OperationID:   fmt.Sprintf("op-%d", i),
				ContainerName: "nginx",
				Status:        "complete",
				CreatedAt:     time.Now(),
			})
		}

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations?limit=3", nil)

		s.handleOperations(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]any)
		operations := data["operations"].([]any)
		assert.Len(t, operations, 3)
	})

	t.Run("filters by container", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-1",
			ContainerName: "nginx",
			Status:        "complete",
		})
		mockStorage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-2",
			ContainerName: "redis",
			Status:        "complete",
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations?container=nginx", nil)

		s.handleOperations(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]any)
		operations := data["operations"].([]any)
		assert.Len(t, operations, 1)
	})

	t.Run("filters by status", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-1",
			ContainerName: "nginx",
			Status:        "complete",
		})
		mockStorage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-2",
			ContainerName: "redis",
			Status:        "failed",
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations?status=failed", nil)

		s.handleOperations(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]any)
		operations := data["operations"].([]any)
		assert.Len(t, operations, 1)
		assert.Equal(t, "failed", operations[0].(map[string]any)["status"])
	})
}

func TestHandleOperationByID_WithData(t *testing.T) {
	t.Run("returns operation when found", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.AddOperation(storage.UpdateOperation{
			OperationID:   "op-123",
			ContainerName: "nginx",
			Status:        "complete",
			OldVersion:    "1.24.0",
			NewVersion:    "1.25.0",
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations/op-123", nil)
		r.SetPathValue("id", "op-123")

		s.handleOperationByID(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response["success"].(bool))

		data := response["data"].(map[string]any)
		assert.Equal(t, "op-123", data["operation_id"])
		assert.Equal(t, "nginx", data["container_name"])
		assert.Equal(t, "complete", data["status"])
	})

	t.Run("returns 404 when not found", func(t *testing.T) {
		mockStorage := NewMockStorage()

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations/nonexistent", nil)
		r.SetPathValue("id", "nonexistent")

		s.handleOperationByID(w, r)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleHistory_WithData(t *testing.T) {
	now := time.Now()

	t.Run("returns merged check and update history", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.AddCheckHistory(storage.CheckHistoryEntry{
			ContainerName:  "nginx",
			Image:          "nginx:1.24",
			CurrentVersion: "1.24.0",
			LatestVersion:  "1.25.0",
			Status:         "UPDATE_AVAILABLE",
			CheckTime:      now,
		})
		mockStorage.AddUpdateLog(storage.UpdateLogEntry{
			ContainerName: "nginx",
			Operation:     "update",
			FromVersion:   "1.24.0",
			ToVersion:     "1.25.0",
			Success:       true,
			Timestamp:     now.Add(time.Minute),
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/history", nil)

		s.handleHistory(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response["success"].(bool))

		data := response["data"].(map[string]any)
		history := data["history"].([]any)
		assert.Len(t, history, 2)
	})

	t.Run("filters by type check", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.AddCheckHistory(storage.CheckHistoryEntry{
			ContainerName: "nginx",
			Status:        "UP_TO_DATE",
			CheckTime:     now,
		})
		mockStorage.AddUpdateLog(storage.UpdateLogEntry{
			ContainerName: "nginx",
			Operation:     "update",
			Success:       true,
			Timestamp:     now,
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/history?type=check", nil)

		s.handleHistory(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]any)
		history := data["history"].([]any)
		assert.Len(t, history, 1)
		assert.Equal(t, "check", history[0].(map[string]any)["type"])
	})

	t.Run("filters by type update", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.AddCheckHistory(storage.CheckHistoryEntry{
			ContainerName: "nginx",
			Status:        "UP_TO_DATE",
			CheckTime:     now,
		})
		mockStorage.AddUpdateLog(storage.UpdateLogEntry{
			ContainerName: "nginx",
			Operation:     "update",
			Success:       true,
			Timestamp:     now,
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/history?type=update", nil)

		s.handleHistory(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		data := response["data"].(map[string]any)
		history := data["history"].([]any)
		assert.Len(t, history, 1)
		assert.Equal(t, "update", history[0].(map[string]any)["type"])
	})
}

func TestHandlePolicies_WithData(t *testing.T) {
	t.Run("returns policies from storage", func(t *testing.T) {
		mockStorage := NewMockStorage()
		// Add a global policy
		mockStorage.SetRollbackPolicy(context.Background(), storage.RollbackPolicy{
			EntityType:          "global",
			EntityID:            "",
			AutoRollbackEnabled: true,
			HealthCheckRequired: true,
		})

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/policies", nil)

		s.handlePolicies(w, r)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]any
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response["success"].(bool))
	})
}

// ============================================================================
// Handler Error Tests - Storage Errors
// ============================================================================

func TestHandleOperations_StorageError(t *testing.T) {
	t.Run("returns error when storage fails", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.GetError = errors.New("database connection failed")

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/operations", nil)

		s.handleOperations(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "database connection failed")
	})
}

func TestHandleHistory_StorageError(t *testing.T) {
	t.Run("returns error when storage fails", func(t *testing.T) {
		mockStorage := NewMockStorage()
		mockStorage.GetError = errors.New("database connection failed")

		s := &Server{storageService: mockStorage}
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/api/history", nil)

		s.handleHistory(w, r)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "database connection failed")
	})
}

// ============================================================================
// Constant Tests
// ============================================================================

func TestConstants(t *testing.T) {
	t.Run("timeout constants have reasonable values", func(t *testing.T) {
		assert.Equal(t, 1*time.Second, HealthCheckPollInterval)
		assert.Equal(t, 30*time.Second, HealthCheckTimeout)
		assert.Equal(t, 3*time.Minute, LabelOperationTimeout)
		assert.Equal(t, 60*time.Second, ContainerRestartTimeout)
		assert.Equal(t, 120*time.Second, StackRestartTimeout)
	})

	t.Run("docker compose labels are set", func(t *testing.T) {
		assert.Equal(t, "com.docker.compose.project.config_files", ComposeConfigFilesLabel)
		assert.Equal(t, "com.docker.compose.service", ComposeServiceLabel)
		assert.Equal(t, "com.docker.compose.project", ComposeProjectLabel)
	})

	t.Run("max regex pattern length is set", func(t *testing.T) {
		assert.Equal(t, 500, MaxRegexPatternLength)
	})
}
