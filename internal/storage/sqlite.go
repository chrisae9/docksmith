package storage

import (
	"context"
	"embed"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"database/sql"

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
	db.SetMaxOpenConns(1)    // SQLite works best with single writer
	db.SetMaxIdleConns(1)    // Keep one connection alive
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
