// Package clock provides an injectable time source. Servers use wall time;
// tests pin a virtual clock so every run is reproducible.
package clock

import (
	"sync"
	"sync/atomic"
	"time"
)

type Clock struct {
	step atomic.Int64
	mu   sync.Mutex
	base time.Time
}

func New(start time.Time) *Clock {
	c := &Clock{base: start}
	c.step.Store(start.Unix())
	return c
}

func (c *Clock) Now() time.Time {
	if c == nil {
		return time.Unix(0, 0).UTC()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.base.IsZero() {
		c.base = time.Unix(0, 0).UTC()
	}
	return time.Unix(c.step.Load(), 0).UTC()
}

// Step advances the virtual clock. Production code calls this rarely;
// the demo server ticks by one second on each mutating action.
func (c *Clock) Step(n int64) int64 { return c.step.Add(n) }

func (c *Clock) Unix() int64 { return c.step.Load() }
