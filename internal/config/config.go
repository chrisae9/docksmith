package config

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/chis/docksmith/internal/storage"
)

// Config represents the application configuration.
// It can be loaded from YAML files and/or database storage.
// Database values take precedence over YAML values.
type Config struct {
	// ScanDirectories lists directories to scan for compose files
	ScanDirectories []string `yaml:"scan_directories"`

	// ExcludePatterns lists directory patterns to exclude from scanning
	ExcludePatterns []string `yaml:"exclude_patterns"`

	// CacheTTLDays specifies how many days to cache version resolutions
	CacheTTLDays int `yaml:"cache_ttl_days"`

	// ComposeFilePaths contains discovered compose file paths
	ComposeFilePaths []string `yaml:"compose_file_paths"`

	// mu protects concurrent access to the config map
	mu sync.RWMutex

	// values stores configuration as key-value pairs for Get/Set operations
	values map[string]string
}

// Load loads configuration from YAML file and merges with database config.
// Database values take precedence over YAML values.
// If the YAML file doesn't exist, it's not considered an error.
func (c *Config) Load(ctx context.Context, store storage.Storage, yamlPath string) error {
	// Load YAML config (returns empty config if file doesn't exist)
	yamlConfig, err := LoadYAMLConfig(yamlPath)
	if err != nil {
		return fmt.Errorf("failed to load YAML config: %w", err)
	}

	// Load database config
	dbConfig, err := c.loadFromDatabase(ctx, store)
	if err != nil {
		return fmt.Errorf("failed to load database config: %w", err)
	}

	// Merge configs (database takes precedence)
	merged := MergeConfigs(yamlConfig, dbConfig)

	// Update current config
	c.ScanDirectories = merged.ScanDirectories
	c.ExcludePatterns = merged.ExcludePatterns
	c.CacheTTLDays = merged.CacheTTLDays
	c.ComposeFilePaths = merged.ComposeFilePaths

	// Initialize values map from merged config
	c.mu.Lock()
	c.values = c.toMap()
	c.mu.Unlock()

	return nil
}

// loadFromDatabase loads configuration values from the database
func (c *Config) loadFromDatabase(ctx context.Context, store storage.Storage) (Config, error) {
	cfg := Config{
		values: make(map[string]string),
	}

	// Load scan_directories
	if val, found, err := store.GetConfig(ctx, "scan_directories"); err == nil && found {
		var dirs []string
		if err := json.Unmarshal([]byte(val), &dirs); err == nil {
			cfg.ScanDirectories = dirs
		}
	}

	// Load exclude_patterns
	if val, found, err := store.GetConfig(ctx, "exclude_patterns"); err == nil && found {
		var patterns []string
		if err := json.Unmarshal([]byte(val), &patterns); err == nil {
			cfg.ExcludePatterns = patterns
		}
	}

	// Load cache_ttl_days
	if val, found, err := store.GetConfig(ctx, "cache_ttl_days"); err == nil && found {
		if ttl, err := strconv.Atoi(val); err == nil {
			cfg.CacheTTLDays = ttl
		}
	}

	// Load compose_file_paths
	if val, found, err := store.GetConfig(ctx, "compose_file_paths"); err == nil && found {
		var paths []string
		if err := json.Unmarshal([]byte(val), &paths); err == nil {
			cfg.ComposeFilePaths = paths
		}
	}

	return cfg, nil
}

