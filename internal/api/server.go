package api

import (
	"context"
	"encoding/json"
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
	backgroundChecker     *update.BackgroundChecker
	checkInterval         time.Duration
	cacheTTL              time.Duration
	rateLimiter           *PathRateLimiter
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

	// Parse cache TTL from environment variable
	cacheTTL := 1 * time.Hour // Default to 1 hour
	if cacheTTLStr := os.Getenv("CACHE_TTL"); cacheTTLStr != "" {
		if parsed, err := time.ParseDuration(cacheTTLStr); err == nil {
			cacheTTL = parsed
			log.Printf("Using CACHE_TTL: %v", cacheTTL)
		} else {
			log.Printf("Warning: Invalid CACHE_TTL '%s', using default %v", cacheTTLStr, cacheTTL)
		}
	}
	discoveryOrchestrator.EnableCache(cacheTTL) // Cache registry responses to avoid rate limits

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

	// Parse check interval from environment variable
	checkInterval := 5 * time.Minute // Default to 5 minutes
	if intervalStr := os.Getenv("CHECK_INTERVAL"); intervalStr != "" {
		if parsed, err := time.ParseDuration(intervalStr); err == nil {
			checkInterval = parsed
			log.Printf("Using CHECK_INTERVAL: %v", checkInterval)
		} else {
			log.Printf("Warning: Invalid CHECK_INTERVAL '%s', using default %v", intervalStr, checkInterval)
		}
	}

	// Create background checker using the discovery orchestrator
	backgroundChecker := update.NewBackgroundChecker(discoveryOrchestrator, cfg.DockerService, eventBus, cfg.StorageService, checkInterval)

	// Create rate limiter with path-specific limits (can be disabled for testing)
	var rateLimiter *PathRateLimiter
	disableRateLimit := os.Getenv("DOCKSMITH_DISABLE_RATE_LIMIT") == "true"
	if !disableRateLimit {
		rateLimiter = NewPathRateLimiter(DefaultRateLimitConfig())
		// Allow higher rate for SSE events endpoint (long-lived connections)
		rateLimiter.SetPathLimit("/api/events", RateLimitConfig{
			RequestsPerMinute: 10,
			BurstSize:         5,
			CleanupInterval:   5 * time.Minute,
		})
		// Allow higher rate for health checks
		rateLimiter.SetPathLimit("/api/health", RateLimitConfig{
			RequestsPerMinute: 120,
			BurstSize:         20,
			CleanupInterval:   5 * time.Minute,
		})
		// Lower rate for mutation endpoints
		rateLimiter.SetPathLimit("/api/update", RateLimitConfig{
			RequestsPerMinute: 30,
			BurstSize:         5,
			CleanupInterval:   5 * time.Minute,
		})
	} else {
		log.Printf("Rate limiting disabled via DOCKSMITH_DISABLE_RATE_LIMIT")
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
		backgroundChecker:     backgroundChecker,
		checkInterval:         checkInterval,
		cacheTTL:              cacheTTL,
		rateLimiter:           rateLimiter,
	}

	// Setup HTTP server with middleware chain
	mux := http.NewServeMux()
	s.registerRoutes(mux, cfg.StaticDir)

	// Apply middleware: CORS -> Correlation ID -> Rate Limit (optional) -> Request Logging -> Handler
	middlewares := []func(http.Handler) http.Handler{
		corsMiddleware,
		CorrelationIDMiddleware,
	}
	if rateLimiter != nil {
		middlewares = append(middlewares, PathRateLimitMiddleware(rateLimiter))
	}
	// Uncomment to enable request logging (can be verbose):
	// middlewares = append(middlewares, RequestLoggingMiddleware)
	handler := ChainMiddleware(mux, middlewares...)

	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      handler,
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

	// Docker configuration
	mux.HandleFunc("GET /api/docker-config", s.handleDockerConfig)

	// Container discovery and checking
	mux.HandleFunc("GET /api/check", s.handleCheck)
	mux.HandleFunc("GET /api/status", s.handleGetStatus)
	mux.HandleFunc("POST /api/trigger-check", s.handleTriggerCheck)
	mux.HandleFunc("GET /api/container/{name}/recheck", s.handleContainerRecheck)

	// Operations history
	mux.HandleFunc("GET /api/operations", s.handleOperations)
	mux.HandleFunc("GET /api/operations/{id}", s.handleOperationByID)

	// Check and update history
	mux.HandleFunc("GET /api/history", s.handleHistory)

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

	// Registry tags (for regex testing UI)
	mux.HandleFunc("GET /api/registry/tags/{imageRef...}", s.handleRegistryTags)

	// Container settings (deprecated - use labels endpoints instead)
	mux.HandleFunc("POST /api/settings/ignore", s.handleSettingsIgnore)
	mux.HandleFunc("POST /api/settings/allow-latest", s.handleSettingsAllowLatest)

	// Mutations (POST/PUT/DELETE)
	mux.HandleFunc("POST /api/update", s.handleUpdate)
	mux.HandleFunc("POST /api/update/batch", s.handleBatchUpdate)
	mux.HandleFunc("POST /api/rollback", s.handleRollback)
	mux.HandleFunc("POST /api/fix-compose-mismatch/{name}", s.handleFixComposeMismatch)

	// Restart operations
	mux.HandleFunc("POST /api/restart/start/{name}", s.handleStartRestart) // New SSE-based restart
	mux.HandleFunc("POST /api/restart/container/{name}", s.handleRestartContainer)
	mux.HandleFunc("POST /api/restart/stack/{name}", s.handleRestartStack)
	mux.HandleFunc("POST /api/restart", s.handleRestartContainerBody)

	// Server-Sent Events for real-time updates
	mux.HandleFunc("GET /api/events", s.handleEvents)

	// Explorer endpoints
	mux.HandleFunc("GET /api/explorer", s.handleExplorer)
	mux.HandleFunc("GET /api/images", s.handleImages)
	mux.HandleFunc("GET /api/networks", s.handleNetworks)
	mux.HandleFunc("GET /api/volumes", s.handleVolumes)
	mux.HandleFunc("DELETE /api/images/{id}", s.handleRemoveImage)
	mux.HandleFunc("DELETE /api/networks/{id}", s.handleRemoveNetwork)
	mux.HandleFunc("DELETE /api/volumes/{name}", s.handleRemoveVolume)

	// Prune endpoints
	mux.HandleFunc("POST /api/prune/containers", s.handlePruneContainers)
	mux.HandleFunc("POST /api/prune/images", s.handlePruneImages)
	mux.HandleFunc("POST /api/prune/networks", s.handlePruneNetworks)
	mux.HandleFunc("POST /api/prune/volumes", s.handlePruneVolumes)
	mux.HandleFunc("POST /api/prune/system", s.handleSystemPrune)

	// Container operations
	mux.HandleFunc("GET /api/containers/{name}/logs", s.handleContainerLogs)
	mux.HandleFunc("GET /api/containers/{name}/inspect", s.handleContainerInspect)
	mux.HandleFunc("GET /api/containers/{name}/stats", s.handleContainerStats)
	mux.HandleFunc("POST /api/containers/{name}/stop", s.handleContainerStop)
	mux.HandleFunc("POST /api/containers/{name}/start", s.handleContainerStart)
	mux.HandleFunc("POST /api/containers/{name}/restart", s.handleContainerRestart)
	mux.HandleFunc("DELETE /api/containers/{name}", s.handleContainerRemove)

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
	// Start background checker
	if s.backgroundChecker != nil {
		s.backgroundChecker.Start()
	}

	log.Printf("Starting API server on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	log.Println("Shutting down API server...")

	// Stop background checker
	if s.backgroundChecker != nil {
		s.backgroundChecker.Stop()
	}

	// Stop rate limiter cleanup goroutines
	if s.rateLimiter != nil {
		s.rateLimiter.Stop()
	}

	return s.httpServer.Shutdown(ctx)
}

// corsMiddleware adds CORS headers for development.
// Returns middleware function compatible with ChainMiddleware.
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
				// HTML files: always validate (allows 304 responses for better mobile performance)
				w.Header().Set("Cache-Control", "no-cache")
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
				// HTML files: always validate (allows 304 responses for better mobile performance)
				w.Header().Set("Cache-Control", "no-cache")
				http.ServeFile(w, r, indexPath)
				return
			}
			// Otherwise serve root index.html for SPA routing
			rootIndex := filepath.Join(staticDir, "index.html")
			// HTML files: always validate (allows 304 responses for better mobile performance)
			w.Header().Set("Cache-Control", "no-cache")
			http.ServeFile(w, r, rootIndex)
			return
		}

		// Set cache headers based on file type
		if strings.HasSuffix(path, ".html") {
			// HTML files: always validate (allows 304 responses for better mobile performance)
			w.Header().Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(path, "/assets/") {
			// Versioned assets (JS/CSS with hashes): cache forever
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			// Other static files (images, fonts): cache for 1 hour
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}

		// Serve the file
		fileServer.ServeHTTP(w, r)
	})
}

// handleRegistryTags returns the list of available tags for an image from the registry cache
// GET /api/registry/tags/{imageRef...}
func (s *Server) handleRegistryTags(w http.ResponseWriter, r *http.Request) {
	imageRef := r.PathValue("imageRef")
	if !validateRequired(w, "image reference", imageRef) {
		return
	}

	ctx := r.Context()

	// Fetch tags from registry (uses cached data if available)
	tags, err := s.registryManager.ListTags(ctx, imageRef)
	if err != nil {
		RespondInternalError(w, fmt.Errorf("failed to fetch tags: %w", err))
		return
	}

	RespondSuccess(w, map[string]any{
		"image_ref": imageRef,
		"tags":      tags,
		"count":     len(tags),
	})
}

// decodeJSONRequest decodes a JSON request body into the provided interface.
// Returns true if successful. If decoding fails, it writes the error response and returns false.
func decodeJSONRequest(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		RespondBadRequest(w, fmt.Errorf("invalid request body"))
		return false
	}
	return true
}

// Ensure fs is used (required by imports)
var _ = fs.FS(nil)
