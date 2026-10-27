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

	"pinglume/agent/internal/netctx"
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

func (f *fakeCtl) NetContext() netctx.Context {
	return netctx.Context{Interface: "en0", Kind: netctx.KindWiFi, Method: "teste"}
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
	dev, _ := st.EnsureDevice(context.Background(), store.Device{Name: "t", AgentVersion: "t"}, time.Now())
	s := &Server{Store: st, Ctl: ctl, Token: strings.Repeat("a", 64), Port: port, Version: "t", DeviceID: dev.ID,
		Static: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Pinglume</title>")},
			"assets/app.js": {Data: []byte("console.log(1)")}}}
	return &env{h: s.Handler(), token: s.Token, ctl: ctl}
}

type reqOpt func(*http.Request)

func withHost(h string) reqOpt         { return func(r *http.Request) { r.Host = h } }
func withHeader(k, v string) reqOpt    { return func(r *http.Request) { r.Header.Set(k, v) } }
func withCookie(c *http.Cookie) reqOpt { return func(r *http.Request) { r.AddCookie(c) } }

func (e *env) do(method, path string, opts ...reqOpt) *httptest.ResponseRecorder {
	return e.doBody(method, path, "", opts...)
}

func (e *env) doBody(method, path, body string, opts ...reqOpt) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
	r.Host = host
	for _, o := range opts {
		o(r)
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) bearer() reqOpt { return withHeader("Authorization", "Bearer "+e.token) }

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

func TestTimelineValidation(t *testing.T) {
	e := newEnv(t)
	if w := e.do("GET", "/api/v1/timeline"); w.Code != http.StatusUnauthorized {
		t.Fatalf("sem auth: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/timeline", e.bearer()); w.Code != 200 {
		t.Fatalf("padrão: %d %s", w.Code, w.Body)
	}
	for _, q := range []string{"?from=ontem", "?buckets=5", "?buckets=5000",
		"?from=2026-10-01T00:00:00Z&to=2026-10-20T00:00:00Z", "?from=2026-10-20T00:00:00Z&to=2026-10-20T00:00:01Z"} {
		if w := e.do("GET", "/api/v1/timeline"+q, e.bearer()); w.Code != http.StatusBadRequest {
			t.Errorf("%s: esperava 400, veio %d", q, w.Code)
		}
	}
}

func TestIncidentLifecycle(t *testing.T) {
	e := newEnv(t)
	w := e.doBody("POST", "/api/v1/incidents", `{"symptom":"audio_cut","note":"reunião das 20h"}`, e.bearer())
	if w.Code != http.StatusCreated {
		t.Fatalf("criar: %d %s", w.Code, w.Body)
	}
	var created struct {
		Incident struct{ ID, Symptom, CreatedVia, Note string }
		Analysis struct {
			Verdict     string
			RuleVersion string
			Final       bool
		}
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	if created.Incident.CreatedVia != "cli" || created.Analysis.RuleVersion != "incident-v1" || created.Analysis.Final {
		t.Fatalf("resposta: %+v", created)
	}
	if created.Analysis.Verdict != "aguardando_dados" {
		t.Fatalf("relato recém-criado deveria aguardar a janela fechar: %s", created.Analysis.Verdict)
	}

	c := e.login(t)
	w = e.doBody("POST", "/api/v1/incidents", `{"symptom":"slow"}`, withCookie(c), withHeader("Origin", "http://"+host))
	if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"createdVia":"dashboard"`) {
		t.Fatalf("criar pelo navegador: %d %s", w.Code, w.Body)
	}

	if w := e.doBody("POST", "/api/v1/incidents", `{"symptom":"slow"}`, withCookie(c), withHeader("Origin", "https://evil.example")); w.Code != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", w.Code)
	}

	w = e.do("GET", "/api/v1/incidents", e.bearer())
	var list []struct{ ID, Verdict string }
	json.Unmarshal(w.Body.Bytes(), &list)
	if len(list) != 2 {
		t.Fatalf("lista: %s", w.Body)
	}
	if w := e.do("GET", "/api/v1/incidents/"+created.Incident.ID, e.bearer()); w.Code != 200 {
		t.Fatalf("detalhe: %d", w.Code)
	}
	if w := e.do("DELETE", "/api/v1/incidents/"+created.Incident.ID, e.bearer()); w.Code != http.StatusNoContent {
		t.Fatalf("apagar: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/incidents/"+created.Incident.ID, e.bearer()); w.Code != http.StatusNotFound {
		t.Fatalf("depois de apagar: %d", w.Code)
	}
}

func TestIncidentValidation(t *testing.T) {
	e := newEnv(t)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	old := time.Now().Add(-48 * time.Hour).UTC().Format(time.RFC3339)
	for _, body := range []string{
		`{"symptom":"desconhecido"}`,
		`{"symptom":"slow","at":"` + future + `"}`,
		`{"symptom":"slow","at":"` + old + `"}`,
		`{"symptom":"slow","note":"` + strings.Repeat("x", 501) + `"}`,
		`não é json`,
	} {
		if w := e.doBody("POST", "/api/v1/incidents", body, e.bearer()); w.Code != http.StatusBadRequest {
			t.Errorf("%.40s: esperava 400, veio %d", body, w.Code)
		}
	}
}

func TestExportRoutes(t *testing.T) {
	e := newEnv(t)
	w := e.do("GET", "/api/v1/exports/preview?window=1h", e.bearer())
	if w.Code != 200 || !strings.Contains(w.Body.String(), "PRÉVIA") {
		t.Fatalf("prévia: %d", w.Code)
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'none'") || strings.Contains(csp, "script-src") {
		t.Fatalf("CSP da prévia: %s", csp)
	}
	from := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	to := time.Now().UTC().Format(time.RFC3339)
	w = e.doBody("POST", "/api/v1/exports", `{"from":"`+from+`","to":"`+to+`","includeIncidents":true}`, e.bearer())
	if w.Code != http.StatusCreated {
		t.Fatalf("criar: %d %s", w.Code, w.Body)
	}
	var m struct{ ID, SHA256 string }
	json.Unmarshal(w.Body.Bytes(), &m)
	w = e.do("GET", "/api/v1/exports/"+m.ID+"/download?format=json", e.bearer())
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || w.Header().Get("X-Pinglume-SHA256") != m.SHA256 {
		t.Fatalf("download: %d %v", w.Code, w.Header())
	}
	if w := e.do("GET", "/api/v1/exports/"+m.ID+"/download?format=pdf", e.bearer()); w.Code != http.StatusBadRequest {
		t.Fatalf("formato inválido: %d", w.Code)
	}
	if w := e.do("GET", "/api/v1/exports/"+m.ID+"/download?format=html"); w.Code != http.StatusUnauthorized {
		t.Fatalf("download sem credencial: %d", w.Code)
	}
	if w := e.doBody("POST", "/api/v1/exports", `{"from":"`+to+`","to":"`+to+`"}`, e.bearer()); w.Code != http.StatusBadRequest {
		t.Fatalf("período vazio: %d", w.Code)
	}
}
