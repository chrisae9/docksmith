package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestDatabaseInitialization tests that database connection succeeds with valid path
func TestDatabaseInitialization(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	if storage == nil {
		t.Fatal("Expected storage to be non-nil")
	}

	if storage.db == nil {
		t.Fatal("Expected database connection to be non-nil")
	}
}

// TestMigrationSystemRuns tests that migration system runs all migrations successfully
func TestMigrationSystemRuns(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database with migrations: %v", err)
	}
	defer storage.Close()

	// Verify that migrations ran by checking if we can query the schema
	var count int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table'").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query database schema: %v", err)
	}

	// We should have at least the migrations table and app_metadata table
	if count < 2 {
		t.Errorf("Expected at least 2 tables after migrations, got %d", count)
	}

	// Verify the app_metadata table was created by the migration
	var metadataCount int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM app_metadata").Scan(&metadataCount)
	if err != nil {
		t.Fatalf("Failed to query app_metadata table: %v", err)
	}

	if metadataCount < 2 {
		t.Errorf("Expected at least 2 metadata entries, got %d", metadataCount)
	}
}

// TestGracefulFallbackWhenDatabaseUnavailable tests graceful fallback when database unavailable
func TestGracefulFallbackWhenDatabaseUnavailable(t *testing.T) {
	// Try to create database in a directory that doesn't exist and can't be created
	invalidPath := "/nonexistent/readonly/path/test.db"

	storage, err := NewSQLiteStorage(invalidPath)

	// Should return nil storage and an error (graceful degradation)
	if storage != nil {
		storage.Close()
		t.Error("Expected nil storage for invalid path")
	}

	if err == nil {
		t.Error("Expected error for invalid database path")
	}
}

// TestWALModeEnabled tests that WAL mode is enabled correctly
func TestWALModeEnabled(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	// Query the journal mode
	var journalMode string
	err = storage.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to query journal mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("Expected WAL journal mode, got %s", journalMode)
	}
}

// TestDatabaseClose tests that Close() method works correctly
func TestDatabaseClose(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}

	err = storage.Close()
	if err != nil {
		t.Errorf("Close() returned error: %v", err)
	}

	// Attempting to use the database after close should fail
	err = storage.db.Ping()
	if err == nil {
		t.Error("Expected error when pinging closed database")
	}
}

// TestContextCancellation tests that operations respect context cancellation
func TestContextCancellation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to execute a query with cancelled context
	var count int
	err = storage.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master").Scan(&count)

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled error, got %v", err)
	}
}

// TestConnectionPoolConfiguration tests that connection pool is configured
func TestConnectionPoolConfiguration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	// Verify connection pool settings
	stats := storage.db.Stats()

	// Should have at least basic connection pool configured
	// For SQLite, we set MaxOpenConnections to 1 for optimal write performance
	if stats.MaxOpenConnections != 1 {
		t.Errorf("Expected max open connections to be 1 for SQLite, got %d", stats.MaxOpenConnections)
	}
}

// TestDatabaseFileCreation tests that database file is actually created
func TestDatabaseFileCreation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	// Check that database file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file was not created")
	}
}
