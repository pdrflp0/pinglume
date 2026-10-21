// Package backoff reconstrói, a partir dos eventos gravados pelo agendador,
// os períodos em que cada sonda rodou com intervalo ampliado. Nesses períodos
// a cobertura é menor por decisão do agente, não por falta de rede.
package backoff

import (
	"context"
	"encoding/json"
	"time"

	"pinglume/agent/internal/store"
)

type Span struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	// MaxMultiplier é o maior fator de ampliação do intervalo no período.
	MaxMultiplier int `json:"maxMultiplier"`
}

type detail struct {
	ProbeDefID string `json:"probeDefId"`
	Multiplier int    `json:"multiplier"`
}

// lookback cobre backoffs que começaram antes do período pedido. Um backoff
// mais antigo que isso, ainda aberto, aparece a partir do início do período
// só se houver eventos dentro dessa margem.
const lookback = 24 * time.Hour

// Spans devolve os períodos de backoff por definição de sonda, recortados a
// [from, to]. Um backoff sem evento de fim vai até "to".
func Spans(ctx context.Context, st *store.Store, from, to time.Time) (map[string][]Span, error) {
	evs, err := st.SystemEvents(ctx, from.Add(-lookback), to)
	if err != nil {
		return nil, err
	}
	open := map[string]*Span{}
	out := map[string][]Span{}
	closeSpan := func(id string, end time.Time) {
		sp := open[id]
		delete(open, id)
		if end.After(to) {
			end = to
		}
		if sp.Start.Before(from) {
			sp.Start = from
		}
		if end.After(sp.Start) {
			sp.End = end
			out[id] = append(out[id], *sp)
		}
	}
	for _, e := range evs {
		if e.Kind != "probe_backoff" && e.Kind != "probe_backoff_end" {
			continue
		}
		var d detail
		if json.Unmarshal([]byte(e.Detail), &d) != nil || d.ProbeDefID == "" {
			continue
		}
		sp, isOpen := open[d.ProbeDefID]
		switch {
		case e.Kind == "probe_backoff" && !isOpen:
			open[d.ProbeDefID] = &Span{Start: e.At, MaxMultiplier: d.Multiplier}
		case e.Kind == "probe_backoff" && isOpen:
			sp.MaxMultiplier = max(sp.MaxMultiplier, d.Multiplier)
		case e.Kind == "probe_backoff_end" && isOpen:
			closeSpan(d.ProbeDefID, e.At)
		}
	}
	for id := range open {
		closeSpan(id, to)
	}
	return out, nil
}

// Total soma a duração dos períodos.
func Total(spans []Span) time.Duration {
	var t time.Duration
	for _, s := range spans {
		t += s.End.Sub(s.Start)
	}
	return t
}
