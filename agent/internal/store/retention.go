package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

type MinuteRow struct {
	ProbeDefID      string
	Kind            string
	Target          string
	EndpointLabel   string
	ProtocolVersion int
	Interval        time.Duration
	Minute          time.Time
	N, OK           int
	Failures        map[string]int
	P50US, P95US    *int64
	MinUS, MaxUS    *int64
}

type CompactResult struct {
	Rows, Minutes int
	From, To      time.Time
}

func IsDiskFull(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "database or disk is full") || strings.Contains(s, "sqlite_full") || strings.Contains(s, "no space left")
}

func (s *Store) OldestObservation(ctx context.Context) (time.Time, bool, error) {
	var ms sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(started_wall_ms) FROM observations`).Scan(&ms)
	if err != nil || !ms.Valid {
		return time.Time{}, false, err
	}
	return time.UnixMilli(ms.Int64), true, nil
}

func (s *Store) OldestMinute(ctx context.Context) (time.Time, bool, error) {
	var ms sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(minute_ms) FROM observation_minutes`).Scan(&ms)
	if err != nil || !ms.Valid {
		return time.Time{}, false, err
	}
	return time.UnixMilli(ms.Int64), true, nil
}

func pctl(sorted []int64, p float64) *int64 {
	if len(sorted) == 0 {
		return nil
	}
	v := sorted[int(float64(len(sorted)-1)*p+0.5)]
	return &v
}

func (s *Store) CompactBefore(ctx context.Context, cutoff time.Time, limit int) (CompactResult, error) {
	var res CompactResult
	cutoff = cutoff.Truncate(time.Minute)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	var lastMS sql.NullInt64
	if err := tx.QueryRowContext(ctx, `
		SELECT MAX(started_wall_ms) FROM (
			SELECT started_wall_ms FROM observations WHERE started_wall_ms < ? ORDER BY started_wall_ms LIMIT ?)`,
		cutoff.UnixMilli(), limit).Scan(&lastMS); err != nil {
		return res, err
	}
	if !lastMS.Valid {
		return res, nil
	}
	upTo := cutoff
	if b := time.UnixMilli(lastMS.Int64).Truncate(time.Minute).Add(time.Minute); b.Before(upTo) {
		upTo = b
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT probe_def_id, device_id, started_wall_ms, duration_us, status FROM observations
		WHERE started_wall_ms < ? ORDER BY probe_def_id, started_wall_ms`, upTo.UnixMilli())
	if err != nil {
		return res, err
	}
	type key struct {
		probe  string
		minute int64
	}
	type acc struct {
		device string
		n, ok  int
		fails  map[string]int
		durs   []int64
	}
	aggs := map[key]*acc{}
	var keys []key
	for rows.Next() {
		var probe, device, status string
		var started, dur int64
		if err := rows.Scan(&probe, &device, &started, &dur, &status); err != nil {
			rows.Close()
			return res, err
		}
		if res.Rows == 0 || time.UnixMilli(started).Before(res.From) {
			res.From = time.UnixMilli(started)
		}
		res.Rows++
		k := key{probe, time.UnixMilli(started).Truncate(time.Minute).UnixMilli()}
		a := aggs[k]
		if a == nil {
			a = &acc{device: device, fails: map[string]int{}}
			aggs[k] = a
			keys = append(keys, k)
		}
		if status == "cancelled" {
			continue
		}
		a.n++
		if status == "ok" {
			a.ok++
			a.durs = append(a.durs, dur)
		} else {
			a.fails[status]++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return res, err
	}
	for _, k := range keys {
		a := aggs[k]
		sort.Slice(a.durs, func(i, j int) bool { return a.durs[i] < a.durs[j] })
		fj, _ := json.Marshal(a.fails)
		var mn, mx *int64
		if len(a.durs) > 0 {
			mn, mx = &a.durs[0], &a.durs[len(a.durs)-1]
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO observation_minutes (probe_def_id, device_id, minute_ms, n, ok, failures_json, p50_us, p95_us, min_us, max_us)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (probe_def_id, minute_ms) DO UPDATE SET
				n = n + excluded.n, ok = ok + excluded.ok,
				p50_us = COALESCE(excluded.p50_us, p50_us), p95_us = MAX(COALESCE(p95_us, 0), COALESCE(excluded.p95_us, 0)),
				min_us = MIN(COALESCE(min_us, excluded.min_us), COALESCE(excluded.min_us, min_us)),
				max_us = MAX(COALESCE(max_us, 0), COALESCE(excluded.max_us, 0))`,
			k.probe, a.device, k.minute, a.n, a.ok, string(fj), pctl(a.durs, 0.5), pctl(a.durs, 0.95), mn, mx); err != nil {
			return res, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM observations WHERE started_wall_ms < ?`, upTo.UnixMilli()); err != nil {
		return res, err
	}
	res.Minutes, res.To = len(keys), upTo
	return res, tx.Commit()
}

func (s *Store) DropHistoryBefore(ctx context.Context, cutoff time.Time) (int64, time.Time, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, time.Time{}, err
	}
	defer tx.Rollback()
	var from sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MIN(minute_ms) FROM observation_minutes WHERE minute_ms < ?`, cutoff.UnixMilli()).Scan(&from); err != nil {
		return 0, time.Time{}, err
	}
	r, err := tx.ExecContext(ctx, `DELETE FROM observation_minutes WHERE minute_ms < ?`, cutoff.UnixMilli())
	if err != nil {
		return 0, time.Time{}, err
	}
	n, _ := r.RowsAffected()
	if _, err := tx.ExecContext(ctx, `DELETE FROM coverage_intervals WHERE is_open = 0 AND end_wall_ms < ?`, cutoff.UnixMilli()); err != nil {
		return 0, time.Time{}, err
	}
	var f time.Time
	if from.Valid {
		f = time.UnixMilli(from.Int64)
	}
	return n, f, tx.Commit()
}

