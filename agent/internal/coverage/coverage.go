// Package coverage mantém os intervalos que dizem se o agente estava medindo.
// Ausência de medição nunca vira "internet ok" nem "queda": vira um intervalo
// com estado e motivo explícitos.
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

	state    string
	prevWall time.Time
	prevMono time.Duration
}

// Detection descreve uma lacuna ou anomalia encontrada; serve para log e testes.
type Detection struct {
	Kind     string // agent_down | suspended | clock_jump
	Interval store.Interval
	Detail   string
}

func (t *Tracker) State() string { return t.state }

// Start abre a cobertura desta execução e registra a lacuna desde a anterior.
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
		// Intervalo ainda aberto: a execução anterior morreu sem encerrar.
		lastAlive, hasLast = open.End, true
		closeAt = open.End
		reason, detectedBy = "encerramento inesperado do agente; último heartbeat registrado é o limite conhecido", "startup_open_interval"
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
	if _, err := t.Store.Transition(ctx, t.DeviceID, closeAt, gaps,
		&store.Interval{Start: now, State: initial, DetectedBy: "agent"}); err != nil {
		return nil, err
	}
	t.state, t.prevWall, t.prevMono = initial, now, t.Clock.Mono()
	return dets, nil
}

// Tick é chamado a cada heartbeat. Compara quanto andaram o relógio civil e o
// monotônico desde o último tick:
//   - civil andou bem mais que o monotônico: o Mac dormiu (ou o relógio foi
//     adiantado — esta heurística não distingue os dois, e o motivo diz isso);
//   - civil andou para trás: ajuste de relógio, registrado como evento;
//   - monotônico andou muito mais que o heartbeat: o agente ficou sem executar.
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
		// Fecha no último instante conhecido e reabre no novo tempo civil.
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

// SetState registra uma mudança pedida (ex.: pausa do usuário).
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

// Stop fecha a cobertura num encerramento normal.
func (t *Tracker) Stop(ctx context.Context) error {
	_, err := t.Store.Transition(ctx, t.DeviceID, t.Clock.Now(), nil, nil)
	return err
}
