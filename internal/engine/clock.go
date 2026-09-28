package engine

import (
	"sync/atomic"
	"time"
)

// Clock abstracts time so tests and the fixed-seed case pack use virtual time
// while the real server uses wall time.
type Clock interface {
	Now() time.Time
	VClock() int64
}

// FakeClock is a deterministic, monotonic clock.
type FakeClock struct {
	base time.Time
	tick atomic.Int64
	step time.Duration
}

func NewFakeClock(base time.Time, step time.Duration) *FakeClock {
	c := &FakeClock{base: base, step: step}
	return c
}

func (c *FakeClock) Now() time.Time {
	n := c.tick.Add(1)
	return c.base.Add(time.Duration(n) * c.step)
}

func (c *FakeClock) VClock() int64 { return c.Now().UnixNano() }

// RealClock uses wall time.
type RealClock struct{}

func (RealClock) Now() time.Time  { return time.Now().UTC() }
func (RealClock) VClock() int64   { return time.Now().UTC().UnixNano() }
