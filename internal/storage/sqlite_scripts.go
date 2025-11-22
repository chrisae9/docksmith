package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// SaveScriptAssignment implements Storage.SaveScriptAssignment.
// Creates or updates container settings (script, ignore, allow_latest).
func (s *SQLiteStorage) SaveScriptAssignment(ctx context.Context, assignment ScriptAssignment) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `
			INSERT OR REPLACE INTO script_assignments
			(container_name, script_path, enabled, ignore, allow_latest, assigned_by, assigned_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, COALESCE((SELECT assigned_at FROM script_assignments WHERE container_name = ?), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		`

		_, err := s.db.ExecContext(ctx, query,
			assignment.ContainerName, assignment.ScriptPath, assignment.Enabled, assignment.Ignore, assignment.AllowLatest, assignment.AssignedBy, assignment.ContainerName)
		if err != nil {
			log.Printf("Failed to save container settings for %s: %v", assignment.ContainerName, err)
			return fmt.Errorf("failed to save container settings: %w", err)
		}

		log.Printf("Saved container settings: container=%s, script=%s, ignore=%v, allow_latest=%v",
			assignment.ContainerName, assignment.ScriptPath, assignment.Ignore, assignment.AllowLatest)
		return nil
	})
}

// GetScriptAssignment implements Storage.GetScriptAssignment.
// Retrieves container settings for a specific container.
func (s *SQLiteStorage) GetScriptAssignment(ctx context.Context, containerName string) (ScriptAssignment, bool, error) {
	query := `
		SELECT id, container_name, script_path, enabled, ignore, allow_latest, assigned_at, assigned_by, updated_at
		FROM script_assignments
		WHERE container_name = ?
	`

	var assignment ScriptAssignment
	var assignedBy sql.NullString

	err := s.db.QueryRowContext(ctx, query, containerName).Scan(
		&assignment.ID, &assignment.ContainerName, &assignment.ScriptPath,
		&assignment.Enabled, &assignment.Ignore, &assignment.AllowLatest, &assignment.AssignedAt, &assignedBy, &assignment.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return ScriptAssignment{}, false, nil
	}
	if err != nil {
		log.Printf("Failed to query script assignment for %s: %v", containerName, err)
		return ScriptAssignment{}, false, fmt.Errorf("failed to query script assignment: %w", err)
	}

	if assignedBy.Valid {
		assignment.AssignedBy = assignedBy.String
	}

	log.Printf("Retrieved script assignment: container=%s, script=%s", assignment.ContainerName, assignment.ScriptPath)
	return assignment, true, nil
}

// ListScriptAssignments implements Storage.ListScriptAssignments.
// Retrieves all container settings ordered by container_name.
func (s *SQLiteStorage) ListScriptAssignments(ctx context.Context, enabledOnly bool) ([]ScriptAssignment, error) {
	var query string
	if enabledOnly {
		query = `
			SELECT id, container_name, script_path, enabled, ignore, allow_latest, assigned_at, assigned_by, updated_at
			FROM script_assignments
			WHERE enabled = 1
			ORDER BY container_name
		`
	} else {
		query = `
			SELECT id, container_name, script_path, enabled, ignore, allow_latest, assigned_at, assigned_by, updated_at
			FROM script_assignments
			ORDER BY container_name
		`
	}

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Printf("Failed to query container settings: %v", err)
		return nil, fmt.Errorf("failed to query container settings: %w", err)
	}
	defer rows.Close()

	return scanScriptAssignmentRows(rows)
}

// DeleteScriptAssignment implements Storage.DeleteScriptAssignment.
// Removes the script assignment for a container.
func (s *SQLiteStorage) DeleteScriptAssignment(ctx context.Context, containerName string) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `DELETE FROM script_assignments WHERE container_name = ?`

		result, err := s.db.ExecContext(ctx, query, containerName)
		if err != nil {
			log.Printf("Failed to delete script assignment for %s: %v", containerName, err)
			return fmt.Errorf("failed to delete script assignment: %w", err)
		}

		rowsAffected, _ := result.RowsAffected()
		if rowsAffected == 0 {
			return fmt.Errorf("no script assignment found for container %s", containerName)
		}

		log.Printf("Deleted script assignment for container: %s", containerName)
		return nil
	})
}
