package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/compare"
	"pinglume/agent/internal/incident"
	"pinglume/agent/internal/store"
)

type Policy struct {
	Raw        time.Duration
	Aggregates time.Duration
	LimitBytes int64
}

func DefaultPolicy() Policy {
	return Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour, LimitBytes: 250 << 20}
}

type Maintainer struct {
	Store  *store.Store
	Clock  clock.Clock
	Policy Policy
	Log    *slog.Logger
	Batch  int
}

type Report struct {
	Frozen       int         `json:"frozen"`
	Compacted    []Compacted `json:"compacted"`
	DroppedRows  int64       `json:"droppedRows"`
	UsageBefore  store.Usage `json:"usageBefore"`
	UsageAfter   store.Usage `json:"usageAfter"`
	OverLimit    bool        `json:"overLimit"`
	LimitReached bool        `json:"limitReached"`
}

type Compacted struct {
	Reason  string    `json:"reason"`
	Rows    int       `json:"rows"`
	Minutes int       `json:"minutes"`
	From    time.Time `json:"from"`
	To      time.Time `json:"to"`
}

func (m *Maintainer) event(ctx context.Context, kind string, detail any) {
	b, _ := json.Marshal(detail)
	if err := m.Store.InsertSystemEvent(ctx, m.Clock.Now(), kind, string(b)); err != nil && m.Log != nil {
		m.Log.Error("registrar evento de retenção", "tipo", kind, "err", err)
	}
}

func (m *Maintainer) Run(ctx context.Context) (Report, error) {
	var rep Report
	now := m.Clock.Now()
	batch := m.Batch
	if batch <= 0 {
		batch = 200_000
	}
	usage, err := m.Store.Usage(ctx)
	if err != nil {
		return rep, err
	}
	rep.UsageBefore = usage

	compact := func(cutoff time.Time, reason string) error {
		n, err := m.freeze(ctx, cutoff, now)
		rep.Frozen += n
		if err != nil {
			return err
		}
		for {
			r, err := m.Store.CompactBefore(ctx, cutoff, batch)
			if err != nil {
				return err
			}
			if r.Rows == 0 {
				return nil
			}
			c := Compacted{Reason: reason, Rows: r.Rows, Minutes: r.Minutes, From: r.From, To: r.To}
			rep.Compacted = append(rep.Compacted, c)
			m.event(ctx, "retention_compaction", c)
		}
	}
	drop := func(cutoff time.Time, reason string) error {
		n, from, err := m.Store.DropHistoryBefore(ctx, cutoff)
		if err != nil || n == 0 {
			return err
		}
		rep.DroppedRows += n
		m.event(ctx, "history_dropped", map[string]any{"reason": reason, "minutes": n, "from": from, "to": cutoff})
		return nil
	}

	if err := compact(now.Add(-m.Policy.Raw), "retenção de dados brutos"); err != nil {
		return rep, err
	}
	if err := drop(now.Add(-m.Policy.Aggregates), "retenção de agregados"); err != nil {
		return rep, err
	}
	if u, err := m.Store.Usage(ctx); err != nil {
		return rep, err
	} else if u.FileBytes > 0 && float64(u.FileBytes-u.UsedBytes) > 0.25*float64(u.FileBytes) {
		if err := m.Store.Reclaim(ctx); err != nil {
			return rep, err
		}
	}

	for m.Policy.LimitBytes > 0 {
		usage, err = m.Store.Usage(ctx)
		if err != nil {
			return rep, err
		}
		if usage.UsedBytes <= m.Policy.LimitBytes {
			break
		}
		rep.OverLimit = true
		if oldest, ok, err := m.Store.OldestObservation(ctx); err != nil {
			return rep, err
		} else if ok && now.Sub(oldest) > 2*time.Minute {
			cutoff := oldest.Add(now.Sub(oldest) / 2)
			if err := compact(cutoff, "limite de disco"); err != nil {
				return rep, err
			}
		} else if oldest, ok, err := m.Store.OldestMinute(ctx); err != nil {
			return rep, err
		} else if ok && now.Sub(oldest) > 2*time.Minute {
			if err := drop(oldest.Add(now.Sub(oldest)/2), "limite de disco"); err != nil {
				return rep, err
			}
		} else {
			rep.LimitReached = true
			m.event(ctx, "disk_limit", map[string]any{"usedBytes": usage.UsedBytes, "limitBytes": m.Policy.LimitBytes,
				"detail": "nada mais a compactar ou descartar"})
			break
		}
		if err := m.Store.Reclaim(ctx); err != nil {
			return rep, err
		}
	}
	rep.UsageAfter, err = m.Store.Usage(ctx)
	return rep, err
}

func (m *Maintainer) freeze(ctx context.Context, cutoff, now time.Time) (int, error) {
	frozen := 0
	incs, err := m.Store.Incidents(ctx, time.Unix(0, 0), cutoff.Add(incident.Before+incident.BaselineSpan))
	if err != nil {
		return 0, err
	}
	for _, inc := range incs {
		if _, ok, err := m.Store.Interpretation(ctx, inc.ID, incident.RuleVersion); err != nil {
			return frozen, err
		} else if ok {
			continue
		}
		a, err := incident.Load(ctx, m.Store, inc, now, now)
		if err != nil {
			return frozen, fmt.Errorf("congelar relato %s: %w", inc.ID, err)
		}
		if a.Final {
			frozen++
		}
	}
	exps, err := m.Store.Experiments(ctx, 1000)
	if err != nil {
		return frozen, err
	}
	for _, e := range exps {
		if compare.Phase(e, now) != compare.PhaseDone || len(e.Windows) == 0 || !e.Windows[0].Start.Before(cutoff) {
			continue
		}
		if _, ok, err := m.Store.ComparisonResult(ctx, e.ID, compare.RuleVersion); err != nil {
			return frozen, err
		} else if ok {
			continue
		}
		if _, err := compare.Load(ctx, m.Store, e, now); err != nil {
			return frozen, fmt.Errorf("congelar comparação %s: %w", e.ID, err)
		}
		frozen++
	}
	return frozen, nil
}
