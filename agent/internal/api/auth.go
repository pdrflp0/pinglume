package api

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// LoadOrCreateToken lê o token da API (256 bits) ou cria um novo com
// permissão 0600. Quem lê este arquivo controla o agente; por isso ele fica
// no diretório de dados do usuário e recusamos permissões mais abertas.
func LoadOrCreateToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err == nil {
		fi, err := os.Stat(path)
		if err != nil {
			return "", err
		}
		if fi.Mode().Perm()&0o077 != 0 {
			return "", fmt.Errorf("%s tem permissão %v; esperado 0600", path, fi.Mode().Perm())
		}
		tok := strings.TrimSpace(string(b))
		if len(tok) != 64 {
			return "", errors.New("arquivo de token inválido")
		}
		return tok, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	tok := randomHex(32)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = f.WriteString(tok + "\n")
	return tok, err
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // sem aleatoriedade segura não há autenticação possível
	}
	return hex.EncodeToString(b)
}

func equalSecret(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// sessions guarda códigos de uso único e sessões do navegador em memória.
// Reiniciar o agente invalida tudo; basta rodar `pinglume open` de novo.
type sessions struct {
	mu       sync.Mutex
	now      func() time.Time
	codeTTL  time.Duration
	sessTTL  time.Duration
	codes    map[string]time.Time
	sessions map[string]time.Time
}

func newSessions(now func() time.Time) *sessions {
	return &sessions{
		now: now, codeTTL: 60 * time.Second, sessTTL: 12 * time.Hour,
		codes: map[string]time.Time{}, sessions: map[string]time.Time{},
	}
}

func (s *sessions) newCode() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gc()
	c := randomHex(16)
	s.codes[c] = s.now().Add(s.codeTTL)
	return c
}

// redeem troca um código válido por uma sessão. O código é apagado mesmo
// quando expirado: nunca serve duas vezes.
func (s *sessions) redeem(code string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.codes[code]
	delete(s.codes, code)
	if !ok || s.now().After(exp) {
		return "", false
	}
	id := randomHex(32)
	s.sessions[id] = s.now().Add(s.sessTTL)
	return id, true
}

func (s *sessions) valid(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.sessions[id]
	if ok && s.now().After(exp) {
		delete(s.sessions, id)
		return false
	}
	return ok
}

func (s *sessions) revoke(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func (s *sessions) gc() {
	now := s.now()
	for k, exp := range s.codes {
		if now.After(exp) {
			delete(s.codes, k)
		}
	}
	for k, exp := range s.sessions {
		if now.After(exp) {
			delete(s.sessions, k)
		}
	}
}
