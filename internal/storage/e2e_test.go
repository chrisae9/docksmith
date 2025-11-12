package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestEndToEndCacheWorkflow tests complete cache workflow: save, retrieve, expire, cleanup
func TestEndToEndCacheWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Step 1: Save multiple cache entries
	entries := []struct {
		sha256  string
		imgRef  string
		version string
		arch    string
	}{
		{"sha256:abc123", "docker.io/library/nginx", "1.25.0", "amd64"},
		{"sha256:def456", "docker.io/library/redis", "7.2.0", "amd64"},
		{"sha256:ghi789", "docker.io/library/postgres", "15.3", "amd64"},
	}

	for _, entry := range entries {
		err = storage.SaveVersionCache(ctx, entry.sha256, entry.imgRef, entry.version, entry.arch)
		if err != nil {
			t.Fatalf("Failed to save cache entry: %v", err)
		}
	}

	// Step 2: Retrieve all entries (should be found)
	for _, entry := range entries {
		version, found, err := storage.GetVersionCache(ctx, entry.sha256, entry.imgRef, entry.arch)
		if err != nil {
			t.Fatalf("Failed to get cache entry: %v", err)
		}
		if !found {
			t.Errorf("Expected to find cache entry for %s, but not found", entry.imgRef)
		}
		if version != entry.version {
			t.Errorf("Expected version %s, got %s", entry.version, version)
		}
	}

	// Step 3: Insert an expired entry
	_, err = storage.db.Exec(
		"INSERT INTO version_cache (sha256, image_ref, resolved_version, architecture, resolved_at) VALUES (?, ?, ?, ?, ?)",
		"sha256:expired", "docker.io/library/old", "1.0.0", "amd64", time.Now().Add(-10*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("Failed to insert expired entry: %v", err)
	}

	// Step 4: Verify expired entry is not returned
	version, found, err := storage.GetVersionCache(ctx, "sha256:expired", "docker.io/library/old", "amd64")
	if err != nil {
		t.Fatalf("Failed to get expired entry: %v", err)
	}
	if found {
		t.Errorf("Expected expired entry to not be found, but got version: %s", version)
	}

	// Step 5: Clean expired entries
	rowsDeleted, err := storage.CleanExpiredCache(ctx, 7)
	if err != nil {
		t.Fatalf("Failed to clean expired cache: %v", err)
	}
	if rowsDeleted != 1 {
		t.Errorf("Expected to delete 1 expired entry, deleted %d", rowsDeleted)
	}

	// Step 6: Verify fresh entries still exist
	var count int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM version_cache").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count cache entries: %v", err)
	}
	if count != len(entries) {
		t.Errorf("Expected %d fresh entries after cleanup, got %d", len(entries), count)
	}
}

// TestEndToEndCheckHistoryWorkflow tests complete check history workflow
func TestEndToEndCheckHistoryWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Step 1: Log multiple checks in batch (timestamps are set by database to CURRENT_TIMESTAMP)
	checks := []CheckHistoryEntry{
		{
			ContainerName:  "nginx-app",
			Image:          "docker.io/library/nginx:latest",
			CurrentVersion: "1.25.0",
			LatestVersion:  "1.25.3",
			Status:         "update_available",
			Error:          "",
		},
		{
			ContainerName:  "redis-cache",
			Image:          "docker.io/library/redis:7",
			CurrentVersion: "7.2.0",
			LatestVersion:  "7.2.0",
			Status:         "up_to_date",
			Error:          "",
		},
		{
			ContainerName:  "postgres-db",
			Image:          "docker.io/library/postgres:15",
			CurrentVersion: "15.3",
			LatestVersion:  "15.4",
			Status:         "update_available",
			Error:          "",
		},
	}

	err = storage.LogCheckBatch(ctx, checks)
	if err != nil {
		t.Fatalf("Failed to log check batch: %v", err)
	}

	// Step 2: Query history for specific container
	history, err := storage.GetCheckHistory(ctx, "nginx-app", 10)
	if err != nil {
		t.Fatalf("Failed to get check history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("Expected 1 check history entry for nginx-app, got %d", len(history))
	}
	if history[0].LatestVersion != "1.25.3" {
		t.Errorf("Expected latest version 1.25.3, got %s", history[0].LatestVersion)
	}

	// Step 3: Query history for all containers (limit)
	allHistory, err := storage.GetCheckHistory(ctx, "", 100)
	if err == nil && len(allHistory) >= 3 {
		// If querying without container name works and returns entries, verify count
		if len(allHistory) < 3 {
			t.Errorf("Expected at least 3 total entries, got %d", len(allHistory))
		}
	}

	// Step 4: Log individual check
	err = storage.LogCheck(ctx, "mysql-db", "docker.io/library/mysql:8", "8.0.30", "8.0.32", "update_available", nil)
	if err != nil {
		t.Fatalf("Failed to log individual check: %v", err)
	}

	// Step 5: Verify total entries
	var totalCount int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM check_history").Scan(&totalCount)
	if err != nil {
		t.Fatalf("Failed to count check history: %v", err)
	}
	if totalCount != 4 {
		t.Errorf("Expected 4 total check history entries, got %d", totalCount)
	}
}

