// Package api expõe o agente em loopback para a CLI e o dashboard.
//
// Defesas, nesta ordem:
//  1. escuta só em 127.0.0.1;
//  2. Host precisa ser o próprio endereço de loopback (contra DNS rebinding);
//  3. pedidos de outro site (Sec-Fetch-Site/Origin) são recusados;
//  4. toda rota /api exige token (CLI) ou sessão de navegador;
//  5. nenhum cabeçalho CORS é emitido;
//  6. nenhuma rota executa comandos, muda a rede ou aceita alvo de sonda.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pinglume/agent/internal/netctx"
	"pinglume/agent/internal/store"
	"pinglume/agent/internal/summary"
	"pinglume/agent/internal/timeline"
)

const sessionCookie = "pinglume_session"

// Controller é o que a API precisa do agente em execução.
type Controller interface {
	State() string
	SetPaused(ctx context.Context, paused bool) error
	NetContext() netctx.Context
}

type Server struct {
	Store   *store.Store
	Ctl     Controller
	Token   string
	Port    int
	Version string
	Static  fs.FS // build do dashboard; pode estar vazio
	// DeviceID identifica esta instalação nos relatos gravados.
	DeviceID string
	Now      func() time.Time

	sess *sessions
}

func (s *Server) Handler() http.Handler {
	if s.Now == nil {
		s.Now = time.Now
	}
	s.sess = newSessions(s.Now)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth", s.handleAuth)
	mux.Handle("POST /api/v1/auth/codes", s.require(tokenOnly, s.handleNewCode))
	mux.Handle("POST /api/v1/auth/logout", s.require(anyAuth, s.handleLogout))
	mux.Handle("GET /api/v1/status", s.require(anyAuth, s.handleStatus))
	mux.Handle("GET /api/v1/timeline", s.require(anyAuth, s.handleTimeline))
	mux.Handle("GET /api/v1/incidents", s.require(anyAuth, s.handleListIncidents))
	mux.Handle("POST /api/v1/incidents", s.require(anyAuth, s.handleCreateIncident))
	mux.Handle("GET /api/v1/incidents/{id}", s.require(anyAuth, s.handleGetIncident))
	mux.Handle("DELETE /api/v1/incidents/{id}", s.require(anyAuth, s.handleDeleteIncident))
	mux.Handle("GET /api/v1/network", s.require(anyAuth, s.handleNetwork))
	mux.Handle("GET /api/v1/experiments", s.require(anyAuth, s.handleListExperiments))
	mux.Handle("POST /api/v1/experiments", s.require(anyAuth, s.handleCreateExperiment))
	mux.Handle("GET /api/v1/experiments/{id}", s.require(anyAuth, s.handleGetExperiment))
	mux.Handle("POST /api/v1/experiments/{id}/intervention", s.require(anyAuth, s.handleStartIntervention))
	mux.Handle("POST /api/v1/experiments/{id}/cancel", s.require(anyAuth, s.handleCancelExperiment))
	mux.Handle("GET /api/v1/exports/preview", s.require(anyAuth, s.handleExportPreview))
	mux.Handle("GET /api/v1/exports", s.require(anyAuth, s.handleListExports))
	mux.Handle("POST /api/v1/exports", s.require(anyAuth, s.handleCreateExport))
	mux.Handle("GET /api/v1/exports/{id}/download", s.require(anyAuth, s.handleDownloadExport))
	mux.Handle("POST /api/v1/pause", s.require(anyAuth, s.handlePause(true)))
	mux.Handle("POST /api/v1/resume", s.require(anyAuth, s.handlePause(false)))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "rota inexistente")
	})
	mux.Handle("/", s.staticHandler())
	return s.securityHeaders(s.hostCheck(mux))
}

// Listen abre o socket somente em loopback.
func (s *Server) Listen() (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(s.Port)))
}

func (s *Server) allowedHost(host string) bool {
	p := strconv.Itoa(s.Port)
	switch host {
	case "127.0.0.1:" + p, "localhost:" + p, "[::1]:" + p:
		return true
	}
	return false
}

