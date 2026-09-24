// Package engine orchestrates the append-only event log: imports, layer
// revisions, deterministic recomputation with local invalidation, human
// decisions/conflicts/undo and the review state machine. All clocks are
// injected virtual clocks so tests are reproducible with a fixed seed.
package engine

import (
	"sync"
	"time"
)

// VirtualClock is an injectable, deterministic clock.
type VirtualClock struct {
	mu  sync.Mutex
	now time.Time
}

func NewVirtualClock(start time.Time) *VirtualClock {
	return &VirtualClock{now: start}
}

func (c *VirtualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves virtual time forward (panics on backwards steps).
func (c *VirtualClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

// Set jumps to an absolute virtual time.
func (c *VirtualClock) Set(t time.Time) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
	return c.now
}
