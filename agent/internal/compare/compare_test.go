package compare

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pinglume/agent/internal/store"
)

var t0 = time.Date(2026, 10, 24, 20, 0, 0, 0, time.UTC)

type fx struct {
	t    *testing.T
	st   *store.Store
	dev  string
	defs map[string]string
	seq  map[string]int64
	rng  *rand.Rand
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
	f := &fx{t: t, st: st, dev: dev.ID, defs: map[string]string{}, seq: map[string]int64{}, rng: rand.New(rand.NewPCG(1, 2))}
	for _, k := range []string{"tcp", "https"} {
		d, _ := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "ep-1", Host: "h", Owner: "dev"},
			store.ProbeDefinition{Kind: k, Target: k, Interval: 10 * time.Second, Timeout: time.Second, ProtocolVersion: 1}, t0)
		f.defs[k] = d.ID
	}

	st.Transition(ctx, f.dev, t0, nil, &store.Interval{Start: t0.Add(-time.Hour), State: "measuring"})
	st.ExtendOpen(ctx, f.dev, t0.Add(3*time.Hour))
	return f
}

func (f *fx) obs(kind string, a, b time.Time, base time.Duration, failEvery int) {
	ctx := context.Background()
	i := 0
	for t := a; t.Before(b); t = t.Add(10 * time.Second) {
		i++
		f.seq[kind]++
		status, dur := "ok", time.Duration(float64(base)*(0.9+0.2*f.rng.Float64()))
		if failEvery > 0 && i%failEvery == 0 {
			status, dur = "refused", time.Millisecond
		}
		if err := f.st.InsertObservation(ctx, store.Observation{ID: fmt.Sprintf("%s-%d", kind, f.seq[kind]),
			DeviceID: f.dev, ProbeDefID: f.defs[kind], Seq: f.seq[kind], StartedWall: t, Duration: dur,
			Status: status, ReceivedAt: t}); err != nil {
			f.t.Fatal(err)
		}
	}
}

var (
	aFrom, aTo = t0, t0.Add(10 * time.Minute)
	bFrom, bTo = t0.Add(12 * time.Minute), t0.Add(22 * time.Minute)
)