func (s *Server) hostCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedHost(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, "host não permitido")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; "+
				"connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/auth" {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

type authMode int

const (
	tokenOnly authMode = iota
	anyAuth
)

// require aplica autenticação e a política de origem a uma rota da API.
func (s *Server) require(mode authMode, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		self := "http://" + r.Host
		// Navegadores modernos dizem de onde vem o pedido. Qualquer coisa
		// que não seja a própria origem é recusada, inclusive leituras.
		if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeError(w, http.StatusForbidden, "pedido de outra origem")
			return
		}
		origin := r.Header.Get("Origin")
		if origin != "" && origin != self {
			writeError(w, http.StatusForbidden, "origem não permitida")
			return
		}

		viaToken := false
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			if !equalSecret(strings.TrimPrefix(auth, "Bearer "), s.Token) {
				writeError(w, http.StatusUnauthorized, "token inválido")
				return
			}
			viaToken = true
		}
		if !viaToken {
			if mode == tokenOnly {
				writeError(w, http.StatusUnauthorized, "esta rota exige o token da CLI")
				return
			}
			c, err := r.Cookie(sessionCookie)
			if err != nil || !s.sess.valid(c.Value) {
				writeError(w, http.StatusUnauthorized, "sessão ausente ou expirada; rode `pinglume open`")
				return
			}
			// Mudança de estado por cookie exige Origin explícito igual ao nosso.
			if r.Method != http.MethodGet && r.Method != http.MethodHead && origin != self {
				writeError(w, http.StatusForbidden, "origem ausente")
				return
			}
		}
		h(w, r)
	})
}

func (s *Server) handleNewCode(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"code": s.sess.newCode(), "expiresInSeconds": int(s.sess.codeTTL.Seconds())})
}

// handleAuth troca o código de uso único por cookie e redireciona para "/",
// tirando o código da barra de endereço e do histórico.
func (s *Server) handleAuth(w http.ResponseWriter, r *http.Request) {
	id, ok := s.sess.redeem(r.URL.Query().Get("code"))
	if !ok {
		writeError(w, http.StatusUnauthorized, "código inválido, expirado ou já usado; rode `pinglume open` de novo")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: id, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode,
		MaxAge: int(s.sess.sessTTL.Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.sess.revoke(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	w.WriteHeader(http.StatusNoContent)
}

type statusResponse struct {
	AgentVersion string          `json:"agentVersion"`
	State        string          `json:"state"`
	Now          time.Time       `json:"now"`
	Summary      summary.Summary `json:"summary"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.parseRange(w, r, 15*time.Minute, time.Minute)
	if !ok {
		return
	}
	sum, err := summary.Build(r.Context(), s.Store, from, to, summary.Options{LiveUntil: s.Now()})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler o histórico")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{AgentVersion: s.Version, State: s.Ctl.State(), Now: s.Now(), Summary: sum})
}

// parseRange lê window=<duração> (relativo a agora) ou from/to (RFC 3339).
// O fim nunca passa de agora: o futuro não tem medição nem lacuna.
func (s *Server) parseRange(w http.ResponseWriter, r *http.Request, def, minSpan time.Duration) (time.Time, time.Time, bool) {
	q := r.URL.Query()
	now := s.Now()
	from, to := now.Add(-def), now
	if v := q.Get("window"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "window deve ser uma duração, ex.: 15m")
			return from, to, false
		}
		from = now.Add(-d)
	}
	for _, p := range []struct {
		name string
		dst  *time.Time
	}{{"from", &from}, {"to", &to}} {
		if v := q.Get(p.name); v != "" {
			t, err := time.Parse(time.RFC3339Nano, v)
			if err != nil {
				writeError(w, http.StatusBadRequest, p.name+" deve estar em RFC 3339")
				return from, to, false
			}
			*p.dst = t
		}
	}
	if to.After(now) {
		to = now
	}
	if span := to.Sub(from); span < minSpan || span > 8*24*time.Hour {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("o intervalo deve ter entre %s e 8 dias", minSpan))
		return from, to, false
	}
	return from, to, true
}

// handleTimeline devolve a série agregada do intervalo pedido (padrão: 15 min).
func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	from, to, ok := s.parseRange(w, r, 15*time.Minute, 10*time.Second)
	if !ok {
		return
	}
	n := 300
	if v := r.URL.Query().Get("buckets"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil || n < 10 || n > 1000 {
			writeError(w, http.StatusBadRequest, "buckets deve estar entre 10 e 1000")
			return
		}
	}
	tl, err := timeline.Build(r.Context(), s.Store, from, to, n, s.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "falha ao ler o histórico")
		return
	}
	writeJSON(w, http.StatusOK, tl)
}

func (s *Server) handlePause(pause bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.Ctl.SetPaused(ctx, pause); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, context.DeadlineExceeded) {
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, "o agente não aplicou a mudança: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"state": s.Ctl.State()})
	}
}

// staticHandler serve o dashboard; caminhos sem arquivo caem no index.html
// (roteamento no cliente).
func (s *Server) staticHandler() http.Handler {
	files := http.FileServer(http.FS(s.Static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "método não permitido")
			return
		}
		if s.Static == nil {
			http.Error(w, "dashboard não compilado: rode `make web`", http.StatusNotFound)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if _, err := fs.Stat(s.Static, p); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(s.Static, "index.html")
		if err != nil {
			http.Error(w, "dashboard não compilado: rode `make web`", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Write(index)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
