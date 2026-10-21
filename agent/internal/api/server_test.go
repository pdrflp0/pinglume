package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"pinglume/agent/internal/store"
)

const port = 47821

var host = "127.0.0.1:47821"

type fakeCtl struct {
	mu     sync.Mutex
	paused bool
}

func (f *fakeCtl) State() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.paused {
		return "paused"
	}
	return "measuring"
}

func (f *fakeCtl) SetPaused(_ context.Context, p bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paused = p
	return nil
}

type env struct {
	h     http.Handler
	token string
	ctl   *fakeCtl
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	ctl := &fakeCtl{}
	s := &Server{Store: st, Ctl: ctl, Token: strings.Repeat("a", 64), Port: port, Version: "t",
		Static: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Pinglume</title>")},
			"assets/app.js": {Data: []byte("console.log(1)")}}}
	return &env{h: s.Handler(), token: s.Token, ctl: ctl}
}

type reqOpt func(*http.Request)

func withHost(h string) reqOpt         { return func(r *http.Request) { r.Host = h } }
func withHeader(k, v string) reqOpt    { return func(r *http.Request) { r.Header.Set(k, v) } }
func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }

func (e *env) do(method, path string, opts ...reqOpt) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, nil)
	r.Host = host
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) bearer() reqOpt { return withHeader("Authorization", "Bearer "+e.token) }

// login faz o fluxo completo: código pela CLI → /auth → cookie.
func (e *env) login(t *testing.T) *http.Cookie {
	t.Helper()
	w := e.do("POST", "/api/v1/auth/codes", e.bearer())
	if w.Code != 200 {
		t.Fatalf("código: %d %s", w.Code, w.Body)
	}
	var body struct{ Code string }
	json.Unmarshal(w.Body.Bytes(), &body)
	w = e.do("GET", "/auth?code="+body.Code)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("troca do código: %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie de sessão inseguro: %+v", cookies)
	}
	return cookies[0]
}

func TestHostValidation(t *testing.T) {
	e := newEnv(t)
	for _, h := range []string{"evil.example:47821", "127.0.0.1:1", "localhost", "pinglume.attacker.test:47821"} {
		if w := e.do("GET", "/api/v1/status", withHost(h), e.bearer()); w.Code != http.StatusMisdirectedRequest {
			t.Errorf("Host %q: esperava 421, veio %d", h, w.Code)
		}
		if w := e.do("GET", "/", withHost(h)); w.Code != http.StatusMisdirectedRequest {
			t.Errorf("Host %q no dashboard: esperava 421, veio %d", h, w.Code)
		}
	}
	for _, h := range []string{"127.0.0.1:47821", "localhost:47821", "[::1]:47821"} {
		if w := e.do("GET", "/api/v1/status", withHost(h), e.bearer()); w.Code != 200 {
			t.Errorf("Host %q: esperava 200, veio %d", h, w.Code)
		}
	}
}

