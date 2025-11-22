package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
)

// GetConfig implements Storage.GetConfig.
// Retrieves a configuration value by key.
// Returns empty string and false if key does not exist.
func (s *SQLiteStorage) GetConfig(ctx context.Context, key string) (string, bool, error) {
	var value string

	query := `
		SELECT value
		FROM config
		WHERE key = ?
	`

	err := s.db.QueryRowContext(ctx, query, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		log.Printf("Failed to query config for key %s: %v", key, err)
		return "", false, fmt.Errorf("failed to query config: %w", err)
	}

	log.Printf("Config retrieved: %s = %s", key, value)
	return value, true, nil
}

// SetConfig implements Storage.SetConfig.
// Stores a configuration value using INSERT OR REPLACE.
// Updates the updated_at timestamp automatically.
func (s *SQLiteStorage) SetConfig(ctx context.Context, key, value string) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `
			INSERT OR REPLACE INTO config
			(key, value, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
		`

		_, err := s.db.ExecContext(ctx, query, key, value)
		if err != nil {
			log.Printf("Failed to set config for key %s: %v", key, err)
			return fmt.Errorf("failed to set config: %w", err)
		}

		log.Printf("Config updated: %s = %s", key, value)
		return nil
	})
}

// SaveConfigSnapshot implements Storage.SaveConfigSnapshot.
// Saves a complete configuration snapshot with JSON serialization of config data.
// Used for configuration rollback and audit trail.
func (s *SQLiteStorage) SaveConfigSnapshot(ctx context.Context, snapshot ConfigSnapshot) error {
	return s.retryWithBackoff(ctx, func() error {
		// Serialize config data to JSON
		configJSON, err := json.Marshal(snapshot.ConfigData)
		if err != nil {
			log.Printf("Failed to serialize config data: %v", err)
			return fmt.Errorf("failed to serialize config data: %w", err)
		}

		query := `
			INSERT INTO config_history
			(snapshot_time, config_snapshot, changed_by)
			VALUES (?, ?, ?)
		`

		_, err = s.db.ExecContext(ctx, query, snapshot.SnapshotTime, string(configJSON), snapshot.ChangedBy)
		if err != nil {
			log.Printf("Failed to save config snapshot: %v", err)
			return fmt.Errorf("failed to save config snapshot: %w", err)
		}

		log.Printf("Saved config snapshot: changed_by=%s, keys=%d", snapshot.ChangedBy, len(snapshot.ConfigData))
		return nil
	})
}

