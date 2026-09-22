// Package clock provides an injectable clock so tests use fixed virtual time
// while the server uses wall time.
package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type Wall struct{}

func (Wall) Now() time.Time { return time.Now().UTC() }

// Virtual is a manually advanced UTC clock.
type Virtual struct {
	mu  sync.Mutex
	now time.Time
}

func NewVirtual(start time.Time) *Virtual {
	if start.IsZero() {
		start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &Virtual{now: start.UTC()}
}

func (v *Virtual) Now() time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.now
}

// Advance moves the clock forward.
func (v *Virtual) Advance(d time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.now = v.now.Add(d)
}
