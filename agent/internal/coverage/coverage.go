package coverage

import (
	"context"
	"fmt"
	"time"

	"pinglume/agent/internal/clock"
	"pinglume/agent/internal/store"
)

const (
	Measuring = "measuring"
	Paused    = "paused"
	Suspended = "suspended"
	AgentDown = "agent_down"
)

type Tracker struct {
	Store     *store.Store
	Clock     clock.Clock
	DeviceID  string
	Heartbeat time.Duration
	state     string
	prevWall  time.Time
	prevMono  time.Duration
}

type Detection struct {
	Kind     string
	Interval store.Interval
	Detail   string
}

func (t *Tracker) State() string { return t.state }

func (t *Tracker) Start(ctx context.Context, initial string) ([]Detection, error) {
	now := t.Clock.Now()
	var dets []Detection
	closeAt := now

	open, err := t.Store.OpenInterval(ctx, t.DeviceID)
	if err != nil {
		return nil, err
	}
	var lastAlive time.Time
	var hasLast bool
	reason, detectedBy := "agente não estava em execução", "startup_gap"
	if open != nil {
		lastAlive, hasLast = open.End, true
		if obsAt, ok, err := t.Store.LastObservationAt(ctx, t.DeviceID); err != nil {
			return nil, err
		} else if ok && obsAt.After(lastAlive) && !obsAt.After(now) {
			lastAlive = obsAt
		}
		closeAt = lastAlive
		reason, detectedBy = "encerramento inesperado do agente; o início é a última evidência de vida registrada (heartbeat ou medição)", "startup_open_interval"
	} else if lastAlive, hasLast, err = t.Store.LastIntervalEnd(ctx, t.DeviceID); err != nil {
		return nil, err
	}

	var gaps []store.Interval
	if hasLast {
		switch gap := now.Sub(lastAlive); {
		case gap > t.Heartbeat:
			iv := store.Interval{Start: lastAlive, End: now, State: AgentDown, Reason: reason, DetectedBy: detectedBy}
			gaps = append(gaps, iv)
			dets = append(dets, Detection{Kind: AgentDown, Interval: iv})
		case gap < -t.Heartbeat:
			detail := fmt.Sprintf("relógio civil está %s antes do último registro", (-gap).Round(time.Second))
			t.Store.InsertSystemEvent(ctx, now, "clock_jump", detail)
			dets = append(dets, Detection{Kind: "clock_jump", Detail: detail})
		}
	}
	startReason := ""
	if initial == Paused {
		startReason = "pausa pelo usuário, mantida após reinício do agente"
	}
	if _, err := t.Store.Transition(ctx, t.DeviceID, closeAt, gaps,
		&store.Interval{Start: now, State: initial, Reason: startReason, DetectedBy: "agent"}); err != nil {
		return nil, err
	}
	t.state, t.prevWall, t.prevMono = initial, now, t.Clock.Mono()
	return dets, nil
}

func (t *Tracker) Tick(ctx context.Context) ([]Detection, error) {
	wall, mono := t.Clock.Now(), t.Clock.Mono()
	dw, dm := wall.Sub(t.prevWall), mono-t.prevMono
	skew := dw - dm
	threshold := t.Heartbeat
	defer func() { t.prevWall, t.prevMono = wall, mono }()

	var iv store.Interval
	var det Detection
	switch {
	case skew > threshold:
		start := t.prevWall.Add(dm)
		iv = store.Interval{Start: start, End: wall, State: Suspended,
			Reason:     "relógio civil avançou sem o monotônico: computador suspenso ou relógio adiantado (indistinguíveis nesta heurística)",
			DetectedBy: "wall_mono_skew"}
		det = Detection{Kind: Suspended, Interval: iv}
		t.Store.InsertSystemEvent(ctx, wall, "sleep_detected", fmt.Sprintf("lacuna de %s", skew.Round(time.Second)))
	case skew < -threshold:
		detail := fmt.Sprintf("relógio civil voltou %s", (-skew).Round(time.Second))
		t.Store.InsertSystemEvent(ctx, wall, "clock_jump", detail)

		_, err := t.Store.Transition(ctx, t.DeviceID, t.prevWall, nil,
			&store.Interval{Start: wall, State: t.state, Reason: detail, DetectedBy: "wall_mono_skew"})
		return []Detection{{Kind: "clock_jump", Detail: detail}}, err
	case dm > 3*t.Heartbeat:
		iv = store.Interval{Start: t.prevWall.Add(t.Heartbeat), End: wall, State: AgentDown,
			Reason: "agente ficou sem executar (heartbeat atrasado)", DetectedBy: "heartbeat_late"}
		det = Detection{Kind: AgentDown, Interval: iv}
	default:
		return nil, t.Store.ExtendOpen(ctx, t.DeviceID, wall)
	}
	_, err := t.Store.Transition(ctx, t.DeviceID, iv.Start, []store.Interval{iv},
		&store.Interval{Start: wall, State: t.state, DetectedBy: "agent"})
	return []Detection{det}, err
}

func (t *Tracker) SetState(ctx context.Context, state, reason string) error {
	if state == t.state {
		return nil
	}
	now := t.Clock.Now()
	if _, err := t.Store.Transition(ctx, t.DeviceID, now, nil,
		&store.Interval{Start: now, State: state, Reason: reason, DetectedBy: "user"}); err != nil {
		return err
	}
	t.state = state
	return nil
}

func (t *Tracker) Stop(ctx context.Context) error {
	_, err := t.Store.Transition(ctx, t.DeviceID, t.Clock.Now(), nil, nil)
	return err
}
