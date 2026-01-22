package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSaveVersionCache tests saving version resolution to cache
func TestSaveVersionCache(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	sha256 := "sha256:abc123def456"
	imageRef := "docker.io/library/nginx"
	version := "1.25.0"
	arch := "amd64"

	err = storage.SaveVersionCache(ctx, sha256, imageRef, version, arch)
	if err != nil {
		t.Fatalf("SaveVersionCache failed: %v", err)
	}

	// Verify the entry was saved by querying directly
	var savedVersion string
	var resolvedAt time.Time
	err = storage.db.QueryRow(
		"SELECT resolved_version, resolved_at FROM version_cache WHERE sha256 = ? AND image_ref = ? AND architecture = ?",
		sha256, imageRef, arch,
	).Scan(&savedVersion, &resolvedAt)

	if err != nil {
		t.Fatalf("Failed to query saved cache entry: %v", err)
	}

	if savedVersion != version {
		t.Errorf("Expected version %s, got %s", version, savedVersion)
	}

	// Check that resolved_at is recent (within last minute)
	if time.Since(resolvedAt) > time.Minute {
		t.Errorf("resolved_at timestamp is too old: %v", resolvedAt)
	}
}

// TestGetVersionCache tests retrieving cached version by SHA256 + image_ref
func TestGetVersionCache(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	sha256 := "sha256:xyz789"
	imageRef := "docker.io/library/redis"
	version := "7.2.0"
	arch := "amd64"

	// First save an entry
	err = storage.SaveVersionCache(ctx, sha256, imageRef, version, arch)
	if err != nil {
		t.Fatalf("SaveVersionCache failed: %v", err)
	}

	// Now retrieve it
	retrievedVersion, found, err := storage.GetVersionCache(ctx, sha256, imageRef, arch)
	if err != nil {
		t.Fatalf("GetVersionCache failed: %v", err)
	}

	if !found {
		t.Fatal("Expected to find cached version, but found = false")
	}

	if retrievedVersion != version {
		t.Errorf("Expected version %s, got %s", version, retrievedVersion)
	}
}

// TestGetVersionCacheNotFound tests retrieving non-existent cache entry
func TestGetVersionCacheNotFound(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Try to retrieve a non-existent entry
	version, found, err := storage.GetVersionCache(ctx, "sha256:nonexistent", "docker.io/library/test", "amd64")
	if err != nil {
		t.Fatalf("GetVersionCache failed: %v", err)
	}

	if found {
		t.Error("Expected found = false for non-existent entry")
	}

	if version != "" {
		t.Errorf("Expected empty version for non-existent entry, got %s", version)
	}
}

// TestCacheTTLExpiration tests cache TTL expiration logic
func TestCacheTTLExpiration(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	sha256 := "sha256:expired123"
	imageRef := "docker.io/library/postgres"
	version := "15.0"
	arch := "amd64"

	// Insert an entry with an old timestamp (8 days ago, beyond default 7-day TTL)
	_, err = storage.db.Exec(
		"INSERT OR REPLACE INTO version_cache (sha256, image_ref, resolved_version, architecture, resolved_at) VALUES (?, ?, ?, ?, ?)",
		sha256, imageRef, version, arch, time.Now().Add(-8*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("Failed to insert expired entry: %v", err)
	}

	// Try to retrieve - should not be found due to TTL expiration
	retrievedVersion, found, err := storage.GetVersionCache(ctx, sha256, imageRef, arch)
	if err != nil {
		t.Fatalf("GetVersionCache failed: %v", err)
	}

	if found {
		t.Errorf("Expected expired entry to not be found, but got version: %s", retrievedVersion)
	}
}

// TestMultiArchitectureSupport tests same SHA with different architectures
func TestMultiArchitectureSupport(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	sha256 := "sha256:multi123"
	imageRef := "docker.io/library/alpine"
	versionAMD64 := "3.18.0"
	versionARM64 := "3.18.0"

	// Save for amd64
	err = storage.SaveVersionCache(ctx, sha256, imageRef, versionAMD64, "amd64")
	if err != nil {
		t.Fatalf("SaveVersionCache for amd64 failed: %v", err)
	}

	// Save for arm64 (same SHA, different arch)
	err = storage.SaveVersionCache(ctx, sha256, imageRef, versionARM64, "arm64")
	if err != nil {
		t.Fatalf("SaveVersionCache for arm64 failed: %v", err)
	}

	// Retrieve amd64 version
	retrievedAMD64, found, err := storage.GetVersionCache(ctx, sha256, imageRef, "amd64")
	if err != nil {
		t.Fatalf("GetVersionCache for amd64 failed: %v", err)
	}
	if !found {
		t.Fatal("Expected to find amd64 cache entry")
	}
	if retrievedAMD64 != versionAMD64 {
		t.Errorf("Expected amd64 version %s, got %s", versionAMD64, retrievedAMD64)
	}

	// Retrieve arm64 version
	retrievedARM64, found, err := storage.GetVersionCache(ctx, sha256, imageRef, "arm64")
	if err != nil {
		t.Fatalf("GetVersionCache for arm64 failed: %v", err)
	}
	if !found {
		t.Fatal("Expected to find arm64 cache entry")
	}
	if retrievedARM64 != versionARM64 {
		t.Errorf("Expected arm64 version %s, got %s", versionARM64, retrievedARM64)
	}
}

// TestSaveVersionCacheUpdatesExisting tests that saving updates existing entries
func TestSaveVersionCacheUpdatesExisting(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	sha256 := "sha256:update123"
	imageRef := "docker.io/library/mysql"
	oldVersion := "8.0.0"
	newVersion := "8.0.32"
	arch := "amd64"

	// Save initial version
	err = storage.SaveVersionCache(ctx, sha256, imageRef, oldVersion, arch)
	if err != nil {
		t.Fatalf("SaveVersionCache (first) failed: %v", err)
	}

	// Save updated version (should replace)
	err = storage.SaveVersionCache(ctx, sha256, imageRef, newVersion, arch)
	if err != nil {
		t.Fatalf("SaveVersionCache (second) failed: %v", err)
	}

	// Retrieve and verify we get the new version
	retrievedVersion, found, err := storage.GetVersionCache(ctx, sha256, imageRef, arch)
	if err != nil {
		t.Fatalf("GetVersionCache failed: %v", err)
	}
	if !found {
		t.Fatal("Expected to find cache entry")
	}
	if retrievedVersion != newVersion {
		t.Errorf("Expected updated version %s, got %s", newVersion, retrievedVersion)
	}

	// Verify only one entry exists for this composite key
	var count int
	err = storage.db.QueryRow(
		"SELECT COUNT(*) FROM version_cache WHERE sha256 = ? AND image_ref = ? AND architecture = ?",
		sha256, imageRef, arch,
	).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count entries: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 entry after update, got %d", count)
	}
}

