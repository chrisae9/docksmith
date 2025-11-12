package config

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/chis/docksmith/internal/storage"
)

// Scanner scans directories for Docker Compose files.
// It supports recursive scanning with exclusion patterns.
type Scanner struct {
	storage storage.Storage
	config  *Config
}

// NewScanner creates a new compose file scanner.
func NewScanner(store storage.Storage, cfg *Config) *Scanner {
	return &Scanner{
		storage: store,
		config:  cfg,
	}
}

// IsComposeFile checks if a filename matches known compose file names.
// Supports: docker-compose.yml, docker-compose.yaml, compose.yml, compose.yaml
// Matching is case-insensitive.
func IsComposeFile(filename string) bool {
	lower := strings.ToLower(filename)
	return lower == "docker-compose.yml" ||
		lower == "docker-compose.yaml" ||
		lower == "compose.yml" ||
		lower == "compose.yaml"
}

// ShouldExclude checks if a path contains any exclusion pattern.
// Returns true if the path should be excluded from scanning.
func (s *Scanner) ShouldExclude(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(path, pattern) {
			return true
		}
	}
	return false
}

// ScanDirectory recursively scans a directory for compose files.
// Returns a list of absolute paths to discovered compose files.
// Respects exclusion patterns from the scanner's config.
func (s *Scanner) ScanDirectory(ctx context.Context, dirPath string) ([]string, error) {
	var found []string
	var mu sync.Mutex

	// Get exclusion patterns from config, or use defaults
	excludePatterns := []string{"node_modules", ".git", ".svn", "vendor"}
	if s.config != nil && len(s.config.ExcludePatterns) > 0 {
		excludePatterns = s.config.ExcludePatterns
	}

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Handle walk errors
		if err != nil {
			// Log but don't fail on permission errors
			if os.IsPermission(err) {
				log.Printf("Permission denied accessing %s: %v", path, err)
				return filepath.SkipDir
			}
			return err
		}

		// Skip excluded directories
		if info.IsDir() && s.ShouldExclude(path, excludePatterns) {
			return filepath.SkipDir
		}

		// Check if file is a compose file
		if !info.IsDir() && IsComposeFile(info.Name()) {
			mu.Lock()
			found = append(found, path)
			mu.Unlock()
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to scan directory %s: %w", dirPath, err)
	}

	return found, nil
}

// ScanAll scans all configured directories for compose files.
// Default directories are /www and /torrent if not configured.
// Discovered paths are stored in the database as a JSON array.
// Returns all discovered compose file paths.
func (s *Scanner) ScanAll(ctx context.Context) ([]string, error) {
	// Get scan directories from config
	scanDirs := []string{"/www", "/torrent"}
	if s.config != nil && len(s.config.ScanDirectories) > 0 {
		scanDirs = s.config.ScanDirectories
	}

	var allFound []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	errChan := make(chan error, len(scanDirs))

	// Scan each directory concurrently
	for _, dir := range scanDirs {
		wg.Add(1)
		go func(dirPath string) {
			defer wg.Done()

			// Check if directory exists and is readable
			if _, err := os.Stat(dirPath); err != nil {
				if os.IsNotExist(err) {
					log.Printf("Scan directory does not exist: %s", dirPath)
					return
				}
				if os.IsPermission(err) {
					log.Printf("No permission to access scan directory: %s", dirPath)
					return
				}
				errChan <- fmt.Errorf("failed to access directory %s: %w", dirPath, err)
				return
			}

			found, err := s.ScanDirectory(ctx, dirPath)
			if err != nil {
				errChan <- fmt.Errorf("failed to scan %s: %w", dirPath, err)
				return
			}

			mu.Lock()
			allFound = append(allFound, found...)
			mu.Unlock()
		}(dir)
	}

	// Wait for all scans to complete
	wg.Wait()
	close(errChan)

	// Check for errors
	for err := range errChan {
		if err != nil {
			return nil, err
		}
	}

	// Validate discovered files are readable
	validPaths := make([]string, 0, len(allFound))
	for _, path := range allFound {
		result := ValidatePath(path)
		if result.IsValid() {
			validPaths = append(validPaths, path)
		} else {
			log.Printf("Skipping unreadable compose file: %s (%v)", path, result.Warnings)
		}
	}

	// Store results in database
	if s.storage != nil && len(validPaths) > 0 {
		data, err := json.Marshal(validPaths)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal compose paths: %w", err)
		}

		if err := s.storage.SetConfig(ctx, "compose_file_paths", string(data)); err != nil {
			return nil, fmt.Errorf("failed to store compose file paths: %w", err)
		}
	}

	// Log scan results
	log.Printf("Found %d compose files in %d directories", len(validPaths), len(scanDirs))

	return validPaths, nil
}
