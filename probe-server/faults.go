package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Faults struct {
	mu sync.RWMutex
	f  FaultState
}

type FaultState struct {
	DNS        string `json:"dns"`
	HTTPS      string `json:"https"`
	HTTPSDelay string `json:"httpsDelay"`
	Listener   string `json:"listener"`
}

func defaultFaults() FaultState {
	return FaultState{DNS: "ok", HTTPS: "ok", HTTPSDelay: "0s", Listener: "open"}
}

func (s FaultState) validate() error {
	switch s.DNS {
	case "ok", "drop", "servfail", "nxdomain":
	default:
		return fmt.Errorf("dns inválido: %q", s.DNS)
	}
	switch s.HTTPS {
	case "ok", "error500", "hang":
	default:
		return fmt.Errorf("https inválido: %q", s.HTTPS)
	}
	if d, err := time.ParseDuration(s.HTTPSDelay); err != nil || d < 0 || d > time.Minute {
		return fmt.Errorf("httpsDelay inválido: %q", s.HTTPSDelay)
	}
	switch s.Listener {
	case "open", "closed":
	default:
		return fmt.Errorf("listener inválido: %q", s.Listener)
	}
	return nil
}

func (f *Faults) Get() FaultState {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.f
}

func (f *Faults) Set(s FaultState) error {
	if err := s.validate(); err != nil {
		return err
	}
	f.mu.Lock()
	f.f = s
	f.mu.Unlock()
	return nil
}

func (f *Faults) httpsDelay() time.Duration {
	d, _ := time.ParseDuration(f.Get().HTTPSDelay)
	return d
}

func adminHandler(f *Faults, onChange func(FaultState)) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /faults", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, f.Get())
	})
	mux.HandleFunc("PUT /faults", func(w http.ResponseWriter, r *http.Request) {
		next := f.Get()
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&next); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := f.Set(next); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		onChange(next)
		writeJSON(w, http.StatusOK, next)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
