// Package store persiste observações, definições de sonda e eventos em SQLite.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"pinglume/agent/internal/ids"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	db *sql.DB
}

// Open abre (ou cria) o banco e aplica migrações pendentes, cada uma em sua
// própria transação. Uma migração que falha não deixa o schema pela metade.
func Open(ctx context.Context, path string) (*Store, error) {
	dsn := "file:" + path +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Um único escritor evita SQLITE_BUSY dentro do próprio processo.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migração: %w", err)
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at_ms INTEGER NOT NULL)`); err != nil {
		return err
	}
	names, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		version, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("nome de migração inválido %q", base)
		}
		var exists int
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		body, err := migrationFS.ReadFile(name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", base, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at_ms) VALUES (?, ?)`,
			version, time.Now().UnixMilli()); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion devolve a maior migração aplicada.
func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&v)
	return int(v.Int64), err
}

type Device struct {
	ID, Name, OS, Arch, AgentVersion string
}

// EnsureDevice devolve o dispositivo desta instalação, criando-o na primeira vez.
func (s *Store) EnsureDevice(ctx context.Context, d Device, now time.Time) (Device, error) {
	var got Device
	err := s.db.QueryRowContext(ctx,
		`SELECT id, name, os, arch, agent_version FROM devices ORDER BY created_at_ms LIMIT 1`).
		Scan(&got.ID, &got.Name, &got.OS, &got.Arch, &got.AgentVersion)
	if err == nil {
		if got.AgentVersion != d.AgentVersion {
			_, err = s.db.ExecContext(ctx, `UPDATE devices SET agent_version = ? WHERE id = ?`, d.AgentVersion, got.ID)
			got.AgentVersion = d.AgentVersion
		}
		return got, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Device{}, err
	}
	d.ID = ids.New(now)
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO devices (id, name, os, arch, agent_version, created_at_ms) VALUES (?, ?, ?, ?, ?, ?)`,
		d.ID, d.Name, d.OS, d.Arch, d.AgentVersion, now.UnixMilli())
	return d, err
}

type Endpoint struct {
	ID, Label, Host, Region, Owner string
}

type ProbeDefinition struct {
	ID              string
	Kind            string
	EndpointID      string
	EndpointLabel   string
	Target          string
	Interval        time.Duration
	Timeout         time.Duration
	ProtocolVersion int
	ParamsJSON      string
}

// EnsureProbeDefinition reutiliza a definição idêntica existente ou cria uma
// nova versão. Definições nunca são alteradas no lugar.
func (s *Store) EnsureProbeDefinition(ctx context.Context, ep Endpoint, pd ProbeDefinition, now time.Time) (ProbeDefinition, error) {
	if pd.ParamsJSON == "" {
		pd.ParamsJSON = "{}"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return pd, err
	}
	defer tx.Rollback()

	err = tx.QueryRowContext(ctx,
		`SELECT id FROM endpoints WHERE label = ? AND host = ? AND owner = ?`, ep.Label, ep.Host, ep.Owner).Scan(&ep.ID)
	if errors.Is(err, sql.ErrNoRows) {
		ep.ID = ids.New(now)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO endpoints (id, label, host, region, owner) VALUES (?, ?, ?, ?, ?)`,
			ep.ID, ep.Label, ep.Host, ep.Region, ep.Owner)
	}
	if err != nil {
		return pd, err
	}
	pd.EndpointID, pd.EndpointLabel = ep.ID, ep.Label

	err = tx.QueryRowContext(ctx, `
		SELECT id FROM probe_definitions
		WHERE kind = ? AND endpoint_id = ? AND target = ? AND interval_ms = ? AND timeout_ms = ?
		  AND protocol_version = ? AND params_json = ?`,
		pd.Kind, pd.EndpointID, pd.Target, pd.Interval.Milliseconds(), pd.Timeout.Milliseconds(),
		pd.ProtocolVersion, pd.ParamsJSON).Scan(&pd.ID)
	if errors.Is(err, sql.ErrNoRows) {
		pd.ID = ids.New(now)
		_, err = tx.ExecContext(ctx, `
			INSERT INTO probe_definitions
			  (id, kind, endpoint_id, target, interval_ms, timeout_ms, protocol_version, params_json, created_at_ms)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			pd.ID, pd.Kind, pd.EndpointID, pd.Target, pd.Interval.Milliseconds(), pd.Timeout.Milliseconds(),
			pd.ProtocolVersion, pd.ParamsJSON, now.UnixMilli())
	}
	if err != nil {
		return pd, err
	}
	return pd, tx.Commit()
}

// NextSeq devolve a próxima sequência de uma sonda, continuando após reinícios.
func (s *Store) NextSeq(ctx context.Context, probeDefID string) (int64, error) {
	var maxSeq sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(seq) FROM observations WHERE probe_def_id = ?`, probeDefID).Scan(&maxSeq)
	return maxSeq.Int64 + 1, err
}

type Observation struct {
	ID          string
	DeviceID    string
	ProbeDefID  string
	Seq         int64
	StartedWall time.Time
	Duration    time.Duration
	Status      string
	ErrorDetail string
	PhasesJSON  string
	ReceivedAt  time.Time
}

func (s *Store) InsertObservation(ctx context.Context, o Observation) error {
	if o.PhasesJSON == "" {
		o.PhasesJSON = "{}"
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO observations
		  (id, device_id, probe_def_id, seq, started_wall_ms, duration_us, status, error_detail, phases_json, received_at_ms)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.ID, o.DeviceID, o.ProbeDefID, o.Seq, o.StartedWall.UnixMilli(), o.Duration.Microseconds(),
		o.Status, o.ErrorDetail, o.PhasesJSON, o.ReceivedAt.UnixMilli())
	return err
}

