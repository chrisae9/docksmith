package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidateGitHubToken_ValidTokens tests that ValidateGitHubToken accepts valid token prefixes.
func TestValidateGitHubToken_ValidTokens(t *testing.T) {
	testCases := []struct {
		name  string
		token string
	}{
		{
			name:  "ghp prefix",
			token: "ghp_1234567890abcdefghijklmnopqrstuvwxyz",
		},
		{
			name:  "gho prefix",
			token: "gho_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefgh",
		},
		{
			name:  "ghcr prefix",
			token: "ghcr_0123456789ABCDEFabcdef",
		},
		{
			name:  "github_pat prefix",
			token: "github_pat_1234567890ABCDEF",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateGitHubToken(tc.token)
			if !result.IsValid() {
				t.Errorf("expected token %q to be valid, got errors: %v", tc.token, result.Errors)
			}
			if result.HasWarnings() {
				t.Errorf("expected no warnings for valid token, got: %v", result.Warnings)
			}
		})
	}
}

// TestValidateGitHubToken_InvalidTokens tests that ValidateGitHubToken rejects invalid tokens.
func TestValidateGitHubToken_InvalidTokens(t *testing.T) {
	testCases := []struct {
		name  string
		token string
	}{
		{
			name:  "empty token",
			token: "",
		},
		{
			name:  "invalid prefix",
			token: "invalid_1234567890",
		},
		{
			name:  "no prefix",
			token: "1234567890abcdef",
		},
		{
			name:  "special characters",
			token: "ghp_1234-5678_abcd",
		},
		{
			name:  "prefix only",
			token: "ghp_",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateGitHubToken(tc.token)
			if result.IsValid() {
				t.Errorf("expected token %q to be invalid, but validation passed", tc.token)
			}
			if len(result.Errors) == 0 {
				t.Error("expected validation errors, got none")
			}
		})
	}
}

// TestValidateTTL_ValidValues tests that ValidateTTL accepts values in the range 1-365.
func TestValidateTTL_ValidValues(t *testing.T) {
	testCases := []struct {
		name string
		ttl  string
	}{
		{name: "minimum value", ttl: "1"},
		{name: "default value", ttl: "7"},
		{name: "medium value", ttl: "30"},
		{name: "maximum value", ttl: "365"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateTTL(tc.ttl)
			if !result.IsValid() {
				t.Errorf("expected TTL %q to be valid, got errors: %v", tc.ttl, result.Errors)
			}
			if result.HasWarnings() {
				t.Errorf("expected no warnings for valid TTL, got: %v", result.Warnings)
			}
		})
	}
}

// TestValidateTTL_InvalidValues tests that ValidateTTL rejects values outside the 1-365 range.
func TestValidateTTL_InvalidValues(t *testing.T) {
	testCases := []struct {
		name string
		ttl  string
	}{
		{name: "zero", ttl: "0"},
		{name: "negative", ttl: "-1"},
		{name: "too large", ttl: "366"},
		{name: "way too large", ttl: "1000"},
		{name: "not a number", ttl: "abc"},
		{name: "empty", ttl: ""},
		{name: "float", ttl: "7.5"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidateTTL(tc.ttl)
			if result.IsValid() {
				t.Errorf("expected TTL %q to be invalid, but validation passed", tc.ttl)
			}
			if len(result.Errors) == 0 {
				t.Error("expected validation errors, got none")
			}
		})
	}
}

// TestValidatePath_InaccessiblePaths tests that ValidatePath warns on inaccessible paths.
func TestValidatePath_InaccessiblePaths(t *testing.T) {
	testCases := []struct {
		name string
		path string
	}{
		{
			name: "non-existent path",
			path: "/path/that/does/not/exist/at/all",
		},
		{
			name: "empty path",
			path: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ValidatePath(tc.path)
			// Path validation should produce warnings, not errors (non-blocking)
			if !result.IsValid() {
				t.Errorf("expected path validation to not produce errors (should warn), got errors: %v", result.Errors)
			}
			if !result.HasWarnings() {
				t.Error("expected warnings for inaccessible path, got none")
			}
		})
	}
}

// TestValidatePath_AccessiblePath tests that ValidatePath succeeds for accessible paths.
func TestValidatePath_AccessiblePath(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()

	result := ValidatePath(tmpDir)
	if !result.IsValid() {
		t.Errorf("expected accessible path to be valid, got errors: %v", result.Errors)
	}
	if result.HasWarnings() {
		t.Errorf("expected no warnings for accessible path, got: %v", result.Warnings)
	}
}

