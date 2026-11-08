package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"pinglume/agent/internal/compare"
	"pinglume/agent/internal/store"
)

type fakeAgent struct {
	mu     sync.Mutex
	calls  []string
	bodies map[string]map[string]any
	list   []experimentDTO
}

func (f *fakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := r.Method + " " + r.URL.Path
	f.calls = append(f.calls, key)
	if r.Body != nil {
		var b map[string]any
		if json.NewDecoder(r.Body).Decode(&b) == nil {
			if f.bodies == nil {
				f.bodies = map[string]map[string]any{}
			}
			f.bodies[key] = b
		}
	}
	switch {
	case key == "GET /api/v1/experiments":
		json.NewEncoder(w).Encode(f.list)
	case r.Method == "POST" && r.URL.Path == "/api/v1/experiments":
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(f.list[0])
	default:
		json.NewEncoder(w).Encode(f.list[0])
	}
}

func newFakeAgent(t *testing.T, list ...experimentDTO) (*fakeAgent, common) {
	t.Helper()
	f := &fakeAgent{list: list}
	srv := httptest.NewUnstartedServer(f)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener.Close()
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	port, _ := strconv.Atoi(strings.Split(ln.Addr().String(), ":")[1])
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "api-token"), []byte("segredo"), 0o600); err != nil {
		t.Fatal(err)
	}
	return f, common{dataDir: dir, port: port}
}

func sampleExperiment(id, phase string) experimentDTO {
	now := time.Now()
	return experimentDTO{
		Experiment: store.Experiment{ID: id, Question: "Cabo melhora?", Intervention: "cabo",
			PlannedWindow: 600000, CreatedAt: now,
			Windows: []store.Window{{Role: "baseline", Start: now, PlannedEnd: now.Add(10 * time.Minute),
				Conditions: []store.Condition{{Key: "conexão", Value: "Wi-Fi, sala", Source: "declared"}}}}},
		Phase: phase,
	}
}

func TestPickExperimentUsesLatest(t *testing.T) {
	f, c := newFakeAgent(t, sampleExperiment("nova", compare.PhaseBaseline), sampleExperiment("velha", compare.PhaseDone))
	e, err := pickExperiment(c, "")
	if err != nil || e.ID != "nova" {
		t.Fatalf("%v %v", e.ID, err)
	}
	if _, err := pickExperiment(c, "velha"); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "GET /api/v1/experiments/velha" {
		t.Fatalf("chamadas: %v", f.calls)
	}

	_, empty := newFakeAgent(t)
	if _, err := pickExperiment(empty, ""); err == nil || !strings.Contains(err.Error(), "compare new") {
		t.Fatalf("lista vazia: %v", err)
	}
}

func TestCompareNewSendsMinutesAndValidates(t *testing.T) {
	f, c := newFakeAgent(t, sampleExperiment("e1", compare.PhaseBaseline))
	common := []string{"--data-dir", c.dataDir, "--port", strconv.Itoa(c.port)}
	if err := compareNew(append(common, "--question", "Cabo melhora?", "--change", "cabo", "--window", "15m", "--condition", "Wi-Fi")); err != nil {
		t.Fatal(err)
	}
	b := f.bodies["POST /api/v1/experiments"]
	if b["windowMinutes"] != float64(15) || b["intervention"] != "cabo" || b["question"] != "Cabo melhora?" || b["condition"] != "Wi-Fi" {
		t.Fatalf("corpo: %v", b)
	}

	n := len(f.calls)
	for _, bad := range [][]string{
		{"--question", "q"},
		{"--change", "m"},
		{"--question", "q", "--change", "m", "--window", "9m"},
		{"--question", "q", "--change", "m", "--window", "61m"},
		{"--question", "q", "--change", "m", "--window", "90s"},
		{"--question", "q", "--change", "m", "--window", "10m30s"},
	} {
		if err := compareNew(append(common, bad...)); err == nil {
			t.Fatalf("deveria recusar %v", bad)
		}
	}
	if len(f.calls) != n {
		t.Fatalf("entradas inválidas não deveriam chamar o agente: %v", f.calls[n:])
	}
}

func TestCompareNextAndCancelTargetLatest(t *testing.T) {
	f, c := newFakeAgent(t, sampleExperiment("atual", compare.PhaseAwaiting), sampleExperiment("antiga", compare.PhaseDone))
	common := []string{"--data-dir", c.dataDir, "--port", strconv.Itoa(c.port)}
	if err := compareNext(append(common, "--condition", "cabo, sala")); err != nil {
		t.Fatal(err)
	}
	if f.bodies["POST /api/v1/experiments/atual/intervention"]["condition"] != "cabo, sala" {
		t.Fatalf("corpo: %v", f.bodies)
	}
	if err := compareCancel(common); err != nil {
		t.Fatal(err)
	}
	if f.calls[len(f.calls)-1] != "POST /api/v1/experiments/atual/cancel" {
		t.Fatalf("chamadas: %v", f.calls)
	}
}

func TestCompareSubcommandErrors(t *testing.T) {
	if err := cmdCompare(nil); err == nil {
		t.Fatal("sem subcomando deveria falhar")
	}
	if err := cmdCompare([]string{"xyz"}); err == nil || !strings.Contains(err.Error(), "xyz") {
		t.Fatalf("%v", err)
	}
}

func TestPrintExperimentRunningAndDone(t *testing.T) {
	var out bytes.Buffer
	printExperiment(&out, sampleExperiment("e1", compare.PhaseBaseline))
	got := out.String()
	for _, want := range []string{"Pergunta: Cabo melhora?", "janela A em andamento", "Janela A:", "faltam", "conexão: Wi-Fi, sala (declarada)"} {
		if !strings.Contains(got, want) {
			t.Fatalf("falta %q em:\n%s", want, got)
		}
	}

	done := sampleExperiment("e1", compare.PhaseDone)
	done.Windows[0].Start = time.Now().Add(-30 * time.Minute)
	done.Windows[0].PlannedEnd = time.Now().Add(-20 * time.Minute)
	done.Result = &compare.Result{RuleVersion: compare.RuleVersion, Verdict: compare.Inconclusive,
		Probes:       []compare.ProbeComparison{{Kind: "udp_echo", Endpoint: "sp-1", Verdict: compare.NotComparable}},
		Findings:     []compare.Finding{{Text: "UDP não foi comparada.", Inputs: []string{"A: 3 execuções"}}},
		Alternatives: []string{"Outras pessoas na rede."},
		NextTest:     "Repita.",
		Caveats:      []string{"A janela B veio depois da A."}}
	out.Reset()
	printExperiment(&out, done)
	got = out.String()
	for _, want := range []string{"concluída", "INCONCLUSIVO", "regra compare-v1", "UDP (sp-1): não comparável",
		"Outras explicações possíveis", "Próximo teste: Repita.", "Limites:", "A janela B veio depois da A."} {
		if !strings.Contains(got, want) {
			t.Fatalf("falta %q em:\n%s", want, got)
		}
	}
	if strings.Contains(got, "faltam") {
		t.Fatalf("janela passada não deveria mostrar tempo restante:\n%s", got)
	}
}
