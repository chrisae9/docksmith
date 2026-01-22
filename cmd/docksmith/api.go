package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/chis/docksmith/internal/api"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/registry"
	"github.com/chis/docksmith/internal/storage"
)

// APICommand implements the API server command
type APICommand struct {
	port      int
	staticDir string
}

// NewAPICommand creates a new API command
func NewAPICommand() *APICommand {
	// Default static directory - check environment variable first
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "/app/ui/dist" // Docker default
	}

	return &APICommand{
		port:      3000,
		staticDir: staticDir,
	}
}

// ParseFlags parses command-line flags for the API command
func (c *APICommand) ParseFlags(args []string) error {
	fs := flag.NewFlagSet("api", flag.ExitOnError)

	fs.IntVar(&c.port, "port", c.port, "Port to listen on")
	fs.IntVar(&c.port, "p", c.port, "Shorthand for --port")
	fs.StringVar(&c.staticDir, "static-dir", c.staticDir, "Directory containing static UI files (empty to disable)")
	fs.StringVar(&c.staticDir, "s", c.staticDir, "Shorthand for --static-dir")

	if err := fs.Parse(args); err != nil {
		return err
	}

	return nil
}

// validateStartup performs pre-flight checks and prints helpful error messages
func (c *APICommand) validateStartup(ctx context.Context) error {
	log.Println("Running startup validation...")

	// Check database directory is writable
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "/data/docksmith.db"
	}
	dbDir := filepath.Dir(dbPath)

	// Check if directory exists
	if info, err := os.Stat(dbDir); err != nil {
		if os.IsNotExist(err) {
			log.Printf("WARNING: Database directory does not exist: %s", dbDir)
			log.Println("  - Create it with: mkdir -p " + dbDir)
			log.Println("  - Or mount a volume: -v ./data:/data")
		} else {
			log.Printf("WARNING: Cannot access database directory %s: %v", dbDir, err)
		}
	} else if !info.IsDir() {
		log.Printf("WARNING: Database path parent is not a directory: %s", dbDir)
	} else {
		// Try to create a test file to verify write access
		testFile := filepath.Join(dbDir, ".docksmith-write-test")
		if f, err := os.Create(testFile); err != nil {
			log.Printf("WARNING: Database directory is not writable: %s", dbDir)
			log.Println("  - Check volume mount permissions")
			log.Println("  - Docksmith runs as UID 1000, ensure the directory is writable")
		} else {
			f.Close()
			os.Remove(testFile)
		}
	}

	log.Println("Startup validation complete")
	return nil
}

// Run starts the API server
func (c *APICommand) Run(ctx context.Context) error {
	// Run startup validation first
	if err := c.validateStartup(ctx); err != nil {
		return err
	}

	// Initialize Docker service
	log.Println("Initializing Docker service...")
	dockerClient, err := docker.NewService()
	if err != nil {
		// Provide helpful error message for common Docker connection issues
		log.Println("")
		log.Println("ERROR: Cannot connect to Docker daemon")
		log.Println("")
		log.Println("Common causes:")
		log.Println("  1. Docker socket not mounted:")
		log.Println("     Add to your docker-compose.yml:")
		log.Println("       volumes:")
		log.Println("         - /var/run/docker.sock:/var/run/docker.sock")
		log.Println("")
		log.Println("  2. Docker daemon not running:")
		log.Println("     sudo systemctl start docker")
		log.Println("")
		log.Println("  3. Permission denied:")
		log.Println("     Docksmith runs as UID 1000 with docker group (GID 972)")
		log.Println("     Check your docker group GID: getent group docker")
		log.Println("")
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Verify Docker connection by listing containers
	if _, err := dockerClient.ListContainers(ctx); err != nil {
		log.Println("")
		log.Println("ERROR: Connected to Docker but cannot list containers")
		log.Println("")
		log.Println("This usually means permission issues with the Docker socket.")
		log.Println("Check that the container has access to /var/run/docker.sock")
		log.Println("")
		return fmt.Errorf("Docker connection test failed: %w", err)
	}
	log.Println("Docker service connected")

	// Initialize storage (optional - graceful degradation)
	var storageService storage.Storage
	store, err := InitializeStorage()
	if err != nil {
		log.Printf("Warning: Failed to initialize storage: %v", err)
		log.Println("Continuing without persistence - some features will be unavailable")
		storageService = nil
	} else {
		defer store.Close()
		storageService = store
		log.Println("Storage service initialized")
	}

	// Initialize registry manager
	token := os.Getenv("GITHUB_TOKEN")
	registryManager := registry.NewManager(token)
	log.Println("Registry manager initialized")

	// Create API server
	server := api.NewServer(api.Config{
		Port:            c.port,
		DockerService:   dockerClient,
		RegistryManager: registryManager,
		StorageService:  storageService,
		StaticDir:       c.staticDir,
	})

	// Handle graceful shutdown
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	errChan := make(chan error, 1)
	go func() {
		errChan <- server.Start()
	}()

	log.Printf("API server running on http://localhost:%d", c.port)
	if c.staticDir != "" {
		log.Printf("UI available at http://localhost:%d/", c.port)
	}
	log.Println("Available endpoints:")
	log.Println("  GET  /api/health     - Server health check")
	log.Println("  GET  /api/check      - Discover and check containers")
	log.Println("  GET  /api/operations - List update operations")
	log.Println("  GET  /api/history    - Check and update history")
	log.Println("  POST /api/update     - Trigger container update")
	log.Println("  POST /api/rollback   - Get rollback information")
	log.Println("")
	log.Println("Press Ctrl+C to stop")

	// Wait for shutdown signal or error
	select {
	case err := <-errChan:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	case <-shutdownChan:
		log.Println("\nReceived shutdown signal...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown error: %w", err)
		}
	}

	log.Println("API server stopped")
	return nil
}