func TestAuthRequired(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/api/v1/status"); w.Code != http.StatusUnauthorized {
		t.Fatalf("sem credencial: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/status", withHeader("Authorization", "Bearer errado")); w.Code != http.StatusUnauthorized {
		t.Fatalf("token errado: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/status", withCookie(&http.Cookie{Name: sessionCookie, Value: "inventada"})); w.Code != http.StatusUnauthorized {
		t.Fatalf("sessão inventada: %d", w.Code)
	}
	// Só a CLI (token) pode emitir códigos: uma sessão de navegador não.
	c := e.login(t)
	if w := e.do("POST", "/api/v1/auth/codes", withCookie(c), withHeader("Origin", "http://"+host)); w.Code != http.StatusUnauthorized {
		t.Fatalf("sessão emitindo código: %d", w.Code)
	}
}

func TestCodeIsSingleUse(t *testing.T) {
	e := newEnv(t)
	w := e.do("POST", "/api/v1/auth/codes", e.bearer())
	var body struct{ Code string }
	json.Unmarshal(w.Body.Bytes(), &body)
	if w := e.do("GET", "/auth?code="+body.Code); w.Code != http.StatusSeeOther {
		t.Fatalf("primeiro uso: %d", w.Code)
	}
	if w := e.do("GET", "/auth?code="+body.Code); w.Code != http.StatusUnauthorized {
		t.Fatalf("segundo uso deveria falhar: %d", w.Code)
	}
}

func TestCodeExpires(t *testing.T) {
	e := newEnv(t)
	now := time.Now()
	st, _ := store.Open(context.Background(), filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	s := &Server{Store: st, Ctl: &fakeCtl{}, Token: e.token, Port: port, Now: func() time.Time { return now }}
	h := s.Handler()
	code := s.sess.newCode()
	now = now.Add(61 * time.Second)
	r := httptest.NewRequest("GET", "/auth?code="+code, nil)
	r.Host = host
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("código expirado aceito: %d", w.Code)
	}
}

func TestCrossOriginRejected(t *testing.T) {
	e := newEnv(t)
	c := e.login(t)
	cases := []struct {
		name string
		opts []reqOpt
	}{
		{"Origin estranho", []reqOpt{withCookie(c), withHeader("Origin", "https://evil.example")}},
		{"Sec-Fetch-Site cross-site", []reqOpt{withCookie(c), withHeader("Sec-Fetch-Site", "cross-site")}},
		{"Sec-Fetch-Site same-site", []reqOpt{withCookie(c), withHeader("Sec-Fetch-Site", "same-site")}},
		{"POST por cookie sem Origin", []reqOpt{withCookie(c)}},
		{"token com Origin estranho", []reqOpt{e.bearer(), withHeader("Origin", "https://evil.example")}},
	}
	for _, tc := range cases {
		if w := e.do("POST", "/api/v1/pause", tc.opts...); w.Code != http.StatusForbidden {
			t.Errorf("%s: esperava 403, veio %d", tc.name, w.Code)
		}
	}
	if e.ctl.State() != "measuring" {
		t.Fatal("algum pedido recusado chegou a pausar o agente")
	}
	// Leitura de outro site também é recusada.
	if w := e.do("GET", "/api/v1/status", withCookie(c), withHeader("Sec-Fetch-Site", "cross-site")); w.Code != http.StatusForbidden {
		t.Fatalf("leitura cross-site: %d", w.Code)
	}
}

func TestSameOriginPauseResume(t *testing.T) {
	e := newEnv(t)
	c := e.login(t)
	same := []reqOpt{withCookie(c), withHeader("Origin", "http://"+host), withHeader("Sec-Fetch-Site", "same-origin")}
	if w := e.do("POST", "/api/v1/pause", same...); w.Code != 200 || e.ctl.State() != "paused" {
		t.Fatalf("pausa: %d %s", w.Code, w.Body)
	}
	if w := e.do("POST", "/api/v1/resume", e.bearer()); w.Code != 200 || e.ctl.State() != "measuring" {
		t.Fatalf("retomada pela CLI: %d %s", w.Code, w.Body)
	}
}

func TestNoCORSAndSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	for _, w := range []*httptest.ResponseRecorder{
		e.do("GET", "/api/v1/status", e.bearer(), withHeader("Origin", "http://"+host)),
		e.do("OPTIONS", "/api/v1/pause", withHeader("Origin", "https://evil.example"),
			withHeader("Access-Control-Request-Method", "POST")),
		e.do("GET", "/"),
	} {
		for k := range w.Header() {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Errorf("cabeçalho CORS emitido: %s", k)
			}
		}
		if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
			t.Error("CSP ausente")
		}
	}
}

func TestStatusShape(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/api/v1/status?window=1h", e.bearer())
	var got statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.State != "measuring" {
		t.Fatalf("status: %v %s", err, w.Body)
	}
	if got.Summary.StateMS["unrecorded"] != time.Hour.Milliseconds() {
		t.Fatalf("banco vazio deveria ser 1 h sem registro: %+v", got.Summary.StateMS)
	}
	if w := e.do("GET", "/api/v1/status?window=30d", e.bearer()); w.Code != http.StatusBadRequest {
		t.Fatalf("janela inválida: %d", w.Code)
	}
}

func TestSPAFallbackAndAssets(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/qualquer/rota"); w.Code != 200 || !strings.Contains(w.Body.String(), "<title>Pinglume") {
		t.Fatalf("fallback do SPA: %d", w.Code)
	}
	if w := e.do("GET", "/assets/app.js"); w.Code != 200 || w.Body.String() != "console.log(1)" {
		t.Fatalf("asset: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/nada", e.bearer()); w.Code != 404 || !strings.Contains(w.Header().Get("Content-Type"), "json") {
		t.Fatalf("rota de API inexistente deve ser 404 JSON, não o SPA: %d", w.Code)
	}
}

func TestTokenFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "token")
	a, err := LoadOrCreateToken(p)
	if err != nil || len(a) != 64 {
		t.Fatalf("criação: %q %v", a, err)
	}
	fi, _ := os.Stat(p)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("permissão = %v", fi.Mode().Perm())
	}
	b, _ := LoadOrCreateToken(p)
	if a != b {
		t.Fatal("token deveria persistir")
	}
	os.Chmod(p, 0o644)
	if _, err := LoadOrCreateToken(p); err == nil {
		t.Fatal("token legível por outros usuários deveria ser recusado")
	}
}
