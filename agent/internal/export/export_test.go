package export

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"pinglume/agent/internal/store"
	"pinglume/agent/internal/summary"
)

var t0 = time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)

const sensitiveNote = "ligação com fulano@exemplo.com, tel +55 11 98765-4321, roteador 192.168.1.1 " +
	"aa:bb:cc:dd:ee:ff no MacBook de Fulano Silva <script>alert(1)</script>"

var leaks = []string{"Fulano", "fulano@", "98765", "192.168.1", "10.0.0.7", "aa:bb:cc", "en0", "<script>alert"}

func seed(t *testing.T) *store.Store {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	dev, _ := st.EnsureDevice(ctx, store.Device{Name: "MacBook-de-Fulano-Silva.local", OS: "darwin", Arch: "arm64", AgentVersion: "t"}, t0)
	st.Transition(ctx, dev.ID, t0, nil, &store.Interval{Start: t0.Add(-3 * time.Hour), State: "measuring"})
	st.ExtendOpen(ctx, dev.ID, t0.Add(2*time.Hour))
	st.InsertSystemEvent(ctx, t0.Add(-3*time.Hour), "net_context", `{"interface":"en0","kind":"wifi","method":"teste"}`)

	ep := store.Endpoint{Label: "ep-1", Host: "x", Owner: "dev"}
	local, _ := st.EnsureProbeDefinition(ctx, ep, store.ProbeDefinition{Kind: "tcp", Target: "127.0.0.1:9443",
		Interval: 10 * time.Second, Timeout: time.Second, ProtocolVersion: 1}, t0)
	private, _ := st.EnsureProbeDefinition(ctx, ep, store.ProbeDefinition{Kind: "https", Target: "https://192.168.1.50:9443/p",
		Interval: 10 * time.Second, Timeout: time.Second, ProtocolVersion: 1}, t0)
	seq := int64(0)
	for ts := t0.Add(-2*time.Hour - 10*time.Minute); ts.Before(t0.Add(40 * time.Minute)); ts = ts.Add(10 * time.Second) {
		for _, d := range []string{local.ID, private.ID} {
			seq++
			status := "ok"
			if ts.After(t0.Add(-2*time.Minute)) && ts.Before(t0.Add(time.Minute)) {
				status = "refused"
			}
			st.InsertObservation(ctx, store.Observation{ID: fmt.Sprint(seq), DeviceID: dev.ID, ProbeDefID: d, Seq: seq,
				StartedWall: ts, Duration: 2 * time.Millisecond, Status: status,
				ErrorDetail: "dial tcp 192.168.1.50:9443: connect: connection refused", ReceivedAt: ts})
		}
	}
	st.InsertIncident(ctx, dev.ID, store.Incident{ReportedAt: t0, CreatedAt: t0, Symptom: "audio_cut", Note: sensitiveNote, CreatedVia: "dashboard"})

	e, _ := st.InsertExperiment(ctx, dev.ID, store.Experiment{Question: "Cabo na casa do Fulano melhora?", Intervention: "cabo",
		PlannedWindow: 600000, CreatedAt: t0.Add(10 * time.Minute)})
	st.AddWindow(ctx, e.ID, store.Window{Role: "baseline", Start: t0.Add(10 * time.Minute), PlannedEnd: t0.Add(20 * time.Minute),
		Conditions: []store.Condition{{Key: "conexão", Value: "Wi-Fi da casa 10.0.0.7", Source: "declared"},
			{Key: "interface", Value: "wifi (en0)", Source: "detected", Method: "teste"}}})
	st.AddWindow(ctx, e.ID, store.Window{Role: "intervention", Start: t0.Add(25 * time.Minute), PlannedEnd: t0.Add(35 * time.Minute),
		Conditions: []store.Condition{{Key: "interface", Value: "wifi (en0)", Source: "detected", Method: "teste"}}})
	return st
}

var now = t0.Add(time.Hour)

func build(t *testing.T, st *store.Store, notes bool) (Snapshot, string, string) {
	t.Helper()
	opt := Options{From: t0.Add(-time.Hour), To: now, IncludeIncidents: true, IncludeComparisons: true, IncludeNotes: notes}
	s, err := Build(context.Background(), st, opt, "0.3.0-test", now)
	if err != nil {
		t.Fatal(err)
	}
	body, digest, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	html, err := RenderHTML(s, digest)
	if err != nil {
		t.Fatal(err)
	}
	return s, string(body), html
}

func TestDefaultExportHasNoKnownIdentifiers(t *testing.T) {
	st := seed(t)
	s, js, html := build(t, st, false)
	if len(s.Incidents) != 1 || len(s.Comparisons) != 1 || len(s.Probes) != 2 {
		t.Fatalf("conteúdo incompleto: %d relatos, %d comparações, %d sondas", len(s.Incidents), len(s.Comparisons), len(s.Probes))
	}
	for _, out := range []struct{ name, body string }{{"JSON", js}, {"HTML", html}} {
		for _, leak := range leaks {
			if strings.Contains(out.body, leak) {
				t.Errorf("%s contém %q", out.name, leak)
			}
		}
		if strings.Contains(out.body, "connection refused") || strings.Contains(out.body, "dial tcp") {
			t.Errorf("%s contém detalhe de erro bruto", out.name)
		}
	}
	if s.Incidents[0].Note != nil {
		t.Fatal("observação deveria ser omitida por padrão")
	}

	if !strings.Contains(js, "incident-v1") || !strings.Contains(js, "compare-v1") || !strings.Contains(html, "conexão recusada") {
		t.Fatal("exportação perdeu método ou resultados")
	}
	if !strings.Contains(js, "[endereço privado]") || !strings.Contains(js, "interface-1") {
		t.Fatal("alvo privado e interface deveriam aparecer substituídos")
	}
}

