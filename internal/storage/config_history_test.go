package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// TestSaveConfigSnapshot tests that SaveConfigSnapshot creates snapshot with correct timestamp
func TestSaveConfigSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create a config snapshot
	snapshot := ConfigSnapshot{
		SnapshotTime: time.Now(),
		ConfigData: map[string]string{
			"scan_directories":  `["/www", "/torrent"]`,
			"cache_ttl_days":    "7",
			"exclude_patterns":  `["node_modules", ".git"]`,
		},
		ChangedBy: "test-user",
	}

	err = storage.SaveConfigSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to save config snapshot: %v", err)
	}

	// Verify snapshot was saved by retrieving history
	history, err := storage.GetConfigHistory(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get config history: %v", err)
	}

	if len(history) != 1 {
		t.Errorf("Expected 1 snapshot in history, got %d", len(history))
	}

	if history[0].ChangedBy != "test-user" {
		t.Errorf("Expected changed_by to be 'test-user', got %s", history[0].ChangedBy)
	}

	if len(history[0].ConfigData) != 3 {
		t.Errorf("Expected 3 config entries, got %d", len(history[0].ConfigData))
	}
}

// TestGetConfigHistory tests that GetConfigHistory retrieves snapshots ordered by time DESC
func TestGetConfigHistory(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create multiple snapshots with different timestamps
	snapshot1 := ConfigSnapshot{
		SnapshotTime: time.Now().Add(-2 * time.Hour),
		ConfigData: map[string]string{
			"key1": "value1",
		},
		ChangedBy: "user1",
	}

	snapshot2 := ConfigSnapshot{
		SnapshotTime: time.Now().Add(-1 * time.Hour),
		ConfigData: map[string]string{
			"key2": "value2",
		},
		ChangedBy: "user2",
	}

	snapshot3 := ConfigSnapshot{
		SnapshotTime: time.Now(),
		ConfigData: map[string]string{
			"key3": "value3",
		},
		ChangedBy: "user3",
	}

	// Save all snapshots
	if err := storage.SaveConfigSnapshot(ctx, snapshot1); err != nil {
		t.Fatalf("Failed to save snapshot1: %v", err)
	}
	if err := storage.SaveConfigSnapshot(ctx, snapshot2); err != nil {
		t.Fatalf("Failed to save snapshot2: %v", err)
	}
	if err := storage.SaveConfigSnapshot(ctx, snapshot3); err != nil {
		t.Fatalf("Failed to save snapshot3: %v", err)
	}

	// Retrieve history with limit
	history, err := storage.GetConfigHistory(ctx, 2)
	if err != nil {
		t.Fatalf("Failed to get config history: %v", err)
	}

	if len(history) != 2 {
		t.Errorf("Expected 2 snapshots with limit=2, got %d", len(history))
	}

	// Verify ordering: most recent first (user3, then user2)
	if history[0].ChangedBy != "user3" {
		t.Errorf("Expected most recent snapshot to be from user3, got %s", history[0].ChangedBy)
	}

	if history[1].ChangedBy != "user2" {
		t.Errorf("Expected second most recent snapshot to be from user2, got %s", history[1].ChangedBy)
	}
}

// TestGetConfigSnapshotByID tests that GetConfigSnapshotByID returns correct snapshot
func TestGetConfigSnapshotByID(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create a snapshot
	snapshot := ConfigSnapshot{
		SnapshotTime: time.Now(),
		ConfigData: map[string]string{
			"test_key": "test_value",
		},
		ChangedBy: "test-retrieval",
	}

	err = storage.SaveConfigSnapshot(ctx, snapshot)
	if err != nil {
		t.Fatalf("Failed to save config snapshot: %v", err)
	}

	// Get the snapshot ID from history
	history, err := storage.GetConfigHistory(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get config history: %v", err)
	}

	if len(history) == 0 {
		t.Fatal("No snapshots found in history")
	}

	snapshotID := history[0].ID

	// Retrieve snapshot by ID
	retrieved, found, err := storage.GetConfigSnapshotByID(ctx, snapshotID)
	if err != nil {
		t.Fatalf("Failed to get config snapshot by ID: %v", err)
	}

	if !found {
		t.Fatal("Expected to find snapshot by ID")
	}

	if retrieved.ID != snapshotID {
		t.Errorf("Expected ID %d, got %d", snapshotID, retrieved.ID)
	}

	if retrieved.ChangedBy != "test-retrieval" {
		t.Errorf("Expected changed_by to be 'test-retrieval', got %s", retrieved.ChangedBy)
	}

	if retrieved.ConfigData["test_key"] != "test_value" {
		t.Errorf("Expected config data to contain test_key=test_value, got %v", retrieved.ConfigData)
	}

	// Test retrieving non-existent snapshot
	_, found, err = storage.GetConfigSnapshotByID(ctx, 99999)
	if err != nil {
		t.Fatalf("Expected no error for non-existent snapshot, got: %v", err)
	}

	if found {
		t.Error("Expected found=false for non-existent snapshot ID")
	}
}

