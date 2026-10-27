package coverage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/store"
)

const hb = 5 * time.Second

var t0 = time.Date(2026, 10, 20, 21, 0, 0, 0, time.UTC)

func setup(t *testing.T) (*store.Store, string, *clock.Fake) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dev, err := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	return st, dev.ID, clock.NewFake(t0)
}

func newTracker(st *store.Store, dev string, clk clock.Clock) *Tracker {
	return &Tracker{Store: st, Clock: clk, DeviceID: dev, Heartbeat: hb}
}

func heartbeats(t *testing.T, tr *Tracker, clk *clock.Fake, n int) {
	t.Helper()
	for range n {
		clk.Advance(hb)
		dets, err := tr.Tick(context.Background())
		if err != nil || len(dets) != 0 {
			t.Fatalf("tick normal não deveria detectar nada: %v %v", dets, err)
		}
	}
}

func intervals(t *testing.T, st *store.Store) []store.Interval {
	t.Helper()
	ivs, err := st.Intervals(context.Background(), t0.Add(-time.Hour), t0.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	return ivs
}

func states(ivs []store.Interval) []string {
	var s []string
	for _, iv := range ivs {
		s = append(s, iv.State)
	}
	return s
}

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestNormalHeartbeatsExtendOneInterval(t *testing.T) {
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	if _, err := tr.Start(context.Background(), Measuring); err != nil {
		t.Fatal(err)
	}
	heartbeats(t, tr, clk, 12)
	ivs := intervals(t, st)
	if len(ivs) != 1 || ivs[0].State != Measuring || ivs[0].Duration() != 60*time.Second || !ivs[0].Open {
		t.Fatalf("esperava 1 intervalo aberto de 60 s, veio %+v", ivs)
	}
}

func TestSleepBecomesSuspendedGap(t *testing.T) {
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	tr.Start(context.Background(), Measuring)
	heartbeats(t, tr, clk, 2)

	clk.Advance(time.Second)
	clk.Sleep(10 * time.Minute)
	clk.Advance(4 * time.Second)
	dets, err := tr.Tick(context.Background())
	if err != nil || len(dets) != 1 || dets[0].Kind != Suspended {
		t.Fatalf("esperava suspensão detectada: %+v %v", dets, err)
	}
	ivs := intervals(t, st)
	if got := states(ivs); !eq(got, []string{Measuring, Suspended, Measuring}) {
		t.Fatalf("estados = %v", got)
	}

	if d := ivs[1].Duration(); d != 10*time.Minute {
		t.Fatalf("lacuna de suspensão = %s", d)
	}
	if !ivs[2].Open {
		t.Fatal("medição deveria reabrir após a suspensão")
	}
}

func TestClockBackwardsIsEventNotGap(t *testing.T) {
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	tr.Start(context.Background(), Measuring)
	heartbeats(t, tr, clk, 2)

	clk.SetWall(clk.Now().Add(-time.Hour))
	clk.Advance(hb)
	dets, err := tr.Tick(context.Background())
	if err != nil || len(dets) != 1 || dets[0].Kind != "clock_jump" {
		t.Fatalf("esperava clock_jump: %+v %v", dets, err)
	}
	for _, iv := range intervals(t, st) {
		if iv.State != Measuring {
			t.Fatalf("relógio para trás não deveria criar lacuna: %+v", iv)
		}
		if iv.End.Before(iv.Start) {
			t.Fatalf("intervalo invertido: %+v", iv)
		}
	}
}

func TestStalledAgentIsAgentDown(t *testing.T) {
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	tr.Start(context.Background(), Measuring)
	heartbeats(t, tr, clk, 1)
	clk.Advance(2 * time.Minute)
	dets, _ := tr.Tick(context.Background())
	if len(dets) != 1 || dets[0].Kind != AgentDown {
		t.Fatalf("esperava agent_down por heartbeat atrasado: %+v", dets)
	}
	if got := states(intervals(t, st)); !eq(got, []string{Measuring, AgentDown, Measuring}) {
		t.Fatalf("estados = %v", got)
	}
}

func TestCrashThenRestartRecordsGapFromLastHeartbeat(t *testing.T) {
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	tr.Start(context.Background(), Measuring)
	heartbeats(t, tr, clk, 3)
	lastAlive := clk.Now()

	clk.Advance(20 * time.Minute)
	tr2 := newTracker(st, dev, clk)
	dets, err := tr2.Start(context.Background(), Measuring)
	if err != nil || len(dets) != 1 || dets[0].Kind != AgentDown {
		t.Fatalf("esperava agent_down: %+v %v", dets, err)
	}
	ivs := intervals(t, st)
	if got := states(ivs); !eq(got, []string{Measuring, AgentDown, Measuring}) {
		t.Fatalf("estados = %v", got)
	}
	if !ivs[0].End.Equal(lastAlive) || !ivs[1].Start.Equal(lastAlive) || ivs[1].Duration() != 20*time.Minute {
		t.Fatalf("lacuna deveria começar no último heartbeat: %+v", ivs)
	}
	if ivs[1].DetectedBy != "startup_open_interval" {
		t.Fatalf("detected_by = %s", ivs[1].DetectedBy)
	}
}

func TestCleanStopThenRestart(t *testing.T) {
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	tr.Start(context.Background(), Measuring)
	heartbeats(t, tr, clk, 2)
	if err := tr.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Hour)
	dets, _ := newTracker(st, dev, clk).Start(context.Background(), Measuring)
	if len(dets) != 1 || dets[0].Interval.DetectedBy != "startup_gap" || dets[0].Interval.Duration() != time.Hour {
		t.Fatalf("esperava lacuna de 1 h por agente parado: %+v", dets)
	}
}

func TestPauseResume(t *testing.T) {
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	tr.Start(context.Background(), Measuring)
	heartbeats(t, tr, clk, 1)
	tr.SetState(context.Background(), Paused, "pausa pelo usuário")
	heartbeats(t, tr, clk, 2)
	tr.SetState(context.Background(), Measuring, "retomada pelo usuário")
	if got := states(intervals(t, st)); !eq(got, []string{Measuring, Paused, Measuring}) {
		t.Fatalf("estados = %v", got)
	}
}

func TestCrashGapStartsAtLastObservation(t *testing.T) {
	ctx := context.Background()
	st, dev, clk := setup(t)
	tr := newTracker(st, dev, clk)
	tr.Start(ctx, Measuring)
	heartbeats(t, tr, clk, 2)
	pd, _ := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "ep", Host: "h", Owner: "dev"},
		store.ProbeDefinition{Kind: "tcp", Target: "h:1", Interval: time.Second, Timeout: time.Millisecond, ProtocolVersion: 1}, t0)
	obsAt := clk.Now().Add(3 * time.Second)
	st.InsertObservation(ctx, store.Observation{ID: "o1", DeviceID: dev, ProbeDefID: pd.ID, Seq: 1,
		StartedWall: obsAt, Status: "ok", ReceivedAt: obsAt})

	clk.Advance(time.Minute)
	dets, err := newTracker(st, dev, clk).Start(ctx, Measuring)
	if err != nil || len(dets) != 1 {
		t.Fatalf("%+v %v", dets, err)
	}
	if !dets[0].Interval.Start.Equal(obsAt.Truncate(time.Millisecond)) {
		t.Fatalf("lacuna começa em %s, esperava %s", dets[0].Interval.Start, obsAt)
	}
	ivs := intervals(t, st)
	if !ivs[0].End.Equal(ivs[1].Start) {
		t.Fatalf("medição anterior deveria terminar onde a lacuna começa: %+v", ivs)
	}
}
