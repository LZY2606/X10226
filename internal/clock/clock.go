// Package clock provides a virtual, seed-deterministic clock used by the engine
// in tests and replays. Production code uses System; deterministic tests inject
// Ticked so every recorded timestamp is reproducible.
package clock

import (
	"sync"
	"time"
)

// Epoch is the fixed origin for all virtual time used inside the application.
var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type Clock interface {
	Now() time.Time
}

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Ticked is a deterministic monotonic clock. Every call to Now advances time
// by a fixed step so two replays with the same seed produce identical records.
type Ticked struct {
	mu   sync.Mutex
	cur  time.Time
	step time.Duration
}

func NewTicked(seed int64, step time.Duration) *Ticked {
	return &Ticked{cur: Epoch.Add(time.Duration(seed) * time.Second), step: step}
}

func (t *Ticked) Now() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	v := t.cur
	t.cur = t.cur.Add(t.step)
	return v
}

// Fixed always returns the same time (useful for single-action tests).
type Fixed time.Time

func (f Fixed) Now() time.Time { return time.Time(f) }
