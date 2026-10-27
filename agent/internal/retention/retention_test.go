package retention

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/incident"
	"pinglume/agent/internal/store"
	"pinglume/agent/internal/summary"
	"pinglume/agent/internal/timeline"
)

var t0 = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type fx struct {
	st  *store.Store
	dev string
	def string
	clk *clock.Fake
	seq int64
}

func newFx(t *testing.T) *fx {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, t0)
	d, _ := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "ep", Host: "h", Owner: "dev"},
		store.ProbeDefinition{Kind: "tcp", Target: "h:1", Interval: 10 * time.Second, Timeout: time.Second, ProtocolVersion: 1}, t0)
	return &fx{st: st, dev: dev.ID, def: d.ID, clk: clock.NewFake(t0)}
}

func (f *fx) obs(t *testing.T, from, to time.Time, every time.Duration, status func(int) string, dur func(int) time.Duration) {
	t.Helper()
	ctx := context.Background()
	i := 0
	for ts := from; ts.Before(to); ts = ts.Add(every) {
		f.seq++
		if err := f.st.InsertObservation(ctx, store.Observation{ID: fmt.Sprint(f.seq), DeviceID: f.dev, ProbeDefID: f.def,
			Seq: f.seq, StartedWall: ts, Duration: dur(i), Status: status(i), ReceivedAt: ts}); err != nil {
			t.Fatal(err)
		}
		i++
	}
}

func ok(int) string { return "ok" }
func ms(n int) func(int) time.Duration {
	return func(int) time.Duration { return time.Duration(n) * time.Millisecond }
}

func (f *fx) maint(p Policy) *Maintainer {
	return &Maintainer{Store: f.st, Clock: f.clk, Policy: p}
}

func events(t *testing.T, st *store.Store, kind string) []map[string]any {
	t.Helper()
	evs, _ := st.SystemEvents(context.Background(), t0.Add(-365*24*time.Hour), t0.Add(365*24*time.Hour))
	var out []map[string]any
	for _, e := range evs {
		if e.Kind == kind {
			var d map[string]any
			json.Unmarshal([]byte(e.Detail), &d)
			out = append(out, d)
		}
	}
	return out
}

func TestCompactsOldRawIntoMinutesWithExactCounts(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	f.obs(t, t0, t0.Add(2*time.Minute), 10*time.Second,
		func(i int) string {
			if i%4 == 3 {
				return "refused"
			}
			return "ok"
		},
		func(i int) time.Duration { return time.Duration(i+1) * time.Millisecond })
	f.obs(t, t0.Add(8*24*time.Hour), t0.Add(8*24*time.Hour+time.Minute), 10*time.Second, ok, ms(1))

	f.clk.SetWall(t0.Add(8*24*time.Hour + 2*time.Minute))
	rep, err := f.maint(Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour}).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Compacted) != 1 || rep.Compacted[0].Rows != 12 || rep.Compacted[0].Minutes != 2 {
		t.Fatalf("compactação: %+v", rep.Compacted)
	}
	rows, _ := f.st.ObservationsBetween(ctx, t0.Add(-time.Hour), t0.Add(time.Hour))
	if len(rows) != 0 {
		t.Fatalf("brutos antigos deveriam ter saído: %d", len(rows))
	}
	recent, _ := f.st.ObservationsBetween(ctx, t0.Add(8*24*time.Hour), t0.Add(9*24*time.Hour))
	if len(recent) != 6 {
		t.Fatalf("brutos recentes não podem ser tocados: %d", len(recent))
	}
	mins, _ := f.st.MinutesBetween(ctx, t0, t0.Add(time.Hour))
	if len(mins) != 2 {
		t.Fatalf("agregados: %+v", mins)
	}
	m0 := mins[0]
	if m0.N != 6 || m0.OK != 5 || m0.Failures["refused"] != 1 || *m0.MinUS != 1000 || *m0.MaxUS != 6000 || *m0.P50US != 3000 {
		t.Fatalf("minuto 0: n=%d ok=%d f=%v min=%d max=%d p50=%d", m0.N, m0.OK, m0.Failures, *m0.MinUS, *m0.MaxUS, *m0.P50US)
	}
	if evs := events(t, f.st, "retention_compaction"); len(evs) != 1 {
		t.Fatalf("evento de compactação: %v", evs)
	}
	again, _ := f.maint(Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour}).Run(ctx)
	if len(again.Compacted) != 0 {
		t.Fatal("rodar de novo não deveria compactar nada")
	}
}

func TestDropsOldAggregatesAndRecordsIt(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	f.obs(t, t0, t0.Add(time.Minute), 10*time.Second, ok, ms(1))
	f.clk.SetWall(t0.Add(100 * 24 * time.Hour))
	rep, err := f.maint(Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour}).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rep.DroppedRows != 1 {
		t.Fatalf("agregado antigo deveria sair: %+v", rep)
	}
	if evs := events(t, f.st, "history_dropped"); len(evs) != 1 || evs[0]["reason"] != "retenção de agregados" {
		t.Fatalf("evento: %v", evs)
	}
}