// GetConfigHistory implements Storage.GetConfigHistory.
// Retrieves configuration snapshots ordered by snapshot_time DESC (most recent first).
// Supports pagination via limit parameter.
func (s *SQLiteStorage) GetConfigHistory(ctx context.Context, limit int) ([]ConfigSnapshot, error) {
	query := `
		SELECT id, snapshot_time, config_snapshot, changed_by, created_at
		FROM config_history
		ORDER BY snapshot_time DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, query, limit)
	if err != nil {
		log.Printf("Failed to query config history: %v", err)
		return nil, fmt.Errorf("failed to query config history: %w", err)
	}
	defer rows.Close()

	var history []ConfigSnapshot
	for rows.Next() {
		var snapshot ConfigSnapshot
		var configJSON string

		err := rows.Scan(
			&snapshot.ID,
			&snapshot.SnapshotTime,
			&configJSON,
			&snapshot.ChangedBy,
			&snapshot.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan config snapshot: %w", err)
		}

		// Deserialize config data from JSON
		err = json.Unmarshal([]byte(configJSON), &snapshot.ConfigData)
		if err != nil {
			log.Printf("Failed to deserialize config data for snapshot %d: %v", snapshot.ID, err)
			return nil, fmt.Errorf("failed to deserialize config data: %w", err)
		}

		history = append(history, snapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating config history rows: %w", err)
	}

	return history, nil
}

// GetConfigSnapshotByID implements Storage.GetConfigSnapshotByID.
// Retrieves a specific configuration snapshot by ID with JSON deserialization.
// Returns false if snapshot does not exist.
func (s *SQLiteStorage) GetConfigSnapshotByID(ctx context.Context, snapshotID int64) (ConfigSnapshot, bool, error) {
	query := `
		SELECT id, snapshot_time, config_snapshot, changed_by, created_at
		FROM config_history
		WHERE id = ?
	`

	var snapshot ConfigSnapshot
	var configJSON string

	err := s.db.QueryRowContext(ctx, query, snapshotID).Scan(
		&snapshot.ID,
		&snapshot.SnapshotTime,
		&configJSON,
		&snapshot.ChangedBy,
		&snapshot.CreatedAt,
	)

	if err == sql.ErrNoRows {
		return ConfigSnapshot{}, false, nil
	}
	if err != nil {
		log.Printf("Failed to query config snapshot %d: %v", snapshotID, err)
		return ConfigSnapshot{}, false, fmt.Errorf("failed to query config snapshot: %w", err)
	}

	// Deserialize config data from JSON
	err = json.Unmarshal([]byte(configJSON), &snapshot.ConfigData)
	if err != nil {
		log.Printf("Failed to deserialize config data for snapshot %d: %v", snapshotID, err)
		return ConfigSnapshot{}, false, fmt.Errorf("failed to deserialize config data: %w", err)
	}

	log.Printf("Retrieved config snapshot: id=%d, changed_by=%s, keys=%d", snapshot.ID, snapshot.ChangedBy, len(snapshot.ConfigData))
	return snapshot, true, nil
}

// RevertToSnapshot implements Storage.RevertToSnapshot.
// Atomically restores configuration from a snapshot using a transaction.
// Creates a new snapshot after revert for audit trail.
// Uses the transaction pattern from LogCheckBatch for atomic operations.
func (s *SQLiteStorage) RevertToSnapshot(ctx context.Context, snapshotID int64) error {
	return s.retryWithBackoff(ctx, func() error {
		// First, retrieve the snapshot to revert to
		snapshot, found, err := s.GetConfigSnapshotByID(ctx, snapshotID)
		if err != nil {
			return fmt.Errorf("failed to retrieve snapshot: %w", err)
		}
		if !found {
			return fmt.Errorf("snapshot %d not found", snapshotID)
		}

		// Start transaction for atomic revert operation
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			log.Printf("Failed to begin transaction for config revert: %v", err)
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		// Delete all current config entries
		_, err = tx.ExecContext(ctx, "DELETE FROM config")
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to delete current config during revert: %v", err)
			return fmt.Errorf("failed to delete current config: %w", err)
		}

		// Restore all key-value pairs from snapshot
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO config (key, value, updated_at)
			VALUES (?, ?, CURRENT_TIMESTAMP)
		`)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to prepare statement for config restore: %w", err)
		}
		defer stmt.Close()

		for key, value := range snapshot.ConfigData {
			_, err = stmt.ExecContext(ctx, key, value)
			if err != nil {
				tx.Rollback()
				log.Printf("Failed to restore config key %s during revert: %v", key, err)
				return fmt.Errorf("failed to restore config key: %w", err)
			}
		}

		// Create new snapshot recording the revert operation (audit trail)
		configJSON, err := json.Marshal(snapshot.ConfigData)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to serialize config for revert snapshot: %w", err)
		}

		_, err = tx.ExecContext(ctx, `
			INSERT INTO config_history (snapshot_time, config_snapshot, changed_by)
			VALUES (?, ?, ?)
		`, time.Now(), string(configJSON), fmt.Sprintf("revert-to-snapshot-%d", snapshotID))
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to create revert snapshot: %v", err)
			return fmt.Errorf("failed to create revert snapshot: %w", err)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Failed to commit config revert transaction: %v", err)
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		log.Printf("Reverted config to snapshot %d: restored %d keys", snapshotID, len(snapshot.ConfigData))
		return nil
	})
}
