package summary

import (
	"context"
	"sort"
	"time"

	"pinglume/agent/internal/backoff"
	"pinglume/agent/internal/stats"
	"pinglume/agent/internal/store"
)

type Gap struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	State  string    `json:"state"`
	Reason string    `json:"reason"`
}

type Probe struct {
	ProbeDefID      string         `json:"probeDefId"`
	Endpoint        string         `json:"endpoint"`
	Kind            string         `json:"kind"`
	ProtocolVersion int            `json:"protocolVersion"`
	Target          string         `json:"target"`
	IntervalMS      int64          `json:"intervalMs"`
	Total           int            `json:"total"`
	OK              int            `json:"ok"`
	Failures        map[string]int `json:"failures"`
	P50US           *int64         `json:"p50Us"`
	P95US           *int64         `json:"p95Us"`
	Coverage        *float64       `json:"coverage"`
	BackoffMS       int64          `json:"backoffMs"`
	Approximate     bool           `json:"approximate"`
	LastStarted     time.Time      `json:"lastStarted"`
}

type Summary struct {
	From    time.Time        `json:"from"`
	To      time.Time        `json:"to"`
	StateMS map[string]int64 `json:"stateMs"`
	Gaps    []Gap            `json:"gaps"`
	Probes  []Probe          `json:"probes"`
}

const Unrecorded = "unrecorded"

type Options struct {
	LiveUntil time.Time
}

func Build(ctx context.Context, st *store.Store, from, to time.Time, opt Options) (Summary, error) {
	s := Summary{From: from, To: to, StateMS: map[string]int64{}, Gaps: []Gap{}, Probes: []Probe{}}
	ivs, err := st.Intervals(ctx, from, to)
	if err != nil {
		return s, err
	}
	ivs = store.ExtendLive(ivs, opt.LiveUntil, to)
	var covered, measuring time.Duration
	for _, iv := range ivs {
		d := iv.Duration()
		s.StateMS[iv.State] += d.Milliseconds()
		covered += d
		if iv.State == "measuring" {
			measuring += d
		} else {
			s.Gaps = append(s.Gaps, Gap{Start: iv.Start, End: iv.End, State: iv.State, Reason: iv.Reason})
		}
	}
	if u := to.Sub(from) - covered; u > 0 {
		s.StateMS[Unrecorded] = u.Milliseconds()
	}

	sums, err := st.Summaries(ctx, from)
	if err != nil {
		return s, err
	}
	spans, err := backoff.Spans(ctx, st, from, to)
	if err != nil {
		return s, err
	}
	mins, err := st.MinutesBetween(ctx, from, to)
	if err != nil {
		return s, err
	}
	type minAcc struct {
		meta     store.MinuteRow
		n, ok    int
		fails    map[string]int
		p50, p95 []stats.Weighted
	}
	byMin := map[string]*minAcc{}
	var minOrder []string
	for _, m := range mins {
		a := byMin[m.ProbeDefID]
		if a == nil {
			a = &minAcc{meta: m, fails: map[string]int{}}
			byMin[m.ProbeDefID] = a
			minOrder = append(minOrder, m.ProbeDefID)
		}
		a.n += m.N
		a.ok += m.OK
		for k, v := range m.Failures {
			a.fails[k] += v
		}
		if m.P50US != nil {
			a.p50 = append(a.p50, stats.Weighted{Value: *m.P50US, Weight: m.OK})
		}
		if m.P95US != nil {
			a.p95 = append(a.p95, stats.Weighted{Value: *m.P95US, Weight: m.OK})
		}
	}
	seen := map[string]bool{}
	for _, ps := range sums {
		seen[ps.ProbeDefID] = true
	}
	for _, id := range minOrder {
		if !seen[id] {
			m := byMin[id].meta
			sums = append(sums, store.ProbeSummary{ProbeDefID: id, Kind: m.Kind, Target: m.Target, EndpointLabel: m.EndpointLabel,
				ProtocolVersion: m.ProtocolVersion, Interval: m.Interval, ByStatus: map[string]int{}})
		}
	}

	for _, ps := range sums {
		p := Probe{
			ProbeDefID: ps.ProbeDefID, BackoffMS: backoff.Total(spans[ps.ProbeDefID]).Milliseconds(),
			Endpoint: ps.EndpointLabel, Kind: ps.Kind, ProtocolVersion: ps.ProtocolVersion, Target: ps.Target,
			IntervalMS: ps.Interval.Milliseconds(), Total: ps.Total, OK: ps.ByStatus["ok"],
			Failures: map[string]int{}, LastStarted: ps.LastStarted,
			P50US: percentile(ps.OKDurations, 0.50), P95US: percentile(ps.OKDurations, 0.95),
		}
		for k, v := range ps.ByStatus {
			if k != "ok" {
				p.Failures[k] = v
			}
		}
		cancelled := ps.ByStatus["cancelled"]
		if a := byMin[ps.ProbeDefID]; a != nil {
			p.Approximate = true
			p.Total += a.n
			p.OK += a.ok
			for k, v := range a.fails {
				p.Failures[k] += v
			}
			p50, p95 := append([]stats.Weighted(nil), a.p50...), append([]stats.Weighted(nil), a.p95...)
			for _, d := range ps.OKDurations {
				p50 = append(p50, stats.Weighted{Value: d.Microseconds(), Weight: 1})
				p95 = append(p95, stats.Weighted{Value: d.Microseconds(), Weight: 1})
			}
			p.P50US, p.P95US = stats.Quantile(p50, 0.5), stats.Quantile(p95, 0.95)
			if p.LastStarted.IsZero() {
				p.LastStarted = a.meta.Minute
			}
		}
		if ps.Interval > 0 && measuring >= ps.Interval {
			expected := float64(measuring) / float64(ps.Interval)
			c := min(1, float64(p.Total-cancelled)/expected)
			p.Coverage = &c
		}
		s.Probes = append(s.Probes, p)
	}
	sort.SliceStable(s.Probes, func(i, j int) bool { return s.Probes[i].Endpoint < s.Probes[j].Endpoint })
	return s, nil
}

func percentile(sorted []time.Duration, p float64) *int64 {
	if len(sorted) == 0 {
		return nil
	}
	v := sorted[int(float64(len(sorted)-1)*p+0.5)].Microseconds()
	return &v
}
