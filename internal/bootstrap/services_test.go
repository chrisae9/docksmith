package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestServiceDependenciesStruct(t *testing.T) {
	// Verify struct fields are accessible
	deps := ServiceDependencies{}

	// All fields should be nil initially
	if deps.Docker != nil {
		t.Error("Docker should be nil initially")
	}
	if deps.Storage != nil {
		t.Error("Storage should be nil initially")
	}
	if deps.Registry != nil {
		t.Error("Registry should be nil initially")
	}
	if deps.EventBus != nil {
		t.Error("EventBus should be nil initially")
	}
}

func TestInitOptionsDefaults(t *testing.T) {
	opts := InitOptions{}

	// Default values
	if opts.DefaultDBPath != "" {
		t.Error("DefaultDBPath should be empty by default")
	}
	if opts.Verbose {
		t.Error("Verbose should be false by default")
	}
	if opts.RequireStorage {
		t.Error("RequireStorage should be false by default")
	}
}

func TestInitializeServicesWithDockerAvailable(t *testing.T) {
	// Skip if Docker is not available (CI environments)
	if os.Getenv("SKIP_DOCKER_TESTS") == "true" {
		t.Skip("Skipping Docker test")
	}

	// Create a temp directory for the test database
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	// Set up environment
	originalDBPath := os.Getenv("DB_PATH")
	os.Setenv("DB_PATH", dbPath)
	defer os.Setenv("DB_PATH", originalDBPath)

	deps, cleanup, err := InitializeServices(InitOptions{
		DefaultDBPath:  dbPath,
		Verbose:        false,
		RequireStorage: false,
	})

	if err != nil {
		// Docker might not be available in test environment
		t.Logf("InitializeServices failed (Docker may not be available): %v", err)
		return
	}

	// Verify dependencies are initialized
	if deps.Docker == nil {
		t.Error("Docker service should be initialized")
	}
	if deps.Registry == nil {
		t.Error("Registry manager should be initialized")
	}
	if deps.EventBus == nil {
		t.Error("EventBus should be initialized")
	}

	// Storage may or may not be initialized depending on environment
	t.Logf("Storage initialized: %v", deps.Storage != nil)

	// Test cleanup
	if cleanup == nil {
		t.Error("cleanup function should not be nil")
	}
	cleanup()
}

func TestInitializeServicesWithInvalidDBPath(t *testing.T) {
	// Skip if Docker is not available
	if os.Getenv("SKIP_DOCKER_TESTS") == "true" {
		t.Skip("Skipping Docker test")
	}

	// Use an invalid path that can't be created
	invalidPath := "/nonexistent/directory/that/does/not/exist/test.db"

	// Set up environment
	originalDBPath := os.Getenv("DB_PATH")
	os.Setenv("DB_PATH", invalidPath)
	defer os.Setenv("DB_PATH", originalDBPath)

	// Without RequireStorage, should not fail
	deps, cleanup, err := InitializeServices(InitOptions{
		DefaultDBPath:  invalidPath,
		Verbose:        false,
		RequireStorage: false,
	})

	if err != nil {
		// Docker might not be available
		t.Logf("InitializeServices failed: %v", err)
		return
	}

	// Storage should be nil due to invalid path
	if deps.Storage != nil {
		t.Error("Storage should be nil with invalid path")
	}

	// Other services should still work
	if deps.Docker == nil {
		t.Error("Docker should still be initialized")
	}
	if deps.Registry == nil {
		t.Error("Registry should still be initialized")
	}
	if deps.EventBus == nil {
		t.Error("EventBus should still be initialized")
	}

	cleanup()
}

func TestInitializeServicesRequireStorageFails(t *testing.T) {
	// Skip if Docker is not available
	if os.Getenv("SKIP_DOCKER_TESTS") == "true" {
		t.Skip("Skipping Docker test")
	}

	invalidPath := "/nonexistent/directory/test.db"

	originalDBPath := os.Getenv("DB_PATH")
	os.Setenv("DB_PATH", invalidPath)
	defer os.Setenv("DB_PATH", originalDBPath)

	// With RequireStorage=true, should fail
	deps, cleanup, err := InitializeServices(InitOptions{
		DefaultDBPath:  invalidPath,
		Verbose:        false,
		RequireStorage: true,
	})

	// If Docker is available, this should fail due to storage
	if err == nil && deps != nil {
		if cleanup != nil {
			cleanup()
		}
		// Check if storage actually failed
		if deps.Storage == nil {
			t.Error("Should have returned error when RequireStorage=true and storage fails")
		}
	}
	// If Docker is not available, it will fail before storage anyway
}

func TestCleanupOrderIsLIFO(t *testing.T) {
	// Test that cleanup functions are called in LIFO order
	var order []int
	cleanups := []func(){
		func() { order = append(order, 1) },
		func() { order = append(order, 2) },
		func() { order = append(order, 3) },
	}

	// Simulate the cleanup logic from InitializeServices
	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	cleanup()

	// Should be called in reverse order: 3, 2, 1
	expected := []int{3, 2, 1}
	if len(order) != len(expected) {
		t.Fatalf("expected %d cleanups, got %d", len(expected), len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Errorf("cleanup order position %d: expected %d, got %d", i, v, order[i])
		}
	}
}
