package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestLogUpdateOperation tests logging update operation to audit log
func TestLogUpdateOperation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "nginx-app"
	operation := "pull"
	fromVer := "1.25.0"
	toVer := "1.25.3"
	success := true

	err = storage.LogUpdate(ctx, containerName, operation, fromVer, toVer, success, nil)
	if err != nil {
		t.Fatalf("LogUpdate failed: %v", err)
	}

	// Verify the entry was saved by querying directly
	var savedContainerName, savedOperation, savedFromVer, savedToVer, savedError string
	var savedSuccess bool
	var timestamp time.Time

	err = storage.db.QueryRow(
		"SELECT container_name, operation, from_version, to_version, timestamp, success, error FROM update_log WHERE container_name = ?",
		containerName,
	).Scan(&savedContainerName, &savedOperation, &savedFromVer, &savedToVer, &timestamp, &savedSuccess, &savedError)

	if err != nil {
		t.Fatalf("Failed to query saved update log: %v", err)
	}

	if savedContainerName != containerName {
		t.Errorf("Expected container_name %s, got %s", containerName, savedContainerName)
	}
	if savedOperation != operation {
		t.Errorf("Expected operation %s, got %s", operation, savedOperation)
	}
	if savedFromVer != fromVer {
		t.Errorf("Expected from_version %s, got %s", fromVer, savedFromVer)
	}
	if savedToVer != toVer {
		t.Errorf("Expected to_version %s, got %s", toVer, savedToVer)
	}
	if savedSuccess != success {
		t.Errorf("Expected success %v, got %v", success, savedSuccess)
	}
	if savedError != "" {
		t.Errorf("Expected empty error, got %s", savedError)
	}

	// Check that timestamp is recent (within last minute)
	if time.Since(timestamp) > time.Minute {
		t.Errorf("timestamp is too old: %v", timestamp)
	}
}

// TestLogUpdateWithError tests logging update operation with error
func TestLogUpdateWithError(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "redis-cache"
	operation := "restart"
	fromVer := "7.0.0"
	toVer := "7.2.0"
	success := false
	updateErr := errors.New("container failed to restart")

	err = storage.LogUpdate(ctx, containerName, operation, fromVer, toVer, success, updateErr)
	if err != nil {
		t.Fatalf("LogUpdate failed: %v", err)
	}

	// Verify the error was saved
	var savedError string
	var savedSuccess bool
	err = storage.db.QueryRow(
		"SELECT success, error FROM update_log WHERE container_name = ?",
		containerName,
	).Scan(&savedSuccess, &savedError)

	if err != nil {
		t.Fatalf("Failed to query saved update log: %v", err)
	}

	if savedSuccess != false {
		t.Errorf("Expected success false, got %v", savedSuccess)
	}
	if savedError != updateErr.Error() {
		t.Errorf("Expected error %s, got %s", updateErr.Error(), savedError)
	}
}