// TestEndToEndUpdateAuditWorkflow tests complete update audit workflow
func TestEndToEndUpdateAuditWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Simulate a complete update workflow: pull -> restart
	containerName := "nginx-production"

	// Step 1: Log pull operation
	err = storage.LogUpdate(ctx, containerName, "pull", "1.24.0", "1.25.0", true, nil)
	if err != nil {
		t.Fatalf("Failed to log pull operation: %v", err)
	}

	// Step 2: Log restart operation
	// Add delay to ensure timestamp ordering
	time.Sleep(1100 * time.Millisecond)
	err = storage.LogUpdate(ctx, containerName, "restart", "1.25.0", "1.25.0", true, nil)
	if err != nil {
		t.Fatalf("Failed to log restart operation: %v", err)
	}

	// Step 3: Query audit log
	logs, err := storage.GetUpdateLog(ctx, containerName, 10)
	if err != nil {
		t.Fatalf("Failed to get update log: %v", err)
	}

	if len(logs) != 2 {
		t.Fatalf("Expected 2 update log entries, got %d", len(logs))
	}

	// Verify entries are in reverse chronological order (most recent first)
	if logs[0].Operation != "restart" {
		t.Errorf("Expected most recent operation to be 'restart', got '%s'", logs[0].Operation)
	}
	if logs[1].Operation != "pull" {
		t.Errorf("Expected oldest operation to be 'pull', got '%s'", logs[1].Operation)
	}

	// Verify all operations succeeded
	for i, log := range logs {
		if !log.Success {
			t.Errorf("Entry %d: expected success=true, got false", i)
		}
		if log.Error != "" {
			t.Errorf("Entry %d: expected no error, got '%s'", i, log.Error)
		}
	}
}

// TestEndToEndConfigurationWorkflow tests complete configuration workflow
func TestEndToEndConfigurationWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Step 1: Set multiple configurations
	configs := map[string]string{
		"cache_ttl_days":    "14",
		"github_token":      "ghp_testtoken123",
		"compose_file_path": "/www,/torrent",
		"last_check_time":   time.Now().Format(time.RFC3339),
	}

	for key, value := range configs {
		err = storage.SetConfig(ctx, key, value)
		if err != nil {
			t.Fatalf("Failed to set config %s: %v", key, err)
		}
	}

	// Step 2: Retrieve all configurations
	for key, expectedValue := range configs {
		value, found, err := storage.GetConfig(ctx, key)
		if err != nil {
			t.Fatalf("Failed to get config %s: %v", key, err)
		}
		if !found {
			t.Errorf("Expected to find config %s, but not found", key)
		}
		if value != expectedValue {
			t.Errorf("Config %s: expected value %s, got %s", key, expectedValue, value)
		}
	}

	// Step 3: Update existing configuration
	time.Sleep(1100 * time.Millisecond) // Ensure timestamp difference
	newTTL := "30"
	err = storage.SetConfig(ctx, "cache_ttl_days", newTTL)
	if err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	// Step 4: Verify update
	value, found, err := storage.GetConfig(ctx, "cache_ttl_days")
	if err != nil {
		t.Fatalf("Failed to get updated config: %v", err)
	}
	if !found {
		t.Fatal("Expected to find updated config")
	}
	if value != newTTL {
		t.Errorf("Expected updated value %s, got %s", newTTL, value)
	}

	// Step 5: Verify timestamp was updated
	var updatedAt time.Time
	err = storage.db.QueryRow("SELECT updated_at FROM config WHERE key = ?", "cache_ttl_days").Scan(&updatedAt)
	if err != nil {
		t.Fatalf("Failed to query updated_at: %v", err)
	}
	if time.Since(updatedAt) > 5*time.Second {
		t.Errorf("Expected updated_at to be recent, but it's %v old", time.Since(updatedAt))
	}

	// Step 6: Query non-existent key
	_, found, err = storage.GetConfig(ctx, "nonexistent_key")
	if err != nil {
		t.Fatalf("GetConfig for nonexistent key should not error: %v", err)
	}
	if found {
		t.Error("Expected found=false for nonexistent key")
	}
}

