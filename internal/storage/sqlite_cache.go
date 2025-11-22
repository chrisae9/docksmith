package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"
)

// SaveVersionCache implements Storage.SaveVersionCache.
// Saves a SHA-to-version resolution to the cache using INSERT OR REPLACE.
// Sets resolved_at to the current timestamp.
func (s *SQLiteStorage) SaveVersionCache(ctx context.Context, sha256, imageRef, version, arch string) error {
	return s.retryWithBackoff(ctx, func() error {
		query := `
			INSERT OR REPLACE INTO version_cache
			(sha256, image_ref, resolved_version, architecture, resolved_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		`

		_, err := s.db.ExecContext(ctx, query, sha256, imageRef, version, arch)
		if err != nil {
			log.Printf("Failed to save version cache for %s (%s, %s): %v", imageRef, sha256, arch, err)
			return fmt.Errorf("failed to save version cache: %w", err)
		}

		log.Printf("Saved version cache: %s -> %s (%s, %s)", sha256, version, imageRef, arch)
		return nil
	})
}

// GetVersionCache implements Storage.GetVersionCache.
// Retrieves a cached version resolution by composite key (sha256, image_ref, architecture).
// Checks TTL before returning (default 1 hour).
// Returns empty string and false if not found or expired.
func (s *SQLiteStorage) GetVersionCache(ctx context.Context, sha256, imageRef, arch string) (string, bool, error) {
	// Get TTL from environment or use default
	ttl := defaultVersionCacheTTL
	if ttlEnv := os.Getenv("CACHE_TTL"); ttlEnv != "" {
		if parsed, err := time.ParseDuration(ttlEnv); err == nil && parsed > 0 {
			ttl = parsed
		} else {
			log.Printf("Warning: Invalid CACHE_TTL '%s', using default %v", ttlEnv, defaultVersionCacheTTL)
		}
	}

	var version string
	var resolvedAt time.Time

	query := `
		SELECT resolved_version, resolved_at
		FROM version_cache
		WHERE sha256 = ? AND image_ref = ? AND architecture = ?
	`

	err := s.db.QueryRowContext(ctx, query, sha256, imageRef, arch).Scan(&version, &resolvedAt)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		log.Printf("Failed to query version cache for %s (%s, %s): %v", imageRef, sha256, arch, err)
		return "", false, fmt.Errorf("failed to query version cache: %w", err)
	}

	// Check if entry is expired based on TTL
	expirationTime := time.Now().Add(-ttl)
	if resolvedAt.Before(expirationTime) {
		log.Printf("Version cache expired for %s (%s, %s): resolved at %v", imageRef, sha256, arch, resolvedAt)
		return "", false, nil
	}

	log.Printf("Version cache hit: %s -> %s (%s, %s)", sha256, version, imageRef, arch)
	return version, true, nil
}

// CleanExpiredCache removes cache entries older than the specified TTL in days.
// Returns the number of rows deleted.
func (s *SQLiteStorage) CleanExpiredCache(ctx context.Context, ttlDays int) (int, error) {
	var rowsDeleted int

	err := s.retryWithBackoff(ctx, func() error {
		query := `
			DELETE FROM version_cache
			WHERE resolved_at < datetime('now', '-' || ? || ' days')
		`

		result, err := s.db.ExecContext(ctx, query, ttlDays)
		if err != nil {
			log.Printf("Failed to clean expired cache entries: %v", err)
			return fmt.Errorf("failed to clean expired cache: %w", err)
		}

		affected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		rowsDeleted = int(affected)
		if rowsDeleted > 0 {
			log.Printf("Cleaned %d expired cache entries (TTL: %d days)", rowsDeleted, ttlDays)
		}

		return nil
	})

	return rowsDeleted, err
}
