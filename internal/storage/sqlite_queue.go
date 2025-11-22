package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
)

// QueueUpdate implements Storage.QueueUpdate.
// Adds an update operation to the queue.
func (s *SQLiteStorage) QueueUpdate(ctx context.Context, queue UpdateQueue) error {
	return s.retryWithBackoff(ctx, func() error {
		// Serialize containers to JSON
		containersJSON, err := json.Marshal(queue.Containers)
		if err != nil {
			log.Printf("Failed to serialize containers: %v", err)
			return fmt.Errorf("failed to serialize containers: %w", err)
		}

		query := `
			INSERT INTO update_queue
			(operation_id, stack_name, containers, priority, queued_at, estimated_start_time)
			VALUES (?, ?, ?, ?, ?, ?)
		`

		_, err = s.db.ExecContext(ctx, query,
			queue.OperationID, queue.StackName, string(containersJSON),
			queue.Priority, queue.QueuedAt, queue.EstimatedStartTime)
		if err != nil {
			log.Printf("Failed to queue update for operation %s: %v", queue.OperationID, err)
			return fmt.Errorf("failed to queue update: %w", err)
		}

		log.Printf("Queued update: operation=%s, stack=%s, containers=%d", queue.OperationID, queue.StackName, len(queue.Containers))
		return nil
	})
}

// DequeueUpdate implements Storage.DequeueUpdate.
// Retrieves and removes the oldest queued operation for a stack.
func (s *SQLiteStorage) DequeueUpdate(ctx context.Context, stackName string) (UpdateQueue, bool, error) {
	var queue UpdateQueue
	var found bool

	err := s.retryWithBackoff(ctx, func() error {
		// Start transaction
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			log.Printf("Failed to begin transaction for dequeue: %v", err)
			return fmt.Errorf("failed to begin transaction: %w", err)
		}

		// Find oldest queued operation for this stack
		query := `
			SELECT id, operation_id, stack_name, containers, priority, queued_at, estimated_start_time
			FROM update_queue
			WHERE stack_name = ?
			ORDER BY priority DESC, queued_at ASC
			LIMIT 1
		`

		var containersJSON string
		var estimatedStartTime sql.NullTime

		err = tx.QueryRowContext(ctx, query, stackName).Scan(
			&queue.ID, &queue.OperationID, &queue.StackName, &containersJSON,
			&queue.Priority, &queue.QueuedAt, &estimatedStartTime,
		)

		if err == sql.ErrNoRows {
			tx.Rollback()
			found = false
			return nil
		}
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to query queued update for stack %s: %v", stackName, err)
			return fmt.Errorf("failed to query queued update: %w", err)
		}

		if estimatedStartTime.Valid {
			queue.EstimatedStartTime = &estimatedStartTime.Time
		}

		// Deserialize containers from JSON
		err = json.Unmarshal([]byte(containersJSON), &queue.Containers)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to deserialize containers: %v", err)
			return fmt.Errorf("failed to deserialize containers: %w", err)
		}

		// Delete the entry
		_, err = tx.ExecContext(ctx, "DELETE FROM update_queue WHERE id = ?", queue.ID)
		if err != nil {
			tx.Rollback()
			log.Printf("Failed to delete queued update: %v", err)
			return fmt.Errorf("failed to delete queued update: %w", err)
		}

		// Commit transaction
		if err := tx.Commit(); err != nil {
			log.Printf("Failed to commit dequeue transaction: %v", err)
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		found = true
		log.Printf("Dequeued update: operation=%s, stack=%s", queue.OperationID, queue.StackName)
		return nil
	})

	if err != nil {
		return UpdateQueue{}, false, err
	}

	return queue, found, nil
}

// GetQueuedUpdates implements Storage.GetQueuedUpdates.
// Retrieves all queued operations ordered by queued_at.
func (s *SQLiteStorage) GetQueuedUpdates(ctx context.Context) ([]UpdateQueue, error) {
	query := `
		SELECT id, operation_id, stack_name, containers, priority, queued_at, estimated_start_time
		FROM update_queue
		ORDER BY priority DESC, queued_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		log.Printf("Failed to query queued updates: %v", err)
		return nil, fmt.Errorf("failed to query queued updates: %w", err)
	}
	defer rows.Close()

	var queues []UpdateQueue
	for rows.Next() {
		var queue UpdateQueue
		var containersJSON string
		var estimatedStartTime sql.NullTime

		err := rows.Scan(
			&queue.ID, &queue.OperationID, &queue.StackName, &containersJSON,
			&queue.Priority, &queue.QueuedAt, &estimatedStartTime,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queued update: %w", err)
		}

		if estimatedStartTime.Valid {
			queue.EstimatedStartTime = &estimatedStartTime.Time
		}

		// Deserialize containers from JSON
		err = json.Unmarshal([]byte(containersJSON), &queue.Containers)
		if err != nil {
			log.Printf("Failed to deserialize containers: %v", err)
			return nil, fmt.Errorf("failed to deserialize containers: %w", err)
		}

		queues = append(queues, queue)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating queued updates rows: %w", err)
	}

	return queues, nil
}
