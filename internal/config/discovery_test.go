package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chis/docksmith/internal/storage"
)

// TestIsComposeFile verifies compose file name detection
func TestIsComposeFile(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		want     bool
	}{
		{"docker-compose.yml", "docker-compose.yml", true},
		{"docker-compose.yaml", "docker-compose.yaml", true},
		{"compose.yml", "compose.yml", true},
		{"compose.yaml", "compose.yaml", true},
		{"DOCKER-COMPOSE.YML", "DOCKER-COMPOSE.YML", true}, // case-insensitive
		{"Compose.Yaml", "Compose.Yaml", true},
		{"not-a-compose.yml", "not-a-compose.yml", false},
		{"docker-compose.txt", "docker-compose.txt", false},
		{"readme.md", "readme.md", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsComposeFile(tt.filename)
			if got != tt.want {
				t.Errorf("IsComposeFile(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}

// TestShouldExclude verifies exclusion pattern matching
func TestShouldExclude(t *testing.T) {
	scanner := &Scanner{}
	patterns := []string{"node_modules", ".git", ".svn", "vendor"}

	tests := []struct {
		name string
		path string
		want bool
	}{
		{"exclude node_modules", "/home/user/project/node_modules/package", true},
		{"exclude .git", "/home/user/project/.git/config", true},
		{"exclude .svn", "/opt/code/.svn/entries", true},
		{"exclude vendor", "/app/vendor/autoload.php", true},
		{"allow normal path", "/home/user/project/docker-compose.yml", false},
		{"allow similar name", "/home/user/vendomatic/compose.yml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scanner.ShouldExclude(tt.path, patterns)
			if got != tt.want {
				t.Errorf("ShouldExclude(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

// TestScanDirectory_FindsComposeFiles verifies recursive compose file discovery
func TestScanDirectory_FindsComposeFiles(t *testing.T) {
	// Create temporary test directory structure
	tempDir := t.TempDir()

	// Create test compose files
	testFiles := []string{
		"docker-compose.yml",
		"project1/compose.yaml",
		"project2/docker-compose.yaml",
		"nested/deep/compose.yml",
	}

	for _, file := range testFiles {
		fullPath := filepath.Join(tempDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("version: '3'"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Create non-compose files (should be ignored)
	nonComposeFiles := []string{
		"README.md",
		"project1/config.txt",
	}

	for _, file := range nonComposeFiles {
		fullPath := filepath.Join(tempDir, file)
		if err := os.WriteFile(fullPath, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create non-compose file: %v", err)
		}
	}

	ctx := context.Background()
	scanner := &Scanner{}

	found, err := scanner.ScanDirectory(ctx, tempDir)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Verify we found all compose files
	if len(found) != len(testFiles) {
		t.Errorf("Expected %d compose files, found %d: %v", len(testFiles), len(found), found)
	}

	// Verify each expected file was found
	for _, expected := range testFiles {
		expectedPath := filepath.Join(tempDir, expected)
		foundIt := false
		for _, f := range found {
			if f == expectedPath {
				foundIt = true
				break
			}
		}
		if !foundIt {
			t.Errorf("Expected to find %s, but it was not in results", expectedPath)
		}
	}
}

// TestScanDirectory_RespectsExclusionPatterns verifies exclusion patterns work
func TestScanDirectory_RespectsExclusionPatterns(t *testing.T) {
	// Create temporary test directory structure
	tempDir := t.TempDir()

	// Create compose files in normal directories
	normalFiles := []string{
		"docker-compose.yml",
		"app/compose.yml",
	}

	for _, file := range normalFiles {
		fullPath := filepath.Join(tempDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("version: '3'"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	// Create compose files in excluded directories
	excludedFiles := []string{
		"node_modules/package/docker-compose.yml",
		".git/hooks/compose.yml",
		"vendor/lib/docker-compose.yml",
	}

	for _, file := range excludedFiles {
		fullPath := filepath.Join(tempDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatalf("Failed to create excluded directory: %v", err)
		}
		if err := os.WriteFile(fullPath, []byte("version: '3'"), 0644); err != nil {
			t.Fatalf("Failed to create excluded file: %v", err)
		}
	}

	ctx := context.Background()
	config := Config{
		ExcludePatterns: []string{"node_modules", ".git", "vendor"},
	}
	scanner := &Scanner{
		config: &config,
	}

	found, err := scanner.ScanDirectory(ctx, tempDir)
	if err != nil {
		t.Fatalf("ScanDirectory failed: %v", err)
	}

	// Should only find normal files, not excluded ones
	if len(found) != len(normalFiles) {
		t.Errorf("Expected %d compose files (excluding patterns), found %d: %v", len(normalFiles), len(found), found)
	}

	// Verify no excluded files were found
	for _, f := range found {
		for _, pattern := range config.ExcludePatterns {
			if strings.Contains(f, pattern) {
				t.Errorf("Found file in excluded directory: %s (pattern: %s)", f, pattern)
			}
		}
	}
}

// TestScanDirectory_HandlesEmptyDirectory verifies behavior with empty directory
func TestScanDirectory_HandlesEmptyDirectory(t *testing.T) {
	tempDir := t.TempDir()

	ctx := context.Background()
	scanner := &Scanner{}

	found, err := scanner.ScanDirectory(ctx, tempDir)
	if err != nil {
		t.Fatalf("ScanDirectory failed on empty directory: %v", err)
	}

	if len(found) != 0 {
		t.Errorf("Expected 0 compose files in empty directory, found %d: %v", len(found), found)
	}
}

// TestScanDirectory_HandlesNonexistentDirectory verifies error handling
func TestScanDirectory_HandlesNonexistentDirectory(t *testing.T) {
	ctx := context.Background()
	scanner := &Scanner{}

	_, err := scanner.ScanDirectory(ctx, "/nonexistent/directory/path")
	if err == nil {
		t.Error("Expected error for nonexistent directory, got nil")
	}
}

// TestScanAll_StoresPathsInDatabase verifies ScanAll stores discovered paths in database
func TestScanAll_StoresPathsInDatabase(t *testing.T) {
	// Create temporary database
	dbFile := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLiteStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer store.Close()

	// Create temporary scan directories
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Create compose files in both directories
	testFiles := map[string][]string{
		dir1: {"docker-compose.yml", "app1/compose.yml"},
		dir2: {"project/docker-compose.yaml"},
	}

	for dir, files := range testFiles {
		for _, file := range files {
			fullPath := filepath.Join(dir, file)
			if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
				t.Fatalf("Failed to create directory: %v", err)
			}
			if err := os.WriteFile(fullPath, []byte("version: '3'"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}
		}
	}

	ctx := context.Background()
	config := Config{
		ScanDirectories: []string{dir1, dir2},
		ExcludePatterns: []string{"node_modules", ".git"},
	}

	scanner := NewScanner(store, &config)

	// Run scan
	found, err := scanner.ScanAll(ctx)
	if err != nil {
		t.Fatalf("ScanAll failed: %v", err)
	}

	// Verify we found all expected files
	expectedCount := len(testFiles[dir1]) + len(testFiles[dir2])
	if len(found) != expectedCount {
		t.Errorf("Expected %d compose files, found %d: %v", expectedCount, len(found), found)
	}

	// Verify paths were stored in database
	storedValue, foundInDB, err := store.GetConfig(ctx, "compose_file_paths")
	if err != nil {
		t.Fatalf("Failed to get stored config: %v", err)
	}
	if !foundInDB {
		t.Fatal("Expected compose_file_paths to be stored in database, but it was not found")
	}

	// Verify JSON can be unmarshalled
	var storedPaths []string
	if err := json.Unmarshal([]byte(storedValue), &storedPaths); err != nil {
		t.Fatalf("Failed to unmarshal stored paths: %v", err)
	}

	if len(storedPaths) != expectedCount {
		t.Errorf("Expected %d paths stored in DB, found %d", expectedCount, len(storedPaths))
	}
}

// TestScanAll_HandlesNonexistentDirectoriesGracefully verifies ScanAll handles missing directories
func TestScanAll_HandlesNonexistentDirectoriesGracefully(t *testing.T) {
	// Create temporary database
	dbFile := filepath.Join(t.TempDir(), "test.db")
	store, err := storage.NewSQLiteStorage(dbFile)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	config := Config{
		ScanDirectories: []string{"/nonexistent/dir1", "/nonexistent/dir2"},
	}

	scanner := NewScanner(store, &config)

	// Run scan - should not error on missing directories
	found, err := scanner.ScanAll(ctx)
	if err != nil {
		t.Fatalf("ScanAll should handle missing directories gracefully, got error: %v", err)
	}

	// Should find no files
	if len(found) != 0 {
		t.Errorf("Expected 0 compose files in nonexistent directories, found %d", len(found))
	}
}
