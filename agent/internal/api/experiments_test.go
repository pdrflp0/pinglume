package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pinglume/agent/internal/store"
)

type clockEnv struct {
	mu  sync.Mutex
	now time.Time
	h   http.Handler
	tok string
}

func (c *clockEnv) advance(d time.Duration) { c.mu.Lock(); c.now = c.now.Add(d); c.mu.Unlock() }

func newClockEnv(t *testing.T) *clockEnv {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	c := &clockEnv{now: time.Date(2026, 10, 24, 20, 0, 0, 0, time.UTC), tok: strings.Repeat("b", 64)}
	dev, _ := st.EnsureDevice(context.Background(), store.Device{Name: "t", AgentVersion: "t"}, c.now)
	s := &Server{Store: st, Ctl: &fakeCtl{}, Token: c.tok, Port: port, DeviceID: dev.ID,
		Now: func() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }}
	c.h = s.Handler()
	return c
}

func (c *clockEnv) call(method, path, body string) (int, map[string]any) {
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Host = host
	r.Header.Set("Authorization", "Bearer "+c.tok)
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	var out map[string]any
	json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestExperimentFlow(t *testing.T) {
	c := newClockEnv(t)
	code, e := c.call("POST", "/api/v1/experiments",
		`{"question":"Cabo melhora?","intervention":"cabo","windowMinutes":10,"condition":"Wi-Fi, quarto"}`)
	if code != http.StatusCreated || e["phase"] != "baseline_running" {
		t.Fatalf("criar: %d %v", code, e)
	}
	id := e["id"].(string)
	conds := e["windows"].([]any)[0].(map[string]any)["conditions"].([]any)
	if len(conds) != 2 {
		t.Fatalf("esperava condição declarada e detectada: %v", conds)
	}
	sources := conds[0].(map[string]any)["source"].(string) + "," + conds[1].(map[string]any)["source"].(string)
	if !strings.Contains(sources, "declared") || !strings.Contains(sources, "detected") {
		t.Fatalf("origens: %s", sources)
	}

	if code, _ := c.call("POST", "/api/v1/experiments", `{"question":"x","intervention":"y","windowMinutes":10}`); code != http.StatusConflict {
		t.Fatalf("segunda comparação simultânea: %d", code)
	}
	if code, _ := c.call("POST", "/api/v1/experiments/"+id+"/intervention", `{"condition":"cabo"}`); code != http.StatusConflict {
		t.Fatalf("janela B antes de A terminar: %d", code)
	}

	c.advance(10*time.Minute + time.Second)
	if _, e := c.call("GET", "/api/v1/experiments/"+id, ""); e["phase"] != "awaiting_intervention" {
		t.Fatalf("fase: %v", e["phase"])
	}
	if code, e := c.call("POST", "/api/v1/experiments/"+id+"/intervention", `{"condition":"cabo"}`); code != 200 || e["phase"] != "intervention_running" {
		t.Fatalf("iniciar B: %d %v", code, e["phase"])
	}
	c.advance(10*time.Minute + time.Second)
	_, e = c.call("GET", "/api/v1/experiments/"+id, "")
	res, _ := e["result"].(map[string]any)
	// Sem medições no teste: as sondas não rodaram, então nada é comparável.
	if e["phase"] != "done" || res == nil || res["verdict"] != "nao_comparavel" {
		t.Fatalf("resultado: %v %v", e["phase"], res)
	}
	if code, _ := c.call("POST", "/api/v1/experiments/"+id+"/cancel", ""); code != http.StatusConflict {
		t.Fatalf("cancelar terminada: %d", code)
	}
}

func TestExperimentCancelAndValidation(t *testing.T) {
	c := newClockEnv(t)
	for _, body := range []string{
		`{"question":"q","intervention":"i","windowMinutes":5}`,
		`{"question":"q","intervention":"i","windowMinutes":90}`,
		`{"question":"","intervention":"i","windowMinutes":10}`,
		`{"question":"q","intervention":"` + strings.Repeat("x", 201) + `","windowMinutes":10}`,
	} {
		if code, _ := c.call("POST", "/api/v1/experiments", body); code != http.StatusBadRequest {
			t.Errorf("%.50s: %d", body, code)
		}
	}
	_, e := c.call("POST", "/api/v1/experiments", `{"question":"q","intervention":"i","windowMinutes":10}`)
	id := e["id"].(string)
	c.advance(3 * time.Minute)
	if code, e := c.call("POST", "/api/v1/experiments/"+id+"/cancel", ""); code != 200 || e["phase"] != "cancelled" {
		t.Fatalf("cancelar: %d %v", code, e["phase"])
	}
	// Cancelada, libera nova comparação.
	if code, _ := c.call("POST", "/api/v1/experiments", `{"question":"q","intervention":"i","windowMinutes":10}`); code != http.StatusCreated {
		t.Fatalf("nova depois de cancelar: %d", code)
	}
}
