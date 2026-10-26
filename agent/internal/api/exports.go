package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"pinglume/agent/internal/export"
	"pinglume/agent/internal/store"
)

// exportCSP vale para o HTML exportado servido pelo agente: sem scripts, sem
// rede; só o CSS embutido. frame-ancestors 'self' permite a prévia em iframe.
const exportCSP = "default-src 'none'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'self'; base-uri 'none'; form-action 'none'"

func validRange(from, to, now time.Time) (time.Time, error) {
	if to.After(now) {
		to = now
	}
	if span := to.Sub(from); span < time.Minute || span > 8*24*time.Hour {
		return to, errors.New("o período deve ter entre 1 min e 8 dias")
	}
	return to, nil
}

func (s *Server) handleExportPreview(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.parseRange(w, r, 24*time.Hour, time.Minute)
	if !ok {
		return
	}
	q := r.URL.Query()
	opt := export.Options{From: from, To: to, IncludeIncidents: q.Get("incidents") != "0",
		IncludeComparisons: q.Get("comparisons") != "0", IncludeNotes: q.Get("notes") == "1"}
	snap, err := export.Build(r.Context(), s.Store, opt, s.Version, s.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao montar a prévia")
		return
	}
	snap.Preview = true
	if q.Get("format") == "json" {
		writeJSON(w, http.StatusOK, snap)
		return
	}
	html, err := export.RenderHTML(snap, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao gerar o HTML")
		return
	}
	w.Header().Set("Content-Security-Policy", exportCSP)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

func (s *Server) handleCreateExport(w http.ResponseWriter, r *http.Request) {
	var opt export.Options
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&opt); err != nil {
		writeError(w, http.StatusBadRequest, "corpo JSON inválido")
		return
	}
	now := s.Now()
	to, err := validRange(opt.From, opt.To, now)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	opt.To = to
	opt.IncludeNotes = opt.IncludeNotes && opt.IncludeIncidents
	snap, err := export.Build(r.Context(), s.Store, opt, s.Version, now)
	if err == nil {
		var m store.ExportMeta
		if m, err = export.Save(r.Context(), s.Store, snap, opt); err == nil {
			writeJSON(w, http.StatusCreated, m)
			return
		}
	}
	writeError(w, http.StatusInternalServerError, "falha ao gerar a exportação")
}

func (s *Server) handleListExports(w http.ResponseWriter, r *http.Request) {
	list, err := s.Store.Exports(r.Context(), 20)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler exportações")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDownloadExport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format != "html" && format != "json" {
		writeError(w, http.StatusBadRequest, "format deve ser html ou json")
		return
	}
	m, body, err := s.Store.Export(r.Context(), r.PathValue("id"), format)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "exportação não encontrada")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler a exportação")
		return
	}
	name := fmt.Sprintf("pinglume-%s-%s.%s", m.CreatedAt.Local().Format("2006-01-02-1504"), m.ID[len(m.ID)-6:], format)
	ctype := "application/json"
	if format == "html" {
		ctype = "text/html; charset=utf-8"
		w.Header().Set("Content-Security-Policy", exportCSP)
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	w.Header().Set("X-Pinglume-SHA256", m.SHA256)
	w.Write([]byte(body))
}
