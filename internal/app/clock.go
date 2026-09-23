package app

import (
	"fmt"
	"time"
)

// Clock is the injectable virtual clock. Production uses fixed increments
// from real UTC; tests construct one from a fixed seed with zero network or
// wall-clock dependence.
type Clock struct {
	t   time.Time
	seq int
}

// NewClock seeds a deterministic clock (tests) at 2026-01-01T00:00:00Z.
func NewClock(seed string) *Clock {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := stableOffset(seed)
	return &Clock{t: base.Add(time.Duration(h) * time.Second)}
}

// RealClock seeds from the provided time (server uses time.Now once).
func RealClock(start time.Time) *Clock {
	return &Clock{t: start.UTC()}
}

// Now returns a strictly increasing, collision-free ISO timestamp.
func (c *Clock) Now() string {
	c.t = c.t.Add(1 * time.Second)
	c.seq++
	return c.t.Format("2006-01-02T15:04:05Z") + fmt.Sprintf("#%04d", c.seq)
}

func stableOffset(seed string) int64 {
	var h int64 = 1469598103934665603
	for i := 0; i < len(seed); i++ {
		h ^= int64(seed[i])
		h *= 1099511628211
	}
	if h < 0 {
		h = -h
	}
	return h % 86400
}

// ForwardTo ensures the next Now() tick is strictly after the n already-used
// positions, while never moving backwards. This keeps event ids stable across
// restarts and prevents replay/append timestamp collisions.
func (c *Clock) ForwardTo(n int) {
	for c.seq < n {
		c.t = c.t.Add(1 * time.Second)
		c.seq++
	}
}
