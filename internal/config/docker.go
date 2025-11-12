package config

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// DockerConfig represents the Docker configuration file structure.
// Typically found at ~/.docker/config.json
type DockerConfig struct {
	// Auths maps registry URLs to authentication configs
	Auths map[string]AuthConfig `json:"auths"`
}

// AuthConfig represents authentication configuration for a registry.
// The Auth field contains base64-encoded "username:password" string.
type AuthConfig struct {
	// Auth is a base64-encoded string in format "username:password"
	Auth string `json:"auth"`
}

// ReadDockerConfig reads and parses a Docker config file.
// Returns an error if the file doesn't exist or contains invalid JSON.
// Parameters:
//   - configPath: Path to the Docker config file (typically ~/.docker/config.json)
func ReadDockerConfig(configPath string) (*DockerConfig, error) {
	// Read the config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read Docker config file: %w", err)
	}

	// Parse JSON
	var config DockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse Docker config JSON: %w", err)
	}

	return &config, nil
}

// DecodeAuth decodes a base64-encoded auth string into username and password.
// The auth string is expected to be in format "username:password" after base64 decoding.
// Returns an error if:
//   - The base64 decoding fails
//   - The decoded string doesn't contain a colon separator
//   - The auth string is empty
//
// Parameters:
//   - base64Auth: Base64-encoded "username:password" string
//
// Returns:
//   - username: The decoded username
//   - password: The decoded password (may contain colons)
//   - err: Any error that occurred during decoding
func DecodeAuth(base64Auth string) (username, password string, err error) {
	if base64Auth == "" {
		return "", "", fmt.Errorf("auth string is empty")
	}

	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(base64Auth)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode base64 auth string: %w", err)
	}

	// Split on first colon to extract username and password
	// Password may contain colons, so only split on the first one
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid auth format: expected 'username:password', got %d parts", len(parts))
	}

	return parts[0], parts[1], nil
}

// ListRegistries reads a Docker config file and returns a sorted list of registry URLs.
// Does not expose credentials - returns only the registry URLs from the "auths" section.
// Returns an error if the config file cannot be read or parsed.
//
// Parameters:
//   - configPath: Path to the Docker config file (typically ~/.docker/config.json)
//
// Returns:
//   - []string: Sorted list of registry URLs
//   - error: Any error that occurred during reading or parsing
func ListRegistries(configPath string) ([]string, error) {
	// Read Docker config
	config, err := ReadDockerConfig(configPath)
	if err != nil {
		return nil, err
	}

	// Extract registry URLs (keys from auths map)
	registries := make([]string, 0, len(config.Auths))
	for registryURL := range config.Auths {
		registries = append(registries, registryURL)
	}

	// Sort for consistent output
	sort.Strings(registries)

	return registries, nil
}

// ValidateDockerConfig validates a Docker config file.
// Checks:
//   - File exists and is readable
//   - JSON structure is valid
//   - Auth fields are properly base64 encoded
//
// Parameters:
//   - configPath: Path to the Docker config file (typically ~/.docker/config.json)
//
// Returns:
//   - ValidationResult with errors for blocking issues and warnings for non-blocking issues
func ValidateDockerConfig(configPath string) ValidationResult {
	result := ValidationResult{}

	// Check file exists
	if _, err := os.Stat(configPath); err != nil {
		if os.IsNotExist(err) {
			result.AddError(fmt.Sprintf("Docker config file does not exist: %s", configPath))
		} else {
			result.AddError(fmt.Sprintf("cannot access Docker config file: %v", err))
		}
		return result
	}

	// Try to read and parse the config
	config, err := ReadDockerConfig(configPath)
	if err != nil {
		result.AddError(fmt.Sprintf("failed to read Docker config: %v", err))
		return result
	}

	// Validate each auth entry
	if len(config.Auths) == 0 {
		result.AddWarning("Docker config contains no registry authentications")
		return result
	}

	// Check that auth fields are valid base64
	for registry, authConfig := range config.Auths {
		if authConfig.Auth == "" {
			result.AddWarning(fmt.Sprintf("registry %s has empty auth field", registry))
			continue
		}

		// Try to decode the auth string
		_, _, err := DecodeAuth(authConfig.Auth)
		if err != nil {
			result.AddError(fmt.Sprintf("registry %s has invalid auth encoding: %v", registry, err))
		}
	}

	return result
}
