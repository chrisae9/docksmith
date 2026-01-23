package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// LogUpdate implements Storage.LogUpdate.
// Records an update operation in the audit log (append-only).
// Validates that operation is one of: pull, restart, rollback.
func (s *SQLiteStorage) LogUpdate(ctx context.Context, containerName, operation, fromVer, toVer string, success bool, updateErr error) error {
	// Validate operation type
	validOperations := map[string]bool{
		"pull":     true,
		"restart":  true,
		"rollback": true,
	}

	if !validOperations[operation] {
		return fmt.Errorf("invalid operation: %s (must be one of: pull, restart, rollback)", operation)
	}

	return s.retryWithBackoff(ctx, func() error {
		query := `
			INSERT INTO update_log
			(container_name, operation, from_version, to_version, success, error)
			VALUES (?, ?, ?, ?, ?, ?)
		`

		var errorMsg string
		if updateErr != nil {
			errorMsg = updateErr.Error()
		}

		_, err := s.db.ExecContext(ctx, query, containerName, operation, fromVer, toVer, success, errorMsg)
		if err != nil {
			log.Printf("Failed to log update for %s: %v", containerName, err)
			return fmt.Errorf("failed to log update: %w", err)
		}

		log.Printf("Logged update: %s [%s] %s -> %s (success: %v)", containerName, operation, fromVer, toVer, success)
		return nil
	})
}

// GetUpdateLog retrieves update log for a specific container.
// Returns entries ordered by timestamp DESC (most recent first).
// Supports pagination via limit parameter.
func (s *SQLiteStorage) GetUpdateLog(ctx context.Context, containerName string, limit int) ([]UpdateLogEntry, error) {
	query := `
		SELECT id, container_name, operation, from_version, to_version, timestamp, success, error
		FROM update_log
		WHERE container_name = ?
		ORDER BY timestamp DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, containerName, limit)
	if err != nil {
		log.Printf("Failed to query update log for %s: %v", containerName, err)
		return nil, fmt.Errorf("failed to query update log: %w", err)
	}
	defer rows.Close()

	return scanUpdateLogRows(rows)
}

// GetAllUpdateLog retrieves update log for all containers.
// Returns entries ordered by timestamp DESC (most recent first).
func (s *SQLiteStorage) GetAllUpdateLog(ctx context.Context, limit int) ([]UpdateLogEntry, error) {
	baseQuery := `
		SELECT id, container_name, operation, from_version, to_version, timestamp, success, error
		FROM update_log
		ORDER BY timestamp DESC
	`
	query, args := withLimit(baseQuery, nil, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("Failed to query all update log: %v", err)
		return nil, fmt.Errorf("failed to query all update log: %w", err)
	}
	defer rows.Close()

	return scanUpdateLogRows(rows)
}

// SaveUpdateOperation implements Storage.SaveUpdateOperation.
// Creates or updates an update operation record using INSERT OR REPLACE.
// Serializes DependentsAffected as JSON array.
func (s *SQLiteStorage) SaveUpdateOperation(ctx context.Context, op UpdateOperation) error {
	return s.retryWithBackoff(ctx, func() error {
		// Serialize dependents affected to JSON
		dependentsJSON, err := json.Marshal(op.DependentsAffected)
		if err != nil {
			log.Printf("Failed to serialize dependents affected: %v", err)
			return fmt.Errorf("failed to serialize dependents affected: %w", err)
		}

		// Serialize batch details to JSON
		var batchDetailsJSON []byte
		if len(op.BatchDetails) > 0 {
			batchDetailsJSON, err = json.Marshal(op.BatchDetails)
			if err != nil {
				log.Printf("Failed to serialize batch details: %v", err)
				return fmt.Errorf("failed to serialize batch details: %w", err)
			}
		}

		query := `
			INSERT OR REPLACE INTO update_operations
			(operation_id, container_id, container_name, stack_name, operation_type, status,
			 old_version, new_version, started_at, completed_at, error_message,
			 dependents_affected, rollback_occurred, batch_details, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT created_at FROM update_operations WHERE operation_id = ?), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		`

		_, err = s.db.ExecContext(ctx, query,
			op.OperationID, op.ContainerID, op.ContainerName, op.StackName, op.OperationType, op.Status,
			op.OldVersion, op.NewVersion, op.StartedAt, op.CompletedAt, op.ErrorMessage,
			string(dependentsJSON), op.RollbackOccurred, string(batchDetailsJSON), op.OperationID)
		if err != nil {
			log.Printf("Failed to save update operation %s: %v", op.OperationID, err)
			return fmt.Errorf("failed to save update operation: %w", err)
		}

		log.Printf("Saved update operation: %s [%s] %s -> %s (status: %s)", op.OperationID, op.ContainerName, op.OldVersion, op.NewVersion, op.Status)
		return nil
	})
}

// GetUpdateOperation implements Storage.GetUpdateOperation.
// Retrieves an update operation by operation ID.
// Deserializes DependentsAffected from JSON array.
func (s *SQLiteStorage) GetUpdateOperation(ctx context.Context, operationID string) (UpdateOperation, bool, error) {
	query := `
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, batch_details, created_at, updated_at
		FROM update_operations
		WHERE operation_id = ?
	`

	var op UpdateOperation
	var dependentsJSON string
	var batchDetailsJSON sql.NullString
	var startedAt, completedAt sql.NullTime
	var containerID, stackName, oldVersion, newVersion, errorMessage sql.NullString

	err := s.db.QueryRowContext(ctx, query, operationID).Scan(
		&op.ID, &op.OperationID, &containerID, &op.ContainerName, &stackName, &op.OperationType, &op.Status,
		&oldVersion, &newVersion, &startedAt, &completedAt, &errorMessage,
		&dependentsJSON, &op.RollbackOccurred, &batchDetailsJSON, &op.CreatedAt, &op.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return UpdateOperation{}, false, nil
	}
	if err != nil {
		log.Printf("Failed to query update operation %s: %v", operationID, err)
		return UpdateOperation{}, false, fmt.Errorf("failed to query update operation: %w", err)
	}

	// Handle nullable fields
	if containerID.Valid {
		op.ContainerID = containerID.String
	}
	if stackName.Valid {
		op.StackName = stackName.String
	}
	if oldVersion.Valid {
		op.OldVersion = oldVersion.String
	}
	if newVersion.Valid {
		op.NewVersion = newVersion.String
	}
	if errorMessage.Valid {
		op.ErrorMessage = errorMessage.String
	}
	if startedAt.Valid {
		op.StartedAt = &startedAt.Time
	}
	if completedAt.Valid {
		op.CompletedAt = &completedAt.Time
	}

	// Deserialize dependents affected from JSON
	if dependentsJSON != "" {
		err = json.Unmarshal([]byte(dependentsJSON), &op.DependentsAffected)
		if err != nil {
			log.Printf("Failed to deserialize dependents affected for operation %s: %v", operationID, err)
			return UpdateOperation{}, false, fmt.Errorf("failed to deserialize dependents affected: %w", err)
		}
	}

	// Deserialize batch details from JSON
	if batchDetailsJSON.Valid && batchDetailsJSON.String != "" {
		err = json.Unmarshal([]byte(batchDetailsJSON.String), &op.BatchDetails)
		if err != nil {
			log.Printf("Failed to deserialize batch details for operation %s: %v", operationID, err)
			return UpdateOperation{}, false, fmt.Errorf("failed to deserialize batch details: %w", err)
		}
	}

	log.Printf("Retrieved update operation: %s [%s] (status: %s)", op.OperationID, op.ContainerName, op.Status)
	return op, true, nil
}

// GetUpdateOperationsByStatus implements Storage.GetUpdateOperationsByStatus.
// Retrieves update operations filtered by status.
// Returns entries ordered by created_at DESC (most recent first).
func (s *SQLiteStorage) GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]UpdateOperation, error) {
	baseQuery := `
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, batch_details, created_at, updated_at
		FROM update_operations
		WHERE status = ?
		ORDER BY created_at DESC
	`
	query, args := withLimit(baseQuery, []interface{}{status}, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("Failed to query update operations by status %s: %v", status, err)
		return nil, fmt.Errorf("failed to query update operations by status: %w", err)
	}
	defer rows.Close()

	return scanUpdateOperationRows(rows)
}

// GetUpdateOperationsByContainer retrieves update operations for a specific container.
// Returns entries ordered by started_at DESC (most recent first).
func (s *SQLiteStorage) GetUpdateOperationsByContainer(ctx context.Context, containerName string, limit int) ([]UpdateOperation, error) {
	baseQuery := `
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, batch_details, created_at, updated_at
		FROM update_operations
		WHERE container_name = ?
		ORDER BY started_at DESC
	`
	query, args := withLimit(baseQuery, []interface{}{containerName}, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("Failed to query update operations for %s: %v", containerName, err)
		return nil, fmt.Errorf("failed to query update operations: %w", err)
	}
	defer rows.Close()

	return scanUpdateOperationRows(rows)
}

// GetUpdateOperationsByTimeRange retrieves update operations within a time range.
// Returns entries ordered by started_at DESC (most recent first).
func (s *SQLiteStorage) GetUpdateOperationsByTimeRange(ctx context.Context, start, end time.Time) ([]UpdateOperation, error) {
	query := `
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, batch_details, created_at, updated_at
		FROM update_operations
		WHERE started_at >= ? AND started_at <= ?
		ORDER BY started_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, start, end)
	if err != nil {
		log.Printf("Failed to query update operations by time range: %v", err)
		return nil, fmt.Errorf("failed to query update operations by time range: %w", err)
	}
	defer rows.Close()

	return scanUpdateOperationRows(rows)
}

