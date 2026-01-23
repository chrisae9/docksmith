package storage

import (
	"context"
	"fmt"
	"log"
	"time"
)

// LogCheck implements Storage.LogCheck.
// Records a check operation in the check_history table.
// Stores error message if checkErr is not nil.
func (s *SQLiteStorage) LogCheck(ctx context.Context, containerName, image, currentVer, latestVer, status string, checkErr error) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `
			INSERT INTO check_history
			(container_name, image, current_version, latest_version, status, error)
			VALUES (?, ?, ?, ?, ?, ?)
		`

		var errorMsg string
		if checkErr != nil {
			errorMsg = checkErr.Error()
		}

		_, err := s.db.ExecContext(ctx, query, containerName, image, currentVer, latestVer, status, errorMsg)
		if err != nil {
			log.Printf("Failed to log check for %s: %v", containerName, err)
			return fmt.Errorf("failed to log check: %w", err)
		}

		log.Printf("Logged check: %s [%s] -> %s", containerName, status, latestVer)
		return nil
	})
}

// LogCheckBatch implements batch logging for multiple containers.
// Uses a transaction for atomic batch insert - all entries succeed or all fail.
func (s *SQLiteStorage) LogCheckBatch(ctx context.Context, checks []CheckHistoryEntry) error {
	return s.retryWithBackoff(ctx, func() error {
		// Start transaction
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			log.Printf("Failed to begin transaction for batch check logging: %v", err)
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		// Prepare statement for efficiency
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO check_history
			(container_name, image, current_version, latest_version, status, error)
			VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to prepare statement: %w", err)
		}
		defer stmt.Close()

		// Insert each check entry
		for _, check := range checks {
			_, err = stmt.ExecContext(ctx, check.ContainerName, check.Image, check.CurrentVersion, check.LatestVersion, check.Status, check.Error)
			if err != nil {
				tx.Rollback()
				log.Printf("Failed to insert check entry for %s: %v", check.ContainerName, err)
				return fmt.Errorf("failed to insert check entry: %w", err)
			}
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Failed to commit batch check logging: %v", err)
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		log.Printf("Logged batch check: %d containers", len(checks))
		return nil
	})
}

// GetCheckHistory retrieves check history for a specific container.
// Returns entries ordered by check_time DESC (most recent first).
// Supports pagination via limit parameter.
func (s *SQLiteStorage) GetCheckHistory(ctx context.Context, containerName string, limit int) ([]CheckHistoryEntry, error) {
	query := `
		SELECT id, container_name, image, check_time, current_version, latest_version, status, error
		FROM check_history
		WHERE container_name = ?
		ORDER BY check_time DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, containerName, limit)
	if err != nil {
		log.Printf("Failed to query check history for %s: %v", containerName, err)
		return nil, fmt.Errorf("failed to query check history: %w", err)
	}
	defer rows.Close()

	return scanCheckHistoryRows(rows)
}

// GetCheckHistoryByTimeRange retrieves check history within a time range.
// Returns entries ordered by check_time DESC (most recent first).
func (s *SQLiteStorage) GetCheckHistoryByTimeRange(ctx context.Context, start, end time.Time) ([]CheckHistoryEntry, error) {
	query := `
		SELECT id, container_name, image, check_time, current_version, latest_version, status, error
		FROM check_history
		WHERE check_time >= ? AND check_time <= ?
		ORDER BY check_time DESC
	`

	rows, err := s.db.QueryContext(ctx, query, start, end)
	if err != nil {
		log.Printf("Failed to query check history by time range: %v", err)
		return nil, fmt.Errorf("failed to query check history by time range: %w", err)
	}
	defer rows.Close()

	return scanCheckHistoryRows(rows)
}

// GetAllCheckHistory retrieves check history for all containers.
// Returns entries ordered by check_time DESC (most recent first).
func (s *SQLiteStorage) GetAllCheckHistory(ctx context.Context, limit int) ([]CheckHistoryEntry, error) {
	baseQuery := `
		SELECT id, container_name, image, check_time, current_version, latest_version, status, error
		FROM check_history
		ORDER BY check_time DESC
	`
	query, args := withLimit(baseQuery, nil, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		log.Printf("Failed to query all check history: %v", err)
		return nil, fmt.Errorf("failed to query all check history: %w", err)
	}
	defer rows.Close()

	return scanCheckHistoryRows(rows)
}

// GetCheckHistorySince retrieves check history since a specific time.
// Returns entries ordered by check_time DESC (most recent first).
func (s *SQLiteStorage) GetCheckHistorySince(ctx context.Context, since time.Time) ([]CheckHistoryEntry, error) {
	query := `
		SELECT id, container_name, image, check_time, current_version, latest_version, status, error
		FROM check_history
		WHERE check_time >= ?
		ORDER BY check_time DESC
	`

	rows, err := s.db.QueryContext(ctx, query, since)
	if err != nil {
		log.Printf("Failed to query check history since %v: %v", since, err)
		return nil, fmt.Errorf("failed to query check history since: %w", err)
	}
	defer rows.Close()

	return scanCheckHistoryRows(rows)
}
