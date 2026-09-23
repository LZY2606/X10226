// Package clock provides a virtual clock used by tests and by deterministic
// case replay, while production simply advances with wall time.
package clock

import (
	"sync"
	"time"
)

// Clock returns RFC3339 timestamps.
type Clock interface {
	Now() time.Time
	Advance(d time.Duration) time.Time
	Set(t time.Time)
}

// Virtual is fully controllable and safe for concurrent use.
type Virtual struct {
	mu  sync.Mutex
	now time.Time
}

// NewVirtual starts at t (or a fixed epoch when zero).
func NewVirtual(t time.Time) *Virtual {
	if t.IsZero() {
		t = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	}
	return &Virtual{now: t}
}

func (c *Virtual) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *Virtual) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

func (c *Virtual) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Wall follows real time; Advance is a no-op-ish helper that returns Now.
type Wall struct{}

func (Wall) Now() time.Time               { return time.Now().UTC() }
func (Wall) Advance(d time.Duration) time.Time { return time.Now().UTC().Add(d) }
func (Wall) Set(time.Time)                {}
