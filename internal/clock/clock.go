// Package clock provides a virtual clock with a fixed seed so tests are
// deterministic, never depend on the developer machine and never touch the
// public network.
package clock

import (
	"math/rand"
	"sync"
	"time"
)

// Fixed epoch for every replay: 2026-01-01T00:00:00Z.
var Epoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Clock is a deterministic, advanceable clock.
type Clock struct {
	mu  sync.Mutex
	now time.Time
	rng *rand.Rand
}

// New returns a clock anchored at Epoch seeded deterministically.
func New(seed int64) *Clock {
	return &Clock{now: Epoch, rng: rand.New(rand.NewSource(seed))}
}

func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock forward by d.
func (c *Clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	return c.now
}

// Intn returns a deterministic pseudo-random integer.
func (c *Clock) Intn(n int) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rng.Intn(n)
}

// RNG exposes the seeded source so generators are reproducible.
func (c *Clock) RNG() *rand.Rand {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.rng
}
