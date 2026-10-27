package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"pinglume/agent/internal/ids"
)

type Incident struct {
	ID         string    `json:"id"`
	ReportedAt time.Time `json:"reportedAt"`
	CreatedAt  time.Time `json:"createdAt"`
	Symptom    string    `json:"symptom"`
	Note       string    `json:"note"`
	CreatedVia string    `json:"createdVia"`
}

var ErrNotFound = errors.New("não encontrado")

func (s *Store) InsertIncident(ctx context.Context, deviceID string, inc Incident) (Incident, error) {
	inc.ID = ids.New(inc.CreatedAt)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO incidents (id, device_id, reported_at_ms, created_at_ms, symptom, note, created_via)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		inc.ID, deviceID, inc.ReportedAt.UnixMilli(), inc.CreatedAt.UnixMilli(), inc.Symptom, inc.Note, inc.CreatedVia)
	return inc, err
}

func scanIncident(sc interface{ Scan(...any) error }) (Incident, error) {
	var inc Incident
	var rep, cre int64
	err := sc.Scan(&inc.ID, &rep, &cre, &inc.Symptom, &inc.Note, &inc.CreatedVia)
	inc.ReportedAt, inc.CreatedAt = time.UnixMilli(rep), time.UnixMilli(cre)
	return inc, err
}

func (s *Store) Incident(ctx context.Context, id string) (Incident, error) {
	inc, err := scanIncident(s.db.QueryRowContext(ctx, `
		SELECT id, reported_at_ms, created_at_ms, symptom, note, created_via FROM incidents WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return inc, ErrNotFound
	}
	return inc, err
}

func (s *Store) Incidents(ctx context.Context, from, to time.Time) ([]Incident, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, reported_at_ms, created_at_ms, symptom, note, created_via FROM incidents
		WHERE reported_at_ms >= ? AND reported_at_ms <= ? ORDER BY reported_at_ms DESC`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		inc, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inc)
	}
	return out, rows.Err()
}

func (s *Store) DeleteIncident(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM incidents WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Interpretation(ctx context.Context, incidentID, ruleVersion string) (string, bool, error) {
	var body string
	err := s.db.QueryRowContext(ctx,
		`SELECT body_json FROM interpretations WHERE incident_id = ? AND rule_version = ?`, incidentID, ruleVersion).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return body, err == nil, err
}

func (s *Store) SaveInterpretation(ctx context.Context, incidentID, ruleVersion, verdict, body string, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO interpretations (id, incident_id, rule_version, verdict, body_json, created_at_ms)
		VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (incident_id, rule_version) DO NOTHING`,
		ids.New(now), incidentID, ruleVersion, verdict, body, now.UnixMilli())
	return err
}
