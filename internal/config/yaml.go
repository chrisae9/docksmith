package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadYAMLConfig loads configuration from a YAML file.
// Returns an empty config if the file doesn't exist (not an error).
// Returns an error only if the file exists but cannot be parsed.
func LoadYAMLConfig(path string) (Config, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// File doesn't exist - return empty config (not an error)
		return Config{}, nil
	}

	// Read file contents
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("failed to read YAML config file: %w", err)
	}

	// Parse YAML into Config struct
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("failed to parse YAML config: %w", err)
	}

	return cfg, nil
}
