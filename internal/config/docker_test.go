package config

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// TestReadDockerConfig tests parsing of Docker config.json
func TestReadDockerConfig(t *testing.T) {
	// Create temporary test config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Test data
	configData := map[string]interface{}{
		"auths": map[string]interface{}{
			"docker.io": map[string]interface{}{
				"auth": base64.StdEncoding.EncodeToString([]byte("testuser:testpass")),
			},
			"ghcr.io": map[string]interface{}{
				"auth": base64.StdEncoding.EncodeToString([]byte("ghuser:ghtoken")),
			},
		},
	}

	// Write test config file
	data, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Test reading the config
	config, err := ReadDockerConfig(configPath)
	if err != nil {
		t.Fatalf("ReadDockerConfig failed: %v", err)
	}

	if config == nil {
		t.Fatal("Expected non-nil config")
	}

	if len(config.Auths) != 2 {
		t.Errorf("Expected 2 auths, got %d", len(config.Auths))
	}

	// Verify specific auth entries
	dockerAuth, found := config.Auths["docker.io"]
	if !found {
		t.Error("Expected docker.io auth entry")
	}

	if dockerAuth.Auth == "" {
		t.Error("Expected non-empty auth string for docker.io")
	}

	ghcrAuth, found := config.Auths["ghcr.io"]
	if !found {
		t.Error("Expected ghcr.io auth entry")
	}

	if ghcrAuth.Auth == "" {
		t.Error("Expected non-empty auth string for ghcr.io")
	}
}

// TestReadDockerConfig_MissingFile tests graceful handling of missing config file
func TestReadDockerConfig_MissingFile(t *testing.T) {
	// Try to read non-existent file
	_, err := ReadDockerConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}
}

// TestReadDockerConfig_InvalidJSON tests handling of invalid JSON
func TestReadDockerConfig_InvalidJSON(t *testing.T) {
	// Create temporary test config file with invalid JSON
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	invalidJSON := []byte("{invalid json content")
	if err := os.WriteFile(configPath, invalidJSON, 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Test reading the invalid config
	_, err := ReadDockerConfig(configPath)
	if err == nil {
		t.Error("Expected error for invalid JSON, got nil")
	}
}

// TestDecodeAuth tests extracting username and password from base64
func TestDecodeAuth(t *testing.T) {
	tests := []struct {
		name       string
		base64Auth string
		wantUser   string
		wantPass   string
		wantErr    bool
	}{
		{
			name:       "valid auth string",
			base64Auth: base64.StdEncoding.EncodeToString([]byte("myuser:mypassword")),
			wantUser:   "myuser",
			wantPass:   "mypassword",
			wantErr:    false,
		},
		{
			name:       "auth with colon in password",
			base64Auth: base64.StdEncoding.EncodeToString([]byte("user:pass:word")),
			wantUser:   "user",
			wantPass:   "pass:word",
			wantErr:    false,
		},
		{
			name:       "invalid base64",
			base64Auth: "not-valid-base64!@#",
			wantUser:   "",
			wantPass:   "",
			wantErr:    true,
		},
		{
			name:       "missing colon separator",
			base64Auth: base64.StdEncoding.EncodeToString([]byte("no-colon-here")),
			wantUser:   "",
			wantPass:   "",
			wantErr:    true,
		},
		{
			name:       "empty auth string",
			base64Auth: "",
			wantUser:   "",
			wantPass:   "",
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, pass, err := DecodeAuth(tt.base64Auth)

			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeAuth() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if user != tt.wantUser {
				t.Errorf("DecodeAuth() user = %v, want %v", user, tt.wantUser)
			}

			if pass != tt.wantPass {
				t.Errorf("DecodeAuth() pass = %v, want %v", pass, tt.wantPass)
			}
		})
	}
}

