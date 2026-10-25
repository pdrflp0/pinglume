package incident

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pinglume/agent/internal/store"
)

var tInc = time.Date(2026, 10, 21, 20, 0, 0, 0, time.UTC)

type fixture struct {
	t    *testing.T
	st   *store.Store
	dev  string
	defs map[string]string // kind → id
	seq  map[string]int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "t", AgentVersion: "t"}, tInc)
	f := &fixture{t: t, st: st, dev: dev.ID, defs: map[string]string{}, seq: map[string]int64{}}
	for _, k := range []string{"tcp", "dns", "https"} {
		d, err := st.EnsureProbeDefinition(ctx, store.Endpoint{Label: "ep-1", Host: "127.0.0.1", Owner: "dev"},
			store.ProbeDefinition{Kind: k, Target: k, Interval: 10 * time.Second, Timeout: time.Second, ProtocolVersion: 1}, tInc)
		if err != nil {
			t.Fatal(err)
		}
		f.defs[k] = d.ID
	}
	return f
}

// measuring registra cobertura "medindo" em [a, b].
func (f *fixture) measuring(a, b time.Time) {
	ctx := context.Background()
	f.st.Transition(ctx, f.dev, a, nil, &store.Interval{Start: a, State: "measuring"})
	f.st.ExtendOpen(ctx, f.dev, b)
	f.st.Transition(ctx, f.dev, b, nil, nil)
}

// obs grava uma observação a cada 10 s em [a, b) para o tipo dado.
func (f *fixture) obs(kind string, a, b time.Time, status string, dur time.Duration) {
	ctx := context.Background()
	for t := a; t.Before(b); t = t.Add(10 * time.Second) {
		f.seq[kind]++
		err := f.st.InsertObservation(ctx, store.Observation{
			ID: fmt.Sprintf("%s-%d", kind, f.seq[kind]), DeviceID: f.dev, ProbeDefID: f.defs[kind],
			Seq: f.seq[kind], StartedWall: t, Duration: dur, Status: status, ReceivedAt: t})
		if err != nil {
			f.t.Fatal(err)
		}
	}
}

func (f *fixture) analyze(now time.Time) Analysis {
	f.t.Helper()
	a, err := Analyze(context.Background(), f.st, store.Incident{ID: "i", ReportedAt: tInc}, now, time.Time{})
	if err != nil {
		f.t.Fatal(err)
	}
	return a
}

var (
	baseFrom = tInc.Add(-Before - BaselineSpan)
	winFrom  = tInc.Add(-Before)
	winTo    = tInc.Add(After)
	later    = tInc.Add(10 * time.Minute)
)

// normal grava cobertura e observações ok para as três sondas em todo o período.
func (f *fixture) normal(until time.Time) {
	f.measuring(baseFrom, until)
	for _, k := range []string{"tcp", "dns", "https"} {
		f.obs(k, baseFrom, winFrom, "ok", time.Millisecond)
	}
}

func allText(a Analysis) string {
	var b strings.Builder
	for _, x := range a.Findings {
		b.WriteString(x.Text + " ")
	}
	for _, x := range a.Alternatives {
		b.WriteString(x + " ")
	}
	return b.String() + a.NextTest
}

func TestNoCoverage(t *testing.T) {
	f := newFixture(t)
	a := f.analyze(later)
	if a.Verdict != VerdictNoCoverage || a.MeasuringMS != 0 {
		t.Fatalf("%s %d", a.Verdict, a.MeasuringMS)
	}
}

func TestAgentStoppedDuringWindowIsNoCoverage(t *testing.T) {
	f := newFixture(t)
	f.measuring(baseFrom, winFrom.Add(2*time.Minute)) // mediu só 2 de 6 min
	f.obs("tcp", baseFrom, winFrom.Add(2*time.Minute), "ok", time.Millisecond)
	if a := f.analyze(later); a.Verdict != VerdictNoCoverage {
		t.Fatalf("veredito = %s", a.Verdict)
	}
}

func TestDegradationAllProbes(t *testing.T) {
	f := newFixture(t)
	f.normal(later)
	for _, k := range []string{"tcp", "https"} {
		f.obs(k, winFrom, winTo, "refused", time.Millisecond)
	}
	f.obs("dns", winFrom, winTo, "timeout", time.Second)
	a := f.analyze(later)
	if a.Verdict != VerdictDegradation || !a.Final {
		t.Fatalf("veredito = %s", a.Verdict)
	}
	failing := 0
	for _, p := range a.Probes {
		if p.Failing {
			failing++
		}
	}
	if failing != 3 {
		t.Fatalf("esperava 3 sondas falhando: %+v", a.Probes)
	}
	txt := allText(a)
	if !strings.Contains(txt, "conexão recusada") || !strings.Contains(txt, "não prova") {
		t.Fatalf("texto: %s", txt)
	}
	for _, fd := range a.Findings {
		if len(fd.Inputs) == 0 {
			t.Fatalf("frase sem entradas rastreáveis: %q", fd.Text)
		}
	}
}

func TestOnlyDNSFailingPointsToResolver(t *testing.T) {
	f := newFixture(t)
	f.normal(later)
	f.obs("tcp", winFrom, winTo, "ok", time.Millisecond)
	f.obs("https", winFrom, winTo, "ok", time.Millisecond)
	f.obs("dns", winFrom, winTo, "timeout", time.Second)
	a := f.analyze(later)
	if a.Verdict != VerdictDegradation {
		t.Fatalf("veredito = %s", a.Verdict)
	}
	if !strings.Contains(strings.Join(a.Alternatives, " "), "servidor DNS") {
		t.Fatalf("alternativas: %v", a.Alternatives)
	}
}

