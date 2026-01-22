package storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

// scanCheckHistoryRows is a helper function that scans check history rows into a slice.
// It handles the nullable error field and ensures consistent error handling.
func scanCheckHistoryRows(rows *sql.Rows) ([]CheckHistoryEntry, error) {
	var history []CheckHistoryEntry
	for rows.Next() {
		var entry CheckHistoryEntry
		var errorMsg sql.NullString

		err := rows.Scan(
			&entry.ID,
			&entry.ContainerName,
			&entry.Image,
			&entry.CheckTime,
			&entry.CurrentVersion,
			&entry.LatestVersion,
			&entry.Status,
			&errorMsg,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan check history entry: %w", err)
		}

		if errorMsg.Valid {
			entry.Error = errorMsg.String
		}

		history = append(history, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating check history rows: %w", err)
	}

	return history, nil
}

// scanUpdateOperationRows scans multiple UpdateOperation rows and handles nullable fields
// This helper consolidates the duplicate row scanning logic used across multiple query methods
func scanUpdateOperationRows(rows *sql.Rows) ([]UpdateOperation, error) {
	operations := make([]UpdateOperation, 0)

	for rows.Next() {
		var op UpdateOperation
		var dependentsJSON string
		var batchDetailsJSON sql.NullString
		var startedAt, completedAt sql.NullTime
		var containerID, stackName, oldVersion, newVersion, errorMessage sql.NullString

		err := rows.Scan(
			&op.ID, &op.OperationID, &containerID, &op.ContainerName, &stackName, &op.OperationType, &op.Status,
			&oldVersion, &newVersion, &startedAt, &completedAt, &errorMessage,
			&dependentsJSON, &op.RollbackOccurred, &batchDetailsJSON, &op.CreatedAt, &op.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan update operation: %w", err)
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
				log.Printf("Failed to deserialize dependents affected: %v", err)
				return nil, fmt.Errorf("failed to deserialize dependents affected: %w", err)
			}
		}

		// Deserialize batch details from JSON
		if batchDetailsJSON.Valid && batchDetailsJSON.String != "" {
			err = json.Unmarshal([]byte(batchDetailsJSON.String), &op.BatchDetails)
			if err != nil {
				log.Printf("Failed to deserialize batch details: %v", err)
				return nil, fmt.Errorf("failed to deserialize batch details: %w", err)
			}
		}

		operations = append(operations, op)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating update operations rows: %w", err)
	}

	return operations, nil
}

// scanUpdateLogRows scans multiple UpdateLogEntry rows and handles nullable error field
// This helper consolidates the duplicate row scanning logic used across multiple query methods
func scanUpdateLogRows(rows *sql.Rows) ([]UpdateLogEntry, error) {
	logs := make([]UpdateLogEntry, 0)

	for rows.Next() {
		var entry UpdateLogEntry
		var errorMsg sql.NullString

		err := rows.Scan(
			&entry.ID, &entry.ContainerName, &entry.Operation, &entry.FromVersion,
			&entry.ToVersion, &entry.Timestamp, &entry.Success, &errorMsg,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan update log entry: %w", err)
		}

		if errorMsg.Valid {
			entry.Error = errorMsg.String
		}

		logs = append(logs, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating update log rows: %w", err)
	}

	return logs, nil
}

// scanScriptAssignmentRows scans multiple ScriptAssignment rows and handles nullable assigned_by field
// This helper consolidates row scanning logic for script assignment queries
func scanScriptAssignmentRows(rows *sql.Rows) ([]ScriptAssignment, error) {
	assignments := make([]ScriptAssignment, 0)

	for rows.Next() {
		var assignment ScriptAssignment
		var assignedBy sql.NullString

		err := rows.Scan(
			&assignment.ID, &assignment.ContainerName, &assignment.ScriptPath,
			&assignment.Enabled, &assignment.Ignore, &assignment.AllowLatest,
			&assignment.AssignedAt, &assignedBy, &assignment.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan script assignment: %w", err)
		}

		if assignedBy.Valid {
			assignment.AssignedBy = assignedBy.String
		}

		assignments = append(assignments, assignment)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating script assignments rows: %w", err)
	}

	return assignments, nil
}

// appendLimitClause appends a SQL LIMIT clause to the query if limit > 0
// This helper consolidates the duplicate limit clause building logic
func appendLimitClause(query string, limit int) string {
	if limit > 0 {
		return query + fmt.Sprintf(" LIMIT %d", limit)
	}
	return query
}