// TestListRegistries tests extracting registry URLs from config
func TestListRegistries(t *testing.T) {
	// Create temporary test config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Test data with multiple registries
	configData := map[string]interface{}{
		"auths": map[string]interface{}{
			"https://index.docker.io/v1/": map[string]interface{}{
				"auth": base64.StdEncoding.EncodeToString([]byte("user1:pass1")),
			},
			"ghcr.io": map[string]interface{}{
				"auth": base64.StdEncoding.EncodeToString([]byte("user2:pass2")),
			},
			"registry.example.com": map[string]interface{}{
				"auth": base64.StdEncoding.EncodeToString([]byte("user3:pass3")),
			},
		},
	}

	// Write test config file
	data, err := json.MarshalIndent(configData, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal test config: %v", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Test listing registries
	registries, err := ListRegistries(configPath)
	if err != nil {
		t.Fatalf("ListRegistries failed: %v", err)
	}

	if len(registries) != 3 {
		t.Errorf("Expected 3 registries, got %d", len(registries))
	}

	// Verify registries are sorted
	expectedRegistries := []string{
		"ghcr.io",
		"https://index.docker.io/v1/",
		"registry.example.com",
	}
	sort.Strings(expectedRegistries)

	for i, expected := range expectedRegistries {
		if registries[i] != expected {
			t.Errorf("Registry[%d] = %v, want %v", i, registries[i], expected)
		}
	}

	// Verify no credentials are exposed
	for _, registry := range registries {
		if registry == "" {
			t.Error("Empty registry URL found")
		}
		// Ensure no auth data is in the registry URL
		if len(registry) > 200 {
			t.Errorf("Registry URL suspiciously long: %d chars (possible credential leak)", len(registry))
		}
	}
}

// TestListRegistries_MissingFile tests handling of missing config file
func TestListRegistries_MissingFile(t *testing.T) {
	_, err := ListRegistries("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}
}

// TestValidateDockerConfig tests validation of Docker config file
func TestValidateDockerConfig(t *testing.T) {
	tests := []struct {
		name          string
		setupConfig   func(string) error
		wantValid     bool
		wantWarnings  bool
		wantErrCount  int
		wantWarnCount int
	}{
		{
			name: "valid config",
			setupConfig: func(dir string) error {
				configPath := filepath.Join(dir, "config.json")
				configData := map[string]interface{}{
					"auths": map[string]interface{}{
						"docker.io": map[string]interface{}{
							"auth": base64.StdEncoding.EncodeToString([]byte("user:pass")),
						},
					},
				}
				data, _ := json.Marshal(configData)
				return os.WriteFile(configPath, data, 0600)
			},
			wantValid:     true,
			wantWarnings:  false,
			wantErrCount:  0,
			wantWarnCount: 0,
		},
		{
			name: "missing file",
			setupConfig: func(dir string) error {
				// Don't create the file
				return nil
			},
			wantValid:     false,
			wantWarnings:  false,
			wantErrCount:  1,
			wantWarnCount: 0,
		},
		{
			name: "invalid JSON",
			setupConfig: func(dir string) error {
				configPath := filepath.Join(dir, "config.json")
				return os.WriteFile(configPath, []byte("{invalid}"), 0600)
			},
			wantValid:     false,
			wantWarnings:  false,
			wantErrCount:  1,
			wantWarnCount: 0,
		},
		{
			name: "invalid base64 auth",
			setupConfig: func(dir string) error {
				configPath := filepath.Join(dir, "config.json")
				configData := map[string]interface{}{
					"auths": map[string]interface{}{
						"docker.io": map[string]interface{}{
							"auth": "not-valid-base64!@#",
						},
					},
				}
				data, _ := json.Marshal(configData)
				return os.WriteFile(configPath, data, 0600)
			},
			wantValid:     false,
			wantWarnings:  false,
			wantErrCount:  1,
			wantWarnCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			configPath := filepath.Join(tmpDir, "config.json")

			// Setup test config
			if err := tt.setupConfig(tmpDir); err != nil {
				t.Fatalf("Failed to setup test config: %v", err)
			}

			// Validate config
			result := ValidateDockerConfig(configPath)

			if result.IsValid() != tt.wantValid {
				t.Errorf("ValidateDockerConfig() valid = %v, want %v", result.IsValid(), tt.wantValid)
			}

			if result.HasWarnings() != tt.wantWarnings {
				t.Errorf("ValidateDockerConfig() hasWarnings = %v, want %v", result.HasWarnings(), tt.wantWarnings)
			}

			if len(result.Errors) != tt.wantErrCount {
				t.Errorf("ValidateDockerConfig() error count = %d, want %d. Errors: %v", len(result.Errors), tt.wantErrCount, result.Errors)
			}

			if len(result.Warnings) != tt.wantWarnCount {
				t.Errorf("ValidateDockerConfig() warning count = %d, want %d. Warnings: %v", len(result.Warnings), tt.wantWarnCount, result.Warnings)
			}
		})
	}
}
