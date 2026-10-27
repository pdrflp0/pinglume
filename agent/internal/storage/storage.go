package storage

import (
	"context"
	"encoding/json"
	"time"

	"pinglume/agent/internal/retention"
	"pinglume/agent/internal/store"
)

type Event struct {
	At     time.Time      `json:"at"`
	Kind   string         `json:"kind"`
	Detail map[string]any `json:"detail"`
}

type Info struct {
	Usage             store.Usage `json:"usage"`
	LimitBytes        int64       `json:"limitBytes"`
	RawRetentionMS    int64       `json:"rawRetentionMs"`
	AggRetentionMS    int64       `json:"aggregateRetentionMs"`
	OldestRaw         *time.Time  `json:"oldestRaw"`
	OldestAggregate   *time.Time  `json:"oldestAggregate"`
	RecentMaintenance []Event     `json:"recentMaintenance"`
}

func Load(ctx context.Context, st *store.Store, p retention.Policy, now time.Time) (Info, error) {
	info := Info{LimitBytes: p.LimitBytes, RawRetentionMS: p.Raw.Milliseconds(), AggRetentionMS: p.Aggregates.Milliseconds(),
		RecentMaintenance: []Event{}}
	var err error
	if info.Usage, err = st.Usage(ctx); err != nil {
		return info, err
	}
	if t, ok, err := st.OldestObservation(ctx); err != nil {
		return info, err
	} else if ok {
		info.OldestRaw = &t
	}
	if t, ok, err := st.OldestMinute(ctx); err != nil {
		return info, err
	} else if ok {
		info.OldestAggregate = &t
	}
	evs, err := st.SystemEvents(ctx, now.Add(-30*24*time.Hour), now)
	if err != nil {
		return info, err
	}
	for i := len(evs) - 1; i >= 0 && len(info.RecentMaintenance) < 10; i-- {
		e := evs[i]
		switch e.Kind {
		case "retention_compaction", "history_dropped", "disk_limit", "disk_full_recovered":
			ev := Event{At: e.At, Kind: e.Kind}
			json.Unmarshal([]byte(e.Detail), &ev.Detail)
			info.RecentMaintenance = append(info.RecentMaintenance, ev)
		}
	}
	return info, nil
}