func (f *fx) experiment(bEnd *time.Time) store.Experiment {
	ctx := context.Background()
	e, _ := f.st.InsertExperiment(ctx, f.dev, store.Experiment{Question: "Cabo melhora?", Intervention: "cabo",
		PlannedWindow: (10 * time.Minute).Milliseconds(), CreatedAt: aFrom})
	f.st.AddWindow(ctx, e.ID, store.Window{Role: "baseline", Start: aFrom, PlannedEnd: aTo,
		Conditions: []store.Condition{{Key: "conexão", Value: "Wi-Fi", Source: "declared"}}})
	f.st.AddWindow(ctx, e.ID, store.Window{Role: "intervention", Start: bFrom, PlannedEnd: bTo,
		Conditions: []store.Condition{{Key: "conexão", Value: "cabo", Source: "declared"}}})
	if bEnd != nil {
		f.st.CancelExperiment(ctx, e.ID, *bEnd)
	}
	got, err := f.st.Experiment(ctx, e.ID)
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

func (f *fx) compute(e store.Experiment) Result {
	f.t.Helper()
	r, err := Compute(context.Background(), f.st, e)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func probe(r Result, kind string) ProbeComparison {
	for _, p := range r.Probes {
		if p.Kind == kind {
			return p
		}
	}
	return ProbeComparison{}
}

func TestSameConditionsNoDifference(t *testing.T) {
	f := newFx(t)
	for _, k := range []string{"tcp", "https"} {
		f.obs(k, aFrom, aTo, 5*time.Millisecond, 0)
		f.obs(k, bFrom, bTo, 5*time.Millisecond, 0)
	}
	r := f.compute(f.experiment(nil))
	if r.Verdict != NoDifference {
		t.Fatalf("veredito = %s; %+v", r.Verdict, probe(r, "tcp").Latency)
	}
}

func TestClearImprovement(t *testing.T) {
	f := newFx(t)
	f.obs("tcp", aFrom, aTo, 5*time.Millisecond, 0)
	f.obs("tcp", bFrom, bTo, 5*time.Millisecond, 0)
	f.obs("https", aFrom, aTo, 20*time.Millisecond, 0)
	f.obs("https", bFrom, bTo, 8*time.Millisecond, 0)
	r := f.compute(f.experiment(nil))
	h := probe(r, "https")
	if h.Verdict != Better || h.Latency.CIHigh >= 0 {
		t.Fatalf("https: %+v", h.Latency)
	}
	if probe(r, "tcp").Verdict != NoDifference || r.Verdict != Better {
		t.Fatalf("geral = %s, tcp = %s", r.Verdict, probe(r, "tcp").Verdict)
	}
	if !strings.Contains(strings.Join(r.Caveats, " "), "mudança e horário se confundem") {
		t.Fatal("a ressalva de ordem A→B precisa aparecer mesmo com melhora")
	}
}

func TestFailuresMakeItWorse(t *testing.T) {
	f := newFx(t)
	for _, k := range []string{"tcp", "https"} {
		f.obs(k, aFrom, aTo, 5*time.Millisecond, 0)
		f.obs(k, bFrom, bTo, 5*time.Millisecond, 4)
	}
	r := f.compute(f.experiment(nil))
	if p := probe(r, "tcp"); p.Failures.Verdict != Worse || r.Verdict != Worse {
		t.Fatalf("falhas: %+v geral=%s", p.Failures, r.Verdict)
	}
}

func TestNoisySmallDifferenceIsNotAClaim(t *testing.T) {
	f := newFx(t)

	ctx := context.Background()
	for i, w := range [][2]time.Time{{aFrom, aTo}, {bFrom, bTo}} {
		n := 0
		for ts := w[0]; ts.Before(w[1]); ts = ts.Add(10 * time.Second) {
			n++
			d := time.Millisecond
			if f.rng.IntN(2) == 0 {
				d = 40 * time.Millisecond
			}
			if i == 1 {
				d += 500 * time.Microsecond
			}
			f.seq["tcp"]++
			f.st.InsertObservation(ctx, store.Observation{ID: fmt.Sprintf("n-%d", f.seq["tcp"]), DeviceID: f.dev,
				ProbeDefID: f.defs["tcp"], Seq: f.seq["tcp"], StartedWall: ts, Duration: d, Status: "ok", ReceivedAt: ts})
		}
	}
	r := f.compute(f.experiment(nil))
	if v := probe(r, "tcp").Verdict; v == Better || v == Worse {
		t.Fatalf("diferença pequena e ruidosa não pode virar %s: %+v", v, probe(r, "tcp").Latency)
	}
}

func TestLowCoverageNotComparable(t *testing.T) {
	f := newFx(t)
	f.obs("tcp", aFrom, aTo, 5*time.Millisecond, 0)
	f.obs("tcp", bFrom, bFrom.Add(5*time.Minute), 5*time.Millisecond, 0)
	r := f.compute(f.experiment(nil))
	p := probe(r, "tcp")
	if p.Comparable || r.Verdict != NotComparable || !strings.Contains(strings.Join(p.Reasons, " "), "janela B") {
		t.Fatalf("%+v %s", p, r.Verdict)
	}
}

func TestShortCancelledWindowNotComparable(t *testing.T) {
	f := newFx(t)
	f.obs("tcp", aFrom, aTo, 5*time.Millisecond, 0)
	f.obs("tcp", bFrom, bFrom.Add(4*time.Minute), 5*time.Millisecond, 0)
	end := bFrom.Add(4 * time.Minute)
	e := f.experiment(&end)
	if Phase(e, bTo.Add(time.Hour)) != PhaseCancelled {
		t.Fatal("experimento cancelado deveria ficar cancelado")
	}
	r := f.compute(e)
	if p := probe(r, "tcp"); p.Comparable || !strings.Contains(strings.Join(p.Reasons, " "), "abaixo do mínimo") {
		t.Fatalf("%+v", p)
	}
}

func TestDifferentProbeDefinitionsNotComparable(t *testing.T) {
	f := newFx(t)
	f.obs("tcp", aFrom, aTo, 5*time.Millisecond, 0)
	f.obs("https", bFrom, bTo, 5*time.Millisecond, 0)
	r := f.compute(f.experiment(nil))
	if r.Verdict != NotComparable || len(r.Probes) != 2 || r.Probes[0].Comparable || r.Probes[1].Comparable {
		t.Fatalf("%s %+v", r.Verdict, r.Probes)
	}
}

func TestDetectedInterfaceUnchangedIsFlagged(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	f.st.InsertSystemEvent(ctx, aFrom.Add(-time.Minute), "net_context", `{"interface":"en0","kind":"wifi","method":"teste"}`)
	f.st.InsertSystemEvent(ctx, bFrom.Add(3*time.Minute), "net_change", `{"interface":"en5","kind":"ethernet","method":"teste"}`)
	for _, k := range []string{"tcp", "https"} {
		f.obs(k, aFrom, aTo, 5*time.Millisecond, 0)
		f.obs(k, bFrom, bTo, 5*time.Millisecond, 0)
	}
	r := f.compute(f.experiment(nil))
	all := strings.Join(r.Caveats, " ")
	if !strings.Contains(all, "não foi confirmada pela detecção") {
		t.Fatalf("Wi-Fi nas duas janelas deveria ser apontado: %v", r.Caveats)
	}
	if !strings.Contains(all, "mudou 1 vez(es) durante a janela B") {
		t.Fatalf("mudança de rede dentro de B deveria ser apontada: %v", r.Caveats)
	}
}

func TestDeterministic(t *testing.T) {
	f := newFx(t)
	for _, k := range []string{"tcp", "https"} {
		f.obs(k, aFrom, aTo, 5*time.Millisecond, 0)
		f.obs(k, bFrom, bTo, 6*time.Millisecond, 0)
	}
	e := f.experiment(nil)
	r1, r2 := f.compute(e), f.compute(e)
	if *probe(r1, "tcp").Latency != *probe(r2, "tcp").Latency {
		t.Fatal("o mesmo experimento deveria dar o mesmo intervalo")
	}
}

func TestPhases(t *testing.T) {
	f := newFx(t)
	ctx := context.Background()
	e, _ := f.st.InsertExperiment(ctx, f.dev, store.Experiment{Question: "q", Intervention: "i", PlannedWindow: 600000, CreatedAt: aFrom})
	f.st.AddWindow(ctx, e.ID, store.Window{Role: "baseline", Start: aFrom, PlannedEnd: aTo})
	e, _ = f.st.Experiment(ctx, e.ID)
	if Phase(e, aFrom.Add(time.Minute)) != PhaseBaseline || Phase(e, aTo.Add(time.Second)) != PhaseAwaiting {
		t.Fatal("fases da janela A")
	}
	f.st.AddWindow(ctx, e.ID, store.Window{Role: "intervention", Start: bFrom, PlannedEnd: bTo})
	e, _ = f.st.Experiment(ctx, e.ID)
	if Phase(e, bFrom.Add(time.Minute)) != PhaseIntervention || Phase(e, bTo) != PhaseDone {
		t.Fatal("fases da janela B")
	}
	if r, _ := Load(ctx, f.st, e, bFrom.Add(time.Minute)); r != nil {
		t.Fatal("sem resultado antes do fim")
	}
}
