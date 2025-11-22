package storage

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // SQLite driver
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// SQLiteStorage implements the Storage interface using SQLite.
type SQLiteStorage struct {
	db     *sql.DB
	dbPath string
}

// Default cache TTL for version resolution cache
const defaultVersionCacheTTL = 1 * time.Hour

// NewSQLiteStorage creates a new SQLite storage instance.
// Initializes the database connection, enables WAL mode, and runs migrations.
// Returns nil and an error if initialization fails (graceful degradation).
func NewSQLiteStorage(dbPath string) (*SQLiteStorage, error) {
	// Ensure parent directory exists
	dir := filepath.Dir(dbPath)
	if dir != "." && dir != "/" {
		// Note: In production, the /data directory should be created by Docker
		// For testing, we need to handle this gracefully
		log.Printf("Database will be created at: %s", dbPath)
	}

	// Open database connection
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		log.Printf("Failed to open database at %s: %v", dbPath, err)
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool for SQLite
	// SQLite benefits from limited connections due to write serialization
	db.SetMaxOpenConns(1)  // SQLite works best with single writer
	db.SetMaxIdleConns(1)  // Keep one connection alive
	db.SetConnMaxLifetime(0) // Connections never expire

	// Test the connection
	if err := db.Ping(); err != nil {
		db.Close()
		log.Printf("Failed to ping database: %v", err)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &SQLiteStorage{
		db:     db,
		dbPath: dbPath,
	}

	// Enable WAL mode for better concurrent access
	if err := storage.enableWALMode(); err != nil {
		db.Close()
		log.Printf("Failed to enable WAL mode: %v", err)
		return nil, fmt.Errorf("failed to enable WAL mode: %w", err)
	}

	// Run migrations
	if err := storage.runMigrations(); err != nil {
		db.Close()
		log.Printf("Failed to run migrations: %v", err)
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	log.Printf("Database initialized successfully at %s", dbPath)
	return storage, nil
}

// enableWALMode enables Write-Ahead Logging mode for better concurrency.
func (s *SQLiteStorage) enableWALMode() error {
	_, err := s.db.Exec("PRAGMA journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Verify WAL mode was set
	var mode string
	err = s.db.QueryRow("PRAGMA journal_mode").Scan(&mode)
	if err != nil {
		return fmt.Errorf("failed to verify WAL mode: %w", err)
	}

	if mode != "wal" {
		return fmt.Errorf("WAL mode not enabled, got: %s", mode)
	}

	log.Println("WAL mode enabled successfully")
	return nil
}

// runMigrations executes all migration files in order.
func (s *SQLiteStorage) runMigrations() error {
	// Create migrations tracking table if it doesn't exist
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Read migration files from embedded filesystem
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	appliedCount := 0
	skippedCount := 0

	// Process each migration file
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()

		// Only process .up.sql files
		if len(filename) < 7 || filename[len(filename)-7:] != ".up.sql" {
			continue
		}

		// Extract version number from filename (e.g., "000001_create_version_cache.up.sql" -> 1)
		var version int
		_, err := fmt.Sscanf(filename, "%d_", &version)
		if err != nil {
			log.Printf("Skipping invalid migration filename: %s", filename)
			continue
		}

		// Check if migration was already applied
		var count int
		err = s.db.QueryRow("SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version).Scan(&count)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if count > 0 {
			skippedCount++
			continue
		}

		// Read migration SQL
		migrationSQL, err := migrationsFS.ReadFile("migrations/" + filename)
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		// Execute migration in a transaction
		tx, err := s.db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %s: %w", filename, err)
		}

		_, err = tx.Exec(string(migrationSQL))
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to execute migration %s: %w", filename, err)
		}

		// Record migration as applied
		_, err = tx.Exec("INSERT INTO schema_migrations (version) VALUES (?)", version)
		if err != nil {
			tx.Rollback()
			return fmt.Errorf("failed to record migration %s: %w", filename, err)
		}

		err = tx.Commit()
		if err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", filename, err)
		}

		log.Printf("Applied migration: %s", filename)
		appliedCount++
	}

	if appliedCount > 0 {
		log.Printf("Migrations complete: %d applied, %d skipped", appliedCount, skippedCount)
	} else if skippedCount > 0 {
		log.Printf("All migrations already applied (%d skipped)", skippedCount)
	} else {
		log.Println("No migrations found")
	}

	return nil
}

// Close closes the database connection.
func (s *SQLiteStorage) Close() error {
	if s.db != nil {
		log.Printf("Closing database connection: %s", s.dbPath)
		return s.db.Close()
	}
	return nil
}

