// Package runner orquestra o agente: heartbeat da cobertura, agendador de
// sondas e pausa/retomada. Pausar cancela as sondas em andamento; nenhuma nova
// medição começa até a retomada.
package runner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"pinglume/agent/internal/coverage"
	"pinglume/agent/internal/netctx"
	"pinglume/agent/internal/sched"
	"pinglume/agent/internal/store"
)

const pausedKey = "paused"

type Runner struct {
	Store     *store.Store
	Tracker   *coverage.Tracker
	Scheduler *sched.Scheduler
	Jobs      []sched.Job
	Log       *slog.Logger

	// Control recebe pedidos de pausa/retomada (da API local). Cada pedido é
	// respondido em Done depois que o novo estado foi aplicado e gravado.
	Control <-chan Command
	// OnState, se definido, é chamado após cada mudança de estado (testes).
	OnState func(state string)
	// Net, se definido, é consultado a cada heartbeat; mudanças de interface
	// viram eventos net_change (fator de confusão nas comparações).
	Net netctx.Detector

	state atomic.Value // string
	net   atomic.Value // netctx.Context
}

// NetContext devolve a última interface detectada; seguro para outras goroutines.
func (r *Runner) NetContext() netctx.Context {
	v, _ := r.net.Load().(netctx.Context)
	return v
}

// checkNet detecta a interface e grava um evento quando ela muda.
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
		kind = "net_context" // estado inicial desta execução
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

// State devolve o estado atual de cobertura; seguro para outras goroutines.
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
		initial = coverage.Paused // a pausa sobrevive a reinícios
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

	ticker := time.NewTicker(r.Tracker.Heartbeat)
	defer ticker.Stop()
	for {
		select {
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
