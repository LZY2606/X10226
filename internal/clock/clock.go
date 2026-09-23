// Package clock provides a swappable clock so the whole system can run on
// deterministic virtual time in tests and demos, and wall time in production.
package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC().Truncate(time.Second) }

// Virtual is a deterministic monotonic clock. It never reads the host clock.
type Virtual struct {
	mu  sync.Mutex
	now time.Time
}

func NewVirtual(start time.Time) *Virtual {
	return &Virtual{now: start.UTC()}
}

func (v *Virtual) Now() time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.now
}

// Advance moves the virtual clock forward. It never moves backwards.
func (v *Virtual) Advance(d time.Duration) time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.now = v.now.Add(d)
	return v.now
}

// Set moves the clock to t (t must be >= current time).
func (v *Virtual) Set(t time.Time) time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	if t.After(v.now) {
		v.now = t.UTC()
	}
	return v.now
}
