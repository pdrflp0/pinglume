package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"pinglume/agent/internal/compare"
	"pinglume/agent/internal/ids"
	"pinglume/agent/internal/incident"
	"pinglume/agent/internal/store"
	"pinglume/agent/internal/summary"
)

const SchemaVersion = "pinglume-export/1"

type Options struct {
	From               time.Time `json:"from"`
	To                 time.Time `json:"to"`
	IncludeIncidents   bool      `json:"includeIncidents"`
	IncludeComparisons bool      `json:"includeComparisons"`
	IncludeNotes       bool      `json:"includeNotes"`
}

type ProbeDoc struct {
	Endpoint        string         `json:"endpoint"`
	Kind            string         `json:"kind"`
	ProtocolVersion int            `json:"protocolVersion"`
	Measures        string         `json:"measures"`
	Target          string         `json:"target"`
	IntervalMS      int64          `json:"intervalMs"`
	Total           int            `json:"total"`
	OK              int            `json:"ok"`
	Failures        map[string]int `json:"failures"`
	P50US           *int64         `json:"p50Us"`
	P95US           *int64         `json:"p95Us"`
	Coverage        *float64       `json:"coverage"`
	BackoffMS       int64          `json:"backoffMs"`
}

type IncidentDoc struct {
	ReportedAt time.Time         `json:"reportedAt"`
	Symptom    string            `json:"symptom"`
	Note       *string           `json:"note"`
	Analysis   incident.Analysis `json:"analysis"`
}

type WindowDoc struct {
	Role       string            `json:"role"`
	Start      time.Time         `json:"start"`
	End        time.Time         `json:"end"`
	Conditions []store.Condition `json:"conditions"`
}

type ComparisonDoc struct {
	Question     string          `json:"question"`
	Intervention string          `json:"intervention"`
	Windows      []WindowDoc     `json:"windows"`
	Result       *compare.Result `json:"result"`
}

type Snapshot struct {
	SchemaVersion string    `json:"schemaVersion"`
	ID            string    `json:"id"`
	GeneratedAt   time.Time `json:"generatedAt"`
	AgentVersion  string    `json:"agentVersion"`
	Preview       bool      `json:"preview"`
	Period        struct {
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
	} `json:"period"`
	Notice   []string `json:"notice"`
	Included struct {
		Incidents   bool `json:"incidents"`
		Comparisons bool `json:"comparisons"`
		Notes       bool `json:"notes"`
	} `json:"included"`
	Device struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
	} `json:"device"`
	Method      Method          `json:"method"`
	Coverage    CoverageDoc     `json:"coverage"`
	Probes      []ProbeDoc      `json:"probes"`
	Incidents   []IncidentDoc   `json:"incidents"`
	Comparisons []ComparisonDoc `json:"comparisons"`
	Redactions  []Redaction     `json:"redactions"`
}

type CoverageDoc struct {
	StateMS map[string]int64 `json:"stateMs"`
	Gaps    []summary.Gap    `json:"gaps"`
}

type Method struct {
	Coverage string            `json:"coverage"`
	Probes   map[string]string `json:"probes"`
	Rules    map[string]string `json:"rules"`
}

var measures = map[string]string{
	"tcp":   "tempo do handshake TCP até o endpoint, por IP",
	"dns":   "tempo para resolver um nome único (sem cache) na zona Pinglume",
	"https": "requisição HTTPS completa com conexão nova, por IP",
}

func method() Method {
	return Method{
		Coverage: "Intervalos em que o agente estava medindo, pausado, suspenso ou parado. Período sem medição não indica conexão boa nem queda.",
		Probes:   measures,
		Rules: map[string]string{
			incident.RuleVersion: "Relato comparado com as medições de 5 min antes a 1 min depois, contra as 2 h anteriores. Coincidência no tempo não é tratada como causa.",
			compare.RuleVersion:  "Janelas A e B com a mesma definição de sonda, ≥ 10 min e ≥ 80% de cobertura; diferença com intervalo de 95% por bootstrap em blocos e limiar prático.",
		},
	}
}

