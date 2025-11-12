package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chis/docksmith/internal/storage"
)

// TestLoadConfigMergesYAMLWithDatabase tests that LoadConfig merges YAML file with database config
func TestLoadConfigMergesYAMLWithDatabase(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "test_config.yaml")

	// Create a test YAML file
	yamlContent := `scan_directories:
  - /www
  - /torrent
exclude_patterns:
  - node_modules
  - .git
cache_ttl_days: 7
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to create test YAML file: %v", err)
	}

	// Create in-memory storage
	store, err := storage.NewSQLiteStorage(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Set a database value that should override YAML
	if err := store.SetConfig(ctx, "cache_ttl_days", "14"); err != nil {
		t.Fatalf("Failed to set config in database: %v", err)
	}

	// Load config with merge
	cfg := &Config{}
	if err := cfg.Load(ctx, store, yamlPath); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify database value overrides YAML value
	if cfg.CacheTTLDays != 14 {
		t.Errorf("Expected CacheTTLDays to be 14 (from database), got %d", cfg.CacheTTLDays)
	}

	// Verify YAML values are loaded when not in database
	if len(cfg.ScanDirectories) != 2 {
		t.Errorf("Expected 2 scan directories from YAML, got %d", len(cfg.ScanDirectories))
	}
}

// TestLoadConfigHandlesMissingYAMLFile tests that LoadConfig handles missing YAML file gracefully
func TestLoadConfigHandlesMissingYAMLFile(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "nonexistent.yaml")

	store, err := storage.NewSQLiteStorage(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	cfg := &Config{}
	// Should not return error for missing YAML file
	if err := cfg.Load(ctx, store, yamlPath); err != nil {
		t.Errorf("Load should not fail for missing YAML file, got error: %v", err)
	}
}

// TestSaveConfigCreatesSnapshot tests that SaveConfig creates snapshot before saving
func TestSaveConfigCreatesSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	store, err := storage.NewSQLiteStorage(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Set initial config
	if err := store.SetConfig(ctx, "cache_ttl_days", "7"); err != nil {
		t.Fatalf("Failed to set initial config: %v", err)
	}

	// Create config and modify it
	cfg := &Config{
		CacheTTLDays: 14,
	}

	// Save should create snapshot
	if err := cfg.Save(ctx, store, "test-user"); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify snapshot was created
	history, err := store.GetConfigHistory(ctx, 10)
	if err != nil {
		t.Fatalf("Failed to get config history: %v", err)
	}

	if len(history) == 0 {
		t.Error("Expected at least one snapshot to be created")
	}

	if history[0].ChangedBy != "test-user" {
		t.Errorf("Expected snapshot changed_by to be 'test-user', got '%s'", history[0].ChangedBy)
	}
}

// TestGetConfigReturnsCorrectPrecedence tests that GetConfig returns correct precedence (DB > file)
func TestGetConfigReturnsCorrectPrecedence(t *testing.T) {
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "test_config.yaml")

	// Create YAML with default value
	yamlContent := `cache_ttl_days: 7
`
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("Failed to create test YAML file: %v", err)
	}

	store, err := storage.NewSQLiteStorage(filepath.Join(tempDir, "test.db"))
	if err != nil {
		t.Fatalf("Failed to create storage: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Set database value
	if err := store.SetConfig(ctx, "cache_ttl_days", "21"); err != nil {
		t.Fatalf("Failed to set config in database: %v", err)
	}

	cfg := &Config{}
	if err := cfg.Load(ctx, store, yamlPath); err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Get should return database value, not YAML value
	value, found := cfg.Get("cache_ttl_days")
	if !found {
		t.Fatal("Expected to find cache_ttl_days config")
	}

	if value != "21" {
		t.Errorf("Expected value '21' from database, got '%s'", value)
	}
}

// TestSetConfigUpdatesInMemory tests that Set updates config value in-memory
func TestSetConfigUpdatesInMemory(t *testing.T) {
	cfg := &Config{
		CacheTTLDays: 7,
	}

	cfg.Set("cache_ttl_days", "14")

	value, found := cfg.Get("cache_ttl_days")
	if !found {
		t.Fatal("Expected to find cache_ttl_days after Set")
	}

	if value != "14" {
		t.Errorf("Expected value '14' after Set, got '%s'", value)
	}
}

// TestGetConfigReturnsEmptyForUnknownKey tests that Get returns false for unknown keys
func TestGetConfigReturnsEmptyForUnknownKey(t *testing.T) {
	cfg := &Config{}

	value, found := cfg.Get("nonexistent_key")
	if found {
		t.Error("Expected found=false for nonexistent key")
	}

	if value != "" {
		t.Errorf("Expected empty string for nonexistent key, got '%s'", value)
	}
}

// TestMergeConfigsPrioritizesDatabase tests that merge logic prioritizes database over YAML
func TestMergeConfigsPrioritizesDatabase(t *testing.T) {
	yamlConfig := Config{
		ScanDirectories: []string{"/www", "/torrent"},
		ExcludePatterns: []string{"node_modules"},
		CacheTTLDays:    7,
	}

	dbConfig := Config{
		CacheTTLDays: 14, // Override YAML value
	}

	merged := MergeConfigs(yamlConfig, dbConfig)

	// Database value should win
	if merged.CacheTTLDays != 14 {
		t.Errorf("Expected CacheTTLDays to be 14 from database, got %d", merged.CacheTTLDays)
	}

	// YAML values should be preserved when not in database
	if len(merged.ScanDirectories) != 2 {
		t.Errorf("Expected 2 scan directories from YAML, got %d", len(merged.ScanDirectories))
	}

	if len(merged.ExcludePatterns) != 1 {
		t.Errorf("Expected 1 exclude pattern from YAML, got %d", len(merged.ExcludePatterns))
	}
}
