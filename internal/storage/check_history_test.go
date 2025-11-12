package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// TestLogCheckSuccessful tests logging a successful check operation
func TestLogCheckSuccessful(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "nginx-app"
	image := "docker.io/library/nginx:latest"
	currentVer := "1.25.0"
	latestVer := "1.25.0"
	status := "up_to_date"

	err = storage.LogCheck(ctx, containerName, image, currentVer, latestVer, status, nil)
	if err != nil {
		t.Fatalf("LogCheck failed: %v", err)
	}

	// Verify the entry was saved by querying directly
	var savedContainerName, savedImage, savedStatus, savedError string
	var savedCurrentVer, savedLatestVer string
	var checkTime time.Time

	err = storage.db.QueryRow(
		"SELECT container_name, image, current_version, latest_version, status, error, check_time FROM check_history WHERE container_name = ?",
		containerName,
	).Scan(&savedContainerName, &savedImage, &savedCurrentVer, &savedLatestVer, &savedStatus, &savedError, &checkTime)

	if err != nil {
		t.Fatalf("Failed to query saved check history: %v", err)
	}

	if savedContainerName != containerName {
		t.Errorf("Expected container_name %s, got %s", containerName, savedContainerName)
	}
	if savedImage != image {
		t.Errorf("Expected image %s, got %s", image, savedImage)
	}
	if savedCurrentVer != currentVer {
		t.Errorf("Expected current_version %s, got %s", currentVer, savedCurrentVer)
	}
	if savedLatestVer != latestVer {
		t.Errorf("Expected latest_version %s, got %s", latestVer, savedLatestVer)
	}
	if savedStatus != status {
		t.Errorf("Expected status %s, got %s", status, savedStatus)
	}
	if savedError != "" {
		t.Errorf("Expected empty error, got %s", savedError)
	}

	// Check that check_time is recent (within last minute)
	if time.Since(checkTime) > time.Minute {
		t.Errorf("check_time timestamp is too old: %v", checkTime)
	}
}

// TestLogCheckWithError tests logging a check with an error
func TestLogCheckWithError(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "redis-cache"
	image := "docker.io/library/redis:latest"
	currentVer := "7.0.0"
	latestVer := ""
	status := "failed"
	checkErr := errors.New("registry authentication failed")

	err = storage.LogCheck(ctx, containerName, image, currentVer, latestVer, status, checkErr)
	if err != nil {
		t.Fatalf("LogCheck failed: %v", err)
	}

	// Verify the error was saved
	var savedError string
	var savedStatus string
	err = storage.db.QueryRow(
		"SELECT status, error FROM check_history WHERE container_name = ?",
		containerName,
	).Scan(&savedStatus, &savedError)

	if err != nil {
		t.Fatalf("Failed to query saved check history: %v", err)
	}

	if savedStatus != status {
		t.Errorf("Expected status %s, got %s", status, savedStatus)
	}
	if savedError != checkErr.Error() {
		t.Errorf("Expected error %s, got %s", checkErr.Error(), savedError)
	}
}

// TestGetCheckHistory tests querying history by container name
func TestGetCheckHistory(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "postgres-db"
	now := time.Now()

	// Insert entries with explicit timestamps to ensure proper ordering
	entries := []struct {
		currentVer string
		latestVer  string
		status     string
		checkTime  time.Time
	}{
		{"15.0", "15.0", "up_to_date", now.Add(-2 * time.Hour)},
		{"15.0", "15.1", "update_available", now.Add(-1 * time.Hour)},
		{"15.1", "15.2", "update_available", now.Add(-10 * time.Minute)},
	}

	for _, entry := range entries {
		_, err = storage.db.Exec(
			"INSERT INTO check_history (container_name, image, check_time, current_version, latest_version, status, error) VALUES (?, ?, ?, ?, ?, ?, ?)",
			containerName, "docker.io/library/postgres:latest", entry.checkTime, entry.currentVer, entry.latestVer, entry.status, "",
		)
		if err != nil {
			t.Fatalf("Failed to insert check history: %v", err)
		}
	}

	// Retrieve history
	history, err := storage.GetCheckHistory(ctx, containerName, 10)
	if err != nil {
		t.Fatalf("GetCheckHistory failed: %v", err)
	}

	if len(history) != 3 {
		t.Fatalf("Expected 3 history entries, got %d", len(history))
	}

	// Verify entries are in descending order (most recent first)
	if history[0].LatestVersion != "15.2" {
		t.Errorf("Expected most recent entry to have latest_version 15.2, got %s", history[0].LatestVersion)
	}
	if history[2].LatestVersion != "15.0" {
		t.Errorf("Expected oldest entry to have latest_version 15.0, got %s", history[2].LatestVersion)
	}

	// Verify container names match
	for i, entry := range history {
		if entry.ContainerName != containerName {
			t.Errorf("Entry %d: expected container_name %s, got %s", i, containerName, entry.ContainerName)
		}
	}
}