func Build(ctx context.Context, st *store.Store, opt Options, agentVersion string, now time.Time) (Snapshot, error) {
	var s Snapshot
	s.SchemaVersion, s.ID, s.GeneratedAt, s.AgentVersion = SchemaVersion, ids.New(now), now.UTC(), agentVersion
	s.Period.From, s.Period.To = opt.From.UTC(), opt.To.UTC()
	s.Notice = []string{
		"Gerado pelo Pinglume a partir de medições feitas neste computador.",
		"Um arquivo baixado não pode ser revogado: quem o receber pode guardá-lo e repassá-lo.",
		"As medições descrevem o caminho até os endpoints medidos; não provam a causa de um problema numa chamada.",
	}
	s.Method = method()
	s.Included.Incidents, s.Included.Comparisons = opt.IncludeIncidents, opt.IncludeComparisons
	s.Included.Notes = opt.IncludeIncidents && opt.IncludeNotes
	s.Probes, s.Incidents, s.Comparisons = []ProbeDoc{}, []IncidentDoc{}, []ComparisonDoc{}

	dev, err := st.Device(ctx)
	if err != nil {
		return s, err
	}
	s.Device.OS, s.Device.Arch = dev.OS, dev.Arch
	red := NewRedactor(dev.Name)

	sum, err := summary.Build(ctx, st, opt.From, opt.To, summary.Options{LiveUntil: now})
	if err != nil {
		return s, err
	}
	s.Coverage = CoverageDoc{StateMS: sum.StateMS, Gaps: sum.Gaps}
	for i := range s.Coverage.Gaps {
		s.Coverage.Gaps[i].Reason = red.Text(s.Coverage.Gaps[i].Reason)
	}
	for _, p := range sum.Probes {
		s.Probes = append(s.Probes, ProbeDoc{Endpoint: red.Text(p.Endpoint), Kind: p.Kind, ProtocolVersion: p.ProtocolVersion,
			Measures: measures[p.Kind], Target: red.Target(p.Target), IntervalMS: p.IntervalMS, Total: p.Total, OK: p.OK,
			Failures: p.Failures, P50US: p.P50US, P95US: p.P95US, Coverage: p.Coverage, BackoffMS: p.BackoffMS})
	}

	notesOmitted := 0
	if opt.IncludeIncidents {
		incs, err := st.Incidents(ctx, opt.From, opt.To)
		if err != nil {
			return s, err
		}
		for _, inc := range incs {
			a, err := incident.Load(ctx, st, inc, now, now)
			if err != nil {
				return s, err
			}
			redactAnalysis(red, &a)
			doc := IncidentDoc{ReportedAt: inc.ReportedAt.UTC(), Symptom: inc.Symptom, Analysis: a}
			if inc.Note != "" {
				if opt.IncludeNotes {
					n := red.Text(inc.Note)
					doc.Note = &n
				} else {
					notesOmitted++
				}
			}
			s.Incidents = append(s.Incidents, doc)
		}
	}

	if opt.IncludeComparisons {
		exps, err := st.Experiments(ctx, 100)
		if err != nil {
			return s, err
		}
		for _, e := range exps {
			if e.CreatedAt.Before(opt.From) || e.CreatedAt.After(opt.To) || compare.Phase(e, now) != compare.PhaseDone {
				continue
			}
			r, err := compare.Load(ctx, st, e, now)
			if err != nil {
				return s, err
			}
			redactResult(red, r)
			doc := ComparisonDoc{Question: red.Text(e.Question), Intervention: red.Text(e.Intervention), Result: r}
			for _, w := range e.Windows {
				wd := WindowDoc{Role: w.Role, Start: w.Start.UTC(), End: w.PlannedEnd.UTC(), Conditions: []store.Condition{}}
				if w.End != nil {
					wd.End = w.End.UTC()
				}
				for _, c := range w.Conditions {
					c.Value = red.Text(c.Value)
					wd.Conditions = append(wd.Conditions, c)
				}
				doc.Windows = append(doc.Windows, wd)
			}
			s.Comparisons = append(s.Comparisons, doc)
		}
	}

	s.Redactions = red.Report(opt.IncludeNotes, notesOmitted)
	return s, nil
}

func redactAnalysis(red *Redactor, a *incident.Analysis) {
	for i := range a.Findings {
		a.Findings[i].Text = red.Text(a.Findings[i].Text)
		for j := range a.Findings[i].Inputs {
			a.Findings[i].Inputs[j] = red.Text(a.Findings[i].Inputs[j])
		}
	}
	for i := range a.Alternatives {
		a.Alternatives[i] = red.Text(a.Alternatives[i])
	}
	for i := range a.Caveats {
		a.Caveats[i] = red.Text(a.Caveats[i])
	}
	a.NextTest = red.Text(a.NextTest)
	for i := range a.Probes {
		a.Probes[i].Endpoint = red.Text(a.Probes[i].Endpoint)
	}
}

func redactResult(red *Redactor, r *compare.Result) {
	if r == nil {
		return
	}
	for i := range r.Windows {
		if n := r.Windows[i].NetAtStart; n != nil {
			n.Interface = red.Iface(n.Interface)
		}
		for j := range r.Windows[i].Conditions {
			r.Windows[i].Conditions[j].Value = red.Text(r.Windows[i].Conditions[j].Value)
		}
	}
	for i := range r.Findings {
		r.Findings[i].Text = red.Text(r.Findings[i].Text)
		for j := range r.Findings[i].Inputs {
			r.Findings[i].Inputs[j] = red.Text(r.Findings[i].Inputs[j])
		}
	}
	for i := range r.Alternatives {
		r.Alternatives[i] = red.Text(r.Alternatives[i])
	}
	for i := range r.Caveats {
		r.Caveats[i] = red.Text(r.Caveats[i])
	}
	r.NextTest = red.Text(r.NextTest)
	for i := range r.Probes {
		r.Probes[i].Endpoint = red.Text(r.Probes[i].Endpoint)
	}
}

func Encode(s Snapshot) ([]byte, string, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, "", err
	}
	b = append(b, '\n')
	sum := sha256.Sum256(b)
	return b, hex.EncodeToString(sum[:]), nil
}

func Save(ctx context.Context, st *store.Store, s Snapshot, opt Options) (store.ExportMeta, error) {
	body, digest, err := Encode(s)
	if err != nil {
		return store.ExportMeta{}, err
	}
	html, err := RenderHTML(s, digest)
	if err != nil {
		return store.ExportMeta{}, err
	}
	optJSON, _ := json.Marshal(opt)
	m := store.ExportMeta{ID: s.ID, CreatedAt: s.GeneratedAt, From: opt.From, To: opt.To, OptionsJSON: string(optJSON),
		SchemaVersion: SchemaVersion, AgentVersion: s.AgentVersion, SHA256: digest}
	if err := st.InsertExport(ctx, m, string(body), html); err != nil {
		return m, fmt.Errorf("gravar exportação: %w", err)
	}
	return m, nil
}
