// Package clock provides a virtual, deterministic clock used for event timestamps.
// Tests freeze/advance the clock; production uses wall time.
package clock

import (
	"sync"
	"time"
)

type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// New returns a clock seeded at a fixed, reproducible epoch.
func New() *Clock {
	return &Clock{now: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func NewAt(t time.Time) *Clock { return &Clock{now: t} }

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the virtual clock forward.
func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}
