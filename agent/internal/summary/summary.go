// Package summary monta o resumo de cobertura e sondas de um período. É a
// mesma visão para a CLI (lendo o banco) e para a API local (dashboard).
package summary

import (
	"context"
	"sort"
	"time"

	"pinglume/agent/internal/store"
)

type Gap struct {
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
	State  string    `json:"state"`
	Reason string    `json:"reason"`
}

type Probe struct {
	Endpoint        string         `json:"endpoint"`
	Kind            string         `json:"kind"`
	ProtocolVersion int            `json:"protocolVersion"`
	Target          string         `json:"target"`
	IntervalMS      int64          `json:"intervalMs"`
	Total           int            `json:"total"`
	OK              int            `json:"ok"`
	Failures        map[string]int `json:"failures"`
	// Percentis só das execuções ok, em microssegundos; nil sem execuções ok.
	P50US *int64 `json:"p50Us"`
	P95US *int64 `json:"p95Us"`
	// Coverage = execuções concluídas ÷ esperadas no tempo medindo; nil se
	// o tempo medindo é curto demais para esperar ao menos uma execução.
	Coverage    *float64  `json:"coverage"`
	LastStarted time.Time `json:"lastStarted"`
}

type Summary struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	// Tempo em cada estado de cobertura, em ms. "unrecorded" é o tempo sem
	// nenhum intervalo registrado: nem medição, nem lacuna conhecida.
	StateMS map[string]int64 `json:"stateMs"`
	Gaps    []Gap            `json:"gaps"`
	Probes  []Probe          `json:"probes"`
}

const Unrecorded = "unrecorded"

// Options ajusta como o intervalo aberto (o atual) é contado.
type Options struct {
	// LiveUntil, se não for zero, estende o intervalo aberto até esse instante.
	// Só o agente em execução pode afirmar isso (a API passa "agora"). Quem lê
	// o banco de fora (CLI) deixa zero: o intervalo vai até o último heartbeat,
	// porque o agente pode ter morrido depois dele.
	LiveUntil time.Time
}

func Build(ctx context.Context, st *store.Store, from, to time.Time, opt Options) (Summary, error) {
	s := Summary{From: from, To: to, StateMS: map[string]int64{}, Gaps: []Gap{}, Probes: []Probe{}}
	ivs, err := st.Intervals(ctx, from, to)
	if err != nil {
		return s, err
	}
	ivs = store.ExtendLive(ivs, opt.LiveUntil, to)
	var covered, measuring time.Duration
	for _, iv := range ivs {
		d := iv.Duration()
		s.StateMS[iv.State] += d.Milliseconds()
		covered += d
		if iv.State == "measuring" {
			measuring += d
		} else {
			s.Gaps = append(s.Gaps, Gap{Start: iv.Start, End: iv.End, State: iv.State, Reason: iv.Reason})
		}
	}
	if u := to.Sub(from) - covered; u > 0 {
		s.StateMS[Unrecorded] = u.Milliseconds()
	}

	sums, err := st.Summaries(ctx, from)
	if err != nil {
		return s, err
	}
	for _, ps := range sums {
		p := Probe{
			Endpoint: ps.EndpointLabel, Kind: ps.Kind, ProtocolVersion: ps.ProtocolVersion, Target: ps.Target,
			IntervalMS: ps.Interval.Milliseconds(), Total: ps.Total, OK: ps.ByStatus["ok"],
			Failures: map[string]int{}, LastStarted: ps.LastStarted,
			P50US: percentile(ps.OKDurations, 0.50), P95US: percentile(ps.OKDurations, 0.95),
		}
		for k, v := range ps.ByStatus {
			if k != "ok" {
				p.Failures[k] = v
			}
		}
		if ps.Interval > 0 && measuring >= ps.Interval {
			expected := float64(measuring) / float64(ps.Interval)
			c := min(1, float64(ps.Total-ps.ByStatus["cancelled"])/expected)
			p.Coverage = &c
		}
		s.Probes = append(s.Probes, p)
	}
	sort.SliceStable(s.Probes, func(i, j int) bool { return s.Probes[i].Endpoint < s.Probes[j].Endpoint })
	return s, nil
}

// percentile usa o posto mais próximo sobre durações já ordenadas.
func percentile(sorted []time.Duration, p float64) *int64 {
	if len(sorted) == 0 {
		return nil
	}
	v := sorted[int(float64(len(sorted)-1)*p+0.5)].Microseconds()
	return &v
}