type Usage struct {
	UsedBytes int64 `json:"usedBytes"`
	FileBytes int64 `json:"fileBytes"`
}

func (s *Store) Usage(ctx context.Context) (Usage, error) {
	var pages, free, size int64
	for _, q := range []struct {
		sql string
		dst *int64
	}{{"PRAGMA page_count", &pages}, {"PRAGMA freelist_count", &free}, {"PRAGMA page_size", &size}} {
		if err := s.db.QueryRowContext(ctx, q.sql).Scan(q.dst); err != nil {
			return Usage{}, err
		}
	}
	return Usage{UsedBytes: (pages - free) * size, FileBytes: pages * size}, nil
}

func (s *Store) Reclaim(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `VACUUM`)
	return err
}

func (s *Store) SetMaxPages(ctx context.Context, n int64) error {
	_, err := s.db.ExecContext(ctx, `PRAGMA max_page_count = `+strconv.FormatInt(n, 10))
	return err
}

func (s *Store) MinutesBetween(ctx context.Context, from, to time.Time) ([]MinuteRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pd.id, pd.kind, pd.target, e.label, pd.protocol_version, pd.interval_ms,
		       m.minute_ms, m.n, m.ok, m.failures_json, m.p50_us, m.p95_us, m.min_us, m.max_us
		FROM observation_minutes m
		JOIN probe_definitions pd ON pd.id = m.probe_def_id
		JOIN endpoints e ON e.id = pd.endpoint_id
		WHERE m.minute_ms >= ? AND m.minute_ms < ?
		ORDER BY m.minute_ms`, from.Truncate(time.Minute).UnixMilli(), to.UnixMilli())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MinuteRow
	for rows.Next() {
		var r MinuteRow
		var intervalMS, minute int64
		var fj string
		var p50, p95, mn, mx sql.NullInt64
		if err := rows.Scan(&r.ProbeDefID, &r.Kind, &r.Target, &r.EndpointLabel, &r.ProtocolVersion, &intervalMS,
			&minute, &r.N, &r.OK, &fj, &p50, &p95, &mn, &mx); err != nil {
			return nil, err
		}
		r.Interval, r.Minute = time.Duration(intervalMS)*time.Millisecond, time.UnixMilli(minute)
		json.Unmarshal([]byte(fj), &r.Failures)
		r.P50US, r.P95US, r.MinUS, r.MaxUS = nullable(p50), nullable(p95), nullable(mn), nullable(mx)
		out = append(out, r)
	}
	return out, rows.Err()
}

func nullable(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	x := v.Int64
	return &x
}
