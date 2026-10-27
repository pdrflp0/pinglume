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
		panic(err)
	}
	return hex.EncodeToString(b)
}

func equalSecret(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

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
