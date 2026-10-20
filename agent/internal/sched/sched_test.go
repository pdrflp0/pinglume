package sched

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
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