// TestValidateComposeFile_InvalidYAML tests that ValidateComposeFile detects invalid YAML.
func TestValidateComposeFile_InvalidYAML(t *testing.T) {
	// Create a temporary file with invalid YAML
	tmpDir := t.TempDir()
	invalidYAMLPath := filepath.Join(tmpDir, "invalid.yml")

	// This YAML is truly invalid: unbalanced brackets and invalid syntax
	invalidYAML := `
version: "3.8"
services:
  web:
    image: nginx
    ports: [[[
      - "80:80"
`
	if err := os.WriteFile(invalidYAMLPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	result := ValidateComposeFile(invalidYAMLPath)
	if result.IsValid() {
		t.Error("expected invalid YAML to produce validation errors")
	}
	if len(result.Errors) == 0 {
		t.Error("expected validation errors for invalid YAML, got none")
	}
}

// TestValidateComposeFile_ValidYAML tests that ValidateComposeFile accepts valid YAML.
func TestValidateComposeFile_ValidYAML(t *testing.T) {
	// Create a temporary file with valid YAML
	tmpDir := t.TempDir()
	validYAMLPath := filepath.Join(tmpDir, "valid.yml")

	validYAML := `
version: "3.8"
services:
  web:
    image: nginx:latest
    ports:
      - "80:80"
    environment:
      - NODE_ENV=production
`
	if err := os.WriteFile(validYAMLPath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	result := ValidateComposeFile(validYAMLPath)
	if !result.IsValid() {
		t.Errorf("expected valid YAML to pass validation, got errors: %v", result.Errors)
	}
	// File not existing would produce warnings, but valid file should have none
	if result.HasWarnings() {
		t.Errorf("expected no warnings for valid YAML file, got: %v", result.Warnings)
	}
}

// TestValidateComposeFile_MissingFile tests that ValidateComposeFile warns on missing files.
func TestValidateComposeFile_MissingFile(t *testing.T) {
	result := ValidateComposeFile("/path/to/nonexistent/compose.yml")

	// Missing files should produce warnings (non-blocking), not errors
	if !result.IsValid() {
		t.Errorf("expected missing file to not produce errors (should warn), got errors: %v", result.Errors)
	}
	if !result.HasWarnings() {
		t.Error("expected warnings for missing compose file, got none")
	}
}

// TestValidationResult_IsValid tests the IsValid method.
func TestValidationResult_IsValid(t *testing.T) {
	t.Run("no errors", func(t *testing.T) {
		result := ValidationResult{}
		if !result.IsValid() {
			t.Error("expected result with no errors to be valid")
		}
	})

	t.Run("with warnings only", func(t *testing.T) {
		result := ValidationResult{
			Warnings: []string{"warning 1", "warning 2"},
		}
		if !result.IsValid() {
			t.Error("expected result with only warnings to be valid")
		}
	})

	t.Run("with errors", func(t *testing.T) {
		result := ValidationResult{
			Errors: []string{"error 1"},
		}
		if result.IsValid() {
			t.Error("expected result with errors to be invalid")
		}
	})

	t.Run("with both errors and warnings", func(t *testing.T) {
		result := ValidationResult{
			Errors:   []string{"error 1"},
			Warnings: []string{"warning 1"},
		}
		if result.IsValid() {
			t.Error("expected result with errors to be invalid")
		}
	})
}

// TestValidationResult_HasWarnings tests the HasWarnings method.
func TestValidationResult_HasWarnings(t *testing.T) {
	t.Run("no warnings", func(t *testing.T) {
		result := ValidationResult{}
		if result.HasWarnings() {
			t.Error("expected result with no warnings to return false")
		}
	})

	t.Run("with warnings", func(t *testing.T) {
		result := ValidationResult{
			Warnings: []string{"warning 1"},
		}
		if !result.HasWarnings() {
			t.Error("expected result with warnings to return true")
		}
	})
}

// TestValidateConfig tests the ValidateConfig function with complete config objects.
func TestValidateConfig(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		tmpDir := t.TempDir()

		cfg := Config{
			ScanDirectories: []string{tmpDir},
			CacheTTLDays:    7,
		}

		result := ValidateConfig(&cfg)
		if !result.IsValid() {
			t.Errorf("expected valid config to pass validation, got errors: %v", result.Errors)
		}
	})

	t.Run("invalid TTL", func(t *testing.T) {
		cfg := Config{
			CacheTTLDays: 500, // Out of range
		}

		result := ValidateConfig(&cfg)
		if result.IsValid() {
			t.Error("expected config with invalid TTL to fail validation")
		}
	})

	t.Run("inaccessible paths produce warnings", func(t *testing.T) {
		cfg := Config{
			ScanDirectories: []string{"/path/does/not/exist"},
			CacheTTLDays:    7,
		}

		result := ValidateConfig(&cfg)
		// Inaccessible paths should produce warnings, not errors
		if !result.IsValid() {
			t.Errorf("expected config with inaccessible paths to be valid (with warnings), got errors: %v", result.Errors)
		}
		if !result.HasWarnings() {
			t.Error("expected warnings for inaccessible paths, got none")
		}
	})
}