func (s *Store) InsertSystemEvent(ctx context.Context, at time.Time, kind, detail string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO system_events (id, at_wall_ms, kind, detail) VALUES (?, ?, ?, ?)`,
		ids.New(at), at.UnixMilli(), kind, detail)
	return err
}

// ProbeSummary descreve uma sonda num período. Durações só entram nos
// percentis quando o status é ok: um timeout não tem latência.
type ProbeSummary struct {
	Kind, Target, EndpointLabel string
	ProtocolVersion             int
	Interval                    time.Duration
	Total                       int
	ByStatus                    map[string]int
	OKDurations                 []time.Duration // ordenadas
	LastStarted                 time.Time
}

func (s *Store) Summaries(ctx context.Context, since time.Time) ([]ProbeSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pd.id, pd.kind, pd.target, e.label, pd.protocol_version, pd.interval_ms, o.status, o.duration_us, o.started_wall_ms
		FROM observations o
		JOIN probe_definitions pd ON pd.id = o.probe_def_id
		JOIN endpoints e ON e.id = pd.endpoint_id
		WHERE o.started_wall_ms >= ?
		ORDER BY e.label, pd.kind, pd.id`, since.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*ProbeSummary{}
	var order []string
	for rows.Next() {
		var id, kind, target, label, status string
		var version int
		var intervalMS, durUS, startedMS int64
		if err := rows.Scan(&id, &kind, &target, &label, &version, &intervalMS, &status, &durUS, &startedMS); err != nil {
			return nil, err
		}
		ps, ok := byID[id]
		if !ok {
			ps = &ProbeSummary{Kind: kind, Target: target, EndpointLabel: label, ProtocolVersion: version,
				Interval: time.Duration(intervalMS) * time.Millisecond, ByStatus: map[string]int{}}
			byID[id] = ps
			order = append(order, id)
		}
		ps.Total++
		ps.ByStatus[status]++
		if status == "ok" {
			ps.OKDurations = append(ps.OKDurations, time.Duration(durUS)*time.Microsecond)
		}
		if t := time.UnixMilli(startedMS); t.After(ps.LastStarted) {
			ps.LastStarted = t
		}
	}
	out := make([]ProbeSummary, 0, len(order))
	for _, id := range order {
		ps := byID[id]
		sort.Slice(ps.OKDurations, func(i, j int) bool { return ps.OKDurations[i] < ps.OKDurations[j] })
		out = append(out, *ps)
	}
	return out, rows.Err()
}

// ObservationRow é uma observação com os metadados da sonda, para séries temporais.
type ObservationRow struct {
	ProbeDefID      string
	Kind            string
	Target          string
	EndpointLabel   string
	ProtocolVersion int
	Interval        time.Duration
	Started         time.Time
	Duration        time.Duration
	Status          string
}

// ObservationsBetween devolve as observações iniciadas em [from, to), em ordem de tempo.
func (s *Store) ObservationsBetween(ctx context.Context, from, to time.Time) ([]ObservationRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pd.id, pd.kind, pd.target, e.label, pd.protocol_version, pd.interval_ms,
		       o.started_wall_ms, o.duration_us, o.status
		FROM observations o
		JOIN probe_definitions pd ON pd.id = o.probe_def_id
		JOIN endpoints e ON e.id = pd.endpoint_id
		WHERE o.started_wall_ms >= ? AND o.started_wall_ms < ?
		ORDER BY o.started_wall_ms`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ObservationRow
	for rows.Next() {
		var r ObservationRow
		var intervalMS, startedMS, durUS int64
		if err := rows.Scan(&r.ProbeDefID, &r.Kind, &r.Target, &r.EndpointLabel, &r.ProtocolVersion,
			&intervalMS, &startedMS, &durUS, &r.Status); err != nil {
			return nil, err
		}
		r.Interval = time.Duration(intervalMS) * time.Millisecond
		r.Started = time.UnixMilli(startedMS)
		r.Duration = time.Duration(durUS) * time.Microsecond
		out = append(out, r)
	}
	return out, rows.Err()
}

type SystemEvent struct {
	At     time.Time
	Kind   string
	Detail string
}

func (s *Store) SystemEvents(ctx context.Context, from, to time.Time) ([]SystemEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT at_wall_ms, kind, detail FROM system_events
		WHERE at_wall_ms >= ? AND at_wall_ms <= ? ORDER BY at_wall_ms`, from.UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SystemEvent
	for rows.Next() {
		var e SystemEvent
		var atMS int64
		if err := rows.Scan(&atMS, &e.Kind, &e.Detail); err != nil {
			return nil, err
		}
		e.At = time.UnixMilli(atMS)
		out = append(out, e)
	}
	return out, rows.Err()
}

// LastObservationAt devolve quando a observação mais recente do dispositivo
// foi gravada: também é evidência de que o agente estava vivo.
func (s *Store) LastObservationAt(ctx context.Context, deviceID string) (time.Time, bool, error) {
	var ms sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(received_at_ms) FROM observations WHERE device_id = ?`, deviceID).Scan(&ms)
	if err != nil || !ms.Valid {
		return time.Time{}, false, err
	}
	return time.UnixMilli(ms.Int64), true, nil
}
