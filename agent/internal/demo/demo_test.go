package demo

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pinglume/agent/internal/compare"
	"pinglume/agent/internal/incident"
	"pinglume/agent/internal/store"
)

var now = time.Date(2026, 11, 1, 15, 30, 20, 0, time.UTC)

func generate(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "demo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err := st.ClaimMode(ctx, store.ModeDemo); err != nil {
		t.Fatal(err)
	}
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: DeviceName, AgentVersion: "t"}, now)
	if err := NewScenario(now).Generate(ctx, st, dev.ID); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestScenarioProducesTheIntendedStory(t *testing.T) {
	ctx := context.Background()
	st := generate(t)
	incs, _ := st.Incidents(ctx, now.Add(-4*time.Hour), now)
	if len(incs) != 2 {
		t.Fatalf("relatos: %d", len(incs))
	}
	verdicts := map[string]string{}
	for _, inc := range incs {
		a, err := incident.Load(ctx, st, inc, now.Add(time.Hour), time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		verdicts[inc.Symptom] = a.Verdict
	}
	if verdicts["audio_cut"] != incident.VerdictDegradation || verdicts["slow"] != incident.VerdictNoChange {
		t.Fatalf("vereditos dos relatos: %v", verdicts)
	}

	exps, _ := st.Experiments(ctx, 5)
	if len(exps) != 1 {
		t.Fatalf("comparações: %d", len(exps))
	}
	r, err := compare.Load(ctx, st, exps[0], now.Add(time.Hour))
	if err != nil || r == nil {
		t.Fatalf("comparação: %v", err)
	}
	if r.Verdict != compare.Better {
		t.Fatalf("comparação deveria mostrar melhora: %s %+v", r.Verdict, r.Probes)
	}
	if n := r.Windows[1].NetAtStart; n == nil || n.Kind != "ethernet" {
		t.Fatalf("janela B deveria detectar cabo: %+v", n)
	}

	ivs, _ := st.Intervals(ctx, now.Add(-4*time.Hour), now)
	states := map[string]int{}
	for _, iv := range ivs {
		states[iv.State]++
	}
	if states["agent_down"] != 1 || states["paused"] != 1 {
		t.Fatalf("lacunas simuladas: %v", states)
	}
}

func TestScenarioIsReproducible(t *testing.T) {
	ctx := context.Background()
	a, b := generate(t), generate(t)
	ra, _ := a.ObservationsBetween(ctx, now.Add(-4*time.Hour), now)
	rb, _ := b.ObservationsBetween(ctx, now.Add(-4*time.Hour), now)
	if len(ra) == 0 || len(ra) != len(rb) {
		t.Fatalf("tamanhos: %d %d", len(ra), len(rb))
	}
	for i := range ra {
		if ra[i].Kind != rb[i].Kind || ra[i].Status != rb[i].Status || ra[i].Duration != rb[i].Duration || !ra[i].Started.Equal(rb[i].Started) {
			t.Fatalf("observação %d diferente: %+v x %+v", i, ra[i], rb[i])
		}
	}
}

func TestLiveProbesBurstOnSchedule(t *testing.T) {
	ctx := context.Background()
	st := generate(t)
	clock := now
	jobs, err := Jobs(ctx, st, func() time.Time { return clock })
	if err != nil || len(jobs) != 3 {
		t.Fatalf("jobs: %d %v", len(jobs), err)
	}
	count := func() int {
		fails := 0
		for i := 0; i < 200; i++ {
			if jobs[0].Prober.Probe(ctx).Status != "ok" {
				fails++
			}
		}
		return fails
	}
	clock = time.Date(2026, 11, 1, 15, 22, 0, 0, time.UTC)
	burst := count()
	clock = time.Date(2026, 11, 1, 15, 25, 0, 0, time.UTC)
	calm := count()
	if burst < 60 || calm > 10 {
		t.Fatalf("falhas na rajada %d, fora dela %d", burst, calm)
	}
}
