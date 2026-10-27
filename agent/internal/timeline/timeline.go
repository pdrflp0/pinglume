package timeline

import (
	"context"
	"sort"
	"time"

	"pinglume/agent/internal/backoff"
	"pinglume/agent/internal/store"
)

type Bucket struct {
	T     int64          `json:"t"`
	N     int            `json:"n"`
	OK    int            `json:"ok"`
	Fail  map[string]int `json:"fail,omitempty"`
	P50US *int64         `json:"p50Us"`
	P95US *int64         `json:"p95Us"`
	MaxUS *int64         `json:"maxUs"`
}

type Series struct {
	ProbeDefID      string         `json:"probeDefId"`
	Endpoint        string         `json:"endpoint"`
	Kind            string         `json:"kind"`
	ProtocolVersion int            `json:"protocolVersion"`
	Target          string         `json:"target"`
	IntervalMS      int64          `json:"intervalMs"`
	Buckets         []Bucket       `json:"buckets"`
	Backoff         []backoff.Span `json:"backoff"`
}

type Interval struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	State  string    `json:"state"`
	Reason string    `json:"reason"`
	Open   bool      `json:"open"`
}

type Event struct {
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail string    `json:"detail"`
}

type Timeline struct {
	From     time.Time  `json:"from"`
	To       time.Time  `json:"to"`
	BucketMS int64      `json:"bucketMs"`
	Series   []Series   `json:"series"`
	Coverage []Interval `json:"coverage"`
	Events   []Event    `json:"events"`
}

func Build(ctx context.Context, st *store.Store, from, to time.Time, n int, liveUntil time.Time) (Timeline, error) {
	bucket := max(time.Second, to.Sub(from)/time.Duration(n)).Round(time.Second)
	if bucket*time.Duration(n) < to.Sub(from) {
		bucket += time.Second
	}
	tl := Timeline{From: from, To: to, BucketMS: bucket.Milliseconds(),
		Series: []Series{}, Coverage: []Interval{}, Events: []Event{}}

	rows, err := st.ObservationsBetween(ctx, from, to)
	if err != nil {
		return tl, err
	}
	type acc struct {
		b   Bucket
		oks []time.Duration
	}
	bySeries := map[string]*Series{}
	accs := map[string]map[int64]*acc{}
	var order []string
	for _, r := range rows {
		s, ok := bySeries[r.ProbeDefID]
		if !ok {
			s = &Series{ProbeDefID: r.ProbeDefID, Endpoint: r.EndpointLabel, Kind: r.Kind,
				ProtocolVersion: r.ProtocolVersion, Target: r.Target, IntervalMS: r.Interval.Milliseconds()}
			bySeries[r.ProbeDefID] = s
			accs[r.ProbeDefID] = map[int64]*acc{}
			order = append(order, r.ProbeDefID)
		}
		idx := int64(r.Started.Sub(from) / bucket)
		a := accs[r.ProbeDefID][idx]
		if a == nil {
			a = &acc{b: Bucket{T: from.Add(time.Duration(idx) * bucket).UnixMilli()}}
			accs[r.ProbeDefID][idx] = a
		}
		a.b.N++
		if r.Status == "ok" {
			a.b.OK++
			a.oks = append(a.oks, r.Duration)
		} else {
			if a.b.Fail == nil {
				a.b.Fail = map[string]int{}
			}
			a.b.Fail[r.Status]++
		}
	}
	spans, err := backoff.Spans(ctx, st, from, to)
	if err != nil {
		return tl, err
	}
	for _, id := range order {
		s := bySeries[id]
		s.Backoff = spans[id]
		if s.Backoff == nil {
			s.Backoff = []backoff.Span{}
		}
		idxs := make([]int64, 0, len(accs[id]))
		for i := range accs[id] {
			idxs = append(idxs, i)
		}
		sort.Slice(idxs, func(i, j int) bool { return idxs[i] < idxs[j] })
		for _, i := range idxs {
			a := accs[id][i]
			if len(a.oks) > 0 {
				sort.Slice(a.oks, func(i, j int) bool { return a.oks[i] < a.oks[j] })
				a.b.P50US = rank(a.oks, 0.50)
				a.b.P95US = rank(a.oks, 0.95)
				m := a.oks[len(a.oks)-1].Microseconds()
				a.b.MaxUS = &m
			}
			s.Buckets = append(s.Buckets, a.b)
		}
		tl.Series = append(tl.Series, *s)
	}
	sort.SliceStable(tl.Series, func(i, j int) bool {
		if tl.Series[i].Endpoint != tl.Series[j].Endpoint {
			return tl.Series[i].Endpoint < tl.Series[j].Endpoint
		}
		return tl.Series[i].Kind < tl.Series[j].Kind
	})

	ivs, err := st.Intervals(ctx, from, to)
	if err != nil {
		return tl, err
	}
	for _, iv := range store.ExtendLive(ivs, liveUntil, to) {
		if iv.Duration() > 0 {
			tl.Coverage = append(tl.Coverage, Interval{Start: iv.Start, End: iv.End, State: iv.State, Reason: iv.Reason, Open: iv.Open})
		}
	}
	evs, err := st.SystemEvents(ctx, from, to)
	if err != nil {
		return tl, err
	}
	for _, e := range evs {
		tl.Events = append(tl.Events, Event{At: e.At, Kind: e.Kind, Detail: e.Detail})
	}
	return tl, nil
}

func rank(sorted []time.Duration, p float64) *int64 {
	v := sorted[int(float64(len(sorted)-1)*p+0.5)].Microseconds()
	return &v
}
