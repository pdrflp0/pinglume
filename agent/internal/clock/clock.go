// Package clock separa o tempo civil (alinhamento com eventos) do tempo
// monotônico (durações). Testes injetam um relógio falso para simular sono,
// saltos de relógio e passagem de dias sem esperar.
package clock

import (
	"sync"
	"time"
)

type Clock interface {
	// Now devolve só o tempo civil, sem leitura monotônica embutida, para que
	// Sub entre dois valores reflita o relógio de parede.
	Now() time.Time
	// Mono devolve o tempo monotônico desde um ponto arbitrário. No macOS ele
	// não avança enquanto o computador dorme, o que permite detectar suspensão.
	Mono() time.Duration
}

type System struct{}

var processStart = time.Now()

func (System) Now() time.Time      { return time.Now().Round(0) }
func (System) Mono() time.Duration { return time.Since(processStart) }

// Fake é um relógio controlado por testes. Advance move os dois relógios
// juntos; Sleep move só o civil (como o sono do Mac); SetWall ajusta o civil.
type Fake struct {
	mu   sync.Mutex
	wall time.Time
	mono time.Duration
}

func NewFake(wall time.Time) *Fake { return &Fake{wall: wall.Round(0)} }

func (f *Fake) Now() time.Time      { f.mu.Lock(); defer f.mu.Unlock(); return f.wall }
func (f *Fake) Mono() time.Duration { f.mu.Lock(); defer f.mu.Unlock(); return f.mono }

func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wall = f.wall.Add(d)
	f.mono += d
}

func (f *Fake) Sleep(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wall = f.wall.Add(d)
}

func (f *Fake) SetWall(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.wall = t.Round(0)
}