func TestNotesIncludedAreSanitizedAndEscaped(t *testing.T) {
	st := seed(t)
	s, js, html := build(t, st, true)
	n := s.Incidents[0].Note
	if n == nil {
		t.Fatal("observação pedida não veio")
	}
	for _, want := range []string{"[e-mail]", "[número]", "[endereço IP]", "[endereço MAC]", "[dispositivo]"} {
		if !strings.Contains(*n, want) {
			t.Errorf("observação sem %s: %q", want, *n)
		}
	}
	for _, leak := range leaks[:7] {
		if strings.Contains(js, leak) || strings.Contains(html, leak) {
			t.Errorf("vazou %q com observações incluídas", leak)
		}
	}
	if strings.Contains(html, "<script>") || !strings.Contains(html, "&lt;script&gt;") {
		t.Fatal("HTML da observação não foi escapado")
	}
}

func TestHTMLIsSelfContained(t *testing.T) {
	st := seed(t)
	_, _, html := build(t, st, true)
	for _, re := range []string{`(?i)<script`, `(?i)<link`, `(?i)@import`, `(?i)(src|href)\s*=\s*["']?(https?:)?//`, `(?i)url\(\s*['"]?https?:`} {
		if regexp.MustCompile(re).MatchString(html) {
			t.Errorf("HTML referencia recurso externo ou script (%s)", re)
		}
	}
}

func TestTimesAreNotRedacted(t *testing.T) {
	r := NewRedactor("Mac-de-Fulano")
	in := "A janela fecha às 21:40:43; mediana 12,34 ms; 120 execuções; loopback 127.0.0.1:9443"
	if got := r.Text(in); got != in {
		t.Fatalf("texto sem identificadores foi alterado:\n%s\n%s", in, got)
	}
	if got := r.Text("fe80::1 e 2001:db8:0:0:0:0:0:1"); strings.Contains(got, "fe80") || strings.Contains(got, "2001") {
		t.Fatalf("IPv6 não redigido: %s", got)
	}
}

func TestSaveIsImmutableAndHashed(t *testing.T) {
	ctx := context.Background()
	st := seed(t)
	opt := Options{From: t0.Add(-time.Hour), To: now, IncludeIncidents: true}
	s, _ := Build(ctx, st, opt, "t", now)
	m, err := Save(ctx, st, s, opt)
	if err != nil {
		t.Fatal(err)
	}
	_, body, err := st.Export(ctx, m.ID, "json")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(body))
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		t.Fatal("SHA-256 gravado não confere com o JSON")
	}
	_, html, _ := st.Export(ctx, m.ID, "html")
	if !strings.Contains(html, m.SHA256) || strings.Contains(html, "PRÉVIA") {
		t.Fatal("HTML gerado deveria citar o hash do JSON e não ser marcado como prévia")
	}
	if strings.Contains(html, "Comparações guiadas") {
		t.Fatal("seção não pedida apareceu")
	}

	if _, err := Save(ctx, st, s, opt); err == nil {
		t.Fatal("snapshot existente não pode ser sobrescrito")
	}
}

func TestOwnerNameFromHostname(t *testing.T) {
	cases := map[string][]string{
		"MacBook-de-Fulano-Silva.local": {"Fulano Silva", "Fulano", "Silva"},
		"MacBook-Pro-do-Ze":             {"Ze"},
		"Fulanas-MacBook-Air":           {"Fulana", "Fulana"},
		"servidor-01":                   nil,
	}
	for host, want := range cases {
		got := ownerNames(host)
		if len(got) != len(want) || (len(want) > 0 && got[0] != want[0]) {
			t.Errorf("%s: %v, esperava %v", host, got, want)
		}
	}
	r := NewRedactor("MacBook-de-Fulano-Silva.local")
	if got := r.Text("casa do fulano, sala da Silvana"); got != "casa do [nome], sala da Silvana" {
		t.Fatalf("redação por nome: %q", got)
	}
}

func TestPhoneWithParenthesis(t *testing.T) {
	r := NewRedactor("")
	if got := r.Text("fone (11) 91234-5678, ok"); got != "fone [número], ok" {
		t.Fatalf("%q", got)
	}
}

func TestGapEndShowsDateWhenDayChanges(t *testing.T) {
	var s Snapshot
	start := time.Date(2026, 10, 24, 22, 2, 16, 0, time.Local)
	s.Coverage = CoverageDoc{StateMS: map[string]int64{"agent_down": 1}, Gaps: []summary.Gap{{Start: start, End: start.Add(25 * time.Hour), State: "agent_down"}}}
	html, err := RenderHTML(s, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "25/10/2026 23:02:16") {
		t.Fatal("fim de lacuna em outro dia precisa mostrar a data")
	}
}