// SaveVersionCache implements Storage.SaveVersionCache.
// Saves a SHA-to-version resolution to the cache using INSERT OR REPLACE.
// Sets resolved_at to the current timestamp.
func (s *SQLiteStorage) SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `
			INSERT OR REPLACE INTO version_cache
			(sha256, image_ref, resolved_version, architecture, resolved_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		`

		_, err := s.db.ExecContext(ctx, query, sha256, imageRef, version, arch)
		if err != nil {
			log.Printf("Failed to save version cache for %s (%s, %s): %v", imageRef, sha256, arch, err)
			return fmt.Errorf("failed to save version cache: %w", err)
		}

		log.Printf("Saved version cache: %s -> %s (%s, %s)", sha256, version, imageRef, arch)
		return nil
	})
}

// GetVersionCache implements Storage.GetVersionCache.
// Retrieves a cached version resolution by composite key (sha256, image_ref, architecture).
// Checks TTL before returning (default 1 hour).
// Returns empty string and false if not found or expired.
func (s *SQLiteStorage) GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (string, bool, error) {
	// Get TTL from environment or use default
	ttl := defaultVersionCacheTTL
	if ttlEnv := os.Getenv("CACHE_TTL"); ttlEnv != "" {
		if parsed, err := time.ParseDuration(ttlEnv); err == nil && parsed > 0 {
			ttl = parsed
		} else {
			log.Printf("Warning: Invalid CACHE_TTL '%s', using default %v", ttlEnv, defaultVersionCacheTTL)
		}
	}

	var version string
	var resolvedAt time.Time

	query := `
		SELECT resolved_version, resolved_at
		FROM version_cache
		WHERE sha256 = ? AND image_ref = ? AND architecture = ?
	`

	err := s.db.QueryRowContext(ctx, query, sha256, imageRef, arch).Scan(&version, &resolvedAt)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		log.Printf("Failed to query version cache for %s (%s, %s): %v", imageRef, sha256, arch, err)
		return "", false, fmt.Errorf("failed to query version cache: %w", err)
	}

	// Check if entry is expired based on TTL
	expirationTime := time.Now().Add(-ttl)
	if resolvedAt.Before(expirationTime) {
		log.Printf("Version cache expired for %s (%s, %s): resolved at %v", imageRef, sha256, arch, resolvedAt)
		return "", false, nil
	}

	log.Printf("Version cache hit: %s -> %s (%s, %s)", sha256, version, imageRef, arch)
	return version, true, nil
}

