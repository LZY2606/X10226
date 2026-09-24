package engine

import (
	"sync"
	"time"
)

// Clock is the time source used for event timestamps. Production uses wall
// time; tests use a fixed, controllable virtual clock (never the developer
// machine's clock) so replays are deterministic.
type Clock interface {
	Now() time.Time
}

// WallClock is the production clock.
type WallClock struct{}

func (WallClock) Now() time.Time { return time.Now().UTC() }

// VirtualClock advances only when explicitly asked; fixed seed scenarios use
// an epoch so event ids are reproducible.
type VirtualClock struct {
	mu  sync.Mutex
	cur time.Time
}

func NewVirtualClock(start time.Time) *VirtualClock {
	return &VirtualClock{cur: start.UTC()}
}

func (c *VirtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

// Advance moves the clock forward and returns the new time.
func (c *VirtualClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = c.cur.Add(d)
	return c.cur
}