func TestNoDetectableDegradation(t *testing.T) {
	f := newFixture(t)
	f.normal(later)
	for _, k := range []string{"tcp", "dns", "https"} {
		f.obs(k, winFrom, winTo, "ok", time.Millisecond)
	}
	a := f.analyze(later)
	if a.Verdict != VerdictNoChange {
		t.Fatalf("veredito = %s", a.Verdict)
	}
	if len(a.Alternatives) == 0 || a.NextTest == "" {
		t.Fatal("sem degradação ainda precisa de alternativas e próximo teste")
	}
}

func TestSlowLatency(t *testing.T) {
	f := newFixture(t)
	f.normal(later)
	f.obs("tcp", winFrom, winTo, "ok", 50*time.Millisecond)
	f.obs("dns", winFrom, winTo, "ok", time.Millisecond)
	f.obs("https", winFrom, winTo, "ok", time.Millisecond)
	a := f.analyze(later)
	if a.Verdict != VerdictDegradation {
		t.Fatalf("veredito = %s", a.Verdict)
	}
	if !strings.Contains(allText(a), "mais lenta") {
		t.Fatalf("texto: %s", allText(a))
	}
}

// Sem linha de base suficiente, a latência não é julgada (não vira "lenta").
func TestNoBaselineNoSlowVerdict(t *testing.T) {
	f := newFixture(t)
	f.measuring(winFrom, later)
	f.obs("tcp", winFrom, winTo, "ok", 50*time.Millisecond)
	a := f.analyze(later)
	if a.Verdict != VerdictNoChange {
		t.Fatalf("veredito = %s", a.Verdict)
	}
	if !strings.Contains(strings.ToLower(strings.Join(a.Caveats, " ")), "linha de base") {
		t.Fatalf("ressalva ausente: %v", a.Caveats)
	}
}

func TestNeverBlamesTheProvider(t *testing.T) {
	f := newFixture(t)
	f.normal(later)
	for _, k := range []string{"tcp", "dns", "https"} {
		f.obs(k, winFrom, winTo, "timeout", time.Second)
	}
	txt := strings.ToLower(allText(f.analyze(later)))
	// A ressalva obrigatória nega a causalidade; o resto do texto não pode afirmá-la.
	disclaimer := "isso não prova que ela causou o problema da chamada."
	if !strings.Contains(txt, disclaimer) {
		t.Fatal("ressalva de causalidade ausente")
	}
	txt = strings.ReplaceAll(txt, disclaimer, "")
	for _, bad := range []string{"culpa", "a operadora é", "causado pela", "causou"} {
		if strings.Contains(txt, bad) {
			t.Fatalf("texto atribui causa (%q): %s", bad, txt)
		}
	}
}

func TestPendingThenFrozen(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.normal(later)
	f.obs("tcp", winFrom, tInc, "ok", time.Millisecond)
	inc, _ := f.st.InsertIncident(ctx, f.dev, store.Incident{ReportedAt: tInc, CreatedAt: tInc, Symptom: "audio_cut", CreatedVia: "cli"})

	a, err := Load(ctx, f.st, inc, tInc.Add(10*time.Second), time.Time{})
	if err != nil || a.Verdict != VerdictPending || a.Final {
		t.Fatalf("antes de fechar: %s final=%v %v", a.Verdict, a.Final, err)
	}
	if _, ok, _ := f.st.Interpretation(ctx, inc.ID, RuleVersion); ok {
		t.Fatal("interpretação provisória não deve ser gravada")
	}

	first, _ := Load(ctx, f.st, inc, later, time.Time{})
	if !first.Final {
		t.Fatal("deveria estar final")
	}
	// Dados que chegam depois (ex.: sincronização atrasada) não mudam um resultado gravado.
	f.obs("https", tInc, winTo, "timeout", time.Second)
	again, _ := Load(ctx, f.st, inc, later.Add(time.Hour), time.Time{})
	if again.Verdict != first.Verdict || len(again.Findings) != len(first.Findings) {
		t.Fatalf("interpretação gravada mudou: %s → %s", first.Verdict, again.Verdict)
	}

	if err := f.st.DeleteIncident(ctx, inc.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.st.Interpretation(ctx, inc.ID, RuleVersion); ok {
		t.Fatal("apagar o relato deveria apagar a interpretação")
	}
}

// Falha medida é evidência mesmo com cobertura parcial: vira degradação com
// ressalva, e não "sem cobertura".
func TestPartialCoverageWithFailuresIsDegradation(t *testing.T) {
	f := newFixture(t)
	start := winFrom.Add(3*time.Minute + 10*time.Second) // agente começou no meio da janela
	f.measuring(start, later)
	f.obs("tcp", start, tInc, "ok", time.Millisecond)
	f.obs("tcp", tInc, winTo, "refused", time.Millisecond)
	a := f.analyze(later)
	if a.Verdict != VerdictDegradation {
		t.Fatalf("veredito = %s", a.Verdict)
	}
	if !strings.Contains(strings.Join(a.Caveats, " "), "Cobertura parcial") {
		t.Fatalf("ressalva de cobertura ausente: %v", a.Caveats)
	}
}
