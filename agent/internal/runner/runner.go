package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"pinglume/agent/internal/coverage"
	"pinglume/agent/internal/netctx"
	"pinglume/agent/internal/retention"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

const pausedKey = "paused"

type Runner struct {
	Store         *store.Store
	Tracker       *coverage.Tracker
	Scheduler     *sched.Scheduler
	Jobs          []sched.Job
	Log           *slog.Logger
	Control       <-chan Command
	OnState       func(state string)
	Net           netctx.Detector
	Maintainer    *retention.Maintainer
	MaintainEvery time.Duration
	state         atomic.Value
	net           atomic.Value
}

func (r *Runner) NetContext() netctx.Context {
	v, _ := r.net.Load().(netctx.Context)
	return v
}

func (r *Runner) checkNet(ctx context.Context, first bool) {
	if r.Net == nil {
		return
	}
	cur := r.Net.Detect(ctx)
	prev, had := r.net.Load().(netctx.Context)
	if had && prev.Equal(cur) {
		return
	}
	r.net.Store(cur)
	kind := "net_change"
	if first {
		kind = "net_context"
	}
	d, _ := json.Marshal(cur)
	r.Store.InsertSystemEvent(ctx, r.Tracker.Clock.Now(), kind, string(d))
	if !first {
		r.Log.Warn("rede mudou", "interface", cur.Interface, "tipo", cur.Kind)
	}
}

type Command struct {
	Pause bool
	Done  chan<- error
}

func (r *Runner) State() string {
	v, _ := r.state.Load().(string)
	return v
}

func (r *Runner) Run(ctx context.Context) error {
	v, _, err := r.Store.GetState(ctx, pausedKey)
	if err != nil {
		return err
	}
	initial := coverage.Measuring
	if v == "1" {
		initial = coverage.Paused
	}
	dets, err := r.Tracker.Start(ctx, initial)
	if err != nil {
		return err
	}
	r.logDetections(dets)
	r.notify(initial)
	r.checkNet(ctx, true)

	var stopSched func()
	start := func() {
		sctx, cancel := context.WithCancel(ctx)
		done := make(chan struct{})
		go func() {
			defer close(done)
			if err := r.Scheduler.Run(sctx, r.Jobs); err != nil {
				r.Log.Error("agendador", "err", err)
			}
		}()
		stopSched = func() { cancel(); <-done; stopSched = nil }
	}
	if initial == coverage.Measuring {
		start()
	} else {
		r.Log.Info("agente iniciou pausado; use `pinglume resume` para medir")
	}

	writeErr := make(chan error, 1)
	r.Scheduler.OnWriteError = func(err error) {
		if store.IsDiskFull(err) {
			select {
			case writeErr <- err:
			default:
			}
		}
	}
	var maint sync.WaitGroup
	if r.Maintainer != nil {
		maint.Add(1)
		go func() {
			defer maint.Done()
			r.maintain(ctx)
		}()
	}
	defer maint.Wait()

	var diskFull, pendingGap bool
	var fullAt time.Time
	const fullReason = "disco cheio: medições suspensas até haver espaço"

	ticker := time.NewTicker(r.Tracker.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-writeErr:
			if diskFull {
				continue
			}
			diskFull, fullAt = true, r.Tracker.Clock.Now()
			if stopSched != nil {
				stopSched()
			}
			if err := r.Tracker.SetState(ctx, coverage.ProbeUnavailable, fullReason); err != nil {
				pendingGap = true
			}
			r.notify(coverage.ProbeUnavailable)
			r.Log.Error("disco cheio: medições suspensas")
			continue

		case <-ctx.Done():
			if stopSched != nil {
				stopSched()
			}
			stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			defer cancel()
			return r.Tracker.Stop(stopCtx)

		case <-ticker.C:
			dets, err := r.Tracker.Tick(ctx)
			if err != nil {
				r.Log.Error("heartbeat", "err", err)
			}
			r.logDetections(dets)
			r.checkNet(ctx, false)
			if diskFull && r.Store.SetState(ctx, "disk_check", r.Tracker.Clock.Now().Format(time.RFC3339)) == nil {
				var err error
				if pendingGap {
					err = r.Tracker.Recover(ctx, fullAt, fullReason)
				} else {
					err = r.Tracker.SetState(ctx, coverage.Measuring, "espaço em disco disponível de novo")
				}
				if err == nil {
					diskFull, pendingGap = false, false
					r.Store.InsertSystemEvent(ctx, r.Tracker.Clock.Now(), "disk_full_recovered", "")
					r.notify(coverage.Measuring)
					start()
					r.Log.Info("espaço em disco disponível; medições retomadas")
				}
			}

		case cmd := <-r.Control:
			cmd.Done <- r.apply(ctx, cmd.Pause, start, &stopSched)
		}
	}
}

func (r *Runner) apply(ctx context.Context, pause bool, start func(), stopSched *func()) error {
	paused := r.Tracker.State() == coverage.Paused
	if pause == paused {
		return nil
	}
	if pause {
		if *stopSched != nil {
			(*stopSched)()
		}
		if err := r.setPaused(ctx, true); err != nil {
			return err
		}
		r.Log.Info("medição pausada")
		return nil
	}
	if err := r.setPaused(ctx, false); err != nil {
		return err
	}
	start()
	r.Log.Info("medição retomada")
	return nil
}

func (r *Runner) setPaused(ctx context.Context, paused bool) error {
	state, reason, flag := coverage.Measuring, "retomada pelo usuário", "0"
	if paused {
		state, reason, flag = coverage.Paused, "pausa pelo usuário", "1"
	}
	if err := r.Tracker.SetState(ctx, state, reason); err != nil {
		return err
	}
	if err := r.Store.SetState(ctx, pausedKey, flag); err != nil {
		return err
	}
	r.notify(state)
	return nil
}

func (r *Runner) notify(state string) {
	r.state.Store(state)
	if r.OnState != nil {
		r.OnState(state)
	}
}

func (r *Runner) logDetections(dets []coverage.Detection) {
	for _, d := range dets {
		if d.Detail != "" {
			r.Log.Warn("cobertura", "tipo", d.Kind, "detalhe", d.Detail)
			continue
		}
		r.Log.Warn("cobertura: lacuna registrada", "tipo", d.Kind,
			"de", d.Interval.Start.Local().Format(time.TimeOnly),
			"até", d.Interval.End.Local().Format(time.TimeOnly),
			"motivo", d.Interval.Reason)
	}
}

func (r *Runner) maintain(ctx context.Context) {
	every := r.MaintainEvery
	if every <= 0 {
		every = 10 * time.Minute
	}
	for {
		rep, err := r.Maintainer.Run(ctx)
		switch {
		case err != nil && ctx.Err() == nil:
			r.Log.Error("manutenção do armazenamento", "err", err)
		case len(rep.Compacted) > 0 || rep.DroppedRows > 0 || rep.LimitReached:
			r.Log.Info("armazenamento mantido", "compactações", len(rep.Compacted), "minutos_descartados", rep.DroppedRows,
				"uso_mb", rep.UsageAfter.UsedBytes>>20, "limite_atingido", rep.LimitReached)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}