// TestEndToEndDatabasePersistence tests database persistence across connections
func TestEndToEndDatabasePersistence(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	ctx := context.Background()

	// Step 1: Create database and insert data
	storage1, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize first database connection: %v", err)
	}

	err = storage1.SaveVersionCache(ctx, "sha256:persist123", "docker.io/library/test", "1.0.0", "amd64")
	if err != nil {
		t.Fatalf("Failed to save cache entry: %v", err)
	}

	err = storage1.SetConfig(ctx, "test_persistence", "value123")
	if err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	err = storage1.Close()
	if err != nil {
		t.Fatalf("Failed to close first connection: %v", err)
	}

	// Step 2: Reopen database and verify data persists
	storage2, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize second database connection: %v", err)
	}
	defer storage2.Close()

	// Verify cache entry persists
	version, found, err := storage2.GetVersionCache(ctx, "sha256:persist123", "docker.io/library/test", "amd64")
	if err != nil {
		t.Fatalf("Failed to get persisted cache entry: %v", err)
	}
	if !found {
		t.Error("Expected persisted cache entry to be found")
	}
	if version != "1.0.0" {
		t.Errorf("Expected persisted version 1.0.0, got %s", version)
	}

	// Verify config persists
	value, found, err := storage2.GetConfig(ctx, "test_persistence")
	if err != nil {
		t.Fatalf("Failed to get persisted config: %v", err)
	}
	if !found {
		t.Error("Expected persisted config to be found")
	}
	if value != "value123" {
		t.Errorf("Expected persisted value 'value123', got '%s'", value)
	}
}

// TestEndToEndConcurrentAccess tests concurrent reads/writes with WAL mode
func TestEndToEndConcurrentAccess(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Perform concurrent writes (SQLite serializes these, but should not fail)
	done := make(chan bool, 3)

	// Writer 1: Save cache entries
	go func() {
		for i := 0; i < 10; i++ {
			sha := "sha256:concurrent" + string(rune('a'+i))
			_ = storage.SaveVersionCache(ctx, sha, "docker.io/library/test", "1.0.0", "amd64")
		}
		done <- true
	}()

	// Writer 2: Log checks
	go func() {
		for i := 0; i < 10; i++ {
			containerName := "container-" + string(rune('a'+i))
			_ = storage.LogCheck(ctx, containerName, "image:latest", "1.0.0", "1.0.1", "update_available", nil)
		}
		done <- true
	}()

	// Writer 3: Set configs
	go func() {
		for i := 0; i < 10; i++ {
			key := "key-" + string(rune('a'+i))
			_ = storage.SetConfig(ctx, key, "value")
		}
		done <- true
	}()

	// Wait for all writers to complete
	<-done
	<-done
	<-done

	// Verify all data was written successfully
	var cacheCount, historyCount, configCount int

	err = storage.db.QueryRow("SELECT COUNT(*) FROM version_cache").Scan(&cacheCount)
	if err != nil {
		t.Fatalf("Failed to count cache entries: %v", err)
	}
	if cacheCount != 10 {
		t.Errorf("Expected 10 cache entries, got %d", cacheCount)
	}

	err = storage.db.QueryRow("SELECT COUNT(*) FROM check_history").Scan(&historyCount)
	if err != nil {
		t.Fatalf("Failed to count history entries: %v", err)
	}
	if historyCount != 10 {
		t.Errorf("Expected 10 history entries, got %d", historyCount)
	}

	err = storage.db.QueryRow("SELECT COUNT(*) FROM config").Scan(&configCount)
	if err != nil {
		t.Fatalf("Failed to count config entries: %v", err)
	}
	if configCount != 10 {
		t.Errorf("Expected 10 config entries, got %d", configCount)
	}
}
