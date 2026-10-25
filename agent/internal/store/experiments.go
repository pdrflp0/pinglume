package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pinglume/agent/internal/ids"
)

type Condition struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Source string `json:"source"` // declared | detected
	Method string `json:"method,omitempty"`
}

type Window struct {
	ID         string      `json:"id"`
	Role       string      `json:"role"`
	Start      time.Time   `json:"start"`
	PlannedEnd time.Time   `json:"plannedEnd"`
	End        *time.Time  `json:"end"` // nil enquanto não foi encerrada por cancelamento
	Conditions []Condition `json:"conditions"`
}

type Experiment struct {
	ID            string     `json:"id"`
	Question      string     `json:"question"`
	Intervention  string     `json:"intervention"`
	PlannedWindow int64      `json:"plannedWindowMs"`
	CancelledAt   *time.Time `json:"cancelledAt"`
	CreatedAt     time.Time  `json:"createdAt"`
	Windows       []Window   `json:"windows"`
}

func (s *Store) InsertExperiment(ctx context.Context, deviceID string, e Experiment) (Experiment, error) {
	e.ID = ids.New(e.CreatedAt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO experiments (id, device_id, question, intervention, planned_window_ms, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?)`, e.ID, deviceID, e.Question, e.Intervention, e.PlannedWindow, e.CreatedAt.UnixMilli())
	return e, err
}

// AddWindow abre uma janela com suas condições, numa transação.
func (s *Store) AddWindow(ctx context.Context, experimentID string, w Window) (Window, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return w, err
	}
	defer tx.Rollback()
	w.ID = ids.New(w.Start)
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO experiment_windows (id, experiment_id, role, start_ms, planned_end_ms) VALUES (?, ?, ?, ?, ?)`,
		w.ID, experimentID, w.Role, w.Start.UnixMilli(), w.PlannedEnd.UnixMilli()); err != nil {
		return w, err
	}
	for _, c := range w.Conditions {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO conditions (id, window_id, key, value, source, method) VALUES (?, ?, ?, ?, ?, ?)`,
			ids.New(w.Start), w.ID, c.Key, c.Value, c.Source, c.Method); err != nil {
			return w, err
		}
	}
	return w, tx.Commit()
}

// CancelExperiment marca o cancelamento e encerra a janela em andamento.
func (s *Store) CancelExperiment(ctx context.Context, id string, at time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE experiments SET cancelled_at_ms = ? WHERE id = ? AND cancelled_at_ms IS NULL`, at.UnixMilli(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE experiment_windows SET end_ms = ? WHERE experiment_id = ? AND end_ms IS NULL AND planned_end_ms > ?`,
		at.UnixMilli(), id, at.UnixMilli()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Experiment(ctx context.Context, id string) (Experiment, error) {
	var e Experiment
	var created int64
	var cancelled sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		SELECT id, question, intervention, planned_window_ms, cancelled_at_ms, created_at_ms FROM experiments WHERE id = ?`, id).
		Scan(&e.ID, &e.Question, &e.Intervention, &e.PlannedWindow, &cancelled, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	if err != nil {
		return e, err
	}
	e.CreatedAt = time.UnixMilli(created)
	if cancelled.Valid {
		t := time.UnixMilli(cancelled.Int64)
		e.CancelledAt = &t
	}
	e.Windows, err = s.windows(ctx, id)
	return e, err
}

func (s *Store) windows(ctx context.Context, experimentID string) ([]Window, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, role, start_ms, planned_end_ms, end_ms FROM experiment_windows
		WHERE experiment_id = ? ORDER BY start_ms`, experimentID)
	if err != nil {
		return nil, err
	}
	var out []Window
	for rows.Next() {
		var w Window
		var start, planned int64
		var end sql.NullInt64
		if err := rows.Scan(&w.ID, &w.Role, &start, &planned, &end); err != nil {
			rows.Close()
			return nil, err
		}
		w.Start, w.PlannedEnd = time.UnixMilli(start), time.UnixMilli(planned)
		if end.Valid {
			t := time.UnixMilli(end.Int64)
			w.End = &t
		}
		out = append(out, w)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		crows, err := s.db.QueryContext(ctx, `SELECT key, value, source, method FROM conditions WHERE window_id = ? ORDER BY key`, out[i].ID)
		if err != nil {
			return nil, err
		}
		out[i].Conditions = []Condition{}
		for crows.Next() {
			var c Condition
			if err := crows.Scan(&c.Key, &c.Value, &c.Source, &c.Method); err != nil {
				crows.Close()
				return nil, err
			}
			out[i].Conditions = append(out[i].Conditions, c)
		}
		crows.Close()
	}
	return out, nil
}

// Experiments lista do mais recente para o mais antigo.
func (s *Store) Experiments(ctx context.Context, limit int) ([]Experiment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM experiments ORDER BY created_at_ms DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	var idsList []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		idsList = append(idsList, id)
	}
	rows.Close()
	out := []Experiment{}
	for _, id := range idsList {
		e, err := s.Experiment(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func (s *Store) ComparisonResult(ctx context.Context, experimentID, ruleVersion string) (string, bool, error) {
	var body string
	err := s.db.QueryRowContext(ctx,
		`SELECT body_json FROM comparison_results WHERE experiment_id = ? AND rule_version = ?`, experimentID, ruleVersion).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return body, err == nil, err
}

func (s *Store) SaveComparisonResult(ctx context.Context, experimentID, ruleVersion, verdict, body string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO comparison_results (experiment_id, rule_version, verdict, body_json, created_at_ms)
		VALUES (?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`, experimentID, ruleVersion, verdict, body, now.UnixMilli())
	return err
}