// TestRevertToSnapshot tests that RevertToSnapshot atomically restores config
func TestRevertToSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Set initial config values
	initialConfig := map[string]string{
		"key1": "original_value1",
		"key2": "original_value2",
		"key3": "original_value3",
	}

	for key, value := range initialConfig {
		if err := storage.SetConfig(ctx, key, value); err != nil {
			t.Fatalf("Failed to set initial config: %v", err)
		}
	}

	// Create snapshot of initial config
	snapshot := ConfigSnapshot{
		SnapshotTime: time.Now(),
		ConfigData:   initialConfig,
		ChangedBy:    "initial-setup",
	}

	if err := storage.SaveConfigSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("Failed to save initial snapshot: %v", err)
	}

	// Get snapshot ID
	history, err := storage.GetConfigHistory(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get config history: %v", err)
	}
	snapshotID := history[0].ID

	// Modify config (simulate bad changes)
	if err := storage.SetConfig(ctx, "key1", "modified_value1"); err != nil {
		t.Fatalf("Failed to modify config: %v", err)
	}
	if err := storage.SetConfig(ctx, "key2", "modified_value2"); err != nil {
		t.Fatalf("Failed to modify config: %v", err)
	}
	if err := storage.SetConfig(ctx, "new_key", "new_value"); err != nil {
		t.Fatalf("Failed to add new config: %v", err)
	}

	// Verify config was modified
	value1, found, _ := storage.GetConfig(ctx, "key1")
	if !found || value1 != "modified_value1" {
		t.Errorf("Expected key1 to be modified_value1, got %s", value1)
	}

	// Revert to snapshot
	if err := storage.RevertToSnapshot(ctx, snapshotID); err != nil {
		t.Fatalf("Failed to revert to snapshot: %v", err)
	}

	// Verify config was restored
	value1, found, _ = storage.GetConfig(ctx, "key1")
	if !found || value1 != "original_value1" {
		t.Errorf("Expected key1 to be reverted to original_value1, got %s", value1)
	}

	value2, found, _ := storage.GetConfig(ctx, "key2")
	if !found || value2 != "original_value2" {
		t.Errorf("Expected key2 to be reverted to original_value2, got %s", value2)
	}

	value3, found, _ := storage.GetConfig(ctx, "key3")
	if !found || value3 != "original_value3" {
		t.Errorf("Expected key3 to be reverted to original_value3, got %s", value3)
	}

	// Verify new_key was removed (should not exist in snapshot)
	_, found, _ = storage.GetConfig(ctx, "new_key")
	if found {
		t.Error("Expected new_key to be removed after revert")
	}

	// Verify a new snapshot was created after revert (audit trail)
	newHistory, err := storage.GetConfigHistory(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to get config history after revert: %v", err)
	}

	// Should have at least 2 snapshots: original + revert snapshot
	if len(newHistory) < 2 {
		t.Errorf("Expected at least 2 snapshots after revert (original + revert), got %d", len(newHistory))
	}
}

// TestRevertToSnapshotNonExistent tests that RevertToSnapshot fails gracefully for non-existent snapshot
func TestRevertToSnapshotNonExistent(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Try to revert to non-existent snapshot
	err = storage.RevertToSnapshot(ctx, 99999)
	if err == nil {
		t.Error("Expected error when reverting to non-existent snapshot")
	}
}

// TestConfigSnapshotJSONSerialization tests that config snapshots serialize/deserialize correctly as JSON
func TestConfigSnapshotJSONSerialization(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Create snapshot with complex config data including JSON strings
	snapshot := ConfigSnapshot{
		SnapshotTime: time.Now(),
		ConfigData: map[string]string{
			"simple_key":        "simple_value",
			"json_array":        `["item1", "item2", "item3"]`,
			"json_object":       `{"nested": "value", "count": 42}`,
			"special_chars":     `value with "quotes" and \backslash`,
			"unicode":           "日本語 text with émojis 🚀",
		},
		ChangedBy: "json-test",
	}

	if err := storage.SaveConfigSnapshot(ctx, snapshot); err != nil {
		t.Fatalf("Failed to save snapshot with complex data: %v", err)
	}

	// Retrieve and verify
	history, err := storage.GetConfigHistory(ctx, 1)
	if err != nil {
		t.Fatalf("Failed to get config history: %v", err)
	}

	if len(history) == 0 {
		t.Fatal("No snapshots found in history")
	}

	retrieved := history[0]

	// Verify all values were preserved correctly
	if retrieved.ConfigData["simple_key"] != "simple_value" {
		t.Errorf("Simple key not preserved correctly: %s", retrieved.ConfigData["simple_key"])
	}

	if retrieved.ConfigData["json_array"] != `["item1", "item2", "item3"]` {
		t.Errorf("JSON array not preserved correctly: %s", retrieved.ConfigData["json_array"])
	}

	if retrieved.ConfigData["json_object"] != `{"nested": "value", "count": 42}` {
		t.Errorf("JSON object not preserved correctly: %s", retrieved.ConfigData["json_object"])
	}

	if retrieved.ConfigData["special_chars"] != `value with "quotes" and \backslash` {
		t.Errorf("Special chars not preserved correctly: %s", retrieved.ConfigData["special_chars"])
	}

	if retrieved.ConfigData["unicode"] != "日本語 text with émojis 🚀" {
		t.Errorf("Unicode not preserved correctly: %s", retrieved.ConfigData["unicode"])
	}
}
