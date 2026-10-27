package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type ExportMeta struct {
	ID            string    `json:"id"`
	CreatedAt     time.Time `json:"createdAt"`
	From          time.Time `json:"from"`
	To            time.Time `json:"to"`
	OptionsJSON   string    `json:"options"`
	SchemaVersion string    `json:"schemaVersion"`
	AgentVersion  string    `json:"agentVersion"`
	SHA256        string    `json:"sha256"`
}

func (s *Store) InsertExport(ctx context.Context, m ExportMeta, bodyJSON, bodyHTML string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO export_snapshots (id, created_at_ms, from_ms, to_ms, options_json, schema_version, agent_version, sha256, body_json, body_html)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.CreatedAt.UnixMilli(), m.From.UnixMilli(), m.To.UnixMilli(), m.OptionsJSON, m.SchemaVersion,
		m.AgentVersion, m.SHA256, bodyJSON, bodyHTML)
	return err
}

const exportCols = `id, created_at_ms, from_ms, to_ms, options_json, schema_version, agent_version, sha256`

func scanExport(sc interface{ Scan(...any) error }, extra ...any) (ExportMeta, error) {
	var m ExportMeta
	var c, f, t int64
	err := sc.Scan(append([]any{&m.ID, &c, &f, &t, &m.OptionsJSON, &m.SchemaVersion, &m.AgentVersion, &m.SHA256}, extra...)...)
	m.CreatedAt, m.From, m.To = time.UnixMilli(c), time.UnixMilli(f), time.UnixMilli(t)
	return m, err
}

func (s *Store) Export(ctx context.Context, id, format string) (ExportMeta, string, error) {
	col := "body_json"
	if format == "html" {
		col = "body_html"
	}
	var body string
	m, err := scanExport(s.db.QueryRowContext(ctx, `SELECT `+exportCols+`, `+col+` FROM export_snapshots WHERE id = ?`, id), &body)
	if errors.Is(err, sql.ErrNoRows) {
		return m, "", ErrNotFound
	}
	return m, body, err
}

func (s *Store) Exports(ctx context.Context, limit int) ([]ExportMeta, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+exportCols+` FROM export_snapshots ORDER BY created_at_ms DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportMeta{}
	for rows.Next() {
		m, err := scanExport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Device(ctx context.Context) (Device, error) {
	var d Device
	err := s.db.QueryRowContext(ctx, `SELECT id, name, os, arch, agent_version FROM devices ORDER BY created_at_ms LIMIT 1`).
		Scan(&d.ID, &d.Name, &d.OS, &d.Arch, &d.AgentVersion)
	return d, err
}
