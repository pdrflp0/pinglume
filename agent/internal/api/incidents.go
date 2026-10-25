package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"pinglume/agent/internal/incident"
	"pinglume/agent/internal/store"
)

var symptoms = map[string]bool{"audio_cut": true, "video_freeze": true, "dropped_call": true, "slow": true, "other": true}

const (
	maxNoteRunes = 500
	maxReportAge = 24 * time.Hour
)

type createIncidentRequest struct {
	Symptom string `json:"symptom"`
	Note    string `json:"note"`
	// At é quando a pessoa diz que aconteceu; vazio = agora.
	At *time.Time `json:"at"`
}

type incidentResponse struct {
	Incident store.Incident    `json:"incident"`
	Analysis incident.Analysis `json:"analysis"`
}

func (s *Server) handleCreateIncident(w http.ResponseWriter, r *http.Request) {
	var req createIncidentRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "corpo JSON inválido")
		return
	}
	if !symptoms[req.Symptom] {
		writeError(w, http.StatusBadRequest, "symptom deve ser audio_cut, video_freeze, dropped_call, slow ou other")
		return
	}
	req.Note = strings.TrimSpace(req.Note)
	if utf8.RuneCountInString(req.Note) > maxNoteRunes {
		writeError(w, http.StatusBadRequest, "nota com mais de 500 caracteres")
		return
	}
	now := s.Now()
	at := now
	if req.At != nil {
		at = *req.At
		if at.After(now.Add(5*time.Second)) || now.Sub(at) > maxReportAge {
			writeError(w, http.StatusBadRequest, "o horário do relato deve estar entre as últimas 24 h e agora")
			return
		}
	}
	via := "dashboard"
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		via = "cli"
	}
	inc, err := s.Store.InsertIncident(r.Context(), s.DeviceID, store.Incident{
		ReportedAt: at, CreatedAt: now, Symptom: req.Symptom, Note: req.Note, CreatedVia: via})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao gravar o relato")
		return
	}
	s.respondIncident(w, r, inc, http.StatusCreated)
}

func (s *Server) respondIncident(w http.ResponseWriter, r *http.Request, inc store.Incident, status int) {
	a, err := incident.Load(r.Context(), s.Store, inc, s.Now(), s.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao analisar o relato")
		return
	}
	writeJSON(w, status, incidentResponse{Incident: inc, Analysis: a})
}

func (s *Server) handleGetIncident(w http.ResponseWriter, r *http.Request) {
	inc, err := s.Store.Incident(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "relato não encontrado")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler o relato")
		return
	}
	s.respondIncident(w, r, inc, http.StatusOK)
}

type incidentListItem struct {
	store.Incident
	Verdict string `json:"verdict"`
	Final   bool   `json:"final"`
}

func (s *Server) handleListIncidents(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.parseRange(w, r, 24*time.Hour, time.Minute)
	if !ok {
		return
	}
	incs, err := s.Store.Incidents(r.Context(), from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler os relatos")
		return
	}
	out := make([]incidentListItem, 0, len(incs))
	for _, inc := range incs {
		a, err := incident.Load(r.Context(), s.Store, inc, s.Now(), s.Now())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "falha ao analisar os relatos")
			return
		}
		out = append(out, incidentListItem{Incident: inc, Verdict: a.Verdict, Final: a.Final})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleDeleteIncident(w http.ResponseWriter, r *http.Request) {
	err := s.Store.DeleteIncident(r.Context(), r.PathValue("id"))
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "relato não encontrado")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao apagar o relato")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