// TestAuditLogImmutability tests audit log immutability (append-only)
func TestAuditLogImmutability(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "postgres-db"

	// Log multiple operations for the same container with delays to ensure timestamp ordering
	operations := []struct {
		operation string
		fromVer   string
		toVer     string
		success   bool
	}{
		{"pull", "15.0", "15.1", true},
		{"restart", "15.1", "15.1", true},
		{"pull", "15.1", "15.2", true},
	}

	for i, op := range operations {
		err = storage.LogUpdate(ctx, containerName, op.operation, op.fromVer, op.toVer, op.success, nil)
		if err != nil {
			t.Fatalf("LogUpdate failed: %v", err)
		}
		// Add delay between inserts to ensure timestamp ordering
		// SQLite CURRENT_TIMESTAMP has second precision
		if i < len(operations)-1 {
			time.Sleep(1100 * time.Millisecond)
		}
	}

	// Verify all entries are preserved (append-only, no updates or deletes)
	var count int
	err = storage.db.QueryRow(
		"SELECT COUNT(*) FROM update_log WHERE container_name = ?",
		containerName,
	).Scan(&count)

	if err != nil {
		t.Fatalf("Failed to count update log entries: %v", err)
	}

	if count != len(operations) {
		t.Errorf("Expected %d update log entries, got %d (audit log should be append-only)", len(operations), count)
	}

	// Verify entries remain unchanged after multiple inserts
	rows, err := storage.db.Query(
		"SELECT operation, from_version, to_version FROM update_log WHERE container_name = ? ORDER BY timestamp ASC",
		containerName,
	)
	if err != nil {
		t.Fatalf("Failed to query update log: %v", err)
	}
	defer rows.Close()

	i := 0
	for rows.Next() {
		var operation, fromVer, toVer string
		err = rows.Scan(&operation, &fromVer, &toVer)
		if err != nil {
			t.Fatalf("Failed to scan row: %v", err)
		}

		if operation != operations[i].operation {
			t.Errorf("Entry %d: expected operation %s, got %s", i, operations[i].operation, operation)
		}
		if fromVer != operations[i].fromVer {
			t.Errorf("Entry %d: expected from_version %s, got %s", i, operations[i].fromVer, fromVer)
		}
		if toVer != operations[i].toVer {
			t.Errorf("Entry %d: expected to_version %s, got %s", i, operations[i].toVer, toVer)
		}
		i++
	}
}

// TestLogUpdateValidation tests operation validation
func TestLogUpdateValidation(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Test invalid operation
	err = storage.LogUpdate(ctx, "test-container", "invalid_operation", "1.0.0", "1.0.1", true, nil)
	if err == nil {
		t.Error("Expected error for invalid operation, got nil")
	}

	// Test valid operations
	validOperations := []string{"pull", "restart", "rollback"}
	for _, op := range validOperations {
		err = storage.LogUpdate(ctx, "test-container", op, "1.0.0", "1.0.1", true, nil)
		if err != nil {
			t.Errorf("Expected no error for valid operation %s, got %v", op, err)
		}
	}
}

// TestSetConfig tests setting configuration values
func TestSetConfig(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	key := "cache_ttl_days"
	value := "14"

	err = storage.SetConfig(ctx, key, value)
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Verify the config was saved
	var savedValue string
	var updatedAt time.Time
	err = storage.db.QueryRow(
		"SELECT value, updated_at FROM config WHERE key = ?",
		key,
	).Scan(&savedValue, &updatedAt)

	if err != nil {
		t.Fatalf("Failed to query saved config: %v", err)
	}

	if savedValue != value {
		t.Errorf("Expected value %s, got %s", value, savedValue)
	}

	// Check that updated_at is recent (within last minute)
	if time.Since(updatedAt) > time.Minute {
		t.Errorf("updated_at timestamp is too old: %v", updatedAt)
	}
}

// TestGetConfig tests getting configuration values
func TestGetConfig(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	key := "github_token"
	value := "ghp_testtoken123"

	// First set a config value
	err = storage.SetConfig(ctx, key, value)
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	// Now retrieve it
	retrievedValue, found, err := storage.GetConfig(ctx, key)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if !found {
		t.Fatal("Expected to find config value, but found = false")
	}

	if retrievedValue != value {
		t.Errorf("Expected value %s, got %s", value, retrievedValue)
	}
}

// TestGetConfigNotFound tests getting non-existent configuration
func TestGetConfigNotFound(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Try to retrieve a non-existent config key
	value, found, err := storage.GetConfig(ctx, "nonexistent_key")
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}

	if found {
		t.Error("Expected found = false for non-existent key")
	}

	if value != "" {
		t.Errorf("Expected empty value for non-existent key, got %s", value)
	}
}

