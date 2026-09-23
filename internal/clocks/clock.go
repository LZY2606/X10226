// Package clocks supplies the real wall clock and a controllable virtual
// clock used by tests so every replay uses fixed timestamps.
package clocks

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Virtual is a goroutine-safe fixed/advanced clock.
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

func (v *Virtual) Advance(d time.Duration) time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.now = v.now.Add(d)
	return v.now
}
