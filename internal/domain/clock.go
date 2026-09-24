package domain

import (
	"strconv"
	"sync"
	"time"
)

// Clock is the only time source used by the system. Tests and the demo use a
// fixed, scripted clock so replay is deterministic; production uses wall time.
type Clock interface {
	Now() time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// WallClock returns real UTC time.
func WallClock() Clock { return wallClock{} }

// FakeClock is a thread-safe scripted clock with a fixed start.
type FakeClock struct {
	mu  sync.Mutex
	cur time.Time
}

// NewFakeClock starts at a fixed instant (2026-01-01T00:00:00Z).
func NewFakeClock() *FakeClock {
	t, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	return &FakeClock{cur: t}
}

func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

// Advance moves the clock forward by d and returns the new instant.
func (c *FakeClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = c.cur.Add(d)
	return c.cur
}

// Set moves the clock to an explicit instant.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = t
}

// Stamp is a monotonic ISO-8601 + counter helper for ordering within a tick.
type Stamp struct {
	clock Clock
	n     int
}

func NewStamp(c Clock) *Stamp { return &Stamp{clock: c} }

func (s *Stamp) Next() string {
	s.n++
	return s.clock.Now().Format("2006-01-02T15:04:05.000000Z") + "#" + strconv.Itoa(s.n)
}
