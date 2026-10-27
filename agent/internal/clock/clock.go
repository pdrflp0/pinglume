package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
	Mono() time.Duration
}

type System struct{}

var processStart = time.Now()

func (System) Now() time.Time      { return time.Now().Round(0) }
func (System) Mono() time.Duration { return time.Since(processStart) }

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
