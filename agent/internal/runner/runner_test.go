package runner

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/coverage"
	"pinglume/agent/internal/probe"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

func total(t *testing.T, st *store.Store, since time.Time) int {
	t.Helper()
	sums, err := st.Summaries(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range sums {
		n += s.Total
	}
	return n
}

// Pausa interrompe novas medições em ≤ 1 s, persiste entre reinícios e a
// retomada volta a medir.
func TestPauseStopsMeasuringAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()

	dbPath := filepath.Join(t.TempDir(), "t.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	clk := clock.System{}
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, clk.Now())
	p := probe.TCPConnect{Addr: ln.Addr().String(), Timeout: 200 * time.Millisecond}
	def, _ := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "ep", Host: "127.0.0.1", Owner: "dev"},
		store.ProbeDefinition{Kind: "tcp", Target: p.Addr, Interval: 20 * time.Millisecond, Timeout: p.Timeout, ProtocolVersion: 1}, clk.Now())
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	newRunner := func(ctl chan Command, states chan string) *Runner {
		return &Runner{
			Store:     st,
			Tracker:   &coverage.Tracker{Store: st, Clock: clk, DeviceID: dev.ID, Heartbeat: 50 * time.Millisecond},
			Scheduler: &sched.Scheduler{Store: st, Clock: clk, DeviceID: dev.ID, Concurrency: 2, Jitter: 0.1, Log: log},
			Jobs:      []sched.Job{{Def: def, Prober: p}},
			Log:       log,
			Control:   ctl,
			OnState:   func(s string) { states <- s },
		}
	}
	waitState := func(states chan string, want string) time.Duration {
		t.Helper()
		start := time.Now()
		select {
		case got := <-states:
			if got != want {
				t.Fatalf("estado = %s, esperava %s", got, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("estado %s não chegou", want)
		}
		return time.Since(start)
	}

	ctl, states := make(chan Command), make(chan string, 4)
	send := func(pause bool) {
		t.Helper()
		done := make(chan error, 1)
		ctl <- Command{Pause: pause, Done: done}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- newRunner(ctl, states).Run(runCtx) }()
	waitState(states, coverage.Measuring)
	time.Sleep(200 * time.Millisecond)

	t0 := time.Now()
	send(true)
	waitState(states, coverage.Paused)
	if d := time.Since(t0); d > time.Second {
		t.Fatalf("pausa levou %s (limite 1 s)", d)
	}
	pausedAt := time.Now()
	time.Sleep(300 * time.Millisecond)
	if n := total(t, st, pausedAt); n != 0 {
		t.Fatalf("%d medições começaram durante a pausa", n)
	}

	// Reinício com a pausa ativa: continua pausado.
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	runCtx, cancel = context.WithCancel(ctx)
	defer cancel()
	go func() { done <- newRunner(ctl, states).Run(runCtx) }()
	waitState(states, coverage.Paused)
	time.Sleep(150 * time.Millisecond)
	if n := total(t, st, pausedAt); n != 0 {
		t.Fatalf("%d medições após reinício pausado", n)
	}

	send(false)
	waitState(states, coverage.Measuring)
	resumedAt := time.Now()
	time.Sleep(300 * time.Millisecond)
	if n := total(t, st, resumedAt); n == 0 {
		t.Fatal("retomada deveria voltar a medir")
	}

	ivs, _ := st.Intervals(ctx, pausedAt.Add(-time.Hour), time.Now().Add(time.Hour))
	sawPaused := false
	for _, iv := range ivs {
		sawPaused = sawPaused || iv.State == coverage.Paused
	}
	if !sawPaused {
		t.Fatalf("intervalo de pausa ausente: %+v", ivs)
	}
}
