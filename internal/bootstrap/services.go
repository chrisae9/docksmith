package bootstrap

import (
	"fmt"
	"log"
	"os"

	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/registry"
	"github.com/chis/docksmith/internal/storage"
)

// ServiceDependencies holds all initialized service dependencies for CLI commands.
type ServiceDependencies struct {
	Docker   *docker.Service
	Storage  storage.Storage
	Registry *registry.Manager
	EventBus *events.Bus
}

// InitOptions configures service initialization behavior.
type InitOptions struct {
	// DefaultDBPath is used if DB_PATH environment variable is not set
	DefaultDBPath string
	// Verbose enables detailed logging during initialization
	Verbose bool
	// RequireStorage determines if storage initialization failure should be fatal
	RequireStorage bool
}

// InitializeServices initializes all service dependencies with consistent error handling.
// Returns ServiceDependencies and a cleanup function that should be deferred.
func InitializeServices(opts InitOptions) (*ServiceDependencies, func(), error) {
	deps := &ServiceDependencies{}
	var cleanups []func()

	cleanup := func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}

	// Initialize Docker service
	if opts.Verbose {
		log.Println("Initializing Docker client...")
	}
	dockerService, err := docker.NewService()
	if err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("failed to create Docker service: %w", err)
	}
	deps.Docker = dockerService
	cleanups = append(cleanups, func() { dockerService.Close() })
	if opts.Verbose {
		log.Println("✓ Docker client connected")
	}

	// Initialize storage (optional - graceful degradation unless required)
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = opts.DefaultDBPath
	}

	if opts.Verbose {
		log.Printf("Initializing storage at %s...", dbPath)
	}
	storageService, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		if opts.RequireStorage {
			cleanup()
			return nil, nil, fmt.Errorf("failed to initialize storage: %w", err)
		}
		if opts.Verbose {
			log.Printf("⚠ Warning: Failed to initialize storage: %v", err)
			log.Println("⚠ Continuing without progress tracking")
		} else {
			log.Printf("Warning: Failed to initialize storage (continuing without persistence): %v", err)
		}
		deps.Storage = nil
	} else {
		deps.Storage = storageService
		cleanups = append(cleanups, func() { storageService.Close() })
		if opts.Verbose {
			log.Println("✓ Storage initialized")
		}
	}

	// Initialize registry manager
	token := os.Getenv("GITHUB_TOKEN")
	deps.Registry = registry.NewManager(token)
	if opts.Verbose {
		log.Println("✓ Registry manager initialized")
	}

	// Create event bus
	deps.EventBus = events.NewBus()

	return deps, cleanup, nil
}