// TestGetCheckHistoryByTimeRange tests querying history by time range
func TestGetCheckHistoryByTimeRange(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Insert entries at different times
	now := time.Now()
	entries := []struct {
		containerName string
		checkTime     time.Time
		status        string
	}{
		{"container1", now.Add(-2 * time.Hour), "up_to_date"},
		{"container2", now.Add(-1 * time.Hour), "update_available"},
		{"container3", now.Add(-30 * time.Minute), "up_to_date"},
		{"container4", now.Add(-5 * time.Minute), "failed"},
	}

	for _, entry := range entries {
		_, err = storage.db.Exec(
			"INSERT INTO check_history (container_name, image, check_time, current_version, latest_version, status, error) VALUES (?, ?, ?, ?, ?, ?, ?)",
			entry.containerName, "docker.io/library/test:latest", entry.checkTime, "1.0.0", "1.0.0", entry.status, "",
		)
		if err != nil {
			t.Fatalf("Failed to insert check history: %v", err)
		}
	}

	// Query for entries in the last hour
	start := now.Add(-1 * time.Hour)
	end := now

	history, err := storage.GetCheckHistoryByTimeRange(ctx, start, end)
	if err != nil {
		t.Fatalf("GetCheckHistoryByTimeRange failed: %v", err)
	}

	// Should get entries from the last hour (container2, container3, container4)
	if len(history) != 3 {
		t.Fatalf("Expected 3 history entries in time range, got %d", len(history))
	}

	// Verify entries are in descending order (most recent first)
	if history[0].ContainerName != "container4" {
		t.Errorf("Expected most recent entry to be container4, got %s", history[0].ContainerName)
	}
	if history[2].ContainerName != "container2" {
		t.Errorf("Expected oldest entry in range to be container2, got %s", history[2].ContainerName)
	}
}

// TestLogCheckBatch tests batch logging for multiple containers
func TestLogCheckBatch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create batch of check entries
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
		t.Fatalf("LogCheckBatch failed: %v", err)
	}

	// Verify all entries were saved
	var count int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM check_history").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count check history entries: %v", err)
	}

	if count != 3 {
		t.Errorf("Expected 3 check history entries, got %d", count)
	}

	// Verify each container has an entry
	for _, check := range checks {
		var savedStatus string
		err = storage.db.QueryRow(
			"SELECT status FROM check_history WHERE container_name = ?",
			check.ContainerName,
		).Scan(&savedStatus)

		if err != nil {
			t.Fatalf("Failed to query check history for %s: %v", check.ContainerName, err)
		}

		if savedStatus != check.Status {
			t.Errorf("Expected status %s for %s, got %s", check.Status, check.ContainerName, savedStatus)
		}
	}
}

// TestLogCheckBatchRollback tests that batch logging rolls back on error
func TestLogCheckBatchRollback(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create a batch with an entry that will fail (e.g., very long container name)
	// Note: This test verifies transaction rollback behavior
	// We'll use a canceled context to simulate a failure
	ctxCanceled, cancel := context.WithCancel(ctx)
	cancel() // Cancel immediately

	checks := []CheckHistoryEntry{
		{
			ContainerName:  "test-container-1",
			Image:          "docker.io/library/nginx:latest",
			CurrentVersion: "1.0.0",
			LatestVersion:  "1.0.1",
			Status:         "update_available",
			Error:          "",
		},
	}

	err = storage.LogCheckBatch(ctxCanceled, checks)
	if err == nil {
		t.Fatal("Expected LogCheckBatch to fail with canceled context")
	}

	// Verify no entries were saved (transaction rolled back)
	var count int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM check_history").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count check history entries: %v", err)
	}

	if count != 0 {
		t.Errorf("Expected 0 check history entries after rollback, got %d", count)
	}
}

// TestGetCheckHistoryLimit tests pagination support with limit
func TestGetCheckHistoryLimit(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	containerName := "test-pagination"
	now := time.Now()

	// Insert 10 entries with explicit timestamps
	for i := 0; i < 10; i++ {
		checkTime := now.Add(-time.Duration(10-i) * time.Minute)
		_, err = storage.db.Exec(
			"INSERT INTO check_history (container_name, image, check_time, current_version, latest_version, status, error) VALUES (?, ?, ?, ?, ?, ?, ?)",
			containerName, "docker.io/library/test:latest", checkTime, "1.0.0", "1.0.0", "up_to_date", "",
		)
		if err != nil {
			t.Fatalf("Failed to insert check history: %v", err)
		}
	}

	// Retrieve with limit of 5
	history, err := storage.GetCheckHistory(ctx, containerName, 5)
	if err != nil {
		t.Fatalf("GetCheckHistory failed: %v", err)
	}

	if len(history) != 5 {
		t.Errorf("Expected 5 history entries with limit=5, got %d", len(history))
	}
}