func TestFreezesIncidentAnalysisBeforeCompacting(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	f.st.Transition(ctx, f.dev, t0, nil, &store.Interval{Start: t0.Add(-3 * time.Hour), State: "measuring"})
	f.st.ExtendOpen(ctx, f.dev, t0.Add(time.Hour))
	f.st.Transition(ctx, f.dev, t0.Add(time.Hour), nil, nil)
	f.obs(t, t0.Add(-3*time.Hour), t0.Add(-4*time.Minute), 10*time.Second, ok, ms(1))
	f.obs(t, t0.Add(-4*time.Minute), t0.Add(time.Minute), 10*time.Second, func(int) string { return "refused" }, ms(1))
	inc, _ := f.st.InsertIncident(ctx, f.dev, store.Incident{ReportedAt: t0, CreatedAt: t0, Symptom: "audio_cut", CreatedVia: "cli"})

	f.clk.SetWall(t0.Add(10 * 24 * time.Hour))
	rep, err := f.maint(Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour}).Run(ctx)
	if err != nil || rep.Frozen != 1 {
		t.Fatalf("congelamento: %+v %v", rep, err)
	}
	a, err := incident.Load(ctx, f.st, inc, f.clk.Now(), time.Time{})
	if err != nil || a.Verdict != incident.VerdictDegradation {
		t.Fatalf("análise depois da compactação deveria ser a congelada: %s %v", a.Verdict, err)
	}
}

func TestDiskLimitCompactsAndThenDrops(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	f.obs(t, t0, t0.Add(3*time.Hour), time.Second, ok, ms(2))
	f.clk.SetWall(t0.Add(3 * time.Hour))
	before, _ := f.st.Usage(ctx)

	rep, err := f.maint(Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour, LimitBytes: before.UsedBytes / 4}).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.OverLimit || len(rep.Compacted) == 0 || rep.Compacted[0].Reason != "limite de disco" {
		t.Fatalf("limite deveria forçar compactação: %+v", rep)
	}
	if rep.UsageAfter.UsedBytes > before.UsedBytes/4 && !rep.LimitReached {
		t.Fatalf("uso %d acima do limite %d sem registrar", rep.UsageAfter.UsedBytes, before.UsedBytes/4)
	}

	tiny, _ := f.maint(Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour, LimitBytes: 1}).Run(ctx)
	if !tiny.LimitReached || len(events(t, f.st, "disk_limit")) == 0 {
		t.Fatalf("limite impossível deveria ser registrado, não silenciado: %+v", tiny)
	}
	if len(events(t, f.st, "history_dropped")) == 0 {
		t.Fatal("antes de desistir, agregados antigos deveriam ter sido descartados com evento")
	}
}

func TestDiskFullIsDetectable(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	u, _ := f.st.Usage(ctx)
	if err := f.st.SetMaxPages(ctx, u.FileBytes/4096+2); err != nil {
		t.Fatal(err)
	}
	var err error
	for i := 0; i < 5000 && err == nil; i++ {
		f.seq++
		err = f.st.InsertObservation(ctx, store.Observation{ID: fmt.Sprint(f.seq), DeviceID: f.dev, ProbeDefID: f.def,
			Seq: f.seq, StartedWall: t0, Duration: time.Millisecond, Status: "ok", ReceivedAt: t0,
			ErrorDetail: fmt.Sprintf("%0200d", i)})
	}
	if !store.IsDiskFull(err) {
		t.Fatalf("esperava disco cheio, veio %v", err)
	}
}

func TestReadersUseAggregatesAfterCompaction(t *testing.T) {
	ctx := context.Background()
	f := newFx(t)
	f.st.Transition(ctx, f.dev, t0, nil, &store.Interval{Start: t0, State: "measuring"})
	f.st.ExtendOpen(ctx, f.dev, t0.Add(10*time.Minute))
	f.st.Transition(ctx, f.dev, t0.Add(10*time.Minute), nil, nil)
	f.obs(t, t0, t0.Add(10*time.Minute), 10*time.Second,
		func(i int) string {
			if i%10 == 0 {
				return "timeout"
			}
			return "ok"
		}, ms(5))
	from, to := t0, t0.Add(10*time.Minute)
	before, _ := summary.Build(ctx, f.st, from, to, summary.Options{})
	tlBefore, _ := timeline.Build(ctx, f.st, from, to, 10, time.Time{})

	f.clk.SetWall(t0.Add(8 * 24 * time.Hour))
	if _, err := f.maint(Policy{Raw: 7 * 24 * time.Hour, Aggregates: 90 * 24 * time.Hour}).Run(ctx); err != nil {
		t.Fatal(err)
	}
	after, _ := summary.Build(ctx, f.st, from, to, summary.Options{})
	b, a := before.Probes[0], after.Probes[0]
	if a.Total != b.Total || a.OK != b.OK || a.Failures["timeout"] != b.Failures["timeout"] || *a.Coverage != *b.Coverage {
		t.Fatalf("contagens mudaram: antes %+v depois %+v", b, a)
	}
	if !a.Approximate || b.Approximate || *a.P50US != 5000 {
		t.Fatalf("percentis depois da compactação: approx=%v p50=%d", a.Approximate, *a.P50US)
	}
	tlAfter, _ := timeline.Build(ctx, f.st, from, to, 10, time.Time{})
	nb, na := 0, 0
	for _, x := range tlBefore.Series[0].Buckets {
		nb += x.N
	}
	for _, x := range tlAfter.Series[0].Buckets {
		na += x.N
		if !x.Approx {
			t.Fatal("trecho vindo de agregado deveria ser marcado como aproximado")
		}
	}
	if na != nb {
		t.Fatalf("linha do tempo perdeu execuções: %d → %d", nb, na)
	}
}