// UpdateOperationStatus implements Storage.UpdateOperationStatus.
// Updates the status and error message of an operation.
// Also updates the updated_at timestamp automatically.
func (s *SQLiteStorage) UpdateOperationStatus(ctx context.Context, operationID string, status string, errorMsg string) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `
			UPDATE update_operations
			SET status = ?, error_message = ?, updated_at = CURRENT_TIMESTAMP
			WHERE operation_id = ?
		`

		result, err := s.db.ExecContext(ctx, query, status, errorMsg, operationID)
		if err != nil {
			log.Printf("Failed to update operation status for %s: %v", operationID, err)
			return fmt.Errorf("failed to update operation status: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if affected == 0 {
			return fmt.Errorf("operation %s not found", operationID)
		}

		log.Printf("Updated operation status: %s -> %s", operationID, status)
		return nil
	})
}

// GetUpdateOperations implements Storage.GetUpdateOperations.
// Retrieves update operations for history display.
// Returns entries ordered by started_at DESC (most recent first).
// Only returns completed or failed operations (not queued/in-progress).
func (s *SQLiteStorage) GetUpdateOperations(ctx context.Context, limit int) ([]UpdateOperation, error) {
	baseQuery := `
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, batch_details, created_at, updated_at
		FROM update_operations
		WHERE status IN ('complete', 'failed')
		ORDER BY started_at DESC
	`
	query, args := withLimit(baseQuery, nil, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("Failed to query update operations: %v", err)
		return nil, fmt.Errorf("failed to query update operations: %w", err)
	}
	defer rows.Close()

	return scanUpdateOperationRows(rows)
}
