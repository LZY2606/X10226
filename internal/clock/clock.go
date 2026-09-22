package clock

import (
	"sync"
	"time"
)

// Clock is the time source used by the whole system. Tests use a fixed,
// step-advancing virtual clock so event timestamps are reproducible and the
// suite never depends on wall time.
type Clock interface {
	Now() time.Time
}

// Real is the wall clock used by cmd/server outside of tests.
type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Fake is a deterministic virtual clock. Now advances by Step on every read so
// events never share a timestamp; seed/start is fixed by the caller.
type Fake struct {
	mu    sync.Mutex
	cur   time.Time
	step  time.Duration
	ticks int
}

func NewFake(start time.Time, step time.Duration) *Fake {
	if step <= 0 {
		step = time.Second
	}
	return &Fake{cur: start.UTC(), step: step}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	t := f.cur
	f.cur = f.cur.Add(f.step)
	f.ticks++
	return t
}

// Advance moves the clock by d without consuming a tick value.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cur = f.cur.Add(d)
}

// Ticks reports how many timestamps were handed out.
func (f *Fake) Ticks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ticks
}
