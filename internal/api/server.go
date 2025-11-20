package api

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chis/docksmith/internal/config"
	"github.com/chis/docksmith/internal/docker"
	"github.com/chis/docksmith/internal/events"
	"github.com/chis/docksmith/internal/registry"
	"github.com/chis/docksmith/internal/scripts"
	"github.com/chis/docksmith/internal/storage"
	"github.com/chis/docksmith/internal/update"
)

// Server represents the HTTP API server
type Server struct {
	dockerService         *docker.Service
	registryManager       *registry.Manager
	storageService        storage.Storage
	discoveryOrchestrator *update.Orchestrator
	updateOrchestrator    *update.UpdateOrchestrator
	scriptManager         *scripts.Manager
	eventBus              *events.Bus
	httpServer            *http.Server
	pathTranslator        *docker.PathTranslator
}

// Config holds configuration for the API server
type Config struct {
	Port            int
	DockerService   *docker.Service
	RegistryManager *registry.Manager
	StorageService  storage.Storage
	StaticDir       string // Directory containing static UI files (optional)
}

// NewServer creates a new API server with the given configuration
func NewServer(cfg Config) *Server {
	eventBus := events.NewBus()

	discoveryOrchestrator := update.NewOrchestrator(cfg.DockerService, cfg.RegistryManager)
	discoveryOrchestrator.SetEventBus(eventBus) // Enable check progress events
	if cfg.StorageService != nil {
		discoveryOrchestrator.SetStorage(cfg.StorageService)
	}

	var updateOrchestrator *update.UpdateOrchestrator
	if cfg.StorageService != nil {
		updateOrchestrator = update.NewUpdateOrchestrator(
			cfg.DockerService,
			cfg.DockerService.GetClient(),
			cfg.StorageService,
			eventBus,
			cfg.RegistryManager,
			cfg.DockerService.GetPathTranslator(),
		)
	}

	// Initialize script manager if storage is available
	var scriptManager *scripts.Manager
	if cfg.StorageService != nil {
		// Load config for script manager
		appConfig := &config.Config{}
		ctx := context.Background()
		configPath := os.Getenv("CONFIG_PATH")
		if configPath == "" {
			configPath = "/data/docksmith.yaml"
		}
		if err := appConfig.Load(ctx, cfg.StorageService, configPath); err != nil {
			log.Printf("Warning: Failed to load config for script manager: %v", err)
		}

		// Run compose file discovery to populate ComposeFilePaths
		// This is required for the script manager to find containers in compose files
		scanner := config.NewScanner(cfg.StorageService, appConfig)
		discoveredPaths, err := scanner.ScanAll(ctx)
		if err != nil {
			log.Printf("Warning: Failed to discover compose files: %v", err)
		} else {
			log.Printf("Discovered %d compose files", len(discoveredPaths))
			// Reload config to get the discovered paths
			if err := appConfig.Load(ctx, cfg.StorageService, configPath); err != nil {
				log.Printf("Warning: Failed to reload config after discovery: %v", err)
			}
		}

		scriptManager = scripts.NewManager(cfg.StorageService, appConfig)
	}

	s := &Server{
		dockerService:         cfg.DockerService,
		registryManager:       cfg.RegistryManager,
		storageService:        cfg.StorageService,
		discoveryOrchestrator: discoveryOrchestrator,
		updateOrchestrator:    updateOrchestrator,
		scriptManager:         scriptManager,
		eventBus:              eventBus,
		pathTranslator:        cfg.DockerService.GetPathTranslator(),
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	s.registerRoutes(mux, cfg.StaticDir)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      corsMiddleware(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 120 * time.Second, // Long timeout for discovery operations
		IdleTimeout:  60 * time.Second,
	}

	return s
}

// registerRoutes sets up all API routes
func (s *Server) registerRoutes(mux *http.ServeMux, staticDir string) {
	// Health check
	mux.HandleFunc("GET /api/health", s.handleHealth)

	// Container discovery and checking
	mux.HandleFunc("GET /api/check", s.handleCheck)

	// Operations history
	mux.HandleFunc("GET /api/operations", s.handleOperations)
	mux.HandleFunc("GET /api/operations/{id}", s.handleOperationByID)

	// Check and update history
	mux.HandleFunc("GET /api/history", s.handleHistory)

	// Compose backups
	mux.HandleFunc("GET /api/backups", s.handleBackups)

	// Rollback policies
	mux.HandleFunc("GET /api/policies", s.handlePolicies)

	// Script management
	mux.HandleFunc("GET /api/scripts", s.handleScriptsList)
	mux.HandleFunc("GET /api/scripts/assigned", s.handleScriptsAssigned)
	mux.HandleFunc("POST /api/scripts/assign", s.handleScriptsAssign)
	mux.HandleFunc("DELETE /api/scripts/assign/{container}", s.handleScriptsUnassign)

	// Label management (atomic: compose + restart)
	mux.HandleFunc("GET /api/labels/{container}", s.handleLabelsGet)
	mux.HandleFunc("POST /api/labels/set", s.handleLabelsSet)
	mux.HandleFunc("POST /api/labels/remove", s.handleLabelsRemove)

	// Container settings (deprecated - use labels endpoints instead)
	mux.HandleFunc("POST /api/settings/ignore", s.handleSettingsIgnore)
	mux.HandleFunc("POST /api/settings/allow-latest", s.handleSettingsAllowLatest)

	// Mutations (POST/PUT/DELETE)
	mux.HandleFunc("POST /api/update", s.handleUpdate)
	mux.HandleFunc("POST /api/update/batch", s.handleBatchUpdate)
	mux.HandleFunc("POST /api/rollback", s.handleRollback)

	// Restart operations
	mux.HandleFunc("POST /api/restart/container/{name}", s.handleRestartContainer)
	mux.HandleFunc("POST /api/restart/stack/{name}", s.handleRestartStack)
	mux.HandleFunc("POST /api/restart", s.handleRestartContainerBody)

	// Server-Sent Events for real-time updates
	mux.HandleFunc("GET /api/events", s.handleEvents)

	// Serve static UI files if directory is configured
	if staticDir != "" {
		if _, err := os.Stat(staticDir); err == nil {
			log.Printf("Serving static UI from %s", staticDir)
			mux.Handle("/", spaHandler(staticDir))
		} else {
			log.Printf("Static directory %s not found, UI will not be served", staticDir)
		}
	}
}

// Start starts the HTTP server
func (s *Server) Start() error {
	log.Printf("Starting API server on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down API server...")
	return s.httpServer.Shutdown(ctx)
}

// corsMiddleware adds CORS headers for development
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allow requests from common development ports
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}

		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// spaHandler serves static files and falls back to index.html for SPA routing
func spaHandler(staticDir string) http.Handler {
	fileServer := http.FileServer(http.Dir(staticDir))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't serve API routes through static handler
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		// Clean the path
		path := filepath.Clean(r.URL.Path)
		if path == "/" {
			path = "/index.html"
		}

		// Check if file exists
		fullPath := filepath.Join(staticDir, path)
		_, err := os.Stat(fullPath)

		if os.IsNotExist(err) {
			// File doesn't exist, serve index.html for SPA routing
			indexPath := filepath.Join(staticDir, "index.html")
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
			http.NotFound(w, r)
			return
		} else if err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Check if it's a directory
		info, _ := os.Stat(fullPath)
		if info.IsDir() {
			// Try to serve index.html in the directory
			indexPath := filepath.Join(fullPath, "index.html")
			if _, indexErr := os.Stat(indexPath); indexErr == nil {
				http.ServeFile(w, r, indexPath)
				return
			}
			// Otherwise serve root index.html for SPA routing
			rootIndex := filepath.Join(staticDir, "index.html")
			http.ServeFile(w, r, rootIndex)
			return
		}

		// Serve the file
		fileServer.ServeHTTP(w, r)
	})
}

// Ensure fs is used (required by imports)
var _ = fs.FS(nil)
