package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadYAMLConfig loads configuration from a YAML file.
// Returns an empty config if the file doesn't exist (not an error).
// Returns an error only if the file exists but cannot be parsed.
// Returns a pointer to avoid copying the sync.RWMutex embedded in Config.
func LoadYAMLConfig(path string) (*Config, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// File doesn't exist - return empty config (not an error)
		return &Config{values: make(map[string]string)}, nil
	}

	// Read file contents
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML config file: %w", err)
	}

	// Parse YAML into Config struct
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	// Initialize values map
	cfg.values = make(map[string]string)
	return &cfg, nil
}