// Save saves the configuration to database storage with snapshot creation.
// Creates a snapshot before saving to enable rollback capability.
func (c *Config) Save(ctx context.Context, store storage.Storage, changedBy string) error {
	c.mu.Lock()
	// Initialize values map if needed (under write lock to avoid race)
	if c.values == nil {
		c.values = c.toMap()
	}
	// Copy values while holding lock, then release
	valuesCopy := make(map[string]string, len(c.values))
	for k, v := range c.values {
		valuesCopy[k] = v
	}
	scanDirs := c.ScanDirectories
	excludePatterns := c.ExcludePatterns
	cacheTTL := c.CacheTTLDays
	composePaths := c.ComposeFilePaths
	c.mu.Unlock()

	// Create snapshot of current configuration before saving
	snapshot := storage.ConfigSnapshot{
		SnapshotTime: time.Now(),
		ConfigData:   valuesCopy,
		ChangedBy:    changedBy,
	}

	if err := store.SaveConfigSnapshot(ctx, snapshot); err != nil {
		return fmt.Errorf("failed to create config snapshot: %w", err)
	}

	fmt.Printf("Config snapshot created: %d keys by %s\n", len(valuesCopy), changedBy)

	// Save scan_directories
	if len(scanDirs) > 0 {
		data, _ := json.Marshal(scanDirs)
		if err := store.SetConfig(ctx, "scan_directories", string(data)); err != nil {
			return fmt.Errorf("failed to save scan_directories: %w", err)
		}
	}

	// Save exclude_patterns
	if len(excludePatterns) > 0 {
		data, _ := json.Marshal(excludePatterns)
		if err := store.SetConfig(ctx, "exclude_patterns", string(data)); err != nil {
			return fmt.Errorf("failed to save exclude_patterns: %w", err)
		}
	}

	// Save cache_ttl_days
	if cacheTTL > 0 {
		if err := store.SetConfig(ctx, "cache_ttl_days", strconv.Itoa(cacheTTL)); err != nil {
			return fmt.Errorf("failed to save cache_ttl_days: %w", err)
		}
	}

	// Save compose_file_paths
	if len(composePaths) > 0 {
		data, _ := json.Marshal(composePaths)
		if err := store.SetConfig(ctx, "compose_file_paths", string(data)); err != nil {
			return fmt.Errorf("failed to save compose_file_paths: %w", err)
		}
	}

	return nil
}

// Get retrieves a configuration value by key.
// Returns the value and true if found, empty string and false otherwise.
func (c *Config) Get(key string) (string, bool) {
	c.mu.RLock()
	if c.values != nil {
		val, found := c.values[key]
		c.mu.RUnlock()
		return val, found
	}
	c.mu.RUnlock()

	// values is nil - need write lock to initialize
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine may have initialized)
	if c.values == nil {
		c.values = c.toMap()
	}

	val, found := c.values[key]
	return val, found
}

// Set updates a configuration value in memory.
// Does not persist to storage - call Save() to persist changes.
func (c *Config) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.values == nil {
		c.values = make(map[string]string)
	}

	c.values[key] = value

	// Update struct fields based on key
	switch key {
	case "cache_ttl_days":
		if ttl, err := strconv.Atoi(value); err == nil {
			c.CacheTTLDays = ttl
		}
	case "scan_directories":
		var dirs []string
		if err := json.Unmarshal([]byte(value), &dirs); err == nil {
			c.ScanDirectories = dirs
		}
	case "exclude_patterns":
		var patterns []string
		if err := json.Unmarshal([]byte(value), &patterns); err == nil {
			c.ExcludePatterns = patterns
		}
	case "compose_file_paths":
		var paths []string
		if err := json.Unmarshal([]byte(value), &paths); err == nil {
			c.ComposeFilePaths = paths
		}
	}
}

// toMap converts the config struct to a map for Get/Set operations
func (c *Config) toMap() map[string]string {
	m := make(map[string]string)

	if len(c.ScanDirectories) > 0 {
		data, _ := json.Marshal(c.ScanDirectories)
		m["scan_directories"] = string(data)
	}

	if len(c.ExcludePatterns) > 0 {
		data, _ := json.Marshal(c.ExcludePatterns)
		m["exclude_patterns"] = string(data)
	}

	if c.CacheTTLDays > 0 {
		m["cache_ttl_days"] = strconv.Itoa(c.CacheTTLDays)
	}

	if len(c.ComposeFilePaths) > 0 {
		data, _ := json.Marshal(c.ComposeFilePaths)
		m["compose_file_paths"] = string(data)
	}

	return m
}

// MergeConfigs merges two configurations with database config taking precedence.
// YAML config provides defaults, database config overrides specific values.
func MergeConfigs(yamlConfig, dbConfig Config) Config {
	merged := yamlConfig // Start with YAML defaults

	// Database values override YAML values
	if len(dbConfig.ScanDirectories) > 0 {
		merged.ScanDirectories = dbConfig.ScanDirectories
	}

	if len(dbConfig.ExcludePatterns) > 0 {
		merged.ExcludePatterns = dbConfig.ExcludePatterns
	}

	if dbConfig.CacheTTLDays > 0 {
		merged.CacheTTLDays = dbConfig.CacheTTLDays
	}

	if len(dbConfig.ComposeFilePaths) > 0 {
		merged.ComposeFilePaths = dbConfig.ComposeFilePaths
	}

	return merged
}
