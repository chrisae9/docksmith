package storage

import (
	"context"
	"database/sql"
	"fmt"
	"log"
)

// GetRollbackPolicy implements Storage.GetRollbackPolicy.
// Retrieves the rollback policy for an entity.
func (s *SQLiteStorage) GetRollbackPolicy(ctx context.Context, entityType, entityID string) (RollbackPolicy, bool, error) {
	query := `
		SELECT id, entity_type, entity_id, auto_rollback_enabled, health_check_required, created_at, updated_at
		FROM rollback_policies
		WHERE entity_type = ? AND (entity_id = ? OR (entity_id IS NULL AND ? = ''))
		ORDER BY created_at DESC
		LIMIT 1
	`

	var policy RollbackPolicy
	var entityIDNull sql.NullString

	err := s.db.QueryRowContext(ctx, query, entityType, entityID, entityID).Scan(
		&policy.ID, &policy.EntityType, &entityIDNull,
		&policy.AutoRollbackEnabled, &policy.HealthCheckRequired,
		&policy.CreatedAt, &policy.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		return RollbackPolicy{}, false, nil
	}
	if err != nil {
		log.Printf("Failed to query rollback policy for %s/%s: %v", entityType, entityID, err)
		return RollbackPolicy{}, false, fmt.Errorf("failed to query rollback policy: %w", err)
	}

	if entityIDNull.Valid {
		policy.EntityID = entityIDNull.String
	}

	log.Printf("Retrieved rollback policy: type=%s, id=%s, enabled=%v", policy.EntityType, policy.EntityID, policy.AutoRollbackEnabled)
	return policy, true, nil
}

// SetRollbackPolicy implements Storage.SetRollbackPolicy.
// Creates or updates a rollback policy using INSERT OR REPLACE.
func (s *SQLiteStorage) SetRollbackPolicy(ctx context.Context, policy RollbackPolicy) error {
	return s.retryWithBackoff(ctx, func() error {
		// Handle empty entity ID for global policy
		var entityID interface{}
		if policy.EntityID == "" {
			entityID = nil
		} else {
			entityID = policy.EntityID
		}

		query := `
			INSERT OR REPLACE INTO rollback_policies
			(entity_type, entity_id, auto_rollback_enabled, health_check_required, created_at, updated_at)
			VALUES (?, ?, ?, ?, COALESCE((SELECT created_at FROM rollback_policies WHERE entity_type = ? AND entity_id IS ?), CURRENT_TIMESTAMP), CURRENT_TIMESTAMP)
		`

		_, err := s.db.ExecContext(ctx, query,
			policy.EntityType, entityID, policy.AutoRollbackEnabled, policy.HealthCheckRequired,
			policy.EntityType, entityID)
		if err != nil {
			log.Printf("Failed to set rollback policy for %s/%s: %v", policy.EntityType, policy.EntityID, err)
			return fmt.Errorf("failed to set rollback policy: %w", err)
		}

		log.Printf("Set rollback policy: type=%s, id=%s, enabled=%v", policy.EntityType, policy.EntityID, policy.AutoRollbackEnabled)
		return nil
	})
}
