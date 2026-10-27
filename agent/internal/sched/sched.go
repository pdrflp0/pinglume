package sched

import (
	"context"
	"encoding/json"
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
	Store        *store.Store
	Clock        clock.Clock
	DeviceID     string
	Concurrency  int
	Jitter       float64
	Log          *slog.Logger
	BackoffAfter int
	BackoffMax   int
	OnWriteError func(error)
}

type BackoffDetail struct {
	ProbeDefID string `json:"probeDefId"`
	Multiplier int    `json:"multiplier"`
	IntervalMS int64  `json:"intervalMs"`
	Reason     string `json:"reason,omitempty"`
}

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
	after, maxMult := s.BackoffAfter, s.BackoffMax
	if after <= 0 {
		after = 3
	}
	if maxMult <= 0 {
		maxMult = 8
	}
	fails, mult := 0, 1
	setMult := func(m int, reason string) {
		if m == mult {
			return
		}
		kind := "probe_backoff"
		if m == 1 {
			kind = "probe_backoff_end"
		}
		d, _ := json.Marshal(BackoffDetail{ProbeDefID: j.Def.ID, Multiplier: m,
			IntervalMS: (j.Def.Interval * time.Duration(m)).Milliseconds(), Reason: reason})
		wctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		s.Store.InsertSystemEvent(wctx, s.Clock.Now(), kind, string(d))
		cancel()
		mult = m
	}
	defer func() {
		if mult > 1 {
			setMult(1, "agente parou ou pausou")
		}
	}()

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
		if len(res.Phases) > 0 {
			b, _ := json.Marshal(res.Phases)
			obs.PhasesJSON = string(b)
		}

		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		err := s.Store.InsertObservation(writeCtx, obs)
		cancel()
		if err != nil {
			s.Log.Error("falha ao gravar observação", "probe", j.Def.ID, "seq", seq, "err", err)
			if s.OnWriteError != nil {
				s.OnWriteError(err)
			}
		} else {
			seq++
		}

		switch res.Status {
		case "ok":
			fails = 0
			setMult(1, "sonda voltou a responder")
		case "cancelled":

		default:
			fails++
			if fails >= after {
				setMult(min(maxMult, 1<<(fails-after+1)), "falhas seguidas: "+res.Status)
			}
		}
		wait = s.nextWait(j.Def.Interval*time.Duration(mult), res.Duration)
	}
}

func (s *Scheduler) nextWait(interval, spent time.Duration) time.Duration {
	j := 1 + s.Jitter*(2*rand.Float64()-1)
	w := time.Duration(float64(interval)*j) - spent
	return max(w, 0)
}
