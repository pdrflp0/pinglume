package summary

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"pinglume/agent/internal/store"
)

func TestOpenIntervalLiveVsOffline(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	t0 := time.Date(2026, 10, 20, 21, 0, 0, 0, time.UTC)
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, t0)
	st.Transition(ctx, dev.ID, t0, nil, &store.Interval{Start: t0, State: "paused"})
	st.ExtendOpen(ctx, dev.ID, t0.Add(5*time.Second))

	now := t0.Add(8 * time.Second)
	from := t0.Add(-time.Minute)

	offline, _ := Build(ctx, st, from, now, Options{})
	if got := offline.StateMS["paused"]; got != 5000 {
		t.Fatalf("de fora, pausa deveria ir até o heartbeat (5 s), veio %d ms", got)
	}
	if got := offline.StateMS[Unrecorded]; got != 63000 {
		t.Fatalf("sem registro = %d ms", got)
	}

	live, _ := Build(ctx, st, from, now, Options{LiveUntil: now})
	if got := live.StateMS["paused"]; got != 8000 {
		t.Fatalf("agente vivo: pausa deveria ir até agora (8 s), veio %d ms", got)
	}
	if len(live.Gaps) != 1 || !live.Gaps[0].End.Equal(now) {
		t.Fatalf("lacuna de pausa deveria terminar agora: %+v", live.Gaps)
	}
}
