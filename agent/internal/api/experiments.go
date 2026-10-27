package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pinglume/agent/internal/compare"
	"pinglume/agent/internal/netctx"
	"pinglume/agent/internal/store"
)

var expMu sync.Mutex

type experimentView struct {
	store.Experiment
	Phase  string          `json:"phase"`
	Result *compare.Result `json:"result"`
}

func (s *Server) view(r *http.Request, e store.Experiment) (experimentView, error) {
	now := s.Now()
	res, err := compare.Load(r.Context(), s.Store, e, now)
	return experimentView{Experiment: e, Phase: compare.Phase(e, now), Result: res}, err
}

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Ctl.NetContext())
}

func cleanText(v string, max int) (string, bool) {
	v = strings.TrimSpace(v)
	return v, utf8.RuneCountInString(v) <= max
}

func (s *Server) conditionsFor(declared string) []store.Condition {
	conds := []store.Condition{}
	if declared != "" {
		conds = append(conds, store.Condition{Key: "conexão", Value: declared, Source: "declared"})
	}
	if nc := s.Ctl.NetContext(); nc.Kind != "" && nc.Kind != netctx.KindUnknown {
		conds = append(conds, store.Condition{Key: "interface", Value: fmt.Sprintf("%s (%s)", nc.Kind, nc.Interface),
			Source: "detected", Method: nc.Method})
	}
	return conds
}

func (s *Server) handleCreateExperiment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question      string `json:"question"`
		Intervention  string `json:"intervention"`
		WindowMinutes int    `json:"windowMinutes"`
		Condition     string `json:"condition"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo JSON inválido")
		return
	}
	q, ok1 := cleanText(req.Question, 200)
	iv, ok2 := cleanText(req.Intervention, 200)
	cond, ok3 := cleanText(req.Condition, 100)
	if q == "" || iv == "" || !ok1 || !ok2 || !ok3 {
		writeError(w, http.StatusBadRequest, "question e intervention são obrigatórios (até 200 caracteres); condition até 100")
		return
	}
	if req.WindowMinutes < int(compare.MinWindow/time.Minute) || req.WindowMinutes > 60 {
		writeError(w, http.StatusBadRequest, "windowMinutes deve estar entre 10 e 60")
		return
	}
	expMu.Lock()
	defer expMu.Unlock()
	list, err := s.Store.Experiments(r.Context(), 5)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler comparações")
		return
	}
	now := s.Now()
	for _, e := range list {
		if p := compare.Phase(e, now); p != compare.PhaseDone && p != compare.PhaseCancelled {
			writeError(w, http.StatusConflict, "já existe uma comparação em andamento; conclua ou cancele antes")
			return
		}
	}
	win := time.Duration(req.WindowMinutes) * time.Minute
	e, err := s.Store.InsertExperiment(r.Context(), s.DeviceID, store.Experiment{
		Question: q, Intervention: iv, PlannedWindow: win.Milliseconds(), CreatedAt: now})
	if err == nil {
		_, err = s.Store.AddWindow(r.Context(), e.ID, store.Window{Role: "baseline", Start: now, PlannedEnd: now.Add(win),
			Conditions: s.conditionsFor(cond)})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao criar a comparação")
		return
	}
	s.respondExperiment(w, r, e.ID, http.StatusCreated)
}

func (s *Server) respondExperiment(w http.ResponseWriter, r *http.Request, id string, status int) {
	e, err := s.Store.Experiment(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "comparação não encontrada")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler a comparação")
		return
	}
	v, err := s.view(r, e)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao calcular o resultado")
		return
	}
	writeJSON(w, status, v)
}

func (s *Server) handleGetExperiment(w http.ResponseWriter, r *http.Request) {
	s.respondExperiment(w, r, r.PathValue("id"), http.StatusOK)
}

func (s *Server) handleStartIntervention(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Condition string `json:"condition"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo JSON inválido")
		return
	}
	cond, ok := cleanText(req.Condition, 100)
	if !ok {
		writeError(w, http.StatusBadRequest, "condition até 100 caracteres")
		return
	}
	expMu.Lock()
	defer expMu.Unlock()
	e, err := s.Store.Experiment(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "comparação não encontrada")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler a comparação")
		return
	}
	now := s.Now()
	if p := compare.Phase(e, now); p != compare.PhaseAwaiting {
		writeError(w, http.StatusConflict, "a janela B só pode começar depois que a janela A terminar (etapa atual: "+p+")")
		return
	}
	win := time.Duration(e.PlannedWindow) * time.Millisecond
	if _, err := s.Store.AddWindow(r.Context(), e.ID, store.Window{Role: "intervention", Start: now, PlannedEnd: now.Add(win),
		Conditions: s.conditionsFor(cond)}); err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao iniciar a janela B")
		return
	}
	s.respondExperiment(w, r, e.ID, http.StatusOK)
}

func (s *Server) handleCancelExperiment(w http.ResponseWriter, r *http.Request) {
	expMu.Lock()
	defer expMu.Unlock()
	e, err := s.Store.Experiment(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "comparação não encontrada")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler a comparação")
		return
	}
	if p := compare.Phase(e, s.Now()); p == compare.PhaseDone || p == compare.PhaseCancelled {
		writeError(w, http.StatusConflict, "esta comparação já terminou")
		return
	}
	if err := s.Store.CancelExperiment(r.Context(), e.ID, s.Now()); err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao cancelar")
		return
	}
	s.respondExperiment(w, r, e.ID, http.StatusOK)
}

func (s *Server) handleListExperiments(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Experiments(r.Context(), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler comparações")
		return
	}
	out := make([]experimentView, 0, len(list))
	for _, e := range list {
		v, err := s.view(r, e)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "falha ao calcular resultados")
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}
