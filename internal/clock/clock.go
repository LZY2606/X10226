// Package clock provides a virtual clock used for deterministic replay.
//
// Wall-clock time never enters the domain directly. The clock is advanced
// explicitly in tests; production code advances it from the real wall clock
// while serving each request, preserving a monotonic, single-threaded order.
package clock

import (
	"sync"
	"time"
)

// Clock is a controllable, monotonic, thread-safe clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// New returns a Clock fixed at the RFC3339 instant 2026-01-01T00:00:00Z.
// The fixed origin keeps every replay identical across machines.
func New() *Clock {
	c := &Clock{}
	c.now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return c
}

// Now returns the current virtual time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d and returns the new time.
func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

// Set moves the clock to t; t must not be before the current time.
func (c *Clock) Set(t time.Time) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if t.After(c.now) {
		c.now = t
	}
	return c.now
}

// SyncTo advances to wall-clock time if it is later (server mode only).
func (c *Clock) SyncToWall() time.Time {
	return c.Set(time.Now().UTC())
}
