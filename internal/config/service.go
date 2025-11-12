package config

import (
	"context"
	"fmt"
	"time"

	"github.com/chis/docksmith/internal/storage"
)

// Service manages configuration loading, merging, and caching.
// It follows the service pattern from internal/docker/service.go.
type Service struct {
	storage  storage.Storage
	config   *Config
	yamlPath string
}

// NewService creates a new configuration service.
// It loads the YAML config, merges it with database config, and caches the result in memory.
// Returns an error if configuration cannot be loaded.
func NewService(store storage.Storage, yamlPath string) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("storage cannot be nil")
	}

	cfg := &Config{}
	ctx := context.Background()

	// Load and merge YAML and database config
	if err := cfg.Load(ctx, store, yamlPath); err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	service := &Service{
		storage:  store,
		config:   cfg,
		yamlPath: yamlPath,
	}

	// Create initial snapshot if no history exists
	if err := service.ensureInitialSnapshot(ctx); err != nil {
		// Log error but don't fail service creation
		fmt.Printf("Warning: Failed to create initial config snapshot: %v\n", err)
	}

	return service, nil
}

// ensureInitialSnapshot creates an initial config snapshot if none exists
func (s *Service) ensureInitialSnapshot(ctx context.Context) error {
	// Check if any snapshots exist
	history, err := s.storage.GetConfigHistory(ctx, 1)
	if err != nil {
		return fmt.Errorf("failed to check config history: %w", err)
	}

	// If no snapshots exist, create an initial one
	if len(history) == 0 {
		// Build config data map from current config
		configData := make(map[string]string)
		if val, found := s.config.Get("scan_directories"); found {
			configData["scan_directories"] = val
		}
		if val, found := s.config.Get("exclude_patterns"); found {
			configData["exclude_patterns"] = val
		}
		if val, found := s.config.Get("cache_ttl_days"); found {
			configData["cache_ttl_days"] = val
		}
		if val, found := s.config.Get("compose_file_paths"); found {
			configData["compose_file_paths"] = val
		}

		snapshot := storage.ConfigSnapshot{
			SnapshotTime: time.Now(),
			ConfigData:   configData,
			ChangedBy:    "system-init",
		}

		if err := s.storage.SaveConfigSnapshot(ctx, snapshot); err != nil {
			return fmt.Errorf("failed to create initial snapshot: %w", err)
		}

		fmt.Println("Created initial configuration snapshot")
	}

	return nil
}

// GetConfig returns the current cached configuration.
func (s *Service) GetConfig() *Config {
	return s.config
}

// Reload reloads the configuration from YAML and database.
// Useful for hot-reloading configuration changes.
func (s *Service) Reload(ctx context.Context) error {
	cfg := &Config{}
	if err := cfg.Load(ctx, s.storage, s.yamlPath); err != nil {
		return fmt.Errorf("failed to reload configuration: %w", err)
	}

	s.config = cfg
	return nil
}

// SaveConfig saves the current configuration to database with snapshot.
func (s *Service) SaveConfig(ctx context.Context, changedBy string) error {
	if err := s.config.Save(ctx, s.storage, changedBy); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	return nil
}

// UpdateConfig updates configuration values in memory and saves to database.
func (s *Service) UpdateConfig(ctx context.Context, updates map[string]string, changedBy string) error {
	// Update in-memory config
	for key, value := range updates {
		s.config.Set(key, value)
	}

	// Save to database with snapshot
	if err := s.config.Save(ctx, s.storage, changedBy); err != nil {
		return fmt.Errorf("failed to save updated configuration: %w", err)
	}

	return nil
}

// GetConfigValue retrieves a specific configuration value.
func (s *Service) GetConfigValue(key string) (string, bool) {
	return s.config.Get(key)
}

// GetConfigHistory retrieves configuration history from storage.
func (s *Service) GetConfigHistory(ctx context.Context, limit int) ([]storage.ConfigSnapshot, error) {
	history, err := s.storage.GetConfigHistory(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get config history: %w", err)
	}

	return history, nil
}

// RevertToSnapshot reverts configuration to a specific snapshot.
func (s *Service) RevertToSnapshot(ctx context.Context, snapshotID int64) error {
	if err := s.storage.RevertToSnapshot(ctx, snapshotID); err != nil {
		return fmt.Errorf("failed to revert to snapshot: %w", err)
	}

	// Reload configuration after revert
	if err := s.Reload(ctx); err != nil {
		return fmt.Errorf("failed to reload after revert: %w", err)
	}

	return nil
}
