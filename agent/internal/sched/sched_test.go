package sched

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/probe"
	"pinglume/agent/internal/store"
)

// Integração: agenda uma sonda real contra um listener local, grava no SQLite
// e confere que o cancelamento encerra rápido sem perder a sequência.
func TestRunRecordsAndStopsQuickly(t *testing.T) {
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

	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, now)
	p := probe.TCPConnect{Addr: ln.Addr().String(), Timeout: 200 * time.Millisecond}
	def, _ := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "ep", Host: "127.0.0.1", Owner: "dev"},
		store.ProbeDefinition{Kind: "tcp", Target: p.Addr, Interval: 20 * time.Millisecond, Timeout: p.Timeout, ProtocolVersion: 1}, now)

	runCtx, cancel := context.WithCancel(ctx)
	s := &Scheduler{Store: st, Clock: clock.System{}, DeviceID: dev.ID, Concurrency: 2, Jitter: 0.1,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	done := make(chan error)
	go func() { done <- s.Run(runCtx, []Job{{Def: def, Prober: p}}) }()

	time.Sleep(300 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("agendador não encerrou em 2 s após cancelamento")
	}

	sums, err := st.Summaries(ctx, now.Add(-time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0].ByStatus["ok"] < 3 {
		t.Fatalf("esperava várias observações ok, veio %+v", sums)
	}
	next, _ := st.NextSeq(ctx, def.ID)
	if int(next-1) != sums[0].Total {
		t.Fatalf("seq (%d) deveria acompanhar o total gravado (%d) sem buracos", next-1, sums[0].Total)
	}
}

type scripted struct {
	mu      sync.Mutex
	results []string
	i       int
}

func (s *scripted) Kind() string         { return "tcp" }
func (s *scripted) ProtocolVersion() int { return 1 }
func (s *scripted) Probe(context.Context) probe.Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.results[min(s.i, len(s.results)-1)]
	s.i++
	return probe.Result{Status: st, Duration: time.Millisecond}
}

// Falhas seguidas ampliam o intervalo e geram eventos; a primeira resposta ok
// encerra o backoff.
func TestBackoffEventsAndReset(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Now()
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, now)
	def, _ := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "ep", Host: "h", Owner: "dev"},
		store.ProbeDefinition{Kind: "tcp", Target: "h:1", Interval: 10 * time.Millisecond, Timeout: time.Millisecond, ProtocolVersion: 1}, now)
	// 3 falhas → ×2, 4ª → ×4, depois ok.
	p := &scripted{results: []string{"refused", "refused", "refused", "refused", "ok"}}
	s := &Scheduler{Store: st, Clock: clock.System{}, DeviceID: dev.ID, Concurrency: 1,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	runCtx, cancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer cancel()
	s.Run(runCtx, []Job{{Def: def, Prober: p}})

	evs, _ := st.SystemEvents(ctx, now.Add(-time.Minute), time.Now().Add(time.Minute))
	var kinds []string
	var mults []int
	for _, e := range evs {
		var d BackoffDetail
		json.Unmarshal([]byte(e.Detail), &d)
		kinds = append(kinds, e.Kind)
		mults = append(mults, d.Multiplier)
		if d.ProbeDefID != def.ID {
			t.Fatalf("evento sem a sonda: %+v", e)
		}
	}
	want := []string{"probe_backoff", "probe_backoff", "probe_backoff_end"}
	if len(kinds) != 3 || kinds[0] != want[0] || kinds[1] != want[1] || kinds[2] != want[2] {
		t.Fatalf("eventos = %v", kinds)
	}
	if mults[0] != 2 || mults[1] != 4 || mults[2] != 1 {
		t.Fatalf("multiplicadores = %v", mults)
	}
}
