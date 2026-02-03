package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// ValidationResult contains the results of configuration validation.
// Separates errors (blocking issues) from warnings (non-blocking issues).
type ValidationResult struct {
	// Errors contains validation failures that should block operations
	Errors []string

	// Warnings contains validation issues that should be logged but not block operations
	Warnings []string
}

// IsValid returns true if there are no validation errors.
// Warnings do not affect validity.
func (vr *ValidationResult) IsValid() bool {
	return len(vr.Errors) == 0
}

// HasWarnings returns true if there are any validation warnings.
func (vr *ValidationResult) HasWarnings() bool {
	return len(vr.Warnings) > 0
}

// AddError adds an error message to the validation result.
func (vr *ValidationResult) AddError(msg string) {
	vr.Errors = append(vr.Errors, msg)
}

// AddWarning adds a warning message to the validation result.
func (vr *ValidationResult) AddWarning(msg string) {
	vr.Warnings = append(vr.Warnings, msg)
}

// Merge combines multiple validation results into a single result.
func (vr *ValidationResult) Merge(other ValidationResult) {
	vr.Errors = append(vr.Errors, other.Errors...)
	vr.Warnings = append(vr.Warnings, other.Warnings...)
}

// ValidateGitHubToken validates that a token follows GitHub token format.
// Accepts tokens starting with: ghp_, gho_, ghcr_, github_pat_
// Pattern: ^(ghp_|gho_|ghcr_|github_pat_)[A-Za-z0-9]+$
func ValidateGitHubToken(token string) ValidationResult {
	result := ValidationResult{}

	if token == "" {
		result.AddError("GitHub token cannot be empty")
		return result
	}

	// GitHub token format: starts with specific prefix followed by alphanumeric characters
	pattern := `^(ghp_|gho_|ghcr_|github_pat_)[A-Za-z0-9]+$`
	matched, err := regexp.MatchString(pattern, token)

	if err != nil {
		result.AddError(fmt.Sprintf("failed to validate GitHub token format: %v", err))
		return result
	}

	if !matched {
		result.AddError("invalid GitHub token format: token must start with ghp_, gho_, ghcr_, or github_pat_ followed by alphanumeric characters")
	}

	return result
}

// ValidateTTL validates that a TTL value is within the acceptable range.
// Accepts integer values between 1 and 365 (days).
// Suggests default value of 7 days if validation fails.
func ValidateTTL(ttl string) ValidationResult {
	result := ValidationResult{}

	if ttl == "" {
		result.AddError("TTL value cannot be empty (default: 7 days)")
		return result
	}

	// Parse TTL as integer
	ttlValue, err := strconv.Atoi(ttl)
	if err != nil {
		result.AddError("invalid TTL value: must be an integer between 1 and 365 days (default: 7 days)")
		return result
	}

	// Check range
	if ttlValue < 1 || ttlValue > 365 {
		result.AddError(fmt.Sprintf("TTL value %d is out of range: must be between 1 and 365 days (default: 7 days)", ttlValue))
	}

	return result
}

// ValidatePath validates that a path exists and is accessible.
// Returns warnings (not errors) for inaccessible paths.
// This allows configuration to be saved even if paths are temporarily unavailable
// (e.g., network mounts that may not be mounted at validation time).
func ValidatePath(path string) ValidationResult {
	result := ValidationResult{}

	if path == "" {
		result.AddWarning("path is empty")
		return result
	}

	// Check if path exists and is accessible
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			result.AddWarning(fmt.Sprintf("path does not exist: %s", path))
		} else if os.IsPermission(err) {
			result.AddWarning(fmt.Sprintf("path is not readable: %s", path))
		} else {
			result.AddWarning(fmt.Sprintf("cannot access path %s: %v", path, err))
		}
		return result
	}

	// Check if path is a directory (optional additional validation)
	if !info.IsDir() {
		result.AddWarning(fmt.Sprintf("path is not a directory: %s", path))
	}

	return result
}

// ValidateComposeFile validates that a file contains valid YAML syntax.
// Does not validate Docker Compose schema, only YAML parsing.
// Returns errors for invalid YAML, warnings for missing files.
func ValidateComposeFile(filePath string) ValidationResult {
	result := ValidationResult{}

	if filePath == "" {
		result.AddWarning("compose file path is empty")
		return result
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			result.AddWarning(fmt.Sprintf("compose file does not exist: %s", filePath))
		} else {
			result.AddError(fmt.Sprintf("failed to read compose file %s: %v", filePath, err))
		}
		return result
	}

	// Attempt to parse as YAML
	var content interface{}
	if err := yaml.Unmarshal(data, &content); err != nil {
		result.AddError(fmt.Sprintf("invalid YAML syntax in compose file %s: %v", filePath, err))
		return result
	}

	return result
}

// ValidateConfig validates an entire configuration object.
// Aggregates validation results from all configured values.
// Returns a combined ValidationResult with all errors and warnings.
func ValidateConfig(cfg *Config) ValidationResult {
	result := ValidationResult{}

	// Validate TTL if set
	if cfg.CacheTTLDays > 0 {
		ttlResult := ValidateTTL(strconv.Itoa(cfg.CacheTTLDays))
		result.Merge(ttlResult)
	}

	// Validate scan directories
	for _, dir := range cfg.ScanDirectories {
		pathResult := ValidatePath(dir)
		result.Merge(pathResult)
	}

	// Validate compose file paths
	for _, path := range cfg.ComposeFilePaths {
		fileResult := ValidateComposeFile(path)
		result.Merge(fileResult)
	}

	return result
}
