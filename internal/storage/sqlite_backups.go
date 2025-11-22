package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// SaveComposeBackup implements Storage.SaveComposeBackup.
// Records metadata about a compose file backup.
func (s *SQLiteStorage) SaveComposeBackup(ctx context.Context, backup ComposeBackup) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `
			INSERT INTO compose_backups
			(operation_id, container_name, stack_name, compose_file_path, backup_file_path, backup_timestamp)
			VALUES (?, ?, ?, ?, ?, ?)
		`

		_, err := s.db.ExecContext(ctx, query,
			backup.OperationID, backup.ContainerName, backup.StackName,
			backup.ComposeFilePath, backup.BackupFilePath, backup.BackupTimestamp)
		if err != nil {
			log.Printf("Failed to save compose backup for operation %s: %v", backup.OperationID, err)
			return fmt.Errorf("failed to save compose backup: %w", err)
		}

		log.Printf("Saved compose backup: operation=%s, container=%s, path=%s", backup.OperationID, backup.ContainerName, backup.BackupFilePath)
		return nil
	})
}

// GetComposeBackup implements Storage.GetComposeBackup.
// Retrieves compose backup metadata by operation ID.
func (s *SQLiteStorage) GetComposeBackup(ctx context.Context, operationID string) (ComposeBackup, bool, error) {
	query := `
		SELECT id, operation_id, container_name, stack_name, compose_file_path, backup_file_path, backup_timestamp, created_at
		FROM compose_backups
		WHERE operation_id = ?
		ORDER BY created_at DESC
		LIMIT 1
	`

	var backup ComposeBackup
	var stackName sql.NullString

	err := s.db.QueryRowContext(ctx, query, operationID).Scan(
		&backup.ID, &backup.OperationID, &backup.ContainerName, &stackName,
		&backup.ComposeFilePath, &backup.BackupFilePath, &backup.BackupTimestamp, &backup.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return ComposeBackup{}, false, nil
	}
	if err != nil {
		log.Printf("Failed to query compose backup for operation %s: %v", operationID, err)
		return ComposeBackup{}, false, fmt.Errorf("failed to query compose backup: %w", err)
	}

	if stackName.Valid {
		backup.StackName = stackName.String
	}

	log.Printf("Retrieved compose backup: operation=%s, path=%s", backup.OperationID, backup.BackupFilePath)
	return backup, true, nil
}

// GetComposeBackupsByContainer retrieves all backups for a specific container.
// Returns entries ordered by backup_timestamp DESC (most recent first).
func (s *SQLiteStorage) GetComposeBackupsByContainer(ctx context.Context, containerName string) ([]ComposeBackup, error) {
	query := `
		SELECT id, operation_id, container_name, stack_name, compose_file_path, backup_file_path, backup_timestamp, created_at
		FROM compose_backups
		WHERE container_name = ?
		ORDER BY backup_timestamp DESC
	`

	rows, err := s.db.QueryContext(ctx, query, containerName)
	if err != nil {
		log.Printf("Failed to query compose backups for %s: %v", containerName, err)
		return nil, fmt.Errorf("failed to query compose backups: %w", err)
	}
	defer rows.Close()

	return scanComposeBackupRows(rows)
}

// GetAllComposeBackups retrieves all compose backups.
// Returns entries ordered by backup_timestamp DESC (most recent first).
func (s *SQLiteStorage) GetAllComposeBackups(ctx context.Context, limit int) ([]ComposeBackup, error) {
	query := appendLimitClause(`
		SELECT id, operation_id, container_name, stack_name, compose_file_path, backup_file_path, backup_timestamp, created_at
		FROM compose_backups
		ORDER BY backup_timestamp DESC
	`, limit)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Printf("Failed to query all compose backups: %v", err)
		return nil, fmt.Errorf("failed to query all compose backups: %w", err)
	}
	defer rows.Close()

	return scanComposeBackupRows(rows)
}
