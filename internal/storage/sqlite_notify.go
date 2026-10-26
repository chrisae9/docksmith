package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const notifyServiceColumns = `id, name, type, enabled, delivery, events, config,
	last_success_at, last_error, last_error_at, last_summary_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNotifyService(sc rowScanner) (NotifyServiceRow, error) {
	var r NotifyServiceRow
	var enabled int
	var events, config string
	var succ, errAt, sumAt sql.NullTime
	if err := sc.Scan(&r.ID, &r.Name, &r.Type, &enabled, &r.Delivery, &events, &config,
		&succ, &r.LastError, &errAt, &sumAt); err != nil {
		return r, err
	}
	r.Enabled = enabled != 0
	if err := json.Unmarshal([]byte(events), &r.Events); err != nil {
		return r, fmt.Errorf("invalid events JSON: %w", err)
	}
	if err := json.Unmarshal([]byte(config), &r.Config); err != nil {
		return r, fmt.Errorf("invalid config JSON: %w", err)
	}
	if succ.Valid {
		t := succ.Time
		r.LastSuccessAt = &t
	}
	if errAt.Valid {
		t := errAt.Time
		r.LastErrorAt = &t
	}
	if sumAt.Valid {
		t := sumAt.Time
		r.LastSummaryAt = &t
	}
	return r, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ListNotifyServices implements NotifyStore.
func (s *SQLiteStorage) ListNotifyServices(ctx context.Context) ([]NotifyServiceRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+notifyServiceColumns+` FROM notify_services ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("failed to list notify services: %w", err)
	}
	defer rows.Close()
	var out []NotifyServiceRow
	for rows.Next() {
		r, err := scanNotifyService(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan notify service: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetNotifyService implements NotifyStore.
func (s *SQLiteStorage) GetNotifyService(ctx context.Context, id int64) (NotifyServiceRow, bool, error) {
	r, err := scanNotifyService(s.db.QueryRowContext(ctx,
		`SELECT `+notifyServiceColumns+` FROM notify_services WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return NotifyServiceRow{}, false, nil
	}
	if err != nil {
		return NotifyServiceRow{}, false, fmt.Errorf("failed to get notify service: %w", err)
	}
	return r, true, nil
}

// SaveNotifyService implements NotifyStore. Inserts when row.ID == 0, else updates.
func (s *SQLiteStorage) SaveNotifyService(ctx context.Context, row NotifyServiceRow) (int64, error) {
	events := row.Events
	if events == nil {
		events = []string{}
	}
	config := row.Config
	if config == nil {
		config = map[string]string{}
	}
	eventsJSON, err := json.Marshal(events)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal events: %w", err)
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal config: %w", err)
	}

	id := row.ID
	err = s.retryWithBackoff(ctx, func() error {
		if row.ID == 0 {
			res, err := s.db.ExecContext(ctx, `
				INSERT INTO notify_services (name, type, enabled, delivery, events, config)
				VALUES (?, ?, ?, ?, ?, ?)`,
				row.Name, row.Type, boolInt(row.Enabled), row.Delivery, string(eventsJSON), string(configJSON))
			if err != nil {
				return err
			}
			id, err = res.LastInsertId()
			return err
		}
		res, err := s.db.ExecContext(ctx, `
			UPDATE notify_services
			SET name = ?, type = ?, enabled = ?, delivery = ?, events = ?, config = ?,
				updated_at = CURRENT_TIMESTAMP
			WHERE id = ?`,
			row.Name, row.Type, boolInt(row.Enabled), row.Delivery, string(eventsJSON), string(configJSON), row.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("notify service %d not found", row.ID)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("failed to save notify service: %w", err)
	}
	return id, nil
}

// DeleteNotifyService implements NotifyStore. Pending rows cascade.
func (s *SQLiteStorage) DeleteNotifyService(ctx context.Context, id int64) error {
	err := s.retryWithBackoff(ctx, func() error {
		_, err := s.db.ExecContext(ctx, `DELETE FROM notify_services WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to delete notify service: %w", err)
	}
	return nil
}

// RecordNotifySendResult implements NotifyStore. A nil sendErr records success
// and clears the last error; otherwise the error and its time are recorded.
func (s *SQLiteStorage) RecordNotifySendResult(ctx context.Context, id int64, sendErr error, at time.Time) error {
	err := s.retryWithBackoff(ctx, func() error {
		var err error
		if sendErr == nil {
			_, err = s.db.ExecContext(ctx, `
				UPDATE notify_services SET last_success_at = ?, last_error = '', last_error_at = NULL
				WHERE id = ?`, at.UTC(), id)
		} else {
			_, err = s.db.ExecContext(ctx, `
				UPDATE notify_services SET last_error = ?, last_error_at = ?
				WHERE id = ?`, sendErr.Error(), at.UTC(), id)
		}
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to record notify send result: %w", err)
	}
	return nil
}

// SetNotifySummarySent implements NotifyStore.
func (s *SQLiteStorage) SetNotifySummarySent(ctx context.Context, id int64, at time.Time) error {
	err := s.retryWithBackoff(ctx, func() error {
		_, err := s.db.ExecContext(ctx,
			`UPDATE notify_services SET last_summary_at = ? WHERE id = ?`, at.UTC(), id)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to set notify summary sent: %w", err)
	}
	return nil
}

// LoadNotifyContainerStates implements NotifyStore.
func (s *SQLiteStorage) LoadNotifyContainerStates(ctx context.Context) (map[string]NotifyContainerState, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT container_name, last_notified_version, last_notified_digest,
			blocked, check_fail_count, check_fail_notified, compose_mismatch
		FROM notify_container_state`)
	if err != nil {
		return nil, fmt.Errorf("failed to load notify container states: %w", err)
	}
	defer rows.Close()
	out := make(map[string]NotifyContainerState)
	for rows.Next() {
		var st NotifyContainerState
		var blocked, failNotified, mismatch int
		if err := rows.Scan(&st.ContainerName, &st.LastNotifiedVersion, &st.LastNotifiedDigest,
			&blocked, &st.CheckFailCount, &failNotified, &mismatch); err != nil {
			return nil, fmt.Errorf("failed to scan notify container state: %w", err)
		}
		st.Blocked, st.CheckFailNotified, st.ComposeMismatch = blocked != 0, failNotified != 0, mismatch != 0
		out[st.ContainerName] = st
	}
	return out, rows.Err()
}

// ReplaceNotifyContainerStates implements NotifyStore. Runs in one transaction;
// rows not present in states are deleted. Map keys are the container names.
func (s *SQLiteStorage) ReplaceNotifyContainerStates(ctx context.Context, states map[string]NotifyContainerState) error {
	err := s.retryWithBackoff(ctx, func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()

		if _, err := tx.ExecContext(ctx, `DELETE FROM notify_container_state`); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `
			INSERT INTO notify_container_state
			(container_name, last_notified_version, last_notified_digest, blocked,
			 check_fail_count, check_fail_notified, compose_mismatch)
			VALUES (?, ?, ?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for name, st := range states {
			if _, err := stmt.ExecContext(ctx, name, st.LastNotifiedVersion, st.LastNotifiedDigest,
				boolInt(st.Blocked), st.CheckFailCount, boolInt(st.CheckFailNotified),
				boolInt(st.ComposeMismatch)); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
	if err != nil {
		return fmt.Errorf("failed to replace notify container states: %w", err)
	}
	return nil
}

// UpsertNotifyPending implements NotifyStore.
func (s *SQLiteStorage) UpsertNotifyPending(ctx context.Context, row NotifyPendingRow) error {
	created := row.CreatedAt
	if created.IsZero() {
		created = time.Now()
	}
	err := s.retryWithBackoff(ctx, func() error {
		_, err := s.db.ExecContext(ctx, `
			INSERT INTO notify_pending (service_id, dedup_key, kind, container_name, payload, created_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(service_id, dedup_key) DO UPDATE SET
				payload = excluded.payload,
				kind = excluded.kind,
				created_at = excluded.created_at`,
			row.ServiceID, row.DedupKey, row.Kind, row.ContainerName, row.Payload, created.UTC())
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to upsert notify pending: %w", err)
	}
	return nil
}

// ListNotifyPending implements NotifyStore.
func (s *SQLiteStorage) ListNotifyPending(ctx context.Context, serviceID int64) ([]NotifyPendingRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, service_id, dedup_key, kind, container_name, payload, created_at
		FROM notify_pending WHERE service_id = ? ORDER BY created_at, id`, serviceID)
	if err != nil {
		return nil, fmt.Errorf("failed to list notify pending: %w", err)
	}
	defer rows.Close()
	var out []NotifyPendingRow
	for rows.Next() {
		var r NotifyPendingRow
		if err := rows.Scan(&r.ID, &r.ServiceID, &r.DedupKey, &r.Kind, &r.ContainerName,
			&r.Payload, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan notify pending: %w", err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteNotifyPending implements NotifyStore.
func (s *SQLiteStorage) DeleteNotifyPending(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	err := s.retryWithBackoff(ctx, func() error {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM notify_pending WHERE id IN (`+placeholders+`)`, args...)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to delete notify pending: %w", err)
	}
	return nil
}

// DeleteNotifyPendingForContainer implements NotifyStore.
func (s *SQLiteStorage) DeleteNotifyPendingForContainer(ctx context.Context, containerName string) error {
	err := s.retryWithBackoff(ctx, func() error {
		_, err := s.db.ExecContext(ctx,
			`DELETE FROM notify_pending WHERE container_name = ?`, containerName)
		return err
	})
	if err != nil {
		return fmt.Errorf("failed to delete notify pending for container: %w", err)
	}
	return nil
}