// CleanExpiredCache removes cache entries older than the specified TTL in days.
// Returns the number of rows deleted.
func (s *SQLiteStorage) CleanExpiredCache(ctx context.Context, ttlDays int) (int, error) {
	var rowsDeleted int

	err := s.retryWithBackoff(ctx, func() error {
		query := `
			DELETE FROM version_cache
			WHERE resolved_at < datetime('now', '-' || ? || ' days')
		`

		result, err := s.db.ExecContext(ctx, query, ttlDays)
		if err != nil {
			log.Printf("Failed to clean expired cache entries: %v", err)
			return fmt.Errorf("failed to clean expired cache: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		rowsDeleted = int(affected)
		if rowsDeleted > 0 {
			log.Printf("Cleaned %d expired cache entries (TTL: %d days)", rowsDeleted, ttlDays)
		}

		return nil
	})

	return rowsDeleted, err
}

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
	query := `
		SELECT id, container_name, image, check_time, current_version, latest_version, status, error
		FROM check_history
		ORDER BY check_time DESC
	`

	// Add limit if specified
	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query)
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
	query := appendLimitClause(`
		SELECT id, container_name, operation, from_version, to_version, timestamp, success, error
		FROM update_log
		ORDER BY timestamp DESC
	`, limit)

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Printf("Failed to query all update log: %v", err)
		return nil, fmt.Errorf("failed to query all update log: %w", err)
	}
	defer rows.Close()

	return scanUpdateLogRows(rows)
}

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

		query := `
			INSERT OR REPLACE INTO update_operations
			(operation_id, container_id, container_name, stack_name, operation_type, status,
			 old_version, new_version, started_at, completed_at, error_message,
			 dependents_affected, rollback_occurred, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE((SELECT created_at FROM update_operations WHERE operation_id = ?), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		`

		_, err = s.db.ExecContext(ctx, query,
			op.OperationID, op.ContainerID, op.ContainerName, op.StackName, op.OperationType, op.Status,
			op.OldVersion, op.NewVersion, op.StartedAt, op.CompletedAt, op.ErrorMessage,
			string(dependentsJSON), op.RollbackOccurred, op.OperationID)
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
		       dependents_affected, rollback_occurred, created_at, updated_at
		FROM update_operations
		WHERE operation_id = ?
	`

	var op UpdateOperation
	var dependentsJSON string
	var startedAt, completedAt sql.NullTime
	var containerID, stackName, oldVersion, newVersion, errorMessage sql.NullString

	err := s.db.QueryRowContext(ctx, query, operationID).Scan(
		&op.ID, &op.OperationID, &containerID, &op.ContainerName, &stackName, &op.OperationType, &op.Status,
		&oldVersion, &newVersion, &startedAt, &completedAt, &errorMessage,
		&dependentsJSON, &op.RollbackOccurred, &op.CreatedAt, &op.UpdatedAt,
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

	log.Printf("Retrieved update operation: %s [%s] (status: %s)", op.OperationID, op.ContainerName, op.Status)
	return op, true, nil
}

// scanUpdateOperationRows scans multiple UpdateOperation rows and handles nullable fields
// This helper consolidates the duplicate row scanning logic used across multiple query methods
func scanUpdateOperationRows(rows *sql.Rows) ([]UpdateOperation, error) {
	operations := make([]UpdateOperation, 0)

	for rows.Next() {
		var op UpdateOperation
		var dependentsJSON string
		var startedAt, completedAt sql.NullTime
		var containerID, stackName, oldVersion, newVersion, errorMessage sql.NullString

		err := rows.Scan(
			&op.ID, &op.OperationID, &containerID, &op.ContainerName, &stackName, &op.OperationType, &op.Status,
			&oldVersion, &newVersion, &startedAt, &completedAt, &errorMessage,
			&dependentsJSON, &op.RollbackOccurred, &op.CreatedAt, &op.UpdatedAt,
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

// scanComposeBackupRows scans multiple ComposeBackup rows and handles nullable stack_name field
// This helper consolidates the duplicate row scanning logic used across multiple query methods
func scanComposeBackupRows(rows *sql.Rows) ([]ComposeBackup, error) {
	backups := make([]ComposeBackup, 0)

	for rows.Next() {
		var backup ComposeBackup
		var stackName sql.NullString

		err := rows.Scan(
			&backup.ID, &backup.OperationID, &backup.ContainerName, &stackName,
			&backup.ComposeFilePath, &backup.BackupFilePath, &backup.BackupTimestamp, &backup.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan compose backup: %w", err)
		}

		if stackName.Valid {
			backup.StackName = stackName.String
		}

		backups = append(backups, backup)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating compose backup rows: %w", err)
	}

	return backups, nil
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

// GetUpdateOperationsByStatus implements Storage.GetUpdateOperationsByStatus.
// Retrieves update operations filtered by status.
// Returns entries ordered by created_at DESC (most recent first).
func (s *SQLiteStorage) GetUpdateOperationsByStatus(ctx context.Context, status string, limit int) ([]UpdateOperation, error) {
	query := appendLimitClause(`
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, created_at, updated_at
		FROM update_operations
		WHERE status = ?
		ORDER BY created_at DESC
	`, limit)

	rows, err := s.db.QueryContext(ctx, query, status)
	if err != nil {
		log.Printf("Failed to query update operations by status %s: %v", status, err)
		return nil, fmt.Errorf("failed to query update operations by status: %w", err)
	}
	defer rows.Close()

	return scanUpdateOperationRows(rows)
}

// GetUpdateOperations implements Storage.GetUpdateOperations.
// Retrieves update operations for history display.
// Returns entries ordered by started_at DESC (most recent first).
// Only returns completed or failed operations (not queued/in-progress).
func (s *SQLiteStorage) GetUpdateOperations(ctx context.Context, limit int) ([]UpdateOperation, error) {
	query := `
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, created_at, updated_at
		FROM update_operations
		WHERE status IN ('complete', 'failed')
		ORDER BY started_at DESC
	`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Printf("Failed to query update operations: %v", err)
		return nil, fmt.Errorf("failed to query update operations: %w", err)
	}
	defer rows.Close()

	return scanUpdateOperationRows(rows)
}

// GetUpdateOperationsByContainer retrieves update operations for a specific container.
// Returns entries ordered by started_at DESC (most recent first).
func (s *SQLiteStorage) GetUpdateOperationsByContainer(ctx context.Context, containerName string, limit int) ([]UpdateOperation, error) {
	query := `
		SELECT id, operation_id, container_id, container_name, stack_name, operation_type, status,
		       old_version, new_version, started_at, completed_at, error_message,
		       dependents_affected, rollback_occurred, created_at, updated_at
		FROM update_operations
		WHERE container_name = ?
		ORDER BY started_at DESC
	`

	if limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := s.db.QueryContext(ctx, query, containerName)
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
		       dependents_affected, rollback_occurred, created_at, updated_at
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

// GetRollbackPolicy implements Storage.GetRollbackPolicy.
// Retrieves the rollback policy for an entity.
func (s *SQLiteStorage) GetRollbackPolicy(ctx context.Context, entityType, entityID string) (RollbackPolicy, bool, error) {
	query := `
		SELECT id, entity_type, entity_id, auto_rollback_enabled, health_check_required, created_at, updated_at
		FROM rollback_policies
		WHERE entity_type = ? AND (entity_id = ? OR (entity_id IS NULL AND ? = ''))
		ORDER BY created_at DESC
		LIMIT 1
	`

	var policy RollbackPolicy
	var entityIDNull sql.NullString

	err := s.db.QueryRowContext(ctx, query, entityType, entityID, entityID).Scan(
		&policy.ID, &policy.EntityType, &entityIDNull,
		&policy.AutoRollbackEnabled, &policy.HealthCheckRequired,
		&policy.CreatedAt, &policy.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return RollbackPolicy{}, false, nil
	}
	if err != nil {
		log.Printf("Failed to query rollback policy for %s/%s: %v", entityType, entityID, err)
		return RollbackPolicy{}, false, fmt.Errorf("failed to query rollback policy: %w", err)
	}

	if entityIDNull.Valid {
		policy.EntityID = entityIDNull.String
	}

	log.Printf("Retrieved rollback policy: type=%s, id=%s, enabled=%v", policy.EntityType, policy.EntityID, policy.AutoRollbackEnabled)
	return policy, true, nil
}

// SetRollbackPolicy implements Storage.SetRollbackPolicy.
// Creates or updates a rollback policy using INSERT OR REPLACE.
func (s *SQLiteStorage) SetRollbackPolicy(ctx context.Context, policy RollbackPolicy) error {
	return s.retryWithBackoff(ctx, func() error {
		// Handle empty entity ID for global policy
		var entityID interface{}
		if policy.EntityID == "" {
			entityID = nil
		} else {
			entityID = policy.EntityID
		}

		query := `
			INSERT OR REPLACE INTO rollback_policies
			(entity_type, entity_id, auto_rollback_enabled, health_check_required, created_at, updated_at)
			VALUES (?, ?, ?, ?, COALESCE((SELECT created_at FROM rollback_policies WHERE entity_type = ? AND entity_id IS ?), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		`

		_, err := s.db.ExecContext(ctx, query,
			policy.EntityType, entityID, policy.AutoRollbackEnabled, policy.HealthCheckRequired,
			policy.EntityType, entityID)
		if err != nil {
			log.Printf("Failed to set rollback policy for %s/%s: %v", policy.EntityType, policy.EntityID, err)
			return fmt.Errorf("failed to set rollback policy: %w", err)
		}

		log.Printf("Set rollback policy: type=%s, id=%s, enabled=%v", policy.EntityType, policy.EntityID, policy.AutoRollbackEnabled)
		return nil
	})
}

// QueueUpdate implements Storage.QueueUpdate.
// Adds an update operation to the queue.
func (s *SQLiteStorage) QueueUpdate(ctx context.Context, queue UpdateQueue) error {
	return s.retryWithBackoff(ctx, func() error {
		// Serialize containers to JSON
		containersJSON, err := json.Marshal(queue.Containers)
		if err != nil {
			log.Printf("Failed to serialize containers: %v", err)
			return fmt.Errorf("failed to serialize containers: %w", err)
		}

		query := `
			INSERT INTO update_queue
			(operation_id, stack_name, containers, priority, queued_at, estimated_start_time)
			VALUES (?, ?, ?, ?, ?, ?)
		`

		_, err = s.db.ExecContext(ctx, query,
			queue.OperationID, queue.StackName, string(containersJSON),
			queue.Priority, queue.QueuedAt, queue.EstimatedStartTime)
		if err != nil {
			log.Printf("Failed to queue update for operation %s: %v", queue.OperationID, err)
			return fmt.Errorf("failed to queue update: %w", err)
		}

		log.Printf("Queued update: operation=%s, stack=%s, containers=%d", queue.OperationID, queue.StackName, len(queue.Containers))
		return nil
	})
}

// DequeueUpdate implements Storage.DequeueUpdate.
// Retrieves and removes the oldest queued operation for a stack.
func (s *SQLiteStorage) DequeueUpdate(ctx context.Context, stackName string) (UpdateQueue, bool, error) {
	var queue UpdateQueue
	var found bool

	err := s.retryWithBackoff(ctx, func() error {
		// Start transaction
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			log.Printf("Failed to begin transaction for dequeue: %v", err)
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		// Find oldest queued operation for this stack
		query := `
			SELECT id, operation_id, stack_name, containers, priority, queued_at, estimated_start_time
			FROM update_queue
			WHERE stack_name = ?
			ORDER BY priority DESC, queued_at ASC
			LIMIT 1
		`

		var containersJSON string
		var estimatedStartTime sql.NullTime

		err = tx.QueryRowContext(ctx, query, stackName).Scan(
			&queue.ID, &queue.OperationID, &queue.StackName, &containersJSON,
			&queue.Priority, &queue.QueuedAt, &estimatedStartTime,
		)

		if err == sql.ErrNoRows {
			tx.Rollback()
			found = false
			return nil
		}
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to query queued update for stack %s: %v", stackName, err)
			return fmt.Errorf("failed to query queued update: %w", err)
		}

		if estimatedStartTime.Valid {
			queue.EstimatedStartTime = &estimatedStartTime.Time
		}

		// Deserialize containers from JSON
		err = json.Unmarshal([]byte(containersJSON), &queue.Containers)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to deserialize containers: %v", err)
			return fmt.Errorf("failed to deserialize containers: %w", err)
		}

		// Delete the entry
		_, err = tx.ExecContext(ctx, "DELETE FROM update_queue WHERE id = ?", queue.ID)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to delete queued update: %v", err)
			return fmt.Errorf("failed to delete queued update: %w", err)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Failed to commit dequeue transaction: %v", err)
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		found = true
		log.Printf("Dequeued update: operation=%s, stack=%s", queue.OperationID, queue.StackName)
		return nil
	})

	if err != nil {
		return UpdateQueue{}, false, err
	}

	return queue, found, nil
}

// GetQueuedUpdates implements Storage.GetQueuedUpdates.
// Retrieves all queued operations ordered by queued_at.
func (s *SQLiteStorage) GetQueuedUpdates(ctx context.Context) ([]UpdateQueue, error) {
	query := `
		SELECT id, operation_id, stack_name, containers, priority, queued_at, estimated_start_time
		FROM update_queue
		ORDER BY priority DESC, queued_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Printf("Failed to query queued updates: %v", err)
		return nil, fmt.Errorf("failed to query queued updates: %w", err)
	}
	defer rows.Close()

	var queues []UpdateQueue
	for rows.Next() {
		var queue UpdateQueue
		var containersJSON string
		var estimatedStartTime sql.NullTime

		err := rows.Scan(
			&queue.ID, &queue.OperationID, &queue.StackName, &containersJSON,
			&queue.Priority, &queue.QueuedAt, &estimatedStartTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queued update: %w", err)
		}

		if estimatedStartTime.Valid {
			queue.EstimatedStartTime = &estimatedStartTime.Time
		}

		// Deserialize containers from JSON
		err = json.Unmarshal([]byte(containersJSON), &queue.Containers)
		if err != nil {
			log.Printf("Failed to deserialize containers: %v", err)
			return nil, fmt.Errorf("failed to deserialize containers: %w", err)
		}

		queues = append(queues, queue)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating queued updates rows: %w", err)
	}

	return queues, nil
}

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

// retryWithBackoff executes a function with exponential backoff for SQLITE_BUSY errors.
// This handles transient locking issues in SQLite.
func (s *SQLiteStorage) retryWithBackoff(ctx context.Context, operation func() error) error {
	maxRetries := 5
	baseDelay := 10 * time.Millisecond

	for attempt := 0; attempt < maxRetries; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		// Check if this is a SQLITE_BUSY error
		if err.Error() != "database is locked" && err.Error() != "database table is locked" {
			return err
		}

		// Check if context is cancelled
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Calculate delay with exponential backoff
		delay := baseDelay * time.Duration(1<<uint(attempt))
		if delay > 1*time.Second {
			delay = 1 * time.Second
		}

		log.Printf("Database locked, retrying in %v (attempt %d/%d)", delay, attempt+1, maxRetries)
		time.Sleep(delay)
	}

	return fmt.Errorf("database operation failed after %d retries", maxRetries)
}