// TestConfigTimestampUpdates tests configuration timestamp updates
func TestConfigTimestampUpdates(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	key := "test_key"
	value1 := "value1"
	value2 := "value2"

	// Set initial value
	err = storage.SetConfig(ctx, key, value1)
	if err != nil {
		t.Fatalf("SetConfig (first) failed: %v", err)
	}

	// Get initial timestamp
	var timestamp1 time.Time
	err = storage.db.QueryRow(
		"SELECT updated_at FROM config WHERE key = ?",
		key,
	).Scan(&timestamp1)
	if err != nil {
		t.Fatalf("Failed to query initial timestamp: %v", err)
	}

	// Wait to ensure timestamp difference (SQLite has second precision)
	time.Sleep(1100 * time.Millisecond)

	// Update the value
	err = storage.SetConfig(ctx, key, value2)
	if err != nil {
		t.Fatalf("SetConfig (second) failed: %v", err)
	}

	// Get updated timestamp and value
	var timestamp2 time.Time
	var retrievedValue string
	err = storage.db.QueryRow(
		"SELECT value, updated_at FROM config WHERE key = ?",
		key,
	).Scan(&retrievedValue, &timestamp2)
	if err != nil {
		t.Fatalf("Failed to query updated timestamp: %v", err)
	}

	// Verify value was updated
	if retrievedValue != value2 {
		t.Errorf("Expected updated value %s, got %s", value2, retrievedValue)
	}

	// Verify timestamp was updated (should be after or equal to initial timestamp)
	if timestamp2.Before(timestamp1) {
		t.Errorf("Expected updated_at to be updated, but %v is before %v", timestamp2, timestamp1)
	}

	// With the sleep, timestamps should actually be different
	if !timestamp2.After(timestamp1) {
		t.Errorf("Expected updated_at to be after initial timestamp, but %v is not after %v", timestamp2, timestamp1)
	}

	// Verify only one entry exists for this key
	var count int
	err = storage.db.QueryRow(
		"SELECT COUNT(*) FROM config WHERE key = ?",
		key,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count config entries: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 config entry after update, got %d", count)
	}
}

// TestGetUpdateLog tests querying update log for a container
func TestGetUpdateLog(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "mysql-db"
	now := time.Now()

	// Insert entries with explicit timestamps
	entries := []struct {
		operation string
		fromVer   string
		toVer     string
		success   bool
		timestamp time.Time
	}{
		{"pull", "8.0.0", "8.0.30", true, now.Add(-2 * time.Hour)},
		{"restart", "8.0.30", "8.0.30", true, now.Add(-1 * time.Hour)},
		{"pull", "8.0.30", "8.0.32", true, now.Add(-10 * time.Minute)},
	}

	for _, entry := range entries {
		_, err = storage.db.Exec(
			"INSERT INTO update_log (container_name, operation, from_version, to_version, timestamp, success, error) VALUES (?, ?, ?, ?, ?, ?, ?)",
			containerName, entry.operation, entry.fromVer, entry.toVer, entry.timestamp, entry.success, "",
		)
		if err != nil {
			t.Fatalf("Failed to insert update log: %v", err)
		}
	}

	// Retrieve update log
	logs, err := storage.GetUpdateLog(ctx, containerName, 10)
	if err != nil {
		t.Fatalf("GetUpdateLog failed: %v", err)
	}

	if len(logs) != 3 {
		t.Fatalf("Expected 3 update log entries, got %d", len(logs))
	}

	// Verify entries are in descending order (most recent first)
	if logs[0].ToVersion != "8.0.32" {
		t.Errorf("Expected most recent entry to have to_version 8.0.32, got %s", logs[0].ToVersion)
	}
	if logs[2].ToVersion != "8.0.30" {
		t.Errorf("Expected oldest entry to have to_version 8.0.30, got %s", logs[2].ToVersion)
	}

	// Verify container names match
	for i, entry := range logs {
		if entry.ContainerName != containerName {
			t.Errorf("Entry %d: expected container_name %s, got %s", i, containerName, entry.ContainerName)
		}
	}
}
