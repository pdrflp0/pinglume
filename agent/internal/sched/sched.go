// Package sched agenda as sondas com concorrência limitada e grava cada
// resultado. Cada definição de sonda tem uma única goroutine, o que mantém a
// sequência (seq) monotônica sem trava.
package sched

import (
	"context"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/ids"
	"pinglume/agent/internal/probe"
	"pinglume/agent/internal/store"
)

type Job struct {
	Def    store.ProbeDefinition
	Prober probe.Prober
}

type Scheduler struct {
	Store       *store.Store
	Clock       clock.Clock
	DeviceID    string
	Concurrency int     // sondas simultâneas no máximo
	Jitter      float64 // fração do intervalo, ex.: 0.1 = ±10%
	Log         *slog.Logger
}

// Run bloqueia até ctx ser cancelado e todas as sondas em andamento terminarem.
func (s *Scheduler) Run(ctx context.Context, jobs []Job) error {
	sem := make(chan struct{}, max(1, s.Concurrency))
	var wg sync.WaitGroup
	for _, j := range jobs {
		seq, err := s.Store.NextSeq(ctx, j.Def.ID)
		if err != nil {
			return err
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.loop(ctx, j, seq, sem)
		}()
	}
	wg.Wait()
	return nil
}

func (s *Scheduler) loop(ctx context.Context, j Job, seq int64, sem chan struct{}) {
	// Primeiro disparo espalhado dentro do intervalo para não sincronizar sondas.
	wait := time.Duration(rand.Float64() * float64(j.Def.Interval))
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		select {
		case <-ctx.Done():
			return
		case sem <- struct{}{}:
		}
		startedWall := s.Clock.Now()
		res := j.Prober.Probe(ctx)
		<-sem

		obs := store.Observation{
			ID:          ids.New(startedWall),
			DeviceID:    s.DeviceID,
			ProbeDefID:  j.Def.ID,
			Seq:         seq,
			StartedWall: startedWall.UTC(),
			Duration:    res.Duration,
			Status:      res.Status,
			ErrorDetail: res.ErrorDetail,
			ReceivedAt:  s.Clock.Now().UTC(),
		}
		// A gravação usa contexto próprio e curto: uma sonda cancelada no
		// desligamento ainda fica registrada como 'cancelled'.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		err := s.Store.InsertObservation(writeCtx, obs)
		cancel()
		if err != nil {
			s.Log.Error("falha ao gravar observação", "probe", j.Def.ID, "seq", seq, "err", err)
		} else {
			seq++
		}

		wait = s.nextWait(j.Def.Interval, res.Duration)
	}
}

// nextWait aplica jitter e desconta o tempo gasto na própria sonda.
func (s *Scheduler) nextWait(interval, spent time.Duration) time.Duration {
	j := 1 + s.Jitter*(2*rand.Float64()-1)
	w := time.Duration(float64(interval)*j) - spent
	return max(w, 0)
}
