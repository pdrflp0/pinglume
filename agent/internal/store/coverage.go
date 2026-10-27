package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pinglume/agent/internal/ids"
)

type Interval struct {
	ID         string
	Start, End time.Time
	State      string
	Reason     string
	DetectedBy string
	Open       bool
}

func (iv Interval) Duration() time.Duration { return iv.End.Sub(iv.Start) }

func (s *Store) OpenInterval(ctx context.Context, deviceID string) (*Interval, error) {
	iv := Interval{Open: true}
	var startMS, endMS int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, start_wall_ms, end_wall_ms, state, reason, detected_by
		FROM coverage_intervals WHERE device_id = ? AND is_open = 1`, deviceID).
		Scan(&iv.ID, &startMS, &endMS, &iv.State, &iv.Reason, &iv.DetectedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	iv.Start, iv.End = time.UnixMilli(startMS), time.UnixMilli(endMS)
	return &iv, nil
}

func (s *Store) LastIntervalEnd(ctx context.Context, deviceID string) (time.Time, bool, error) {
	var endMS sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(end_wall_ms) FROM coverage_intervals WHERE device_id = ?`, deviceID).Scan(&endMS)
	if err != nil || !endMS.Valid {
		return time.Time{}, false, err
	}
	return time.UnixMilli(endMS.Int64), true, nil
}

func (s *Store) Transition(ctx context.Context, deviceID string, closeAt time.Time, closed []Interval, next *Interval) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
		UPDATE coverage_intervals SET end_wall_ms = MAX(start_wall_ms, ?), is_open = 0
		WHERE device_id = ? AND is_open = 1`, closeAt.UnixMilli(), deviceID); err != nil {
		return "", err
	}
	for _, iv := range closed {
		if !iv.End.After(iv.Start) {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO coverage_intervals (id, device_id, start_wall_ms, end_wall_ms, state, reason, detected_by, is_open)
			VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
			ids.New(iv.Start), deviceID, iv.Start.UnixMilli(), iv.End.UnixMilli(), iv.State, iv.Reason, iv.DetectedBy); err != nil {
			return "", err
		}
	}
	var openID string
	if next != nil {
		openID = ids.New(next.Start)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO coverage_intervals (id, device_id, start_wall_ms, end_wall_ms, state, reason, detected_by, is_open)
			VALUES (?, ?, ?, ?, ?, ?, ?, 1)`,
			openID, deviceID, next.Start.UnixMilli(), next.Start.UnixMilli(), next.State, next.Reason, next.DetectedBy); err != nil {
			return "", err
		}
	}
	return openID, tx.Commit()
}

func (s *Store) ExtendOpen(ctx context.Context, deviceID string, end time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE coverage_intervals SET end_wall_ms = MAX(start_wall_ms, ?)
		WHERE device_id = ? AND is_open = 1`, end.UnixMilli(), deviceID)
	return err
}

func (s *Store) Intervals(ctx context.Context, since, until time.Time) ([]Interval, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, start_wall_ms, end_wall_ms, state, reason, detected_by, is_open
		FROM coverage_intervals
		WHERE end_wall_ms >= ? AND start_wall_ms <= ?
		ORDER BY start_wall_ms, id`, since.UnixMilli(), until.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Interval
	for rows.Next() {
		var iv Interval
		var startMS, endMS int64
		if err := rows.Scan(&iv.ID, &startMS, &endMS, &iv.State, &iv.Reason, &iv.DetectedBy, &iv.Open); err != nil {
			return nil, err
		}
		iv.Start, iv.End = time.UnixMilli(startMS), time.UnixMilli(endMS)
		if iv.Start.Before(since) {
			iv.Start = since
		}
		if iv.End.After(until) {
			iv.End = until
		}
		out = append(out, iv)
	}
	return out, rows.Err()
}

func (s *Store) GetState(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM agent_state WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetState(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO agent_state (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func ExtendLive(ivs []Interval, liveUntil, to time.Time) []Interval {
	if liveUntil.IsZero() {
		return ivs
	}
	if liveUntil.After(to) {
		liveUntil = to
	}
	for i := range ivs {
		if ivs[i].Open && liveUntil.After(ivs[i].End) {
			ivs[i].End = liveUntil
		}
	}
	return ivs
}