// TestCleanExpiredCache tests cache cleanup functionality
func TestCleanExpiredCache(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Insert some fresh entries (within TTL)
	_, err = storage.db.Exec(
		"INSERT INTO version_cache (sha256, image_ref, resolved_version, architecture, resolved_at) VALUES (?, ?, ?, ?, ?)",
		"sha256:fresh1", "docker.io/library/fresh1", "1.0.0", "amd64", time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to insert fresh entry 1: %v", err)
	}

	_, err = storage.db.Exec(
		"INSERT INTO version_cache (sha256, image_ref, resolved_version, architecture, resolved_at) VALUES (?, ?, ?, ?, ?)",
		"sha256:fresh2", "docker.io/library/fresh2", "2.0.0", "amd64", time.Now(),
	)
	if err != nil {
		t.Fatalf("Failed to insert fresh entry 2: %v", err)
	}

	// Insert some expired entries (beyond TTL)
	_, err = storage.db.Exec(
		"INSERT INTO version_cache (sha256, image_ref, resolved_version, architecture, resolved_at) VALUES (?, ?, ?, ?, ?)",
		"sha256:old1", "docker.io/library/old1", "0.5.0", "amd64", time.Now().Add(-10*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("Failed to insert old entry 1: %v", err)
	}

	_, err = storage.db.Exec(
		"INSERT INTO version_cache (sha256, image_ref, resolved_version, architecture, resolved_at) VALUES (?, ?, ?, ?, ?)",
		"sha256:old2", "docker.io/library/old2", "0.8.0", "amd64", time.Now().Add(-15*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("Failed to insert old entry 2: %v", err)
	}

	// Clean expired entries with 7-day TTL
	rowsDeleted, err := storage.CleanExpiredCache(ctx, 7)
	if err != nil {
		t.Fatalf("CleanExpiredCache failed: %v", err)
	}

	if rowsDeleted != 2 {
		t.Errorf("Expected to delete 2 rows, deleted %d", rowsDeleted)
	}

	// Verify fresh entries still exist
	var count int
	err = storage.db.QueryRow("SELECT COUNT(*) FROM version_cache").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to count remaining entries: %v", err)
	}

	if count != 2 {
		t.Errorf("Expected 2 remaining entries after cleanup, got %d", count)
	}
}

// TestGetVersionCacheWithCustomTTL tests cache retrieval respects custom TTL via CACHE_TTL env var
func TestGetVersionCacheWithCustomTTL(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	storage, err := NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("Failed to initialize database: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()
	sha256 := "sha256:ttltest"
	imageRef := "docker.io/library/ttltest"
	version := "1.0.0"
	arch := "amd64"

	// Insert entry that's 5 days old
	_, err = storage.db.Exec(
		"INSERT INTO version_cache (sha256, image_ref, resolved_version, architecture, resolved_at) VALUES (?, ?, ?, ?, ?)",
		sha256, imageRef, version, arch, time.Now().Add(-5*24*time.Hour),
	)
	if err != nil {
		t.Fatalf("Failed to insert entry: %v", err)
	}

	// Set custom TTL via environment variable (7 days)
	oldTTL := os.Getenv("CACHE_TTL")
	os.Setenv("CACHE_TTL", "168h") // 7 days
	defer func() {
		if oldTTL == "" {
			os.Unsetenv("CACHE_TTL")
		} else {
			os.Setenv("CACHE_TTL", oldTTL)
		}
	}()

	// With 7-day TTL, entry that's 5 days old should be found
	_, found, err := storage.GetVersionCache(ctx, sha256, imageRef, arch)
	if err != nil {
		t.Fatalf("GetVersionCache failed: %v", err)
	}
	if !found {
		t.Error("Expected to find entry with 7-day TTL (entry is 5 days old)")
	}
}
